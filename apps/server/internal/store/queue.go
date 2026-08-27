package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/jovid18/show-gi/apps/server/internal/store/db"
)

// 대인전 대기열의 표 접근. 정책은 하나도 없다 — 밴드도 만료 시간도 부르는 쪽이 준다
// (internal/queue). 여기가 상수를 들면 그것을 흔들어 보는 데 DB가 필요해진다.

// QueueWaiter 는 대기열에 서 있는 한 사람이다. internal/queue.Waiter 와 같은 칸이고,
// 그 타입을 안 쓰는 이유는 store 가 그 패키지를 모르는 채로 있어야 하기 때문이다.
type QueueWaiter struct {
	UserID            int64
	Rating, Deviation float64
	JoinedAt          time.Time
	// Name 은 표시 이름이다. 후보로 올라온 사람에게만 채워진다 — 짝이 되면 그 자리에서
	// 방을 세우고(match.Hub.CreatePaired) 방이 그 값을 들기 때문이다. 고르는 데는 안 쓴다.
	Name string
}

// QueueSeat 은 짝이 잡힌 자리다. 방 id 와 그 방에서 잡을 쪽이다.
type QueueSeat struct {
	RoomID string
	// Color 는 b·w 다. games.my_color 와 같은 어휘다(match.ColorCode).
	Color string
}

// QueuePairing 은 짝짓기 하나의 결과다. 고르는 쪽이 채워서 돌려준다(PairInQueue).
type QueuePairing struct {
	// Opponent 는 고른 짝이다.
	Opponent QueueWaiter
	// RoomID 는 두 사람이 갈 방이다. 짝짓기보다 먼저 정해진다 — 표에 적을 값이라
	// 방을 세우기 전에 있어야 한다(server/queue.go).
	RoomID string
	// MyColor·OppColor 는 그 방에서 두 사람이 잡는 쪽이다. 표에 적히는 것은 짝의 것
	// 하나다 — 내 행은 이 자리에서 지워지고, 내 쪽은 부르는 쪽이 이미 손에 들고 있다.
	MyColor, OppColor string
}

// ErrNoQueueSeat 은 아직 짝이 안 잡혔다는 것 하나다.
var ErrNoQueueSeat = errors.New("store: no queue seat")

// SweepQueue 는 낡은 행을 걷는다. 다시 안 물어보는 대기자와 안 찾아간 자리 둘이다.
//
// 대기열에 서는 그 요청이 부른다 — 리더도 sweeper 도 두지 않는 것이 이 대기열의
// 설계다(journal §92).
func (s *Store) SweepQueue(ctx context.Context, staleBefore, pickupBefore time.Time) error {
	err := s.q.SweepQueue(ctx, db.SweepQueueParams{
		SeenAt:    stamp(staleBefore),
		MatchedAt: stamp(pickupBefore),
	})
	if err != nil {
		return fmt.Errorf("sweep queue: %w", err)
	}
	return nil
}

// JoinQueue 는 대기열에 서고, 이미 서 있으면 살아 있다고 알린다. 한 사람이 한 행이라 멱등이다.
//
// 레이팅은 처음 설 때 한 번만 적힌다(query/queue.sql). 돌려주는 값은 표에 있는 것이라,
// 두 번째 호출은 넘긴 값이 아니라 처음 값을 받는다 — 밴드가 그 위에서 돈다.
func (s *Store) JoinQueue(ctx context.Context, userID int64, rating, deviation float64) (QueueWaiter, error) {
	row, err := s.q.JoinQueue(ctx, db.JoinQueueParams{
		UserID: userID, Rating: rating, Deviation: deviation,
	})
	if err != nil {
		return QueueWaiter{}, fmt.Errorf("join queue %d: %w", userID, err)
	}
	return QueueWaiter{
		UserID:    row.UserID,
		Rating:    row.Rating,
		Deviation: row.Deviation,
		JoinedAt:  row.JoinedAt.Time,
	}, nil
}

// TakeQueueSeat 은 잡힌 자리를 가져오고 그 행을 지운다. 아직이면 ErrNoQueueSeat.
//
// 한 번만 답한다. 읽는 것과 지우는 것이 한 문장이라(query/queue.sql) 같은 자리가 두 번
// 나가지 않는다 — 나가면 화면이 두 번 방으로 가고, 두 번째는 남의 자리를 노린다.
func (s *Store) TakeQueueSeat(ctx context.Context, userID int64) (QueueSeat, error) {
	row, err := s.q.TakeQueueSeat(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return QueueSeat{}, ErrNoQueueSeat
	}
	if err != nil {
		return QueueSeat{}, fmt.Errorf("take queue seat %d: %w", userID, err)
	}
	// 방 id 가 있는 행만 지우는 질의라(WHERE room_id IS NOT NULL) 여기 온 행은 둘 다 있다.
	// 그래도 보는 것은 색이 CHECK 뿐이고 NOT NULL 이 아니어서다 — 나중에 적는 자리가
	// 하나 더 생기는 날 빈 색이 그대로 화면까지 간다.
	if row.RoomID == nil || row.Color == nil {
		return QueueSeat{}, fmt.Errorf("take queue seat %d: half-written seat", userID)
	}
	return QueueSeat{RoomID: *row.RoomID, Color: *row.Color}, nil
}

// LeaveQueue 는 대기열에서 빠진다. 없는 사람을 지워도 에러가 아니다 — 「이미 없다」와
// 「방금 지웠다」가 부르는 쪽에 같은 뜻이다.
func (s *Store) LeaveQueue(ctx context.Context, userID int64) error {
	if err := s.q.LeaveQueue(ctx, userID); err != nil {
		return fmt.Errorf("leave queue %d: %w", userID, err)
	}
	return nil
}

