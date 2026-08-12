package skill

import (
	"context"
	"math"
	"testing"
	"time"
)

const beginnerThreshold = 0.25 // intervene.Beginner.Threshold(). 이 패키지는 그쪽을 모른다

func clean() Move   { return Move{DeltaWin: 0, Threshold: beginnerThreshold} }
func blunder() Move { return Move{Blunder: true, DeltaWin: 0.42, Threshold: beginnerThreshold} }

// 도움은 그 자리에서 와야 하고, 「이제 잘 둔다」는 판단은 천천히 해야 한다.
func TestOneBlunderMovesMoreThanOneGoodMove(t *testing.T) {
	up := NewTrack()
	up.Observe(blunder())
	rose := up.Estimate().Loss - PriorLoss

	down := NewTrack()
	down.Observe(clean())
	fell := PriorLoss - down.Estimate().Loss

	if rose <= fell {
		t.Fatalf("블런더가 더 크게 움직여야 한다: 올라간 폭=%.3f 내려간 폭=%.3f", rose, fell)
	}
}

// 詰み을 놓친 수는 승률이 거의 안 움직인다. 낙폭만 보면 잘 둔 수로 들어온다.
func TestLostMateCountsAsFullLossThoughWinRateBarelyMoves(t *testing.T) {
	mate := Move{Blunder: true, DeltaWin: 0.01, Threshold: beginnerThreshold}

	withMate := NewTrack()
	withMate.Observe(mate)

	withBlunder := NewTrack()
	withBlunder.Observe(blunder())

	if withMate.Estimate().Loss != withBlunder.Estimate().Loss {
		t.Fatalf("종반 블런더가 낙폭 블런더보다 가볍게 셌다: %.3f vs %.3f",
			withMate.Estimate().Loss, withBlunder.Estimate().Loss)
	}
}

// 통과한 수도 값을 갖는다 — 임계치의 4분의 3을 잃었으면 0.75다.
//
// **임계치를 beginner(0.25)로 두지 않는다.** 그 값으로 절반을 잃으면 정규화 결과가 딱
// PriorLoss 가 되어 EWMA가 제자리에 있고, 그러면 `m.DeltaWin / 0.25` 로 못 박은 구현도
// 이 테스트를 통과한다 — `Move.Threshold` 주석이 경계하는 바로 그 결합이다.
func TestPassedMoveIsScaledByTheThresholdThatJudgedIt(t *testing.T) {
	const intermediateThreshold = 0.12 // intervene.Intermediate.Threshold()
	lost := Move{DeltaWin: intermediateThreshold * 0.75, Threshold: intermediateThreshold}

	tr := NewTrack()
	got := tr.Observe(lost).Loss

	// 0.75 > PriorLoss 라 오르는 쪽 비율이 걸린다.
	want := PriorLoss + RiseRate*(0.75-PriorLoss)
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("정규화가 판정에 쓰인 임계치를 안 썼다: got=%.4f want=%.4f", got, want)
	}
	// beginner 임계치로 나눴다면 0.36으로 **내려가는** 쪽이 걸린다 — 방향까지 갈린다.
	if got <= PriorLoss {
		t.Fatalf("임계치를 0.25로 못 박은 것과 구별되지 않는다: %.4f", got)
	}
}

// 판정의 두 탐색은 뿌리가 한 수 다르다(06-status.md §41) — 낙폭이 음수로 나올 수 있다.
func TestLossStaysInRange(t *testing.T) {
	for _, m := range []Move{
		{DeltaWin: -0.4, Threshold: beginnerThreshold},
		{DeltaWin: 9, Threshold: beginnerThreshold},
		{DeltaWin: 0.3, Threshold: 0}, // 임계치를 모르는 경우
	} {
		tr := NewTrack()
		if l := tr.Observe(m).Loss; l < 0 || l > 1 {
			t.Fatalf("%+v 에서 낙폭이 범위를 벗어났다: %.3f", m, l)
		}
	}
}

func TestNotReadyUntilEnoughMoves(t *testing.T) {
	tr := NewTrack()
	for i := 1; i < MinSamples; i++ {
		if e := tr.Observe(clean()); e.Ready() {
			t.Fatalf("%d수만 보고 준비됐다고 했다", i)
		}
	}
	if e := tr.Observe(clean()); !e.Ready() {
		t.Fatalf("%d수를 봤는데 아직 안 됐다고 한다", MinSamples)
	}
}

// 매 수 블런더면 1로, 매 수 최선이면 0으로 간다 — 양 끝을 넘지 않는다.
func TestConvergesToTheEnds(t *testing.T) {
	worst, best := NewTrack(), NewTrack()
	for range 200 {
		worst.Observe(blunder())
		best.Observe(clean())
	}
	if l := worst.Estimate().Loss; l < 0.99 || l > 1 {
		t.Errorf("계속 블런더인데 1로 안 갔다: %.4f", l)
	}
	if l := best.Estimate().Loss; l > 0.01 || l < 0 {
		t.Errorf("계속 최선인데 0으로 안 갔다: %.4f", l)
	}
}

func TestWorkerEstimatesWhatItWasGiven(t *testing.T) {
	w := NewWorker(t.Context())
	for range 4 {
		w.Observe(blunder())
	}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case e := <-w.Estimates():
			if e.Samples < 4 {
				continue // 최신 값만 남기므로 중간 값은 안 올 수 있다
			}
			if e.Loss <= PriorLoss {
				t.Fatalf("블런더 4수인데 낙폭이 안 올랐다: %.3f", e.Loss)
			}
			return
		case <-deadline:
			t.Fatal("추정치가 안 왔다")
		}
	}
}

// 세션 goroutine 이 여기서 막히면 그동안 착수도 투료도 못 받는다. 소비자가 죽어 있어도
// 돌아와야 한다 — 그것이 「큐가 차면 버린다」의 뜻이다.
func TestObserveNeverBlocksWhenNobodyConsumes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 소비자가 시작하자마자 끝난다
	w := NewWorker(ctx)

	done := make(chan struct{})
	go func() {
		for range queueSize * 4 {
			w.Observe(blunder())
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Observe 가 막혔다")
	}
}
