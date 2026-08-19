package server

import (
	"context"
	"encoding/json"
	"errors"
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
// 판(SFEN)을 클라이언트에서 받지 않는다. 받으면 이 표면이 곧 공개 엔진이 되고,
// 그 풀은 대국이 쓰는 것과 같다 — 근거와 표면 조건은 journal §37.

const (
	// whatifDepth 는 가정 수순의 깊이다. 대국이 쓰는 것과 같아야 한다 — 다르면
	// positions 가 서로 못 쓰는 두 무리로 갈린다(캐시는 깊이로 견준다, 02-architecture.md §4).
	whatifDepth = game.DefaultDepth

	// whatifCandidates 는 한 국면에 내놓는 후보의 수다. 첫 번째가 화면의 초록 화살표이고,
	// 셋이 되짚기의 「최선수 Top 3」이다(03-frontend.md §3).
	whatifCandidates = 3

	// whatifMaxLine 은 분기 한 줄의 상한이다. 「그 다음엔 어떻게 됐을지」를 따라가기에
	// 넉넉하고, 요청 하나가 재생해야 하는 수를 유한하게 묶는다.
	whatifMaxLine = 60

	// whatifBodyLimit 은 본문 상한이다. 手数 하나와 수순 한 줄이 전부라 이보다 클 이유가 없다.
	whatifBodyLimit = 8 << 10

	// whatifTimeout 은 탐색 하나에 주는 시한이다. 요청 ctx만으로는 안 된다 —
	// http.Server.Shutdown 이 진행 중 요청의 ctx를 취소하지 않아 종료가 막힌다(usi.Pool.Close).
	whatifTimeout = 20 * time.Second
)

// Searcher 는 가정 수순이 엔진에 묻는 것 전부다 — *usi.Pool 이 이걸 만족한다.
// MultiPV 하나로 족하다(근거는 whatifNodeOf 의 탐색 자리).
type Searcher interface {
	SearchMultiPV(ctx context.Context, startSFEN string, moves []string, depth, multiPV int) (usi.SearchResult, error)
}

// Cache 는 이미 잰 국면을 다시 꺼내는 자리다. *store.Store 가 이걸 만족한다.
// 쓰는 쪽은 여기 없다 — 기록은 탐색 자체에 붙어 있다(internal/archive).
//
// nil이면 답은 같고 둘을 잃는다: 같은 국면을 다시 재는 것과, 물렀을 때 숫자가
// 흔들리는 것(journal §34 ②).
type Cache interface {
	GetPosition(ctx context.Context, sfenKey string) (store.Position, error)
}

// cacheOf 는 캐시가 있으면 그것을, 없으면 nil을 준다.
//
// *store.Store 를 그대로 인터페이스에 넣지 않는다. nil 포인터를 넣으면 인터페이스
// 값 자체는 non-nil이 되어 == nil 검사를 통과하고 다음 줄에서 죽는다 — main.go 가
// mate solver 에서 같은 자리를 이미 밟았다.
func cacheOf(st *store.Store) Cache {
	if st == nil {
		return nil
	}
	return st
}

// whatifHandler 는 분기를 한 걸음 진행시킨다. 상태를 안 들고 있다 — 분기는 화면이
// 들고 매번 통째로 온다(whatifRequest).
type whatifHandler struct {
	store  *store.Store
	search Searcher
	// auth 는 여기도 남의 판을 막는다. 되짚기만 걸러 두면 이 표면이 남는데,
	// 여기는 기록에서 판을 다시 두는 자리라 그것만으로 남의 기보가 열린다(review.go).
	auth *authHandler
}

// whatifRequest 는 「ply 手目에서 이 수순을 뒀다면」이다.
//
// Moves 에는 상대의 응수도 함께 들어 있다. 사람의 수만 보내고 서버가 매번 다시
// 답하게 하면, 되돌아갈 때마다 상대가 다른 수를 둘 수 있다.
type whatifRequest struct {
	Ply   int      `json:"ply"`
	Moves []string `json:"moves"`
}

// whatifRoot 은 분기가 자라날 정본이다. 요청은 여기에 대해서만 뜻이 있고, 뿌리를
// 얻는 곳은 표면마다 다르다(journal §37).
type whatifRoot struct {
	// StartSFEN 은 0手目의 국면. 비어 있으면 평수 초기 국면이다.
	StartSFEN string
	// Moves 는 확정된 수다. 여기까지가 실제로 벌어진 일이고, 분기는 그 뒤에 붙는다.
	Moves []string
	Human shogi.Color
}

// rootOf 는 DB 기록에서 뿌리를 만든다.
//
// 구멍에서 끊는다(구멍이 없던 국면을 만드는 이유는 detailOf). 되짚기는 거기서 멈추고
// 뒤를 표기 없이 내보내면 되지만, 분기는 그 국면 위에서 새로 두는 일이라 아예 안 된다 —
// 끊어 두면 그 뒤의 手数가 replayTo 에서 「기록 밖」으로 거절된다.
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

// whatifMove 는 분기의 한 수다. reviewMove 와 같은 어휘(review.go).
type whatifMove struct {
	Ply int       `json:"ply"`
	USI string    `json:"usi"`
	Ja  string    `json:"ja"`
	By  game.Side `json:"by"`
	// SFEN 은 이 수를 둔 뒤의 국면이다. 화면은 그대로 그린다 — 여기서도 수를 두지 않는다.
	SFEN    string `json:"sfen"`
	Checked string `json:"checked,omitempty"`
}

// whatifCandidate 는 그 국면에서 수번 쪽이 둘 수 있는 좋은 수 하나다. 첫 번째가
// 최선수이자 화면의 초록 화살표다 — 대국 중에는 후보 목록을 안 켠다(journal §37 ②).
type whatifCandidate struct {
	USI string `json:"usi"`
	Ja  string `json:"ja"`
	// EvalCp 는 그 수를 둔 쪽 관점 cp다 — 이 값의 주인만 Turn 이고, 노드의 EvalCp 는
	// 패키지 doc 대로 플레이어 관점이다.
	EvalCp int `json:"evalCp"`
	// LossCp 는 최선수 대비 낙폭이다 — 「이 수를 고르면 얼마를 내주나」.
	//
	// 없는 자리가 둘이다: 최선수 자신(기준이라 0)과 詰み이 섞인 줄(candidatesOf).
	// 그래서 0을 안 내보낸다 — 화면이 「낙폭 0」과 「낙폭을 모른다」를 갈라야 한다.
	LossCp int `json:"lossCp,omitempty"`
	// MateIn 은 詰み까지의 手数다. 0이면 詰み이 아니다 — cp만 내보내면 30000이라는
	// 숫자가 화면에 그대로 나가고, 그건 평가치가 아니라 환산값이다.
	MateIn int `json:"mateIn,omitempty"`
}

// whatifNode 는 분기의 지금 서 있는 자리다. 넘겨 보는 것도 둬 보는 것도 전부 이 하나를
// 묻는 일이고, 응수를 서버가 대신 두지 않아 한 걸음에 탐색이 한 번이다(journal §37).
type whatifNode struct {
	// BasePly 는 분기가 갈라져 나온 手数다. 「分岐の前へ」가 돌아가는 자리다.
	BasePly int `json:"basePly"`
	// Ply 는 지금 국면의 手数(BasePly + 분기 길이)다.
	Ply  int    `json:"ply"`
	SFEN string `json:"sfen"`
	// Turn 은 지금 수번이다. 합법수와 후보가 이 쪽의 것이고, 화면은 이 값으로 어느
	// 駒台를 집을 수 있는지를 정한다.
	Turn     string `json:"turn"` // "b" | "w"
	YourTurn bool   `json:"yourTurn"`
	Checked  string `json:"checked,omitempty"`
	// Status 는 대국과 같은 어휘다. 千日手는 여기서 안 본다 — 아래 주석 참조.
	Status game.Status `json:"status"`
	// LegalMoves 는 화면이 규칙을 모르기 때문에 온다. 대국의 스냅샷과 같은 자리다.
	LegalMoves []string `json:"legalMoves"`
	// EvalCp 는 지금 국면의 플레이어 관점 cp다. 끝난 국면이면 없다.
	//
	// 줄의 마지막 수가 만든 값이 곧 이것이다. 화면은 앞선 노드를 이미 받아 뒀으므로
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

	rec, err := h.store.GameRecord(r.Context(), id, h.auth.owner(r))
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
		// 엔진 고장·시한 초과. 대국이 안 되는 것과 같은 종류라 503이다 — 다시 눌러
		// 볼 수 있는 실패이고, 판을 되짚는 쪽은 여전히 살아 있다.
		log.Printf("whatif: game %d ply %d: %v", id, req.Ply, err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "engine_unavailable", "message": whatifMessages["engine_unavailable"],
		})
	}
}

