package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 「そのとき、こう指していたら」 — 되짚는 판에서 가정 수순을 직접 둬 본다.
//
// **여기가 HTTP 경로에서 엔진을 부르는 첫 자리다.** 리뷰의 나머지(review.go)는 DB와 룰
// 엔진만 쓰므로 엔진이 죽어도 그대로 돌지만, 가정 수순은 「그래서 상대가 어떻게 하나」가
// 내용이라 엔진 없이는 성립하지 않는다. 그래서 이 표면만 조건이 하나 더 붙고, 엔진이
// 없으면 **여기만** 꺼진다 — 되짚기는 계속 된다.
//
// **판은 클라이언트가 보내지 않는다.** 요청에 오는 것은 「기보의 몇 手目에서」와 「거기서
// 어떤 수를 뒀나」뿐이고, 뿌리 국면은 서버가 기록에서 다시 둬서 만든다. SFEN을 받으면
// 이 표면이 곧 **아무 국면이나 깊이 12로 재 주는 공개 엔진**이 된다 — 대국 세 판이 쓰는
// 풀을 남이 마음대로 쓰게 된다는 뜻이고, 그건 기능이 아니라 구멍이다.

const (
	// whatifDepth 는 가정 수순에서 상대가 두는 깊이다.
	//
	// 대국의 상대와 **같은 깊이지만 적응형이 아니다**(NewAdaptiveOpponent 를 안 쓴다).
	// 적응형은 판을 접전으로 유지하려고 밴드 안에서 일부러 최선이 아닌 수를 고르는데,
	// 가정 수순이 답해야 하는 것은 「그 수가 왜 나빴나」다 — 봐준 응수로 그리면 나쁜
	// 수가 안 나쁜 것으로 보인다(06-status.md §25).
	whatifDepth = game.DefaultDepth

	// whatifCandidates 는 ↖ 방향에 내놓는 최선수의 수다(03-frontend.md §3).
	whatifCandidates = 3

	// whatifMaxLine 은 분기 한 줄의 상한이다. 「그 다음엔 어떻게 됐을지」를 따라가기에
	// 넉넉하고, 요청 하나가 재생해야 하는 수를 유한하게 묶는다.
	whatifMaxLine = 60

	// whatifBodyLimit 은 본문 상한이다. 手数 하나와 수순 한 줄이 전부라 이보다 클 이유가 없다.
	whatifBodyLimit = 8 << 10

	// whatifTimeout 은 탐색 두 번에 주는 시한이다.
	//
	// **요청 ctx만으로는 안 된다.** `http.Server.Shutdown` 은 진행 중인 요청의 ctx를
	// 취소하지 않아서, 엔진이 물리면 그 탐색이 풀 슬롯을 붙든 채 종료까지 막는다
	// (usi.Pool.Close 주석이 같은 자리를 짚어 뒀다).
	whatifTimeout = 20 * time.Second
)

// Searcher 는 가정 수순이 엔진에 묻는 것 전부다. `*usi.Pool` 이 이걸 만족한다.
//
// **MultiPV 하나로 족하다.** k=1이면 상대의 수이고 k=3이면 ↖ 최선수 Top 3이라, 두 자리가
// 같은 질문의 크기만 다른 것이다.
type Searcher interface {
	SearchMultiPV(ctx context.Context, startSFEN string, moves []string, depth, multiPV int) (usi.SearchResult, error)
}

// whatifHandler 는 분기를 한 걸음 진행시킨다.
//
// **상태를 안 들고 있다.** 분기는 화면이 들고 있고 매번 통째로 온다 — 세션 goroutine이
// 소유하는 것은 **진행 중인 대국**이고, 여기는 이미 끝난 판의 가정이라 소유할 상태가 없다.
// 상대의 응수를 서버가 기억하지 않는 것도 그래서다. 같은 국면·같은 깊이가 늘 같은 수를
// 주지는 않으므로(06-status.md §34 ②) **화면이 받은 수를 그대로 돌려보내야** 되돌아갔다
// 다시 와도 같은 수순이 선다.
type whatifHandler struct {
	store  *store.Store
	search Searcher
}

// whatifRequest 는 「기보의 ply 手目에서 이 수순을 뒀다면」이다.
//
// Moves 에는 **상대의 응수도 함께** 들어 있다. 사람의 수만 보내고 서버가 매번 다시
// 답하게 하면, 되돌아갈 때마다 상대가 다른 수를 둘 수 있다.
type whatifRequest struct {
	Ply   int      `json:"ply"`
	Moves []string `json:"moves"`
}

