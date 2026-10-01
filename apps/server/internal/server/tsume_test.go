package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func postTsume(t *testing.T, h *tsumeHandler, userID int64, sfen string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(tsumeRequest{SFEN: sfen})
	req := httptest.NewRequest(http.MethodPost, "/api/tsume/solve", bytes.NewReader(body))
	if userID != 0 {
		value, err := h.auth.codec.Encode(userID, "さとし", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: value})
	}
	rec := httptest.NewRecorder()
	h.solve(rec, req)
	return rec
}

func TestTsumeSolvesHeadGold(t *testing.T) {
	h := newTsumeHandler(signedInHandler())
	rec := postTsume(t, h, 7, "4k4/9/4P4/9/9/9/9/9/9 b G 1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var res tsumeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Status != "mate" || res.Plies != 1 || res.Tree == nil || res.Tree.USI != "G*5b" || res.Message != "1手詰めです。" {
		t.Fatalf("got %+v", res)
	}
	// 화면이 수를 누르면 그리는 판이다. 빠지면 트리를 눌러도 판이 그대로다.
	if res.Tree.SFEN == "" {
		t.Fatal("the move carries no position")
	}
}

func TestTsumeSaysWhenThereIsNoMate(t *testing.T) {
	h := newTsumeHandler(signedInHandler())
	rec := postTsume(t, h, 7, "4k4/9/9/9/9/9/9/9/9 b P 1")
	var res tsumeResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	if rec.Code != http.StatusOK || res.Status != "nomate" || res.Tree != nil {
		t.Fatalf("status %d: %+v", rec.Code, res)
	}
}

func TestTsumeNeedsASignIn(t *testing.T) {
	h := newTsumeHandler(signedInHandler())
	if rec := postTsume(t, h, 0, "4k4/9/4P4/9/9/9/9/9/9 b G 1"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
}

// 二歩인 판은 확인 화면이 막는다. 여기 오면 화면의 버그라 풀지 않고 거절한다.
func TestTsumeRefusesAnImpossiblePosition(t *testing.T) {
	h := newTsumeHandler(signedInHandler())
	rec := postTsume(t, h, 7, "4k4/9/4P4/4P4/9/9/9/9/9 b G 1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body)
	}
}

// 자리가 차 있으면 기다리다 「混み合っています」로 답한다. 둘이 함께 돌면 치환표 둘이 메모리에 선다.
func TestTsumeWaitsForTheSlot(t *testing.T) {
	h := newTsumeHandler(signedInHandler())
	h.slots <- struct{}{}
	defer func() { <-h.slots }()

	body, _ := json.Marshal(tsumeRequest{SFEN: "4k4/9/4P4/9/9/9/9/9/9 b G 1"})
	req := httptest.NewRequest(http.MethodPost, "/api/tsume/solve", bytes.NewReader(body))
	value, _ := h.auth.codec.Encode(7, "さとし", time.Now())
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: value})
	ctx, cancel := context.WithTimeout(req.Context(), 50*time.Millisecond)
	defer cancel()
	rec := httptest.NewRecorder()
	h.solve(rec, req.WithContext(ctx))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d while the slot was taken, want 503: %s", rec.Code, rec.Body)
	}
}

// 詰め将棋는 공격 쪽 玉을 놓지 않는다. 확인 화면이 그것을 사유로 막으면 책의 문제를 하나도 풀 수
// 없다. 수비 쪽 玉이 없는 것은 여전히 사유다.
func TestTsumeCheckAllowsAMissingAttackerKing(t *testing.T) {
	res := checkedAs("4k4/9/4P4/9/9/9/9/9/9 b G 1", true)
	if len(res.Faults) != 0 {
		t.Fatalf("faults %+v, want none", res.Faults)
	}
	for _, w := range res.Warnings {
		if strings.Contains(w, "玉") {
			t.Fatalf("warns about the missing attacker king: %s", w)
		}
	}
	if res := checkedAs("4k4/9/4P4/9/9/9/9/9/9 b G 1", false); len(res.Faults) == 0 {
		t.Fatal("the plain check accepts a position with one king")
	}
	if res := checkedAs("9/9/4P4/9/9/9/9/9/4K4 b G 1", true); len(res.Faults) == 0 {
		t.Fatal("the tsume check accepts a position without the defending king")
	}
}