// whatifMessages 는 거절을 화면에 나갈 일본어로 옮긴다.
//
// 세 표면이 같은 표를 쓴다. HTTP(리뷰) · ws(대국 중) · HTTP(검토, explore.go)가 같은
// 실패를 다른 말로 말하면 같은 일이 세 화면에서 다른 일로 읽힌다.
var whatifMessages = map[string]string{
	"bad_ply":            "この手数からは試せません。",
	"bad_move":           "その手はここでは指せません。",
	"bad_line":           "この手順はこれ以上進められません。",
	"engine_unavailable": "エンジンが応答しませんでした。",
	"busy":               "まだ読んでいます。",
	// 대국 중에만 나온다. 되짚기에는 이 벽이 없다 — 끝난 판이라 무엇을 둬 봐도
	// 아무도 안 잃는다(ws.go 의 branchRoot).
	"locked": "対局中は、戻された手のあとだけ試せます。",
	// 검토에만 나온다. 저 둘은 뿌리가 자기 판이라 로그인이 자격을 정하지 않는데,
	// 검토는 뿌리가 手合割 표라 아무 국면이나 물을 수 있다 — 막는 것이 기록이 아니라
	// 엔진이고, 그 벽이 로그인이다(explore.go).
	"login_required": "検討モードはログインしてから使えます。",
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
