package server

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 끝난 판을 되짚는 표면. `/ws/game` 과 달리 여기는 **요청/응답**이다 — 대국은 서버가
// 먼저 말을 걸어야 하지만 리뷰는 사람이 넘길 때만 움직인다.
//
// **기보를 저장한 그대로 내보내지 않는다.** DB에 있는 것은 USI 문자열뿐이라, 그것만으로는
// 판을 그릴 수도(국면이 없다) 읽을 수도(표기가 없다) 없다. 그래서 여기서 한 판을 처음부터
// 다시 두며 手数마다 국면과 棋譜 표기를 붙인다 — **클라이언트가 두게 하지 않는 것**은
// 대국에서 정한 것과 같은 자리다(01-core.md: 화면은 규칙을 모른다). 룰 엔진을 두 벌
// 갖는 순간 어긋났을 때 어느 쪽이 맞는지 알 수 없다.

// listLimit 은 목록의 기본·최대 크기다.
//
// 리뷰는 「방금 둔 판」을 보러 오는 화면이라 깊은 페이지네이션이 필요 없다. 최대만
// 막아두면 커서를 안 만들어도 된다 — 필요해지면 그때 판다.
const (
	listLimitDefault = 20
	listLimitMax     = 100
)

// reviewHandler 는 기록을 읽는다. **세션 goroutine과 아무 관계가 없다** —
// 여기서 보는 것은 이미 끝난 판이고, 진행 중인 판의 상태는 여전히 세션이 소유한다.
//
// **지금은 누구의 판인지를 안 본다.** 로그인이 없어 대국이 전부 익명(`games.user_id` NULL)
// 이고, 그래서 가릴 주인이 아직 없다. **Google 로그인이 붙는 순간 이 자리가 위협이 된다** —
// `user_id` 가 채워지기 시작하면 남의 기보와 「그 사람이 어디서 막혔나」가 그대로 열린다
// (02-architecture.md §7 위협 2가 실력 프로파일에 대해 말하는 것과 같은 자리다).
// 그때 목록은 세션의 user_id 로 걸러야 하고, 상세는 소유자가 아니면 404여야 한다.
type reviewHandler struct {
	store *store.Store
}

// gameSummary 는 목록 한 줄이다.
type gameSummary struct {
	ID      int64  `json:"id"`
	MyColor string `json:"myColor"` // "b" | "w"
	// FinishedAt·Result 는 **아직 두는 중인 판이면 안 온다.** 빈 값으로 보내면
	// 화면이 「1970년에 끝난 판」을 그린다.
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Result     string     `json:"result,omitempty"` // "win" | "loss" | "draw" | "abandoned"

	MoveCount         int `json:"moveCount"`
	InterventionCount int `json:"interventionCount"`
}

// gameDetail 은 한 판 전체다.
type gameDetail struct {
	gameSummary
	// StartSFEN 은 **0手目의 국면**이다. 手数를 하나씩 되감으면 여기까지 온다.
	StartSFEN     string               `json:"startSfen"`
	Moves         []reviewMove         `json:"moves"`
	Interventions []reviewIntervention `json:"interventions"`
}

// reviewMove 는 기보의 한 수다. `game.Move` 와 같은 어휘를 쓴다 — 같은 것을 두 이름으로
// 부르면 화면이 대국과 리뷰에서 다른 타입을 갖는다.
type reviewMove struct {
	Ply int       `json:"ply"`
	USI string    `json:"usi"`
	Ja  string    `json:"ja"` // 棋譜 표기(▲7六歩)
	By  game.Side `json:"by"`
	// SFEN 은 **이 수를 둔 뒤**의 국면이다. 화면은 이 값을 그대로 그린다.
	//
	// **비어 있을 수 있다.** 기록이 중간에 끊겼거나(큐가 넘쳐 한 수가 빠졌다) 읽을 수 없는
	// 수가 들어 있으면 거기서부터 재현이 멈춘다. 그때 그 수를 목록에서 빼지는 않는다 —
	// 둔 것은 둔 것이고, 판을 못 그릴 뿐이다.
	SFEN string `json:"sfen,omitempty"`
	// EvalCp 는 **플레이어 관점** cp다. DB에는 先手 관점으로 들어 있고 여기서 뒤집는다
	// (store.SetMoveEval). 리뷰는 「내가 어디서 무너졌나」를 보는 화면이라 판마다 관점이
	// 바뀌면 두 판을 나란히 못 놓는다.
	//
	// nil이면 그 手数에 평가치가 안 붙었다. 0과 다르다 — 0은 호각이다.
	EvalCp *int `json:"evalCp,omitempty"`
	// Checked 는 이 수 뒤에 王手를 받고 있는 玉의 칸이다(`5a`). 王手가 아니면 비어 있다.
	//
	// **화면이 이걸 스스로 구하지 않는다.** 王手인지는 규칙을 알아야 알고, 규칙은 서버만
	// 갖기로 한 것이다 — 대국에서 `Snapshot.inCheck` 를 서버가 보내는 것과 같은 자리다.
	Checked string `json:"checked,omitempty"`
}

