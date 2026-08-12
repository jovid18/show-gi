package game

import (
	"context"

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

// DefaultDepth 는 상대 수를 고를 때의 탐색 깊이다. 실측으로 정한다.
const DefaultDepth = 12

// NewEngineOpponent 는 엔진의 최선수를 그대로 두는 상대를 만든다.
//
// **프로덕션은 이걸 쓰지 않는다.** `cmd/api` 가 배선하는 상대는 NewAdaptiveOpponent 하나뿐이고
// (adaptive.go), 이쪽은 「최선수를 그대로 두면 어떻게 되는가」의 기준선으로 테스트만 쓴다.
//
// **깊이로만 탐색한다(go depth). 시간 제한도 중간 절단도 없다** — 이유는 01-core.md §4.
// 그래서 ctx 취소의 의미는 하나뿐이다: **버린다**(세션 종료·롤백).
func NewEngineOpponent(s Searcher, depth int) Opponent {
	if depth < 1 {
		depth = DefaultDepth
	}
	return &engineOpponent{search: s, depth: depth}
}

func (o *engineOpponent) Choose(ctx context.Context, startSFEN string, moves []string) (string, error) {
	res, err := o.search.SearchDepth(ctx, startSFEN, moves, o.depth)
	if err != nil {
		return "", err
	}
	return res.Best, nil
}
