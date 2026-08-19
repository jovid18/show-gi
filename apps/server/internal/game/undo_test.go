package game

import (
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 사람의 수 하나를 무르면 상대의 응수까지 사라지고 판이 사람 차례로 돌아온다.
//
// 되돌리는 폭이 두 手다 — 한 手만 되돌리면 상대 차례가 되어 사람이 다시 둘 수 없다.
func TestUndoTakesBackTheHumanMoveAndTheReply(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "엔진 응수")

	snap, err := s.Undo(t.Context())
	if err != nil {
		t.Fatalf("Undo: %v", err)
	}
	if snap.Ply != 0 {
		t.Fatalf("무른 뒤 手数 = %d (0 기대)", snap.Ply)
	}
	if len(snap.Moves) != 0 {
		t.Fatalf("기보가 안 비었다: %+v", snap.Moves)
	}
	if !snap.YourTurn {
		t.Fatalf("무른 뒤에는 사람 차례여야 한다: %+v", snap)
	}
	// 판이 실제로 되돌아갔는가 — 되감기가 기보만 자르고 국면을 안 되돌리면 여기서 걸린다.
	if snap.SFEN != shogi.StartSFEN {
		t.Fatalf("국면이 안 돌아왔다: %s", snap.SFEN)
	}
	if len(snap.LegalMoves) != 30 {
		t.Fatalf("초기 국면 합법수 = %d (30 기대)", len(snap.LegalMoves))
	}
}

// 무른 자리에서 다시 두면 그 판이 그대로 이어진다. 되감기가 千日手 계수나 도착 칸을
// 안 되돌리면 여기서 표기가 어긋난다.
func TestUndoLetsThePlayerPlayAgain(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d", "8c8d"}}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "엔진 응수")
	if _, err := s.Undo(t.Context()); err != nil {
		t.Fatalf("Undo: %v", err)
	}

	snap, err := s.Play(t.Context(), "2g2f")
	if err != nil {
		t.Fatalf("무른 뒤 Play: %v", err)
	}
	if snap.Ply != 1 || snap.Moves[0].Ja != "▲2六歩" {
		t.Fatalf("다시 둔 수 = %+v", snap.Moves)
	}
	got := waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "다시 둔 뒤 엔진 응수")
	if got.Moves[1].By != SideEngine {
		t.Fatalf("2手目가 상대 수가 아니다: %+v", got.Moves[1])
	}
}

// 예산은 판당 UndoMaxPerGame 이다.
func TestUndoRunsOutAfterTheBudget(t *testing.T) {
	// 정해진 수순을 쓰지 않는다. 되감으면 판은 처음으로 돌아가는데 대본은 그대로
	// 앞으로 가서, 두 번째 회차의 수가 그 국면에서 불법이 된다.
	s := newSession(t, Config{Opponent: legalOpponent{}, HumanColor: shogi.Black})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	for i := range UndoMaxPerGame {
		if _, err := s.Play(t.Context(), "7g7f"); err != nil {
			t.Fatalf("%d번째 Play: %v", i+1, err)
		}
		waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "엔진 응수")

		snap, err := s.Undo(t.Context())
		if err != nil {
			t.Fatalf("%d번째 Undo: %v", i+1, err)
		}
		if want := UndoMaxPerGame - i - 1; snap.UndoLeft != want {
			t.Fatalf("%d번째 뒤 남은 횟수 = %d (%d 기대)", i+1, snap.UndoLeft, want)
		}
	}

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "엔진 응수")

	snap, err := s.Undo(t.Context())
	if err == nil {
		t.Fatal("예산을 다 썼는데 무르기가 통과했다")
	}
	if err != ErrNoUndoLeft {
		t.Fatalf("err = %v (ErrNoUndoLeft 기대)", err)
	}
	if snap.Ply != 2 {
		t.Fatalf("거절된 무르기가 판을 건드렸다: 手数 = %d", snap.Ply)
	}
	if snap.CanUndo {
		t.Fatal("예산이 없는데 canUndo 가 참이다")
	}
}

// 이어하는 판은 예산을 이어받는다. 이 값이 0으로 돌아가면 새로고침 한 번에 예산이
// 다시 차서 「3회 제한」이 「연결당 3회」가 된다(Config.UndoUsed).
func TestUndoBudgetCarriesIntoAResumedGame(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"8c8d"}}
	s := newSession(t, Config{
		Opponent:   opp,
		HumanColor: shogi.Black,
		StartMoves: []string{"7g7f", "3c3d"},
		UndoUsed:   UndoMaxPerGame,
	})

	snap, err := s.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.UndoLeft != 0 {
		t.Fatalf("이어한 판의 남은 횟수 = %d (0 기대)", snap.UndoLeft)
	}
	if snap.CanUndo {
		t.Fatal("예산을 다 쓴 판을 이어했는데 canUndo 가 참이다")
	}
	if _, err := s.Undo(t.Context()); err != ErrNoUndoLeft {
		t.Fatalf("err = %v (ErrNoUndoLeft 기대)", err)
	}
}

