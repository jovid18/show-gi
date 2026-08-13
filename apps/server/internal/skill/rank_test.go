package skill

import "testing"

// 아무것도 모르는 사람에게 붙는 이름이 척도의 가운데여야 한다. 한쪽 끝이면 첫 화면이
// 이미 판정처럼 읽힌다(rankNames).
func TestPriorLandsOnTheMiddleName(t *testing.T) {
	got, ok := RankOf(Estimate{Loss: PriorLoss, Samples: MinSamples})
	if !ok {
		t.Fatal("표본이 찼는데 이름이 없다")
	}
	if got.NameJa != "8級" {
		t.Errorf("PriorLoss 의 이름 = %q, want 8級", got.NameJa)
	}
	if got.Step*2 != RankMax {
		t.Errorf("Step = %d, 척도의 가운데(%d)가 아니다", got.Step, RankMax/2)
	}
}

// 표본이 모자라면 이름을 안 붙인다. 0을 돌려주면 화면이 그것을 가장 낮은 이름으로 그린다.
func TestNoNameBeforeEnoughSamples(t *testing.T) {
	if _, ok := RankOf(Estimate{Loss: 0.2, Samples: MinSamples - 1}); ok {
		t.Error("표본이 모자란데 이름이 붙었다")
	}
	if _, ok := RankOf(Unknown); ok {
		t.Error("아무것도 안 본 추정치에 이름이 붙었다")
	}
}

// 낙폭이 커질수록 이름이 약해져야 한다. 부호를 뒤집는 자리가 RankOf 한 곳뿐이라,
// 여기가 깨지면 화면 전체가 반대로 간다.
func TestWorseLossNeverGivesAStrongerName(t *testing.T) {
	prev := RankMax + 1
	for i := 0; i <= 20; i++ {
		loss := float64(i) / 20
		got, ok := RankOf(Estimate{Loss: loss, Samples: MinSamples})
		if !ok {
			t.Fatalf("loss=%.2f 에 이름이 없다", loss)
		}
		if got.Step > prev {
			t.Errorf("loss=%.2f 에서 이름이 세졌다: Step %d → %d", loss, prev, got.Step)
		}
		prev = got.Step
	}
}

// 밖에서 온 값이 척도를 벗어나게 하면 안 된다 — 저장해 둔 값이 DB에서 오고(store),
// `NewTrackFrom` 과 같은 이유로 여기서도 자른다.
func TestOutOfRangeLossStaysOnTheScale(t *testing.T) {
	for _, loss := range []float64{-3, -0.001, 1.001, 42} {
		got, ok := RankOf(Estimate{Loss: loss, Samples: MinSamples})
		if !ok {
			t.Fatalf("loss=%v 에 이름이 없다", loss)
		}
		if got.Step < 0 || got.Step > RankMax {
			t.Errorf("loss=%v 의 Step = %d, 척도 밖이다", loss, got.Step)
		}
		if got.NameJa == "" {
			t.Errorf("loss=%v 의 이름이 비었다", loss)
		}
	}
}

// 양 끝이 척도의 양 끝이어야 한다.
func TestEndsOfTheScale(t *testing.T) {
	best, _ := RankOf(Estimate{Loss: 0, Samples: MinSamples})
	if best.NameJa != "初段" || best.Step != RankMax {
		t.Errorf("낙폭 0의 이름 = %q(%d), want 初段(%d)", best.NameJa, best.Step, RankMax)
	}
	worst, _ := RankOf(Estimate{Loss: 1, Samples: MinSamples})
	if worst.NameJa != "16級" || worst.Step != 0 {
		t.Errorf("낙폭 1의 이름 = %q(%d), want 16級(0)", worst.NameJa, worst.Step)
	}
}
