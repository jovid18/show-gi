package archive

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

const matePlies = 11

type fakeMateEngine struct {
	res   usi.MateResult
	err   error
	calls int
	last  struct {
		startSFEN string
		moves     []string
	}
}

func (f *fakeMateEngine) SearchMate(
	_ context.Context, startSFEN string, moves []string,
) (usi.MateResult, error) {
	f.calls++
	f.last.startSFEN, f.last.moves = startSFEN, moves
	return f.res, f.err
}

// fakeMateStore 는 쌓인 것을 그대로 들고 있는다. 기록이 goroutine에서 도므로 잠근다.
type fakeMateStore struct {
	mu     sync.Mutex
	rows   map[string]store.Mate
	getErr error
	putErr error
	puts   int
}

func newMateStore() *fakeMateStore { return &fakeMateStore{rows: map[string]store.Mate{}} }

func (s *fakeMateStore) GetMate(_ context.Context, key string) (store.Mate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return store.Mate{}, s.getErr
	}
	m, ok := s.rows[key]
	if !ok {
		return store.Mate{}, store.ErrNoMate
	}
	return m, nil
}

func (s *fakeMateStore) PutMate(_ context.Context, m store.Mate) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.putErr != nil {
		return false, s.putErr
	}
	s.puts++
	// 얕은 한계가 깊은 한계를 덮지 않는다 — 질의가 하는 일을 여기서도 흉내낸다.
	if old, ok := s.rows[m.SFENKey]; ok && old.DepthLimit >= m.DepthLimit {
		return false, nil
	}
	s.rows[m.SFENKey] = m
	return true, nil
}

func (s *fakeMateStore) row(key string) (store.Mate, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.rows[key]
	return m, ok
}

func (s *fakeMateStore) putCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.puts
}

// keyOf 는 그 수순 뒤 국면의 캐시 키다. 테스트가 키를 직접 만들지 않는다 —
// 부르는 쪽과 같은 자를 써야 「히트해야 하는데 안 한다」를 잡는다.
func keyOf(t *testing.T, startSFEN string, moves []string) string {
	t.Helper()
	pos, err := positionAfter(startSFEN, moves)
	if err != nil {
		t.Fatalf("positionAfter: %v", err)
	}
	return Key(pos)
}

type mateObs struct {
	mu   sync.Mutex
	seen []string
}

func (o *mateObs) ObserveMateSearch(_ time.Duration, cached, proven bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	switch {
	case cached:
		o.seen = append(o.seen, "cached")
	case !proven:
		o.seen = append(o.seen, "unproven")
	default:
		o.seen = append(o.seen, "computed")
	}
}

func (o *mateObs) results() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.seen...)
}

func TestMateSecondCallHitsTheCache(t *testing.T) {
	eng := &fakeMateEngine{res: usi.MateResult{Moves: []string{"1a1b", "2a2b", "3a3b"}, Proven: true}}
	st := newMateStore()
	obs := &mateObs{}
	a := WrapMate(eng, st, matePlies)
	a.Observe(obs)

	first, err := a.SearchMate(context.Background(), shogi.StartSFEN, nil)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	a.Wait()

	second, err := a.SearchMate(context.Background(), shogi.StartSFEN, nil)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if eng.calls != 1 {
		t.Fatalf("solver 를 %d번 불렀다. 두 번째는 캐시가 답해야 한다", eng.calls)
	}
	if len(second.Moves) != len(first.Moves) || !second.Proven {
		t.Fatalf("캐시가 다른 답을 줬다: %+v (원본 %+v)", second, first)
	}
	if got := obs.results(); len(got) != 2 || got[0] != "computed" || got[1] != "cached" {
		t.Fatalf("계측이 %v 다. computed 다음 cached 여야 한다", got)
	}
}

// 「詰み이 없다」도 캐시한다. 그 답이 가장 비싸다 — 한계까지 다 뒤진 뒤에야 나온다.
func TestMateCachesProvenNoMate(t *testing.T) {
	eng := &fakeMateEngine{res: usi.MateResult{Proven: true}}
	st := newMateStore()
	a := WrapMate(eng, st, matePlies)

	if _, err := a.SearchMate(context.Background(), shogi.StartSFEN, nil); err != nil {
		t.Fatalf("first: %v", err)
	}
	a.Wait()

	got, err := a.SearchMate(context.Background(), shogi.StartSFEN, nil)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if eng.calls != 1 {
		t.Fatalf("solver 를 %d번 불렀다. nomate 도 캐시해야 한다", eng.calls)
	}
	if got.Found() {
		t.Fatalf("詰み이 없다고 답해야 하는데 %v 를 줬다", got.Moves)
	}
	if !got.Proven {
		t.Fatal("캐시에 있는 것은 증명된 것뿐이라 Proven 이어야 한다")
	}
}

