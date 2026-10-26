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
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/tsume"
)

// 詰め将棋를 푸는 표면(POST /api/tsume/solve). 사진에서 읽어 사람이 고친 국면이 온다
// (position.go 의 읽기·검사를 그대로 쓴다). 근거와 정한 것은 journal §147.
//
// 엔진을 쓰지 않는다. internal/tsume 이 이 프로세스 안에서 푼다. 한 번에 CPU 하나와 치환표
// 200MB 를 오래(상한까지 3분) 쥐므로 동시에 하나만 돈다(tsumeSlots).
//
// 로그인한 사람만이다. 익명끼리는 구별할 수단이 없어 한 사람이 자리를 계속 쥘 수 있다.
//
// 기록이 있으면 증명된 답을 국면마다 남기고(tsume_results), 같은 국면은 자리를 기다리지 않고
// 거기서 답한다. 푼 사람의 기록(tsume_solves)도 여기서 남긴다. 근거는 journal §155.

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
	auth *authHandler
	// store 는 nil 일 수 있다. 그때는 캐시도 기록도 없이 매번 푼다.
	store *store.Store
	slots chan struct{}
	lim   tsume.Limits
}

func newTsumeHandler(ah *authHandler, st *store.Store) *tsumeHandler {
	return &tsumeHandler{auth: ah, store: st, slots: make(chan struct{}, tsumeSlots), lim: tsume.DefaultLimits}
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

	key := shogi.PositionKey(pos)
	if out, ok := h.cached(r.Context(), key); ok {
		h.record(r.Context(), s.UserID, key, out)
		writeJSON(w, http.StatusOK, out)
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

	// 기다리는 동안 앞사람이 같은 국면을 풀었을 수 있다.
	if out, ok := h.cached(r.Context(), key); ok {
		h.record(r.Context(), s.UserID, key, out)
		writeJSON(w, http.StatusOK, out)
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

	out := tsumeOut(res)
	h.remember(r.Context(), key, res, out)
	h.record(r.Context(), s.UserID, key, out)
	writeJSON(w, http.StatusOK, out)
}

// cached 는 캐시된 답이다. 기록이 없거나 읽지 못하면 false 이고, 부르는 쪽은 그냥 푼다.
func (h *tsumeHandler) cached(ctx context.Context, key string) (tsumeResponse, bool) {
	if !h.cacheable() {
		return tsumeResponse{}, false
	}
	row, ok, err := h.store.TsumeResult(ctx, key, tsume.Version)
	if err != nil {
		log.Printf("tsume: read cache for %s: %v", key, err)
		return tsumeResponse{}, false
	}
	if !ok {
		return tsumeResponse{}, false
	}
	var tree *tsumeMove
	if row.Tree != nil {
		tree = new(tsumeMove)
		if err := json.Unmarshal(row.Tree, tree); err != nil {
			log.Printf("tsume: broken cached tree for %s: %v", key, err)
			return tsumeResponse{}, false
		}
	}
	return tsumeMessage(row.Status, row.Plies, row.Shortest, tree), true
}

// remember 는 증명된 답을 남긴다. unknown 은 남기지 않는다(025 마이그레이션). 실패해도
// 답은 이미 손에 있으므로 로그만 남긴다.
func (h *tsumeHandler) remember(ctx context.Context, key string, res tsume.Result, out tsumeResponse) {
	if !h.cacheable() || out.Status == "unknown" {
		return
	}
	row := store.TsumeResult{Status: out.Status, Plies: out.Plies, Shortest: out.Shortest, Nodes: res.Nodes}
	if out.Tree != nil {
		tree, err := json.Marshal(out.Tree)
		if err != nil {
			log.Printf("tsume: encode tree for %s: %v", key, err)
			return
		}
		row.Tree = tree
	}
	if err := h.store.SaveTsumeResult(ctx, key, tsume.Version, row); err != nil {
		log.Printf("tsume: save cache for %s: %v", key, err)
	}
}

// cacheable 은 캐시를 읽고 써도 되는가다. 캐시의 키는 tsume.Version 하나라, 다른 몫(테스트의
// 작은 상한)으로 낸 답을 그 판의 답으로 남기면 안 된다.
func (h *tsumeHandler) cacheable() bool {
	return h.store != nil && h.lim == tsume.DefaultLimits
}

// record 는 그 사람이 그 국면을 풀었다는 것을 남긴다. 실패해도 답은 돌려준다.
func (h *tsumeHandler) record(ctx context.Context, userID int64, key string, out tsumeResponse) {
	if h.store == nil {
		return
	}
	if err := h.store.RecordTsumeSolve(ctx, userID, key, out.Status, out.Plies); err != nil {
		log.Printf("tsume: record %s for %d: %v", key, userID, err)
	}
}

// tsumeOut 은 결과를 응답으로 옮긴다.
func tsumeOut(res tsume.Result) tsumeResponse {
	switch res.Status {
	case tsume.Mate:
		return tsumeMessage("mate", res.Plies, res.Shortest, wireMove(res.First))
	case tsume.NoMate:
		return tsumeMessage("nomate", 0, false, nil)
	}
	return tsumeMessage("unknown", 0, false, nil)
}

// tsumeMessage 는 답에 문장을 붙인다. 캐시에서 꺼낸 답도 여기를 지나 같은 문장을 얻는다.
func tsumeMessage(status string, plies int, shortest bool, tree *tsumeMove) tsumeResponse {
	switch status {
	case "mate":
		msg := fmt.Sprintf("%d手詰めです。", plies)
		if !shortest {
			msg = fmt.Sprintf("%d手で詰みます。これより短い詰みがないかは調べきれませんでした。", plies)
		}
		return tsumeResponse{Status: "mate", Plies: plies, Shortest: shortest, Tree: tree, Message: msg}
	case "nomate":
		return tsumeResponse{Status: "nomate", Message: "王手を続けても詰みません。"}
	}
	return tsumeResponse{Status: "unknown", Message: "詰むかどうかを調べきれませんでした。"}
}
