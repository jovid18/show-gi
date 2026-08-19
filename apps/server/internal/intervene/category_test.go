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
			"駒는 땄는데 되따이고 형세가 나빠졌다",
			Features{Known: true, CapturedValue: 10, MovedValue: 6, LandsAttacked: true, LandsDefended: true},
			CategoryGreedyCapture,
		},
		{
			"駒는 땄는데 그 사이 玉이 밀렸다",
			Features{Known: true, CapturedValue: 6, ThreatGain: 2},
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

// **공짜로 딴 것은 이유가 아니다.**
//
// 반대쪽에서 벌어진 일 때문에 나쁜 수인데 마침 歩를 하나 공짜로 땄다면, 딴 것을
// 이유라고 말하는 순간 설명이 틀린다. 짚을 것이 없으면 미분류로 두는 편이 낫다.
func TestACleanCaptureIsNotTheReason(t *testing.T) {
	f := Features{Known: true, CapturedValue: 1, MovedValue: 6} // 되따이지도, 玉이 밀리지도 않았다
	if got := Judge(blunderInput(f)).Category; got == CategoryGreedyCapture {
		t.Error("공짜로 딴 歩를 블런더의 이유라고 했다")
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

// 成 여부만 다른 수는 **다른 무엇으로도 분류되지 않아야 한다.**
//
// 실측에서 이 수가 greedy_capture 로 떨어져 「잡는 것이 문제」라고 가르쳤고, 플레이어가
// 그 문장을 믿고 세 수를 더 헤맸다(08-playtest.md §8). 그래서 이 테스트가 지키는 것은
// 「unpromoted 로 간다」가 아니라 **「다른 이유를 대지 않는다」**다.
func TestUnpromotedBeatsEveryOtherReason(t *testing.T) {
	// 딴 것도 있고(greedy_capture), 그냥 잡히기도 하고(hangs_piece), 王手까지 거는
	// 수를 만든다. 이 셋이 전부 켜져 있어도 成 여부가 이유여야 한다.
	in := Input{
		BestCp:  120,
		AfterCp: -900,
		Level:   Beginner,
		Features: Features{
			Known:          true,
			UnpromotedOnly: true,
			CapturedValue:  10,
			MovedValue:     6,
			LandsAttacked:  true,
			LandsDefended:  false,
			GivesCheck:     true,
			ShieldLoss:     3,
			ThreatGain:     2,
		},
	}
	if got := Judge(in).Category; got != CategoryUnpromoted {
		t.Fatalf("%s — 成 여부가 이유여야 한다", got)
	}

	// **칸을 내리면 실측에서 나갔던 그 오분류가 그대로 돌아온다.** 이 줄이 있어야
	// 위의 단정이 무엇을 막고 있는지가 코드에 남는다 — greedy_capture 는 「잡는 것이
	// 문제」라고 말하는데 그 국면에서 잡는 것은 정답이었다.
	in.Features.UnpromotedOnly = false
	if got := Judge(in).Category; got != CategoryGreedyCapture {
		t.Fatalf("%s — 끄면 옛 분류(greedy_capture)로 돌아가야 한다", got)
	}
}

// TestShallowTrapReadsTheBaseline 은 **駒落ち에서도 그 카테고리가 나오는지**를 본다.
//
// 이 규칙만 절대 부호를 읽는다(`ShallowCp > Baseline` · `AfterCp < Baseline`). 기준점을
// 안 보면 二枚落ち에서 앞 조건이 언제나 참이고 뒤 조건이 거의 언제나 거짓이라, 판정은
// 걸리는데 이름이 `other` 로 떨어진다 — 개입은 살아 있고 **설명만 조용히 나빠지는** 모양이라
// 눈으로는 안 잡힌다(journal §84).
func TestShallowTrapReadsTheBaseline(t *testing.T) {
	const nimai = 1490 // internal/handicap 의 실측값

	// 얕게는 기준점보다 좋아 보이고(+400) 깊게는 나쁘다(-900). 낙폭이 입문 임계치를
	// 넘어야 카테고리가 붙으므로(Judge) -900이다 — -400은 통과해서 이름이 아예 안 생긴다.
	flat := Input{
		BestCp: 0, AfterCp: -900, Level: Beginner,
		Features: Features{Known: true, HasShallow: true, ShallowCp: 400},
	}
	if got := Judge(flat).Category; got != CategoryShallowTrap {
		t.Fatalf("전제가 깨졌다 — 平手에서 %q 다", got)
	}

	// 같은 국면을 二枚落ち로 옮긴다. 기준점을 안 보면 여기서 이름이 갈린다.
	komaochi := Input{
		BestCp: nimai, AfterCp: nimai - 900, BaselineCp: nimai, Level: Beginner,
		Features: Features{Known: true, HasShallow: true, ShallowCp: nimai + 400},
	}
	v := Judge(komaochi)
	if v.Kind != KindBlunder {
		t.Fatalf("판정이 안 걸렸다: Δ=%.3f", v.DeltaWin)
	}
	if v.Category != CategoryShallowTrap {
		t.Errorf("二枚落ち에서 카테고리 = %q, want %q", v.Category, CategoryShallowTrap)
	}
}
