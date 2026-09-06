package eval

import "testing"

// 판정으로 나가는 값이 태그를 붙이기 전과 한 비트도 다르지 않아야 한다.
//
// 개입의 K와 임계치가 이 자로 실측된 값이라(journal §19·§39), 여기가 움직이면 상수를
// 다시 잡아야 한다. 왼쪽이 옛 코드의 산수이고 오른쪽이 지금의 것이다.
func TestApproxCpMatchesTheArithmeticItReplaced(t *testing.T) {
	// 옛 usi.parseScore 가 만들던 값 그대로.
	was := func(mate bool, v int) int {
		if !mate {
			return v
		}
		if v >= 0 {
			return 30000 - 10*v
		}
		return -30000 - 10*v
	}
	for _, v := range []int{-35281, -900, -1, 0, 1, 900, 35281} {
		if got, want := ApproxCp(Cp(v)), was(false, v); got != want {
			t.Errorf("cp %d = %d, want %d", v, got, want)
		}
		if got, want := ApproxCp(Cp(v).Neg()), -was(false, v); got != want {
			t.Errorf("cp %d 뒤집기 = %d, want %d", v, got, want)
		}
	}
	// 0 을 빼는 이유는 Mate 의 doc 에 있다 — 엔진이 안 내는 값이고, 부호가 없어서
	// 뒤집기가 성립하지 않는다.
	for _, n := range []int{-17, -2, -1, 1, 2, 17} {
		if got, want := ApproxCp(Mate(n)), was(true, n); got != want {
			t.Errorf("mate %d = %d, want %d", n, got, want)
		}
		if got, want := ApproxCp(Mate(n).Neg()), -was(true, n); got != want {
			t.Errorf("mate %d 뒤집기 = %d, want %d", n, got, want)
		}
	}
}
