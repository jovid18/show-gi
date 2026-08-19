package game

import (
	"errors"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

func hangsPieceVerdict() intervene.Verdict {
	return intervene.Verdict{
		Kind: intervene.KindBlunder, DeltaWin: 0.42, Category: intervene.CategoryHangsPiece,
	}
}

// 카드의 문장은 explain.Render 가 만든다. 세션이 문구를 직접 짓지 않는다.
//
// 갈라지면 같은 수가 대국 중과 되짚기에서 다른 이유로 나쁜 것이 되고, 그 사실이 아무
// 에러도 내지 않는다 — 화면만 조용히 다른 말을 한다.
func TestInterventionMessageComesFromRender(t *testing.T) {
	facts := explain.Facts{
		Kind: intervene.KindBlunder, Category: intervene.CategoryHangsPiece,
		Known: true, MovedPiece: "銀", Attackers: 2,
	}
	an := &fixedAnalyst{verdict: hangsPieceVerdict(), facts: facts}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: an,
		HumanColor: shogi.Black,
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

	// 판정이 구한 사실이 그대로 문장이 된다. 여기서 비면 문장이 국면을 못 짚는다.
	if got, want := snap.Intervention.Message, explain.Render(facts); got != want {
		t.Errorf("문장이 %q, want %q", got, want)
	}
	if !strings.Contains(snap.Intervention.Message, "2枚") {
		t.Errorf("사실이 빠진 문장이다: %q", snap.Intervention.Message)
	}
}

// 통과한 수에는 카드가 안 뜬다. 개입은 큰 실수에서만 멈춘다.
func TestNoCardForMovesThatStand(t *testing.T) {
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: &fixedAnalyst{},
		HumanColor: shogi.Black,
	})
	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	snap := waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "상대 응수")

	if snap.Intervention != nil {
		t.Errorf("개입이 없는데 카드가 떴다: %+v", snap.Intervention)
	}
}

// 판정이 실패해도 카드를 만들지 않는다. 이유를 모르는데 문장을 낼 수는 없고,
// 무엇보다 대국이 그대로 이어져야 한다 — 개입은 부가 기능이다.
func TestNoCardWhenJudgingFails(t *testing.T) {
	an := &fixedAnalyst{err: errors.New("engine down")}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: an,
		HumanColor: shogi.Black,
	})
	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	snap := waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "상대 응수")

	if snap.Intervention != nil {
		t.Errorf("판정이 실패했는데 카드가 떴다: %+v", snap.Intervention)
	}
}

// 플레이 기록의 그 국면에서 다시 확인한다.
//
// 69手 ▲2五飛 — 카테고리가 other 로 떨어져 「その手は形勢を大きく損ねます。もう一度
// 考えてみてください。」만 나갔던 자리다(08-playtest.md §5·§7). 기록자는 8九桂를 金 하나가
// 지킨다고 셌는데 9九馬도 그 자리를 보고 있었다(§6-2).
//
// 반박 수순의 첫 수가 그 桂를 딴다. 엔진이 실제로 돌려준 PV는 [§25](journal/21-40.md)에 적혀
// 있으므로 엔진 없이 같은 것을 확인할 수 있다.
func TestPlaytestOtherNowNamesWhatIsTaken(t *testing.T) {
	moves := append(append([]string{}, playtestUpTo69...), "2h2e")
	// journal §25 가 실측으로 적어둔 PV다.
	pv := []string{"8f8i+", "P*3d", "8i7i", "3d3c+"}

	r := refutationLine(shogi.StartSFEN, moves, pv, RefutationPlies, false)

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
