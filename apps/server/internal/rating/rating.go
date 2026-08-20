// Package rating 은 사람끼리 둔 판의 결과로 레이팅을 갱신한다.
//
// 엔진도 DB도 판도 모른다. 입력은 두 사람의 지금 레이팅과 승패뿐이다 — intervene 과
// skill 이 같은 성질을 갖는 것과 같은 이유이고, 그래서 아래 상수를 흔들어 보는 데
// 대국도 DB도 필요 없다.
//
// skill 과 갈라 둔 이유는 척도가 다르기 때문이다. 저쪽의 Loss 는 임계치에 대한 비율이라
// 임계치가 사람마다 갈리는 순간 사람 사이에 비교할 수 없게 된다(intervene.Level.Threshold).
// 여기는 승패로만 움직이므로 정의상 사람 사이의 값이다.
//
// 밖으로 안 나간다. 어느 API 도 이 값을 돌려주지 않는다 — 매칭이 쓰는 내부 값이고,
// 보여주면 사람이 그것을 지키려 두기 시작한다.
package rating

import (
	"math"
	"time"
)

// Rating 은 한 사람의 레이팅과 그 불확실성이다. Glicko(1) 의 r 과 RD 다.
//
// Glicko-2 를 안 쓴다. 저쪽은 변동성(volatility) 칸이 하나 더 필요한데
// skill_profile 에 있는 것은 rating_est·rating_sd 둘이고(001_init.sql), 표본이 이만한
// 규모에서 변동성까지 추정해 얻을 것이 없다.
type Rating struct {
	// Value 는 레이팅이다. 클수록 세다.
	Value float64
	// Deviation 은 그 값을 얼마나 못 믿는가다(RD). 매칭 밴드가 이 값을 그대로 더한다 —
	// 모르는 사람에게 좁은 밴드는 뜻이 없다.
	Deviation float64
}

// Default 는 아무것도 모를 때의 레이팅이다. Glicko 의 관례값이고, 이 척도의 중앙이다.
const Default = 1500

// MaxDeviation 은 「전혀 모른다」에 해당하는 RD 다. 001_init.sql 의 rating_sd 기본값이
// 이 값이라, 스키마와 여기가 같은 것을 말한다.
const MaxDeviation = 350

// MinDeviation 은 RD 의 하한이다. 없으면 판을 많이 둔 사람의 레이팅이 굳어서, 실제로
// 세진 뒤에도 밴드가 옛 자리에 머문다.
//
// [미확정] 50은 Glicko 문헌의 흔한 값이고 실측이 아니다.
const MinDeviation = 50

// Unrated 는 한 판도 안 둔 사람이다. 시드가 없을 때 여기서 시작한다.
var Unrated = Rating{Value: Default, Deviation: MaxDeviation}

// Outcome 은 한 사람 관점의 결과다. match.Result 와 같은 어휘를 쓰지 않는다 —
// 이 패키지는 그쪽을 모르고, 값이 셋뿐이라 옮기는 자리가 부르는 쪽에 하나 있으면 된다.
type Outcome float64

const (
	Loss Outcome = 0
	Draw Outcome = 0.5
	Win  Outcome = 1
)

// q 는 Glicko 가 로지스틱 척도를 400점 단위로 옮기는 상수다.
var q = math.Ln10 / 400

// Update 는 한 판의 결과로 두 사람을 같이 갱신한다. outcome 은 a 관점이다.
//
// 둘을 한 함수에서 내는 이유는 순서 때문이다. 갱신된 값으로 상대를 계산하면 먼저
// 계산한 쪽이 이득을 보므로, 둘 다 갱신 전 값으로 상대를 본다.
func Update(a, b Rating, outcome Outcome) (Rating, Rating) {
	return one(a, b, outcome), one(b, a, Win-outcome)
}