// 사람이 아직 한 수도 안 뒀으면 되돌릴 것이 없다.
func TestUndoNeedsAHumanMoveToTakeBack(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	snap, err := s.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.CanUndo {
		t.Fatal("한 수도 안 뒀는데 canUndo 가 참이다")
	}
	if snap.UndoLeft != UndoMaxPerGame {
		t.Fatalf("남은 횟수 = %d (%d 기대)", snap.UndoLeft, UndoMaxPerGame)
	}
	if _, err := s.Undo(t.Context()); err != ErrNothingToUndo {
		t.Fatalf("err = %v (ErrNothingToUndo 기대)", err)
	}
}

// 상대가 생각하는 동안에는 못 무른다. 그 사이에 되감으면 날아오는 탐색 결과가
// 되감기 전 국면의 것이다.
func TestUndoIsRefusedWhileTheOpponentThinks(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}, delay: 300 * time.Millisecond}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	snap, err := s.Undo(t.Context())
	if err != ErrNotYourTurn {
		t.Fatalf("err = %v (ErrNotYourTurn 기대)", err)
	}
	if snap.CanUndo {
		t.Fatal("상대가 생각 중인데 canUndo 가 참이다")
	}
	if snap.Ply != 1 {
		t.Fatalf("거절된 무르기가 판을 건드렸다: 手数 = %d", snap.Ply)
	}
}

// 끝난 판은 못 무른다.
func TestUndoIsRefusedAfterTheGameEnds(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	if _, err := s.Resign(t.Context()); err != nil {
		t.Fatalf("Resign: %v", err)
	}
	if _, err := s.Undo(t.Context()); err != ErrFinished {
		t.Fatalf("err = %v (ErrFinished 기대)", err)
	}
}

// 무른 수도 실력 추정에 남는다 — 회차 1 #4 의 두 번째 요구다.
//
// 판정을 통과한 수는 그때 추정기가 이미 먹었고(applyVerdict), 무르기는 그것을 안 되돌린다.
// 되돌리면 「어려운 수를 두고 무르면 실력이 안 떨어진다」가 되어 상대가 실제보다 약해진다.
func TestUndoKeepsTheMoveInTheSkillEstimate(t *testing.T) {
	rater := newFakeRater()
	analyst := &fixedAnalyst{
		verdict:   intervene.Verdict{Kind: intervene.KindNone, DeltaWin: 0.12},
		threshold: 0.25,
	}
	opp := &scriptedOpponent{moves: []string{"3c3d"}}
	s := newSession(t, Config{
		Opponent:   opp,
		Analyst:    analyst,
		Rater:      rater,
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
	waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "엔진 응수")

	before := rater.moves()
	if len(before) != 1 {
		t.Fatalf("판정을 통과한 수가 추정기에 안 갔다: %+v", before)
	}

	if _, err := s.Undo(t.Context()); err != nil {
		t.Fatalf("Undo: %v", err)
	}

	after := rater.moves()
	if len(after) != len(before) {
		t.Fatalf("무르기가 추정기의 표본을 건드렸다: %d → %d", len(before), len(after))
	}
	if after[0].DeltaWin != 0.12 {
		t.Fatalf("남은 표본의 낙폭 = %v (0.12 기대)", after[0].DeltaWin)
	}
}

// 무른 수는 기보가 아니라 무르기 기록으로 간다. 기보에 남으면 되감기가
// 되감기가 아니게 되고, interventions 로 가면 개입 횟수가 부풀어 오른다.
func TestUndoRecordsTheMoveOutsideTheKifu(t *testing.T) {
	rec := &fakeRecorder{}
	opp := &scriptedOpponent{moves: []string{"3c3d"}}
	s := newSession(t, Config{Opponent: opp, Recorder: rec, HumanColor: shogi.Black})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "엔진 응수")
	if _, err := s.Undo(t.Context()); err != nil {
		t.Fatalf("Undo: %v", err)
	}

	var undone string
	for _, l := range rec.all() {
		if len(l) >= 6 && l[:6] == "undone" {
			undone = l
		}
	}
	// 手数는 무른 사람 수의 것이다. 여기가 어긋나면 store 가 엉뚱한 자리부터 자른다.
	if undone != "undone 1 7g7f" {
		t.Fatalf("무르기 기록 = %q (\"undone 1 7g7f\" 기대), 전체 = %v", undone, rec.all())
	}
}
