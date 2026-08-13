package game

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 대국 중 엔진 탐색의 시한 — **탐색 하나가 판 전체를 붙들지 못한다**가 여기 전부다.
//
// 사람이 둔 첫 판이 정확히 그것으로 멈췄다(playtests/2026-08-13-human-1.md #2 · #9).
// 시한은 결과를 **버리는** 것이지 자르는 것이 아니라서, 확인할 것은 「제때 포기하는가」와
// 「포기한 뒤 판이 어떻게 되는가」 둘이다.

// 부가 기능이 대국보다 오래 풀을 붙들면 안 된다. 값이 뒤집히면 이 파일의 나머지가
// 전부 참인 채로 제품만 조용히 옛날로 돌아간다.
func TestExtraDeadlineIsShorterThanTheMoveDeadline(t *testing.T) {
	if DefaultExtraDeadline >= DefaultMoveDeadline {
		t.Fatalf("부가 경로(%v)가 대국 경로(%v)보다 오래 붙든다", DefaultExtraDeadline, DefaultMoveDeadline)
	}
}

// 상대 수 탐색이 안 돌아오면 **판이 접힌다.** 매달려 있지도 않고, 승패를 지어내지도 않는다.
func TestOpponentSearchDeadlineAbortsTheGame(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}, delay: 5 * time.Second}
	s := newSession(t, Config{
		Opponent: opp, HumanColor: shogi.Black,
		MoveDeadline: 50 * time.Millisecond,
	})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}

	start := time.Now()
	got := waitFor(t, ch, func(s Snapshot) bool { return s.Status != StatusPlaying }, "시한 초과로 대국 종료")
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("시한이 안 걸렸다 — %v 를 기다렸다", elapsed)
	}
	if got.Status != StatusAborted || got.Winner != "" {
		t.Fatalf("시한 초과에 승패를 붙였다: status=%q winner=%q", got.Status, got.Winner)
	}
	if got.Thinking {
		t.Fatal("접힌 판이 아직 생각 중이라고 말한다")
	}
}

// 판정이 안 돌아오면 **수는 그대로 서고 대국이 이어진다.** 개입은 부가이고 대국이 본체다.
// 대신 아무 말도 안 하지 않는다 — 개입이 없는 화면은 「괜찮은 수」와 똑같이 생겼다.
func TestJudgeDeadlineLetsTheMoveStandWithANotice(t *testing.T) {
	an := &fixedAnalyst{verdict: blunder(), delay: 5 * time.Second}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Analyst: an,
		HumanColor: shogi.Black, MoveDeadline: 50 * time.Millisecond,
	})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}

	// 시한을 넘긴 판정은 **블런더라고 답할 예정이었다.** 그래도 물러지지 않는다 —
	// 안 돌아온 답으로 기보를 고치는 것이 시한을 거는 것보다 나쁘다.
	got := waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "상대 응수")
	if got.Intervention != nil {
		t.Fatalf("답을 못 받은 판정으로 되물렀다: %+v", got.Intervention)
	}
	if got.Moves[0].USI != "7g7f" {
		t.Fatalf("판정을 못 한 수가 기보에서 사라졌다: %+v", got.Moves)
	}
	if got.Notice == nil || got.Notice.Code != NoticeJudgeSkipped {
		t.Fatalf("알림 = %+v", got.Notice)
	}
}

// 게이지는 **조용히** 없어진다. 테두리가 어두운 채로 남고 대국은 그대로 간다.
func TestGaugeDeadlineLeavesTheBorderDark(t *testing.T) {
	mate := &scriptedMate{plies: 3, delay: 5 * time.Second}
	s := newSession(t, Config{
		Opponent: &scriptedOpponent{moves: []string{"3c3d"}}, Mate: mate,
		HumanColor: shogi.Black, ExtraDeadline: 50 * time.Millisecond,
	})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	got := waitFor(t, ch, func(s Snapshot) bool { return s.YourTurn && s.Ply == 2 }, "상대 응수")
	if got.MateHeat != 0 {
		t.Fatalf("안 돌아온 탐색으로 게이지를 켰다: %d", got.MateHeat)
	}
	if got.Notice != nil {
		t.Fatalf("부가 기능의 실패를 사람에게 말했다: %+v", got.Notice)
	}
}

// expiringSearch 는 **첫 탐색 뒤에 시한이 끝난** 상황을 만든다. 게이트가 후보를 하나씩
// 도는 도중에 ctx가 끝나는 것이 실제 모양이다.
type expiringSearch struct {
	cancel context.CancelFunc
	calls  int
}

func (s *expiringSearch) SearchMultiPV(ctx context.Context, _ string, _ []string, _, _ int) (usi.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return usi.SearchResult{}, err
	}
	s.calls++
	if s.calls == 1 {
		return usi.SearchResult{ScoreCp: 100}, nil // 착수 전 국면
	}
	s.cancel() // 후보 하나를 재고 나서 시한이 끝난다
	return usi.SearchResult{ScoreCp: -100}, nil
}

// 시한이 끝나면 **남은 후보를 세어서 돌려준다.** 조용히 넘기면 「手筋이 없었다」와
// 「못 물어봤다」가 같은 결과가 되고, 그건 상한에 걸린 쪽을 세는 이유와 같다.
func TestGateCountsCandidatesItCouldNotAskAboutAfterTheDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s := &expiringSearch{cancel: cancel}

	opts := []TesujiOption{{USI: "6g5e"}, {USI: "6g7e"}, {USI: "2g2f"}}
	kept, dropped, err := gateTesujiOptions(ctx, s, 12, shogi.StartSFEN, nil, opts, shogi.Black)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("시한 초과가 에러로 안 나왔다: %v", err)
	}
	if dropped != len(opts)-1 {
		t.Fatalf("못 물어본 후보 = %d, 기대 %d (kept=%+v)", dropped, len(opts)-1, kept)
	}
}
