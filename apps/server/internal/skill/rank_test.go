package skill

import "testing"

// 척도의 가운데가 8級이어야 한다. 한쪽 끝으로 몰리면 처음 붙는 이름이 이미 판정처럼
// 읽힌다(rankNames).
func TestMiddleOfTheScaleIsEightKyu(t *testing.T) {
	got, ok := RankOf(named(RankLossScale / 2))
	if !ok {
		t.Fatal("표본이 찼는데 이름이 없다")
	}
	if got.NameJa != "8級" {
		t.Errorf("척도 가운데의 이름 = %q, want 8級", got.NameJa)
	}
	if got.Step*2 != RankMax {
		t.Errorf("Step = %d, 척도의 가운데(%d)가 아니다", got.Step, RankMax/2)
	}
}

// 표본이 모자라면 이름을 안 붙인다. 0을 돌려주면 화면이 그것을 가장 낮은 이름으로 그린다.
func TestNoNameBeforeEnoughSamples(t *testing.T) {
	if _, ok := RankOf(Estimate{AbsLoss: 0.05, AbsSamples: MinSamples - 1}); ok {
		t.Error("표본이 모자란데 이름이 붙었다")
	}
	if _, ok := RankOf(Unknown); ok {
		t.Error("아무것도 안 본 추정치에 이름이 붙었다")
	}
}

// 절대 낙폭이 없는 프로파일에는 이름을 안 붙인다. 014_skill_absolute_loss.sql 전에 쌓인
// 행은 Samples 가 차 있는데 그 칸이 비어 있고, 그것을 「낙폭 0」으로 읽으면 아무것도 안
// 재고 가장 센 이름을 붙이게 된다.
func TestNoNameWhenOnlyTheNormalizedLossIsKnown(t *testing.T) {
	if got, ok := RankOf(Estimate{Loss: 0.4, Samples: 40}); ok {
		t.Errorf("절대 낙폭이 없는데 %q 가 붙었다", got.NameJa)
	}
}

// 낙폭이 커질수록 이름이 약해져야 한다. 부호를 뒤집는 자리가 RankOf 한 곳뿐이라,
// 여기가 깨지면 화면 전체가 반대로 간다.
func TestWorseLossNeverGivesAStrongerName(t *testing.T) {
	prev := RankMax + 1
	for i := 0; i <= 20; i++ {
		abs := RankLossScale * float64(i) / 20
		got, ok := RankOf(named(abs))
		if !ok {
			t.Fatalf("absLoss=%.3f 에 이름이 없다", abs)
		}
		if got.Step > prev {
			t.Errorf("absLoss=%.3f 에서 이름이 세졌다: Step %d → %d", abs, prev, got.Step)
		}
		prev = got.Step
	}
}

// 밖에서 온 값이 척도를 벗어나게 하면 안 된다 — 저장해 둔 값이 DB에서 오고(store),
// NewTrackFrom 과 같은 이유로 여기서도 자른다.
func TestOutOfRangeLossStaysOnTheScale(t *testing.T) {
	for _, abs := range []float64{-3, -0.001, 1.001, 42} {
		got, ok := RankOf(named(abs))
		if !ok {
			t.Fatalf("absLoss=%v 에 이름이 없다", abs)
		}
		if got.Step < 0 || got.Step > RankMax {
			t.Errorf("absLoss=%v 의 Step = %d, 척도 밖이다", abs, got.Step)
		}
		if got.NameJa == "" {
			t.Errorf("absLoss=%v 의 이름이 비었다", abs)
		}
	}
}

// 양 끝이 척도의 양 끝이어야 한다.
func TestEndsOfTheScale(t *testing.T) {
	best, _ := RankOf(named(0))
	if best.NameJa != "初段" || best.Step != RankMax {
		t.Errorf("낙폭 0의 이름 = %q(%d), want 初段(%d)", best.NameJa, best.Step, RankMax)
	}
	worst, _ := RankOf(named(RankLossScale))
	if worst.NameJa != "16級" || worst.Step != 0 {
		t.Errorf("낙폭 %v의 이름 = %q(%d), want 16級(0)", RankLossScale, worst.NameJa, worst.Step)
	}
}

// 같은 실수가 레벨이 갈려도 같은 이름이어야 한다. 정규화값으로 세면 네 계급 움직이고
// (journal §92의 표) 그 순간 실측 앵커가 통째로 낡는다.
func TestNameDoesNotDependOnTheThresholdThatJudged(t *testing.T) {
	const drop = 0.06 // 승률 0.06 손해. 어느 레벨에서도 통과하는 크기다

	name := func(threshold float64) string {
		tr := NewTrack()
		var e Estimate
		for range MinSamples {
			e = tr.Observe(Move{DeltaWin: drop, Threshold: threshold})
		}
		got, ok := RankOf(e)
		if !ok {
			t.Fatalf("임계치 %v에서 이름이 없다", threshold)
		}
		return got.NameJa
	}

	beginner, intermediate := name(0.25), name(0.12)
	if beginner != intermediate {
		t.Errorf("같은 낙폭에 이름이 갈렸다: beginner=%s intermediate=%s", beginner, intermediate)
	}
}

// named 는 이름이 붙을 만큼의 표본을 가진 추정치다. 段級은 절대 낙폭만 보므로 Loss 는
// 안 채운다 — 채우면 어느 값이 이름을 만들었는지가 테스트에서 안 보인다.
func named(absLoss float64) Estimate {
	return Estimate{AbsLoss: absLoss, AbsSamples: MinSamples}
}
