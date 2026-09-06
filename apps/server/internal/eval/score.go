// Package eval 은 엔진 점수 하나를 든다 — centipawn 이거나 詰み까지의 手数이거나, 둘 중 하나다.
//
// 리프 패키지다. usi 도 store 도 여기를 들여오고, 여기는 이 레포의 어느 패키지도 안
// 들여온다 — 그래서 순환이 안 생긴다.
package eval

// MateCp 는 詰み을 정수 하나로 눌러야 하는 자리에서 쓰는 환산 기준이다(ApproxCp).
//
// 상한이 아니다. 엔진의 생 cp 가 이 값을 넘어온다 — 「이기는 것은 아는데 手数를 모름」이
// ±35281 이다. 상한으로 읽었더니 詰み 후보가 생 cp 뒤로 밀려 화살표가 1手詰み을 안
// 가리켰다(journal §131). 그래서 순서를 정하는 데 안 쓴다 — Compare 는 태그를 본다.
//
// 남은 자리는 개입 판정 하나다. 화면에 나가는 숫자에는 이제 안 쓴다 — 저장도 API 도
// 태그를 그대로 든다. 여기만 남은 이유는 K 와 임계치가 이 자로 실측된 값이라서다.
//
// 35281 이 더 큰데도 눌린 값이 그 자리에서 성립한다. WinRate(K=600) 가 둘 다 1.0 으로
// 포화하므로, 태그를 살려 「詰み이면 승률 1.0」이라고 쓰는 것과 결과가 같다. 옮기려면
// 상수를 다시 재야 하고 그때는 이 상수가 사라진다.
const MateCp = 30000

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

// Neg 는 관점을 상대 쪽으로 뒤집는다. cp 도 手数도 부호만 바뀐다.
//
// Mate(0) 만 자기 자신으로 돌아온다. 엔진이 안 내는 값이라 여기 오지 않는다(Mate).
func (s Score) Neg() Score { return Score{mate: s.mate, v: -s.v} }

// ApproxCp 는 태그를 버리고 정수 하나로 누른다.
//
// 부르는 자리가 넷이고 전부 개입 판정의 자다 — intervene.Input 의 두 칸과 얕은 평가,
// interventions 행, 그리고 총평·퀴즈가 「이기고 있었나」를 묻는 자리다. 넷 다 답이
// 승률로 포화해서 눌린 값과 태그가 같은 결론을 낸다(MateCp).
//
// 화면에 나가는 숫자에는 안 쓴다. 순서를 정하는 데도 안 쓴다.
//
// 0 을 이기는 쪽에 안 넣는다. Compare 는 Mate(0) 을 맨 아래로 보내므로 여기서 양수를
// 주면 한 패키지가 같은 값을 두 방향으로 읽는다 — 들어올 수 없는 값이지만(Mate) 두
// 자가 어긋나 있는 것 자체가 다음 사람의 함정이다.
func ApproxCp(s Score) int {
	if !s.mate {
		return s.v
	}
	if s.v > 0 {
		return MateCp - 10*s.v
	}
	return -MateCp - 10*s.v
}

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
