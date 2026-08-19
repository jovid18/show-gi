package game

import (
	"context"
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// fakeRater 는 받은 착수를 모으고, 시험이 시키는 대로 추정치를 올려보낸다.
// 실제 계산은 skill 쪽 테스트가 본다 — 여기서 보는 것은 배선이다.
type fakeRater struct {
	mu   sync.Mutex
	seen []skill.Move
	out  chan skill.Estimate
}

func newFakeRater() *fakeRater {
	return &fakeRater{out: make(chan skill.Estimate, 1)}
}

func (r *fakeRater) Observe(m skill.Move) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, m)
}

func (r *fakeRater) Estimates() <-chan skill.Estimate { return r.out }

func (r *fakeRater) moves() []skill.Move {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]skill.Move(nil), r.seen...)
}

// recordingOpponent 는 넘겨받은 추정치를 적어 두고 정해진 수를 둔다.
type recordingOpponent struct {
	usi  string
	sk   chan skill.Estimate
	once sync.Once
}

func newRecordingOpponent(usi string) *recordingOpponent {
	return &recordingOpponent{usi: usi, sk: make(chan skill.Estimate, 4)}
}

// 추정치를 보는 상대라고 말한다 — 화면의 눈금이 이걸로 갈린다(SkillAdapter).
func (o *recordingOpponent) AdaptsToSkill() bool { return true }

func (o *recordingOpponent) Choose(_ context.Context, _ string, _ []string, sk skill.Estimate) (string, error) {
	select {
	case o.sk <- sk:
	default:
	}
	return o.usi, nil
}

// 걸린 수도 통과한 수도 신호다. 물러진 것만 세면 표본이 개입에 오염되고, 통과한 것만
// 세면 제일 큰 실수가 안 들어온다(journal §47).
func TestRaterSeesBothTheRetractedAndThePassedMove(t *testing.T) {
	rater := newFakeRater()
	an := &fixedAnalyst{verdict: blunder(), threshold: 0.25}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: an,
		Rater: rater, HumanColor: shogi.Black,
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

	an.verdict = intervene.Verdict{DeltaWin: 0.05} // 이번엔 통과한다. 낙폭은 남아 있다
	if _, err := s.Play(t.Context(), "2g2f"); err != nil {
		t.Fatalf("두 번째 착수: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "엔진 응수")

	got := rater.moves()
	if len(got) != 2 {
		t.Fatalf("착수 2건이 가야 한다: %+v", got)
	}
	if !got[0].Blunder || got[0].DeltaWin != 0.42 {
		t.Errorf("물러진 수가 그대로 안 갔다: %+v", got[0])
	}
	if got[1].Blunder || got[1].DeltaWin != 0.05 {
		t.Errorf("통과한 수가 그대로 안 갔다: %+v", got[1])
	}
	for i, m := range got {
		if m.Threshold != 0.25 {
			t.Errorf("%d번째: 판정에 쓰인 임계치가 안 갔다 — 정규화가 무너진다: %+v", i, m)
		}
	}
}

// 추정치는 상대를 고를 때 쓰인다. 세션이 들고 있는 최신 값이 그대로 내려가야 한다.
func TestOpponentIsGivenTheLatestEstimate(t *testing.T) {
	rater := newFakeRater()
	opp := newRecordingOpponent("3c3d")
	s := newSession(t, Config{
		Opponent: opp, Analyst: &fixedAnalyst{threshold: 0.25},
		Rater: rater, HumanColor: shogi.Black,
	})
	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	// 헤매는 사람으로 올려보낸다. 세션이 받았다는 것은 화면에 나가는 단계로 확인한다.
	rater.out <- skill.Estimate{Loss: 1, Samples: skill.MinSamples}
	waitFor(t, ch, func(s Snapshot) bool { return s.OpponentStrength == 1 }, "강함 눈금이 내려간 스냅샷")

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "엔진 응수")

	select {
	case sk := <-opp.sk:
		if sk.Loss != 1 || !sk.Ready() {
			t.Fatalf("상대가 받은 추정치가 최신이 아니다: %+v", sk)
		}
	default:
		t.Fatal("상대가 추정치를 못 받았다")
	}
}

// 추정기가 없으면 눈금을 그리지 않는다. 0을 보내면 화면이 「고정된 강함」과
// 「조절 중이지만 아직 모름」을 구별할 수 없다.
//
// 상대가 추정치를 무시할 때도 안 그린다. 추정기만 보고 갈랐더니 「강함이 내려가는데
// 상대는 최선수를 그대로 두는」 조립이 눈금을 얻고 있었다 — 프로덕션이 adaptive 하나라
// 안 드러났을 뿐이다(SkillAdapter).
func TestStrengthIsAbsentUnlessTheOpponentActuallyAdapts(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{"추정기가 없다", Config{
			Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, HumanColor: shogi.Black,
		}},
		{"상대가 추정치를 안 본다", Config{
			Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, HumanColor: shogi.Black,
			Analyst: &fixedAnalyst{}, Rater: newFakeRater(),
		}},
	} {
		s := newSession(t, tc.cfg)
		snap, err := s.Snapshot(t.Context())
		if err != nil {
			t.Fatalf("%s: Snapshot: %v", tc.name, err)
		}
		if snap.OpponentStrength != 0 {
			t.Errorf("%s: 강함을 말했다: %d", tc.name, snap.OpponentStrength)
		}
	}

	// 반대쪽 — adaptive 는 첫 스냅샷부터 한복판을 말한다. 0이 아니다.
	adaptive := NewAdaptiveOpponent(&stubMulti{res: usi.SearchResult{Best: "3c3d"}}, 12, DefaultBand)
	s := newSession(t, Config{
		Opponent: adaptive, HumanColor: shogi.Black,
		Analyst: &fixedAnalyst{}, Rater: newFakeRater(),
	})
	snap, err := s.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.OpponentStrength != 3 {
		t.Fatalf("조절하는 상대인데 눈금이 한복판이 아니다: %d", snap.OpponentStrength)
	}
}

// 판정이 실패한 수는 추정에 안 들어간다 — 낙폭이 없는데 「손해 0」으로 세면
// 엔진이 죽어 있는 동안 플레이어가 최선수만 둔 것이 된다.
func TestFailedJudgementIsNotASignal(t *testing.T) {
	rater := newFakeRater()
	an := &fixedAnalyst{err: context.DeadlineExceeded}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: an,
		Rater: rater, HumanColor: shogi.Black,
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

	if got := rater.moves(); len(got) != 0 {
		t.Fatalf("판정이 실패한 수가 추정으로 갔다: %+v", got)
	}
}
