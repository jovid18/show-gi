package server

import (
	"context"
	"log"
	"sync"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// matchAnalyzer 는 끝난 대인전의 평가치를 **판이 끝난 뒤에** 채운다.
//
// 두는 동안에는 엔진이 한 번도 안 돈다 — 그것이 이 갈래의 규약이라(`internal/match` 는
// `usi` 를 import 하지 않는다) 대인전 기보에는 `eval_cp` 가 통째로 비고, 되짚기의 평가치
// 궤적이 빈 채로 열린다. 그 자리를 여기서 나중에 메운다.
//
// **기보는 안 기다린다.** 수는 둘 때 이미 들어갔고 여기가 채우는 것은 평가치뿐이다 —
// 판이 끝나자마자 들어온 사람은 기보를 보면서 그래프가 차기를 기다린다.
//
// **워커가 하나다.** 엔진 풀은 지금 두고 있는 사람들과 공유라(01-core.md §4), 여러 판을
// 동시에 분석하면 그만큼 그쪽의 상대 수가 느려진다.
//
// **진행 상태는 메모리다.** 배포하면 하던 분석이 끊기고 그 판은 평가치 없이 남는다 —
// 방과 같은 성질이고(match.Hub), 그때 화면은 「분석 중」이 아니라 「남지 않았다」로 돌아간다.
type matchAnalyzer struct {
	store      *store.Store
	newAnalyst func() game.Analyst

	queue chan []int64

	mu      sync.Mutex
	pending map[int64]struct{}
}

// analysisQueue 는 분석을 기다리는 판을 몇 개까지 쌓아 둘 것인가다. 넘치면 **버린다** —
// 평가치는 있으면 좋은 것이고, 여기서 막으면 판이 끝나는 자리가 같이 막힌다.
const analysisQueue = 64

// newMatchAnalyzer 는 워커를 띄운다. **store 나 analyst 가 없으면 nil 을 준다** — 엔진
// 없는 배포에서 대인전이 그대로 도는 규약을 여기서도 지킨다. nil 인 채로 불려도 되도록
// 아래 메서드가 전부 nil 수신자를 받는다.
func newMatchAnalyzer(ctx context.Context, st *store.Store, newAnalyst func() game.Analyst) *matchAnalyzer {
	if st == nil || newAnalyst == nil {
		return nil
	}
	a := &matchAnalyzer{
		store:      st,
		newAnalyst: newAnalyst,
		queue:      make(chan []int64, analysisQueue),
		pending:    map[int64]struct{}{},
	}
	go a.run(ctx)
	return a
}

func (a *matchAnalyzer) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ids := <-a.queue:
			a.analyze(ctx, ids)
			a.forget(ids)
		}
	}
}

// hold 는 그 판을 **미리** 「분석 중」으로 세운다.
//
// **줄에 세우기 전에 표시해야 한다.** 두 행의 번호는 따로 정해지는데(matchRecords.collect)
// 화면은 자기 번호 하나만 알면 되짚기를 열 수 있다 — 다른 쪽 번호를 기다리는 사이에 열면
// 「분석 중」이 아직 false 라, 그래프가 「남지 않았다」에 굳고 폴링도 안 시작한다.
func (a *matchAnalyzer) hold(id int64) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending[id] = struct{}{}
}

// enqueue 는 끝난 한 판의 두 행을 줄에 세운다. `hold` 로 이미 표시된 것을 받는다.
func (a *matchAnalyzer) enqueue(ids []int64) {
	if a == nil || len(ids) == 0 {
		return
	}
	for _, id := range ids {
		a.hold(id)
	}

	select {
	case a.queue <- ids:
	default:
		a.forget(ids)
		log.Printf("match: analysis queue is full, leaving games %v without evals", ids)
	}
}

// analyzing 은 그 판이 아직 줄에 있거나 도는 중인가다. 되짚기가 이 값으로 「분석 중」과
// 「남지 않았다」를 가른다.
func (a *matchAnalyzer) analyzing(gameID int64) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.pending[gameID]
	return ok
}

func (a *matchAnalyzer) forget(ids []int64) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range ids {
		delete(a.pending, id)
	}
}

// analyze 는 한 판을 처음부터 다시 재서 `eval_cp` 를 채운다.
//
// **한 번 재서 두 행에 쓴다.** `eval_cp` 는 先手 관점이고 관점을 뒤집는 것은 되짚기라
// (review.go), 같은 값이 두 사람의 행에 그대로 들어간다.
//
// **판정(`Judgement.Verdict`)은 버린다.** 개입은 대인전에 없다 — 여기서 개입 행을 쓰면
// 「사람과 둔 판에는 개입이 없다」가 그 자리에서 깨진다.
//
// **`Before` 로 직전 칸을 덮는 것은 일부러다**(kifu/import.go 와 같은 모양). 같은 칸에 두
// 탐색이 쓰고, 그래야 되짚기가 읽는 값이 엔진 대국의 것과 같은 규약이 된다(journal §41).
func (a *matchAnalyzer) analyze(ctx context.Context, ids []int64) {
	rec, err := a.store.GameRecordAnyOwner(ctx, ids[0])
	if err != nil {
		log.Printf("match: cannot read game %d to analyze: %v", ids[0], err)
		return
	}
	moves := make([]string, 0, len(rec.Moves))
	for _, m := range rec.Moves {
		moves = append(moves, m.USI)
	}
	if len(moves) == 0 {
		return
	}

	analyst := a.newAnalyst()
	for ply := 1; ply <= len(moves); ply++ {
		j, err := analyst.Judge(ctx, rec.StartSFEN, moves[:ply], ply)
		if err != nil {
			// **거기서 멈춘다.** 뒤의 수를 재려면 그 앞을 지나야 하고, 엔진이 한 번
			// 못 답한 자리는 대개 다음도 못 답한다. 앞쪽까지는 이미 채워져 있다.
			log.Printf("match: analysis of game %d stopped at ply %d: %v", ids[0], ply, err)
			return
		}
		if !j.HasEvals {
			continue
		}
		a.setEval(ctx, ids, ply, j.SenteCpAfter)
		if ply > 1 {
			a.setEval(ctx, ids, ply-1, j.SenteCpBefore)
		}
	}
}

func (a *matchAnalyzer) setEval(ctx context.Context, ids []int64, ply, senteCp int) {
	for _, id := range ids {
		if err := a.store.SetMoveEval(ctx, id, ply, senteCp); err != nil {
			log.Printf("match: set eval of game %d ply %d: %v", id, ply, err)
		}
	}
}
