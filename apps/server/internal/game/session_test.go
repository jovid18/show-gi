package game

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// scriptedOpponent 는 정해진 수를 순서대로 둔다. 엔진 없이 대국 흐름만 본다.
type scriptedOpponent struct {
	moves []string
	i     int
	delay time.Duration
	err   error
}

func (o *scriptedOpponent) Choose(ctx context.Context, _ string, _ []string, _ skill.Estimate) (string, error) {
	if o.delay > 0 {
		select {
		case <-time.After(o.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if o.err != nil {
		return "", o.err
	}
	if o.i >= len(o.moves) {
		return "resign", nil
	}
	m := o.moves[o.i]
	o.i++
	return m, nil
}

// legalOpponent 는 합법수 중 첫 번째를 둔다. 끝까지 두는 대국에 쓴다.
type legalOpponent struct{}

func (legalOpponent) Choose(_ context.Context, startSFEN string, moves []string, _ skill.Estimate) (string, error) {
	pos, err := shogi.ParseSFEN(startSFEN)
	if err != nil {
		return "", err
	}
	for _, u := range moves {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			return "", err
		}
		pos = pos.Apply(m)
	}
	legal := pos.LegalMoves()
	if len(legal) == 0 {
		return "resign", nil
	}
	return legal[0].USI(), nil
}

func newSession(t *testing.T, cfg Config) *Session {
	t.Helper()
	s, err := New(t.Context(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// waitFor 는 조건을 만족하는 스냅샷이 올 때까지 구독 채널을 읽는다.
func waitFor(t *testing.T, ch <-chan Snapshot, cond func(Snapshot) bool, what string) Snapshot {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case snap, ok := <-ch:
			if !ok {
				t.Fatalf("%s 전에 세션이 닫힘", what)
			}
			if cond(snap) {
				return snap
			}
		case <-deadline:
			t.Fatalf("%s 대기 시간 초과", what)
		}
	}
}

func TestHumanMoveThenEngineReplies(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	snap, err := s.Play(t.Context(), "7g7f")
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if snap.Ply != 1 || snap.Moves[0].Ja != "▲7六歩" {
		t.Fatalf("사람 착수 반영 안 됨: %+v", snap.Moves)
	}

	got := waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "엔진 응수")
	if got.Moves[1].Ja != "△3四歩" || got.Moves[1].By != SideEngine {
		t.Fatalf("엔진 수 = %+v", got.Moves[1])
	}
	if !got.YourTurn || got.Thinking {
		t.Fatalf("엔진 응수 후에는 사람 차례여야 한다: %+v", got)
	}
	if len(got.LegalMoves) == 0 {
		t.Fatal("사람 차례인데 합법수가 비었다")
	}
}

// 클라이언트가 합법수만 보여줄 수 있어야 한다. 그게 반칙이 실사용자에게 안 닿는 이유다.
func TestSnapshotCarriesLegalMovesOnlyOnHumanTurn(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}, delay: 300 * time.Millisecond}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	snap, _ := s.Snapshot(t.Context())
	if len(snap.LegalMoves) != 30 {
		t.Fatalf("초기 국면 합법수 = %d (30 기대)", len(snap.LegalMoves))
	}

	// 엔진이 생각하는 동안에는 사람 차례가 아니다
	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	thinking, err := s.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if thinking.YourTurn || len(thinking.LegalMoves) != 0 {
		t.Fatalf("엔진 차례인데 합법수를 내보냈다: %+v", thinking)
	}
	if !thinking.Thinking {
		t.Fatal("엔진이 생각 중이라고 표시되지 않음")
	}
}

// 상태를 소유한 goroutine은 엔진이 생각하는 동안에도 막히면 안 된다.
func TestSessionAnswersWhileEngineThinks(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}, delay: time.Second}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}

	start := time.Now()
	if _, err := s.Snapshot(t.Context()); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, err := s.Resign(t.Context()); err != nil {
		t.Fatalf("Resign: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("탐색이 세션을 막았다: %v", elapsed)
	}
}

