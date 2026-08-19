package server

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/handicap"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 끝난 판을 되짚는 표면. /ws/game 과 달리 요청/응답이다.
//
// 저장된 USI만으로는 판도 표기도 못 그리므로 여기서 한 판을 다시 둔다. 클라이언트가
// 두게 하면 룰 엔진이 두 벌이 된다 — 화면은 규칙을 모른다(01-core.md).

// 목록의 기본 크기와 상한이다. 리뷰는 「방금 둔 판」을 보러 오는 화면이라 깊은
// 페이지네이션이 필요 없다 — 상한만 막아두면 커서를 안 만들어도 된다.
const (
	listLimitDefault = 20
	listLimitMax     = 100
)

// reviewHandler 는 기록을 읽는다. 세션 goroutine과 아무 관계가 없다 — 이미 끝난 판이다.
//
// 읽는 것은 자기 판뿐이다(journal §33 · §46). 로그인 안 한 사람에게는 익명 판이
// 자기 판이다 — 익명끼리는 애초에 구별할 수단이 없어 지금까지와 같고, 갈리는 것은
// 로그인한 판이 그 사람에게만 보인다는 쪽이다(02-architecture.md §7 위협 2).
type reviewHandler struct {
	store *store.Store
	auth  *authHandler
	// analyzer 는 묻기만 한다 — 이 핸들러가 엔진과 무관하다는 성질은 그대로다.
	// nil 일 수 있다(엔진 없는 배포).
	analyzer *matchAnalyzer
	// level 은 총평(summary.go)에만 쓴다. 엔진이 아니다 — 이 핸들러가 엔진과 무관하다는
	// 성질은 그대로다.
	level intervene.Level
}

// owner 는 이 요청이 볼 수 있는 주인이다. 로그인 안 했으면 nil = 익명 판.
//
// 되짚기와 가정 수순이 같은 함수를 쓴다. 거르는 규칙이 두 벌이 되면 한쪽만
// 고쳐진 채로 남고, 그 한쪽이 곧 구멍이다.
func (h *authHandler) owner(r *http.Request) *int64 {
	s, ok := h.viewer(r)
	if !ok {
		return nil
	}
	return &s.UserID
}

// gameSummary 는 목록 한 줄이다.
type gameSummary struct {
	ID        int64     `json:"id"`
	MyColor   string    `json:"myColor"` // "b" | "w"
	StartedAt time.Time `json:"startedAt"`
	// FinishedAt·Result 는 아직 두는 중인 판이면 안 온다. 빈 값으로 보내면
	// 화면이 「1970년에 끝난 판」을 그린다.
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Result     string     `json:"result,omitempty"` // "win" | "loss" | "draw" | "abandoned"

	MoveCount         int `json:"moveCount"`
	InterventionCount int `json:"interventionCount"`
	// HandicapJa 는 그 판의 手合割 이름이다(일본어). 平手면 안 온다.
	//
	// 화면이 이름을 만들지 않는다 — 표는 서버에 있고(internal/handicap) 목록에 나가는
	// 것은 이름뿐이다. 이 줄이 없으면 駒落ち 판의 형세 그래프가 +1386(二枚落ち)에서 시작하는
	// 이유가 화면 어디에도 없어서, 되짚는 사람이 그것을 「엄청 잘 둔 판」으로 읽는다.
	//
	// 이름에 Ja 가 붙는 규약은 game.Snapshot.HandicapJa 에 있다.
	HandicapJa string `json:"handicapJa,omitempty"`
	// IsMatch 는 사람과 둔 판인가다(journal §83).
	//
	// 화면이 이 값으로 두 자리를 닫는다: 총평과 퀴즈. 대인전에는 엔진 판정이 없어서
	// 개입이 0건이고 평가치가 비는데, 그것을 「블런더 없이 잘 둔 판」으로 그리면 거짓말이
	// 된다 — 그 둘은 없는 것이지 0인 것이 아니다.
	IsMatch bool `json:"isMatch,omitempty"`
	// Analyzing 은 평가치를 지금 채우는 중인가다. 대인전에만 뜬다(matchAnalyzer).
	//
	// 화면이 이 값으로 「분석 중」과 「남지 않았다」를 가른다. 갈라 두지 않으면 판이
	// 끝나자마자 들어온 사람이 「평가치가 남지 않았습니다」를 보고 영영 없는 줄 안다.
	Analyzing bool `json:"analyzing,omitempty"`
}

