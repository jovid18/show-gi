package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tsume"
)

// 詰め将棋를 푸는 표면(POST /api/tsume/solve). 사진에서 읽어 사람이 고친 국면이 온다
// (position.go 의 읽기·검사를 그대로 쓴다). 근거와 정한 것은 journal §147.
//
// 엔진을 쓰지 않는다. internal/tsume 이 이 프로세스 안에서 푼다. 한 번에 CPU 하나와 치환표
// 200MB 를 오래(상한까지 3분) 쥐므로 동시에 하나만 돈다(tsumeSlots).
//
// 로그인한 사람만이다. 익명끼리는 구별할 수단이 없어 한 사람이 자리를 계속 쥘 수 있다.

const (
	// tsumeSlots 는 동시에 푸는 수다. 태스크 메모리가 엔진까지 합쳐 1.5GB 라(infra 의
	// task_memory) 치환표 둘을 함께 둘 수 없다.
	tsumeSlots = 1

	// tsumeWait 는 자리를 기다리는 시간이다. 넘으면 「ほかの詰将棋を解いています」로 답한다.
	//
	// [미확정] 앞사람의 실전 25手詰め(로컬 66초)를 기다릴 만큼이다. 상한까지 쓰는 풀이(3분)는
	// 기다리지 못한다.
	tsumeWait = 90 * time.Second

	tsumeBodyMax = 4 << 10
)

type tsumeHandler struct {
	auth  *authHandler
	slots chan struct{}
	lim   tsume.Limits
}

func newTsumeHandler(ah *authHandler) *tsumeHandler {
	return &tsumeHandler{auth: ah, slots: make(chan struct{}, tsumeSlots), lim: tsume.DefaultLimits}
}

type tsumeRequest struct {
	SFEN string `json:"sfen"`
}

// tsumeResponse 는 푼 결과다. Message 는 화면에 그대로 나가는 일본어다.
type tsumeResponse struct {
	// Status 는 mate · nomate · unknown 이다.
	Status string `json:"status"`
	// Plies 는 詰みまでの手数다. 無駄合い는 세지 않는다. mate 일 때만 온다.
	Plies int `json:"plies,omitempty"`
	// Shortest 는 Plies 보다 짧은 詰み가 없음을 확인했는가다.
	Shortest bool `json:"shortest"`
	// Tree 는 공격 쪽 첫 수에서 자라는 트리다. mate 일 때만 온다.
	Tree    *tsumeMove `json:"tree,omitempty"`
	Message string     `json:"message"`
}

// tsumeMove 는 tsume.Move 를 그대로 옮긴다.
type tsumeMove struct {
	USI     string       `json:"usi"`
	Ja      string       `json:"ja"`
	SFEN    string       `json:"sfen"`
	Rest    int          `json:"rest"`
	Futile  bool         `json:"futile,omitempty"`
	Replies []*tsumeMove `json:"replies,omitempty"`
}

func wireMove(m *tsume.Move) *tsumeMove {
	out := &tsumeMove{USI: m.USI, Ja: m.Ja, SFEN: m.SFEN, Rest: m.Rest, Futile: m.Futile}
	for _, r := range m.Replies {
		out.Replies = append(out.Replies, wireMove(r))
	}
	return out
}

func (h *tsumeHandler) solve(w http.ResponseWriter, r *http.Request) {
	s, ok := h.auth.viewer(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "unauthorized", "message": "ログインが必要です。",
		})
		return
	}

	var req tsumeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, tsumeBodyMax)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_request", "message": "リクエストを読み取れませんでした。",
		})
		return
	}
	pos, err := shogi.ParseSFEN(req.SFEN)
	if err != nil || len(pos.TsumeFaults()) > 0 {
		// 확인 화면이 같은 검사(POST /api/position/check)를 통과한 판만 보낸다. 여기 오는 것은
		// 화면의 버그다.
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "bad_position", "message": "成り立たない局面は調べられません。",
		})
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
	res, err := tsume.Solve(r.Context(), pos, h.lim)
	if err != nil {
		if r.Context().Err() != nil {
			return // 사람이 떠났다. 받을 쪽이 없다
		}
		if errors.Is(err, tsume.ErrTooLarge) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error": "too_large", "message": "変化が多すぎて、手順の木を表示できません。",
			})
			return
		}
		if errors.Is(err, tsume.ErrNoKing) {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "no_king", "message": "相手の玉がない局面は調べられません。",
			})
			return
		}
		log.Printf("tsume: could not solve %s for %d: %v", req.SFEN, s.UserID, err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "詰みを調べられませんでした。",
		})
		return
	}
	log.Printf("tsume: %s for %d: status %d, %d plies, %d nodes in %s",
		req.SFEN, s.UserID, res.Status, res.Plies, res.Nodes, time.Since(start).Round(time.Millisecond))

	writeJSON(w, http.StatusOK, tsumeOut(res))
}

// tsumeOut 은 결과를 응답으로 옮기고 문장을 붙인다.
func tsumeOut(res tsume.Result) tsumeResponse {
	switch res.Status {
	case tsume.Mate:
		msg := fmt.Sprintf("%d手詰めです。", res.Plies)
		if !res.Shortest {
			msg = fmt.Sprintf("%d手で詰みます。これより短い詰みがないかは調べきれませんでした。", res.Plies)
		}
		return tsumeResponse{Status: "mate", Plies: res.Plies, Shortest: res.Shortest, Tree: wireMove(res.First), Message: msg}
	case tsume.NoMate:
		return tsumeResponse{Status: "nomate", Message: "王手を続けても詰みません。"}
	}
	return tsumeResponse{Status: "unknown", Message: "詰むかどうかを調べきれませんでした。"}
}