// 투료로 국면이 바뀐 뒤 도착한 탐색 결과는 그 국면에 대한 답이 아니다. 기보에 붙으면 안 된다.
// 판정 기준은 걸린 시간이 아니라 국면이다 — 오래 걸려도 국면이 그대로면 유효하다.
func TestStaleEngineResultIsDropped(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}, delay: 200 * time.Millisecond}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	snap, err := s.Resign(t.Context())
	if err != nil {
		t.Fatalf("Resign: %v", err)
	}
	if snap.Status != StatusResigned || snap.Winner != SideEngine {
		t.Fatalf("투료 결과 = %+v", snap)
	}

	time.Sleep(400 * time.Millisecond) // 탐색이 끝나고도 남을 시간
	after, err := s.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if after.Ply != 1 {
		t.Fatalf("투료 뒤에 엔진 수가 붙었다: %+v", after.Moves)
	}
	if after.Status != StatusResigned {
		t.Fatalf("상태가 바뀌었다: %s", after.Status)
	}
}

func TestIllegalMoveIsRejectedWithReason(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}}
	s := newSession(t, Config{
		Opponent:   opp,
		HumanColor: shogi.Black,
		StartSFEN:  "k8/9/9/9/9/9/4P4/9/4K4 b P 1",
	})

	_, err := s.Play(t.Context(), "P*5e")
	var ime *shogi.IllegalMoveError
	if !errors.As(err, &ime) {
		t.Fatalf("*IllegalMoveError 기대, got %v", err)
	}
	if ime.Reason != shogi.ReasonNifu {
		t.Fatalf("사유 = %v", ime.Reason)
	}
	if ime.Message() == "" {
		t.Fatal("화면에 내보낼 문구가 비었다")
	}

	// 거절된 수는 기보에 남지 않는다
	snap, _ := s.Snapshot(t.Context())
	if snap.Ply != 0 {
		t.Fatalf("불법수가 기보에 남았다: %+v", snap.Moves)
	}
}

func TestNotYourTurn(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}, delay: 300 * time.Millisecond}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if _, err := s.Play(t.Context(), "2g2f"); !errors.Is(err, ErrNotYourTurn) {
		t.Fatalf("ErrNotYourTurn 기대, got %v", err)
	}
}

// 詰み까지 실제로 두고 끝난다. D2의 완료 기준이 "한 판을 끝까지"다.
func TestGameEndsInCheckmate(t *testing.T) {
	// 후수 玉이 5一, 선수가 金을 5二에 두면 頭金으로 詰み
	opp := &scriptedOpponent{}
	s := newSession(t, Config{
		Opponent:   opp,
		HumanColor: shogi.Black,
		StartSFEN:  "4k4/9/4P4/9/9/9/9/9/4K4 b G 1",
	})

	snap, err := s.Play(t.Context(), "G*5b")
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if snap.Status != StatusCheckmate {
		t.Fatalf("詰み로 끝나야 한다: %+v", snap)
	}
	if snap.Winner != SideHuman {
		t.Fatalf("승자 = %q", snap.Winner)
	}
	if snap.YourTurn || len(snap.LegalMoves) != 0 {
		t.Fatalf("끝난 대국에 착수 여지가 남았다: %+v", snap)
	}
	if _, err := s.Play(t.Context(), "5i4i"); !errors.Is(err, ErrFinished) {
		t.Fatalf("ErrFinished 기대, got %v", err)
	}
}

// 엔진이 둘 수 없는 수를 돌려주면 기보를 깨뜨리지 않고 그 자리에서 끝낸다.
func TestUnplayableEngineMoveEndsGame(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"9i9h"}} // 그 칸에 선수 말이 없다
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	got := waitFor(t, ch, func(s Snapshot) bool { return s.Status != StatusPlaying }, "대국 종료")
	if got.Status != StatusResigned || got.Winner != SideHuman {
		t.Fatalf("결과 = %+v", got)
	}
	if got.Ply != 1 {
		t.Fatalf("둘 수 없는 수가 기보에 남았다: %+v", got.Moves)
	}
}

