package game

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/skill"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// fixedBest 는 언제나 같은 수를 최선수로 답하는 탐색이다. **부르는 힌트가 재는 것은
// 예산과 단계이지 엔진이 아니라서**, 여기서 진짜 탐색을 쓰면 국면마다 답이 갈려
// 무엇을 재는지가 흐려진다.
type fixedBest struct{ best string }

func (f *fixedBest) SearchMultiPV(
	_ context.Context, _ string, _ []string, _, _ int,
) (usi.SearchResult, error) {
	return usi.SearchResult{Best: f.best}, nil
}

// countingRater 는 추정기로 몇 건이 갔는지만 센다. **값은 안 본다** — 이 테스트가 재는 것은
// 「갔는가」이고, 얼마였는가는 skill 패키지가 따로 잰다.
type countingRater struct {
	mu sync.Mutex
	n  int
}

func (r *countingRater) Observe(skill.Move) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
}

// Estimates 는 아무것도 안 보낸다. 닫아 두면 세션이 그 채널을 영원히 안 읽는다.
func (r *countingRater) Estimates() <-chan skill.Estimate { return nil }

func (r *countingRater) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

// askStage 는 힌트를 부르고 **그 단계가 화면에 실릴 때까지** 기다린다.
//
// **「힌트가 있다」로 기다리면 안 된다.** 1단계가 이미 떠 있는 채로 2단계를 부르면 그 조건이
// 그 자리에서 참이라, 답이 오기 전에 다음 줄로 넘어간다 — 실제로 그렇게 통과했다가 전체
// 스위트에서 깨졌다.
func askStage(t *testing.T, s *Session, ch <-chan Snapshot, stage int) Snapshot {
	t.Helper()
	if _, err := s.Hint(t.Context()); err != nil {
		t.Fatalf("Hint(%d단계): %v", stage, err)
	}
	return waitFor(t, ch, func(sn Snapshot) bool {
		if sn.Hint == nil {
			return false
		}
		if stage >= HintStageMax {
			return sn.Hint.USI != ""
		}
		return sn.Hint.Square != "" || sn.Hint.Drop != ""
	}, "힌트 "+strconv.Itoa(stage)+"단계")
}

func hintSession(t *testing.T, rec Recorder) *Session {
	t.Helper()
	return newSession(t, Config{
		Opponent:   &scriptedOpponent{moves: []string{"3c3d", "3d3e", "3e3f"}},
		HumanColor: shogi.Black,
		HintSearch: &fixedBest{best: "7g7f"},
		Recorder:   rec,
	})
}

// 1단계는 **駒만** 짚고 2단계에서 수가 붙는다. 갇힘 힌트와 같은 그림이고(buildHint),
// 갈리는 것은 문을 여는 방식뿐이다.
func TestCalledHintOpensPieceThenMove(t *testing.T) {
	s := hintSession(t, nil)
	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	first := askStage(t, s, ch, 1)
	if first.Hint.Square != "7g" {
		t.Errorf("駒를 안 짚었다: %+v", first.Hint)
	}
	if first.Hint.USI != "" {
		t.Fatalf("1단계에서 수를 알려줬다: %+v", first.Hint)
	}
	if first.HintLeft != HintMaxPerGame-1 {
		t.Errorf("예산이 하나 줄어야 한다: %d", first.HintLeft)
	}

	second := askStage(t, s, ch, 2)
	if second.Hint.USI != "7g7f" {
		t.Errorf("2단계에서 그 수가 나와야 한다: %+v", second.Hint)
	}
	if second.HintLeft != HintMaxPerGame-2 {
		t.Errorf("예산이 둘 줄어야 한다: %d", second.HintLeft)
	}

	// **세 번째는 막힌다.** 그 국면에서 더 줄 것이 없다.
	if _, err := s.Hint(t.Context()); err == nil {
		t.Fatal("같은 국면에서 세 번째가 통과했다")
	}
	if second.CanHint {
		t.Error("답까지 본 국면에서 버튼이 살아 있다")
	}
}

