package game

import (
	"context"
	"fmt"
	"math"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// MultiSearcher 는 후보 여러 개를 한 번에 받아오는 탐색이다. usi.Pool 이 만족한다.
//
// 적응형 상대에게 후보 풀이 없으면 성립하지 않는다 — **강함을 탐색 길이가 아니라
// 고르는 쪽에서 얻겠다는 설계가 곧 MultiPV를 요구한다**(01-core.md §6).
type MultiSearcher interface {
	SearchMultiPV(ctx context.Context, startSFEN string, moves []string, depth, multiPV int) (usi.SearchResult, error)
}

// Band 는 상대가 겨냥하는 형세 구간이다. **플레이어 관점 cp**다.
//
// 양수면 플레이어가 유리하다는 뜻이므로, 상대는 자기가 조금 지는 쪽을 겨냥한다.
type Band struct{ LoCp, HiCp int }

// DefaultBand 는 「조금씩 지고 있지만 아직 모른다」 구간이다. **플레이어 관점 cp.**
// 값은 실측으로 유지 판정을 받았고, 「초심자에게 맞는 폭인가」는 아직 못 쟀다 —
// **[미확정]** 근거와 숫자는 06-status.md §39 ③.
var DefaultBand = Band{LoCp: 100, HiCp: 300}

// SkillShiftCp 는 실력 추정이 밴드를 옮길 수 있는 최대 폭이다. **플레이어 관점 cp.**
//
// 낙폭이 skill.PriorLoss 면 0이고, 매 수 블런더면 +300(플레이어가 더 유리한 쪽으로 겨냥한다),
// 매 수 최선이면 -300이다. 즉 **잘 두는 사람에게는 상대가 이기려 든다.**
//
// **조절하는 것은 밴드뿐이다.** 자살수 필터도 후보 k도 안 건드린다 — 쉽게 해주는 것과
// 던지는 것은 다르고, 화면이 「取り返せない場所」라고 가르친 수를 상대가 두면 방금 배운
// 것이 무너진다(06-status.md §16 · §21 ①).
//
// **[미확정]** 초기값이다. 근거와 남은 것은 06-status.md §47.
const SkillShiftCp = 300

// CandidateK 는 후보를 몇 개까지 받을지다. **실측으로 정했다**(06-status.md §10).
//
// 밴드 적중이 10에서 멈추고 20은 덮이는 국면이 안 늘면서 비용만 배가 된다.
// k=1은 0/6이다 — 후보가 하나면 밴드 제어라는 것이 성립할 수 없다.
const CandidateK = 10

type adaptiveOpponent struct {
	search MultiSearcher
	depth  int
	k      int
	// base 는 아무것도 모를 때 겨냥하는 구간이다. 실력 추정이 매 수 여기서 옮긴다(skillShift).
	base Band
}

// NewAdaptiveOpponent 는 「지지만 던지지 않는」 상대를 만든다.
//
// **약화는 엔진이 아니라 고르는 자리에서 한다.** 엔진이 스스로 실수를 섞으면 고른 수가 얼마나
// 나쁜지를 우리가 모르게 되고, 평가치가 오염되면 밴드 제어와 「180cp 나빴다」가 함께 무너진다
// (01-core.md §6).
//
// band 는 **기준선**이다. 실제로 겨냥하는 구간은 매 수 실력 추정이 옮긴다(Choose).
func NewAdaptiveOpponent(s MultiSearcher, depth int, band Band) Opponent {
	if depth < 1 {
		depth = DefaultDepth
	}
	if band.LoCp == 0 && band.HiCp == 0 {
		band = DefaultBand
	}
	return &adaptiveOpponent{search: s, depth: depth, k: CandidateK, base: band}
}

// AdaptsToSkill 은 언제나 true 다 — 밴드를 옮기는 것이 이 구현의 전부다(SkillAdapter).
func (o *adaptiveOpponent) AdaptsToSkill() bool { return true }

func (o *adaptiveOpponent) Choose(ctx context.Context, startSFEN string, moves []string, sk skill.Estimate) (string, error) {
	band := o.base.shifted(skillShift(sk))

	res, err := o.search.SearchMultiPV(ctx, startSFEN, moves, o.depth, o.k)
	if err != nil {
		return "", err
	}
	if res.Best == "" {
		return "", fmt.Errorf("adaptive: engine returned no move")
	}

	pos, err := positionAfter(startSFEN, moves)
	if err != nil {
		// 판을 못 읽으면 고를 근거가 없다. **최선수로 물러선다** — 약화는 부가 기능이고
		// 대국이 본체다. 개입 판정이 실패해도 대국을 멈추지 않는 것과 같은 판단이다.
		return res.Best, nil
	}

	best, bestDist := res.Best, -1
	for _, line := range res.Lines {
		if line.Move == "" {
			continue
		}
		m, err := shogi.ParseUSIMove(line.Move)
		if err != nil {
			continue
		}
		// 엔진은 늘 수번 측(=상대) 관점으로 답한다. 밴드는 플레이어 관점이라 뒤집는다.
		playerCp := -line.ScoreCp

		// 「던지지 않는다」. 이 필터는 **엔진이 필요 없다** — 룰 엔진만으로 된다.
		if MoveFeatures(pos, m).HangsPiece() {
			continue
		}
		if d := band.distance(playerCp); bestDist < 0 || d < bestDist {
			best, bestDist = line.Move, d
		}
	}
	return best, nil
}

// skillShift 는 추정치가 밴드를 옮기는 폭이다(플레이어 관점 cp).
//
// **표본이 모자라면 0이다** — 첫 수 몇 개로 상대가 바뀌면 사람이 알아차리기 전에 강함이
// 흔들린다(skill.MinSamples).
//
// **양쪽을 따로 정규화한다.** `(loss - prior) * 2` 로 쓰면 그 식이 `prior = 0.5` 를 몰래
// 못 박는다 — 그 값은 아직 실측 전이고(§47), 0.3으로 옮기는 날 너그러운 쪽이 최대 폭을
// 40% 넘어가고 세지는 쪽은 60%에서 잘린다. 아래는 prior 가 어디에 있어도 양 끝이
// 정확히 ±SkillShiftCp 다. `prior = 0.5` 에서는 옛 식과 같은 값이라 §47의 실측이 그대로 산다.
func skillShift(sk skill.Estimate) int {
	if !sk.Ready() {
		return 0
	}
	// **바깥에서 온 값을 자른다.** `Rater` 가 밖에 열린 인터페이스라, 1을 넘는 낙폭을
	// 돌려주는 구현 하나가 밴드를 임의로 밀 수 있다.
	loss := min(max(sk.Loss, 0), 1)
	prior := skill.PriorLoss

	// prior 가 0이나 1이어도 그쪽 분기가 안 돌아 0으로 나누는 일이 없다 — loss 를 이미 잘랐다.
	var ratio float64
	switch {
	case loss > prior:
		ratio = (loss - prior) / (1 - prior)
	case loss < prior:
		ratio = -(prior - loss) / prior
	}
	return int(math.Round(ratio * SkillShiftCp))
}

// strengthStep 은 그 폭을 화면용 5단계로 자른 것이다. 5가 최선수에 가깝고 1이 가장 너그럽다.
//
// **밴드와 같은 숫자에서 나온다.** 단계를 따로 계산하면 화면이 말하는 강함과 상대가 실제로
// 겨냥하는 강함이 갈린다 — 게이지에서 手数를 따로 구했다가 한 수 낡았던 것과 같은 실패다
// (06-status.md §31).
func strengthStep(shiftCp int) int {
	// 한 눈금이 SkillShiftCp 의 절반이다. **상수 나눗셈으로 쓰지 않는다** — `SkillShiftCp` 가
	// 홀수가 되는 날 정수 나눗셈이 조용히 한 칸을 먹는다.
	step := 3 - int(math.Round(float64(shiftCp)/(float64(SkillShiftCp)/2)))
	return min(max(step, 1), 5)
}

// shifted 는 밴드를 통째로 옮긴 것이다. **폭은 그대로다** — 넓히는 것과 옮기는 것은 다르고,
// 넓히면 같은 실력에서도 상대의 강함이 수마다 튄다.
func (b Band) shifted(cp int) Band {
	return Band{LoCp: b.LoCp + cp, HiCp: b.HiCp + cp}
}

// distance 는 밴드에서 벗어난 정도다(안에 들면 0). **가장 가까운 후보를 고르면 두 경우가 저절로
// 갈린다** — 상대가 지고 있으면 전부 밴드 위라 최선수가 뽑히고(던지지 않는다), 이기고 있으면
// 전부 아래라 가장 너그러운 수가 뽑힌다. 여기서 최선수로 물러서면 조절이 가장 필요한 자리에서
// 상대가 초심자를 뭉갠다.
func (b Band) distance(cp int) int {
	switch {
	case cp < b.LoCp:
		return b.LoCp - cp
	case cp > b.HiCp:
		return cp - b.HiCp
	default:
		return 0
	}
}

// positionAfter 는 startSFEN 에 수순을 전부 놓은 국면을 돌려준다.
func positionAfter(startSFEN string, moves []string) (shogi.Position, error) {
	pos, err := shogi.ParseSFEN(startSFEN)
	if err != nil {
		return pos, err
	}
	for _, u := range moves {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			return pos, err
		}
		pos = pos.Apply(m)
	}
	return pos, nil
}
