package intervene

import "testing"

// 개입이 걸릴 만큼 나쁜 수. 카테고리만 보고 싶은 테스트에서 공통으로 쓴다.
func blunderInput(f Features) Input {
	return Input{BestCp: 0, AfterCp: -1600, Features: f, Level: Beginner}
}

func TestCategoryPicksTheMostConcreteReason(t *testing.T) {
	known := Features{Known: true}

	for _, tc := range []struct {
		name string
		f    Features
		want Category
	}{
		{
			"タダ捨て — 상대만 노리는 칸에 비싼 駒를 놓았다",
			Features{Known: true, LandsAttacked: true, MovedValue: 11},
			CategoryHangsPiece,
		},
		{
			"지켜져 있으면 タダ捨て가 아니다",
			Features{Known: true, LandsAttacked: true, LandsDefended: true, MovedValue: 11},
			CategoryOther,
		},
		{
			"딴 것이 더 비싸면 タダ捨て가 아니다 — 정당한 駒交換이 여기 안 들어온다",
			Features{Known: true, LandsAttacked: true, MovedValue: 1, CapturedValue: 10},
			CategoryGreedyCapture,
		},
		{
			"얕은 이득에 낚임 — 한 수만 보면 이득, 깊게 보면 손해",
			Features{Known: true, ShallowCp: 200, HasShallow: true},
			CategoryShallowTrap,
		},
		{
			"駒는 땄는데 형세가 나빠졌다",
			Features{Known: true, CapturedValue: 6},
			CategoryGreedyCapture,
		},
		{
			"追う手 — 이득 없는 王手",
			Features{Known: true, GivesCheck: true},
			CategoryIdleCheck,
		},
		{
			"玉이 열렸다 — 지키는 利き이 줄고 상대 利き이 늘었다",
			Features{Known: true, ShieldLoss: 2, ThreatGain: 3},
			CategoryKingExposed,
		},
		{
			"짚을 것이 없으면 other",
			known,
			CategoryOther,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := Judge(blunderInput(tc.f))
			if v.Kind != KindBlunder {
				t.Fatalf("테스트 전제가 깨졌다 — 개입부터 안 걸린다: %+v", v)
			}
			if v.Category != tc.want {
				t.Errorf("카테고리 %q 기대, got %q", tc.want, v.Category)
			}
		})
	}
}

// 玉이 열린 것은 **양쪽이 같이** 움직였을 때만이다. 한쪽만 보면 玉을 자연스럽게
// 옮기는 수까지 걸리고, 그러면 설명이 틀린다.
func TestKingExposedNeedsBothSidesToMove(t *testing.T) {
	for _, f := range []Features{
		{Known: true, ShieldLoss: 3},
		{Known: true, ThreatGain: 3},
		{Known: true, ShieldLoss: -2, ThreatGain: 3},
	} {
		if got := Judge(blunderInput(f)).Category; got == CategoryKingExposed {
			t.Errorf("%+v 에서 玉이 열렸다고 했다", f)
		}
	}
}

// 반전 폭이 작으면 함정이 아니라 그냥 평가가 흔들린 것이다.
func TestShallowTrapNeedsARealReversal(t *testing.T) {
	// 얕게 +50, 깊게 −100. 벌어진 폭이 150이라 임계치에 못 미친다.
	in := blunderInput(Features{Known: true, ShallowCp: 50, HasShallow: true})
	in.AfterCp = -100
	// 그 자체로는 개입이 안 걸리는 크기라 낙폭은 따로 만든다
	in.BestCp = 1600
	if got := Judge(in).Category; got == CategoryShallowTrap {
		t.Errorf("반전 폭 %d(<%d)인데 함정이라고 했다", in.Features.ShallowCp-in.AfterCp, ShallowTrapCp)
	}

	// 얕게 보면 손해인 수는 애초에 함정이 아니다 — 초보자도 손해로 본다
	in = blunderInput(Features{Known: true, ShallowCp: -50, HasShallow: true})
	if got := Judge(in).Category; got == CategoryShallowTrap {
		t.Errorf("얕게 봐도 손해인 수를 함정이라고 했다")
	}
}

// 詰み을 놓친 것은 다른 축이다 — 판을 읽었든 아니든 이유가 이미 정해져 있다.
func TestMissedMateWinsOverBoardFacts(t *testing.T) {
	in := Input{
		BestCp: 29970, AfterCp: 2000,
		MateBefore: 3, MateAfter: 0,
		Features: Features{Known: true, CapturedValue: 10, GivesCheck: true},
		Level:    Beginner,
	}
	if got := Judge(in).Category; got != CategoryMissedMate {
		t.Fatalf("카테고리 %q 기대, got %q", CategoryMissedMate, got)
	}
}

// **판을 못 읽었으면 모른다고 말한다.** 지어내면 초심자는 틀린 것을 그대로 배운다.
func TestUnknownFeaturesFallBackToOther(t *testing.T) {
	v := Judge(blunderInput(Features{}))
	if v.Kind != KindBlunder {
		t.Fatalf("사실을 못 구했다고 개입까지 사라지면 안 된다: %+v", v)
	}
	if v.Category != CategoryOther {
		t.Errorf("카테고리 %q 기대, got %q", CategoryOther, v.Category)
	}
}

// 개입하지 않은 수에는 카테고리가 없다. 「나쁘지 않은데 이유가 붙어 있다」는 상태를
// 만들면 약점 프로파일이 그 위에서 쌓인다.
func TestNoCategoryWhenNotIntervening(t *testing.T) {
	in := Input{BestCp: 0, AfterCp: -50, Features: Features{Known: true, GivesCheck: true}}
	if v := Judge(in); v.Category != CategoryNone {
		t.Fatalf("개입하지 않았는데 카테고리가 붙었다: %+v", v)
	}
}
