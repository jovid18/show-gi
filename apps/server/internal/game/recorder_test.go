package game

import (
	"fmt"
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// fakeRecorder 는 넘어온 것을 순서대로 적어둔다.
type fakeRecorder struct {
	mu   sync.Mutex
	logs []string
}

func (r *fakeRecorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, s)
}

func (r *fakeRecorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.logs...)
}

func (r *fakeRecorder) Started(startSFEN string, _ shogi.Color) { r.add("started") }
func (r *fakeRecorder) Moved(ply int, usi string, by Side) {
	r.add(fmt.Sprintf("moved %d %s %s", ply, usi, by))
}

func (r *fakeRecorder) Retracted(ply int, usi string, v intervene.Verdict) {
	r.add(fmt.Sprintf("retracted %d %s %s", ply, usi, v.Category))
}
func (r *fakeRecorder) Finished(status Status, winner Side) {
	r.add(fmt.Sprintf("finished %s %s", status, winner))
}

// **물러진 수는 기보로 안 간다.**
//
// 여기서 새면 기보가 롤백을 반영하지 못하고, 「개입이 막지 않았다면 뒀을 수」와
// 「실제로 둔 수」가 한 통에 섞여 실력 추정이 조용히 틀어진다.
func TestRecorderKeepsRetractedMovesOutOfTheKifu(t *testing.T) {
	rec := &fakeRecorder{}
	an := &fixedAnalyst{verdict: intervene.Verdict{
		Kind: intervene.KindBlunder, DeltaWin: 0.42, Category: intervene.CategoryHangsPiece,
	}}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: an,
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
	waitFor(t, ch, func(s Snapshot) bool { return s.Intervention != nil }, "개입")

	for _, got := range rec.all() {
		if got == "moved 1 7g7f human" {
			t.Fatalf("물러진 수가 기보로 갔다: %v", rec.all())
		}
	}
	want := "retracted 1 7g7f hangs_piece"
	if !contains(rec.all(), want) {
		t.Fatalf("%q 가 없다: %v", want, rec.all())
	}
}

// 판정을 통과한 수만 기보로 간다. 상대 수는 판정하지 않으므로 두는 즉시 확정이다.
func TestRecorderLogsConfirmedMoves(t *testing.T) {
	rec := &fakeRecorder{}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: &fixedAnalyst{},
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
	waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "상대 응수")

	for _, want := range []string{"started", "moved 1 7g7f human", "moved 2 3c3d engine"} {
		if !contains(rec.all(), want) {
			t.Errorf("%q 가 없다: %v", want, rec.all())
		}
	}
}

// 대국이 끝나면 그것도 남는다. 안 남기면 「아직 두는 중」과 구별이 안 된다.
func TestRecorderLogsTheEnding(t *testing.T) {
	rec := &fakeRecorder{}
	s := newSession(t, Config{
		Opponent:   &scriptedOpponent{moves: []string{"3c3d"}},
		HumanColor: shogi.Black, Recorder: rec,
	})
	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Resign(t.Context()); err != nil {
		t.Fatalf("Resign: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return s.Status != StatusPlaying }, "투료")

	if !contains(rec.all(), "finished resigned engine") {
		t.Fatalf("끝난 것이 안 남았다: %v", rec.all())
	}
}

// Recorder 가 nil이어도 대국은 그대로 돈다. 기록은 부가 기능이고 대국이 본체다.
func TestSessionRunsWithoutRecorder(t *testing.T) {
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, HumanColor: shogi.Black,
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
}

func contains(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}
