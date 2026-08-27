package queue

import (
	"testing"
	"time"
)

var now = time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

// waiter 는 밴드만 보는 대기자다. 불확실성을 0으로 두면 밴드가 base 그대로라
// 아래 표에서 두 손잡이(기다린 시간·격차)만 흔들 수 있다.
func waiter(id int64, r float64, waited time.Duration) Waiter {
	return Waiter{UserID: id, Rating: r, JoinedAt: now.Add(-waited)}
}

func TestBandWidens(t *testing.T) {
	for _, c := range []struct {
		waited time.Duration
		want   float64
	}{
		{0, Base0},
		{time.Second, Base0 + Expand},
		{10 * time.Second, Base0 + 10*Expand},
		// 상한에 닿은 뒤로는 안 넓어진다.
		{30 * time.Second, BaseMax},
		{10 * time.Minute, BaseMax},
	} {
		if got := Band(c.waited, 0, 0); got != c.want {
			t.Errorf("Band(%v) = %.0f, want %.0f", c.waited, got, c.want)
		}
	}
}

// 불확실성이 밴드에 그대로 더해진다. 두 사람 것이 다 더해지는 것이 규약이다 —
// 한쪽만 더하면 「모르는 사람」이 「아는 사람」의 좁은 밴드에 갇힌다.
func TestBandAddsBothDeviations(t *testing.T) {
	if got, want := Band(0, 350, 350), float64(Base0+700); got != want {
		t.Errorf("Band = %.0f, want %.0f", got, want)
	}
}

// 양쪽 밴드를 다 본다. 오래 기다린 사람이 방금 온 사람을 끌어당기지 않는다.
func TestPairableUsesTheNarrowerBand(t *testing.T) {
	// 격차 500. 기다린 쪽(60초)의 밴드는 상한 800이라 넉넉하지만 방금 온 쪽은 200이다.
	old := waiter(1, 1500, time.Minute)
	fresh := waiter(2, 2000, 0)
	if Pairable(old, fresh, now) {
		t.Error("방금 온 사람의 밴드를 넘어서 붙었다")
	}
	if Pairable(fresh, old, now) {
		t.Error("인자를 뒤집으면 답이 달라졌다 — 조건은 대칭이어야 한다")
	}

	// 둘 다 기다렸으면 붙는다.
	if !Pairable(old, waiter(2, 2000, time.Minute), now) {
		t.Error("둘 다 상한 밴드인데 안 붙었다")
	}
}

// 경계는 포함이다. 딱 맞는 격차를 거절하면 밴드 상수가 뜻하는 폭이 실제보다 좁다.
func TestPairableIncludesTheEdge(t *testing.T) {
	if !Pairable(waiter(1, 1500, 0), waiter(2, 1500+Base0, 0), now) {
		t.Error("격차가 밴드와 같은데 거절했다")
	}
	if Pairable(waiter(1, 1500, 0), waiter(2, 1500+Base0+1, 0), now) {
		t.Error("밴드를 1점 넘겼는데 붙었다")
	}
}

// 밴드 안에서 FIFO 다. 최근접이 아니다 — 가까운 짝을 가로채면 남은 둘이 최악으로 붙는다.
func TestPickIsFifoInsideTheBand(t *testing.T) {
	me := waiter(9, 1500, 0)
	// 셋 다 밴드 안이다(격차 100·50·10). 오래 기다린 순으로 온다.
	got, ok := Pick(me, []Waiter{
		waiter(1, 1600, 30*time.Second),
		waiter(2, 1550, 20*time.Second),
		waiter(3, 1510, 10*time.Second),
	}, now)
	if !ok {
		t.Fatal("짝을 못 골랐다")
	}
	if got.UserID != 1 {
		t.Errorf("고른 사람 %d, want 1 — 밴드 안에서는 오래 기다린 쪽이 먼저다", got.UserID)
	}
}

// 밴드 밖은 건너뛴다. 첫 후보가 안 맞으면 다음을 본다 — 앞에서 멈추면 후보 하나가
// 뒤의 모든 짝을 막는다.
func TestPickSkipsOutsideTheBand(t *testing.T) {
	me := waiter(9, 1500, 0)
	got, ok := Pick(me, []Waiter{
		waiter(1, 2500, 30*time.Second), // 격차 1000. 상한 밴드도 못 넘는다
		waiter(2, 1550, 10*time.Second),
	}, now)
	if !ok {
		t.Fatal("짝을 못 골랐다")
	}
	if got.UserID != 2 {
		t.Errorf("고른 사람 %d, want 2", got.UserID)
	}
}

// 자기 자신은 짝이 아니다. 질의가 이미 빼고 주지만, 여기서 한 번 더 보는 것은
// 혼자 두는 판이 조용히 서는 것을 막기 위해서다.
func TestPickNeverPicksItself(t *testing.T) {
	me := waiter(9, 1500, 0)
	if _, ok := Pick(me, []Waiter{me}, now); ok {
		t.Error("자기 자신과 붙었다")
	}
}

func TestPickEmpty(t *testing.T) {
	if _, ok := Pick(waiter(9, 1500, 0), nil, now); ok {
		t.Error("아무도 없는데 짝이 나왔다")
	}
}

// 선 시각이 앞서 있어도 밴드가 Base0 아래로 안 내려간다. 선 시각은 DB 의 시계이고
// 지금은 프로세스의 시계라 둘이 어긋날 수 있다.
func TestBandIgnoresNegativeWait(t *testing.T) {
	if got := Band(-time.Minute, 0, 0); got != Base0 {
		t.Errorf("Band(-1m) = %.0f, want %.0f", got, float64(Base0))
	}
}
