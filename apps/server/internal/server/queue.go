package server

import (
	"context"
	"errors"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/auth"

	"github.com/jovid18/show-gi/apps/server/internal/match"
	"github.com/jovid18/show-gi/apps/server/internal/metrics"
	"github.com/jovid18/show-gi/apps/server/internal/queue"
	"github.com/jovid18/show-gi/apps/server/internal/rating"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 대기열의 HTTP 표면. 근거는 journal §92 · §98.
//
// 줄에 서는 그 요청이 짝짓기까지 한다. 리더도 sweeper 도 알림 채널도 없다 — 기다리는
// 쪽이 스스로 재시도하고, 그 호출이 heartbeat 와 만료 청소를 겸한다.
//
// WebSocket 이 아닌 이유는 기다리는 동안 서버가 할 말이 없기 때문이다. 짝은 상대가
// 줄에 서는 순간 생기는데 그것을 아는 것은 그 사람의 요청이고, 알림을 붙이면 인스턴스
// 사이의 통로가 하나 필요해진다 — 큐를 표로 둔 이유가 그것을 안 만드는 것이었다.

type queueHandler struct {
	hub   *match.Hub
	store *store.Store
	auth  *authHandler
	// metrics 는 nil 일 수 있다. 그때는 짝짓기가 안 세어지고 큐는 그대로 돈다.
	metrics *metrics.Registry
}

// queuePayload 는 줄에 선 사람이 받는 답이다.
//
// 상대에 대해 아무것도 안 준다. 짝이 잡혀도 이름조차 여기 없다 — 방에 붙으면 그때
// 스냅샷이 준다(02-architecture.md §7 위협 2). 레이팅은 어느 쪽으로도 안 나간다.
type queuePayload struct {
	// Status 는 waiting·matched 둘이다.
	Status string `json:"status"`
	// RoomID·YourColor 는 matched 에만 온다. 화면이 그 방으로 옮겨 간다.
	RoomID    string `json:"roomId,omitempty"`
	YourColor string `json:"yourColor,omitempty"`
	// WaitedMs 는 줄에 선 뒤로 흐른 시간이다. 화면이 그것을 세지 않는 이유는 새로고침이다 —
	// 정본이 표에 있어야 탭을 다시 열어도 이어 센다(joined_at).
	WaitedMs int64 `json:"waitedMs"`
	// Waiting 은 지금 줄에 서 있는 사람 수다(자기 포함).
	//
	// 화면이 이걸 말해야 「안 잡히는 것」과 「고장」이 갈린다 — 동시 접속자가 없으면
	// 안 잡히는 것을 그대로 받아들이기로 정했고(journal §92), 그러면 사람에게 그 사실을
	// 알려 줄 자리가 하나 필요하다.
	Waiting int `json:"waiting"`
}

const (
	queueStatusWaiting = "waiting"
	queueStatusMatched = "matched"
)

// join 은 줄에 서고, 그 자리에서 짝짓기까지 해 본다. 다시 물어보는 것도 이 경로다.
//
// 멱등이다. 한 사람이 한 행이고(match_queue 의 PK) 두 번째 호출은 seen_at 만 옮긴다 —
// 화면의 재시도가 그대로 heartbeat 가 된다.
func (h *queueHandler) join(w http.ResponseWriter, r *http.Request) {
	s, ok := h.auth.viewer(r)
	if !ok {
		// 401이다. 새어 나갈 것이 없다 — 방과 달리 큐에는 「있다/없다」를 말할 대상이
		// 없고, 화면은 이 답을 보고 「로그인하고 다시」를 그려야 한다(match.go 의 create).
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "unauthorized", "message": "対局相手を探すにはログインが必要です。",
		})
		return
	}

	ctx := r.Context()
	now := time.Now()
	fresh := now.Add(-queue.StaleAfter)

	// 낡은 행을 먼저 걷는다. 실패해도 계속 간다 — 청소가 안 된 것이고, 짝짓기 쪽은
	// seen_at 을 스스로 보므로(LockQueueCandidates) 죽은 대기자와 짝이 되지는 않는다.
	if err := h.store.SweepQueue(ctx, fresh, now.Add(-queue.PickupTTL)); err != nil {
		log.Printf("queue: sweep: %v", err)
	}

	// 이미 잡힌 자리가 있으면 그것이 답이다. 짝짓기보다 먼저 본다 — 안 그러면 방으로
	// 갈 사람이 줄에 다시 서고, 그 사이 상대는 아무도 안 오는 방에서 기다린다.
	switch seat, err := h.store.TakeQueueSeat(ctx, s.UserID); {
	case err == nil:
		writeJSON(w, http.StatusOK, queuePayload{
			Status: queueStatusMatched, RoomID: seat.RoomID, YourColor: seat.Color,
		})
		return
	case !errors.Is(err, store.ErrNoQueueSeat):
		log.Printf("queue: take seat: %v", err)
		queueUnavailable(w)
		return
	}

	// 레이팅은 줄에 설 때 한 번 읽는다. 시드와 불확실성 복원이 여기서 얹힌다 —
	// 판이 끝나고 갱신하는 쪽과 같은 자리를 쓴다(match_rating.go 의 currentRating).
	mine, err := h.enqueue(ctx, s.UserID)
	if err != nil {
		log.Printf("queue: join: %v", err)
		queueUnavailable(w)
		return
	}

	if seat, ok := h.pair(ctx, s, fresh); ok {
		writeJSON(w, http.StatusOK, queuePayload{
			Status: queueStatusMatched, RoomID: seat.RoomID, YourColor: seat.Color,
		})
		return
	}

	waiting, err := h.store.QueueWaiting(ctx, fresh)
	if err != nil {
		// 세는 데 실패한 것뿐이다. 줄에는 서 있으므로 0으로 답한다 — 화면이 그때
		// 「탐색 중」만 그린다.
		log.Printf("queue: count: %v", err)
	}
	writeJSON(w, http.StatusOK, queuePayload{
		Status: queueStatusWaiting,
		// 음수를 안 내보낸다. 선 시각은 DB 의 시계라 앞서 있을 수 있고, 화면은 이 값을
		// 초로 잘라 그리므로 -1초가 그대로 나간다.
		WaitedMs: max(0, time.Since(mine.JoinedAt).Milliseconds()),
		Waiting:  waiting,
	})
}

