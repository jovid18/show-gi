// Package eval 은 엔진 점수 하나를 든다 — centipawn 이거나 詰み까지의 手数이거나, 둘 중 하나다.
//
// 리프 패키지다. usi 도 store 도 여기를 들여오고, 여기는 이 레포의 어느 패키지도 안
// 들여온다 — 그래서 순환이 안 생긴다.
package eval

import "fmt"

// Score 는 후보 하나의 점수다. 수번 측 관점이고, 관점을 옮기는 것은 Neg 다.
//
// 필드가 비공개라 합성값을 만들 수 없다. USI 는 score cp 와 score mate 를 배타적으로
// 내므로(한 라인에 둘이 같이 오지 않는다) 담는 쪽도 그 모양이어야 한다.
//
// 제로값은 Cp(0) 이다 — 「점수를 아직 모른다」가 아니라 「호각」이라는 뜻이고, 모르는
// 것은 부르는 쪽이 ok 플래그로 든다(usi.SearchResult.ScoreAtDepth).
type Score struct {
	mate bool
	v    int
}

// Cp 는 centipawn 점수다. 양수면 수번 측이 좋다.
func Cp(v int) Score { return Score{v: v} }

// Mate 는 詰み까지의 手数다. 양수면 수번 측이 詰ます 쪽이고, 음수면 詰まされる 쪽이다.
//
// n 에 0 을 넣지 않는다. 부호가 없어서 Neg 가 관점을 못 옮기고(-0 = 0), 그래서 「내가
// 詰んでいる」과 「상대가 詰んでいる」이 한 값이 된다 — 여기서 뜻을 정하면 어느 쪽으로
// 정하든 절반이 거짓이다. 그래서 값이 들어오는 자리에서 막는다(usi.parseScore).
//
// 엔진이 그 값을 안 낸다. 이미 詰んでいる 국면에 물으면 score mate -1 에 bestmove
// resign 이다 — 실측이고, 그 사실이 무너지면 실엔진 테스트가 먼저 빨개진다(journal §131).
func Mate(n int) Score { return Score{mate: true, v: n} }

// MateIn 은 詰み까지의 手数다. cp 점수면 0, false — Centipawns 와 대칭이라 ok 를 버려도
// 값이 새지 않는다.
func (s Score) MateIn() (int, bool) {
	if !s.mate {
		return 0, false
	}
	return s.v, true
}

// Centipawns 는 cp 값이다. 詰み이면 ok=false — 부르는 쪽이 분기하게 강제한다.
func (s Score) Centipawns() (int, bool) {
	if s.mate {
		return 0, false
	}
	return s.v, true
}

// String 은 로그와 측정 표에 적히는 모양이다. cp 는 부호를 붙이고 詰み은 手数로 말한다.
//
// 있는 이유는 눌러 적을 자리를 안 남기기 위해서다 — %d 로 찍으려고 환산 함수를 부르는
// 순간 그 숫자가 다시 돌아다닌다.
func (s Score) String() string {
	if s.mate {
		return fmt.Sprintf("mate %+d", s.v)
	}
	return fmt.Sprintf("%+dcp", s.v)
}

// Neg 는 관점을 상대 쪽으로 뒤집는다. cp 도 手数도 부호만 바뀐다.
//
// Mate(0) 만 자기 자신으로 돌아온다. 엔진이 안 내는 값이라 여기 오지 않는다(Mate).
func (s Score) Neg() Score { return Score{mate: s.mate, v: -s.v} }

// mateRank 는 詰み을 cp 가 못 닿는 자리로 밀어 두는 값이다. 엔진의 생 cp 가 얼마까지
// 오르든 상관없게 두 자를 갈라 놓는 것이 요점이고, 그래서 상수를 실측에 안 맞춘다.
const mateRank = int64(1) << 40

// Compare 는 수번 측에게 좋은 쪽이 크다. 이기는 詰み > 어떤 cp > 지는 詰み이고,
// 짧게 이기는 쪽이 위, 늦게 지는 쪽이 위다.
//
// 0 이 아닌 手数에서 화면 쪽 rankOf 와 같은 규칙이다(apps/web/src/app/libs/whatif/branch.ts).
// 둘이 갈리면 목록의 1위와 판 위의 초록 화살표가 다른 수를 가리킨다. 0 은 저쪽이 cp 로
// 흘려보내고 이쪽은 맨 아래로 보내는데, 그 값은 애초에 안 만든다(Mate).
func Compare(a, b Score) int {
	x, y := rank(a), rank(b)
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

func rank(s Score) int64 {
	if !s.mate {
		return int64(s.v)
	}
	if s.v > 0 {
		return mateRank - int64(s.v)
	}
	return -mateRank - int64(s.v)
}
