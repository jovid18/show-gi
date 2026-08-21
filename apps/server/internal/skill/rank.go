package skill

import "math"

// 段級 표기. 추정치를 사람이 아는 말로 바꾸는 것뿐이고 새로 재지 않는다 — 같은 착수
// 흐름에서 나오므로 화면의 段級과 상대의 강함(game.strengthStep)이 다른 재료를 볼 수 없다.
//
// 같은 숫자는 아니다. 강함은 Estimate.Loss(비대칭 EMA)를, 段級은 Estimate.AbsLoss(창
// 안의 평균)를 본다 — 한쪽은 「지금 헤매는가」이고 다른 쪽은 「이 판이 어땠나」다(journal §94).

// rankNames 는 약한 쪽부터 센 쪽까지다. 段까지 가는 이유는 「級만 있는 척도는 위가 없어서
// 잘 두는 사람에게 더 말할 것이 없다」이고, 그 위(二段 이상)를 안 두는 이유는 아래 RankOf.
//
// 개수가 홀수라 가운데가 8級이다. 위도 아래도 있는 자리에 이름이 떨어지게 두는 것이고,
// 척도의 한쪽 끝이면 첫 이름이 이미 판정처럼 읽힌다.
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

// RankLossScale 은 척도의 가장 약한 끝이 뜻하는 절대 낙폭이다. 段級은 Estimate.AbsLoss 를
// 이 값으로 나눠 0~1로 편 뒤 눈금에 얹는다.
//
// intervene.Beginner 의 임계치와 값이 같은데 그 상수를 안 들여온다 — 이 패키지는
// intervene 을 모른다(패키지 주석). 같은 값을 고른 이유는 전원이 그 레벨로 두는 지금
// 화면의 이름이 안 움직이는 것뿐이다. [미확정] — 실측 앵커가 이 자를 대신한다(journal §94).
const RankLossScale = 0.25

// RankOf 는 추정치를 段級으로 바꾼다. 두 번째 값이 false면 「아직 모른다」다 —
// 표본이 모자라면 이름을 붙이지 않는다(MinSamples). 0을 돌려주면 화면이 그것을 「16級」으로
// 그리고, 그건 아무 근거 없이 사람에게 가장 낮은 이름을 붙이는 것이다.
//
// 미는 값은 Estimate.AbsLoss 다. 밴드가 쓰는 Loss 를 안 보는 이유가 둘이고 둘 다 표시에만
// 걸린다 — 분모가 레벨이라 임계치가 좁아지는 날 같은 실력이 네 계급 움직이고(journal §92),
// 비대칭 EMA 라 끝이 무너진 판이 판 전체보다 훨씬 나쁘게 남는다.
//
// 이 값은 道場や将棋ウォーズの段級ではない. 우리가 아는 것은 승률 낙폭뿐이고 그것을 절대
// 실력에 맞춰 본 적이 없다. 그래서 위를 初段에서 끊는다 — 二段·三段까지 늘리면 척도가
// 넓어지는 것이 아니라 더 큰 거짓말을 할 수 있게 될 뿐이다. 화면도 「目安」로 적는다.
// [미확정] — 눈금의 경계도 척도의 양 끝도 실측 전이고, 재는 장치는 journal §94.
func RankOf(e Estimate) (Rank, bool) {
	// 절대 낙폭의 표본으로 센다. Samples 가 차 있어도 이 칸이 빈 프로파일이 있다
	// (Estimate.AbsSamples).
	if e.AbsSamples < MinSamples {
		return Rank{}, false
	}
	// 낙폭이 클수록 약하다. 뒤집는 자리가 여기 하나뿐이라, 아래로 내려가는 값과 위로
	// 올라가는 이름이 어긋나는 자리도 여기 하나다.
	step := int(math.Round((1 - clamp01(e.AbsLoss/RankLossScale)) * float64(RankMax)))
	step = min(max(step, 0), RankMax)
	return Rank{Step: step, NameJa: rankNames[step]}, true
}
