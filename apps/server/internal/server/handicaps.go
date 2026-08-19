package server

import (
	"net/http"

	"github.com/jovid18/show-gi/apps/server/internal/handicap"
)

// handicapItem 은 시작 화면이 그리는 한 줄이다.
//
// SFEN도 기준점도 안 보낸다. 화면이 판을 세우지 않는다 — 시작 국면은 세션이 스냅샷으로
// 보내고(game.Snapshot.SFEN), 기준점은 판정의 상수라 밖으로 나갈 이유가 없다. 여기 있는
// 것은 고르는 데 필요한 이름과 한 줄 설명뿐이다(openingItem 과 같은 판단).
//
// 平手가 목록에 없다. 「접지 않는다」는 서버에 물을 것이 아니라 기본값이고,
// 그 자리는 화면이 직접 그린다 — book 의 「おまかせ」와 같은 자리다(internal/handicap).
type handicapItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Note string `json:"note"`
}

func handicaps(w http.ResponseWriter, _ *http.Request) {
	all := handicap.All()
	out := make([]handicapItem, 0, len(all))
	for _, h := range all {
		out = append(out, handicapItem{ID: h.ID, Name: h.Name, Note: h.Note})
	}
	writeJSON(w, http.StatusOK, map[string]any{"handicaps": out})
}
