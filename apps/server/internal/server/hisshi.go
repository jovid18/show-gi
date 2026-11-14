package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/tsume"
)

// 1手必至를 푸는 표면(POST /api/tsume/hisshi). 詰め将棋 화면이 같은 국면을 보낸다.
//
// 詰め将棋와 같은 자리(tsumeSlots)를 나눠 쓴다. 치환표가 같은 크기라 둘을 함께 둘 수 없다.
// 캐시도 기록도 아직 없다.

// hisshiResponse 는 푼 결과다. Message 는 화면에 그대로 나가는 일본어다.
type hisshiResponse struct {
	// Status 는 hisshi · nohisshi · mate · unknown 이다.
	Status string `json:"status"`
	// Tree 는 必至 수에서 자라는 트리다. 응수마다 詰み 수순 한 줄이 이어진다. hisshi 일 때만 온다.
	Tree *tsumeMove `json:"tree,omitempty"`
	// Answers 는 必至인 수 전부의 棋譜 표기다.
	Answers []string `json:"answers,omitempty"`
	Message string   `json:"message"`
}

func (h *tsumeHandler) hisshi(w http.ResponseWriter, r *http.Request) {
	s, ok := h.auth.viewer(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "unauthorized", "message": "ログインが必要です。",
		})
		return
	}
	req, pos, ok := readTsumeRequest(w, r)
	if !ok {
		return
	}

	wait, cancel := context.WithTimeout(r.Context(), tsumeWait)
	defer cancel()
	select {
	case h.slots <- struct{}{}:
		defer func() { <-h.slots }()
	case <-wait.Done():
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error": "busy", "message": "ほかの詰将棋を解いています。しばらくしてからお試しください。",
		})
		return
	}

	start := time.Now()
	res, err := tsume.SolveHisshi(r.Context(), pos, h.hlim)
	if err != nil {
		if r.Context().Err() != nil {
			return // 사람이 떠났다
		}
		switch {
		case errors.Is(err, tsume.ErrTooLarge):
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error": "too_large", "message": "変化が多すぎて、手順の木を表示できません。",
			})
		case errors.Is(err, tsume.ErrNoKing):
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "no_king", "message": "相手の玉がない局面は調べられません。",
			})
		case errors.Is(err, tsume.ErrAttackerKing):
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "attacker_king", "message": "攻め方の玉がある局面の必至は調べられません。",
			})
		default:
			log.Printf("hisshi: could not solve %s for %d: %v", req.SFEN, s.UserID, err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": "internal", "message": "必至を調べられませんでした。",
			})
		}
		return
	}
	log.Printf("hisshi: %s for %d: status %d, %d answers, %d nodes in %s",
		req.SFEN, s.UserID, res.Status, len(res.Answers), res.Nodes, time.Since(start).Round(time.Millisecond))
	writeJSON(w, http.StatusOK, hisshiOut(res))
}

// hisshiOut 은 결과를 응답으로 옮긴다.
func hisshiOut(res tsume.HisshiResult) hisshiResponse {
	switch res.Status {
	case tsume.HisshiFound:
		// 상한에 걸린 수가 있으면 必至인 수가 더 있을 수 있으므로 개수를 단정하지 않는다.
		n := len(res.Answers) - 1
		msg := "1手必至です。"
		switch {
		case res.Incomplete && n > 0:
			msg = fmt.Sprintf("1手必至です。必至になる手はほかに少なくとも%d通りあります。", n)
		case res.Incomplete:
			msg = "1手必至です。ほかの手には調べきれなかったものがあります。"
		case n > 0:
			msg = fmt.Sprintf("1手必至です。必至になる手はほかに%d通りあります。", n)
		}
		return hisshiResponse{Status: "hisshi", Tree: wireMove(res.First), Answers: res.Answers, Message: msg}
	case tsume.HisshiNone:
		return hisshiResponse{Status: "nohisshi", Message: "どう指しても、詰みを逃れる応手があります。"}
	case tsume.HisshiMate:
		return hisshiResponse{Status: "mate", Message: "この局面は詰みます。「詰みを調べる」で手順を確かめてください。"}
	}
	return hisshiResponse{Status: "unknown", Message: "必至かどうかを調べきれませんでした。"}
}
