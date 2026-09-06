package eval

import "testing"

// 이 버그의 자리다. 엔진의 생 cp 는 MateCp 를 넘어오므로(±35281 = 「이기는데 手数를
// 모름」) 환산값으로 줄을 세우면 1手詰み이 그 뒤로 밀린다(journal §131).
func TestAWinningMateOutranksTheEnginesRawCeiling(t *testing.T) {
	const rawCeiling = 35281
	if got := Compare(Mate(1), Cp(rawCeiling)); got <= 0 {
		t.Errorf("Compare(mate 1, cp %d) = %d, 詰み이 위여야 한다", rawCeiling, got)
	}
	// 환산값으로 재면 뒤집힌다 — 고치기 전의 순서가 이것이었다.
	if ApproxCp(Mate(1)) >= rawCeiling {
		t.Fatalf("환산값 %d 가 생 cp %d 를 넘는다 — 이 시험이 재는 것이 사라졌다",
			ApproxCp(Mate(1)), rawCeiling)
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
// 그것이 합성값이 다시 생기지 않게 막는 유일한 장치다.
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
	// 제로값은 「모른다」가 아니라 호각이다.
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

// 평평한 정수 컬럼으로 나가는 자리의 값. 지금 쌓여 있는 행이 이 자로 적혀 있어서
// (edges.eval_by_depth · game_moves.eval_cp) 여기가 바뀌면 옛 행과 새 행이 갈린다.
func TestApproxCpKeepsTheStoredScale(t *testing.T) {
	cases := []struct {
		in   Score
		want int
	}{
		{Cp(143), 143},
		{Cp(-35281), -35281},
		{Mate(0), MateCp},
		{Mate(1), MateCp - 10},
		{Mate(3), MateCp - 30},
		{Mate(-2), -MateCp + 20},
	}
	for _, c := range cases {
		if got := ApproxCp(c.in); got != c.want {
			t.Errorf("ApproxCp(%+v) = %d, want %d", c.in, got, c.want)
		}
	}
}
