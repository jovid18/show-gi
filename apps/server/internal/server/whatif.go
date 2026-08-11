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

	"github.com/jovid18/show-gi/apps/server/internal/archive"
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
	// whatifDepth 는 가정 수순을 재는 깊이다.
	//
	// **대국이 쓰는 것과 같은 값이어야 한다.** 다르면 `positions` 에 쌓인 분석이 서로
	// 못 쓰는 두 무리로 갈리고(캐시는 깊이로 견준다), 같은 국면의 값이 어디서 왔느냐에
	// 따라 달라진다 — 그건 데이터로 남기는 뜻을 없앤다. 상대의 수(DefaultDepth)와 개입
	// 판정(JudgeDepth)이 12이므로 여기도 12다.
	whatifDepth = game.DefaultDepth

	// whatifCandidates 는 한 국면에 내놓는 후보의 수다. 첫 번째가 화면의 초록 화살표이고,
	// 셋이 되짚기의 「최선수 Top 3」이다(03-frontend.md §3).
	whatifCandidates = 3

	// whatifMaxLine 은 분기 한 줄의 상한이다. 「그 다음엔 어떻게 됐을지」를 따라가기에
	// 넉넉하고, 요청 하나가 재생해야 하는 수를 유한하게 묶는다.
	whatifMaxLine = 60

	// whatifBodyLimit 은 본문 상한이다. 手数 하나와 수순 한 줄이 전부라 이보다 클 이유가 없다.
	whatifBodyLimit = 8 << 10

	// whatifTimeout 은 탐색 하나에 주는 시한이다.
	//
	// **요청 ctx만으로는 안 된다.** `http.Server.Shutdown` 은 진행 중인 요청의 ctx를
	// 취소하지 않아서, 엔진이 물리면 그 탐색이 풀 슬롯을 붙든 채 종료까지 막는다
	// (usi.Pool.Close 주석이 같은 자리를 짚어 뒀다).
	whatifTimeout = 20 * time.Second
)

// Searcher 는 가정 수순이 엔진에 묻는 것 전부다. `*usi.Pool` 이 이걸 만족한다.
//
// **MultiPV 하나로 족하다.** 한 번의 탐색이 이 국면의 값과 최선수(화면의 화살표)와
// 다음 후보들을 함께 준다.
type Searcher interface {
	SearchMultiPV(ctx context.Context, startSFEN string, moves []string, depth, multiPV int) (usi.SearchResult, error)
}

// Cache 는 **이미 잰 국면을 다시 꺼내는** 자리다. `*store.Store` 가 이걸 만족한다.
//
// **쓰는 쪽은 여기 없다.** 기록은 탐색 자체에 붙어 있어서(`internal/archive`) 이 표면이
// 신경 쓸 것이 아니다 — 네 자리 중 하나를 빠뜨리지 않는 방법이 그것이었다.
//
// nil이면 매번 다시 재고 답은 같다. 대신 둘을 잃는다:
//
//  1. **같은 국면을 두 번 재지 않는다.** 탐색은 물렀다 나아가기·다른 분기에서 같은 국면으로
//     같은 자리를 계속 밟는다
//  2. **숫자가 안 흔들린다.** 같은 국면·같은 깊이가 치환표 상태에 따라 ±150cp 갈리는데
//     (06-status.md §34 ②), 한 번 잰 값을 들고 있으면 물러도 후보가 글자까지 같다
type Cache interface {
	GetPosition(ctx context.Context, sfenKey string) (store.Position, error)
}

