package usi

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrPoolClosed 는 닫힌 풀에서 엔진을 빌리려 할 때 나온다.
var ErrPoolClosed = errors.New("usi: pool closed")

// Pool 은 Engine 여러 개를 돌려쓴다 — Engine 하나는 탐색을 직렬화하는데(프로세스 1개 = 동시 탐색 1개),
// 선행 계산 때문에 대국 한 판에도 동시 탐색이 필요하다.
// 빌린 동안 단독 소유다. 옵션은 Engine에 남으므로 값에 기대는 쪽은 매번 직접 건다(journal §6 ②).
type Pool struct {
	free chan *Engine
	all  []*Engine

	done      chan struct{}
	closeOnce sync.Once

	// metrics 는 기동 중에 한 번 달리고 그 뒤로는 읽기만 한다. nil 이면 계측이 꺼진다.
	metrics Metrics
}

// Metrics 는 풀이 밖으로 내는 숫자를 받는 자리다.
//
// 이 패키지가 지표 표면을 모르게 두려고 인터페이스로 받는다 — 풀의 일은 엔진을
// 빌려주는 것이고, 그 숫자를 어디에 어떤 이름으로 쌓는지는 밖의 판단이다.
type Metrics interface {
	// SetSize 는 풀 크기다. 점유 수만으로는 포화를 못 읽는다.
	SetSize(n int)
	// ObserveWait 는 빌리기까지 기다린 시간이다. 안 기다렸으면 0이 들어간다.
	// borrower 는 누가 빌렸나다(WithBorrower).
	ObserveWait(d time.Duration, borrower string)
	// ObserveInUse 는 빌려 나간 엔진 수의 변화다. +1 과 -1 만 들어간다.
	ObserveInUse(delta int)
}

// NewPool 은 엔진 size개를 띄운다. 하나라도 실패하면 이미 띄운 것을 정리하고 에러를 낸다.
//
// opts 는 엔진마다 핸드셰이크 중에 걸린다(New 참조). 엔진 전체의 설정만 여기 둔다 —
// USI_Hash 는 엔진 하나가 통째로 잡는 메모리라 풀 크기를 곱한 만큼 쓴다.
func NewPool(size int, path string, opts map[string]string, args ...string) (*Pool, error) {
	if size < 1 {
		return nil, errors.New("usi: pool size must be at least 1")
	}
	p := &Pool{
		free: make(chan *Engine, size),
		all:  make([]*Engine, 0, size),
		done: make(chan struct{}),
	}
	for range size {
		e, err := New(path, opts, args...)
		if err != nil {
			p.Close()
			return nil, err
		}
		p.all = append(p.all, e)
		p.free <- e
	}
	return p, nil
}

// Size 는 풀에 있는 엔진 수다.
func (p *Pool) Size() int { return len(p.all) }

// Observe 는 계측을 붙인다. 기동 중에 한 번만 부른다 —
// 탐색이 돌기 시작한 뒤에 부르면 그 필드를 읽는 Acquire 와 경합한다.
func (p *Pool) Observe(m Metrics) {
	p.metrics = m
	m.SetSize(p.Size())
}

