package archive

import (
	"context"
	"errors"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// MateStore 는 詰み 답이 쌓이고 다시 꺼내지는 곳. *store.Store 가 만족한다.
type MateStore interface {
	GetMate(ctx context.Context, sfenKey string) (store.Mate, error)
	PutMate(ctx context.Context, m store.Mate) (bool, error)
}

// MateEngine 은 감싸는 대상이다. *usi.Pool 이 만족한다.
type MateEngine interface {
	SearchMate(ctx context.Context, startSFEN string, moves []string) (usi.MateResult, error)
}

// MateMetrics 는 詰み 탐색 하나를 받는 자리다. 탐색부와 갈라 둔다 —
// 섞으면 engine_search_duration_seconds 가 부하 신호로서의 뜻을 잃는다(journal §106).
type MateMetrics interface {
	// ObserveMateSearch 는 詰み 탐색 하나가 답을 받기까지 걸린 시간이다. 풀 대기가
	// 들어 있다. cached 면 solver 를 안 부른 것이고, proven 이 false 면 그 답을 안 쌓았다.
	ObserveMateSearch(d time.Duration, cached, proven bool)
}

// Mate 는 詰み 탐색에 캐시와 기록을 붙인다.
//
// game.MateSearcher · quiz.MateSearcher 를 한꺼번에 만족한다 — 빌리는 자리가 넷이고
// (종반 판정 · 詰み 게이지 · 되짚기 퀴즈 · 대인전 사후 분석) 그 넷이 같은 하나를 받아야
// 한 자리가 감싸지지 않는 일이 안 생긴다. Searcher 와 같은 판단이다.
//
// 값이 큰 이유는 詰み 없는 국면이 가장 비싼 답이라는 것이다 — 한계까지 다 뒤진 뒤에야
// nomate 로 답하므로, 훑기 구간의 거의 모든 국면이 최악 비용이다(journal §110).
type Mate struct {
	inner MateEngine
	store MateStore

	// plies 는 solver 의 手数 한계다(ENGINE_MATE_PLIES). 캐시를 읽는 조건이자 쓰는 값이라
	// 풀에 준 것과 같아야 한다 — 갈리면 캐시가 자기 답을 못 쓰거나, 더 나쁘게는 얕은
	// 한계의 「없다」를 깊은 한계의 「없다」로 읽는다.
	plies int

	// metrics 는 기동 중에 한 번 달리고 그 뒤로는 읽기만 한다. nil 이면 계측이 꺼진다.
	metrics MateMetrics

	// wg 는 떠 있는 기록들이다. Searcher.wg 와 갈라 둔다 — 종료할 때 둘 다 기다린다.
	wg sync.WaitGroup
}

// WrapMate 는 詰み 탐색에 캐시를 붙인다. st 가 nil이면 그대로 넘긴다 —
// DB가 없어도 대국은 된다는 이 레포의 판단과 같은 자리다(Wrap).
//
// plies 는 풀에 준 DepthLimit 이다. 0 이하면 캐시가 꺼진다 — 한계를 모르면 쌓인 답을
// 쓸 수 있는지 판단할 수 없고, 모를 때는 안 쓰는 쪽이 이 레포의 규칙이다.
func WrapMate(inner MateEngine, st MateStore, plies int) *Mate {
	if plies <= 0 {
		st = nil
	}
	return &Mate{inner: inner, store: st, plies: plies}
}

// Observe 는 계측을 붙인다. 기동 중에 한 번만 부른다 — Searcher.Observe 와 같은 이유다.
func (a *Mate) Observe(m MateMetrics) { a.metrics = m }

func (a *Mate) observe(start time.Time, cached, proven bool) {
	if a.metrics == nil {
		return
	}
	a.metrics.ObserveMateSearch(time.Since(start), cached, proven)
}

// Wait 은 떠 있는 기록이 끝날 때까지 기다린다. 종료 순서에서 부른다.
func (a *Mate) Wait() { a.wg.Wait() }

// SearchMate 는 이미 증명된 국면이면 solver 를 안 부른다.
//
// 게이지와 판정이 같은 질문을 한 手 간격으로 두 번 하는 자리가 여기서 한 번이 된다 —
// 게이지는 사람 차례 국면을 묻고(game 의 maybeGauge), 판정은 그 사람이 둔 뒤 착수 전
// 국면을 묻는데 그 둘이 같은 국면이다. 퀴즈는 판이 끝난 뒤 그 전부를 다시 묻는다.
func (a *Mate) SearchMate(ctx context.Context, startSFEN string, moves []string) (usi.MateResult, error) {
	start := time.Now()

	// 국면을 못 되만들면 캐시를 아예 안 쓴다. 그 위에 답을 쌓거나 꺼내면 없던 국면을
	// 다루게 된다 — Searcher 가 positionAfter 를 같은 이유로 지나간다.
	//
	// 캐시가 꺼져 있으면 되만들지도 않는다. 100手째의 판정이 그 수순을 전부 다시 두는
	// 것이고, 쓸 데가 없으면 그것이 그대로 낭비다 — CPU 가 벽인 박스다(journal §110).
	var pos shogi.Position
	usable := false
	if a.store != nil {
		p, err := positionAfter(startSFEN, moves)
		if err == nil {
			pos, usable = p, true
			if hit, ok := a.lookup(ctx, pos); ok {
				a.observe(start, true, true)
				return hit, nil
			}
		}
	}

	res, err := a.inner.SearchMate(ctx, startSFEN, moves)
	if err != nil {
		return res, err
	}
	a.observe(start, false, res.Proven)

	// 증명된 것만 쌓는다. timeout 은 「이 한계 안에서는 모른다」이지 「없다」가 아니라서,
	// 없다고 저장하면 있는 詰み을 놓친 채 종반 판정이 돈다(01-core.md §2).
	if !usable || !res.Proven {
		return res, nil
	}
	key, line := Key(pos), slices.Clone(res.Moves)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.record(key, line)
	}()
	return res, nil
}

