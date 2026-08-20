package rating

import (
	"math"
	"testing"
	"time"
)

// 이긴 쪽이 오르고 진 쪽이 내린다. 합이 보존되지는 않는다 — RD 가 다르면 움직이는
// 폭도 다르다.
func TestUpdateMovesBothWays(t *testing.T) {
	a, b := Update(Unrated, Unrated, Win)
	if a.Value <= Default {
		t.Errorf("the winner is at %.1f, want above %d", a.Value, Default)
	}
	if b.Value >= Default {
		t.Errorf("the loser is at %.1f, want below %d", b.Value, Default)
	}
}

// 무승부는 같은 실력끼리라면 아무도 안 움직인다. 기대값이 정확히 0.5라 갱신항이 0이다.
func TestDrawBetweenEqualsHoldsStill(t *testing.T) {
	a, b := Update(Unrated, Unrated, Draw)
	if math.Abs(a.Value-Default) > 1e-9 || math.Abs(b.Value-Default) > 1e-9 {
		t.Errorf("a=%.6f b=%.6f, want both at %d", a.Value, b.Value, Default)
	}
}

// 판을 두면 RD 가 줄어든다. 그것이 「알게 됐다」의 표현이다.
func TestPlayingNarrowsDeviation(t *testing.T) {
	a, _ := Update(Unrated, Unrated, Win)
	if a.Deviation >= Unrated.Deviation {
		t.Errorf("the deviation is %.1f, want below %.1f", a.Deviation, Unrated.Deviation)
	}
}

// 두 사람을 갱신 전 값으로 계산한다. 순서를 뒤집어도 같은 값이 나오는 것이 그 증거다 —
// 갱신된 값으로 상대를 보면 먼저 계산한 쪽이 이득을 본다.
func TestUpdateIsOrderIndependent(t *testing.T) {
	strong := Rating{Value: 1700, Deviation: 80}
	weak := Rating{Value: 1300, Deviation: 200}

	a1, b1 := Update(strong, weak, Win)
	b2, a2 := Update(weak, strong, Loss)

	if math.Abs(a1.Value-a2.Value) > 1e-9 || math.Abs(b1.Value-b2.Value) > 1e-9 {
		t.Errorf("swapping the arguments moved the result: %+v/%+v vs %+v/%+v", a1, b1, a2, b2)
	}
}

// 약한 사람을 이겨도 거의 안 오르고, 강한 사람을 이기면 많이 오른다.
func TestUpsetMovesMore(t *testing.T) {
	me := Rating{Value: 1500, Deviation: 100}
	weak := Rating{Value: 1100, Deviation: 100}
	strong := Rating{Value: 1900, Deviation: 100}

	overWeak, _ := Update(me, weak, Win)
	overStrong, _ := Update(me, strong, Win)

	if overStrong.Value-me.Value <= overWeak.Value-me.Value {
		t.Errorf("beating the stronger player gained %.2f, beating the weaker %.2f",
			overStrong.Value-me.Value, overWeak.Value-me.Value)
	}
}

// RD 가 하한 밑으로 안 내려간다. 굳으면 실제로 세진 뒤에도 밴드가 옛 자리에 남는다.
func TestDeviationHasAFloor(t *testing.T) {
	r := Rating{Value: 1500, Deviation: MinDeviation}
	for range 200 {
		r, _ = Update(r, Rating{Value: 1500, Deviation: MinDeviation}, Draw)
	}
	if r.Deviation < MinDeviation {
		t.Errorf("the deviation fell to %.4f, want at least %d", r.Deviation, MinDeviation)
	}
}

// 결과가 뻔한 판은 아무것도 안 바꾼다. 기대값이 포화해 0으로 나누는 자리라, 여기서
// NaN 이 새면 그 뒤의 모든 판이 NaN 이다.
func TestSaturatedGameIsIgnoredNotNaN(t *testing.T) {
	me := Rating{Value: 1500, Deviation: 100}
	hopeless := Rating{Value: -1e9, Deviation: MinDeviation}

	got, _ := Update(me, hopeless, Win)
	if math.IsNaN(got.Value) || math.IsNaN(got.Deviation) {
		t.Fatalf("the update produced NaN: %+v", got)
	}
	if got != me {
		t.Errorf("got %+v, want it left alone at %+v", got, me)
	}
}

// 안 두면 RD 가 되돌아간다. 두 달 쉬고 온 사람이 옛 밴드로 매칭되지 않게 하는 자리다.
func TestInflateWidensWithIdleTime(t *testing.T) {
	settled := Rating{Value: 1500, Deviation: MinDeviation}

	if got := Inflate(settled, 0); got != settled {
		t.Errorf("no idle time moved it: %+v", got)
	}

	week := Inflate(settled, 7*24*time.Hour)
	if week.Deviation <= settled.Deviation {
		t.Errorf("a week of idling left the deviation at %.1f", week.Deviation)
	}
	if week.Value != settled.Value {
		t.Errorf("idling moved the rating to %.1f — it must only move the deviation", week.Value)
	}

	full := Inflate(settled, InactivityToUnrated)
	if math.Abs(full.Deviation-MaxDeviation) > 1e-6 {
		t.Errorf("after the full window the deviation is %.4f, want %d", full.Deviation, MaxDeviation)
	}
}

// 오래 쉬어도 상한을 안 넘는다. 넘으면 밴드가 척도 전체보다 넓어져 뜻을 잃는다.
func TestInflateStopsAtMax(t *testing.T) {
	got := Inflate(Rating{Value: 1500, Deviation: MinDeviation}, 10*InactivityToUnrated)
	if got.Deviation != MaxDeviation {
		t.Errorf("the deviation is %.1f, want %d", got.Deviation, MaxDeviation)
	}
}

// 시드는 낙폭이 작을수록 높다. 뒤집히는 자리가 여기 하나이므로 부호를 못 박는다.
func TestSeedFromLossIsMonotonic(t *testing.T) {
	strong := SeedFromLoss(0)
	middle := SeedFromLoss(0.5)
	weak := SeedFromLoss(1)

	if !(strong.Value > middle.Value && middle.Value > weak.Value) {
		t.Errorf("the seed is not monotonic: %.1f / %.1f / %.1f", strong.Value, middle.Value, weak.Value)
	}
	if middle.Value != Default {
		t.Errorf("the prior loss seeded %.1f, want the scale centre %d", middle.Value, Default)
	}
	// 낙폭에서 나온 값을 믿을 근거가 없다. 그것을 RD 가 말한다.
	if strong.Deviation != MaxDeviation {
		t.Errorf("the seed deviation is %.1f, want %d", strong.Deviation, MaxDeviation)
	}
}

// 척도 밖의 낙폭이 와도 시드가 척도 안에 있다. 저장된 값이므로 범위를 못 믿는다.
func TestSeedClampsOutOfRangeLoss(t *testing.T) {
	if got := SeedFromLoss(-5); got.Value != SeedFromLoss(0).Value {
		t.Errorf("a negative loss seeded %.1f", got.Value)
	}
	if got := SeedFromLoss(9); got.Value != SeedFromLoss(1).Value {
		t.Errorf("a loss above one seeded %.1f", got.Value)
	}
}
