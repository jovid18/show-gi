package archive

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 이 패키지는 DB도 엔진도 없이 전부 확인된다 — 감싸는 층이라 양쪽이 인터페이스다.
// 실제 DB에 들어가는지는 `internal/store` 의 테스트가 본다.

type fakeEngine struct {
	res  usi.SearchResult
	err  error
	last struct {
		startSFEN string
		moves     []string
		depth     int
		multiPV   int
	}
}

func (f *fakeEngine) SearchMultiPV(
	_ context.Context, startSFEN string, moves []string, depth, multiPV int,
) (usi.SearchResult, error) {
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

// 깊이별 라인이 붙은 탐색 결과. `PvInterval=0` 이라 엔진이 실제로 이렇게 준다.
func result(depth int, moves ...string) usi.SearchResult {
	res := usi.SearchResult{Depth: depth, Best: moves[0], ScoreCp: 100}
	for i, m := range moves {
		cp := 100 - i*40
		res.Lines = append(res.Lines, usi.SearchLine{Depth: depth, MultiPV: i + 1, Move: m, ScoreCp: cp})
		// 얕은 깊이의 값도 함께 온다. 이것이 `edges.eval_by_depth` 가 되는 원본이다.
		for d := 1; d <= depth; d++ {
			res.History = append(res.History, usi.SearchLine{Depth: d, MultiPV: i + 1, Move: m, ScoreCp: cp - (depth - d)})
		}
	}
	return res
}

// 탐색 하나가 국면과 그 국면의 후보들을 남긴다. **부르는 쪽은 감싼 줄도 모른다.**
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

	// 후보마다 **깊이별 평가치**가 붙는다. 추가 탐색이 없다 — 한 번의 depth 12 탐색이
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

// **`eval_by_depth` 는 先手 관점이다**(001_init.sql). 後手 차례의 국면에서 뒤집지 않으면
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

// 그 국면에 **오게 한 수**가 도착 국면과 함께 남는다. 이것이 A→B다.
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
	// 부모는 **아직 안 재 본 자리**다. 후보를 아는 척하지 않는다.
	p, err := st.GetPosition(t.Context(), Key(start))
	if err != nil {
		t.Fatalf("부모 국면: %v", err)
	}
	if p.ComputedDepth != 0 || len(p.Candidates) != 0 {
		t.Errorf("부모 = %+v, want 빈 자리", p)
	}
}

// **모르면 이름을 붙이지 않는다.** 手筋의 절반은 엔진이 정하는데(§34) 부모의 평가치를
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

// 탐색이 실패하면 아무것도 쌓지 않는다. **없는 분석을 남기는 것이 더 나쁘다.**
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

// 기록이 실패해도 **탐색은 성공이다.** 분석을 못 남긴 것과 대국이 서지 않는 것의 값이 다르다.
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

// 키는 **手数를 뺀 SFEN**이다. 전치(다른 수순으로 같은 국면)가 한 행으로 합쳐진다.
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