// enqueue 는 줄에 선다. 이미 서 있으면 살아 있다고 알리고 처음 값을 받아 온다.
func (h *queueHandler) enqueue(ctx context.Context, userID int64) (store.QueueWaiter, error) {
	r := currentRating(ctx, h.store, userID)
	return h.store.JoinQueue(ctx, userID, r.Value, r.Deviation)
}

// pair 는 짝을 하나 지어 방을 세운다. 못 지으면 두 번째 값이 false 다.
//
// 표를 먼저 고치고 방을 나중에 세운다. 순서가 반대면 짝짓기가 어긋났을 때(내 행이
// 이미 남에게 잡혔다) 아무도 안 오는 방이 남는다 — 반대로 이 순서에서 그 사이에
// 프로세스가 죽으면 두 사람이 없는 방으로 가고, 그때 화면은 「열 수 없다」를 그린다.
func (h *queueHandler) pair(ctx context.Context, s auth.Session, fresh time.Time) (store.QueueSeat, bool) {
	// 색은 짝짓기 밖에서 뽑는다. 큐는 平手 확정 · 先手 랜덤이다(journal §92) — 手合은
	// 미리 만드는 방에만 두고, 레이팅 차를 手合으로 옮기는 계수가 없다.
	//
	// 방 id 도 여기서 뽑는다. 표에 적히는 값이라 방보다 먼저 있어야 한다.
	var (
		roomID  = match.NewRoomID()
		myColor = match.RandomColor()
		mine    store.QueueWaiter
	)

	pairing, err := h.store.PairInQueue(ctx, s.UserID, store.QueuePairOptions{
		FreshAfter: fresh,
		// 잠글 폭은 밴드의 상한이다. 불확실성의 상한을 아는 것은 rating 쪽이라 여기서 넘긴다.
		MaxGap: queue.MaxBand(rating.MaxDeviation),
		Limit:  queue.Candidates,
	},
		func(me store.QueueWaiter, candidates []store.QueueWaiter) (store.QueuePairing, bool) {
			// 고르는 시각을 여기서 잡는다. 밴드가 대기 시간으로 넓어지므로 잠금을
			// 잡는 데 걸린 시간까지 세는 것이 맞다.
			picked, ok := queue.Pick(waiterOf(me), waitersOf(candidates), time.Now())
			if !ok {
				return store.QueuePairing{}, false
			}
			opp, ok := findWaiter(candidates, picked.UserID)
			if !ok {
				return store.QueuePairing{}, false // 있을 수 없다. 방금 그 줄에서 골랐다
			}
			mine = me
			return store.QueuePairing{
				Opponent: opp,
				RoomID:   roomID,
				MyColor:  match.ColorCode(myColor),
				OppColor: match.ColorCode(myColor.Other()),
			}, true
		})
	if err != nil {
		if !errors.Is(err, store.ErrNoQueueSeat) {
			log.Printf("queue: pair: %v", err)
		}
		return store.QueueSeat{}, false
	}

	// 방을 세운다. 손님이 처음부터 정해져 있어서 제3자가 앉을 수 없고, 확인 화면도
	// 안 뜬다(match.Hub.CreatePaired).
	h.hub.CreatePaired(roomID,
		match.Player{UserID: s.UserID, Name: s.Name}, myColor,
		match.Player{UserID: pairing.Opponent.UserID, Name: pairing.Opponent.Name})

	// 두 사람의 대기 시간을 다 넣는다. 밴드가 양쪽을 보므로(queue.Pairable) 한쪽만
	// 재면 「얼마나 기다려서 잡혔나」가 절반만 남는다.
	h.metrics.ObservePairing(
		time.Since(mine.JoinedAt), time.Since(pairing.Opponent.JoinedAt),
		math.Abs(mine.Rating-pairing.Opponent.Rating))

	return store.QueueSeat{RoomID: roomID, Color: pairing.MyColor}, true
}

