package explain

import (
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// 이기고 있는데 진 판을 「무너졌다」고 말하지 않는다.
//
// 회차 1이 그 판이었다 — 295手에 +1782인데 사람이 던졌고, 총평이 「負けました。…後半に
// 崩れたので、そこから見直すとよさそうです。」로 나갔다. 두 문장 다 틀렸다.
func TestLosingFromAheadIsNotCalledACollapse(t *testing.T) {
	f := GameFacts{
		Outcome:  OutcomeLost,
		Top:      []intervene.Category{intervene.CategoryHangsPiece},
		Weight:   WeightMany,
		Trend:    TrendWorsened,
		Standing: StandingAhead,
	}

	got := RenderSummary(f)
	if !strings.Contains(got, "投了") {
		t.Errorf("投了 를 말해야 하는데: %q", got)
	}
	if strings.Contains(got, "崩れた") {
		t.Errorf("이기고 있던 판에 崩れた 가 남았다: %q", got)
	}
	if strings.Contains(got, "負けました") {
		t.Errorf("결과만 말하고 끝났다: %q", got)
	}
}

// 형세를 모르면 지금까지와 같은 문장이다. 옛 판에는 평가치가 없을 수 있고, 그때
// 없는 사실을 지어내면 안 된다.
func TestUnknownStandingKeepsTheOldSentence(t *testing.T) {
	f := GameFacts{
		Outcome:  OutcomeLost,
		Top:      []intervene.Category{intervene.CategoryHangsPiece},
		Weight:   WeightMany,
		Trend:    TrendWorsened,
		Standing: StandingUnknown,
	}

	got := RenderSummary(f)
	if !strings.HasPrefix(got, "負けました。") {
		t.Errorf("負けました 로 시작해야 한다: %q", got)
	}
	if !strings.Contains(got, "崩れた") {
		t.Errorf("崩れた 가 사라졌다 — 형세를 모를 때는 그대로여야 한다: %q", got)
	}
}

// 이기고 이긴 판은 안 건드린다. 규칙이 결과와 형세의 조합에 걸린다는 것을 못박는다.
func TestWinningWhileAheadIsUntouched(t *testing.T) {
	f := GameFacts{Outcome: OutcomeWon, Standing: StandingAhead}
	if got := RenderSummary(f); !strings.HasPrefix(got, "勝ちました。") {
		t.Errorf("勝ちました 로 시작해야 한다: %q", got)
	}
}
