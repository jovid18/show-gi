package server

import (
	"net/http"

	"github.com/jovid18/show-gi/apps/server/internal/book"
)

// openingItem 은 시작 화면이 그리는 한 줄이다.
//
// 수순을 안 보낸다. 상대가 다음에 무엇을 둘지가 클라이언트에 있으면 devtools 하나로
// 초반이 통째로 보이고, 그건 「최선수를 보여주지 않는다」(01-core.md §1)를 진형 쪽에서
// 뚫는 것이다. 화면에 필요한 것은 이름과 한 줄 설명뿐이다.
type openingItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Note string `json:"note"`
	// Source 는 출처 URL. 화면 아래에 그대로 붙는다 — 인용 규약은 journal §30.
	Source string `json:"source"`
}

func openings(w http.ResponseWriter, _ *http.Request) {
	all := book.All()
	out := make([]openingItem, 0, len(all))
	for _, o := range all {
		out = append(out, openingItem{ID: o.ID, Name: o.Name, Note: o.Note, Source: o.Source})
	}
	writeJSON(w, http.StatusOK, map[string]any{"openings": out})
}
