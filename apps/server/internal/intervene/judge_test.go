package intervene

import "testing"

func TestWinRateIsCentredAndMonotone(t *testing.T) {
	if got := WinRate(0); got != 0.5 {
		t.Fatalf("호각이 50%%가 아니다: %v", got)
	}
	prev := 0.0
	for cp := -3000; cp <= 3000; cp += 100 {
		got := WinRate(cp)
		if got <= prev {
			t.Fatalf("cp %d에서 단조가 깨졌다: %v → %v", cp, prev, got)
		}
		prev = got
	}
}

// 이 표가 종반 규칙이 필요한 이유 전부다.
func TestWinRateSaturatesWhenWinning(t *testing.T) {
	mate := WinRate(29970) // 詰み을 cp로 환산한 값
	won := WinRate(2000)
	if d := mate - won; d > 0.05 {
		t.Fatalf("포화가 예상보다 약하다: Δ=%.3f", d)
	}
	// 그 낙폭이 어느 임계치에도 못 미친다 — 그래서 詰み 거리로 판정한다
	for _, l := range []Level{Beginner, Novice, Intermediate} {
		if mate-won > l.Threshold() {
			t.Fatalf("레벨 %v에서는 승률 낙폭만으로 걸린다 — 전제가 틀렸다", l)
		}
	}
}

// 오프닝의 다양성은 수 번호가 아니라 **임계치**가 지킨다.
//
// 전법 선택은 보통 50~200cp 손해라 어느 레벨도 안 걸리고, 銀 이상을 공짜로 주면
// 입문에서도 걸린다. 그래서 "초반 N수는 안 본다" 같은 구간이 필요 없다 —
// 그런 구간은 5수째의 飛 헌납을 놓치면서 25수째의 정당한 선택은 못 봐준다.
func TestOpeningVarietyIsProtectedByThresholds(t *testing.T) {
	for _, cp := range []int{50, 100, 200} {
		in := Input{BestCp: 0, AfterCp: -cp, Level: Intermediate}
		if v := Judge(in); v.Kind != KindNone {
			t.Errorf("%dcp 손해에 개입했다 — 오프닝 선택 폭이 죽는다: Δ=%.3f", cp, v.DeltaWin)
		}
	}
	// 銀(약 1000cp) 이상을 공짜로 주면 제일 너그러운 입문에서도 걸린다
	for _, cp := range []int{1000, 1600, 2000} {
		in := Input{BestCp: 0, AfterCp: -cp, Level: Beginner}
		if v := Judge(in); v.Kind != KindBlunder {
			t.Errorf("%dcp 헌납이 안 걸렸다: Δ=%.3f", cp, v.DeltaWin)
		}
	}
}

func TestLevelThresholds(t *testing.T) {
	// 승률을 약 15%p 떨어뜨리는 수. 중급·초급은 걸리고 입문은 안 걸린다.
	in := Input{BestCp: 0, AfterCp: -350}
	delta := WinRate(in.BestCp) - WinRate(in.AfterCp)
	if delta < 0.12 || delta > 0.18 {
		t.Fatalf("테스트 전제가 깨졌다: Δ=%.3f (0.12~0.18 기대)", delta)
	}

	for _, tc := range []struct {
		level Level
		want  Kind
	}{
		{Intermediate, KindBlunder},
		{Novice, KindNone},
		{Beginner, KindNone},
	} {
		in.Level = tc.level
		if got := Judge(in).Kind; got != tc.want {
			t.Errorf("레벨 %v: %q 기대, got %q (Δ=%.3f)", tc.level, tc.want, got, delta)
		}
	}
}

// 종반 — 승률로는 안 걸리는 수가 詰み 거리로는 걸려야 한다.
func TestLostMateIsCaughtEvenThoughWinRateBarelyMoves(t *testing.T) {
	in := Input{
		BestCp:     29970, // 詰み
		AfterCp:    2000,  // 여전히 이기고 있다
		MateBefore: 3,
		MateAfter:  0, // 놓쳤다
		Level:      Beginner,
	}
	v := Judge(in)
	if v.Kind != KindBlunder || !v.LostMate {
		t.Fatalf("3手詰을 놓쳤는데 안 걸렸다: %+v", v)
	}
	if v.DeltaWin > 0.05 {
		t.Fatalf("전제 확인: 승률 낙폭이 작아야 한다. Δ=%.3f", v.DeltaWin)
	}
}

func TestMateStillThereIsNotABlunder(t *testing.T) {
	in := Input{BestCp: 29970, AfterCp: 29950, MateBefore: 3, MateAfter: 3, Level: Beginner}
	if v := Judge(in); v.Kind != KindNone {
		t.Fatalf("詰み이 남아 있는데 걸렸다: %+v", v)
	}
	// 조금 멀어지는 것은 봐준다 — 어차피 詰み이다
	in.MateAfter = 5
	if v := Judge(in); v.Kind != KindNone {
		t.Fatalf("3→5는 봐줘야 한다: %+v", v)
	}
	// 크게 멀어지면 걸린다
	in.MateAfter = 9
	if v := Judge(in); v.Kind != KindBlunder {
		t.Fatalf("3→9는 걸려야 한다: %+v", v)
	}
}

// **사라진 것과 멀어진 것은 다른 카테고리다.** 한 이름이었을 때 이긴 판에서
// 「詰みを逃した」고 가르쳤다(journal §76).
func TestSlowerMateIsNotMissedMate(t *testing.T) {
	// 5手詰이 있었는데 8手가 됐다 — 詰み은 그대로 있다.
	kept := Input{BestCp: 29950, AfterCp: 29920, MateBefore: 5, MateAfter: 8, Level: Beginner}
	v := Judge(kept)
	if v.Kind != KindBlunder || !v.LostMate {
		t.Fatalf("5→8은 걸려야 한다: %+v", v)
	}
	if v.Category != CategorySlowerMate {
		t.Fatalf("詰み이 남았는데 %q 다 — 문장이 「逃した」가 된다", v.Category)
	}

	// 같은 국면에서 詰み이 사라지면 저쪽이다.
	gone := kept
	gone.MateAfter, gone.AfterCp = 0, 2000
	if v := Judge(gone); v.Category != CategoryMissedMate {
		t.Fatalf("詰み이 사라졌는데 %q 다", v.Category)
	}
}

// 탐색은 11까지 하지만 판정은 5까지만 한다.
func TestLongMateIsNotJudged(t *testing.T) {
	in := Input{BestCp: 29900, AfterCp: 2000, MateBefore: 9, MateAfter: 0, Level: Beginner}
	if v := Judge(in); v.Kind != KindNone {
		t.Fatalf("9手詰을 놓친 것으로 개입했다 — 8급에게 실수가 아니다: %+v", v)
	}
	in.MateBefore = JudgeMatePlies
	if v := Judge(in); v.Kind != KindBlunder {
		t.Fatalf("5手詰은 판정해야 한다: %+v", v)
	}
}

// 詰まされる 수는 종반 규칙이 아니라 승률 낙폭이 잡는다 — 그래서 규칙이 겹치지 않는다.
func TestBeingMatedIsCaughtByWinRate(t *testing.T) {
	in := Input{BestCp: -500, AfterCp: -29970, Level: Beginner}
	v := Judge(in)
	if v.Kind != KindBlunder || v.LostMate {
		t.Fatalf("詰まされる 수는 낙폭으로 걸려야 한다: %+v", v)
	}
	if v.DeltaWin < 0.25 {
		t.Fatalf("낙폭이 임계치를 넘어야 한다: Δ=%.3f", v.DeltaWin)
	}
}
