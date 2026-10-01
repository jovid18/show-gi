package game

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/eval"
	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// treeStub 은 수순(공백으로 이은 것)마다 정해진 답을 준다. 없는 수순은 에러다.
//
// 트리는 묻는 국면이 많아서, 하나라도 엉뚱한 국면을 물으면 에러로 증명이 끊기게 둔다.
type treeStub struct {
	depth map[string]usi.SearchResult
	multi map[string]usi.SearchResult
}

func (s *treeStub) SearchDepth(_ context.Context, _ string, moves []string, _ int) (usi.SearchResult, error) {
	if r, ok := s.depth[strings.Join(moves, " ")]; ok {
		return r, nil
	}
	return usi.SearchResult{}, errors.New("stub: 안 물어야 하는 국면 " + strings.Join(moves, " "))
}

func (s *treeStub) SearchMultiPV(_ context.Context, _ string, moves []string, _, _ int) (usi.SearchResult, error) {
	if r, ok := s.multi[strings.Join(moves, " ")]; ok {
		return r, nil
	}
	return usi.SearchResult{}, errors.New("stub: 안 물어야 하는 국면 " + strings.Join(moves, " "))
}

// mateAt 은 정해진 수순에서만 詰み을 찾는다.
type mateAt map[string]int

func (m mateAt) SearchMate(_ context.Context, _ string, moves []string) (usi.MateResult, error) {
	n := m[strings.Join(moves, " ")]
	line := make([]string, n)
	for i := range line {
		line[i] = "1a1b"
	}
	return usi.MateResult{Moves: line, Proven: true}, nil
}

func pos(t *testing.T, moves ...string) shogi.Position {
	t.Helper()
	p, err := positionAfter(shogi.StartSFEN, moves)
	if err != nil {
		t.Fatalf("국면: %v", err)
	}
	return p
}

// 맞교환은 駒損이 아니다. 딴 駒가 내 持ち駒로 들어오는 몫을 빼먹으면 角交換이 角損으로
// 보였다(journal §146).
func TestSettledLossCountsAnEvenTradeAsNothing(t *testing.T) {
	before := pos(t, "7g7f", "3c3d")
	after := pos(t, "7g7f", "3c3d", "8h2b+")
	if got := settledLoss(after, shogi.White, material(before, shogi.White)); got != 0 {
		t.Errorf("角交換의 손해 = %d, want 0", got)
	}
}

// 되딸 수 없는 角은 손해로 남는다. ▲3三角成 △同角 뒤에 先手는 角을 되찾지 못한다.
func TestSettledLossKeepsWhatCannotBeTakenBack(t *testing.T) {
	before := pos(t, "7g7f", "3c3d")
	after := pos(t, "7g7f", "3c3d", "8h3c+", "2b3c")
	if got := settledLoss(after, shogi.Black, material(before, shogi.Black)); got < ProofLoss {
		t.Errorf("角을 잃은 손해 = %d, want ≥ %d", got, ProofLoss)
	}
}

// 상대의 최선수 한 수로 손해가 확정되면 응수를 펼치지 않는다. 엔진도 부르지 않는다.
func TestProveLossStopsAtTheReplyWhenItAlreadyCosts(t *testing.T) {
	a := &engineAnalyst{search: &treeStub{}, depth: JudgeDepth, level: intervene.Beginner}
	best, losses, ok := a.proveLoss(t.Context(), shogi.StartSFEN, thrownBishopMoves, "2b3c")
	if !ok {
		t.Fatal("증명하지 못했다")
	}
	if best != "△同角" || len(losses) != 1 || len(losses[0].Moves) != 0 {
		t.Fatalf("best=%q losses=%+v", best, losses)
	}
	if !slices.Equal(losses[0].Taken, []string{"馬"}) {
		t.Errorf("따인 駒 = %v, want [馬]", losses[0].Taken)
	}
}

// openedTrade 는 角交換이 걸린 국면에서 응수 둘을 펼치는 트리다. ▲同銀 뒤에는 詰み,
// ▲7七桂 뒤에는 △7九馬 로 銀까지 잃는다.
func openedTrade(secondReply string) (*treeStub, mateAt) {
	root := "7g7f 3c3d 1g1f 2b8h+"
	return &treeStub{
			multi: map[string]usi.SearchResult{
				root: {Lines: []usi.SearchLine{pvLine(1, -100, "7i8h"), pvLine(2, -300, "8i7g")}},
			},
			depth: map[string]usi.SearchResult{
				root + " 7i8h": {Best: "B*5e", Score: eval.Mate(5)},
				root + " 8i7g": {Best: secondReply, Score: eval.Cp(900)},
			},
		}, mateAt{
			root + " 7i8h": 5,
		}
}

// 응수마다 끝점까지 가야 증명이다. 되따기는 엔진 순위에 이미 있으면 한 번만 센다.
func TestProveLossFollowsEveryAnswerToItsEnd(t *testing.T) {
	stub, mate := openedTrade("8h7i")
	a := &engineAnalyst{search: stub, mate: mate, depth: JudgeDepth, level: intervene.Beginner}

	best, losses, ok := a.proveLoss(t.Context(), shogi.StartSFEN, openedDiagonal, "2b8h+")
	if !ok {
		t.Fatal("증명하지 못했다")
	}
	if best != "△8八角成" {
		t.Errorf("상대 최선수 = %q", best)
	}
	if len(losses) != 2 {
		t.Fatalf("줄 %d개: %+v", len(losses), losses)
	}
	if got := losses[0]; !slices.Equal(got.Moves, []string{"▲同銀"}) || got.MatePlies != 5 {
		t.Errorf("첫 줄 = %+v", got)
	}
	if got := losses[1]; !slices.Equal(got.Moves, []string{"▲7七桂", "△7九馬"}) ||
		!slices.Equal(got.Taken, []string{"角", "銀"}) {
		t.Errorf("둘째 줄 = %+v", got)
	}
}

