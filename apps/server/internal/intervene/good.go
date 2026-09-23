package intervene

import "github.com/jovid18/show-gi/apps/server/internal/eval"

// GoodGapWin 은 好手로 부르는 최선수와 차선수의 최소 승률 차다.
//
// 값의 근거는 journal §141. 개입 임계치와 같은 자(승률)를 쓴다. cp로 재면 형세가 기운
// 구간에서 차이가 커 보여도 결과가 바뀌지 않는 수까지 好手가 된다.
const GoodGapWin = 0.10

// GoodInput 은 好手 판정에 필요한 전부다. 전부 두는 쪽 관점이다.
//
// 「둔 수가 최선수인가」와 「누구나 보는 수인가」는 여기 없다. 수와 판을 봐야 답할 수
// 있으므로 부르는 쪽이 정하고, 이 패키지는 숫자만 받는다(Judge 와 같은 규약).
type GoodInput struct {
	// Best 는 착수 전 국면의 최선수(=둔 수) 평가치다.
	Best eval.Score
	// Second 는 같은 탐색의 2위 후보 평가치다.
	Second eval.Score
	// BaselineCp 는 Input.BaselineCp 와 같다.
	BaselineCp int
}

// GoodGap 은 최선수와 차선수의 승률 차다. 0 미만이면 0이다.
func GoodGap(in GoodInput) float64 {
	d := WinRateOf(in.Best, in.BaselineCp) - WinRateOf(in.Second, in.BaselineCp)
	if d < 0 {
		return 0
	}
	return d
}

// IsGood 은 최선수를 둔 한 수가 好手인가다.
//
// 이기는 詰み이 이미 보이는 국면은 부르지 않는다. 그 구간은 詰み 게이지가 맡고,
// 詰み을 이어 가는 수는 대개 차선수가 詰み을 놓쳐 승률 차가 1이 된다.
func IsGood(in GoodInput) bool {
	if n, ok := in.Best.MateIn(); ok && n > 0 {
		return false
	}
	return GoodGap(in) >= GoodGapWin
}
