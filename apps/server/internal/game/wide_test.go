package game

import (
	"context"
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/eval"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// wideStub 은 판정이 두 국면을 몇 개의 후보로 물었는지 수순 길이별로 적는다.
// SearchDepth 는 후보 1로 센다.
type wideStub struct {
	mu    sync.Mutex
	asked map[int]int
}

func (s *wideStub) answer(moves []string, k int) usi.SearchResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked[len(moves)] = k
	best := "7g7f"
	if len(moves)%2 == 1 {
		best = "3c3d"
	}
	return usi.SearchResult{Best: best, Depth: JudgeDepth, Score: eval.Cp(0)}
}

func (s *wideStub) SearchDepth(_ context.Context, _ string, moves []string, _ int) (usi.SearchResult, error) {
	return s.answer(moves, 1), nil
}

func (s *wideStub) SearchMultiPV(_ context.Context, _ string, moves []string, _, k int) (usi.SearchResult, error) {
	return s.answer(moves, k), nil
}

// 가져온 기보의 되짚기는 국면마다 후보 셋을 묻는다. 판정이 두 국면을 그 수로 재 두어야
// 그래프의 점을 눌렀을 때 엔진이 다시 돌지 않는다(server.evalOf).
func TestAWideAnalystMeasuresBothPositionsWithTheCandidatesTheReviewAsks(t *testing.T) {
	for name, c := range map[string]struct {
		wide int
		want int
	}{
		"기본":  {0, 1},
		"넓게":  {3, 3},
		"하나로": {1, 1},
	} {
		s := &wideStub{asked: map[int]int{}}
		var a Analyst = NewEngineAnalyst(s, nil, intervene.Beginner)
		if c.wide > 0 {
			a = a.(Widener).Wide(c.wide)
		}
		if _, err := a.Judge(t.Context(), shogi.StartSFEN, []string{"7g7f"}, 1); err != nil {
			t.Fatalf("%s: Judge: %v", name, err)
		}
		for plies, what := range map[int]string{0: "착수 전", 1: "착수 후"} {
			if got := s.asked[plies]; got != c.want {
				t.Errorf("%s: %s 국면의 후보 수 = %d, want %d", name, what, got, c.want)
			}
		}
	}
}

// 복사본이다. 워커 하나가 대인전과 가져온 판을 번갈아 재므로 원본이 넓어지면 대인전까지
// 후보 셋으로 잰다.
func TestWideLeavesTheOriginalAlone(t *testing.T) {
	s := &wideStub{asked: map[int]int{}}
	a := NewEngineAnalyst(s, nil, intervene.Beginner)
	_ = a.(Widener).Wide(3)
	if _, err := a.Judge(t.Context(), shogi.StartSFEN, []string{"7g7f"}, 1); err != nil {
		t.Fatalf("Judge: %v", err)
	}
	if got := s.asked[1]; got != 1 {
		t.Errorf("원본의 착수 후 후보 수 = %d, want 1", got)
	}
}
