package skill

import "math"

// 段級 표기. 추정치를 사람이 아는 말로 바꾸는 것뿐이고, 새로 재지 않는다 — Estimate.Loss
// 하나에서 나오므로 화면의 段級과 상대의 강함(game.strengthStep)이 갈릴 수 없다. 같은
// 값에서 둘을 뽑는 이유는 journal §31.

// rankNames 는 약한 쪽부터 센 쪽까지다. 段까지 가는 이유는 「級만 있는 척도는 위가 없어서
// 잘 두는 사람에게 더 말할 것이 없다」이고, 그 위(二段 이상)를 안 두는 이유는 아래 RankOf.
//
// 가운데가 8級이다. PriorLoss 가 여기에 떨어지도록 개수를 골랐다 — 아무것도 모르는
// 사람에게 처음 붙는 이름이라, 척도의 한쪽 끝이면 첫 화면이 이미 판정처럼 읽힌다.
var rankNames = [...]string{
	"16級", "15級", "14級", "13級", "12級", "11級", "10級", "9級",
	"8級",
	"7級", "6級", "5級", "4級", "3級", "2級", "1級",
	"初段",
}

// Rank 는 추정치를 段級으로 읽은 것이다.
type Rank struct {
	// Step 은 0부터 RankMax 까지이고 클수록 세다. 화면의 눈금이 이 값이다.
	Step int
	// NameJa 는 그대로 화면에 나가는 표기다. 화면이 숫자에서 이름을 만들기 시작하면
	// 어휘가 두 벌이 되고, 척도를 늘리는 날 한쪽만 늘어난다.
	NameJa string
}

// RankMax 는 Step 의 상한이다(= 初段).
const RankMax = len(rankNames) - 1

// RankOf 는 추정치를 段級으로 바꾼다. 두 번째 값이 false면 「아직 모른다」다 —
// 표본이 모자라면 이름을 붙이지 않는다(MinSamples). 0을 돌려주면 화면이 그것을 「16級」으로
// 그리고, 그건 아무 근거 없이 사람에게 가장 낮은 이름을 붙이는 것이다.
//
// 이 값은 道場や将棋ウォーズの段級ではない. 우리가 아는 것은 임계치에 대한 낙폭뿐이고
// (Estimate.Loss) 그것을 절대 실력에 맞춰 본 적이 없다. 그래서 위를 初段에서 끊는다 —
// 二段·三段까지 늘리면 척도가 넓어지는 것이 아니라 더 큰 거짓말을 할 수 있게 될 뿐이다.
// 화면도 「目安」로 적는다. [미확정] — 보정은 사람 표본을 기다린다(journal §39).
func RankOf(e Estimate) (Rank, bool) {
	if !e.Ready() {
		return Rank{}, false
	}
	// 낙폭이 클수록 약하다. 뒤집는 자리가 여기 하나뿐이라, 아래로 내려가는 값과 위로
	// 올라가는 이름이 어긋나는 자리도 여기 하나다.
	step := int(math.Round((1 - clamp01(e.Loss)) * float64(RankMax)))
	step = min(max(step, 0), RankMax)
	return Rank{Step: step, NameJa: rankNames[step]}, true
}
