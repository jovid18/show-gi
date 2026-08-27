package store

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// 이 파일이 지키는 것은 질의의 원자성이다 — 고르는 규칙은 internal/queue, 화면까지의
// 흐름은 server/queue_test.go 가 본다.

// pairWith 는 그 사람이 후보로 올라오면 짝을 짓는 고르기다. 밴드를 안 본다 — 여기서
// 재는 것은 「누구를 고르나」가 아니라 「두 번 고를 수 있나」다.
//
// 그런데 id 를 받아야 한다. 큐는 표 하나에 사람마다 한 행이고 CI 는 패키지들을 같은 DB 에
// 동시에 거는데, 아무나 집으면 이 테스트가 그 순간 큐에 서 있던 남의 테스트 대기자를
// 낚아채 간다 — 그쪽은 「짝이 안 잡혀야 한다」를 재고 있고, 그래서 그쪽만 빨개진다.
func pairWith(roomID string, only int64) func(QueueWaiter, []QueueWaiter) (QueuePairing, bool) {
	return func(_ QueueWaiter, candidates []QueueWaiter) (QueuePairing, bool) {
		for _, c := range candidates {
			if c.UserID != only {
				continue
			}
			return QueuePairing{
				Opponent: c, RoomID: roomID, MyColor: "b", OppColor: "w",
			}, true
		}
		return QueuePairing{}, false
	}
}

// pairOptions 는 후보 창을 넓게 잡는다. 여기서 재는 것이 「누구를 고르나」가 아니라
// 질의의 원자성이라, 레이팅 폭은 제한하지 않는다.
func pairOptions(fresh time.Time) QueuePairOptions {
	return QueuePairOptions{FreshAfter: fresh, MaxGap: 1e9, Limit: 20}
}

// pairEventually 는 짝짓기를 몇 번 다시 걸어 본다.
//
// 한 번에 성공해야 한다고 재면 안 된다. 잠금이 SKIP LOCKED 라 남이 내 행을 잠근 동안
// 이 호출은 정당하게 ErrNoQueueSeat 를 주고(제품의 답도 「다음 재시도」다), CI 는
// 패키지들을 같은 DB 에 동시에 건다.
func pairEventually(
	t *testing.T, s *Store, userID int64, fresh time.Time,
	choose func(QueueWaiter, []QueueWaiter) (QueuePairing, bool),
) (QueuePairing, error) {
	t.Helper()
	var (
		pairing QueuePairing
		err     error
	)
	for range 50 {
		pairing, err = s.PairInQueue(t.Context(), userID, pairOptions(fresh), choose)
		if !errors.Is(err, ErrNoQueueSeat) {
			return pairing, err
		}
		time.Sleep(20 * time.Millisecond)
	}
	return pairing, err
}

// 큐에 서고 짝이 잡히고 자리를 찾아가기까지. 자리는 한 번만 나가야 한다.
func TestQueueSeatIsHandedOutOnce(t *testing.T) {
	s := open(t)
	a := owner(t, s, "a")
	b := owner(t, s, "b")
	fresh := time.Now().Add(-time.Minute)

	if _, err := s.JoinQueue(t.Context(), a, 1500, 200); err != nil {
		t.Fatalf("JoinQueue(a): %v", err)
	}
	// 아직 짝이 없다.
	if _, err := s.TakeQueueSeat(t.Context(), a); !errors.Is(err, ErrNoQueueSeat) {
		t.Fatalf("TakeQueueSeat: %v, want ErrNoQueueSeat", err)
	}
	if _, err := s.JoinQueue(t.Context(), b, 1500, 200); err != nil {
		t.Fatalf("JoinQueue(b): %v", err)
	}

	// b 가 짝을 짓는다. 자기 행은 그 자리에서 사라지고 a 의 행에 쪽지가 남는다.
	pairing, err := pairEventually(t, s, b, fresh, pairWith("ROOM0001", a))
	if err != nil {
		t.Fatalf("PairInQueue: %v", err)
	}
	if pairing.Opponent.UserID != a {
		t.Fatalf("짝이 %d, want %d", pairing.Opponent.UserID, a)
	}
	if pairing.Opponent.Name == "" {
		t.Error("짝의 이름이 비었다 — 방이 그 값을 든다")
	}
	if _, err := s.TakeQueueSeat(t.Context(), b); !errors.Is(err, ErrNoQueueSeat) {
		t.Errorf("짝을 지은 쪽에 자리가 남았다: %v", err)
	}

	seat, err := s.TakeQueueSeat(t.Context(), a)
	if err != nil {
		t.Fatalf("TakeQueueSeat(a): %v", err)
	}
	if seat.RoomID != "ROOM0001" || seat.Color != "w" {
		t.Fatalf("자리가 %+v, want {ROOM0001 w}", seat)
	}
	// 두 번은 안 나간다. 나가면 화면이 두 번 방으로 가고, 두 번째는 남의 자리를 노린다.
	if _, err := s.TakeQueueSeat(t.Context(), a); !errors.Is(err, ErrNoQueueSeat) {
		t.Errorf("같은 자리가 두 번 나갔다: %v", err)
	}
}

