package game

import (
	"context"
	"fmt"
	"log"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// MateSearcher 는 詰み 탐색이다. **탐색부와 다른 바이너리**라 따로 받는다
// (docs/02-architecture.md §3). nil이면 종반 판정 없이 승률 낙폭만 본다.
type MateSearcher interface {
	SearchMate(ctx context.Context, startSFEN string, moves []string) (usi.MateResult, error)
}

type engineAnalyst struct {
	search Searcher
	mate   MateSearcher
	depth  int
	level  intervene.Level
}

// JudgeDepth 는 개입 판정에 쓰는 탐색 깊이다.
//
// 실측(06-status.md)에서 `depth 10 × k=1`이 최장 144ms, 12가 400ms다. 판정은 정밀도보다
// 속도가 중요하고 **착수 직후에 도는 유일한 탐색**이라 여기를 짧게 잡는다.
const JudgeDepth = 12

// ShallowDepth 는 **초보자의 시야를 모사하는** 깊이다.
//
// 「얕은 이득에 낚임」은 여기서 좋아 보이는데 JudgeDepth 에서 나쁜 수다(01-core.md §3).
// 2인 이유는 捨て駒가 얕게 보면 반드시 손해로 보이기 때문이다 — 그게 捨て駒의 정의이고,
// 그 거울상이 「한 수만 보면 이득」이다.
const ShallowDepth = 2

// NewEngineAnalyst 는 엔진으로 판정하는 Analyst 를 만든다.
//
// mate 가 nil이면 종반 판정을 건너뛴다 — 승률 낙폭은 그대로 돌므로 대국은 된다.
func NewEngineAnalyst(s Searcher, mate MateSearcher, level intervene.Level) Analyst {
	return &engineAnalyst{search: s, mate: mate, depth: JudgeDepth, level: level}
}

func (a *engineAnalyst) Judge(ctx context.Context, startSFEN string, moves []string, _ int) (intervene.Verdict, error) {
	if len(moves) == 0 {
		return intervene.Verdict{}, fmt.Errorf("judge: no move to judge")
	}
	before := moves[:len(moves)-1]

	// 착수 **전** 국면의 최선수. 두는 쪽(=사람) 관점이다.
	best, err := a.search.SearchDepth(ctx, startSFEN, before, a.depth)
	if err != nil {
		return intervene.Verdict{}, fmt.Errorf("judge: search before: %w", err)
	}

	// 착수 **후** 국면. 엔진은 늘 수번 측 관점으로 답하므로 지금은 상대 관점이다.
	after, err := a.search.SearchDepth(ctx, startSFEN, moves, a.depth)
	if err != nil {
		return intervene.Verdict{}, fmt.Errorf("judge: search after: %w", err)
	}

	in := intervene.Input{
		BestCp:  best.ScoreCp,
		AfterCp: -after.ScoreCp, // 사람 관점으로 뒤집는다
		Level:   a.level,
	}

	// 카테고리에 쓸 국면 사실. **판정 자체는 여기에 매이지 않는다** — 못 읽으면
	// Known 이 false로 남고 카테고리만 other 가 된다. 개입은 그대로 걸린다.
	if pos, m, err := replay(startSFEN, moves); err == nil {
		in.Features = MoveFeatures(pos, m)
		// 얕은 평가는 **이미 받아 둔 info 라인**에 있다. PvInterval=0 덕에 depth 12
		// 탐색 한 번이 depth 1~12를 전부 돌려주므로 추가 탐색이 없다(01-core.md §4).
		if cp, ok := after.ScoreAtDepth(ShallowDepth); ok {
			in.Features.ShallowCp, in.Features.HasShallow = -cp, true // 사람 관점
		}
	} else {
		// 판을 못 읽은 것은 우리 버그다. 판정은 계속하되 조용히 넘기지 않는다.
		log.Printf("game: could not replay for features, category will be other: %v", err)
	}

	if a.mate != nil {
		// 착수 **전**에 내가 詰み을 가지고 있었는가. solver 는 수번 측(=사람)의 詰み을
		// 모든 응수에 대해 증명하므로 이 질문에 정확히 답한다.
		if r, err := a.mate.SearchMate(ctx, startSFEN, before); err == nil && r.Found() {
			in.MateBefore = len(r.Moves)
		}
	}

	// 착수 **후**에도 남았는가는 solver 로 물으면 안 된다 — 그 국면의 수번은 상대라
	// `go mate` 가 「상대의 詰み」을 답한다. 내가 알아야 하는 것과 반대다.
	//
	// 대신 이미 구해둔 탐색 결과를 쓴다. 착수 후 국면이 **수번 측에게 불리한 mate**로
	// 나오면(MateIn < 0) 그것이 곧 「상대가 詰まされる」 = 내 詰み이 남았다는 뜻이다.
	if in.MateBefore > 0 && after.IsMate && after.MateIn < 0 {
		in.MateAfter = -after.MateIn
	}

	return intervene.Judge(in), nil
}
