package explain

import (
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// 결정적 총평은 **모든 조합에서 나와야 한다.** 여기가 비면 라우터가 죽은 날 대국이 끝난
// 화면에 아무것도 없다 — 개입 문구의 `Render` 와 같은 자리다.
//
// 그리고 **자기가 만든 문장이 자기 검사를 통과해야 한다.** `Clean` 이 버리는 문장을
// 폴백으로 내보내면, LLM이 실패한 날 화면에 나가는 것이 화면에 나갈 수 없는 문장이다.
func TestRenderSummaryCoversEveryCombination(t *testing.T) {
	cats := []intervene.Category{
		intervene.CategoryHangsPiece, intervene.CategoryOther, intervene.CategoryMissedMate,
		intervene.CategoryLetsMate, intervene.CategoryGreedyCapture, intervene.CategoryUnpromoted,
		intervene.CategoryShallowTrap, intervene.CategoryIdleCheck, intervene.CategoryKingExposed,
	}
	tops := [][]intervene.Category{nil}
	for _, c := range cats {
		tops = append(tops, []intervene.Category{c})
		tops = append(tops, []intervene.Category{c, intervene.CategoryOther})
	}

	weights := []Weight{WeightNone, WeightOnce, WeightSome, WeightMany}
	for _, o := range []Outcome{OutcomeWon, OutcomeLost, OutcomeDrawn, OutcomeUnfinished} {
		for _, p := range []Phase{PhaseNone, PhaseEarly, PhaseMiddle, PhaseLate, PhaseEven} {
			for _, tr := range []Trend{TrendUnknown, TrendImproved, TrendWorsened, TrendSteady} {
				for _, w := range weights {
					for _, top := range tops {
						f := GameFacts{Outcome: o, Top: top, Weight: w, Phase: p, Trend: tr, Level: intervene.Beginner}
						got := RenderSummary(f)
						if got == "" {
							t.Fatalf("%+v: 빈 문장", f)
						}
						if _, ok := Clean(got, SummaryMaxRunes); !ok {
							t.Fatalf("%+v: 결정적 총평이 Clean 을 통과하지 못한다: %q", f, got)
						}
					}
				}
			}
		}
	}
}

// 한 번 걸린 판을 **「여러 번」으로 부르지 않는다.** 사실에 양의 등급이 없던 때 실모델이
// 「場面が多かった」로 썼다(06-status.md §49) — 결정적 문구 쪽도 같은 함정이 있었다.
func TestRenderSummaryDoesNotInflateOneStumble(t *testing.T) {
	f := GameFacts{Outcome: OutcomeLost, Top: []intervene.Category{intervene.CategoryHangsPiece},
		Weight: WeightOnce, Phase: PhaseEarly, Trend: TrendUnknown, Level: intervene.Beginner}
	got := RenderSummary(f)
	for _, bad := range []string{"多かった", "何度も"} {
		if strings.Contains(got, bad) {
			t.Errorf("한 번인데 %q 라고 한다: %q", bad, got)
		}
	}
}

// 개입이 없던 판도 말할 것이 있어야 한다. **「없었다」로 끝내지 않는 것**이 이 화면의 일이다.
func TestRenderSummaryWithoutInterventions(t *testing.T) {
	got := RenderSummary(GameFacts{Outcome: OutcomeWon, Phase: PhaseNone, Trend: TrendUnknown})
	if !strings.Contains(got, "勝ちました") {
		t.Errorf("결과를 안 말한다: %q", got)
	}
	if len([]rune(got)) < 20 {
		t.Errorf("너무 짧다: %q", got)
	}
}

// 카테고리 이름은 **개입 카드와 같은 어휘**여야 한다. 갈라지면 같은 실수가 두 이름으로 불린다.
func TestRenderSummaryUsesCardVocabulary(t *testing.T) {
	f := GameFacts{Outcome: OutcomeLost, Top: []intervene.Category{intervene.CategoryHangsPiece}}
	got := RenderSummary(f)
	if want := CategoryJa(intervene.CategoryHangsPiece); !strings.Contains(got, want) {
		t.Errorf("%q 가 %q 를 안 쓴다", got, want)
	}
}

// 키가 사실마다 갈려야 한다. 안 갈리면 캐시가 다른 판의 총평을 돌려준다.
func TestGameFactsKeyVariesWithEveryField(t *testing.T) {
	base := GameFacts{Outcome: OutcomeWon, Top: []intervene.Category{intervene.CategoryOther},
		Phase: PhaseEarly, Trend: TrendSteady, Level: intervene.Beginner}

	others := []GameFacts{
		{Outcome: OutcomeLost, Top: base.Top, Phase: base.Phase, Trend: base.Trend, Level: base.Level},
		{Outcome: base.Outcome, Top: nil, Phase: base.Phase, Trend: base.Trend, Level: base.Level},
		{Outcome: base.Outcome, Top: base.Top, Weight: WeightMany, Phase: base.Phase, Trend: base.Trend, Level: base.Level},
		{Outcome: base.Outcome, Top: base.Top, Phase: PhaseLate, Trend: base.Trend, Level: base.Level},
		{Outcome: base.Outcome, Top: base.Top, Phase: base.Phase, Trend: TrendImproved, Level: base.Level},
		{Outcome: base.Outcome, Top: base.Top, Phase: base.Phase, Trend: base.Trend, Level: intervene.Intermediate},
	}
	seen := map[string]bool{base.Key(): true}
	for i, f := range others {
		k := f.Key()
		if seen[k] {
			t.Errorf("%d번째가 키를 안 바꿨다", i)
		}
		seen[k] = true
	}
	// 같은 사실이면 같은 키다 — 그렇지 않으면 캐시가 영원히 안 맞는다.
	if base.Key() != (GameFacts{Outcome: OutcomeWon, Top: []intervene.Category{intervene.CategoryOther},
		Phase: PhaseEarly, Trend: TrendSteady, Level: intervene.Beginner}).Key() {
		t.Error("같은 사실이 다른 키를 만든다")
	}
	// **개입 문구의 키와 섞이지 않는다.** 한 표를 쓰므로 접두사가 그 유일한 구분이다.
	if strings.HasPrefix(base.Key(), "s") == false {
		t.Errorf("총평 키에 접두사가 없다: %q", base.Key())
	}
}

// 프롬프트에 **숫자를 안 준다.** GameFacts 에 숫자가 없다는 것이 그 보증인데, 프롬프트가
// 다른 데서 숫자를 끌어오는 날을 여기서 막는다.
func TestSummaryPromptCarriesNoCounts(t *testing.T) {
	f := GameFacts{Outcome: OutcomeLost, Top: []intervene.Category{intervene.CategoryHangsPiece},
		Phase: PhaseMiddle, Trend: TrendWorsened, Level: intervene.Beginner}
	f.Weight = WeightOnce
	got := summaryUserPrompt(f)
	for _, digit := range []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"} {
		if strings.Contains(got, digit) {
			t.Errorf("프롬프트에 숫자가 있다(%s):\n%s", digit, got)
		}
	}
	// 그러면서 사실은 다 들어가야 한다 — 덜 주면 모델이 남은 자리를 지어낸다.
	for _, want := range []string{"負け", CategoryJa(intervene.CategoryHangsPiece), "中盤", "崩れた"} {
		if !strings.Contains(got, want) {
			t.Errorf("프롬프트에 %q 가 없다:\n%s", want, got)
		}
	}
}