// 서로를 동시에 집으면 한쪽만 성공해야 한다. 둘 다 성공하면 방이 둘 생기고 두 사람이
// 각각 두 방에 앉는다 — 잠금이 전부 SKIP LOCKED 인 이유가 이 자리다(query/queue.sql).
//
// 「둘 다 실패」는 정상이다. 같은 DB 에서 도는 남의 짝짓기가 두 행 중 하나를 잠근 회차가
// 그렇고, 그때 제품의 답도 다음 재시도다 — 그래서 한 회차에 재는 것은 「둘은 아니다」이고,
// 「하나는 된다」는 회차를 다시 걸어 확인한다.
func TestMutualPairingSucceedsOnce(t *testing.T) {
	s := open(t)
	a := owner(t, s, "a")
	b := owner(t, s, "b")
	fresh := time.Now().Add(-time.Minute)

	for _, id := range []int64{a, b} {
		if _, err := s.JoinQueue(t.Context(), id, 1500, 200); err != nil {
			t.Fatalf("JoinQueue(%d): %v", id, err)
		}
	}

	won := 0
	for range 50 {
		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			ok      int
			rooms   []string
			errored []error
		)
		for i, id := range []int64{a, b} {
			wg.Add(1)
			go func(i int, id int64) {
				defer wg.Done()
				room := []string{"ROOMAAAA", "ROOMBBBB"}[i]
				// 짝은 상대 한 사람뿐이다. 서로를 집으러 들어가는 것이 이 테스트의 내용이다.
				other := []int64{b, a}[i]
				pairing, err := s.PairInQueue(t.Context(), id, pairOptions(fresh), pairWith(room, other))
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					ok++
					rooms = append(rooms, pairing.RoomID)
				case errors.Is(err, ErrNoQueueSeat):
					// 진 쪽이거나, 남이 잠근 회차다. 다음 회차에서 다시 건다
				default:
					errored = append(errored, err)
				}
			}(i, id)
		}
		wg.Wait()

		for _, err := range errored {
			t.Fatalf("PairInQueue: %v", err)
		}
		if ok > 1 {
			t.Fatalf("짝짓기가 %d 번 성공했다, want 1 이하 (방: %v)", ok, rooms)
		}
		won += ok
		if won == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if won != 1 {
		t.Fatalf("짝짓기가 %d 번 성공했다, want 1", won)
	}

	// 이긴 쪽의 행은 사라졌고 진 쪽에는 쪽지가 하나 남는다. 둘을 합쳐 자리가 정확히 하나다.
	seats := 0
	for _, id := range []int64{a, b} {
		if _, err := s.TakeQueueSeat(t.Context(), id); err == nil {
			seats++
		} else if !errors.Is(err, ErrNoQueueSeat) {
			t.Errorf("TakeQueueSeat(%d): %v", id, err)
		}
	}
	if seats != 1 {
		t.Errorf("자리가 %d 개, want 1", seats)
	}
}

