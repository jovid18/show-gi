package game

import (
	"context"
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
)

// chaseOpponent 는 **어느 문으로 불렸는지**를 남기는 상대다.
//
// 고른 수로는 못 가른다 — 조절된 수와 최선수가 같은 국면이 흔하고, 그러면 조절이
// 안 꺼져도 테스트가 초록으로 남는다.
type chaseOpponent struct {
	mu    sync.Mutex
	moves []string
	i     int
	calls []string // "choose" 또는 "best"
}

func (o *chaseOpponent) next(door string) (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, door)
	if o.i >= len(o.moves) {
		return "resign", nil
	}
	m := o.moves[o.i]
	o.i++
	return m, nil
}

func (o *chaseOpponent) doors() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.calls...)
}

func (o *chaseOpponent) Choose(_ context.Context, _ string, _ []string, _ skill.Estimate) (string, error) {
	return o.next("choose")
}

func (o *chaseOpponent) ChooseBest(_ context.Context, _ string, _ []string) (string, error) {
	return o.next("best")
}

// AdaptsToSkill 이 true 라야 조절하는 상대다 — false 면 애초에 끌 것이 없다.
func (o *chaseOpponent) AdaptsToSkill() bool { return true }

// 사람이 詰み을 걸고 있으면 상대가 밴드를 안 본다. 연습이 성립하려면 저항이 정직해야
// 한다는 것이 근거다(MateChasePlies).
func TestOpponentPlaysBestWhileThePlayerHasAMate(t *testing.T) {
	// 3手詰 — MateChasePlies(7) 안이다.
	mate := &scriptedMate{plies: 3}
	opp := &chaseOpponent{moves: []string{"3c3d"}}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black, Mate: mate})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	// **게이지가 답할 때까지 기다린다.** 안 기다리면 「모르니까 조절을 그대로 둔다」쪽으로
	// 떨어져, 조절이 안 꺼진 것인지 아직 모르는 것인지가 안 갈린다.
	waitFor(t, ch, func(s Snapshot) bool { return s.MateHeat > 0 }, "게이지가 켜지기")

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return len(s.Moves) >= 2 }, "상대가 두기")

	if got := opp.doors(); len(got) == 0 || got[0] != "best" {
		t.Errorf("詰み을 걸고 있는데 조절된 수로 답했다: %v", got)
	}
}

// 詰み이 멀면 평소대로 조절한다 — 이 규칙이 종반 밖으로 새면 상대가 판 내내 최선수다.
func TestOpponentKeepsAdaptingWhenTheMateIsFar(t *testing.T) {
	// 9手詰 — MateChasePlies(7) 밖이다. 게이지에는 불이 붙는다(세기 1).
	mate := &scriptedMate{plies: 9}
	opp := &chaseOpponent{moves: []string{"3c3d"}}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black, Mate: mate})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	waitFor(t, ch, func(s Snapshot) bool { return s.MateHeat > 0 }, "게이지가 켜지기")

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return len(s.Moves) >= 2 }, "상대가 두기")

	if got := opp.doors(); len(got) == 0 || got[0] != "choose" {
		t.Errorf("9手詰인데 조절을 껐다 — 종반 밖으로 샜다: %v", got)
	}
}

// 게이지가 없는 판(solver 미배선)에서는 **모르는 것**이라 조절을 그대로 둔다.
func TestOpponentKeepsAdaptingWithoutAGauge(t *testing.T) {
	opp := &chaseOpponent{moves: []string{"3c3d"}}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return len(s.Moves) >= 2 }, "상대가 두기")

	if got := opp.doors(); len(got) == 0 || got[0] != "choose" {
		t.Errorf("모르는데 조절을 껐다: %v", got)
	}
}

// 인터페이스를 안 만족하는 상대에게도 **대국은 그대로 돈다.** 詰み 연습이 안 되는 것과
// 대국이 멈추는 것 중에서는 앞이 낫다(chooseBest).
func TestChooseBestFallsBackToTheOrdinaryDoor(t *testing.T) {
	plain := &scriptedOpponent{moves: []string{"3c3d"}}
	got, err := chooseBest(t.Context(), plain, "", nil, skill.Unknown)
	if err != nil {
		t.Fatalf("chooseBest: %v", err)
	}
	if got != "3c3d" {
		t.Errorf("평소 문으로 안 떨어졌다: %q", got)
	}
}