// QueueWaiting 은 지금 대기열에 서 있는 사람 수다. 화면에 안 나간다 — 확인용이다.
func (s *Store) QueueWaiting(ctx context.Context, freshAfter time.Time) (int, error) {
	n, err := s.q.CountQueueWaiting(ctx, stamp(freshAfter))
	if err != nil {
		return 0, fmt.Errorf("count queue: %w", err)
	}
	return int(n), nil
}

// QueuePairOptions 는 후보를 고르는 창이다. 정책은 부르는 쪽이 정한다 — 여기 값을 두면
// 그것을 흔들어 보는 데 DB 가 필요해진다(internal/queue).
type QueuePairOptions struct {
	// FreshAfter 는 이 시각 뒤로 다시 물어본 사람만 후보라는 뜻이다.
	FreshAfter time.Time
	// MaxGap 은 후보로 잠글 레이팅 폭이다. 밴드가 아니라 그 상한이다(queue.MaxBand) —
	// 잠기는 행을 줄이는 것이 목적이고, 어떤 밴드보다 넓어야 한다.
	MaxGap float64
	// Limit 은 한 번에 잠글 후보 수다.
	Limit int
}

// PairInQueue 는 짝을 하나 짓는다. 못 지으면 ErrNoQueueSeat.
//
// 트랜잭션 하나 안에서 세 가지를 한다: 내 행과 후보들을 잠그고(FOR UPDATE SKIP LOCKED),
// choose 가 고르고, 그 결과를 짝의 행에 적고 내 행을 지운다.
//
// 잠금이 전부 SKIP LOCKED 라 아무도 기다리지 않는다. 그래서 A가 B를, B가 A를 동시에
// 집어도 교착이 없고 — 한쪽만 성공한다. 다른 쪽은 자기 행을 못 잠가서 이번 회차를
// 포기하고, 다음 재시도에서 방 쪽지를 읽는다.
//
// choose 는 판단만 한다. DB를 만지지 않고 즉시 돌아와야 한다 — 트랜잭션이 열려 있고
// 그 안에 남의 행이 잠겨 있다.
func (s *Store) PairInQueue(
	ctx context.Context,
	userID int64,
	opts QueuePairOptions,
	choose func(me QueueWaiter, candidates []QueueWaiter) (QueuePairing, bool),
) (QueuePairing, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return QueuePairing{}, fmt.Errorf("pair in queue: begin: %w", err)
	}
	// 성공 경로에서는 Commit 이 먼저 끝나 있고, 그때 이 Rollback 은 아무것도 안 한다.
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	me, err := q.LockQueueWaiter(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		// 내 행이 없거나 남이 잠그고 있다. 둘 다 「이번에는 못 짓는다」로 같다 —
		// 갈라 봐도 부르는 쪽이 할 일이 하나다(다음 재시도).
		return QueuePairing{}, ErrNoQueueSeat
	}
	if err != nil {
		return QueuePairing{}, fmt.Errorf("lock queue waiter %d: %w", userID, err)
	}

	// 잠그는 폭이 내 레이팅 주변이다. 전부 잠그면 붙을 수 없는 사람까지 잠기고, 그동안
	// 그 행을 노리던 다른 짝짓기가 헛돈다 — 아무도 기다리지 않는 대신(SKIP LOCKED)
	// 그 회차를 포기하기 때문이다.
	rows, err := q.LockQueueCandidates(ctx, db.LockQueueCandidatesParams{
		UserID:   userID,
		SeenAt:   stamp(opts.FreshAfter),
		Rating:   me.Rating - opts.MaxGap,
		Rating_2: me.Rating + opts.MaxGap,
		Limit:    int32(opts.Limit),
	})
	if err != nil {
		return QueuePairing{}, fmt.Errorf("lock queue candidates: %w", err)
	}
	if len(rows) == 0 {
		return QueuePairing{}, ErrNoQueueSeat
	}

	candidates := make([]QueueWaiter, 0, len(rows))
	for _, r := range rows {
		candidates = append(candidates, QueueWaiter{
			UserID: r.UserID, Rating: r.Rating, Deviation: r.Deviation,
			JoinedAt: r.JoinedAt.Time, Name: r.DisplayName,
		})
	}

	pairing, ok := choose(QueueWaiter{
		UserID: me.UserID, Rating: me.Rating, Deviation: me.Deviation, JoinedAt: me.JoinedAt.Time,
	}, candidates)
	if !ok {
		return QueuePairing{}, ErrNoQueueSeat
	}

	n, err := q.SeatQueueWaiter(ctx, db.SeatQueueWaiterParams{
		UserID: pairing.Opponent.UserID,
		RoomID: &pairing.RoomID,
		Color:  &pairing.OppColor,
	})
	if err != nil {
		return QueuePairing{}, fmt.Errorf("seat queue waiter %d: %w", pairing.Opponent.UserID, err)
	}
	if n == 0 {
		// 잠가 둔 행이라 여기 올 수 없다. 오면 우리 버그이고, 그대로 커밋하면 내 행만
		// 사라져서 두 사람 다 아무 방에도 못 간다.
		return QueuePairing{}, fmt.Errorf("seat queue waiter %d: already taken", pairing.Opponent.UserID)
	}
	if err := q.LeaveQueue(ctx, userID); err != nil {
		return QueuePairing{}, fmt.Errorf("leave queue %d: %w", userID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return QueuePairing{}, fmt.Errorf("pair in queue: commit: %w", err)
	}
	return pairing, nil
}

// stamp 는 시각을 질의가 받는 모양으로 옮긴다.
func stamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