// reviewIntervention 은 그 판에서 물러진 수 하나다.
//
// **여기에만 남는 것이 있다.** 기보에는 확정된 수만 들어가므로, 개입이 막지 않았다면
// 실제로 뒀을 수는 이 줄에서만 보인다(01-core.md §5).
type reviewIntervention struct {
	// Ply 는 **물러진 수의 手数**다. 그 수는 기보에 없으므로, 화면이 그 국면을 보려면
	// `Ply-1` 手目의 판을 그려야 한다 — 물러진 수는 거기서 두어졌다.
	Ply      int    `json:"ply"`
	Kind     string `json:"kind"`     // "blunder" | "tesuji"
	Category string `json:"category"` // 기계용 코드. 화면에 안 나간다
	// CategoryJa·Message 는 서버가 만든 일본어다. **화면이 코드로 문장을 짓지 않는다.**
	CategoryJa string `json:"categoryJa,omitempty"`
	Message    string `json:"message,omitempty"`

	DeltaWin     float64 `json:"deltaWin"`
	LevelBucket  string  `json:"levelBucket,omitempty"`
	RetractedUSI string  `json:"retractedUsi,omitempty"`
	// RetractedJa 는 물러진 수의 棋譜 표기다. 그 手数의 국면을 다시 만들어야 나오므로
	// 재현이 거기까지 못 갔으면 비어 있다.
	RetractedJa string `json:"retractedJa,omitempty"`
}

// list 는 최근 대국 목록이다.
func (h *reviewHandler) list(w http.ResponseWriter, r *http.Request) {
	limit := listLimitDefault
	if raw := r.URL.Query().Get("limit"); raw != "" {
		// **32비트로 파싱한다.** 이 값은 결국 `LIMIT`의 int32가 되는데, `Atoi`로 받으면
		// 64비트에서 int32 범위를 넘는 수가 통과해 변환에서 조용히 음수가 된다.
		// 여기서 자리수를 정해 두면 범위를 넘는 입력이 **거절**로 끝난다.
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || n <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "bad_limit", "message": "limit の値が正しくありません。",
			})
			return
		}
		limit = min(int(n), listLimitMax)
	}

	games, err := h.store.ListGames(r.Context(), limit)
	if err != nil {
		log.Printf("review: list games: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "対局の記録を読み込めませんでした。",
		})
		return
	}

	out := make([]gameSummary, 0, len(games))
	for _, g := range games {
		out = append(out, summaryOf(g))
	}
	// 배열이 아니라 객체로 감싼다. 나중에 커서를 붙일 때 형태를 안 바꿔도 된다.
	writeJSON(w, http.StatusOK, map[string]any{"games": out})
}

// detail 은 한 판을 통째로 준다 — 手数마다의 국면까지.
func (h *reviewHandler) detail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_id", "message": "対局番号が正しくありません。",
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
		log.Printf("review: game %d: %v", id, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "対局の記録を読み込めませんでした。",
		})
		return
	}

	writeJSON(w, http.StatusOK, detailOf(rec))
}

func summaryOf(g store.GameSummary) gameSummary {
	out := gameSummary{
		ID:                g.ID,
		MyColor:           g.MyColor,
		StartedAt:         g.StartedAt,
		Result:            string(g.Result),
		MoveCount:         g.MoveCount,
		InterventionCount: g.InterventionCount,
	}
	if !g.FinishedAt.IsZero() {
		t := g.FinishedAt
		out.FinishedAt = &t
	}
	return out
}