// whatifMove 는 분기의 한 수다. `reviewMove` 와 같은 어휘를 쓴다 — 같은 것을 두 이름으로
// 부르면 화면이 실제 기보와 가정 수순에서 다른 타입을 갖는다.
type whatifMove struct {
	Ply int       `json:"ply"`
	USI string    `json:"usi"`
	Ja  string    `json:"ja"`
	By  game.Side `json:"by"`
	// SFEN 은 **이 수를 둔 뒤**의 국면이다. 화면은 그대로 그린다 — 여기서도 수를 두지 않는다.
	SFEN    string `json:"sfen"`
	Checked string `json:"checked,omitempty"`
}

// whatifCandidate 는 ↖ 방향의 한 수다 — 「최선수 Top 3」.
//
// **최선수를 대국 중에 보여주지 않는 것과 어긋나지 않는다**(01-core.md §7). 저쪽은
// 지금 둘 수를 알려주지 않는 것이고, 여기는 이미 끝난 판에서 「그때 무엇이 있었나」다.
type whatifCandidate struct {
	USI string `json:"usi"`
	Ja  string `json:"ja"`
	// EvalCp 는 **그 수를 둔 쪽 관점** cp다. 뿌리에서 두는 쪽은 늘 사람이므로 곧 사람 관점이다.
	EvalCp int `json:"evalCp"`
	// MateIn 은 詰み까지의 手数다. 0이면 詰み이 아니다 — cp만 내보내면 30000이라는
	// 숫자가 화면에 그대로 나가고, 그건 평가치가 아니라 환산값이다.
	MateIn int `json:"mateIn,omitempty"`
}

// whatifNode 는 분기의 **지금 서 있는 자리**다.
//
// **언제나 사람 차례이거나 끝난 국면이다.** 상대 차례면 서버가 그 자리에서 두게 하고
// 그 수를 Line 끝에 붙여서 돌려준다 — 화면이 「이제 누가 둘 차례인가」로 갈라질 일이 없다.
type whatifNode struct {
	// BasePly 는 분기가 갈라져 나온 기보의 手数다. 실제로 둔 판으로 돌아가는 자리이기도 하다.
	BasePly int `json:"basePly"`
	// Ply 는 지금 국면의 手数(BasePly + 분기 길이)다.
	Ply      int    `json:"ply"`
	SFEN     string `json:"sfen"`
	YourTurn bool   `json:"yourTurn"`
	Checked  string `json:"checked,omitempty"`
	// Status 는 대국과 같은 어휘다. 千日手는 여기서 안 본다 — 아래 주석 참조.
	Status game.Status `json:"status"`
	// LegalMoves 는 **화면이 규칙을 모르기 때문에** 온다. 대국의 스냅샷과 같은 자리다.
	LegalMoves []string `json:"legalMoves"`
	// EvalCp 는 지금 국면의 **사람 관점** cp다. 끝난 국면이면 없다.
	EvalCp *int `json:"evalCp,omitempty"`
	MateIn int  `json:"mateIn,omitempty"`

	Line       []whatifMove      `json:"line"`
	Candidates []whatifCandidate `json:"candidates"`
}

// 요청이 거절되는 이유들. 화면에 나갈 문구는 핸들러가 붙인다.
var (
	// errWhatifPly 는 그 手数를 기록에서 재현할 수 없다는 뜻이다. 범위 밖이거나, 기보에
	// 구멍이 나서 거기까지 못 둔다(review.go 의 재현이 멈추는 자리와 같은 조건이다).
	errWhatifPly = errors.New("whatif: cannot replay the record to that ply")
	// errWhatifMove 는 분기의 수가 그 국면에서 둘 수 없는 수라는 뜻이다.
	errWhatifMove = errors.New("whatif: illegal move in the branch")
	// errWhatifEngine 은 엔진이 답하지 못했다는 뜻이다.
	errWhatifEngine = errors.New("whatif: engine")
)