// timeout 은 안 쌓는다. 「이 한계 안에서는 모른다」이지 「없다」가 아니다(01-core.md §2).
func TestMateDoesNotCacheUnproven(t *testing.T) {
	eng := &fakeMateEngine{res: usi.MateResult{}} // Proven=false
	st := newMateStore()
	obs := &mateObs{}
	a := WrapMate(eng, st, matePlies)
	a.Observe(obs)

	for range 2 {
		if _, err := a.SearchMate(context.Background(), shogi.StartSFEN, nil); err != nil {
			t.Fatalf("search: %v", err)
		}
		a.Wait()
	}
	if eng.calls != 2 {
		t.Fatalf("solver 를 %d번 불렀다. 모른다는 캐시하면 안 된다", eng.calls)
	}
	if st.putCount() != 0 {
		t.Fatalf("쓰기가 %d번 있었다. 미증명 답은 표에 안 들어간다", st.putCount())
	}
	for _, r := range obs.results() {
		if r != "unproven" {
			t.Fatalf("계측이 %v 다. 전부 unproven 이어야 한다", obs.results())
		}
	}
}

// 얕은 한계의 답은 못 쓴다. 한계 9의 「詰み이 없다」는 한계 11에서 참이 아니다.
func TestMateIgnoresShallowerLimit(t *testing.T) {
	eng := &fakeMateEngine{res: usi.MateResult{Moves: []string{"1a1b"}, Proven: true}}
	st := newMateStore()
	key := keyOf(t, shogi.StartSFEN, nil)
	st.rows[key] = store.Mate{SFENKey: key, DepthLimit: 9} // 한계 9의 nomate

	a := WrapMate(eng, st, matePlies)
	got, err := a.SearchMate(context.Background(), shogi.StartSFEN, nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if eng.calls != 1 {
		t.Fatal("얕은 한계의 행을 그대로 썼다. 다시 물어봐야 한다")
	}
	if !got.Found() {
		t.Fatal("solver 가 준 詰み이 안 나왔다")
	}
	a.Wait()
	if row, _ := st.row(key); row.DepthLimit != matePlies || len(row.Moves) != 1 {
		t.Fatalf("깊은 한계의 답이 얕은 것을 안 덮었다: %+v", row)
	}
}

// 깊은 한계가 찾은 긴 詰み은 얕은 한계로 묻는 쪽에 못 준다 — 그 한계로는 증명이 아니다.
func TestMateIgnoresLineLongerThanLimit(t *testing.T) {
	eng := &fakeMateEngine{res: usi.MateResult{Proven: true}}
	st := newMateStore()
	key := keyOf(t, shogi.StartSFEN, nil)
	st.rows[key] = store.Mate{
		SFENKey:    key,
		DepthLimit: 15,
		Moves:      []string{"1a1b", "2a2b", "3a3b", "4a4b", "5a5b", "6a6b", "7a7b"}, // 7手... 아래 한계는 5
	}

	a := WrapMate(eng, st, 5)
	if _, err := a.SearchMate(context.Background(), shogi.StartSFEN, nil); err != nil {
		t.Fatalf("search: %v", err)
	}
	if eng.calls != 1 {
		t.Fatal("한계 5로 물었는데 7手 詰み을 그대로 돌려줬다")
	}
}

// 한계를 모르면 캐시를 안 쓴다. 쌓인 답이 유효한지 판단할 수 없다.
func TestMateWithoutLimitSkipsTheCache(t *testing.T) {
	eng := &fakeMateEngine{res: usi.MateResult{Moves: []string{"1a1b"}, Proven: true}}
	st := newMateStore()
	a := WrapMate(eng, st, 0)

	for range 2 {
		if _, err := a.SearchMate(context.Background(), shogi.StartSFEN, nil); err != nil {
			t.Fatalf("search: %v", err)
		}
		a.Wait()
	}
	if eng.calls != 2 {
		t.Fatalf("solver 를 %d번 불렀다. 한계를 모르면 캐시가 꺼진다", eng.calls)
	}
	if st.putCount() != 0 {
		t.Fatalf("쓰기가 %d번 있었다. 한계를 모르면 쌓지도 않는다", st.putCount())
	}
}

// 게이지와 판정이 같은 국면을 한 手 간격으로 묻는 그 자리다(journal §110).
// 게이지는 사람 차례 국면을, 판정은 그 수를 둔 뒤 착수 전 국면을 묻는데 그 둘이 같다.
func TestMateGaugeAnswersTheNextJudgement(t *testing.T) {
	eng := &fakeMateEngine{res: usi.MateResult{Proven: true}}
	st := newMateStore()
	a := WrapMate(eng, st, matePlies)

	gauge := []string{"7g7f", "3c3d"} // 사람 차례 국면
	if _, err := a.SearchMate(context.Background(), shogi.StartSFEN, gauge); err != nil {
		t.Fatalf("gauge: %v", err)
	}
	a.Wait()

	// 사람이 한 수 두고, 판정이 착수 전 국면(= 위와 같은 국면)을 묻는다.
	played := append(append([]string(nil), gauge...), "2g2f")
	before := played[:len(played)-1]
	if _, err := a.SearchMate(context.Background(), shogi.StartSFEN, before); err != nil {
		t.Fatalf("judge: %v", err)
	}
	if eng.calls != 1 {
		t.Fatalf("solver 를 %d번 불렀다. 판정이 게이지의 답을 써야 한다", eng.calls)
	}
}

// 다른 수순으로 같은 국면에 오면 같은 행이다 — 키가 手数를 뺀 SFEN 이라 전치가 합쳐진다.
func TestMateSharesRowsAcrossTranspositions(t *testing.T) {
	eng := &fakeMateEngine{res: usi.MateResult{Proven: true}}
	st := newMateStore()
	a := WrapMate(eng, st, matePlies)

	if _, err := a.SearchMate(context.Background(), shogi.StartSFEN, []string{"7g7f", "3c3d", "2g2f"}); err != nil {
		t.Fatalf("first: %v", err)
	}
	a.Wait()

	// 같은 국면, 다른 순서.
	if _, err := a.SearchMate(context.Background(), shogi.StartSFEN, []string{"2g2f", "3c3d", "7g7f"}); err != nil {
		t.Fatalf("second: %v", err)
	}
	if eng.calls != 1 {
		t.Fatalf("solver 를 %d번 불렀다. 전치는 같은 행이어야 한다", eng.calls)
	}
}

// 퀴즈는 국면을 SFEN 으로 직접 넘긴다(quiz.MateSearcher). 그 경로도 같은 키여야 한다 —
// 아니면 퀴즈가 게이지·판정이 쌓아 둔 것을 한 건도 못 쓴다.
func TestMateSFENPathSharesRowsWithMovePath(t *testing.T) {
	eng := &fakeMateEngine{res: usi.MateResult{Proven: true}}
	st := newMateStore()
	a := WrapMate(eng, st, matePlies)

	moves := []string{"7g7f", "3c3d"}
	if _, err := a.SearchMate(context.Background(), shogi.StartSFEN, moves); err != nil {
		t.Fatalf("move path: %v", err)
	}
	a.Wait()

	pos, err := positionAfter(shogi.StartSFEN, moves)
	if err != nil {
		t.Fatalf("positionAfter: %v", err)
	}
	if _, err := a.SearchMate(context.Background(), pos.SFEN(), nil); err != nil {
		t.Fatalf("sfen path: %v", err)
	}
	if eng.calls != 1 {
		t.Fatalf("solver 를 %d번 불렀다. 퀴즈 경로가 같은 행을 써야 한다", eng.calls)
	}
}

// DB가 없어도 그대로 돈다. 대국이 본체이고 캐시는 부가라는 이 레포의 판단이다.
func TestMateWithoutStore(t *testing.T) {
	eng := &fakeMateEngine{res: usi.MateResult{Moves: []string{"1a1b"}, Proven: true}}
	a := WrapMate(eng, nil, matePlies)

	got, err := a.SearchMate(context.Background(), shogi.StartSFEN, nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !got.Found() || eng.calls != 1 {
		t.Fatalf("그대로 넘기지 않았다: %+v, calls=%d", got, eng.calls)
	}
	a.Wait()
}

// 읽기가 실패해도 대국은 돈다 — solver 에게 다시 묻는다.
func TestMateFallsBackWhenReadFails(t *testing.T) {
	eng := &fakeMateEngine{res: usi.MateResult{Moves: []string{"1a1b"}, Proven: true}}
	st := newMateStore()
	st.getErr = errors.New("boom")
	a := WrapMate(eng, st, matePlies)

	got, err := a.SearchMate(context.Background(), shogi.StartSFEN, nil)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !got.Found() || eng.calls != 1 {
		t.Fatalf("읽기 실패에서 solver 로 안 넘어갔다: %+v, calls=%d", got, eng.calls)
	}
}

// solver 가 실패하면 그 에러가 그대로 간다. 부르는 쪽이 「모른다」로 다루는 자리다.
func TestMatePassesEngineError(t *testing.T) {
	eng := &fakeMateEngine{err: errors.New("engine exited")}
	st := newMateStore()
	a := WrapMate(eng, st, matePlies)

	if _, err := a.SearchMate(context.Background(), shogi.StartSFEN, nil); err == nil {
		t.Fatal("에러가 삼켜졌다")
	}
	a.Wait()
	if st.putCount() != 0 {
		t.Fatal("실패한 탐색이 표에 들어갔다")
	}
}

// 못 되만드는 수순에서는 캐시를 아예 안 쓴다. 없던 국면에 답을 쌓으면 안 된다.
func TestMateSkipsCacheWhenLineIsUnplayable(t *testing.T) {
	eng := &fakeMateEngine{res: usi.MateResult{Proven: true}}
	st := newMateStore()
	a := WrapMate(eng, st, matePlies)

	if _, err := a.SearchMate(context.Background(), shogi.StartSFEN, []string{"9i9a"}); err != nil {
		t.Fatalf("search: %v", err)
	}
	a.Wait()
	if eng.calls != 1 {
		t.Fatalf("solver 를 %d번 불렀다. 그대로 넘겨야 한다", eng.calls)
	}
	if st.putCount() != 0 {
		t.Fatalf("쓰기가 %d번 있었다. 없던 국면은 안 쌓는다", st.putCount())
	}
}
