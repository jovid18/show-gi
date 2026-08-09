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
// **깊이로 탐색한다. 시간 제한을 쓰지 않는다.**
//
// 시간 기반(`go movetime`)은 같은 국면이 같은 답을 주지 않는다. 그러면 셋이 한꺼번에
// 무너진다 — positions 캐시가 "같은 국면 = 같은 결과"를 못 쓰고, 후보들의 평가치가
// 머신 부하에 따라 흔들려 D4의 밴드 제어가 의미를 잃고, 상대의 강함이 서버 사정에
// 따라 달라진다. 강함은 탐색 길이가 아니라 후보 중에서 고르는 쪽에서 나와야 한다.
//
// 중간에 잘라 쓰는 방법도 안 쓴다. **잘린 결과는 depth N 결과가 아니다** —
// `computed_depth = N`으로 캐시에 적으면 거짓말이 되고, 그 위에서 개입 판정이 돈다.
// 지연은 깊이를 줄이는 것(14→12)과 캐시·선행 계산으로 잡는다.
//
// 그래서 ctx 취소의 의미도 하나뿐이다 — **버린다**(세션 종료·롤백).
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