func (h *whatifHandler) play(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_id", "message": "対局番号が正しくありません。",
		})
		return
	}

	var req whatifRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, whatifBodyLimit)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_request", "message": "リクエストを読み取れませんでした。",
		})
		return
	}
	if req.Ply < 0 || len(req.Moves) > whatifMaxLine {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_line", "message": "この手順はこれ以上進められません。",
		})
		return
	}

	rec, err := h.store.GameRecord(r.Context(), id)
	if errors.Is(err, store.ErrNoGame) {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "not_found", "message": "その対局は見つかりません。",
		})
		return
	}
	if err != nil {
		log.Printf("whatif: game %d: %v", id, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "対局の記録を読み込めませんでした。",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), whatifTimeout)
	defer cancel()

	node, err := whatifNodeOf(ctx, rec, req, h.search)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, node)
	case errors.Is(err, errWhatifPly):
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_ply", "message": "この手数からは試せません。",
		})
	case errors.Is(err, errWhatifMove):
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_move", "message": "その手はここでは指せません。",
		})
	default:
		// 엔진 고장·시한 초과. **대국이 안 되는 것과 같은 종류라 503이다** — 다시 눌러
		// 볼 수 있는 실패이고, 판을 되짚는 쪽은 여전히 살아 있다.
		log.Printf("whatif: game %d ply %d: %v", id, req.Ply, err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "engine_unavailable", "message": "エンジンが応答しませんでした。",
		})
	}
}

// whatifNodeOf 는 분기를 한 걸음 진행시킨다. **DB를 안 탄다** — 기록을 손에 들고 있는
// 채로 도는 함수라, 엔진 하나만 손으로 만들어 넣으면 전부 확인할 수 있다.
func whatifNodeOf(ctx context.Context, rec store.GameRecord, req whatifRequest, search Searcher) (whatifNode, error) {
	start, err := shogi.ParseSFEN(startSFENOf(rec.StartSFEN))
	if err != nil {
		// 시작 국면을 못 읽으면 **한 수도 두지 않는다.** 평수 초기 국면으로 대신 두면
		// 한 번도 없었던 국면 위에서 가정을 세우게 된다(detailOf 와 같은 판단이다).
		return whatifNode{}, fmt.Errorf("%w: start sfen %q: %v", errWhatifPly, rec.StartSFEN, err)
	}

	human := shogi.Black
	if rec.MyColor == "w" {
		human = shogi.White
	}

	pos, prevTo, err := replayTo(start, rec.Moves, req.Ply)
	if err != nil {
		return whatifNode{}, err
	}

	// 엔진에 보낼 수순. **뿌리까지의 실제 기보를 그대로 앞에 둔다** — 국면만 넘기면
	// 千日手를 세는 근거가 사라진다.
	line := make([]string, 0, req.Ply+len(req.Moves)+1)
	for i := range req.Ply {
		line = append(line, rec.Moves[i].USI)
	}

	node := whatifNode{
		BasePly:    req.Ply,
		Ply:        req.Ply,
		Status:     game.StatusPlaying,
		Line:       make([]whatifMove, 0, len(req.Moves)+1),
		Candidates: []whatifCandidate{},
	}

	for _, u := range req.Moves {
		mv, next, ok := step(pos, prevTo, node.Ply, u, human)
		if !ok {
			return whatifNode{}, fmt.Errorf("%w: %q", errWhatifMove, u)
		}
		node.Line = append(node.Line, mv)
		pos, prevTo, node.Ply = next, lastTo(u), node.Ply+1
		line = append(line, u)
	}

	// 상대 차례면 상대가 둔다. **가정 수순의 내용이 「그래서 상대가 어떻게 하나」다** —
	// 물어보게 하면 그 자리에서 「다음へ」를 누르는 통과 의례가 하나 더 생긴다.
	if pos.Turn != human && !pos.NoLegalMoves() {
		res, err := search.SearchMultiPV(ctx, start.SFEN(), line, whatifDepth, 1)
		if err != nil {
			return whatifNode{}, fmt.Errorf("%w: reply: %w", errWhatifEngine, err)
		}
		mv, next, ok := step(pos, prevTo, node.Ply, res.Best, human)
		if !ok {
			// 投了(`resign`)이거나 못 두는 수다. **엔진 출력을 믿지 않는 것**은 대국
			// 루프와 같고(session.applyEngineMove), 어느 쪽이든 여기서 판이 끝난다.
			node.SFEN = pos.SFEN()
			node.Checked = checkedSquare(pos)
			node.Status = game.StatusResigned
			return node, nil
		}
		node.Line = append(node.Line, mv)
		pos, prevTo, node.Ply = next, lastTo(res.Best), node.Ply+1
		line = append(line, res.Best)
	}

	node.SFEN = pos.SFEN()
	node.Checked = checkedSquare(pos)
	node.YourTurn = pos.Turn == human

	legal := pos.LegalMoves()
	if len(legal) == 0 {
		// **千日手는 여기서 안 본다.** 그건 같은 국면이 네 번 나왔는가라서 수순 전체를
		// 세야 하는데, 분기는 실제 대국이 아니라 「둬 보는 것」이고 거기까지 가는 일이
		// 거의 없다. 없는 것을 절반만 세느니 안 센다.
		node.Status = game.StatusCheckmate
		if !pos.InCheck(pos.Turn) {
			node.Status = game.StatusStalemate
		}
		return node, nil
	}

	node.LegalMoves = make([]string, 0, len(legal))
	for _, m := range legal {
		node.LegalMoves = append(node.LegalMoves, m.USI())
	}

	res, err := search.SearchMultiPV(ctx, start.SFEN(), line, whatifDepth, whatifCandidates)
	if err != nil {
		return whatifNode{}, fmt.Errorf("%w: candidates: %w", errWhatifEngine, err)
	}
	// 엔진은 늘 **수번 측 관점**으로 답한다. 지금 수번은 사람이므로 그대로 사람 관점이다.
	cp := res.ScoreCp
	node.EvalCp = &cp
	if res.IsMate {
		node.MateIn = res.MateIn
	}
	node.Candidates = candidatesOf(pos, prevTo, res)
	return node, nil
}