// cacheOf 는 캐시가 있으면 그것을, 없으면 nil을 준다.
//
// **`*store.Store` 를 그대로 인터페이스에 넣지 않는다.** nil 포인터를 넣으면 인터페이스
// 값 자체는 non-nil이 되어 `== nil` 검사를 통과하고 다음 줄에서 죽는다 — main.go 가
// mate solver 에서 같은 자리를 이미 밟았다.
func cacheOf(st *store.Store) Cache {
	if st == nil {
		return nil
	}
	return st
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

// whatifRequest 는 「ply 手目에서 이 수순을 뒀다면」이다.
//
// Moves 에는 **상대의 응수도 함께** 들어 있다. 사람의 수만 보내고 서버가 매번 다시
// 답하게 하면, 되돌아갈 때마다 상대가 다른 수를 둘 수 있다.
type whatifRequest struct {
	Ply   int      `json:"ply"`
	Moves []string `json:"moves"`
}

// whatifRoot 은 분기가 자라날 **정본**이다. 요청은 여기에 대해서만 뜻이 있다.
//
// **두 표면이 이걸 서로 다른 곳에서 얻는다.** 끝난 판은 DB 기록에서(review.go), 두는 중인
// 판은 ws 핸들러가 이미 받아 둔 스냅샷에서(ws.go) 만든다. 어느 쪽이든 **클라이언트가
// 판을 보내지 않는 것**이 조건이고, 그래서 아래 판정 코드가 한 벌로 족하다.
type whatifRoot struct {
	// StartSFEN 은 0手目의 국면. 비어 있으면 평수 초기 국면이다.
	StartSFEN string
	// Moves 는 **확정된 수**다. 여기까지가 실제로 벌어진 일이고, 분기는 그 뒤에 붙는다.
	Moves []string
	Human shogi.Color
}

// rootOf 는 DB 기록에서 뿌리를 만든다.
//
// **구멍에서 끊는다.** 手数에 구멍이 난 기보(큐가 넘쳐 한 수가 빠졌다)를 이어 두면 3手目를
// 2手目 자리에 두는 셈이라 없던 국면이 나온다 — 되짚기는 거기서 멈추고 뒤를 표기 없이
// 내보내면 되지만(review.go), 분기는 그 국면 **위에서 새로 두는** 일이라 아예 안 된다.
// 끊어 두면 그 뒤의 手数는 아래에서 「기록 밖」으로 거절된다.
func rootOf(rec store.GameRecord) whatifRoot {
	root := whatifRoot{StartSFEN: rec.StartSFEN, Human: shogi.Black}
	if rec.MyColor == "w" {
		root.Human = shogi.White
	}
	for i, m := range rec.Moves {
		if m.Ply != i+1 {
			break
		}
		root.Moves = append(root.Moves, m.USI)
	}
	return root
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

// whatifCandidate 는 그 국면에서 **수번 쪽**이 둘 수 있는 좋은 수 하나다.
//
// 첫 번째가 최선수이고, 그것이 곧 화면의 **초록 화살표**다 — 「다음에 올 수」에 배정된
// 채널이라 새 신호를 꺼내지 않는다(03-frontend.md §2).
//
// **최선수를 대국 중에 보여주지 않는 것과 어긋나지 않는다**(01-core.md §7). 저쪽은 **지금
// 다시 둘 국면**의 답을 알려주지 않는 것이고, 여기는 이미 벌어진 수 **뒤**의 가정이다 —
// 그래서 대국 중에는 분기의 뿌리가 물러진 수보다 앞으로 못 간다(ws.go).
type whatifCandidate struct {
	USI string `json:"usi"`
	Ja  string `json:"ja"`
	// EvalCp 는 **그 수를 둔 쪽 관점** cp다. 두는 쪽이 늘 사람은 아니므로(분기에서는
	// 상대의 수도 사람이 고른다) 이 값의 주인은 `Turn` 이다.
	EvalCp int `json:"evalCp"`
	// LossCp 는 최선수 대비 낙폭이다. 최선수는 0 — 「이 수를 고르면 얼마를 내주나」다.
	LossCp int `json:"lossCp"`
	// MateIn 은 詰み까지의 手数다. 0이면 詰み이 아니다 — cp만 내보내면 30000이라는
	// 숫자가 화면에 그대로 나가고, 그건 평가치가 아니라 환산값이다.
	MateIn int `json:"mateIn,omitempty"`
}

// whatifNode 는 분기의 **지금 서 있는 자리**다. 「국면 하나 = 노드 하나」이고, 화면이
// 넘겨 보는 것도 둬 보는 것도 전부 이 하나를 묻는 일이다.
//
// **응수를 서버가 대신 두지 않는다.** 수번이 누구든 그 쪽 합법수를 내주고, 최선수는
// 화살표로 알려주기만 한다 — 두는 것은 사람의 몫이다. 그래서 한 걸음에 탐색이 한 번이고,
// 「상대라면 어떻게 두나」를 직접 둬 보는 것도 같은 장치로 된다.
type whatifNode struct {
	// BasePly 는 분기가 갈라져 나온 手数다. 「分岐の前へ」가 돌아가는 자리다.
	BasePly int `json:"basePly"`
	// Ply 는 지금 국면의 手数(BasePly + 분기 길이)다.
	Ply  int    `json:"ply"`
	SFEN string `json:"sfen"`
	// Turn 은 지금 수번이다. 합법수와 후보가 **이 쪽의 것**이고, 화면은 이 값으로 어느
	// 駒台를 집을 수 있는지를 정한다.
	Turn     string `json:"turn"` // "b" | "w"
	YourTurn bool   `json:"yourTurn"`
	Checked  string `json:"checked,omitempty"`
	// Status 는 대국과 같은 어휘다. 千日手는 여기서 안 본다 — 아래 주석 참조.
	Status game.Status `json:"status"`
	// LegalMoves 는 **화면이 규칙을 모르기 때문에** 온다. 대국의 스냅샷과 같은 자리다.
	LegalMoves []string `json:"legalMoves"`
	// EvalCp 는 지금 국면의 **플레이어 관점** cp다. 끝난 국면이면 없다.
	//
	// **줄의 마지막 수가 만든 값이 곧 이것이다.** 화면은 앞선 노드를 이미 받아 뒀으므로
	// 수마다의 cp를 여기서 또 보내지 않는다 — 추가 탐색이 0인 이유다.
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

	node, err := whatifNodeOf(ctx, rootOf(rec), req, h.search, cacheOf(h.store))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, node)
	case errors.Is(err, errWhatifPly):
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_ply", "message": whatifMessages["bad_ply"],
		})
	case errors.Is(err, errWhatifMove):
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_move", "message": whatifMessages["bad_move"],
		})
	default:
		// 엔진 고장·시한 초과. **대국이 안 되는 것과 같은 종류라 503이다** — 다시 눌러
		// 볼 수 있는 실패이고, 판을 되짚는 쪽은 여전히 살아 있다.
		log.Printf("whatif: game %d ply %d: %v", id, req.Ply, err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "engine_unavailable", "message": whatifMessages["engine_unavailable"],
		})
	}
}

