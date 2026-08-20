package archive

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 이 패키지는 DB도 엔진도 없이 전부 확인된다 — 감싸는 층이라 양쪽이 인터페이스다.
// 실제 DB에 들어가는지는 internal/store 의 테스트가 본다.

type fakeEngine struct {
	res   usi.SearchResult
	err   error
	calls int
	last  struct {
		startSFEN string
		moves     []string
		depth     int
		multiPV   int
	}
}

func (f *fakeEngine) SearchMultiPV(
	_ context.Context, startSFEN string, moves []string, depth, multiPV int,
) (usi.SearchResult, error) {
	f.calls++
	f.last.startSFEN, f.last.moves, f.last.depth, f.last.multiPV = startSFEN, moves, depth, multiPV
	return f.res, f.err
}

// fakeStore 는 쌓인 것을 그대로 들고 있는다. 기록이 goroutine에서 도므로 잠근다.
type fakeStore struct {
	mu        sync.Mutex
	positions map[string]store.Position
	edges     []store.Edge
	putErr    error
}

func newStore() *fakeStore {
	return &fakeStore{positions: map[string]store.Position{}}
}

func (s *fakeStore) GetPosition(_ context.Context, key string) (store.Position, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.positions[key]
	if !ok {
		return store.Position{}, store.ErrNoPosition
	}
	return p, nil
}

func (s *fakeStore) Edges(_ context.Context, parentKey string) ([]store.Edge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.Edge
	for _, e := range s.edges {
		if e.ParentKey == parentKey {
			out = append(out, e)
		}
	}
	return out, nil
}

func (s *fakeStore) PutPosition(_ context.Context, p store.Position) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.putErr != nil {
		return false, s.putErr
	}
	// 얕은 것이 깊은 것을 덮지 않는다 — 질의가 하는 일을 여기서도 흉내낸다.
	if old, ok := s.positions[p.SFENKey]; ok {
		if p.ComputedDepth < old.ComputedDepth ||
			(p.ComputedDepth == old.ComputedDepth && len(p.Candidates) <= len(old.Candidates)) {
			return false, nil
		}
	}
	s.positions[p.SFENKey] = p
	return true, nil
}

func (s *fakeStore) PutEdge(_ context.Context, e store.Edge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.edges = append(s.edges, e)
	return nil
}

func (s *fakeStore) edgeFor(usiMove string) (store.Edge, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.edges {
		if e.USI == usiMove {
			return e, true
		}
	}
	return store.Edge{}, false
}

func (s *fakeStore) rows() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.positions)
}

// 깊이별 라인이 붙은 탐색 결과. PvInterval=0 이라 엔진이 실제로 이렇게 준다.
func result(depth int, moves ...string) usi.SearchResult {
	res := usi.SearchResult{Depth: depth, Best: moves[0], ScoreCp: 100}
	for i, m := range moves {
		cp := 100 - i*40
		res.Lines = append(res.Lines, usi.SearchLine{Depth: depth, MultiPV: i + 1, Move: m, ScoreCp: cp})
		// 얕은 깊이의 값도 함께 온다. 이것이 edges.eval_by_depth 가 되는 원본이다.
		for d := 1; d <= depth; d++ {
			res.History = append(res.History, usi.SearchLine{Depth: d, MultiPV: i + 1, Move: m, ScoreCp: cp - (depth - d)})
		}
	}
	return res
}