func TestEngineFailureEndsGame(t *testing.T) {
	opp := &scriptedOpponent{err: errors.New("boom")}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	got := waitFor(t, ch, func(s Snapshot) bool { return s.Status != StatusPlaying }, "대국 종료")
	if got.Winner != SideHuman {
		t.Fatalf("엔진 고장이면 사람이 이긴 것으로 끝난다: %+v", got)
	}
}

func TestEngineMovesFirstWhenHumanIsWhite(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"7g7f"}}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.White})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	got := waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 1 }, "엔진 선착")
	if got.Moves[0].Ja != "▲7六歩" || got.Moves[0].By != SideEngine {
		t.Fatalf("엔진 선착 = %+v", got.Moves[0])
	}
	if !got.YourTurn {
		t.Fatal("엔진이 두고 나면 사람 차례여야 한다")
	}
}

func TestClosedSessionRejectsCommands(t *testing.T) {
	opp := &scriptedOpponent{}
	s, err := New(t.Context(), Config{Opponent: opp, HumanColor: shogi.Black})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.Close()
	s.Close() // 두 번 닫아도 안전해야 한다

	if _, err := s.Snapshot(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("ErrClosed 기대, got %v", err)
	}
}

func TestNewRejectsMissingOpponent(t *testing.T) {
	if _, err := New(t.Context(), Config{}); err == nil {
		t.Fatal("Opponent 없이 세션이 만들어짐")
	}
	if _, err := New(t.Context(), Config{Opponent: legalOpponent{}, StartSFEN: "not-a-sfen"}); err == nil {
		t.Fatal("잘못된 SFEN이 통과함")
	}
}

// 엔진 없이 양쪽을 다 굴려 한 판이 실제로 끝나는지 본다.
func TestGameReachesAnEndWithoutHanging(t *testing.T) {
	s := newSession(t, Config{
		Opponent:   legalOpponent{},
		HumanColor: shogi.Black,
		StartSFEN:  "4k4/9/4P4/9/9/9/9/9/4K4 b G 1",
	})
	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "G*5b"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	got := waitFor(t, ch, func(s Snapshot) bool { return s.Status != StatusPlaying }, "대국 종료")
	if got.Status != StatusCheckmate {
		t.Fatalf("결과 = %+v", got)
	}
}

// engineOpponent 가 SearchResult.Best 를 그대로 돌려주는지.
type stubSearcher struct {
	res usi.SearchResult
	err error
}

func (s stubSearcher) SearchDepth(context.Context, string, []string, int) (usi.SearchResult, error) {
	return s.res, s.err
}

func TestEngineOpponentReturnsBest(t *testing.T) {
	o := NewEngineOpponent(stubSearcher{res: usi.SearchResult{Best: "7g7f"}}, 12)
	got, err := o.Choose(t.Context(), shogi.StartSFEN, nil, skill.Unknown)
	if err != nil || got != "7g7f" {
		t.Fatalf("Choose = %q, %v", got, err)
	}

	o2 := NewEngineOpponent(stubSearcher{err: errors.New("boom")}, 0)
	if _, err := o2.Choose(t.Context(), shogi.StartSFEN, nil, skill.Unknown); err == nil {
		t.Fatal("탐색 실패가 전달되지 않음")
	}
}

// fixedAnalyst 는 정해진 판정을 돌려준다. 엔진 없이 롤백 흐름만 본다.
type fixedAnalyst struct {
	verdict    intervene.Verdict
	refutation []RefutationMove
	facts      explain.Facts
	bestUSI    string
	threshold  float64
	evalBefore int
	evalAfter  int
	err        error
	delay      time.Duration
	calls      atomic.Int32
}

func (a *fixedAnalyst) Judge(ctx context.Context, startSFEN string, moves []string, _ int) (Judgement, error) {
	a.calls.Add(1)
	if a.delay > 0 {
		select {
		case <-time.After(a.delay):
		case <-ctx.Done():
			return Judgement{}, ctx.Err()
		}
	}
	// 실제 analyst 와 같은 규약으로 뒤집는다 — 여기서 그냥 넘기면 부호 테스트가 무의미해진다.
	j := Judgement{
		Verdict: a.verdict, Refutation: a.refutation, BestUSI: a.bestUSI,
		Facts: a.facts, Threshold: a.threshold,
	}
	if a.evalAfter != 0 || a.evalBefore != 0 {
		pos, _, err := replay(startSFEN, moves)
		if err == nil {
			j.SenteCpBefore = senteCp(a.evalBefore, pos.Turn)
			j.SenteCpAfter = senteCp(-a.evalAfter, pos.Turn)
			j.HasEvals = true
		}
	}
	return j, a.err
}