// whatifMessages 는 거절을 화면에 나갈 일본어로 옮긴다.
//
// **두 표면이 같은 표를 쓴다.** HTTP(리뷰)와 ws(대국 중)가 같은 실패를 다른 말로 말하면
// 같은 일이 두 화면에서 다른 일로 읽힌다.
var whatifMessages = map[string]string{
	"bad_ply":            "この手数からは試せません。",
	"bad_move":           "その手はここでは指せません。",
	"bad_line":           "この手順はこれ以上進められません。",
	"engine_unavailable": "エンジンが応答しませんでした。",
	"busy":               "まだ読んでいます。",
}

// whatifReason 은 에러를 기계용 코드로 옮긴다. 문구는 위 표가 붙인다.
func whatifReason(err error) string {
	switch {
	case errors.Is(err, errWhatifPly):
		return "bad_ply"
	case errors.Is(err, errWhatifMove):
		return "bad_move"
	default:
		return "engine_unavailable"
	}
}

// whatifNodeOf 는 분기를 한 걸음 진행시킨다. **세션을 안 탄다** — 뿌리를 손에 들고 있는
// 채로 도는 함수라, 엔진 하나만 손으로 만들어 넣으면 전부 확인할 수 있다(cache는 nil로 둔다).
func whatifNodeOf(
	ctx context.Context,
	root whatifRoot,
	req whatifRequest,
	search Searcher,
	cache Cache,
) (whatifNode, error) {
	start, err := shogi.ParseSFEN(startSFENOf(root.StartSFEN))
	if err != nil {
		// 시작 국면을 못 읽으면 **한 수도 두지 않는다.** 평수 초기 국면으로 대신 두면
		// 한 번도 없었던 국면 위에서 가정을 세우게 된다(detailOf 와 같은 판단이다).
		return whatifNode{}, fmt.Errorf("%w: start sfen %q: %v", errWhatifPly, root.StartSFEN, err)
	}

	human := root.Human

	pos, prevTo, err := replayTo(start, root.Moves, req.Ply)
	if err != nil {
		return whatifNode{}, err
	}

	// 엔진에 보낼 수순. **뿌리까지의 실제 수순을 그대로 앞에 둔다** — 국면만 넘기면
	// 千日手를 세는 근거가 사라진다.
	line := make([]string, 0, req.Ply+len(req.Moves)+1)
	line = append(line, root.Moves[:req.Ply]...)

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

	node.SFEN = pos.SFEN()
	node.Checked = checkedSquare(pos)
	// SFEN·기록과 같은 한 글자를 쓴다(`games.my_color` 도 이것이다). `Color.String()` 은
	// `sente`/`gote` 라 여기 쓰면 화면이 세 번째 어휘를 갖는다.
	node.Turn = "b"
	if pos.Turn == shogi.White {
		node.Turn = "w"
	}
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

	// **탐색은 한 번이고, 이미 잰 국면이면 0번이다.** 이 하나가 세 가지를 준다 —
	// 이 국면의 값, 수번 쪽의 최선수(화면의 초록 화살표), 그리고 그 다음 후보들.
	cands, err := evalOf(ctx, search, cache, pos, start.SFEN(), line, min(whatifCandidates, len(legal)))
	if err != nil {
		return whatifNode{}, fmt.Errorf("%w: %w", errWhatifEngine, err)
	}
	if len(cands) == 0 {
		// 합법수가 있는데 후보가 하나도 없다. 엔진이 답을 준 적이 없다는 뜻이라,
		// 값을 지어내지 않고 「모른다」로 내보낸다.
		return node, nil
	}

	// 캐시의 cp는 **수번 측 관점**이다(store.Candidate). 여기는 **플레이어 관점**으로
	// 내보낸다 — 상대 차례의 국면까지 상대 관점으로 보내면 한 줄을 넘겨 보는 동안 부호가
	// 뒤집히고, 그러면 「좋아지고 있나」를 사람이 읽을 수 없다(리뷰의 기보가 같은 규약이다).
	cp := playerCp(cands[0].Cp, pos.Turn, human)
	node.EvalCp = &cp
	node.MateIn = playerCp(cands[0].MateIn, pos.Turn, human)
	node.Candidates = candidatesOf(pos, prevTo, cands)
	return node, nil
}