// queueUnavailable 은 표를 못 읽었다는 답이다. 로그인 실패와 갈라 둔다 — 이쪽은 다시
// 눌러 볼 만한 실패이고, 화면이 재시도를 멈추지 않아도 된다.
func queueUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"error": "store_unavailable", "message": "対局相手の待ち列を利用できません。",
	})
}

// leave 는 줄에서 빠진다. 없는 사람이 불러도 200이다 — 「이미 없다」와 「방금 지웠다」가
// 화면에 같은 뜻이고, 탭을 닫는 자리에서 부르는 경로라 실패로 답할 이유가 없다.
func (h *queueHandler) leave(w http.ResponseWriter, r *http.Request) {
	s, ok := h.auth.viewer(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "unauthorized", "message": "対局相手を探すにはログインが必要です。",
		})
		return
	}
	if err := h.store.LeaveQueue(r.Context(), s.UserID); err != nil {
		log.Printf("queue: leave: %v", err)
		queueUnavailable(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// waiterOf 는 표에서 온 대기자를 고르는 쪽의 어휘로 옮긴다. 이름을 안 넘긴다 —
// internal/queue 는 사람을 모르고, 그것이 밴드 상수를 DB 없이 흔들어 볼 수 있는 이유다.
func waiterOf(w store.QueueWaiter) queue.Waiter {
	return queue.Waiter{
		UserID: w.UserID, Rating: w.Rating, Deviation: w.Deviation, JoinedAt: w.JoinedAt,
	}
}

func waitersOf(ws []store.QueueWaiter) []queue.Waiter {
	out := make([]queue.Waiter, 0, len(ws))
	for _, w := range ws {
		out = append(out, waiterOf(w))
	}
	return out
}

// findWaiter 는 고른 사람을 표에서 온 줄에서 되찾는다. 이름이 그쪽에만 있다.
func findWaiter(ws []store.QueueWaiter, userID int64) (store.QueueWaiter, bool) {
	for _, w := range ws {
		if w.UserID == userID {
			return w, true
		}
	}
	return store.QueueWaiter{}, false
}
