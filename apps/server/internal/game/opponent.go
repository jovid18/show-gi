package game

import (
	"context"

	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// Searcher 는 engineOpponent 가 쓰는 탐색 능력이다. usi.Pool 이 이걸 만족한다.
type Searcher interface {
	SearchDepth(ctx context.Context, startSFEN string, moves []string, depth int) (usi.SearchResult, error)
}

type engineOpponent struct {
	search Searcher
	depth  int
}

// DefaultDepth 는 상대 수를 고를 때의 탐색 깊이다. 실측으로 정한다(journal §130).
//
// 값이 지연이다. depth 14 는 12의 약 네 배다(journal §10 의 k≈10 에서 8.4s 대 2.0s).
// 옮기면 positions 의 기존 행이 전부 무효가 된다(computed_depth, journal §37).
const DefaultDepth = 14

// NewEngineOpponent 는 엔진의 최선수를 그대로 두는 상대를 만든다.
//
// 프로덕션은 이걸 쓰지 않는다. cmd/api 가 배선하는 상대는 NewAdaptiveOpponent
// 하나뿐이고, 이쪽은 기준선으로 테스트만 쓴다.
//
// 깊이로만 탐색한다(go depth, 01-core.md §4). 그래서 ctx 취소의 뜻은 하나뿐이다.
// 버린다(세션 종료·롤백).
func NewEngineOpponent(s Searcher, depth int) Opponent {
	if depth < 1 {
		depth = DefaultDepth
	}
	return &engineOpponent{search: s, depth: depth}
}

// 추정치를 보지 않는다. 기준선이 곧 최선수인 상대라 옮길 밴드가 없다.
func (o *engineOpponent) Choose(ctx context.Context, startSFEN string, moves []string, _ skill.Estimate) (string, error) {
	res, err := o.search.SearchDepth(ctx, startSFEN, moves, o.depth)
	if err != nil {
		return "", err
	}
	return res.Best, nil
}

// ChooseBest 는 Choose 와 같은 수다(BestPlayer). 이 상대는 애초에 조절하지 않지만,
// 인터페이스를 만족시키지 않으면 세션이 다른 경로를 탄다(chooseBest).
func (o *engineOpponent) ChooseBest(ctx context.Context, startSFEN string, moves []string) (string, error) {
	return o.Choose(ctx, startSFEN, moves, skill.Unknown)
}