// 탐색 하나가 국면과 그 국면의 후보들을 남긴다. 부르는 쪽은 감싼 줄도 모른다.
func TestRecordsThePositionAndItsCandidates(t *testing.T) {
	st := newStore()
	eng := &fakeEngine{res: result(12, "7g7f", "2g2f")}
	a := Wrap(eng, st)

	if _, err := a.SearchMultiPV(t.Context(), shogi.StartSFEN, nil, 12, 3); err != nil {
		t.Fatalf("SearchMultiPV: %v", err)
	}
	a.Wait()

	start, err := shogi.ParseSFEN(shogi.StartSFEN)
	if err != nil {
		t.Fatalf("ParseSFEN: %v", err)
	}
	p, err := st.GetPosition(t.Context(), Key(start))
	if err != nil {
		t.Fatalf("국면이 안 쌓였다: %v", err)
	}
	if p.ComputedDepth != 12 || p.SideToMove != "b" {
		t.Errorf("position = %+v", p)
	}
	if len(p.Candidates) != 2 || p.Candidates[0].USI != "7g7f" || p.Candidates[0].Cp != 100 {
		t.Fatalf("candidates = %+v", p.Candidates)
	}

	// 후보마다 깊이별 평가치가 붙는다. 추가 탐색이 없다 — 한 번의 depth 12 탐색이
	// 1..12를 전부 돌려준다.
	e, ok := st.edgeFor("2g2f")
	if !ok {
		t.Fatal("후보의 간선이 안 쌓였다")
	}
	if len(e.EvalByDepth) != 12 {
		t.Fatalf("evalByDepth = %v", e.EvalByDepth)
	}
	if e.EvalByDepth[11] != 60 {
		t.Errorf("가장 깊은 값 = %d, want 60", e.EvalByDepth[11])
	}
}

// eval_by_depth 는 先手 관점이다(001_init.sql). 後手 차례의 국면에서 뒤집지 않으면
// 색이 다른 두 판을 나란히 못 놓는다 — 이 컬럼이 있는 이유가 그것이다.
func TestFlipsEvalToSentePointOfView(t *testing.T) {
	st := newStore()
	// 1手 뒤는 後手 차례다. 엔진은 後手에게 +100이라고 답한다.
	eng := &fakeEngine{res: result(4, "3c3d")}
	a := Wrap(eng, st)

	if _, err := a.SearchMultiPV(t.Context(), shogi.StartSFEN, []string{"7g7f"}, 4, 1); err != nil {
		t.Fatalf("SearchMultiPV: %v", err)
	}
	a.Wait()

	e, ok := st.edgeFor("3c3d")
	if !ok {
		t.Fatal("간선이 안 쌓였다")
	}
	if e.EvalByDepth[3] != -100 {
		t.Errorf("후手 +100 이 先手 관점 %d 으로 쌓였다, want -100", e.EvalByDepth[3])
	}
}

// 그 국면에 오게 한 수가 도착 국면과 함께 남는다. 이것이 A→B다.
func TestLinksThePlayedMove(t *testing.T) {
	st := newStore()
	eng := &fakeEngine{res: result(8, "3c3d")}
	a := Wrap(eng, st)

	if _, err := a.SearchMultiPV(t.Context(), shogi.StartSFEN, []string{"7g7f"}, 8, 1); err != nil {
		t.Fatalf("SearchMultiPV: %v", err)
	}
	a.Wait()

	start, err := shogi.ParseSFEN(shogi.StartSFEN)
	if err != nil {
		t.Fatalf("ParseSFEN: %v", err)
	}
	m, err := shogi.ParseUSIMove("7g7f")
	if err != nil {
		t.Fatalf("ParseUSIMove: %v", err)
	}
	after := start.Apply(m)

	e, ok := st.edgeFor("7g7f")
	if !ok {
		t.Fatal("둔 수의 간선이 안 쌓였다")
	}
	if e.ParentKey != Key(start) || e.ChildKey != Key(after) {
		t.Errorf("edge = %+v", e)
	}
	// 부모 국면도 자리가 만들어진다 — 없으면 FK가 간선을 거절한다.
	if st.rows() != 2 {
		t.Errorf("국면 %d개, want 2 (부모와 자식)", st.rows())
	}
	// 부모는 아직 안 재 본 자리다. 후보를 아는 척하지 않는다.
	p, err := st.GetPosition(t.Context(), Key(start))
	if err != nil {
		t.Fatalf("부모 국면: %v", err)
	}
	if p.ComputedDepth != 0 || len(p.Candidates) != 0 {
		t.Errorf("부모 = %+v, want 빈 자리", p)
	}
}

