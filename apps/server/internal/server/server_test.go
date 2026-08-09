package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/game"
)

func TestHealthz(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	Handler(Options{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		OK     bool `json:"ok"`
		Engine bool `json:"engine"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.OK {
		t.Errorf("ok = false, want true")
	}
	// 엔진 없이 만든 Handler다. 200이되 engine 은 false여야 한다 —
	// 여기가 true면 "배포는 성공했는데 대국만 안 되는" 상태를 다시 못 잡는다.
	if body.Engine {
		t.Errorf("engine = true, want false (엔진 없이 만든 Handler)")
	}
}

// 엔진이 있으면 engine:true 여야 한다. 배포 워크플로가 이 값을 보고 막는다.
func TestHealthzReportsEngine(t *testing.T) {
	h := Handler(Options{NewOpponent: func() game.Opponent { return &scriptedOpponent{} }})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var body struct {
		Engine bool `json:"engine"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Engine {
		t.Error("engine = false, want true")
	}
}

// 라우팅 패턴에 메서드를 박아뒀으므로 다른 메서드는 405여야 한다.
// 이게 200이면 패턴에서 "GET "이 빠진 것이다.
func TestHealthzRejectsPost(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/healthz", nil)
	rec := httptest.NewRecorder()

	Handler(Options{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