// evalOf 는 이 국면의 상위 후보들이다. **캐시가 먼저다.**
//
// 캐시를 쓰는 조건이 둘이다 — 깊이가 모자라지 않고, **후보 수가 모자라지 않아야** 한다.
// 뒤엣것을 빼면 개입 판정이 k=1로 남긴 행이 「최선수 Top 3」을 1개로 만든다. 이 표면은
// 3을 약속했으니 그때는 다시 잰다(그리고 그 결과가 그 행을 덮는다, query/positions.sql).
func evalOf(
	ctx context.Context,
	search Searcher,
	cache Cache,
	pos shogi.Position,
	startSFEN string,
	line []string,
	want int,
) ([]store.Candidate, error) {
	// **감싼 층도 캐시를 읽는다**(internal/archive) — 여기가 남아 있는 이유는 둘이다.
	// ① 이 표면은 후보 **셋**을 약속하는데, 합법수가 그보다 적은 국면에서는 셋이 애초에
	// 없다(저쪽은 k=3을 못 채우는 행을 안 쓴다). ② 히트면 탐색 결과를 되짚어 만드는
	// 일까지 건너뛴다 — 되짚는 화면은 手数를 옮길 때마다 이 자리를 지난다.
	if cache != nil {
		p, err := cache.GetPosition(ctx, archive.Key(pos))
		switch {
		case err == nil && p.ComputedDepth >= whatifDepth && len(p.Candidates) >= want:
			return p.Candidates, nil
		case err != nil && !errors.Is(err, store.ErrNoPosition):
			// 캐시가 고장 나도 탐색은 된다. 조용히 넘기지 않고 다시 잰다.
			log.Printf("whatif: read cache: %v", err)
		}
	}

	res, err := search.SearchMultiPV(ctx, startSFEN, line, whatifDepth, whatifCandidates)
	if err != nil {
		return nil, err
	}
	// **여기서 쓰지 않는다.** 탐색을 감싼 쪽이 이미 남겼다(internal/archive) — 두 자리에서
	// 쓰면 한 자리가 빠지거나 두 벌이 어긋난다.
	return archive.Candidates(res), nil
}