// candidatesOf 는 탐색의 후보들을 화면이 그릴 수 있는 모양으로 옮긴다.
//
// **여기서도 엔진 출력을 검증한다.** 못 두는 수가 하나 섞이면 그 줄만 버린다 —
// 화면에서 「이렇게 뒀어야 한다」는 단언이라 틀린 것을 그리느니 적게 그린다.
func candidatesOf(pos shogi.Position, prevTo int, res usi.SearchResult) []whatifCandidate {
	out := make([]whatifCandidate, 0, whatifCandidates)
	seen := make(map[string]bool, whatifCandidates)

	for rank := 1; rank <= whatifCandidates; rank++ {
		for _, l := range res.Lines {
			if l.MultiPV != rank || seen[l.Move] {
				continue
			}
			m, err := shogi.ParseUSIMove(l.Move)
			if err != nil || pos.ValidateMove(m) != nil {
				break
			}
			c := whatifCandidate{USI: l.Move, Ja: pos.MoveJa(m, prevTo), EvalCp: l.ScoreCp}
			if l.IsMate {
				c.MateIn = l.MateIn
			}
			seen[l.Move] = true
			out = append(out, c)
			break
		}
	}
	return out
}

// replayTo 는 기록을 ply 手目까지 다시 둔다. 두 번째 값은 그 手数의 도착 칸이다(「同」이 본다).
//
// **구멍이 나면 거기서 거절한다.** review.go 의 재현은 그 자리에서 멈추고 뒤를 표기 없이
// 내보내면 되지만, 여기는 그 국면 **위에서 새로 두는** 일이라 어긋난 판이면 아예 안 된다.
func replayTo(start shogi.Position, moves []store.RecordedMove, ply int) (shogi.Position, int, error) {
	if ply > len(moves) {
		return start, -1, fmt.Errorf("%w: ply %d > %d recorded", errWhatifPly, ply, len(moves))
	}
	pos, prevTo := start, -1
	for i := range ply {
		m := moves[i]
		if m.Ply != i+1 {
			return pos, prevTo, fmt.Errorf("%w: ply %d is missing", errWhatifPly, i+1)
		}
		next, _, ok := advance(pos, prevTo, m.USI)
		if !ok {
			return pos, prevTo, fmt.Errorf("%w: cannot replay ply %d (%s)", errWhatifPly, m.Ply, m.USI)
		}
		pos, prevTo = next, lastTo(m.USI)
	}
	return pos, prevTo, nil
}

// step 은 한 수를 두어 본다. 못 두는 수면 ok=false — **사람의 수도 엔진의 수도 여기를 지난다.**
func step(pos shogi.Position, prevTo, ply int, u string, human shogi.Color) (whatifMove, shogi.Position, bool) {
	m, err := shogi.ParseUSIMove(u)
	if err != nil {
		return whatifMove{}, pos, false
	}
	if err := pos.ValidateMove(m); err != nil {
		return whatifMove{}, pos, false
	}
	by := game.SideEngine
	if pos.Turn == human {
		by = game.SideHuman
	}
	// 표기는 **두기 전** 국면에서 나온다. 두고 나면 그 駒가 이미 도착 칸에 서 있어서
	// 어느 駒가 갔는지 구별이 안 된다.
	ja := pos.MoveJa(m, prevTo)
	next := pos.Apply(m)
	return whatifMove{
		Ply:     ply + 1,
		USI:     u,
		Ja:      ja,
		By:      by,
		SFEN:    next.SFEN(),
		Checked: checkedSquare(next),
	}, next, true
}
