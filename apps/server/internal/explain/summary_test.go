package explain

import (
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// 총평은 모든 조합에서 나와야 한다. 여기가 비면 대국이 끝난 화면에 아무것도 없다 —
// 개입 문구의 Render 와 같은 자리다.
//
// 그리고 길이 상한 안에 들어와야 한다(SummaryMaxRunes). 절이 넷까지 이어 붙으므로
// 문구를 하나 늘리면 카드를 넘길 수 있고, 그 조합은 손으로 세어 찾을 것이 아니다.
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
						// 개입이 돈 판과 취해 온 판 둘 다 돈다. 문구가 그 값으로 갈리므로
						// (phraseTop) 한쪽만 돌면 다른 쪽의 조합이 시험을 안 지난다.
						for _, iv := range []bool{true, false} {
							f := GameFacts{
								Outcome: o, Top: top, Weight: w, Phase: p, Trend: tr,
								Level: intervene.Beginner, Intervened: iv,
							}
							got := RenderSummary(f)
							if got == "" {
								t.Fatalf("%+v: 빈 문장", f)
							}
							if n := len([]rune(got)); n > SummaryMaxRunes {
								t.Fatalf("%+v: 총평이 %d자다 (상한 %d): %q", f, n, SummaryMaxRunes, got)
							}
						}
					}
				}
			}
		}
	}
}

// 취해 온 판에서는 아무도 그 수를 막지 않았다. 「戻す」로 말하면 없던 일을 있었다고
// 말하는 것이고, 그건 이 화면에서 가장 새기 쉬운 거짓이다(journal §126).
func TestSummaryDoesNotSayItWasStoppedWhenNothingWas(t *testing.T) {
	cats := [][]intervene.Category{
		nil,
		{intervene.CategoryHangsPiece},
		{intervene.CategoryHangsPiece, intervene.CategoryOther},
	}
	for _, top := range cats {
		for _, w := range []Weight{WeightNone, WeightOnce, WeightSome, WeightMany} {
			f := GameFacts{Outcome: OutcomeLost, Top: top, Weight: w, Intervened: false}
			got := RenderSummary(f)
			for _, bad := range []string{"戻し", "戻す"} {
				if strings.Contains(got, bad) {
					t.Errorf("%+v: 아무도 안 막았는데 %q 라고 한다: %q", f, bad, got)
				}
			}
		}
	}
}

// 개입이 돈 판에서는 그대로 「戻す」로 말한다. 위 시험이 지나가려고 문구를 통째로
// 바꿔 버리는 것을 막는다.
func TestSummaryStillSaysStoppedWhenItWas(t *testing.T) {
	f := GameFacts{
		Outcome: OutcomeLost, Top: []intervene.Category{intervene.CategoryHangsPiece},
		Weight: WeightOnce, Intervened: true,
	}
	if got := RenderSummary(f); !strings.Contains(got, "戻し") {
		t.Errorf("개입이 돈 판인데 안 막았다는 듯이 말한다: %q", got)
	}
}

// 한 번 걸린 판을 「여러 번」으로 부르지 않는다. 사실에 양의 등급이 없던 때 실모델이
// 「場面が多かった」로 썼다(journal §49) — 결정적 문구 쪽도 같은 함정이 있었다.
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

// 개입이 없던 판도 말할 것이 있어야 한다. 「없었다」로 끝내지 않는 것이 이 화면의 일이다.
func TestRenderSummaryWithoutInterventions(t *testing.T) {
	got := RenderSummary(GameFacts{Outcome: OutcomeWon, Phase: PhaseNone, Trend: TrendUnknown})
	if !strings.Contains(got, "勝ちました") {
		t.Errorf("결과를 안 말한다: %q", got)
	}
	if len([]rune(got)) < 20 {
		t.Errorf("너무 짧다: %q", got)
	}
}

// 카테고리 이름은 개입 카드와 같은 어휘여야 한다. 갈라지면 같은 실수가 두 이름으로 불린다.
func TestRenderSummaryUsesCardVocabulary(t *testing.T) {
	f := GameFacts{Outcome: OutcomeLost, Top: []intervene.Category{intervene.CategoryHangsPiece}}
	got := RenderSummary(f)
	if want := CategoryJa(intervene.CategoryHangsPiece); !strings.Contains(got, want) {
		t.Errorf("%q 가 %q 를 안 쓴다", got, want)
	}
}