// one 은 self 한 사람을 갱신한다. 상대는 갱신 전 값이다.
func one(self, opp Rating, s Outcome) Rating {
	g := gOf(opp.Deviation)
	e := expected(self.Value, opp.Value, opp.Deviation)

	// dSquared 는 이 판이 주는 정보량의 역수다. 결과가 뻔한 판(e 가 0이나 1에 가까운
	// 판)은 알려 주는 것이 적어서 값이 커지고, 그만큼 아래 갱신이 작아진다.
	dSquared := 1 / (q * q * g * g * e * (1 - e))

	// 0으로 나누기를 막는다. e 가 0이나 1로 포화하면 dSquared 가 Inf 가 되고, 그때
	// 아래 두 식이 NaN 을 낸다 — 레이팅 칸에 NaN 이 한 번 들어가면 그 뒤의 모든 판이
	// NaN 이다.
	if math.IsInf(dSquared, 0) || math.IsNaN(dSquared) {
		return self
	}

	invVar := 1/(self.Deviation*self.Deviation) + 1/dSquared
	value := self.Value + q/invVar*g*(float64(s)-e)
	dev := math.Sqrt(1 / invVar)

	return Rating{Value: value, Deviation: clampDeviation(dev)}
}

// gOf 는 상대의 RD 가 이 판의 무게를 얼마나 줄이는가다. 못 믿는 상대를 이겨도 덜 오른다.
func gOf(dev float64) float64 {
	return 1 / math.Sqrt(1+3*q*q*dev*dev/(math.Pi*math.Pi))
}

// expected 는 self 가 이길 기대값이다(0~1).
func expected(self, opp, oppDev float64) float64 {
	return 1 / (1 + math.Pow(10, -gOf(oppDev)*(self-opp)/400))
}

// InactivityToUnrated 는 한 판도 안 두면 RD 가 MaxDeviation 까지 되돌아가는 데 걸리는
// 시간이다. Inflate 의 유일한 손잡이다 — Glicko 의 c 를 직접 적지 않는 이유는 그 값이
// 무엇을 뜻하는지 읽는 사람이 알 수 없기 때문이다.
//
// [미확정] 90일은 초기값이다. 사람이 두 달 쉬고 돌아왔을 때 옛 밴드로 매칭되면 안 된다는
// 것만 정했고, 얼마가 맞는지는 안 쟀다.
const InactivityToUnrated = 90 * 24 * time.Hour

// Inflate 는 안 둔 시간만큼 RD 를 되돌린다. 읽는 자리에서 부른다 — 저장된 값은 마지막
// 판 직후의 것이고, 그 뒤로 흐른 시간은 저장할 수 없다.
//
// 값이 아니라 불확실성만 움직인다. 안 두는 동안 세지지도 약해지지도 않았다는 것이 아는
// 전부이고, 모르는 것은 RD 가 말한다.
func Inflate(r Rating, since time.Duration) Rating {
	if since <= 0 {
		return r
	}
	// c 는 MinDeviation 에서 InactivityToUnrated 만큼 쉬면 MaxDeviation 에 닿는 크기다.
	periods := since.Hours() / InactivityToUnrated.Hours()
	cSquared := (MaxDeviation*MaxDeviation - MinDeviation*MinDeviation) * periods
	r.Deviation = clampDeviation(math.Sqrt(r.Deviation*r.Deviation + cSquared))
	return r
}

// SeedSpread 는 시드가 Default 에서 벗어날 수 있는 폭이다.
//
// [미확정] 400은 초기값이다. skill 의 낙폭을 레이팅으로 옮기는 환산비를 실측한 적이
// 없고, 재려면 사람끼리 둔 판이 먼저 쌓여야 한다 — 그때 이 값을 다시 잡는다.
const SeedSpread = 400

// SeedFromLoss 는 엔진 대국의 실력 추정치를 첫 레이팅으로 옮긴다. loss 는
// skill.Estimate.Loss 다(0~1, 작을수록 세다).
//
// 사람과 한 판도 안 둔 사람에게도 시작값이 있는 것이 이 함수의 전부다. 유저가 적은
// 동안 첫 매칭이 무작위가 아닌 것과 같은 말이다.
//
// RD 는 MaxDeviation 그대로다. 낙폭은 절대 실력에 맞춰 본 적이 없는 척도라
// (skill.RankOf) 여기서 나온 값을 믿을 근거가 없고, 그것을 RD 가 말한다.
func SeedFromLoss(loss float64) Rating {
	return Rating{
		Value:     Default + SeedSpread*(1-2*clamp01(loss)),
		Deviation: MaxDeviation,
	}
}

func clampDeviation(d float64) float64 {
	return min(max(d, MinDeviation), MaxDeviation)
}

func clamp01(v float64) float64 { return min(max(v, 0), 1) }