// 예산은 판에 붙는다. 다 쓰면 다른 국면에서도 안 열린다.
func TestCalledHintRunsOutOfBudget(t *testing.T) {
	s := hintSession(t, nil)
	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	// 한 국면에 둘씩, 세 국면이면 여섯이다.
	for _, m := range []string{"7g7f", "2g2f", "1g1f"} {
		for stage := 1; stage <= HintStageMax; stage++ {
			askStage(t, s, ch, stage)
		}
		if _, err := s.Play(t.Context(), m); err != nil {
			t.Fatalf("Play %s: %v", m, err)
		}
		waitFor(t, ch, func(s Snapshot) bool { return s.YourTurn && s.Hint == nil }, "상대 응수")
	}

	if _, err := s.Hint(t.Context()); err != ErrNoHintLeft {
		t.Fatalf("예산을 다 썼는데 %v", err)
	}
}

// **답을 본 국면의 수는 실력 추정에서 빠진다.** 알려준 답을 둔 것이 실력으로 기록되면
// 段級이 부풀고, 그 숫자가 화면에 나간다(06-status.md §78).
func TestHintedMoveIsNotRated(t *testing.T) {
	rater := &countingRater{}
	s := newSession(t, Config{
		Opponent:   &scriptedOpponent{moves: []string{"3c3d", "3d3e"}},
		Analyst:    &fixedAnalyst{},
		HumanColor: shogi.Black,
		HintSearch: &fixedBest{best: "7g7f"},
		Rater:      rater,
	})
	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	// 답까지 본다. **2단계가 실제로 실린 뒤에 둬야 한다** — 그 전에 두면 아직 답을 안 본
	// 것이고, 그때 레이팅에 들어가는 것이 오히려 맞다.
	for stage := 1; stage <= HintStageMax; stage++ {
		askStage(t, s, ch, stage)
	}
	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return s.Ply >= 2 }, "상대 응수")

	if n := rater.count(); n != 0 {
		t.Fatalf("답을 본 수가 추정기로 갔다: %d건", n)
	}

	// 힌트를 안 부른 다음 수는 평소대로 센다 — 규칙이 판 전체로 새면 안 된다.
	if _, err := s.Play(t.Context(), "2g2f"); err != nil {
		t.Fatalf("Play 2: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return s.Ply >= 4 }, "상대 응수 2")
	if n := rater.count(); n != 1 {
		t.Errorf("힌트 없는 수는 세어야 한다: %d건", n)
	}
}

// 알려준 수를 실제로 뒀는지가 기록으로 간다 — 01-core.md §5의 `taken` 이 이것이다.
func TestHintTakenIsRecorded(t *testing.T) {
	rec := &fakeRecorder{}
	s := hintSession(t, rec)
	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	for stage := 1; stage <= HintStageMax; stage++ {
		askStage(t, s, ch, stage)
	}
	// 알려준 것과 **다른** 수를 둔다.
	if _, err := s.Play(t.Context(), "2g2f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return s.Ply >= 2 }, "상대 응수")

	if !contains(rec.all(), "hint-taken false") {
		t.Fatalf("알려준 수를 안 뒀다는 것이 안 남았다: %v", rec.all())
	}
	// 1단계와 2단계가 둘 다 남는다 — 무엇을 알려주려 했는지가 남아야 나중에 셀 수 있다.
	var stages int
	for _, e := range rec.all() {
		if strings.HasPrefix(e, "hinted ") {
			stages++
		}
	}
	if stages != 2 {
		t.Errorf("단계 둘이 남아야 한다: %v", rec.all())
	}
}

// 엔진이 없으면 **버튼이 아예 안 산다.** 눌러도 안 되는 것을 띄우지 않는다.
func TestHintIsOffWithoutASearcher(t *testing.T) {
	s := newSession(t, Config{
		Opponent:   &scriptedOpponent{},
		HumanColor: shogi.Black,
	})
	snap, err := s.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.CanHint {
		t.Error("탐색이 없는데 힌트를 누를 수 있다")
	}
	if _, err := s.Hint(t.Context()); err != ErrNoHint {
		t.Fatalf("거절이 %v", err)
	}
}
