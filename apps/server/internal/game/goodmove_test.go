package game

import (
	"context"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

func TestObviousMove(t *testing.T) {
	sq55 := shogi.SquareOf(5, 5)
	cases := []struct {
		name   string
		sfen   string
		move   string
		prevTo int
		want   Obvious
	}{
		{"王手를 받는 수", "4k4/9/9/9/9/9/9/4r4/4K4 b - 1", "5i5h", -1, ObviousEvasion},
		{"직전 칸의 되잡기", "4k4/9/9/4g4/4p4/4P4/9/9/4K4 b - 1", "5f5e", sq55, ObviousRecapture},
		{"되잡히지 않는 駒 따기", "4k4/9/9/9/4p4/4P4/9/9/4K4 b - 1", "5f5e", -1, ObviousFreeCapture},
		{"되잡히는 駒 따기", "4k4/9/9/4g4/4p4/4P4/9/9/4K4 b - 1", "5f5e", -1, ObviousNone},
		{"싼 駒로 비싼 駒 따기", "4k4/9/9/4g4/4s4/4P4/9/9/4K4 b - 1", "5f5e", -1, ObviousGainingCapture},
		{"조용한 수", "4k4/9/9/4g4/4p4/4P4/9/9/4K4 b - 1", "5i4h", -1, ObviousNone},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pos, err := shogi.ParseSFEN(c.sfen)
			if err != nil {
				t.Fatal(err)
			}
			m, err := shogi.ParseUSIMove(c.move)
			if err != nil || pos.ValidateMove(m) != nil {
				t.Fatalf("%s 를 둘 수 없다: %v", c.move, err)
			}
			if got := ObviousMove(pos, m, c.prevTo); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// 好手는 판정을 통과해 확정된 수에만 붙고, 그 手数의 기보 행이 생긴 뒤에 기록된다.
func TestGoodMoveIsRecordedAfterTheMove(t *testing.T) {
	rec := &fakeRecorder{}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: &fixedAnalyst{good: true},
		HumanColor: shogi.Black, Recorder: rec,
	})
	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return s.YourTurn && s.Ply == 2 }, "상대 수")

	moved, good := -1, -1
	for i, l := range rec.all() {
		switch l {
		case "moved 1 7g7f human":
			moved = i
		case "good 1":
			good = i
		}
	}
	if good < 0 || moved < 0 || good < moved {
		t.Errorf("기보 행 뒤에 好手가 적혀야 한다: %v", rec.all())
	}
}

// seqAnalyst 는 처음 blunders 번은 개입을 걸고 그 뒤로는 好手로 통과시킨다.
type seqAnalyst struct {
	blunders int32
	calls    atomic.Int32
}

func (a *seqAnalyst) Judge(context.Context, string, []string, int) (Judgement, error) {
	if a.calls.Add(1) <= a.blunders {
		return Judgement{Verdict: blunder(), BestUSI: "2g2f"}, nil
	}
	return Judgement{Verdict: intervene.Verdict{}, Good: true}, nil
}

// 갇힘 힌트가 짚은 국면에서 둔 수는 스스로 찾은 것이 아니다.
func TestGoodMoveIsNotRecordedAfterAStuckHint(t *testing.T) {
	rec := &fakeRecorder{}
	an := &seqAnalyst{blunders: HintPieceAfter}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: an,
		HumanColor: shogi.Black, Recorder: rec,
	})
	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	for i := range HintPieceAfter {
		if _, err := s.Play(t.Context(), "7g7f"); err != nil {
			t.Fatalf("Play %d: %v", i, err)
		}
		waitFor(t, ch, func(s Snapshot) bool { return s.Intervention != nil && s.YourTurn }, "개입")
	}
	if _, err := s.Play(t.Context(), "2g2f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return s.YourTurn && s.Ply == 2 }, "상대 수")
	for _, l := range rec.all() {
		if strings.HasPrefix(l, "good ") {
			t.Errorf("힌트를 본 수에 好手가 적혔다: %v", rec.all())
		}
	}
}

// laterAnalyst 는 판정에서 물음만 남기고, 好手는 따로 물을 때 답한다(engineAnalyst 와 같은 모양).
type laterAnalyst struct {
	asked    atomic.Int32
	borrower atomic.Value
}

func (a *laterAnalyst) Judge(_ context.Context, start string, moves []string, _ int) (Judgement, error) {
	q := &goodQuery{startSFEN: start, before: moves[:len(moves)-1], played: moves[len(moves)-1]}
	return Judgement{goodQuery: q}, nil
}

func (a *laterAnalyst) askGood(ctx context.Context, _ goodQuery) goodCheck {
	a.asked.Add(1)
	a.borrower.Store(usi.BorrowerFrom(ctx))
	return goodCheck{Asked: true, Gap: 0.3, Good: true}
}

// 대국은 好手를 기다리지 않는다. 확정 뒤에 따로 물은 답이 세션을 거쳐 기록된다.
func TestGoodMoveIsAskedAfterTheMoveStands(t *testing.T) {
	rec := &fakeRecorder{}
	an := &laterAnalyst{}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: an,
		HumanColor: shogi.Black, Recorder: rec,
	})
	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	deadline := time.Now().Add(waitDeadline)
	for {
		if slices.Contains(rec.all(), "good 1") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("好手가 기록되지 않았다: %v (asked %d)", rec.all(), an.asked.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
	// 상대의 수 탐색보다 뒤에 줄을 서야 한다(usi.priorityOf).
	if got := an.borrower.Load(); got != usi.BorrowerGood {
		t.Errorf("borrower = %v, want %q", got, usi.BorrowerGood)
	}
}