// 모르면 이름을 붙이지 않는다. 手筋의 절반은 엔진이 정하는데(§34) 부모의 평가치를
// 아직 모르면 그 판단을 할 수 없다 — 룰만으로 통과시키면 지운 오판이 돌아온다.
func TestNoNamesWithoutTheParentEval(t *testing.T) {
	st := newStore()
	eng := &fakeEngine{res: result(8, "3c3d")}
	a := Wrap(eng, st)

	if _, err := a.SearchMultiPV(t.Context(), shogi.StartSFEN, []string{"7g7f"}, 8, 1); err != nil {
		t.Fatalf("SearchMultiPV: %v", err)
	}
	a.Wait()

	e, ok := st.edgeFor("7g7f")
	if !ok {
		t.Fatal("간선이 안 쌓였다")
	}
	if len(e.Tags) != 0 {
		t.Errorf("tags = %v, want 없음", e.Tags)
	}
}

// DB가 없으면 아무것도 안 쌓고 그대로 넘긴다. 엔진 결과는 그대로 와야 한다.
func TestPassesThroughWithoutStore(t *testing.T) {
	eng := &fakeEngine{res: result(6, "7g7f")}
	a := Wrap(eng, nil)

	res, err := a.SearchDepth(t.Context(), shogi.StartSFEN, nil, 6)
	if err != nil {
		t.Fatalf("SearchDepth: %v", err)
	}
	if res.Best != "7g7f" {
		t.Errorf("best = %q", res.Best)
	}
	// SearchDepth 는 후보 하나짜리 탐색이다.
	if eng.last.multiPV != 1 || eng.last.depth != 6 {
		t.Errorf("engine 에 k=%d depth=%d 로 갔다", eng.last.multiPV, eng.last.depth)
	}
}

// 탐색이 실패하면 아무것도 쌓지 않는다. 없는 분석을 남기는 것이 더 나쁘다.
func TestRecordsNothingOnSearchFailure(t *testing.T) {
	st := newStore()
	eng := &fakeEngine{err: errors.New("engine died")}
	a := Wrap(eng, st)

	if _, err := a.SearchMultiPV(t.Context(), shogi.StartSFEN, nil, 12, 3); err == nil {
		t.Fatal("에러가 그대로 와야 한다")
	}
	a.Wait()
	if st.rows() != 0 {
		t.Errorf("국면 %d개가 쌓였다", st.rows())
	}
}

// 기록이 실패해도 탐색은 성공이다. 분석을 못 남긴 것과 대국이 서지 않는 것의 값이 다르다.
func TestSearchSucceedsWhenWritingFails(t *testing.T) {
	st := newStore()
	st.putErr = errors.New("database is down")
	eng := &fakeEngine{res: result(6, "7g7f")}
	a := Wrap(eng, st)

	if _, err := a.SearchMultiPV(t.Context(), shogi.StartSFEN, nil, 6, 1); err != nil {
		t.Fatalf("SearchMultiPV: %v", err)
	}
	a.Wait()
}

// 키는 手数를 뺀 SFEN이다. 전치(다른 수순으로 같은 국면)가 한 행으로 합쳐진다.
func TestKeyDropsTheMoveNumber(t *testing.T) {
	start, err := shogi.ParseSFEN(shogi.StartSFEN)
	if err != nil {
		t.Fatalf("ParseSFEN: %v", err)
	}
	key := Key(start)
	if strings.HasSuffix(key, " 1") || len(strings.Fields(key)) != 3 {
		t.Errorf("key = %q, want 手数 없는 세 칸", key)
	}

	// 7六歩·3四歩을 순서만 바꿔 두면 같은 국면이고, 키도 같아야 한다.
	one := play(t, start, "7g7f", "3c3d")
	two := play(t, start, "7g7f", "3c3d")
	if Key(one) != Key(two) {
		t.Errorf("같은 국면이 다른 키다:\n%s\n%s", Key(one), Key(two))
	}
}