func blunder() intervene.Verdict {
	return intervene.Verdict{Kind: intervene.KindBlunder, DeltaWin: 0.42}
}

// 제지형 개입의 전부 — 두면 물러지고 이유가 뜬다. D3의 완료 기준이다.
func TestBlunderIsRolledBack(t *testing.T) {
	an := &fixedAnalyst{verdict: blunder()}
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
	got := waitFor(t, ch, func(s Snapshot) bool { return s.Intervention != nil }, "개입")

	if got.Ply != 0 || len(got.Moves) != 0 {
		t.Fatalf("물러지지 않았다: %+v", got.Moves)
	}
	if got.Intervention.RetractedUSI != "7g7f" || got.Intervention.RetractedJa != "▲7六歩" {
		t.Fatalf("물러진 수 기록이 틀렸다: %+v", got.Intervention)
	}
	if got.Intervention.Message == "" || hasHangulInGame(got.Intervention.Message) {
		t.Fatalf("화면 문구: %q", got.Intervention.Message)
	}
	if !got.YourTurn || got.Judging {
		t.Fatalf("물러진 뒤에는 다시 사람 차례여야 한다: %+v", got)
	}
	// **엔진이 두면 안 된다.** 물러질 수가 있는데 상대가 먼저 두면 되돌릴 것이 둘이 된다.
	if an.calls.Load() != 1 {
		t.Fatalf("판정 호출 = %d", an.calls.Load())
	}
	snap, _ := s.Snapshot(t.Context())
	if snap.Ply != 0 {
		t.Fatalf("엔진이 물러진 국면에서 뒀다: %+v", snap.Moves)
	}
}

// 반박 수순은 판정이 아니라 **화면에 그릴 재료**다. 세션은 손대지 않고 그대로 싣는다.
func TestRefutationRidesAlongToTheSnapshot(t *testing.T) {
	line := []RefutationMove{{USI: "3c3d", Ja: "△3四歩", By: SideEngine, SFEN: "after-3c3d"}}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}},
		Analyst:  &fixedAnalyst{verdict: blunder(), refutation: line}, HumanColor: shogi.Black,
	})
	ch, cancel, _ := s.Subscribe(t.Context())
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	got := waitFor(t, ch, func(s Snapshot) bool { return s.Intervention != nil }, "개입")

	if len(got.Intervention.Refutation) != 1 || got.Intervention.Refutation[0].USI != line[0].USI {
		t.Fatalf("반박 수순이 그대로 실리지 않았다: %+v", got.Intervention.Refutation)
	}
	// 기보는 물러진 상태 그대로다 — 반박 수순은 판에 둔 수가 아니다.
	if len(got.Moves) != 0 {
		t.Fatalf("반박 수순이 기보에 섞였다: %+v", got.Moves)
	}
}

func TestCleanMoveIsNotRolledBack(t *testing.T) {
	s := newSession(t, Config{
		Opponent:   &scriptedOpponent{moves: []string{"3c3d"}},
		Analyst:    &fixedAnalyst{},
		HumanColor: shogi.Black,
	})
	ch, cancel, _ := s.Subscribe(t.Context())
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	got := waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "엔진 응수")
	if got.Intervention != nil {
		t.Fatalf("멀쩡한 수에 개입했다: %+v", got.Intervention)
	}
}

// 판정 중에는 다음 수를 못 둔다 — 두면 물러질 수가 둘이 된다.
func TestCannotMoveWhileJudging(t *testing.T) {
	s := newSession(t, Config{
		Opponent:   &scriptedOpponent{moves: []string{"3c3d"}},
		Analyst:    &fixedAnalyst{delay: 300 * time.Millisecond},
		HumanColor: shogi.Black,
	})
	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	snap, _ := s.Snapshot(t.Context())
	if !snap.Judging || snap.YourTurn || len(snap.LegalMoves) != 0 {
		t.Fatalf("판정 중 상태가 틀렸다: %+v", snap)
	}
	if _, err := s.Play(t.Context(), "2g2f"); !errors.Is(err, ErrNotYourTurn) {
		t.Fatalf("판정 중 착수가 통과했다: %v", err)
	}
}

