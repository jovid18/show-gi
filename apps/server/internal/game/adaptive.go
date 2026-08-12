package game

import (
	"context"
	"fmt"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
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

// CandidateK 는 후보를 몇 개까지 받을지다. **실측으로 정했다**(06-status.md §10).
//
// 밴드 적중이 10에서 멈추고 20은 덮이는 국면이 안 늘면서 비용만 배가 된다.
// k=1은 0/6이다 — 후보가 하나면 밴드 제어라는 것이 성립할 수 없다.
const CandidateK = 10

type adaptiveOpponent struct {
	search MultiSearcher
	depth  int
	k      int
	band   Band
}

// NewAdaptiveOpponent 는 「지지만 던지지 않는」 상대를 만든다.
//
// **약화는 엔진이 아니라 고르는 자리에서 한다.** 엔진이 스스로 실수를 섞으면 고른 수가 얼마나
// 나쁜지를 우리가 모르게 되고, 평가치가 오염되면 밴드 제어와 「180cp 나빴다」가 함께 무너진다
// (01-core.md §6).
func NewAdaptiveOpponent(s MultiSearcher, depth int, band Band) Opponent {
	if depth < 1 {
		depth = DefaultDepth
	}
	if band.LoCp == 0 && band.HiCp == 0 {
		band = DefaultBand
	}
	return &adaptiveOpponent{search: s, depth: depth, k: CandidateK, band: band}
}

func (o *adaptiveOpponent) Choose(ctx context.Context, startSFEN string, moves []string) (string, error) {
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
		if d := o.band.distance(playerCp); bestDist < 0 || d < bestDist {
			best, bestDist = line.Move, d
		}
	}
	return best, nil
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