// Acquire 는 엔진 하나를 빌린다. 빈 게 없으면 ctx가 끝날 때까지 기다린다.
// 빌린 쪽은 반드시 Release 해야 한다.
func (p *Pool) Acquire(ctx context.Context) (*Engine, error) {
	select {
	case <-p.done:
		return nil, ErrPoolClosed
	default:
	}

	// 빈 게 있어 바로 받은 경우도 0으로 재 둔다. 기다린 것만 재면 백분위가 늘 나쁘게
	// 보이고(대기가 있었던 회차만 표본이 된다) 「대개 안 기다린다」를 말할 수 없다.
	start := time.Now()
	select {
	case e := <-p.free:
		p.borrowed(ctx, start)
		return e, nil
	case <-p.done:
		return nil, ErrPoolClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// borrowed 는 빌려 간 것을 계측에 남긴다.
func (p *Pool) borrowed(ctx context.Context, start time.Time) {
	if p.metrics == nil {
		return
	}
	p.metrics.ObserveWait(time.Since(start), BorrowerFrom(ctx))
	p.metrics.ObserveInUse(1)
}

// borrowerKey 는 빌리는 쪽의 이름을 나르는 컨텍스트 키다.
type borrowerKey struct{}

// 빌리는 쪽의 이름들. engine_pool_wait_seconds 의 borrower 라벨이 된다.
//
// BorrowerGame 이 기본값이다 — 대국 중의 경로가 그것이고, 상대 수·개입 판정·詰み
// 게이지·힌트가 전부 세션에서 곧장 부른다. 나머지는 부르는 자리에서 붙인다.
const (
	BorrowerGame     = "game"
	BorrowerAnalysis = "analysis"
	BorrowerExplore  = "explore"
	BorrowerWhatIf   = "whatif"
	BorrowerQuiz     = "quiz"
)

// WithBorrower 는 이 컨텍스트로 빌리는 쪽의 이름을 정한다.
//
// 인자로 안 받고 컨텍스트로 나르는 이유는 부르는 자리와 빌리는 자리 사이에 탐색부가
// 끼어 있기 때문이다. 이름은 맨 위(핸들러·분석기)에서만 알고, 그 사이의 함수들은
// 누가 왜 부르는지 알 필요가 없다.
func WithBorrower(ctx context.Context, name string) context.Context {
	if name == "" {
		return ctx
	}
	return context.WithValue(ctx, borrowerKey{}, name)
}

// BorrowerFrom 은 그 이름을 되읽는다. 안 붙였으면 BorrowerGame 이다.
func BorrowerFrom(ctx context.Context) string {
	if ctx == nil {
		return BorrowerGame
	}
	if name, ok := ctx.Value(borrowerKey{}).(string); ok && name != "" {
		return name
	}
	return BorrowerGame
}

// Release 는 빌린 엔진을 돌려준다.
func (p *Pool) Release(e *Engine) {
	if e == nil {
		return
	}
	select {
	case p.free <- e:
		// 돌아온 갈래에서만 내린다. select 앞에서 내리면 이중 Release 가 게이지를
		// -1 로 굳힌다 — 아래 default 는 풀이 이미 꽉 찬 자리라 점유가 0인 상태다.
		if p.metrics != nil {
			p.metrics.ObserveInUse(-1)
		}
	default:
		// 빌려준 것보다 많이 돌아올 수는 없다. 여기 오면 호출 측 버그이므로
		// 막지 말고 흘려보낸다 — 여기서 블록되면 원인이 안 보이는 교착이 된다.
	}
}

// Do 는 엔진을 빌려 fn을 돌리고 반드시 돌려준다.
func (p *Pool) Do(ctx context.Context, fn func(*Engine) error) error {
	e, err := p.Acquire(ctx)
	if err != nil {
		return err
	}
	defer p.Release(e)
	return fn(e)
}

// SearchDepth 는 엔진을 빌려 고정 깊이 탐색을 한 번 돌린다. 후보는 1개다.
// 시간 기반 탐색은 이 패키지에 없다 — Engine.SearchDepth 주석 참조.
func (p *Pool) SearchDepth(ctx context.Context, startSFEN string, moves []string, depth int) (SearchResult, error) {
	return p.SearchMultiPV(ctx, startSFEN, moves, depth, 1)
}

// SearchMultiPV 는 상위 multiPV개 후보를 함께 받아온다. MultiPV를 탐색 직전에 매번 건다 —
// 옵션이 Engine에 남아 다음에 빌리는 쪽으로 새기 때문이다. SearchDepth 도 여기를 지나며 1을 명시한다(journal §16).
func (p *Pool) SearchMultiPV(ctx context.Context, startSFEN string, moves []string, depth, multiPV int) (SearchResult, error) {
	if multiPV < 1 {
		multiPV = 1
	}
	var res SearchResult
	err := p.Do(ctx, func(e *Engine) error {
		if err := e.SetMultiPV(multiPV); err != nil {
			return err
		}
		var err error
		res, err = e.SearchDepth(ctx, startSFEN, moves, depth)
		return err
	})
	return res, err
}

// SearchMate 는 엔진을 빌려 詰み 탐색을 한 번 돌린다.
// 詰将棋 solver 로 만든 풀에만 쓴다 — 탐색부는 checkmate 로 답하지 않는다.
func (p *Pool) SearchMate(ctx context.Context, startSFEN string, moves []string) (MateResult, error) {
	var res MateResult
	err := p.Do(ctx, func(e *Engine) error {
		var err error
		res, err = e.SearchMate(ctx, startSFEN, moves)
		return err
	})
	return res, err
}

// Close 는 엔진을 전부 종료한다. 빌려나간 엔진도 함께 죽는다.
// 다만 탐색 중이면 그 탐색이 끝날 때까지 막힌다 — Engine.Close 가 같은 mutex를 잡는다.
// 지금은 모든 탐색이 세션 ctx를 타서 풀린다. context.Background() 로 걸면 종료가 걸린다.
func (p *Pool) Close() {
	p.closeOnce.Do(func() {
		close(p.done)
		for _, e := range p.all {
			e.Close()
		}
	})
}