func play(t *testing.T, pos shogi.Position, usis ...string) shogi.Position {
	t.Helper()
	for _, u := range usis {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			t.Fatalf("ParseUSIMove(%q): %v", u, err)
		}
		pos = pos.Apply(m)
	}
	return pos
}

// 이미 잰 국면은 엔진을 안 부른다. 여기가 §12의 캐시를 실제로 쓰는 자리다.
//
// 그리고 깊이별 값이 함께 살아나야 한다 — 개입 판정이 보는 얕은 값이 그것이고, 캐시가
// 그걸 빠뜨리면 「얕은 이득에 낚임」 카테고리가 조용히 사라진다(01-core.md §3).
func TestServesFromTheCache(t *testing.T) {
	st := newStore()
	eng := &fakeEngine{res: result(12, "7g7f", "2g2f", "6g6f")}
	a := Wrap(eng, st)

	first, err := a.SearchMultiPV(t.Context(), shogi.StartSFEN, nil, 12, 3)
	if err != nil {
		t.Fatalf("첫 탐색: %v", err)
	}
	a.Wait()

	second, err := a.SearchMultiPV(t.Context(), shogi.StartSFEN, nil, 12, 3)
	if err != nil {
		t.Fatalf("두 번째: %v", err)
	}
	if eng.calls != 1 {
		t.Fatalf("엔진을 %d번 불렀다, want 1", eng.calls)
	}

	if second.Best != first.Best || second.ScoreCp != first.ScoreCp || second.Depth != first.Depth {
		t.Errorf("best/score/depth 가 갈렸다: %+v vs %+v", second, first)
	}
	if len(second.Lines) != 3 {
		t.Fatalf("lines = %d, want 3", len(second.Lines))
	}

	// 얕은 값이 살아 있어야 한다. 없으면 판정이 그 축을 잃는다.
	want, ok := first.ScoreAtDepth(2)
	if !ok {
		t.Fatal("첫 결과에 depth 2 가 없다 — 테스트가 틀렸다")
	}
	got, ok := second.ScoreAtDepth(2)
	if !ok {
		t.Fatal("캐시에서 온 결과에 depth 2 가 없다")
	}
	if got != want {
		t.Errorf("depth 2 = %d, want %d", got, want)
	}
}

// 캐시가 수번 관점으로 돌아와야 한다. 저장은 先手 관점이라, 되돌리는 것을 빠뜨리면
// 後手로 잡은 판에서만 부호가 뒤집히고 에러는 안 난다.
func TestCacheKeepsTheMoverPointOfView(t *testing.T) {
	st := newStore()
	// 1手 뒤는 後手 차례다. 엔진은 後手에게 +100이라고 답한다.
	eng := &fakeEngine{res: result(6, "3c3d")}
	a := Wrap(eng, st)

	first, err := a.SearchMultiPV(t.Context(), shogi.StartSFEN, []string{"7g7f"}, 6, 1)
	if err != nil {
		t.Fatalf("첫 탐색: %v", err)
	}
	a.Wait()

	second, err := a.SearchMultiPV(t.Context(), shogi.StartSFEN, []string{"7g7f"}, 6, 1)
	if err != nil {
		t.Fatalf("두 번째: %v", err)
	}
	if eng.calls != 1 {
		t.Fatalf("엔진을 %d번 불렀다, want 1", eng.calls)
	}
	for _, d := range []int{1, 3, 6} {
		want, _ := first.ScoreAtDepth(d)
		got, ok := second.ScoreAtDepth(d)
		if !ok || got != want {
			t.Errorf("depth %d = %d(ok=%v), want %d", d, got, ok, want)
		}
	}
}

