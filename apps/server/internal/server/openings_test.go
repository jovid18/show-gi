package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// TestOpeningsListNeedsNothing 은 DB도 엔진도 없이 목록이 나오는지다. 시작 화면이 이걸
// 먼저 부르므로, 여기가 503이면 아무것도 시작할 수 없다.
func TestOpeningsListNeedsNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/openings", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Openings []openingItem `json:"openings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Openings) == 0 {
		t.Fatal("목록이 비어 있다")
	}
	for _, o := range body.Openings {
		if o.ID == "" || o.Name == "" || !strings.HasPrefix(o.Source, "https://") {
			t.Errorf("빈 칸이 있다: %+v", o)
		}
	}
	// **수순이 새지 않는다**(openingItem 주석). 필드가 늘어나 초반이 통째로 나가는 것을 막는다.
	if strings.Contains(rec.Body.String(), "7g7f") {
		t.Error("응답에 수순이 들어 있다")
	}
}

func TestSetupFromQuery(t *testing.T) {
	cases := []struct {
		query   string
		def     shogi.Color
		human   shogi.Color
		opening string // 빈 값이면 북이 없어야 한다
	}{
		{"", shogi.Black, shogi.Black, ""},
		{"?color=w", shogi.Black, shogi.White, ""},
		{"?color=b", shogi.White, shogi.Black, ""},
		{"?opening=shikenbisha", shogi.Black, shogi.Black, "四間飛車"},
		{"?color=w&opening=yagura", shogi.Black, shogi.White, "矢倉"},
		// 못 읽는 값은 조용히 기본값이다(setupFrom).
		{"?color=x&opening=nope", shogi.White, shogi.White, ""},
	}
	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/ws/game"+tc.query, nil)
			got := setupFrom(r, Options{HumanColor: tc.def})

			if got.human != tc.human {
				t.Errorf("human = %v, want %v", got.human, tc.human)
			}
			if tc.opening == "" {
				if got.hasBook {
					t.Errorf("북이 붙었다: %s", got.opening.Name)
				}
				return
			}
			if !got.hasBook || got.opening.Name != tc.opening {
				t.Errorf("opening = %q(%v), want %q", got.opening.Name, got.hasBook, tc.opening)
			}
		})
	}
}