// gameDetail 은 한 판 전체다.
type gameDetail struct {
	gameSummary
	// StartSFEN 은 0手目의 국면이다. 手数를 하나씩 되감으면 여기까지 온다.
	StartSFEN string `json:"startSfen"`
	// BaselineCp 는 이 판의 「형세 0」이다. reviewMove.EvalCp 와 같은 관점(플레이어)이고,
	// 平手면 안 온다(0).
	//
	// 목록에는 없고 여기만 있다. 쓰는 곳이 판 하나를 펼친 화면뿐이라서다 — 형세
	// 그래프(EvalGraph)와 후보 줄의 색(evalTone) 둘이다. 빼지 않으면 駒落ち 판의 곡선이
	// 천장에 붙어 어디서 흘렸는지가 안 보이고 「호각」 선이 핸디캡을 다 잃은 자리에
	// 그려지며, 후보 줄은 전부 최대 파랑이 된다. 판정이 같은 값을 빼는 것과 같은 이유이고
	// (intervene.Input.BaselineCp), 그래서 화면이 이 숫자를 다시 만들지 않는다.
	BaselineCp    int                  `json:"baselineCp,omitempty"`
	Moves         []reviewMove         `json:"moves"`
	Interventions []reviewIntervention `json:"interventions"`
	// Undos 는 사람이 스스로 무른 수들이다. 개입과 갈라서 준다 — 판이 되돌아간 것은
	// 같지만 시작한 쪽이 반대라, 한 배열로 주면 화면이 그 둘을 같은 줄로 그린다(§72).
	Undos []reviewUndo `json:"undos"`
}

// reviewMove 는 기보의 한 수다. game.Move 와 같은 어휘를 쓴다 — 같은 것을 두 이름으로
// 부르면 화면이 대국과 리뷰에서 다른 타입을 갖는다.
type reviewMove struct {
	Ply int       `json:"ply"`
	USI string    `json:"usi"`
	Ja  string    `json:"ja"` // 棋譜 표기(▲7六歩)
	By  game.Side `json:"by"`
	// SFEN 은 이 수를 둔 뒤의 국면이다. 화면은 이 값을 그대로 그린다.
	//
	// 비어 있을 수 있다. 기록이 중간에 끊겼거나(큐가 넘쳐 한 수가 빠졌다) 읽을 수 없는
	// 수가 들어 있으면 거기서부터 재현이 멈춘다. 그때 그 수를 목록에서 빼지는 않는다 —
	// 둔 것은 둔 것이고, 판을 못 그릴 뿐이다.
	SFEN string `json:"sfen,omitempty"`
	// EvalCp 는 플레이어 관점 cp다 — DB의 先手 관점을 여기서 뒤집는다(패키지 doc).
	//
	// nil이면 그 手数에 평가치가 안 붙었다. 0과 다르다 — 0은 호각이다.
	EvalCp *int `json:"evalCp,omitempty"`
	// Checked 는 이 수 뒤에 王手를 받고 있는 玉의 칸이다(5a). 아니면 빈 값 — checkedSquare 참조.
	Checked string `json:"checked,omitempty"`
}

