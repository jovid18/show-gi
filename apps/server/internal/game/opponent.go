package game

import (
	"context"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// Searcher 는 engineOpponent 가 쓰는 탐색 능력이다. usi.Pool 이 이걸 만족한다.
type Searcher interface {
	Search(ctx context.Context, startSFEN string, moves []string, movetimeMs int) (usi.SearchResult, error)
}

type engineOpponent struct {
	search   Searcher
	movetime time.Duration
}

// NewEngineOpponent 는 엔진의 최선수를 그대로 두는 상대를 만든다.
//
// **이건 D2용이다.** 최선수를 그대로 두면 초심자는 한 판도 못 이긴다.
// D4에서 적응형 상대(밴드 제어)로 바뀌는데, 그때도 엔진은 최대 강도로 정직하게
// 평가하고 약화는 후보 중에서 고르는 쪽에서만 일어난다 — `Skill Level`은 쓰지 않는다
// (01-core.md §6).
func NewEngineOpponent(s Searcher, movetime time.Duration) Opponent {
	if movetime <= 0 {
		movetime = 500 * time.Millisecond
	}
	return &engineOpponent{search: s, movetime: movetime}
}

func (o *engineOpponent) Choose(ctx context.Context, startSFEN string, moves []string) (string, error) {
	res, err := o.search.Search(ctx, startSFEN, moves, int(o.movetime/time.Millisecond))
	if err != nil {
		return "", err
	}
	return res.Best, nil
}
