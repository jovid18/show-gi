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
// 임계치를 beginner(0.25)로 두지 않는다. 그 값으로 절반을 잃으면 정규화 결과가 딱
// PriorLoss 가 되어 EWMA가 제자리에 있고, 그러면 m.DeltaWin / 0.25 로 못 박은 구현도
// 이 테스트를 통과한다 — Move.Threshold 주석이 경계하는 바로 그 결합이다.
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
	// beginner 임계치로 나눴다면 0.36으로 내려가는 쪽이 걸린다 — 방향까지 갈린다.
	if got <= PriorLoss {
		t.Fatalf("임계치를 0.25로 못 박은 것과 구별되지 않는다: %.4f", got)
	}
}

// 판정의 두 탐색은 뿌리가 한 수 다르다(journal §41) — 낙폭이 음수로 나올 수 있다.
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

// 이어 시작하는 판. 표본이 차 있으면 첫 판정 전부터 밴드가 움직인다 — 그것이
// skill_profile 을 채운 이유이고(journal §47), 안 되면 매 판 기준선에서 다시 시작한다.
func TestNewTrackFromResumes(t *testing.T) {
	got := NewTrackFrom(Estimate{Loss: 0.8, Samples: 12}).Estimate()
	if got.Loss != 0.8 || got.Samples != 12 {
		t.Errorf("%+v, want {0.8 12}", got)
	}
	if !got.Ready() {
		t.Error("표본 12로 시작했는데 아직 안 움직인다")
	}
}

// 표본이 없으면 저장된 낙폭을 안 믿는다. 0건에서 온 값은 아무것도 안 본 값이라
// 그것으로 상대를 옮기면 근거 없이 세거나 약해진다.
func TestNewTrackFromIgnoresEmpty(t *testing.T) {
	for _, e := range []Estimate{{}, {Loss: 0.9}, {Loss: 0.9, Samples: -1}} {
		if got := NewTrackFrom(e).Estimate(); got != Unknown {
			t.Errorf("NewTrackFrom(%+v) = %+v, want %+v", e, got, Unknown)
		}
	}
}

// 저장된 값이 범위 밖이면 자른다. DB는 밖이라 1을 넘는 낙폭 하나가 밴드를 임의로 민다.
func TestNewTrackFromClamps(t *testing.T) {
	for _, c := range []struct{ in, want float64 }{{2.5, 1}, {-1, 0}} {
		if got := NewTrackFrom(Estimate{Loss: c.in, Samples: 5}).Estimate().Loss; got != c.want {
			t.Errorf("Loss %v → %v, want %v", c.in, got, c.want)
		}
	}
}

// 이어 시작한 판은 첫 수 전에 한 번 올려보낸다. 안 올리면 지난 값이 있는데도 첫 판정까지
// 상대가 기준선으로 두고, 그 한 수가 이 기능이 있는 이유다.
func TestWorkerPushesResumedEstimateBeforeAnyMove(t *testing.T) {
	w := NewWorkerFrom(t.Context(), Estimate{Loss: 0.9, Samples: 5}, nil)
	select {
	case got := <-w.Estimates():
		if got.Loss != 0.9 || got.Samples != 5 {
			t.Errorf("%+v, want {0.9 5}", got)
		}
	case <-time.After(time.Second):
		t.Fatal("이어 시작한 값이 안 왔다")
	}
}

// 아무것도 모르는 판에서는 아무것도 안 올린다. 기준선 밴드가 곧 「모름」이라, 여기서
// 값을 올리면 화면이 조절 중이라고 말하기 시작한다(Snapshot.OpponentStrength).
func TestWorkerStaysQuietWhenUnknown(t *testing.T) {
	w := NewWorkerFrom(t.Context(), Unknown, nil)
	select {
	case got := <-w.Estimates():
		t.Errorf("안 왔어야 하는데 %+v 가 왔다", got)
	case <-time.After(50 * time.Millisecond):
	}
}

// onChange 는 판정마다 불린다. 끝에 한 번이 아니다 — 새로고침하면 판이 끝나므로
// 몰아 쓰면 끊긴 판의 추정이 통째로 사라진다(query/skill.sql).
func TestWorkerReportsEveryObservation(t *testing.T) {
	seen := make(chan Estimate, 4)
	w := NewWorkerFrom(t.Context(), Unknown, func(e Estimate) { seen <- e })
	for range 2 {
		w.Observe(Move{Blunder: true})
	}
	for i := range 2 {
		select {
		case got := <-seen:
			if got.Samples != i+1 {
				t.Errorf("%d번째 보고의 Samples = %d, want %d", i+1, got.Samples, i+1)
			}
		case <-time.After(time.Second):
			t.Fatalf("%d번째 보고가 안 왔다", i+1)
		}
	}
}