// detailOf 는 기록을 화면이 쓸 수 있는 모양으로 만든다. **판을 처음부터 다시 둔다.**
func detailOf(rec store.GameRecord) gameDetail {
	out := gameDetail{
		gameSummary:   summaryOf(rec.GameSummary),
		Moves:         make([]reviewMove, 0, len(rec.Moves)),
		Interventions: make([]reviewIntervention, 0, len(rec.Interventions)),
	}

	humanColor := shogi.Black
	if rec.MyColor == "w" {
		humanColor = shogi.White
	}

	// posAt[i] 는 **i手目까지 둔 뒤**의 국면이고, toAt[i] 는 그 i手目의 도착 칸이다
	// (「同」 표기가 직전 수의 도착 칸을 본다). 재현이 끊기면 여기서 더 자라지 않고,
	// 그 뒤의 手数는 표기도 국면도 없이 나간다.
	var posAt []shogi.Position
	var toAt []int

	// 사람이 1手目를 두는가. **手数의 홀짝만으로 정하지 않는다** — 中盤 국면에서 시작하는
	// 판이 있고(games.start_sfen), 그때는 1手目가 後手일 수 있다.
	humanFirst := humanColor == shogi.Black

	start, err := shogi.ParseSFEN(startSFENOf(rec.StartSFEN))
	if err != nil {
		// **시작 국면을 못 읽으면 아예 안 둔다.** 평수 초기 국면으로 대신 두면 수들이
		// 거기서도 합법일 수 있고, 그러면 **한 번도 없었던 국면**을 그럴듯하게 그린다.
		// 기보는 그대로 내보낸다 — 판도 표기도 없지만 「무엇을 뒀는가」는 여전히 사실이다.
		log.Printf("review: game %d: start sfen %q: %v", rec.ID, rec.StartSFEN, err)
	} else {
		out.StartSFEN = start.SFEN()
		posAt = []shogi.Position{start}
		toAt = []int{-1}
		humanFirst = start.Turn == humanColor
	}

	for i, m := range rec.Moves {
		// **手数로 정한다, 배열의 자리가 아니라.** 기보에 구멍이 나면 그 뒤가 한 칸씩
		// 밀리는데, 그때 자리로 세면 리뷰가 **남의 실수를 내 것으로** 보여준다.
		view := reviewMove{Ply: m.Ply, USI: m.USI, By: game.SideEngine}
		if (m.Ply%2 == 1) == humanFirst {
			view.By = game.SideHuman
		}
		if m.EvalCp != nil {
			cp := *m.EvalCp
			if humanColor == shogi.White {
				cp = -cp
			}
			view.EvalCp = &cp
		}

		// 재현이 여기까지 이어졌고, 手数에 구멍이 없을 때만 이어 둔다. 구멍이 있으면
		// 그 뒤를 한 칸씩 밀어 두는 셈이 되어 **없던 국면을 그린다.**
		if len(posAt) == i+1 && m.Ply == i+1 {
			if next, ja, ok := advance(posAt[i], toAt[i], m.USI); ok {
				view.Ja = ja
				view.SFEN = next.SFEN()
				// 玉이 없는 국면(기록된 SFEN이 그럴 수 있다)에서 -1이 온다. 그때는 안 짚는다.
				if next.InCheck(next.Turn) {
					if sq := next.KingSquare(next.Turn); sq >= 0 {
						view.Checked = shogi.SquareUSI(sq)
					}
				}
				posAt = append(posAt, next)
				toAt = append(toAt, lastTo(m.USI))
			} else {
				log.Printf("review: game %d: cannot replay ply %d (%s)", rec.ID, m.Ply, m.USI)
			}
		}
		out.Moves = append(out.Moves, view)
	}

	for _, iv := range rec.Interventions {
		cat := intervene.Category(iv.Category)
		view := reviewIntervention{
			Ply:          iv.Ply,
			Kind:         iv.Kind,
			Category:     iv.Category,
			CategoryJa:   explain.CategoryJa(cat),
			Message:      explain.BaseMessage(cat),
			DeltaWin:     iv.DeltaWin,
			LevelBucket:  iv.LevelBucket,
			RetractedUSI: iv.RetractedUSI,
		}
		// 물러진 수는 `Ply-1` 手目의 국면에서 두어졌다. 거기까지 재현했을 때만 표기가 나온다.
		if iv.RetractedUSI != "" && iv.Ply >= 1 && iv.Ply-1 < len(posAt) {
			if _, ja, ok := advance(posAt[iv.Ply-1], toAt[iv.Ply-1], iv.RetractedUSI); ok {
				view.RetractedJa = ja
			}
		}
		out.Interventions = append(out.Interventions, view)
	}

	return out
}

// startSFENOf 는 기록된 시작 국면이다. **비어 있으면 평수 초기 국면**이다 —
// 세션은 기본 국면도 문자열로 적지만(session.go), 002 이전에 열린 판에는 칸이 비어 있다.
func startSFENOf(recorded string) string {
	if recorded == "" {
		return shogi.StartSFEN
	}
	return recorded
}

// advance 는 한 수를 두어 본다. 읽을 수 없거나 합법이 아니면 ok=false.
//
// **여기서 판정을 새로 하지 않는다.** 기록에 남은 수는 이미 그때 룰 엔진을 통과한
// 것이고, 이 검사는 기록이 깨졌는지를 보는 것이다.
func advance(pos shogi.Position, prevTo int, usi string) (shogi.Position, string, bool) {
	m, err := shogi.ParseUSIMove(usi)
	if err != nil {
		return pos, "", false
	}
	if err := pos.ValidateMove(m); err != nil {
		return pos, "", false
	}
	return pos.Apply(m), pos.MoveJa(m, prevTo), true
}

// lastTo 는 그 수의 도착 칸이다. 「同」 표기가 이 값을 본다.
func lastTo(usi string) int {
	m, err := shogi.ParseUSIMove(usi)
	if err != nil {
		return -1
	}
	return int(m.To)
}
