package server

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/book"
	"github.com/jovid18/show-gi/apps/server/internal/handicap"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 「이전에 남았던 게임이 있습니다. 이어하시겠습니까?」의 서버 쪽.
//
// **여기는 묻고 답하는 자리뿐이다.** 실제로 이어 두는 것은 `/ws/game?resume=<id>` 이고
// (ws.go), 그쪽이 점유·되열기·국면 되만들기를 전부 한다. 갈라 둔 이유는 이 물음이
// 대국을 열기 **전**에 나와야 하기 때문이다 — 붙는 순간 판이 하나 열린다(useGame).
//
// **되짚기 목록과 길이 갈린다.** 저쪽은 결과가 나온 판만 주고(§51) 이쪽은 그 반대 —
// 중단된 판 하나만 준다. 한 질의를 나눠 쓰면 두 목표가 서로를 지운다.

// resumeHandler 는 이어할 판을 묻고 「いいえ」를 받는다.
type resumeHandler struct {
	store *store.Store
	auth  *authHandler
}

// resumableGame 은 물음 카드가 그리는 것 전부다.
//
// **기보를 안 싣는다.** 화면이 그릴 것은 「몇 手目까지 두던 판인가」뿐이고, 수순을 실으면
// 이 표면이 되짚기의 우회로가 된다 — 그쪽은 끝난 판만 연다.
type resumableGame struct {
	ID        int64     `json:"id"`
	MyColor   string    `json:"myColor"` // "b" | "w"
	StartedAt time.Time `json:"startedAt"`
	MoveCount int       `json:"moveCount"`

	// Opening 은 그때 고른 상대의 진형 id다. 「おまかせ」였으면 안 온다.
	//
	// **이어할 때 클라이언트가 되보내지 않는다** — 서버가 그 행에서 읽는다(ws.go). 이건
	// 「이어하지 않기로 하면 다음 판의 기본값이 무엇인가」를 화면이 아는 데 쓴다.
	Opening string `json:"opening,omitempty"`
	// OpeningJa 는 그 진형의 일본어 이름이다. 화면이 id로 문장을 짓지 않는다.
	OpeningJa string `json:"openingJa,omitempty"`

	// Handicap·HandicapJa 는 그 판의 手合割이다. **平手면 둘 다 안 온다.**
	//
	// 위 진형과 **같은 짝이고 같은 이유다**: 이어할 때 되보내지는 않고(서버가 그 행에서
	// 읽는다) 「다음 판의 기본값」이 무엇인지를 화면이 아는 데 쓴다 — 六枚落ち를 이어 두던
	// 사람의 「もう一局」이 平手로 떨어지면 그 자리에서 판이 뒤집힌다.
	Handicap   string `json:"handicap,omitempty"`
	HandicapJa string `json:"handicapJa,omitempty"`
}

// find 는 이어할 수 있는 판을 준다. 없으면 `{"game":null}` 이다.
//
// **없는 것이 404가 아니다.** 화면이 늘 부르는 자리이고(첫 화면), 404면 「이어할 판이
// 없다」와 「서버가 고장났다」가 같은 그림이 된다 — `/api/me` 와 같은 판단이다(§46).
//
// **로그인 안 했으면 늘 null이다.** 익명 판은 서로 구별할 수단이 없어서
// (002_anonymous_games.sql) 「누구의 중단된 판인가」에 답할 수가 없다.
func (h *resumeHandler) find(w http.ResponseWriter, r *http.Request) {
	owner := h.auth.owner(r)
	if owner == nil {
		writeJSON(w, http.StatusOK, map[string]any{"game": nil})
		return
	}

	g, err := h.store.ResumableGame(r.Context(), *owner)
	if errors.Is(err, store.ErrNoGame) {
		writeJSON(w, http.StatusOK, map[string]any{"game": nil})
		return
	}
	if err != nil {
		log.Printf("resume: find: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "前の対局を確認できませんでした。",
		})
		return
	}

	out := resumableGame{
		ID:        g.ID,
		MyColor:   g.MyColor,
		StartedAt: g.StartedAt,
		MoveCount: g.MoveCount,
	}
	if h, ok := handicap.Of(g.StartSFEN); ok {
		out.Handicap, out.HandicapJa = h.ID, h.Name
	}
	if o, ok := book.Find(g.OpeningID); ok {
		out.Opening, out.OpeningJa = o.ID, o.Name
	}
	writeJSON(w, http.StatusOK, map[string]any{"game": out})
}

// decline 은 「いいえ」다. 그 판은 중단된 채로 끝나고 다시 물어보지 않는다.
//
// **되짚기에서도 안 보인다** — 결과가 나온 판만 나가므로(§51) `declined` 는 그 목록에
// 애초에 안 걸린다. 사람이 「이어하지 않겠다」고 답한 순간 그 판은 화면에서 사라진다.
func (h *resumeHandler) decline(w http.ResponseWriter, r *http.Request) {
	owner := h.auth.owner(r)
	if owner == nil {
		// 익명에게는 이어할 판이 애초에 없다. 없는 판을 거절하는 것도 404다.
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "not_found", "message": "その対局は見つかりません。",
		})
		return
	}

	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_id", "message": "対局番号が正しくありません。",
		})
		return
	}

	// 없는 판·남의 판·이미 답한 판이 **같은 404**다. 갈라 주면 남의 판 번호를 훑어볼 수
	// 있다(§46).
	err = h.store.DeclineResume(r.Context(), id, *owner)
	if errors.Is(err, store.ErrNoGame) {
		writeJSON(w, http.StatusNotFound, map[string]any{
			"error": "not_found", "message": "その対局は見つかりません。",
		})
		return
	}
	if err != nil {
		log.Printf("resume: decline %d: %v", id, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "前の対局を閉じられませんでした。",
		})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