// playerCp 는 **수번 측 관점** 값을 플레이어 관점으로 옮긴다.
func playerCp(moverCp int, turn, human shogi.Color) int {
	if turn == human {
		return moverCp
	}
	return -moverCp
}

// candidatesOf 는 탐색의 후보들을 화면이 그릴 수 있는 모양으로 옮긴다.
//
// **여기서도 엔진 출력을 검증한다.** 못 두는 수가 하나 섞이면 그 줄만 버린다 —
// 화면에서 「이렇게 뒀어야 한다」는 단언이라 틀린 것을 그리느니 적게 그린다.
func candidatesOf(pos shogi.Position, prevTo int, cands []store.Candidate) []whatifCandidate {
	out := make([]whatifCandidate, 0, whatifCandidates)

	for _, l := range cands {
		if len(out) == whatifCandidates {
			break
		}
		m, err := shogi.ParseUSIMove(l.USI)
		if err != nil || pos.ValidateMove(m) != nil {
			continue
		}
		c := whatifCandidate{USI: l.USI, Ja: pos.MoveJa(m, prevTo), EvalCp: l.Cp, MateIn: l.MateIn}
		// 낙폭은 **최선수 대비**다. 화면이 뺄셈을 하지 않는다 — 두 값을 나란히 두면
		// 어느 쪽이 기준인지가 흐려지고, 詰み 환산값(30000)이 섞이면 더 그렇다.
		if len(out) > 0 {
			c.LossCp = out[0].EvalCp - c.EvalCp
		}
		out = append(out, c)
	}
	return out
}

// replayTo 는 정본 수순을 ply 手目까지 다시 둔다. 두 번째 값은 그 手数의 도착 칸이다
// (「同」 표기가 본다).
//
// **범위를 넘으면 거절한다.** 뿌리가 구멍에서 끊겨 있으면(rootOf) 그 뒤의 手数가 여기서
// 「기록 밖」으로 걸린다.
func replayTo(start shogi.Position, moves []string, ply int) (shogi.Position, int, error) {
	if ply < 0 || ply > len(moves) {
		return start, -1, fmt.Errorf("%w: ply %d is not in the first %d moves", errWhatifPly, ply, len(moves))
	}
	pos, prevTo := start, -1
	for _, u := range moves[:ply] {
		next, _, ok := advance(pos, prevTo, u)
		if !ok {
			return pos, prevTo, fmt.Errorf("%w: cannot replay %s", errWhatifPly, u)
		}
		pos, prevTo = next, lastTo(u)
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