// 모자란 캐시는 안 쓴다. 얕게 잰 행과 후보가 적은 행이 그렇다 — 뒤엣것을 안 막으면
// k=1로 쓰인 행이 k=10을 원하는 적응형 상대에게 후보 하나만 주고, 그건 강함 조절이 꺼진 것이다.
func TestIgnoresInsufficientCache(t *testing.T) {
	for name, ask := range map[string][2]int{
		"더 깊이 원한다":  {14, 1},
		"후보를 더 원한다": {6, 3},
	} {
		st := newStore()
		eng := &fakeEngine{res: result(6, "7g7f")}
		a := Wrap(eng, st)

		if _, err := a.SearchMultiPV(t.Context(), shogi.StartSFEN, nil, 6, 1); err != nil {
			t.Fatalf("%s: 첫 탐색: %v", name, err)
		}
		a.Wait()

		if _, err := a.SearchMultiPV(t.Context(), shogi.StartSFEN, nil, ask[0], ask[1]); err != nil {
			t.Fatalf("%s: 두 번째: %v", name, err)
		}
		if eng.calls != 2 {
			t.Errorf("%s: 엔진을 %d번 불렀다, want 2 (캐시를 쓰면 안 된다)", name, eng.calls)
		}
	}
}

// 합법수가 k보다 적은 국면은 그것으로 다 찬 것이다. 「모자란다」로 보면 그 자리는
// 영원히 캐시를 못 쓰고, 종반에 k=10을 묻는 상대(§16)가 정확히 거기서 매번 다시 잰다.
func TestServesWhenThereAreFewerLegalMovesThanK(t *testing.T) {
	st := newStore()
	// 玉 둘만 있는 국면. 先手 玉이 1九에 몰려 합법수가 셋이다(9八·8八·8九 방향).
	const cornered = "8k/9/9/9/9/9/9/9/8K b - 1"
	pos, err := shogi.ParseSFEN(cornered)
	if err != nil {
		t.Fatalf("ParseSFEN: %v", err)
	}
	legal := len(pos.LegalMoves())
	if legal >= 10 {
		t.Fatalf("합법수가 %d개다 — 이 테스트가 노리는 국면이 아니다", legal)
	}

	// 엔진은 있는 만큼만 준다 — k=10을 물어도 합법수가 셋이면 세 줄이다.
	eng := &fakeEngine{res: result(8, "1i1h", "1i2h", "1i2i")}
	a := Wrap(eng, st)

	if _, err := a.SearchMultiPV(t.Context(), cornered, nil, 8, 10); err != nil {
		t.Fatalf("첫 탐색: %v", err)
	}
	a.Wait()

	if _, err := a.SearchMultiPV(t.Context(), cornered, nil, 8, 10); err != nil {
		t.Fatalf("두 번째: %v", err)
	}
	if eng.calls != 1 {
		t.Errorf("엔진을 %d번 불렀다, want 1 — 후보가 합법수만큼 있으면 다 찬 것이다", eng.calls)
	}
}

// 飛を振った 수가 전법 태그를 만든다. 수순 없이는 전법이 안 보였던 자리다.
func TestTagsFormationOnTheEdge(t *testing.T) {
	st := newStore()
	const openBoard = "4k4/9/9/9/9/9/9/7R1/4K4 b - 1"
	eng := &fakeEngine{res: result(8, "5a4a")}
	a := Wrap(eng, st)

	if _, err := a.SearchMultiPV(t.Context(), openBoard, []string{"2h5h"}, 8, 1); err != nil {
		t.Fatalf("SearchMultiPV: %v", err)
	}
	a.Wait()

	e, ok := st.edgeFor("2h5h")
	if !ok {
		t.Fatal("간선이 안 쌓였다")
	}
	want := "naka_bisha"
	for _, tag := range e.Tags {
		if tag == want {
			return
		}
	}
	t.Errorf("tags = %v, want %q", e.Tags, want)
}

