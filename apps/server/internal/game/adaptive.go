package game

import (
	"context"
	"fmt"
	"math"

	"github.com/jovid18/show-gi/apps/server/internal/handicap"
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
//
// **좌표가 둘이다.** 구간 안이나 아래에서는 절대 좌표로 읽고(플레이어가 아직 못 이기고
// 있으므로 「+100~+300으로 끌어올린다」가 곧 뜻이다), 구간 **위**에서는 지금 형세에 대한
// **양보 폭**으로 읽는다. 절대 좌표 하나로 쓰면 이 숫자가 그 구간에서 정반대를 뜻한다 —
// 「+300으로 되돌려라」가 되고, 그 자리에서 조절이 꺼진다(journal §55).
//
// **그 절대 좌표의 원점은 手合割이 정한다.** 平手는 0cp이고 駒落ち는 그 手合의 초기
// 평가치다 — 옮기는 자리는 Choose 하나뿐이다.
type Band struct{ LoCp, HiCp int }

// DefaultBand 는 「조금씩 지고 있지만 아직 모른다」 구간이다. **플레이어 관점 cp.**
// 값은 실측으로 유지 판정을 받았고, 「초심자에게 맞는 폭인가」는 아직 못 쟀다 —
// **[미확정]** 근거와 숫자는 journal §39 ③.
var DefaultBand = Band{LoCp: 100, HiCp: 300}

// SkillShiftCp 는 실력 추정이 밴드를 옮길 수 있는 최대 폭이다. **플레이어 관점 cp.**
//
// 낙폭이 skill.PriorLoss 면 0이고, 매 수 블런더면 +300(플레이어가 더 유리한 쪽으로 겨냥한다),
// 매 수 최선이면 -300이다. 즉 **잘 두는 사람에게는 상대가 이기려 든다.**
//
// **입력은 수의 질뿐이다**(`skill.Track.Observe` 가 낙폭 하나를 먹는다). 대국 결과도 형세도
// 안 본다 — 「이기고 있으면 상대가 세진다」는 여기서 나온 적이 없고, 밴드의 절대 좌표에서
// 나왔다(journal §55).
//
// **조절하는 것은 밴드뿐이다.** 두 안전 필터도 후보 k도 안 건드린다 — 쉽게 해주는 것과
// 던지는 것은 다르고, 화면이 「取り返せない場所」라고 가르친 수를 상대가 두면 방금 배운
// 것이 무너진다(journal §16 · §21 ①).
//
// **[미확정]** 초기값이다. 근거와 남은 것은 journal §47.
const SkillShiftCp = 300

// CandidateK 는 후보를 몇 개까지 받을지다. **실측으로 정했다**(journal §10).
//
// 밴드 적중이 10에서 멈추고 20은 덮이는 국면이 안 늘면서 비용만 배가 된다.
// k=1은 0/6이다 — 후보가 하나면 밴드 제어라는 것이 성립할 수 없다.
const CandidateK = 10

// MaxConcessionCp 는 난이도가 아무리 낮아도 한 수에 양보할 수 있는 최대 폭이다.
// **최선의 안전한 후보와의 차이**로 센다 — 절대 형세로 세면 이미 크게 이기거나 지는
// 국면에서 같은 수가 안전했다가 위험해진다.
//
// 가장 너그러운 밴드의 상단이 DefaultBand.HiCp + SkillShiftCp = 600cp다. 난이도는 이
// 안에서 목표를 옮길 뿐이고, 이 선을 넘는 수로 밴드를 억지로 맞추지 않는다.
const MaxConcessionCp = 600

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

// ChooseBest 는 밴드를 안 보고 최선수를 낸다(BestPlayer). 사람이 詰み을 걸고 있는 동안만
// 불린다 — 근거는 MateChasePlies.
//
// **같은 k로 묻는다.** 깊이도 k도 평소와 같아야 `positions` 캐시가 같은 행을 쓰고,
// k가 갈리면 같은 국면의 1위가 갈린다(journal §34 ②). 조절을 끄는 것이지 다른
// 탐색을 하는 것이 아니다.
func (o *adaptiveOpponent) ChooseBest(ctx context.Context, startSFEN string, moves []string) (string, error) {
	res, err := o.search.SearchMultiPV(ctx, startSFEN, moves, o.depth, o.k)
	if err != nil {
		return "", err
	}
	if res.Best == "" {
		return "", fmt.Errorf("adaptive: engine returned no move")
	}
	return res.Best, nil
}

func (o *adaptiveOpponent) Choose(ctx context.Context, startSFEN string, moves []string, sk skill.Estimate) (string, error) {
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

	// **밴드의 원점을 手合割이 옮긴다.** 절대 좌표 쪽은 「호각」을 0cp로 읽는데(Band),
	// 駒落ち에서 호각은 그 手合의 초기 평가치다 — 안 옮기면 그 좌표가 판 내내 도달
	// 불가능해진다(첫 수부터 `now` 가 상단 위라 아래 상대 좌표 분기로만 간다). 그러면
	// 상대가 **핸디캡을 되돌려 주는 일을 아예 안 한다**: 사람이 二枚落ち의 +1490을 +500까지
	// 흘려도 「지금 형세에서 100~300 더」만 겨냥해서, 조절이 가장 필요한 자리에서 가장
	// 약하게 돈다 — journal §55가 고친 것의 거울상이다(journal §84).
	//
	// 사람의 색은 지금 수번(=상대)의 반대다. `pos` 에서 얻으므로 배선이 늘지 않는다 —
	// `bookOpponent` 가 상태를 안 들고 매번 국면에서 다시 구하는 것과 같은 이유다.
	band := o.base.shifted(handicap.BaselineCpFor(startSFEN, pos.Turn.Other()) + skillShift(sk))

	opts := o.options(pos, res.Lines)
	if len(opts) == 0 {
		return res.Best, nil
	}

	// 지금 형세 — **후보의 최솟값이다.** 상대가 무엇을 둬도 플레이어 관점 cp는 이보다
	// 낮아지지 않으므로(수를 두는 쪽이 상대다) 이 값이 곧 「상대가 가장 잘 뒀을 때」다.
	//
	// 뿌리 점수(`res.ScoreCp`)를 쓰지 않는다 — 같은 값이지만 저쪽은 **거른 뒤**를 모른다.
	// 던지는 수가 최선이었던 국면에서 기준점이 우리가 두지 않을 수의 것이 된다.
	now := opts[0].playerCp
	for _, opt := range opts[1:] {
		now = min(now, opt.playerCp)
	}

	// 난이도보다 안전이 먼저다. 밴드에 맞는 수가 이것밖에 없어도 최선수보다 600cp 넘게
	// 나쁜 후보는 두지 않는다. HangsPiece 는 「움직인 駒가 잡히는가」만 보므로, 잡아야 할
	// 駒를 외면해 큰 손해를 보는 수는 이 평가치 상한이 막는다(journal §81).
	opts = withinConcession(opts, now, MaxConcessionCp)

	// **구간 위에서는 절대 좌표가 뜻을 잃는다.** 「+100~+300을 겨냥한다」가 그 자리에서는
	// 「거기까지 되돌려라」가 되므로, 같은 폭을 지금 형세에 더한 양보 구간으로 읽는다.
	target := band
	if now > band.HiCp {
		target = Band{LoCp: now + band.LoCp, HiCp: now + band.HiCp}
	}
	return closestToBand(opts, target), nil
}

// option 은 상대가 실제로 둘 수 있는 후보 하나다. cp는 **플레이어 관점**으로 뒤집어 든다.
type option struct {
	move     string
	playerCp int
}

// options 는 후보에서 둘 수 있는 것만 남긴다.
//
// **詰み 줄을 뺀다.** `ScoreCp` 가 환산값(`usi.MateCp`)이라 밴드 산수에 섞이면 기준점이
// 판 밖으로 나간다. 빼도 지금까지와 같게 돈다 — 詰み 줄은 어느 쪽이든 밴드에서 가장 먼
// 후보였으므로 뽑히지 않았고, **전부 詰み이면** 후보가 비어 최선수로 물러선다(부르는 쪽).
func (o *adaptiveOpponent) options(pos shogi.Position, lines []usi.SearchLine) []option {
	out := make([]option, 0, len(lines))
	for _, line := range lines {
		if line.Move == "" || line.IsMate {
			continue
		}
		m, err := shogi.ParseUSIMove(line.Move)
		if err != nil {
			continue
		}
		// 「던지지 않는다」. 이 필터는 **엔진이 필요 없다** — 룰 엔진만으로 된다.
		if MoveFeatures(pos, m).HangsPiece() {
			continue
		}
		out = append(out, option{move: line.Move, playerCp: -line.ScoreCp})
	}
	return out
}

// withinConcession 은 최선의 안전한 후보보다 지나치게 나쁜 수를 난이도 선택 전에 뺀다.
// opts 는 HangsPiece 를 이미 거친 뒤라 여기서의 최솟값이 비교 기준이다. 최솟값 자신은 항상
// 남으므로, 입력이 비지 않았다면 결과도 비지 않는다.
func withinConcession(opts []option, bestCp, maxCp int) []option {
	out := make([]option, 0, len(opts))
	for _, opt := range opts {
		if opt.playerCp-bestCp <= maxCp {
			out = append(out, opt)
		}
	}
	return out
}

// closestToBand 는 닫힌 구간 안의 최소 양보를 고른다. 구간 안에 안전한 후보가 없으면
// 경계에 가장 가까운 안전한 수를 고르고, 거리가 같으면 덜 양보하는 쪽을 택한다.
// 밴드를 맞추려고 안전 상한 밖의 후보를 되살리는 경로는 없다.
func closestToBand(opts []option, band Band) string {
	best, bestCp, bestDistance := "", 0, 0
	for _, opt := range opts {
		distance := 0
		switch {
		case opt.playerCp < band.LoCp:
			distance = band.LoCp - opt.playerCp
		case opt.playerCp > band.HiCp:
			distance = opt.playerCp - band.HiCp
		}
		if best == "" || distance < bestDistance || distance == bestDistance && opt.playerCp < bestCp {
			best, bestCp, bestDistance = opt.move, opt.playerCp, distance
		}
	}
	return best
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
// (journal §31).
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
