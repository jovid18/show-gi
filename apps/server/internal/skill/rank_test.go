package skill

import (
	"math"
	"testing"
)

// 실측한 급수는 자기 이름에 떨어져야 한다. 앵커가 이름과 어긋나면 그 표본을 잰 의미가
// 없어진다(rankAnchors).
func TestAnchorsLandOnTheirOwnNames(t *testing.T) {
	for _, a := range rankAnchors {
		got, ok := RankOf(named(a.Loss))
		if !ok {
			t.Fatalf("absLoss=%v 에 이름이 없다", a.Loss)
		}
		if got.Step != a.Step {
			t.Errorf("absLoss=%v → %q(%d), want Step %d", a.Loss, got.NameJa, got.Step, a.Step)
		}
	}
}

// 앵커로 안 쓴 실측 라벨이 그 선 근처에 있어야 한다. 벗어나면 척도가 실측을 설명하지
// 못하는 것이고, 그때는 앵커를 늘려야 한다(rankAnchors).
//
// 세 계급까지 봐준다. 그 라벨들의 표준오차가 낙폭의 12~15%인데 계급당 차이가 5%라,
// 지금 표본에서 라벨 하나의 위치 자체가 ±3계급이다 — 그보다 좁게 걸면 판 하나가
// 들어올 때마다 이 테스트가 깨진다.
func TestMeasuredLabelsSitNearTheLine(t *testing.T) {
	for _, c := range []struct {
		name    string
		step    int
		absLoss float64
	}{
		{"5級", 10, 0.0770},
		{"1級", 14, 0.0706},
		{"初段", 15, 0.0683},
	} {
		got := rankStepOf(c.absLoss)
		if math.Abs(got-float64(c.step)) > 3 {
			t.Errorf("%s(실측 %v)의 자리 = %.1f, want %d ±3", c.name, c.absLoss, got, c.step)
		}
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

// 낙폭이 커질수록 이름이 약해져야 한다. 부호를 뒤집는 자리가 rankStepOf 한 곳뿐이라,
// 여기가 깨지면 화면 전체가 반대로 간다.
func TestWorseLossNeverGivesAStrongerName(t *testing.T) {
	prev := RankMax + 1
	for i := 0; i <= 40; i++ {
		abs := 0.20 * float64(i) / 40 // 작은 낙폭(센 쪽)에서 큰 낙폭(약한 쪽)으로
		got, ok := RankOf(named(abs))
		if !ok {
			t.Fatalf("absLoss=%.4f 에 이름이 없다", abs)
		}
		if got.Step > prev {
			t.Errorf("absLoss=%.4f 에서 이름이 세졌다: Step %d → %d", abs, prev, got.Step)
		}
		prev = got.Step
	}
}

// 밖에서 온 값이 척도를 벗어나게 하면 안 된다 — 저장해 둔 값이 DB에서 오고(store),
// NewTrackFrom 과 같은 이유로 여기서도 자른다.
func TestOutOfRangeLossStaysOnTheScale(t *testing.T) {
	for _, abs := range []float64{-3, -0.001, 0, 1.001, 42} {
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

// 양 끝이 척도의 양 끝이어야 한다. 아래 끝은 15級 앵커이고, 위 끝은 그 위 전부다 —
// 段 사이를 이 자로 못 가른다(rankNames).
func TestEndsOfTheScale(t *testing.T) {
	worst, _ := RankOf(named(0.5))
	if worst.NameJa != "15級" || worst.Step != 0 {
		t.Errorf("큰 낙폭의 이름 = %q(%d), want 15級(0)", worst.NameJa, worst.Step)
	}
	// 위 앵커보다 작은 낙폭은 전부 그 한 칸이다. 段 사이를 이 자로 못 가르므로
	// (§94의 평평한 구간) 三段도 5段도 같은 이름으로 나간다.
	for _, abs := range []float64{0.0651, 0.0332, 0.001} {
		got, _ := RankOf(named(abs))
		if got.NameJa != "初段" {
			t.Errorf("absLoss=%v 의 이름 = %q, want 初段", abs, got.NameJa)
		}
	}
}

// 같은 실수가 레벨이 갈려도 같은 이름이어야 한다. 정규화값으로 세면 네 계급 움직이고
// (journal §92의 표) 그 순간 실측 앵커가 통째로 낡는다.
func TestNameDoesNotDependOnTheThresholdThatJudged(t *testing.T) {
	const drop = 0.06 // 승률 0.06 손해. 어느 레벨에서도 통과하는 크기다

	name := func(threshold float64) string {
		tr := NewTrack()
		var e Estimate
		for ply := AnchorFromPly; ply < AnchorFromPly+MinSamples; ply++ {
			e = tr.Observe(Move{DeltaWin: drop, Threshold: threshold, Ply: ply})
		}
		got, ok := RankOf(e)
		if !ok {
			t.Fatalf("임계치 %v에서 이름이 없다", threshold)
		}
		return got.NameJa
	}

	if beginner, intermediate := name(0.25), name(0.12); beginner != intermediate {
		t.Errorf("같은 낙폭에 이름이 갈렸다: beginner=%s intermediate=%s", beginner, intermediate)
	}
}

// 앵커 사이는 로그 보간이다. 두 앵커의 기하 중앙이 그 두 칸의 가운데로 와야 한다 —
// 낙폭이 곱셈적이라(SD가 평균에 비례한다) 산술로 이으면 아래쪽 계급이 뭉친다.
func TestBetweenAnchorsIsLogarithmic(t *testing.T) {
	lo, hi := rankAnchors[0], rankAnchors[len(rankAnchors)-1]
	mid := math.Sqrt(lo.Loss * hi.Loss)
	got := rankStepOf(mid)
	want := float64(lo.Step+hi.Step) / 2
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("기하 중앙의 자리 = %.4f, want %.1f", got, want)
	}
}

// named 는 이름이 붙을 만큼의 표본을 가진 추정치다. 段級은 절대 낙폭만 보므로 Loss 는
// 안 채운다 — 채우면 어느 값이 이름을 만들었는지가 테스트에서 안 보인다.
func named(absLoss float64) Estimate {
	return Estimate{AbsLoss: absLoss, AbsSamples: MinSamples}
}
