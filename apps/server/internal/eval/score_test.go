package eval

import "testing"

// 이 버그의 자리다. 엔진의 생 cp 가 詰み을 눌러 담던 값(30000 − 10×手数)을 넘어오므로
// (±35281 = 「이기는데 手数를 모름」) 숫자 하나로 줄을 세우면 1手詰み이 그 뒤로 밀린다.
func TestAWinningMateOutranksTheEnginesRawCeiling(t *testing.T) {
	const rawCeiling = 35281
	if got := Compare(Mate(1), Cp(rawCeiling)); got <= 0 {
		t.Errorf("Compare(mate 1, cp %d) = %d, 詰み이 위여야 한다", rawCeiling, got)
	}
	// 늦게 이기는 詰み도 마찬가지다. 手数가 아무리 커도 cp 뒤로 안 간다 — 그것이
	// 상수를 키우는 것과 태그로 가르는 것의 차이다(journal §131).
	if got := Compare(Mate(999), Cp(rawCeiling)); got <= 0 {
		t.Errorf("Compare(mate 999, cp %d) = %d, 詰み이 위여야 한다", rawCeiling, got)
	}
}

func TestCompareOrdersMatesAroundEveryCp(t *testing.T) {
	// 위에서 아래로 좋은 순. 이기는 詰み > 어떤 cp > 지는 詰み이고, 짧게 이기는 쪽이
	// 위, 늦게 지는 쪽이 위다 — 화면 쪽 rankOf 와 같은 규칙이다.
	order := []Score{
		Mate(1), Mate(3), Mate(99),
		Cp(35281), Cp(900), Cp(0), Cp(-900), Cp(-35281),
		Mate(-99), Mate(-3), Mate(-1), Mate(0),
	}
	for i := 0; i+1 < len(order); i++ {
		if got := Compare(order[i], order[i+1]); got <= 0 {
			t.Errorf("Compare(%+v, %+v) = %d, 앞이 더 좋아야 한다", order[i], order[i+1], got)
		}
	}
	if got := Compare(Cp(42), Cp(42)); got != 0 {
		t.Errorf("같은 값 비교 = %d, want 0", got)
	}
}

// 태그가 갈려 있어야 부르는 쪽이 분기한다. cp 를 물으면 詰み은 답하지 않는다 —
// 그것이 합성값이 다시 생기지 않게 막는 하나뿐인 장치다.
func TestTheTagForcesTheCaller(t *testing.T) {
	if _, ok := Mate(3).Centipawns(); ok {
		t.Error("詰み이 cp 를 내줬다")
	}
	if _, ok := Cp(300).MateIn(); ok {
		t.Error("cp 가 詰み 手数를 내줬다")
	}
	// ok 를 버려도 값이 안 샌다. 두 접근자가 대칭이라 0 으로 떨어진다.
	if n, _ := Cp(300).MateIn(); n != 0 {
		t.Errorf("cp 300 의 MateIn = %d, want 0", n)
	}
	if cp, _ := Mate(3).Centipawns(); cp != 0 {
		t.Errorf("mate 3 의 Centipawns = %d, want 0", cp)
	}
	// 제로값은 호각이다. 「모른다」는 ok 플래그가 든다.
	if cp, ok := (Score{}).Centipawns(); !ok || cp != 0 {
		t.Errorf("제로값 = (%d, %v), want (0, true)", cp, ok)
	}
}

func TestNegFlipsBothKinds(t *testing.T) {
	if got := Cp(300).Neg(); got != Cp(-300) {
		t.Errorf("Cp(300).Neg() = %+v", got)
	}
	if got := Mate(3).Neg(); got != Mate(-3) {
		t.Errorf("Mate(3).Neg() = %+v", got)
	}
	if got := Mate(-5).Neg(); got != Mate(5) {
		t.Errorf("Mate(-5).Neg() = %+v", got)
	}
}

// 로그에 적히는 모양. 눌러 적을 자리를 안 남기려고 둔 것이라, 이 함수가 없어지면
// 측정 코드가 다시 환산 함수를 찾는다.
func TestStringSaysWhichKindItIs(t *testing.T) {
	for _, c := range []struct {
		in   Score
		want string
	}{
		{Cp(143), "+143cp"},
		{Cp(-20), "-20cp"},
		{Cp(0), "+0cp"},
		{Mate(3), "mate +3"},
		{Mate(-2), "mate -2"},
	} {
		if got := c.in.String(); got != c.want {
			t.Errorf("%#v.String() = %q, want %q", c.in, got, c.want)
		}
	}
}
