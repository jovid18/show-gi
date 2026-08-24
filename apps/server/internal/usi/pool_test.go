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
	p, err := NewPool(size, "sh", nil, "testdata/fakeengine.sh")
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
			results[i], errs[i] = p.SearchDepth(t.Context(), testSFEN, nil, 6)
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
	p, err := NewPool(1, "sh", nil, "testdata/fakeengine.sh")
	if err != nil {
		t.Fatalf("풀 기동 실패: %v", err)
	}
	p.Close()
	p.Close() // 두 번 닫아도 안전해야 한다

	if _, err := p.Acquire(t.Context()); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("ErrPoolClosed 기대, got %v", err)
	}
	if _, err := p.SearchDepth(t.Context(), testSFEN, nil, 6); !errors.Is(err, ErrPoolClosed) {
		t.Fatalf("Search에서 ErrPoolClosed 기대, got %v", err)
	}
}

func TestPoolRejectsBadSize(t *testing.T) {
	if _, err := NewPool(0, "sh", nil, "testdata/fakeengine.sh"); err == nil {
		t.Fatal("size 0 이 통과함")
	}
}

// 없는 엔진으로 풀을 만들면 이미 띄운 프로세스를 남기지 않고 실패해야 한다.
func TestPoolFailsCleanly(t *testing.T) {
	if _, err := NewPool(2, "definitely-not-an-engine-binary", nil); err == nil {
		t.Fatal("없는 바이너리로 풀이 만들어짐")
	}
}

// fakeMetrics 는 풀이 내는 숫자를 그대로 받아 둔다.
type fakeMetrics struct {
	mu     sync.Mutex
	size   int
	waits  []time.Duration
	inUse  int
	peak   int
	deltas int

	borrowers []string
}

func (m *fakeMetrics) SetSize(n int) { m.mu.Lock(); m.size = n; m.mu.Unlock() }

func (m *fakeMetrics) ObserveWait(d time.Duration, borrower string) {
	m.mu.Lock()
	m.waits = append(m.waits, d)
	m.borrowers = append(m.borrowers, borrower)
	m.mu.Unlock()
}

func (m *fakeMetrics) ObserveInUse(delta int) {
	m.mu.Lock()
	m.inUse += delta
	m.deltas++
	m.peak = max(m.peak, m.inUse)
	m.mu.Unlock()
}

// 계측이 붙으면 대기 시간과 점유 수가 나온다. 이게 포화를 읽는 유일한 신호다.
func TestPoolObservesWaitAndUse(t *testing.T) {
	p := newFakePool(t, 1)
	m := &fakeMetrics{}
	p.Observe(m)
	if m.size != 1 {
		t.Fatalf("SetSize 를 안 불렀다: size=%d", m.size)
	}

	// 엔진 하나를 붙들고 있는 동안 다른 탐색이 줄을 선다. 그 대기가 지표에 남아야 한다.
	held, err := p.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := p.SearchDepth(t.Context(), testSFEN, nil, 6); err != nil {
			t.Errorf("탐색 실패: %v", err)
		}
	}()

	time.Sleep(50 * time.Millisecond)
	p.Release(held)
	<-done

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inUse != 0 {
		t.Errorf("다 돌려줬는데 점유가 %d 로 남았다 — 게이지가 샌다", m.inUse)
	}
	if m.peak != 1 {
		t.Errorf("동시 점유 최고가 %d, 풀 크기는 1이다", m.peak)
	}
	if len(m.waits) != 2 {
		t.Fatalf("대기 관측이 %d개 — 안 기다린 것도 0으로 남아야 한다", len(m.waits))
	}
	// 줄을 선 쪽은 붙들고 있던 시간만큼 기다렸다. 둘 중 하나가 그 값이어야 한다.
	if max(m.waits[0], m.waits[1]) < 40*time.Millisecond {
		t.Errorf("대기 시간이 %v — 기다린 것이 안 잡혔다", m.waits)
	}
}

// 계측을 안 붙인 풀은 그대로 돈다. 지표는 대국의 전제가 아니다.
func TestPoolWithoutMetrics(t *testing.T) {
	p := newFakePool(t, 1)
	if _, err := p.SearchDepth(t.Context(), testSFEN, nil, 6); err != nil {
		t.Fatalf("탐색 실패: %v", err)
	}
}

// 이중 Release 는 호출 측 버그이지만 게이지를 망가뜨리면 안 된다. 음수로 굳으면
// 점유가 실제보다 낮게 보여 포화가 안 보인다.
func TestDoubleReleaseKeepsGaugeAtZero(t *testing.T) {
	p := newFakePool(t, 1)
	m := &fakeMetrics{}
	p.Observe(m)

	e, err := p.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	p.Release(e)
	p.Release(e) // 버그. 풀은 이미 꽉 차 있다

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inUse != 0 {
		t.Fatalf("점유=%d — 이중 Release 가 게이지를 옮겼다", m.inUse)
	}
}