// 판정이 고장 나도 대국은 계속된다. 개입은 부가 기능이고 대국이 본체다.
func TestJudgeFailureLetsTheMoveStand(t *testing.T) {
	s := newSession(t, Config{
		Opponent:   &scriptedOpponent{moves: []string{"3c3d"}},
		Analyst:    &fixedAnalyst{err: errors.New("engine down")},
		HumanColor: shogi.Black,
	})
	ch, cancel, _ := s.Subscribe(t.Context())
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	got := waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "엔진 응수")
	if got.Intervention != nil {
		t.Fatalf("판정 실패로 개입했다: %+v", got.Intervention)
	}
}

// 다음 수를 두면 직전 개입 표시는 사라진다.
func TestInterventionClearsOnNextMove(t *testing.T) {
	an := &fixedAnalyst{verdict: blunder()}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: an,
		HumanColor: shogi.Black,
	})
	ch, cancel, _ := s.Subscribe(t.Context())
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	waitFor(t, ch, func(s Snapshot) bool { return s.Intervention != nil }, "개입")

	an.verdict = intervene.Verdict{} // 이번엔 통과시킨다
	snap, err := s.Play(t.Context(), "2g2f")
	if err != nil {
		t.Fatalf("다시 두기: %v", err)
	}
	if snap.Intervention != nil {
		t.Fatalf("새 수를 뒀는데 이전 개입이 남았다: %+v", snap.Intervention)
	}
}

// 물러진 국면은 千日手 계수까지 되돌아가야 한다. 남으면 다음 판정이 그 위에서 돈다.
func TestRollbackRestoresRepetitionCount(t *testing.T) {
	an := &fixedAnalyst{verdict: blunder()}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: an,
		HumanColor: shogi.Black,
	})
	ch, cancel, _ := s.Subscribe(t.Context())
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	got := waitFor(t, ch, func(s Snapshot) bool { return s.Intervention != nil }, "개입")
	if got.SFEN != shogi.StartSFEN {
		t.Fatalf("국면이 초기 상태로 안 돌아왔다: %s", got.SFEN)
	}
}

func hasHangulInGame(s string) bool {
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 {
			return true
		}
	}
	return false
}

// 관측 구간은 **기본값이 없다** — 첫 수부터 판정한다. 명시적으로 준 경우에만 건너뛴다.
//
// 원래 20수를 비워뒀는데 그건 「오프닝의 다양성을 인정한다」를 수 번호로 잘못 옮긴
// 것이었다. 다양성은 임계치가 지킨다(intervene 쪽 테스트).
func TestObservePliesIsOptInAndSkipsJudging(t *testing.T) {
	an := &fixedAnalyst{verdict: blunder()}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: an,
		HumanColor: shogi.Black, ObservePlies: 4,
	})
	ch, cancel, _ := s.Subscribe(t.Context())
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	got := waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "엔진 응수")
	if got.Intervention != nil {
		t.Fatalf("관측 구간에서 개입했다: %+v", got.Intervention)
	}
	if an.calls.Load() != 0 {
		t.Fatalf("관측 구간인데 판정을 %d번 불렀다", an.calls.Load())
	}
}

// 기본값에서는 첫 수부터 판정한다.
func TestJudgesFromTheFirstMoveByDefault(t *testing.T) {
	an := &fixedAnalyst{verdict: blunder()}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: an, HumanColor: shogi.Black,
	})
	ch, cancel, _ := s.Subscribe(t.Context())
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	got := waitFor(t, ch, func(s Snapshot) bool { return s.Intervention != nil }, "1수째 개입")
	if got.Intervention.RetractedUSI != "7g7f" {
		t.Fatalf("%+v", got.Intervention)
	}
}