// 다시 안 물어보는 사람은 큐에서 빠지고, 안 찾아간 자리도 걷힌다. 큐에 sweeper 가
// 없으므로(journal §92) 이 문장이 유일한 청소다.
func TestSweepDropsStaleRowsAndUnclaimedSeats(t *testing.T) {
	s := open(t)
	stale := owner(t, s, "stale")
	seated := owner(t, s, "seated")
	live := owner(t, s, "live")
	fresh := time.Now().Add(-time.Minute)

	for _, id := range []int64{stale, seated, live} {
		if _, err := s.JoinQueue(t.Context(), id, 1500, 200); err != nil {
			t.Fatalf("JoinQueue(%d): %v", id, err)
		}
	}
	// seated 에 쪽지를 남긴다. live 가 짝을 지으면 live 의 행이 사라지므로,
	// 짝짓기가 아니라 손으로 적어 「안 찾아간 자리」만 만든다.
	if _, err := s.pool.Exec(t.Context(),
		`UPDATE match_queue SET room_id = 'ROOMSEAT', color = 'b', matched_at = now() WHERE user_id = $1`,
		seated); err != nil {
		t.Fatalf("자리 적기: %v", err)
	}
	// 두 사람의 시각을 과거로 밀어 둔다. 시계를 잡을 자리가 없으므로(now() 가 DB 안이다)
	// 행을 직접 옮기는 것이 이 표를 늙게 하는 유일한 방법이다.
	if _, err := s.pool.Exec(t.Context(),
		`UPDATE match_queue SET seen_at = now() - interval '1 hour' WHERE user_id = $1`, stale); err != nil {
		t.Fatalf("seen_at 밀기: %v", err)
	}
	if _, err := s.pool.Exec(t.Context(),
		`UPDATE match_queue SET matched_at = now() - interval '1 hour' WHERE user_id = $1`, seated); err != nil {
		t.Fatalf("matched_at 밀기: %v", err)
	}

	if err := s.SweepQueue(t.Context(), time.Now().Add(-time.Minute), time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("SweepQueue: %v", err)
	}

	// 살아 있는 사람만 남는다.
	for _, c := range []struct {
		name string
		id   int64
		want bool
	}{{"stale", stale, false}, {"seated", seated, false}, {"live", live, true}} {
		var exists bool
		if err := s.pool.QueryRow(t.Context(),
			`SELECT exists(SELECT 1 FROM match_queue WHERE user_id = $1)`, c.id).Scan(&exists); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if exists != c.want {
			t.Errorf("%s: 행이 있나 = %v, want %v", c.name, exists, c.want)
		}
	}

	// 세는 쪽도 살아 있는 사람만 센다.
	n, err := s.QueueWaiting(t.Context(), fresh)
	if err != nil {
		t.Fatalf("QueueWaiting: %v", err)
	}
	if n < 1 {
		t.Errorf("큐에 %d명, want 1 이상", n)
	}
}

// 낡은 대기자는 후보가 아니다. 걷히기 전에도 그 사람은 이미 화면을 떠났다.
func TestStaleWaitersAreNotCandidates(t *testing.T) {
	s := open(t)
	gone := owner(t, s, "gone")
	here := owner(t, s, "here")

	for _, id := range []int64{gone, here} {
		if _, err := s.JoinQueue(t.Context(), id, 1500, 200); err != nil {
			t.Fatalf("JoinQueue(%d): %v", id, err)
		}
	}
	if _, err := s.pool.Exec(t.Context(),
		`UPDATE match_queue SET seen_at = now() - interval '1 hour' WHERE user_id = $1`, gone); err != nil {
		t.Fatalf("seen_at 밀기: %v", err)
	}

	// 떠난 사람만 짝으로 받는다. 그 사람이 후보에서 빠지는 것이 여기서 재는 것이고,
	// 아무나 받으면 같은 DB 에서 동시에 도는 남의 대기자와 붙어 버린다.
	_, err := s.PairInQueue(t.Context(), here, pairOptions(time.Now().Add(-time.Minute)),
		pairWith("ROOMGONE", gone))
	if !errors.Is(err, ErrNoQueueSeat) {
		t.Fatalf("PairInQueue: %v, want ErrNoQueueSeat — 떠난 사람과 짝이 됐다", err)
	}
}
