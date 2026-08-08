package usi

import (
	"context"
	"errors"
	"sync"
)

// ErrPoolClosed 는 닫힌 풀에서 엔진을 빌리려 할 때 나온다.
var ErrPoolClosed = errors.New("usi: pool closed")

// Pool 은 Engine 여러 개를 돌려쓴다.
//
// Engine 하나는 탐색을 mutex로 직렬화한다 — 프로세스 1개 = 동시 탐색 1개.
// 그런데 개입 판정은 사용자가 수를 두기 전에 미리 돌려두는 것이라, 대국 한 판 안에서도
// 동시 탐색이 여러 개 필요하다. Engine을 고치는 대신 여러 개를 두고 빌려준다 —
// 엔진의 직렬성은 프로세스의 성질이지 코드의 결함이 아니다.
//
// **빌린 동안에는 그 Engine을 혼자 쓴다.** 옵션(Skill Level, MultiPV)을 걸어야 하면
// 빌린 뒤 탐색 직전에 건다. 옵션은 Engine에 남아서 다음에 빌리는 쪽이 그대로 물려받으므로,
// 값에 기대는 쪽은 매번 직접 걸어야 한다. 이걸 어기면 A 대국의 강함이 B 대국에 새어 나간다.
type Pool struct {
	free chan *Engine
	all  []*Engine

	done      chan struct{}
	closeOnce sync.Once
}

// NewPool 은 엔진 size개를 띄운다. 하나라도 실패하면 이미 띄운 것을 정리하고 에러를 낸다.
func NewPool(size int, path string, args ...string) (*Pool, error) {
	if size < 1 {
		return nil, errors.New("usi: pool size must be at least 1")
	}
	p := &Pool{
		free: make(chan *Engine, size),
		all:  make([]*Engine, 0, size),
		done: make(chan struct{}),
	}
	for range size {
		e, err := New(path, args...)
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

// Acquire 는 엔진 하나를 빌린다. 빈 게 없으면 ctx가 끝날 때까지 기다린다.
// 빌린 쪽은 반드시 Release 해야 한다.
func (p *Pool) Acquire(ctx context.Context) (*Engine, error) {
	select {
	case <-p.done:
		return nil, ErrPoolClosed
	default:
	}
	select {
	case e := <-p.free:
		return e, nil
	case <-p.done:
		return nil, ErrPoolClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Release 는 빌린 엔진을 돌려준다.
func (p *Pool) Release(e *Engine) {
	if e == nil {
		return
	}
	select {
	case p.free <- e:
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

// Search 는 엔진을 빌려 movetime 탐색을 한 번 돌린다.
func (p *Pool) Search(ctx context.Context, startSFEN string, moves []string, movetimeMs int) (SearchResult, error) {
	var res SearchResult
	err := p.Do(ctx, func(e *Engine) error {
		var err error
		res, err = e.Search(ctx, startSFEN, moves, movetimeMs)
		return err
	})
	return res, err
}

// SearchDepth 는 엔진을 빌려 고정 깊이 탐색을 한 번 돌린다.
func (p *Pool) SearchDepth(ctx context.Context, startSFEN string, moves []string, depth int) (SearchResult, error) {
	var res SearchResult
	err := p.Do(ctx, func(e *Engine) error {
		var err error
		res, err = e.SearchDepth(ctx, startSFEN, moves, depth)
		return err
	})
	return res, err
}

// Close 는 엔진을 전부 종료한다. 빌려나간 엔진도 함께 죽는다 —
// 서버가 내려가는 중이므로 진행 중인 탐색을 기다려줄 이유가 없다.
func (p *Pool) Close() {
	p.closeOnce.Do(func() {
		close(p.done)
		for _, e := range p.all {
			e.Close()
		}
	})
}