// 갇힘 힌트는 **단계마다 실리는 것이 달라야 한다.** 첫 칸에서 수를 통째로 내려보내면
// 계단이 화면에만 있고 답은 페이로드에 그대로 있다.
func TestBuildHintStaysBehindItsStage(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stuck int
		best  string
		want  *Hint
	}{
		// **횟수를 여기 박지 않는다.** 지키는 것은 「칸마다 실리는 것이 다르다」이지
		// 2나 4라는 값이 아니다 — 값은 실측으로 움직인다(06-status.md §39).
		{"아직 안 열린다", HintPieceAfter - 1, "5d5f", nil},
		{"첫 칸 — 칸만", HintPieceAfter, "5d5f", &Hint{Square: "5d"}},
		{"그 사이 — 그대로", HintMoveAfter - 1, "5d5f", &Hint{Square: "5d"}},
		{"윗칸 — 수까지", HintMoveAfter, "5d5f", &Hint{Square: "5d", USI: "5d5f"}},
		{"打는 駒台를 짚는다", HintPieceAfter, "B*4a", &Hint{Drop: "B"}},
		{"打도 윗칸에 수까지", HintMoveAfter, "B*4a", &Hint{Drop: "B", USI: "B*4a"}},
		// 최선수를 못 구했으면 힌트도 없다. 판정이 고장 나도 대국은 계속된다는 것과 같은 판단이다.
		{"최선수가 없으면 없다", 9, "", nil},
		{"읽을 수 없으면 없다", 9, "zzzz", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := buildHint(tc.stuck, tc.best)
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("없어야 한다: %+v", got)
			case tc.want == nil:
				return
			case got == nil:
				t.Fatalf("있어야 한다: %+v", tc.want)
			case *got != *tc.want:
				t.Fatalf("%+v, want %+v", *got, *tc.want)
			}
		})
	}
}

// 갇힘 힌트는 **같은 국면에서 연속으로** 물러진 횟수로 열린다.
//
// 한 판 누적으로 세면 서로 다른 이유로 실수한 사람에게 엉뚱한 힌트가 열린다. 그래서
// 이 테스트가 지키는 것은 계단이 열리는 것과, **통과하는 수 하나로 닫히는 것** 둘이다.
func TestStuckHintOpensAndResets(t *testing.T) {
	an := &fixedAnalyst{verdict: blunder(), bestUSI: "2g2f"}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: an,
		HumanColor: shogi.Black,
	})
	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	// 같은 국면에서 계속 물러진다. 되무르기가 국면을 그대로 되돌리므로 매번 같은 수를 쓴다.
	bounce := func(n int) Snapshot {
		t.Helper()
		if _, err := s.Play(t.Context(), "7g7f"); err != nil {
			t.Fatalf("%d회 Play: %v", n, err)
		}
		return waitFor(t, ch, func(s Snapshot) bool { return s.Intervention != nil }, "개입")
	}

	for i := 1; i < HintPieceAfter; i++ {
		if got := bounce(i); got.Hint != nil {
			t.Fatalf("%d회에는 아직 없어야 한다: %+v", i, got.Hint)
		}
	}

	// 첫 칸 — 駒만. **수는 오지 않는다.** 오면 계단이 화면에만 있고 답은 페이로드에 있다.
	got := bounce(HintPieceAfter)
	if got.Hint == nil || got.Hint.Square != "2g" {
		t.Fatalf("%d회에 칸을 짚어야 한다: %+v", HintPieceAfter, got.Hint)
	}
	if got.Hint.USI != "" {
		t.Fatalf("%d회에 수가 실렸다: %+v", HintPieceAfter, got.Hint)
	}

	for i := HintPieceAfter + 1; i < HintMoveAfter; i++ {
		if got := bounce(i); got.Hint == nil || got.Hint.USI != "" {
			t.Fatalf("%d회는 %d회와 같아야 한다: %+v", i, HintPieceAfter, got.Hint)
		}
	}

	// 윗칸 — 수까지.
	if got := bounce(HintMoveAfter); got.Hint == nil || got.Hint.USI != "2g2f" {
		t.Fatalf("%d회에 수를 줘야 한다: %+v", HintMoveAfter, got.Hint)
	}

	// 통과하는 수 하나로 닫힌다.
	an.verdict = intervene.Verdict{}
	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("통과할 Play: %v", err)
	}
	// 상대 응수까지 기다린다 — 그 전에는 내 차례가 아니다.
	waitFor(t, ch, func(s Snapshot) bool { return s.YourTurn && s.Ply >= 2 }, "상대 응수 뒤 내 차례")

	// **여기서 「힌트가 없다」만 보면 아무것도 안 지킨다** — `playHuman` 이 착수마다
	// 힌트를 지우므로 계수가 6이어도 그 스냅샷은 비어 있다. 계수가 실제로 0으로
	// 돌아갔는지는 **한 번 더 물러져 봐야** 갈린다. 안 돌아갔으면 그 한 번이 윗칸을 넘겨
	// 곧바로 수까지 실린 힌트가 온다.
	an.verdict = blunder()
	if _, err := s.Play(t.Context(), "2g2f"); err != nil {
		t.Fatalf("새 국면에서 Play: %v", err)
	}
	again := waitFor(t, ch, func(s Snapshot) bool { return s.Intervention != nil }, "새 국면의 개입")
	if again.Hint != nil {
		t.Fatalf("계수가 0으로 안 돌아갔다 — 1회에 힌트가 떴다: %+v", again.Hint)
	}
}