// 이름을 안 붙이면 대국이다. 대국 중의 경로가 컨텍스트를 그대로 흘려보내므로
// (세션 goroutine) 기본값이 그 자리를 가리켜야 라벨이 뜻을 갖는다.
func TestBorrowerDefaultsToGame(t *testing.T) {
	if got := BorrowerFrom(context.Background()); got != BorrowerGame {
		t.Errorf("이름 없는 컨텍스트 = %q, want %q", got, BorrowerGame)
	}
	//nolint:staticcheck // nil 컨텍스트로도 안 죽어야 한다. 계측이 부르는 쪽을 못 막는다.
	if got := BorrowerFrom(nil); got != BorrowerGame {
		t.Errorf("nil 컨텍스트 = %q, want %q", got, BorrowerGame)
	}
	ctx := WithBorrower(context.Background(), BorrowerAnalysis)
	if got := BorrowerFrom(ctx); got != BorrowerAnalysis {
		t.Errorf("붙인 이름 = %q, want %q", got, BorrowerAnalysis)
	}
	// 빈 이름은 안 붙인다. 붙이면 라벨 하나가 빈 문자열로 갈려 계열이 늘어난다.
	if got := BorrowerFrom(WithBorrower(ctx, "")); got != BorrowerAnalysis {
		t.Errorf("빈 이름이 앞의 이름을 덮었다: %q", got)
	}
}

// 빌린 쪽의 이름이 계측까지 간다. 풀을 지나면서 잃으면 라벨이 늘 game 으로 보인다.
func TestPoolReportsBorrower(t *testing.T) {
	p := newFakePool(t, 1)
	m := &fakeMetrics{}
	p.Observe(m)

	e, err := p.Acquire(WithBorrower(context.Background(), BorrowerAnalysis))
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	p.Release(e)

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.borrowers) != 1 || m.borrowers[0] != BorrowerAnalysis {
		t.Fatalf("borrowers=%v, want [%s]", m.borrowers, BorrowerAnalysis)
	}
}

// 사람이 기다리는 쪽이 먼저 받는다. 사후 분석이 풀을 다 쓰고 있어도 착수가 그 뒤로
// 밀리지 않는 것이 이 줄의 이유다(journal §106).
func TestAPersonWaitingGetsTheEngineFirst(t *testing.T) {
	p := newFakePool(t, 1)

	held, err := p.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// 분석이 먼저 줄에 선다. 채널이라면 이쪽이 먼저 받는다.
	got := make(chan string, 2)
	analysis := make(chan struct{})
	go func() {
		close(analysis)
		e, err := p.Acquire(WithBorrower(t.Context(), BorrowerAnalysis))
		if err != nil {
			return
		}
		got <- BorrowerAnalysis
		p.Release(e)
	}()
	<-analysis
	waitForWaiters(t, p, 1, 1)

	// 그 뒤에 대국이 선다.
	game := make(chan struct{})
	go func() {
		close(game)
		e, err := p.Acquire(WithBorrower(t.Context(), BorrowerGame))
		if err != nil {
			return
		}
		got <- BorrowerGame
		p.Release(e)
	}()
	<-game
	waitForWaiters(t, p, 0, 1)

	p.Release(held)

	select {
	case first := <-got:
		if first != BorrowerGame {
			t.Errorf("first = %q, want %q", first, BorrowerGame)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("nobody got the engine")
	}
}

// waitForWaiters 는 그 줄에 n 명이 설 때까지 기다린다. goroutine 이 Acquire 안까지
// 들어갔는지를 밖에서 알 방법이 그것뿐이다.
func waitForWaiters(t *testing.T, p *Pool, prio, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		got := len(p.waiting[prio])
		p.mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("waiting[%d] never reached %d", prio, n)
}

// 기다리다 그만둔 사람이 엔진을 들고 사라지지 않는다. 넘겨주는 쪽이 이미 골랐는데
// 받는 쪽이 포기하면 그 엔진은 아무 줄에도 없게 된다.
func TestGivingUpWhileBeingHandedAnEngineDoesNotLoseIt(t *testing.T) {
	p := newFakePool(t, 1)

	held, err := p.Acquire(t.Context())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := p.Acquire(ctx)
		done <- err
	}()
	waitForWaiters(t, p, 0, 1)

	// 포기와 넘겨주기를 같이 일으킨다. 어느 쪽이 이기든 엔진은 풀에 남아야 한다.
	cancel()
	p.Release(held)
	<-done

	// 풀이 온전하면 다시 빌릴 수 있다.
	ctx2, cancel2 := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel2()
	back, err := p.Acquire(ctx2)
	if err != nil {
		t.Fatalf("the engine did not come back: %v", err)
	}
	p.Release(back)
}