// lookup 은 쌓인 답을 쓸 수 있으면 돌려준다. 못 쓰면 ok=false.
//
// 조건이 둘이다. 어느 하나를 빠뜨리면 「모른다」나 「이 한계 밖의 詰み」이 「증명된 답」이
// 되어 나가고, 그건 에러 없이 조용히 틀린 판정을 만든다.
func (a *Mate) lookup(ctx context.Context, pos shogi.Position) (usi.MateResult, bool) {
	m, err := a.store.GetMate(ctx, Key(pos))
	if err != nil {
		if !errors.Is(err, store.ErrNoMate) {
			log.Printf("archive: read mate: %v", err)
		}
		return usi.MateResult{}, false
	}
	// ① 얕은 한계의 답은 못 쓴다. 한계 9의 「詰み이 없다」는 한계 11에서 참이 아니다.
	if m.DepthLimit < a.plies {
		return usi.MateResult{}, false
	}
	// ② 쌓인 詰み이 이 한계 밖이면 못 쓴다. 한계 15가 찾은 13手詰을 한계 11로 물은 쪽에
	// 돌려주면 그 한계로는 증명되지 않은 답을 주는 것이다.
	if len(m.Moves) > a.plies {
		return usi.MateResult{}, false
	}
	// 쌓인 것은 증명된 것뿐이다. 비어 있으면 증명된 「詰み이 없다」이고, 그 구별이
	// Proven 을 두는 이유다 — 부르는 쪽이 「모른다」와 가른다(quiz 의 distance).
	return usi.MateResult{Moves: m.Moves, Proven: true}, true
}

// record 는 증명된 답 하나를 남긴다.
//
// 실패해도 대국에 영향이 없다. 여기서 나는 에러는 전부 로그로 끝난다 —
// Searcher.record 와 같은 자리다.
func (a *Mate) record(key string, moves []string) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	if _, err := a.store.PutMate(ctx, store.Mate{
		SFENKey:    key,
		DepthLimit: a.plies,
		Moves:      moves,
	}); err != nil {
		log.Printf("archive: put mate %s: %v", key, err)
	}
}