// 한 줄이라도 끝점에 닿지 못하면 아무것도 주장하지 않는다.
func TestProveLossClaimsNothingWhenOneAnswerHolds(t *testing.T) {
	stub, mate := openedTrade("3d3e")
	a := &engineAnalyst{search: stub, mate: mate, depth: JudgeDepth, level: intervene.Beginner}

	if _, losses, ok := a.proveLoss(t.Context(), shogi.StartSFEN, openedDiagonal, "2b8h+"); ok {
		t.Errorf("버틴 응수가 있는데 증명했다: %+v", losses)
	}
}

// 탐색의 mate 점수만으로는 詰み이라고 말하지 않는다. solver 가 찾지 못하면 증명이 아니다.
func TestProveLossNeedsTheSolverForMate(t *testing.T) {
	stub, _ := openedTrade("8h7i")
	a := &engineAnalyst{search: stub, mate: mateAt{}, depth: JudgeDepth, level: intervene.Beginner}

	if _, _, ok := a.proveLoss(t.Context(), shogi.StartSFEN, openedDiagonal, "2b8h+"); ok {
		t.Error("solver 가 찾지 못한 詰み으로 증명했다")
	}
}

// 플레이어 쪽에 詰ます 수가 있으면 그 국면은 손해가 아니다.
func TestProveLossGivesUpWhenThePlayerMates(t *testing.T) {
	stub, mate := openedTrade("8h7i")
	stub.multi["7g7f 3c3d 1g1f 2b8h+"] = usi.SearchResult{Lines: []usi.SearchLine{
		{Depth: JudgeDepth, MultiPV: 1, Move: "7i8h", Score: eval.Mate(3), PV: []string{"7i8h"}},
	}}
	a := &engineAnalyst{search: stub, mate: mate, depth: JudgeDepth, level: intervene.Beginner}

	if _, _, ok := a.proveLoss(t.Context(), shogi.StartSFEN, openedDiagonal, "2b8h+"); ok {
		t.Error("詰ます 수가 있는데 증명했다")
	}
}

// 예산을 넘으면 증명하지 않는다. 그 예산이 카드 지연의 상한이다.
func TestProveLossStopsAtTheBudget(t *testing.T) {
	stub, mate := openedTrade("8h7i")
	a := &engineAnalyst{search: stub, mate: mate, depth: JudgeDepth, level: intervene.Beginner}

	saved := proofSearches
	proofSearches = 2
	t.Cleanup(func() { proofSearches = saved })
	if _, _, ok := a.proveLoss(t.Context(), shogi.StartSFEN, openedDiagonal, "2b8h+"); ok {
		t.Error("예산을 넘겼는데 증명했다")
	}
}

// 龍·馬를 만들어 내가 딸 수 없으면 끝점이다. ▲2二角成 △同銀 은 맞교환이라 아니다.
func TestNewPromotedMajorNeedsOneThatStays(t *testing.T) {
	base := pos(t, "7g7f", "3c3d")
	traded := pos(t, "7g7f", "3c3d", "8h2b+")
	if _, ok := newPromotedMajor(base, traded, shogi.White); ok {
		t.Error("되딸 수 있는 馬를 끝점으로 셌다")
	}
	// 9九의 角이 성한 것만 다르다. 後手玉은 5一이라 그 馬에 닿지 않는다.
	bishop, _ := shogi.ParseSFEN("4k4/9/9/9/9/9/9/9/B3K4 w - 1")
	horse, _ := shogi.ParseSFEN("4k4/9/9/9/9/9/9/9/+B3K4 w - 1")
	if got, ok := newPromotedMajor(bishop, horse, shogi.White); !ok || got != shogi.PromBishop {
		t.Errorf("남는 馬 = %v %v", got, ok)
	}
}

// Judge 가 other 를 forced_loss 로 바꾸고 문장에 줄을 싣는다.
func TestJudgeTurnsAProvenOtherIntoForcedLoss(t *testing.T) {
	stub, mate := openedTrade("8h7i")
	stub.depth["7g7f 3c3d"] = usi.SearchResult{Best: "2g2f", Score: eval.Cp(0), PV: []string{"2g2f"}}
	stub.depth["7g7f 3c3d 1g1f"] = usi.SearchResult{Best: "2b8h+", Score: eval.Cp(1600), PV: []string{"2b8h+", "7i8h"}}
	stub.multi["7g7f 3c3d 1g1f"] = usi.SearchResult{Lines: []usi.SearchLine{pvLine(1, 1600, "2b8h+", "7i8h")}}
	a := &engineAnalyst{search: stub, mate: mate, depth: JudgeDepth, level: intervene.Beginner}

	j, err := a.Judge(t.Context(), shogi.StartSFEN, openedDiagonal, len(openedDiagonal))
	if err != nil {
		t.Fatalf("판정: %v", err)
	}
	if j.Verdict.Category != intervene.CategoryForcedLoss {
		t.Fatalf("category = %s, want forced_loss", j.Verdict.Category)
	}
	got := explain.Render(j.Facts)
	for _, want := range []string{"△8八角成", "▲同銀 → 5手で詰まされる", "▲7七桂 → △7九馬 → 角と銀を取られて駒損"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q 가 없다: %q", want, got)
		}
	}
}