// reviewIntervention 은 그 판에서 물러진 수 하나다.
//
// 여기에만 남는 것이 있다. 기보에는 확정된 수만 들어가므로, 개입이 막지 않았다면
// 실제로 뒀을 수는 이 줄에서만 보인다(01-core.md §5).
type reviewIntervention struct {
	// Ply 는 물러진 수의 手数다. 그 수는 기보에 없으므로, 화면이 그 국면을 보려면
	// Ply-1 手目의 판을 그려야 한다 — 물러진 수는 거기서 두어졌다.
	Ply      int    `json:"ply"`
	Kind     string `json:"kind"`     // "blunder" | "tesuji"
	Category string `json:"category"` // 기계용 코드. 화면에 안 나간다
	// CategoryJa·Message 는 서버가 만든 일본어다. 화면이 코드로 문장을 짓지 않는다.
	CategoryJa string `json:"categoryJa,omitempty"`
	Message    string `json:"message,omitempty"`

	DeltaWin     float64 `json:"deltaWin"`
	LevelBucket  string  `json:"levelBucket,omitempty"`
	RetractedUSI string  `json:"retractedUsi,omitempty"`
	// RetractedJa 는 물러진 수의 棋譜 표기다. 그 手数의 국면을 다시 만들어야 나오므로
	// 재현이 거기까지 못 갔으면 비어 있다.
	RetractedJa string `json:"retractedJa,omitempty"`

	// AfterCp 는 그 수를 두면 얼마가 되나다. moves[].evalCp 와 같은 자여야 되짚기
	// 화면이 물러진 수·실제로 둔 수·최선수를 한 줄에 세울 수 있다. 옛 기록에는 없다(§39).
	AfterCp *int `json:"afterCp,omitempty"`
	// BestCp 는 판정 당시 최선수의 cp. 낙폭과 겹치지 않는다 — 낙폭은 그때 K로 구한
	// 승률 차라 K가 바뀌면 낡고, 이 값은 원본이라 안 낡는다.
	BestCp *int `json:"bestCp,omitempty"`
}

// reviewUndo 는 사람이 스스로 무른 수 하나다.
//
// reviewIntervention 과 모양이 닮았지만 뜻이 반대다. 저쪽은 AI가 막은 것이고
// 이쪽은 사람이 되돌리고 싶었던 것이라, 되짚기에서 읽는 이야기가 정반대다 —
// 카테고리도 문구도 없는 것이 그래서다. 무르기에는 판정이 없다(§72).
type reviewUndo struct {
	// Ply 는 무른 수의 手数다. 그 수는 기보에 없으므로, 화면이 그 국면을 보려면
	// Ply-1 手目의 판을 그려야 한다 — 개입과 같은 규약이다.
	Ply int    `json:"ply"`
	USI string `json:"usi"`
	// Ja 는 무른 수의 棋譜 표기다. 그 手数의 국면을 다시 만들어야 나오므로 재현이
	// 거기까지 못 갔으면 비어 있다.
	Ja string `json:"ja,omitempty"`
	// EvalCp 는 플레이어 관점 cp다 — DB의 先手 관점을 여기서 뒤집는다(패키지 doc).
	// 무를 때 판정이 아직 그 手数를 안 채웠으면 nil이다.
	EvalCp *int `json:"evalCp,omitempty"`
}

// list 는 최근 대국 목록이다.
func (h *reviewHandler) list(w http.ResponseWriter, r *http.Request) {
	limit := listLimitDefault
	if raw := r.URL.Query().Get("limit"); raw != "" {
		// 32비트로 파싱한다. 이 값은 결국 LIMIT의 int32가 되는데, Atoi로 받으면
		// 64비트에서 int32 범위를 넘는 수가 통과해 변환에서 조용히 음수가 된다.
		// 여기서 자리수를 정해 두면 범위를 넘는 입력이 거절로 끝난다.
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || n <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "bad_limit", "message": "limit の値が正しくありません。",
			})
			return
		}
		limit = min(int(n), listLimitMax)
	}

	games, err := h.store.ListGames(r.Context(), limit, h.auth.owner(r))
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
	rec, ok := h.record(w, r)
	if !ok {
		return
	}
	out := detailOf(rec)
	// 여기서만 붙인다. 목록은 판을 여러 개 들고 오는데 그 하나하나에 물으면 목록이
	// 분석기의 잠금을 그만큼 잡는다 — 그래프가 있는 자리는 여기뿐이다.
	out.Analyzing = h.analyzer.analyzing(rec.ID)
	writeJSON(w, http.StatusOK, out)
}

