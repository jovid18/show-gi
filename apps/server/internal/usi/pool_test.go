package usi

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func newFakePool(t *testing.T, size int) *Pool {
	t.Helper()
	p, err := NewPool(size, "sh", "testdata/fakeengine.sh")
	if err != nil {
		t.Fatalf("풀 기동 실패: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// 풀의 존재 이유. Engine 하나는 탐색을 직렬화하므로, 동시 탐색은 프로세스 수만큼만 된다.
func TestPoolConcurrentSearches(t *testing.T) {
	p := newFakePool(t, 3)
	if p.Size() != 3 {
		t.Fatalf("Size = %d", p.Size())
	}

	const n = 12
	var wg sync.WaitGroup
	errs := make([]error, n)
	results := make([]SearchResult, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = p.Search(t.Context(), testSFEN, nil, 20)
		}()
	}
	wg.Wait()

	for i := range n {
		if errs[i] != nil {
			t.Fatalf("%d번 탐색 실패: %v", i, errs[i])
		}
		if results[i].Best != "7g7f" || results[i].ScoreCp != 42 {
			t.Fatalf("%d번 결과가 섞임: %+v", i, results[i])
		}
	}
}

// 빈 엔진이 없으면 기다린다. 부른 쪽이 그만두겠다고 하면 그때 돌아온다.
func TestPoolAcquireRespectsContext(t *testing.T) {
	p := newFakePool(t, 1)

	held, err := p.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := p.Acquire(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("대기 만료 기대, got %v", err)
	}

	// 돌려주면 다시 빌릴 수 있다
	p.Release(held)
	got, err := p.Acquire(t.Context())
	if err != nil {
		t.Fatalf("반납 후 Acquire: %v", err)
	}
	p.Release(got)
}

func TestPoolClosed(t *testing.T) {
	p, err := NewPool(1, "sh", "testdata/fakeengine.sh")
	if err != nil {
		t.Fatalf("풀 기동 실패: %v", err)
	}
	p.Close()
	p.Close() // 두 번 닫아도 안전해야 한다

	if _, err := p.Acquire(t.Context()); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("ErrPoolClosed 기대, got %v", err)
	}
	if _, err := p.Search(t.Context(), testSFEN, nil, 20); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("Search에서 ErrPoolClosed 기대, got %v", err)
	}
}

func TestPoolRejectsBadSize(t *testing.T) {
	if _, err := NewPool(0, "sh", "testdata/fakeengine.sh"); err == nil {
		t.Fatal("size 0 이 통과함")
	}
}

// 없는 엔진으로 풀을 만들면 이미 띄운 프로세스를 남기지 않고 실패해야 한다.
func TestPoolFailsCleanly(t *testing.T) {
	if _, err := NewPool(2, "definitely-not-an-engine-binary"); err == nil {
		t.Fatal("없는 바이너리로 풀이 만들어짐")
	}
}
