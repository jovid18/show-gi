package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/handicap"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// TestHandicapsListNeedsNothing 은 DB도 엔진도 없이 목록이 나오는지다 —
// /api/openings 와 같은 자리이고 같은 이유다(TestOpeningsListNeedsNothing).
func TestHandicapsListNeedsNothing(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/handicaps", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Handicaps []handicapItem `json:"handicaps"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Handicaps) != len(handicap.All()) {
		t.Fatalf("%d개가 나왔다. 표는 %d개다", len(body.Handicaps), len(handicap.All()))
	}
	for _, h := range body.Handicaps {
		if h.ID == "" || h.Name == "" || h.Note == "" {
			t.Errorf("빈 칸이 있다: %+v", h)
		}
	}
	// 판도 기준점도 새지 않는다(handicapItem 주석). 화면이 판을 세우는 길을 안 만든다.
	// 기준점은 표에서 뽑아 본다 — 숫자를 여기 적어 두면 값을 옮기는 날 이 확인이 조용히 죽는다.
	body2 := rec.Body.String()
	if strings.Contains(body2, "ppppppppp") {
		t.Error("응답에 SFEN이 들어 있다")
	}
	for _, h := range handicap.All() {
		if strings.Contains(body2, strconv.Itoa(h.BaselineCp)) {
			t.Errorf("응답에 %s 의 기준점(%d)이 들어 있다", h.ID, h.BaselineCp)
		}
	}
	// 平手는 목록에 없다. 「접지 않는다」는 서버에 물을 것이 아니라 기본값이다.
	// 이름만 본다 — 香落ち의 설명 문구는 平手를 말한다(「平手にいちばん近い」).
	for _, h := range body.Handicaps {
		if h.Name == "平手" || h.ID == "hirate" {
			t.Error("平手가 목록에 있다")
		}
	}
}

// TestHandicapSetupForcesShitate 는 手合割을 고른 판의 手番과 진형이 어떻게 되는지다.
//
// 셋이 한 자리에서 정해진다(newSetup): 시작 국면 · 사람은 下手 · 진형 없음. 하나라도
// 어긋나면 접어 준 쪽이 사람이 되거나, 상대가 없는 駒를 움직이려 든다.
func TestHandicapSetupForcesShitate(t *testing.T) {
	nimai, ok := handicap.Find("nimaiochi")
	if !ok {
		t.Fatal("nimaiochi 가 표에 없다")
	}

	// color=w 와 진형을 같이 보내도 手合割이 덮는다. 화면이 그것을 안 보내지만,
	// 쿼리는 사람이 손으로 고칠 수 있는 자리다.
	r := httptest.NewRequest(http.MethodGet, "/ws/game?color=w&opening=yagura&handicap=nimaiochi", nil)
	got := newSetup(r, Options{HumanColor: shogi.White})

	if got.startSFEN != nimai.SFEN {
		t.Errorf("startSFEN = %q, want %q", got.startSFEN, nimai.SFEN)
	}
	if got.human != shogi.Black {
		t.Errorf("human = %v, 駒落ち는 사람이 下手(Black)다", got.human)
	}
	if got.hasBook {
		t.Errorf("진형이 붙었다: %s", got.opening.Name)
	}

	// 모르는 id는 조용히 平手다 — 목록을 서버가 주므로(newSetup) 여기 오는 이상한 값은
	// 클라이언트가 틀린 경우이고, 그때 대국을 거절하는 것보다 평수로 두는 것이 낫다.
	r = httptest.NewRequest(http.MethodGet, "/ws/game?handicap=nope", nil)
	if got := newSetup(r, Options{}); got.startSFEN != "" {
		t.Errorf("모르는 手合에 국면이 붙었다: %q", got.startSFEN)
	}
}