// summary 는 그 판의 총평이다. 대국이 끝나는 자리에서 WS가 보내는 것과 같은 모양이고
// 같은 함수가 만든다(ws.go sendSummary · summarize) — 두 벌이면 되짚기와 대국이 같은
// 판을 두 문장으로 말한다.
//
// 기보와 따로 준다. 화면이 판을 먼저 그리고 총평을 뒤에 채우는 모양을 그대로 둔다 —
// WS가 스냅샷을 먼저 보내고 총평을 뒤에 보내는 것과 같은 순서다(§49).
func (h *reviewHandler) summary(w http.ResponseWriter, r *http.Request) {
	rec, ok := h.record(w, r)
	if !ok {
		return
	}
	// 대인전 판에는 총평이 없다. 총평이 세는 것은 개입이고(explain.GameFacts) 대인전은
	// 판정을 안 돌리므로, 그대로 만들면 「一度も止められませんでした」가 나간다 — 사실은
	// 재지 않았다이고, 그 둘이 초심자에게 정반대다.
	//
	// 빈 총평을 200으로 주지 않는다. 화면이 자리를 그리고 나서 지우게 되고, 그 한 틱
	// 동안 위 문장이 실제로 보인다.
	if rec.MatchID != "" {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "no_summary", "message": "対人戦には総評がありません。",
		})
		return
	}
	writeJSON(w, http.StatusOK, summarize(rec, h.level))
}

// record 는 {id} 가 가리키는 기록을 읽고, 실패면 그 자리에서 답하고 false 를 준다.
//
// detail 과 summary 가 같은 함수를 쓴다. 주인 거르기도 「끝난 판만」도 GameRecord 가
// 들고 있으므로(§46 · §51), 여기를 갈라 두면 한쪽만 고쳐진 채 남고 그 한쪽이 곧 구멍이다.
func (h *reviewHandler) record(w http.ResponseWriter, r *http.Request) (store.GameRecord, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_id", "message": "対局番号が正しくありません。",
		})
		return store.GameRecord{}, false
	}

	rec, err := h.store.GameRecord(r.Context(), id, h.auth.owner(r))
	if errors.Is(err, store.ErrNoGame) {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "not_found", "message": "その対局は見つかりません。",
		})
		return store.GameRecord{}, false
	}
	if err != nil {
		log.Printf("review: game %d: %v", id, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "対局の記録を読み込めませんでした。",
		})
		return store.GameRecord{}, false
	}
	return rec, true
}

func summaryOf(g store.GameSummary) gameSummary {
	out := gameSummary{
		ID:                g.ID,
		MyColor:           g.MyColor,
		StartedAt:         g.StartedAt,
		Result:            string(g.Result),
		MoveCount:         g.MoveCount,
		InterventionCount: g.InterventionCount,
		IsMatch:           g.MatchID != "",
		HandicapJa:        handicap.NameOf(g.StartSFEN),
	}
	if !g.FinishedAt.IsZero() {
		t := g.FinishedAt
		out.FinishedAt = &t
	}
	return out
}

