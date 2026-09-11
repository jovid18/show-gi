package skill

import "math"

// 段級 표기. 추정치를 사람이 아는 말로 바꿀 뿐 새로 재지 않는다.
//
// 段級과 상대의 강함(game.strengthStep)은 서로 다른 숫자다. 강함은 Estimate.Loss(비대칭
// EMA)를 보고 段級은 Estimate.AbsLoss(창 안의 평균)를 본다(journal §94).

// rankNames 는 약한 쪽부터 센 쪽까지다. 실측 앵커는 양 끝 둘이다(rankAnchors).
//
// 段을 나누지 않는다. 級 구간은 계급당 낙폭이 단조롭게 줄지만 그 위는 평평하다. 강한
// 사람이 날카로운 국면을 두어 한 수의 승률 값이 커지는 것과 정확도가 좋아지는 것이 그
// 구간에서 상쇄된다. 그래서 맨 위 「初段」 한 칸이 「初段 이상」을 받는다(journal §94).
var rankNames = [...]string{
	"15級", "14級", "13級", "12級", "11級", "10級", "9級",
	"8級",
	"7級", "6級", "5級", "4級", "3級", "2級", "1級",
	"初段",
}

// Rank 는 추정치를 段級으로 읽은 것이다.
type Rank struct {
	// Step 은 0부터 RankMax 까지이고 클수록 세다. 화면의 눈금이 이 값이다.
	Step int
	// NameJa 는 그대로 화면에 나가는 표기다. 화면이 숫자에서 이름을 만들면 어휘가 두 벌이
	// 되고, 척도를 늘리는 날 한쪽만 늘어난다.
	NameJa string
}

// RankMax 는 Step 의 상한이다(= 初段).
const RankMax = len(rankNames) - 1

// rankAnchor 는 실측한 급수 하나다. Loss 는 그 급수가 낸 절대 낙폭의 평균이다.
type rankAnchor struct {
	Step int
	Loss float64
}

// rankAnchors 는 사람이 둔 판에서 잰 기준값이다. 약한 쪽부터이고 Loss 는 줄어든다.
//
// 창은 AnchorFromPly~AnchorToPly, 이미 갈린 국면은 뺐다(game.DecidedWinRate). 표본과
// 재는 법은 journal §94, 장치는 internal/kifu 의 TestMeasureRankAnchors.
//
// 양 끝 둘만 앵커다. 5級~三段이 거의 평평해서 그 구간에 점을 박으면 낙폭 1%가 계급 넷을
// 움직인다. 사이에서 쟀던 급수들은 검산으로 쓴다(rank_test.go).
//
// [미확정] 이름 사이는 실측 없이 이은 것이라 화면이 「目安」로 적는다. 아래 끝은 판이
// 들어올 때마다 ±14% 튄다(0.1265 → 0.1439 → 0.1287).
var rankAnchors = [...]rankAnchor{
	{Step: 0, Loss: 0.1287},       // 15級 — 판 5개
	{Step: RankMax, Loss: 0.0662}, // 初段 이상 — 初段 2판 0.0659 · 三段 3판 0.0651
}

// RankOf 는 추정치를 段級으로 바꾼다. 두 번째 값이 false면 「아직 모른다」다. 표본이
// 모자라면 이름을 붙이지 않는다(MinSamples). 0을 돌려주면 화면이 그것을 「15級」으로
// 그리고, 그건 근거 없이 가장 낮은 이름을 붙이는 것이다.
//
// 보는 값은 Estimate.AbsLoss 다. 밴드가 쓰는 Loss 는 분모가 레벨이라 임계치가 좁아지는 날
// 같은 실력이 네 계급 움직이고(journal §92), 비대칭 EMA 라 끝이 무너진 판이 판 전체보다
// 나쁘게 남는다.
//
// 한 프로파일에 엔진 대국과 사람끼리의 판이 섞이고, 앞쪽이 한 계급쯤 세게 나온다
// (journal §94, §95).
func RankOf(e Estimate) (Rank, bool) {
	// Samples 가 차 있어도 이 칸이 빈 프로파일이 있다(Estimate.AbsSamples).
	if e.AbsSamples < MinSamples {
		return Rank{}, false
	}
	step := min(max(int(math.Round(rankStepOf(e.AbsLoss))), 0), RankMax)
	return Rank{Step: step, NameJa: rankNames[step]}, true
}

// rankStepOf 는 낙폭을 척도 위의 자리로 옮긴다. 자리가 소수로 나오고 부르는 쪽이 반올림한다.
//
// 앵커 사이는 로그 보간이다. 낙폭이 곱셈적이라(SD가 평균에 비례한다) 로그에서 잇고,
// 그러면 실측한 두 점이 자기 이름에 정확히 떨어진다.
//
// 낙폭이 클수록 약하다. 부호를 뒤집는 자리가 여기 하나뿐이다.
func rankStepOf(absLoss float64) float64 {
	first, last := rankAnchors[0], rankAnchors[len(rankAnchors)-1]
	switch {
	case !(absLoss > 0):
		// 낙폭 0은 「매 수 최선」이다. 0을 로그에 넣을 수 없어 척도의 위 끝으로 보낸다.
		return float64(RankMax)
	case absLoss >= first.Loss:
		return float64(first.Step)
	case absLoss < last.Loss:
		// 앵커 아래로 척도를 늘리지 않는다. 段 사이를 이 자로 가를 수 없다(rankNames).
		return float64(RankMax)
	}
	for i := 0; i+1 < len(rankAnchors); i++ {
		lo, hi := rankAnchors[i], rankAnchors[i+1]
		if absLoss <= hi.Loss {
			continue
		}
		span := math.Log(lo.Loss / hi.Loss)
		return float64(lo.Step) + float64(hi.Step-lo.Step)*math.Log(lo.Loss/absLoss)/span
	}
	return float64(last.Step)
}