// 절대 낙폭은 평균이다. 段級이 이 값에서 나오므로(rank.go) 「최근 몇 수」가 아니라
// 「이 판 전체」여야 하고, 그것이 비대칭 EMA와 갈라 둔 이유다.
func TestAbsLossIsTheMeanOfRawDrops(t *testing.T) {
	tr := NewTrack()
	for _, d := range []float64{0.1, 0, 0.2} {
		tr.Observe(Move{DeltaWin: d, Threshold: beginnerThreshold})
	}
	got := tr.Estimate()
	if math.Abs(got.AbsLoss-0.1) > 1e-9 {
		t.Errorf("AbsLoss = %.4f, want 0.1", got.AbsLoss)
	}
	if got.AbsSamples != 3 {
		t.Errorf("AbsSamples = %d, want 3", got.AbsSamples)
	}
	// 같은 세 수의 EMA는 마지막 수에 끌려 훨씬 위에 있다. 두 값이 같아지면 이 축이
	// 정규화 축을 그냥 베낀 것이다.
	if math.Abs(got.Loss-got.AbsLoss) < 0.1 {
		t.Errorf("두 축이 구별되지 않는다: Loss=%.4f AbsLoss=%.4f", got.Loss, got.AbsLoss)
	}
}

// 절대 낙폭에는 분모가 없다. 임계치가 갈려도 같은 값이어야 실측 앵커가 안 낡는다
// (journal §92).
func TestAbsLossIgnoresTheThreshold(t *testing.T) {
	abs := func(threshold float64) float64 {
		tr := NewTrack()
		return tr.Observe(Move{DeltaWin: 0.06, Threshold: threshold}).AbsLoss
	}
	if beginner, intermediate := abs(0.25), abs(0.12); beginner != intermediate {
		t.Errorf("임계치가 절대값을 움직였다: %.4f vs %.4f", beginner, intermediate)
	}
}

// 詰み을 놓친 수는 승률이 거의 안 움직인다(Move.Blunder). 절대 축에서도 잘 둔 수로
// 들어가면 안 되므로 임계치를 바닥으로 놓는다.
func TestLostMateIsNotFreeInTheAbsoluteAxis(t *testing.T) {
	mate := Move{Blunder: true, DeltaWin: 0.01, Threshold: beginnerThreshold}
	if got := NewTrack().Observe(mate).AbsLoss; got != beginnerThreshold {
		t.Errorf("AbsLoss = %.4f, want %v", got, beginnerThreshold)
	}
	// 낙폭으로 걸린 블런더는 그 자리가 안 걸린다 — 이미 임계치 이상이라 값이 그대로다.
	if got := NewTrack().Observe(blunder()).AbsLoss; got != 0.42 {
		t.Errorf("낙폭 블런더의 AbsLoss = %.4f, want 0.42", got)
	}
}

// 이어 두는 판에서 평균이 이어져야 한다. 합으로 되돌리지 않으면 판이 바뀔 때마다
// 이번 판의 몇 수가 지난 수십 수와 같은 무게가 된다.
func TestNewTrackFromResumesTheAbsoluteMean(t *testing.T) {
	tr := NewTrackFrom(Estimate{Loss: 0.5, Samples: 4, AbsLoss: 0.1, AbsSamples: 4})
	got := tr.Observe(Move{DeltaWin: 0, Threshold: beginnerThreshold})
	if math.Abs(got.AbsLoss-0.08) > 1e-9 || got.AbsSamples != 5 {
		t.Errorf("%+v, want AbsLoss 0.08 · AbsSamples 5", got)
	}
}

// 그 칸이 없던 시절의 프로파일은 0에서 다시 센다. Samples 를 개수로 쓰면 없는 낙폭을
// 0으로 세는 것이 되고, 0은 「매 수 최선」이라 가장 센 이름이 붙는다(rank.go).
func TestNewTrackFromRestartsTheAbsoluteMeanWhenItWasNeverStored(t *testing.T) {
	tr := NewTrackFrom(Estimate{Loss: 0.8, Samples: 12})
	got := tr.Observe(Move{DeltaWin: 0.2, Threshold: beginnerThreshold})
	if math.Abs(got.AbsLoss-0.2) > 1e-9 || got.AbsSamples != 1 {
		t.Errorf("%+v, want AbsLoss 0.2 · AbsSamples 1", got)
	}
	if got.Samples != 13 {
		t.Errorf("Samples = %d — 정규화 축은 이어져야 한다", got.Samples)
	}
}