// detailOf 는 기록을 화면이 쓸 수 있는 모양으로 만든다. 판을 처음부터 다시 둔다.
func detailOf(rec store.GameRecord) gameDetail {
	// 사람의 색을 여기 한 번만 구한다. 아래의 부호 뒤집기 전부와 기준점이 같은 값을
	// 써야 한다 — 두 벌로 두면 규약이 바뀌는 날 한쪽만 고쳐지고, 그때 그래프는 뺀 기준점과
	// 다른 관점의 곡선을 그린다(gameDetail.BaselineCp).
	humanColor := shogi.Black
	if rec.MyColor == "w" {
		humanColor = shogi.White
	}

	out := gameDetail{
		gameSummary:   summaryOf(rec.GameSummary),
		BaselineCp:    handicap.BaselineCpFor(rec.StartSFEN, humanColor),
		Moves:         make([]reviewMove, 0, len(rec.Moves)),
		Interventions: make([]reviewIntervention, 0, len(rec.Interventions)),
		Undos:         make([]reviewUndo, 0, len(rec.Undos)),
	}

	// posAt[i] 는 i手目까지 둔 뒤의 국면이고, toAt[i] 는 그 i手目의 도착 칸이다
	// (「同」 표기가 직전 수의 도착 칸을 본다).
	var posAt []shogi.Position
	var toAt []int

	// 사람이 1手目를 두는가. 手数의 홀짝만으로 정하지 않는다 — 中盤 국면에서 시작하는
	// 판이 있고(games.start_sfen), 그때는 1手目가 後手일 수 있다.
	humanFirst := humanColor == shogi.Black

	start, err := shogi.ParseSFEN(startSFENOf(rec.StartSFEN))
	if err != nil {
		// 시작 국면을 못 읽으면 아예 안 둔다. 평수 초기 국면으로 대신 두면 수들이
		// 거기서도 합법일 수 있고, 그러면 한 번도 없었던 국면을 그럴듯하게 그린다.
		// 기보는 그대로 내보낸다 — 판도 표기도 없지만 「무엇을 뒀는가」는 여전히 사실이다.
		log.Printf("review: game %d: start sfen %q: %v", rec.ID, rec.StartSFEN, err)
	} else {
		out.StartSFEN = start.SFEN()
		posAt = []shogi.Position{start}
		toAt = []int{-1}
		humanFirst = start.Turn == humanColor
	}

	for i, m := range rec.Moves {
		// 手数로 정한다, 배열의 자리가 아니라. 기보에 구멍이 나면 그 뒤가 한 칸씩
		// 밀리는데, 그때 자리로 세면 리뷰가 남의 실수를 내 것으로 보여준다.
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

		// 재현이 여기까지 이어졌고, 手数에 구멍이 없을 때만 이어 둔다. 구멍을 무시하고
		// 이어 두면 3手目가 2手目 자리에 서서 없던 국면을 그린다 — 여기서 멈추고
		// 그 뒤의 手数는 표기도 국면도 없이 나간다.
		if len(posAt) == i+1 && m.Ply == i+1 {
			if next, ja, ok := advance(posAt[i], toAt[i], m.USI); ok {
				view.Ja = ja
				view.SFEN = next.SFEN()
				view.Checked = checkedSquare(next)
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
		// 관점은 여기서 맞춘다(flipToPlayer).
		if iv.AfterCp != nil {
			view.AfterCp = flipToPlayer(*iv.AfterCp, humanColor)
		}
		if iv.BestCp != nil {
			view.BestCp = flipToPlayer(*iv.BestCp, humanColor)
		}
		// 물러진 수는 Ply-1 手目의 국면에서 두어졌다. 거기까지 재현했을 때만 표기가 나온다.
		if iv.RetractedUSI != "" && iv.Ply >= 1 && iv.Ply-1 < len(posAt) {
			if _, ja, ok := advance(posAt[iv.Ply-1], toAt[iv.Ply-1], iv.RetractedUSI); ok {
				view.RetractedJa = ja
			}
		}
		out.Interventions = append(out.Interventions, view)
	}

	for _, u := range rec.Undos {
		view := reviewUndo{Ply: u.Ply, USI: u.USI}
		// 기보와 같은 줄을 쓴다. 이 값은 game_moves.eval_cp 에서 그대로 옮겨온
		// 先手 관점이라(store.RecordUndo), 위 moves 루프와 같은 변환이라야 같은 수가
		// 두 목록에서 같은 숫자로 나온다 — 개입 쪽 flipToPlayer 는 관점의 출처가 다르다.
		if u.EvalCp != nil {
			cp := *u.EvalCp
			if humanColor == shogi.White {
				cp = -cp
			}
			view.EvalCp = &cp
		}
		// 무른 수는 Ply-1 手目의 국면에서 두어졌다 — 개입과 같은 자리, 같은 이유다.
		if u.Ply >= 1 && u.Ply-1 < len(posAt) {
			if _, ja, ok := advance(posAt[u.Ply-1], toAt[u.Ply-1], u.USI); ok {
				view.Ja = ja
			}
		}
		out.Undos = append(out.Undos, view)
	}

	return out
}

// flipToPlayer 는 수번 측 cp를 플레이어 관점으로 옮긴다(패키지 doc의 규약).
// 개입은 늘 사람이 둔 수라 그 국면의 수번이 사람이다 — 그래서 색만 보면 된다.
func flipToPlayer(cp int, human shogi.Color) *int {
	if human == shogi.White {
		cp = -cp
	}
	return &cp
}