// 히트에도 오는 길은 남는다. 같은 국면에 다른 수로 도달하면(전치) 그 간선은 새것이다 —
// 안 남기면 그 자리가 영원히 비어 있고, 「A→B를 쌓는다」가 반만 사실이 된다.
func TestLinksThePathEvenOnACacheHit(t *testing.T) {
	st := newStore()
	eng := &fakeEngine{res: result(8, "3c3d")}
	a := Wrap(eng, st)

	// ① 국면을 한 번 잰다(수순 없이) — 이러면 「오게 한 수」가 없다.
	start, err := shogi.ParseSFEN(shogi.StartSFEN)
	if err != nil {
		t.Fatalf("ParseSFEN: %v", err)
	}
	m, err := shogi.ParseUSIMove("7g7f")
	if err != nil {
		t.Fatalf("ParseUSIMove: %v", err)
	}
	after := start.Apply(m)

	if _, err := a.SearchMultiPV(t.Context(), after.SFEN(), nil, 8, 1); err != nil {
		t.Fatalf("첫 탐색: %v", err)
	}
	a.Wait()
	if _, ok := st.edgeFor("7g7f"); ok {
		t.Fatal("아직 그 수로 온 적이 없다")
	}

	// ② 같은 국면을 수순으로 물으면 캐시가 답한다. 그때 오는 길이 남아야 한다.
	if _, err := a.SearchMultiPV(t.Context(), shogi.StartSFEN, []string{"7g7f"}, 8, 1); err != nil {
		t.Fatalf("두 번째: %v", err)
	}
	a.Wait()
	if eng.calls != 1 {
		t.Errorf("엔진을 %d번 불렀다, want 1", eng.calls)
	}

	e, ok := st.edgeFor("7g7f")
	if !ok {
		t.Fatal("히트에서 오는 길이 안 남았다")
	}
	if e.ParentKey != Key(start) || e.ChildKey != Key(after) {
		t.Errorf("edge = %+v", e)
	}
}

// fakeSearchMetrics 는 탐색 계측을 그대로 받아 둔다.
type fakeSearchMetrics struct {
	mu     sync.Mutex
	cached int
	engine int
}

func (m *fakeSearchMetrics) ObserveSearch(_ time.Duration, cached bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cached {
		m.cached++
		return
	}
	m.engine++
}

// 캐시가 답한 것과 엔진을 부른 것이 지표에서 갈려야 한다. 이 비율이 국면 캐시가
// 실제로 일하는지를 말하는 유일한 숫자다.
func TestObservesCacheHitsSeparately(t *testing.T) {
	st := newStore()
	eng := &fakeEngine{res: result(8, "3c3d")}
	a := Wrap(eng, st)
	m := &fakeSearchMetrics{}
	a.Observe(m)

	if _, err := a.SearchMultiPV(t.Context(), shogi.StartSFEN, nil, 8, 1); err != nil {
		t.Fatalf("첫 탐색: %v", err)
	}
	a.Wait()
	// 같은 국면을 다시 물으면 캐시가 답한다.
	if _, err := a.SearchMultiPV(t.Context(), shogi.StartSFEN, nil, 8, 1); err != nil {
		t.Fatalf("두 번째: %v", err)
	}
	a.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.engine != 1 || m.cached != 1 {
		t.Fatalf("엔진 %d회 · 캐시 %d회, want 1·1 (엔진 호출은 %d회)", m.engine, m.cached, eng.calls)
	}
}

// 실패한 탐색은 안 센다. 세션이 끝나 ctx가 닫히는 것이 흔해서, 그걸 세면
// 「탐색 수」가 사람이 판을 떠난 횟수까지 담는다.
func TestDoesNotObserveFailedSearches(t *testing.T) {
	a := Wrap(&fakeEngine{err: errors.New("boom")}, newStore())
	m := &fakeSearchMetrics{}
	a.Observe(m)

	if _, err := a.SearchMultiPV(t.Context(), shogi.StartSFEN, nil, 8, 1); err == nil {
		t.Fatal("에러가 안 나왔다")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.engine != 0 || m.cached != 0 {
		t.Fatalf("실패를 셌다: 엔진 %d · 캐시 %d", m.engine, m.cached)
	}
}