// 평가치는 **先手 관점**으로 기보에 들어간다.
//
// 부호를 틀리면 아무 데서도 안 터진다 — 궤적이 상하로 뒤집힌 채 그려지고, 밴드가
// 지켜졌는지 물었을 때 정확히 반대 답이 나온다. 그래서 사람이 後手인 판까지 본다.
func TestEvalsAreRecordedFromSentesSide(t *testing.T) {
	for _, tc := range []struct {
		name  string
		human shogi.Color
		// 사람이 두는 수, 그리고 상대가 둘 수들. 상대의 색이 반대라 목록도 갈린다.
		first  string
		engine []string
		// 사람 관점 +200 을 겨냥한다. 사람이 先手면 그대로, 後手면 뒤집혀 들어간다.
		wantAfter int
	}{
		{"사람이 先手", shogi.Black, "7g7f", []string{"3c3d"}, +200},
		{"사람이 後手", shogi.White, "3c3d", []string{"7g7f", "2g2f"}, -200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// after 는 **상대 관점**으로 오는 값이라, 사람 관점 +200 은 상대 관점 −200 이다.
			an := &fixedAnalyst{evalAfter: -200, evalBefore: 50}
			rec := &fakeRecorder{}
			cfg := Config{
				Opponent: &scriptedOpponent{moves: tc.engine},
				Analyst:  an, HumanColor: tc.human, Recorder: rec,
			}
			s := newSession(t, cfg)
			ch, cancel, err := s.Subscribe(t.Context())
			if err != nil {
				t.Fatalf("Subscribe: %v", err)
			}
			defer cancel()

			// 사람이 後手면 상대가 먼저 둔다. 내 차례가 될 때까지 기다린다.
			waitFor(t, ch, func(s Snapshot) bool { return s.YourTurn }, "내 차례")
			if _, err := s.Play(t.Context(), tc.first); err != nil {
				t.Fatalf("Play: %v", err)
			}
			waitFor(t, ch, func(s Snapshot) bool { return len(s.Moves) >= 1 && !s.Judging }, "착수 확정")

			var got []string
			for _, l := range rec.all() {
				if strings.HasPrefix(l, "eval ") {
					got = append(got, l)
				}
			}
			if len(got) == 0 {
				t.Fatalf("평가치가 기록되지 않았다: %v", rec.all())
			}
			// 사람의 수 뒤 평가치. ply 는 사람이 後手면 2다.
			ply := 1
			if tc.human == shogi.White {
				ply = 2
			}
			want := fmt.Sprintf("eval %d %+d", ply, tc.wantAfter)
			if got[0] != want {
				t.Fatalf("%q, want %q (전체 %v)", got[0], want, got)
			}
		})
	}
}
