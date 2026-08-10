package game

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// fakeExplainer 는 받은 사실을 적어두고 정해진 문장을 돌려준다.
type fakeExplainer struct {
	mu     sync.Mutex
	result explain.Result
	seen   []explain.Facts
}

func (e *fakeExplainer) Explain(_ context.Context, f explain.Facts) explain.Result {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seen = append(e.seen, f)
	return e.result
}

func (e *fakeExplainer) facts() []explain.Facts {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]explain.Facts(nil), e.seen...)
}

func hangsPieceVerdict() intervene.Verdict {
	return intervene.Verdict{
		Kind: intervene.KindBlunder, DeltaWin: 0.42, Category: intervene.CategoryHangsPiece,
	}
}

// 카드의 문장은 **설명 계층에서 온다.** 세션이 문구를 직접 짓지 않는다.
//
// 여기가 끊기면 LLM을 붙여도 화면은 그대로 템플릿을 보여주고, 그 사실이 아무 에러도
// 내지 않는다 — `explain_cache` 에 행은 쌓이고 화면만 안 바뀐다.
func TestInterventionMessageComesFromTheExplainer(t *testing.T) {
	ex := &fakeExplainer{result: explain.Result{
		Body: "その銀を取れる相手の駒が2枚あります。", Tier: 2, CostYen: 0.3, Model: "tiny-jp",
	}}
	an := &fixedAnalyst{
		verdict: hangsPieceVerdict(),
		facts: explain.Facts{
			Kind: intervene.KindBlunder, Category: intervene.CategoryHangsPiece,
			Known: true, MovedPiece: "銀", Attackers: 2,
		},
	}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: an,
		HumanColor: shogi.Black, Explainer: ex,
	})
	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	snap := waitFor(t, ch, func(s Snapshot) bool { return s.Intervention != nil }, "개입")

	if got := snap.Intervention.Message; got != ex.result.Body {
		t.Errorf("카드 문장이 설명 계층의 것이 아니다: %q", got)
	}

	// 판정이 구한 사실이 그대로 건너간다. 여기서 비면 문장이 국면을 못 짚는다.
	seen := ex.facts()
	if len(seen) != 1 {
		t.Fatalf("설명을 %d번 불렀다, want 1", len(seen))
	}
	if seen[0].Category != intervene.CategoryHangsPiece || seen[0].Attackers != 2 {
		t.Errorf("사실이 안 넘어왔다: %+v", seen[0])
	}
}

// Explainer 가 없으면 **결정적 문구가 그대로 나간다.** 카드가 비는 경로가 없다.
func TestInterventionFallsBackToTheTemplate(t *testing.T) {
	facts := explain.Facts{
		Kind: intervene.KindBlunder, Category: intervene.CategoryHangsPiece,
		Known: true, MovedPiece: "銀", Attackers: 2,
	}
	an := &fixedAnalyst{verdict: hangsPieceVerdict(), facts: facts}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: an,
		HumanColor: shogi.Black, // Explainer 없음
	})
	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	snap := waitFor(t, ch, func(s Snapshot) bool { return s.Intervention != nil }, "개입")

	if got, want := snap.Intervention.Message, explain.Render(facts); got != want {
		t.Errorf("문장이 %q, want %q", got, want)
	}
	if snap.Intervention.Message == "" {
		t.Error("문장이 비었다")
	}
}

// **통과한 수에는 설명을 부르지 않는다.**
//
// 한 판이 100수가 넘고 개입은 그중 여덟 국면이었다(08-playtest.md §5). 통과한 수마다 부르면
// 비용이 열 배가 되는데, 화면에는 그 문장이 나갈 자리조차 없다.
func TestNoExplanationForMovesThatStand(t *testing.T) {
	ex := &fakeExplainer{result: explain.Result{Body: "呼ばれてはいけない"}}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: &fixedAnalyst{},
		HumanColor: shogi.Black, Explainer: ex,
	})
	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "상대 응수")

	if n := len(ex.facts()); n != 0 {
		t.Errorf("개입이 없는데 설명을 %d번 불렀다", n)
	}
}

// 판정이 실패하면 설명도 안 부른다. 이유를 모르는데 문장을 살 수는 없다.
func TestNoExplanationWhenJudgingFails(t *testing.T) {
	ex := &fakeExplainer{result: explain.Result{Body: "呼ばれてはいけない"}}
	an := &fixedAnalyst{err: errors.New("engine down")}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: an,
		HumanColor: shogi.Black, Explainer: ex,
	})
	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "상대 응수")

	if n := len(ex.facts()); n != 0 {
		t.Errorf("판정이 실패했는데 설명을 %d번 불렀다", n)
	}
}

// **플레이 기록의 그 국면에서 다시 확인한다.**
//
// 69手 `▲2五飛` — 카테고리가 `other` 로 떨어져 「その手は形勢を大きく損ねます。もう一度
// 考えてみてください。」만 나갔던 자리다(08-playtest.md §5·§7). 기록자는 8九桂를 金 하나가
// 지킨다고 셌는데 9九馬도 그 자리를 보고 있었다(§6-2).
//
// 반박 수순의 첫 수가 그 桂를 딴다. 엔진이 실제로 돌려준 PV는 [§25](06-status.md)에 적혀
// 있으므로 **엔진 없이** 같은 것을 확인할 수 있다.
func TestPlaytestOtherNowNamesWhatIsTaken(t *testing.T) {
	moves := append(append([]string{}, playtestUpTo69...), "2h2e")
	// 06-status.md §25 가 실측으로 적어둔 PV다.
	pv := []string{"8f8i+", "P*3d", "8i7i", "3d3c+"}

	r := refutationLine(shogi.StartSFEN, moves, pv, RefutationPlies)

	if len(r.line) == 0 {
		t.Fatal("반박 수순이 비었다 — PV가 그 국면에서 합법이어야 한다")
	}
	if r.threatened != "桂" {
		t.Fatalf("threatened=%q, want 桂 (8九의 桂를 香이 딴다)", r.threatened)
	}

	// 그리고 그것이 그대로 문장이 된다. 그전에는 이 자리에 아무 사실도 없었다.
	got := explain.Render(explain.Facts{
		Kind: intervene.KindBlunder, Category: intervene.CategoryOther,
		Known: true, Threatened: r.threatened,
	})
	if !strings.Contains(got, "桂を取れます") {
		t.Errorf("문장이 잡히는 駒를 말하지 않는다: %q", got)
	}
	t.Logf("69手 ▲2五飛 → %s", got)
}
