package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/tsume"
)

func postHisshi(t *testing.T, h *tsumeHandler, sfen string) (*httptest.ResponseRecorder, hisshiResponse) {
	t.Helper()
	body, _ := json.Marshal(tsumeRequest{SFEN: sfen})
	req := httptest.NewRequest(http.MethodPost, "/api/tsume/hisshi", bytes.NewReader(body))
	value, err := h.auth.codec.Encode(7, "さとし", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: value})
	rec := httptest.NewRecorder()
	h.hisshi(rec, req)
	var res hisshiResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &res)
	return rec, res
}

func TestHisshiAnswersWithATree(t *testing.T) {
	h := newTsumeHandler(signedInHandler(), nil)
	rec, res := postHisshi(t, h, "5RSk1/6g1l/6s1p/9/9/9/9/9/9 b Sr2b3gs4n3l17p 1")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if res.Status != "hisshi" || res.Message != "1手必至です。" || res.Tree == nil || res.Tree.USI != "S*2c" {
		t.Fatalf("got %+v", res)
	}
	if len(res.Tree.Replies) == 0 || res.Tree.Replies[0].SFEN == "" {
		t.Fatal("the answer carries no replies to show")
	}
	if !slices.Equal(res.Answers, []string{"▲2三銀"}) {
		t.Errorf("answers %v", res.Answers)
	}
}

// 상한에 걸린 수가 있으면 必至인 수의 개수를 단정하지 않는다.
func TestHisshiDoesNotCountWhatItCouldNotCheck(t *testing.T) {
	first := &tsume.Move{USI: "S*2c", Ja: "▲2三銀"}
	for _, c := range []struct {
		answers    []string
		incomplete bool
		want       string
	}{
		{[]string{"▲2三銀"}, false, "1手必至です。"},
		{[]string{"▲2三銀", "▲2二銀"}, false, "1手必至です。必至になる手はほかに1通りあります。"},
		{[]string{"▲2三銀", "▲2二銀"}, true, "1手必至です。必至になる手はほかに少なくとも1通りあります。"},
		{[]string{"▲2三銀"}, true, "1手必至です。ほかの手には調べきれなかったものがあります。"},
	} {
		res := hisshiOut(tsume.HisshiResult{Status: tsume.HisshiFound, First: first, Answers: c.answers, Incomplete: c.incomplete})
		if res.Message != c.want {
			t.Errorf("%v incomplete %v: %q, want %q", c.answers, c.incomplete, res.Message, c.want)
		}
	}
}

// 詰め将棋와 같은 자리를 쓴다. 자리가 차 있으면 기다리다 503 이다.
func TestHisshiWaitsForTheTsumeSlot(t *testing.T) {
	h := newTsumeHandler(signedInHandler(), nil)
	h.slots <- struct{}{}
	defer func() { <-h.slots }()

	body, _ := json.Marshal(tsumeRequest{SFEN: "5RSk1/6g1l/6s1p/9/9/9/9/9/9 b Sr2b3gs4n3l17p 1"})
	req := httptest.NewRequest(http.MethodPost, "/api/tsume/hisshi", bytes.NewReader(body))
	value, _ := h.auth.codec.Encode(7, "さとし", time.Now())
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: value})
	ctx, cancel := context.WithTimeout(req.Context(), 50*time.Millisecond)
	defer cancel()
	rec := httptest.NewRecorder()
	h.hisshi(rec, req.WithContext(ctx))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d while the slot was taken, want 503: %s", rec.Code, rec.Body)
	}
}

func TestHisshiPointsMateToTheTsumeSolver(t *testing.T) {
	h := newTsumeHandler(signedInHandler(), nil)
	rec, res := postHisshi(t, h, "4k4/9/4P4/9/9/9/9/9/9 b G2r2b3g4s4n4l17p 1")
	if rec.Code != http.StatusOK || res.Status != "mate" || res.Tree != nil {
		t.Fatalf("status %d: %+v", rec.Code, res)
	}
}

func TestHisshiRefusesTheAttackingKing(t *testing.T) {
	h := newTsumeHandler(signedInHandler(), nil)
	rec, res := postHisshi(t, h, "4k4/9/9/9/9/9/9/9/4K4 b G2r2b3g4s4n4l18p 1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %+v", rec.Code, res)
	}
}
