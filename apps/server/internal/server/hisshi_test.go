package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
