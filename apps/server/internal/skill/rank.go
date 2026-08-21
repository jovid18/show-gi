package skill

import "math"

// 段級 표기. 추정치를 사람이 아는 말로 바꾸는 것뿐이고 새로 재지 않는다 — 같은 착수
// 흐름에서 나오므로 화면의 段級과 상대의 강함(game.strengthStep)이 다른 재료를 볼 수 없다.
//
// 같은 숫자는 아니다. 강함은 Estimate.Loss(비대칭 EMA)를, 段級은 Estimate.AbsLoss(창
// 안의 평균)를 본다 — 한쪽은 「지금 헤매는가」이고 다른 쪽은 「이 판이 어땠나」다(journal §94).

// rankNames 는 약한 쪽부터 센 쪽까지다. 실측 앵커가 15級·5級·1級에 있고(rankAnchors)
// 그 셋이 이 척도의 아래 끝·가운데·위 끝이다.
//
// 段을 나누지 않는다. 級 구간은 계급당 낙폭이 ×1.05~1.10으로 단조롭게 줄지만 그 위는
// 평평하다 — 실측에서 初段 0.0662 · 三段 0.0651이고 1級이 0.0521로 오히려 낮았다.
// 강한 사람이 날카로운 국면을 두어 한 수의 승률 값이 커지는 것과 정확도가 좋아지는 것이
// 그 구간에서 상쇄된다. 그래서 위는 「初段」 한 칸이 「初段 이상」을 뜻한다(journal §94).
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
	// NameJa 는 그대로 화면에 나가는 표기다. 화면이 숫자에서 이름을 만들기 시작하면
	// 어휘가 두 벌이 되고, 척도를 늘리는 날 한쪽만 늘어난다.
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
// 창은 AnchorFromPly~AnchorToPly, 이미 갈린 국면은 뺐다(game.DecidedWinRate).
// 표본과 재는 법은 journal §94, 장치는 internal/kifu 의 TestMeasureRankAnchors.
//
// 양 끝 둘만 앵커다. 5級(0.0770)·1級(0.0706)도 쟀지만 5級~三段이 거의 평평해서
// (0.077 → 0.065, 7계급) 그 구간에 점을 박으면 낙폭 1%가 계급 넷을 움직인다. 그 둘은
// 검산으로 쓴다 — 이 선에서 두 계급 안에 들어온다(rank_test.go).
//
// [미확정] 판 18개다. 아래 끝은 판이 들어올 때마다 ±14% 튀고(0.1265 → 0.1439 → 0.1287)
// 위 끝은 네 라벨이 한 덩어리로 모인 자리다 — 5級 0.0770 · 1級 0.0706 · 初段 0.0659 ·
// 三段 0.0651이 서로 1σ 안이라 그 사이를 이 자로 못 가른다.
//
// 그래서 이름 사이가 실측이 아니다. 실측이 가르는 것은 셋이다 — 15級 · 그 덩어리 ·
// 5段(0.0332). 인접한 이름의 차이는 이 자로 확인된 사실이 아니고, 화면이 「目安」로
// 적는 이유가 그것이다(journal §94).
var rankAnchors = [...]rankAnchor{
	{Step: 0, Loss: 0.1287},       // 15級 — 판 5개
	{Step: RankMax, Loss: 0.0662}, // 初段 이상 — 初段 2판 0.0659 · 三段 3판 0.0651
}

// RankOf 는 추정치를 段級으로 바꾼다. 두 번째 값이 false면 「아직 모른다」다 —
// 표본이 모자라면 이름을 붙이지 않는다(MinSamples). 0을 돌려주면 화면이 그것을 「15級」으로
// 그리고, 그건 아무 근거 없이 사람에게 가장 낮은 이름을 붙이는 것이다.
//
// 미는 값은 Estimate.AbsLoss 다. 밴드가 쓰는 Loss 를 안 보는 이유가 둘이고 둘 다 표시에만
// 걸린다 — 분모가 레벨이라 임계치가 좁아지는 날 같은 실력이 네 계급 움직이고(journal §92),
// 비대칭 EMA 라 끝이 무너진 판이 판 전체보다 훨씬 나쁘게 남는다.
//
// 이 값은 道場や将棋ウォーズの段級ではない. 앵커는 사람끼리 둔 판에서 왔지만 재는 자가
// 승률 낙폭이라 「그 사이트의 급수」가 아니고, 엔진 대국에서는 상대가 실력에 맞춰 약해져
// 국면이 쉬워지므로 한 계급쯤 세게 나온다(journal §94). 화면도 「目安」로 적는다.
func RankOf(e Estimate) (Rank, bool) {
	// 절대 낙폭의 표본으로 센다. Samples 가 차 있어도 이 칸이 빈 프로파일이 있다
	// (Estimate.AbsSamples).
	if e.AbsSamples < MinSamples {
		return Rank{}, false
	}
	step := min(max(int(math.Round(rankStepOf(e.AbsLoss))), 0), RankMax)
	return Rank{Step: step, NameJa: rankNames[step]}, true
}

// rankStepOf 는 낙폭을 척도 위의 자리로 옮긴다. 자리가 소수로 나온다 — 부르는 쪽이 반올림한다.
//
// 앵커 사이는 로그 보간이다. 낙폭이 곱셈적이라(SD가 평균에 비례한다) 로그에서 잇는 것이고,
// 그러면 실측한 세 점이 자기 이름에 정확히 떨어진다. **앵커 사이는 실측이 아니다** —
// 15級과 5級 사이의 「9級」은 그 두 점을 이은 선 위의 자리일 뿐이다.
//
// 낙폭이 클수록 약하다. 뒤집는 자리가 여기 하나뿐이라, 아래로 내려가는 값과 위로
// 올라가는 이름이 어긋나는 자리도 여기 하나다.
func rankStepOf(absLoss float64) float64 {
	first, last := rankAnchors[0], rankAnchors[len(rankAnchors)-1]
	switch {
	case !(absLoss > 0):
		// 낙폭 0은 「매 수 최선」이다. 척도의 위 끝으로 보낸다 — 0을 로그에 넣을 수 없다.
		return float64(RankMax)
	case absLoss >= first.Loss:
		return float64(first.Step)
	case absLoss < last.Loss:
		// 앵커 아래는 안 늘린다. 段 사이를 이 자로 못 가르므로(rankNames) 마지막 앵커
		// 다음 칸 하나가 「初段 이상」을 다 받는다.
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
