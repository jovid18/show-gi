package server

import (
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

func recordFor(myColor string, plies int, ivs ...store.RecordedIntervention) store.GameRecord {
	rec := store.GameRecord{Interventions: ivs}
	rec.MyColor = myColor
	rec.Result = store.ResultWin
	for i := 1; i <= plies; i++ {
		rec.Moves = append(rec.Moves, store.RecordedMove{Ply: i, USI: "7g7f"})
	}
	return rec
}

func iv(ply int, category string) store.RecordedIntervention {
	return store.RecordedIntervention{Ply: ply, Kind: "blunder", Category: category, DeltaWin: 0.4}
}

// 사람이 둔 수를 **색으로 가른다.** 기록에 「누가 뒀나」가 없어서 手数의 홀짝으로 세는데,
// 그 규칙이 뒤집히면 後手로 둔 판의 手数가 상대의 것으로 세어진다.
func TestPlayerMovesCountByColor(t *testing.T) {
	for _, c := range []struct {
		color string
		plies int
		want  int
	}{
		{"b", 10, 5}, // 先手는 홀수 手数
		{"w", 10, 5},
		{"b", 9, 5}, // 1·3·5·7·9
		{"w", 9, 4}, // 2·4·6·8
		{"b", 0, 0},
	} {
		_, stats := factsOf(recordFor(c.color, c.plies), intervene.Beginner)
		if stats.PlayerMoves != c.want {
			t.Errorf("%s %d手: PlayerMoves = %d, want %d", c.color, c.plies, stats.PlayerMoves, c.want)
		}
	}
}

// 카테고리는 많은 순, 같으면 코드 순이다. **결정적이어야 한다** — 흔들리면 같은 판이
// 두 문장을 갖고 캐시 키도 같이 흔들린다.
func TestCategoriesRankDeterministically(t *testing.T) {
	rec := recordFor("b", 20,
		iv(3, "other"), iv(5, "hangs_piece"), iv(7, "hangs_piece"), iv(9, "greedy_capture"))

	facts, stats := factsOf(rec, intervene.Beginner)
	if len(stats.Categories) != 3 {
		t.Fatalf("카테고리 %d개", len(stats.Categories))
	}
	if stats.Categories[0].Code != "hangs_piece" || stats.Categories[0].Count != 2 {
		t.Errorf("1위 = %+v", stats.Categories[0])
	}
	// 1건씩인 둘은 코드 순: greedy_capture < other
	if stats.Categories[1].Code != "greedy_capture" || stats.Categories[2].Code != "other" {
		t.Errorf("동수 정렬이 흔들린다: %+v", stats.Categories)
	}
	// 문장은 **최대 둘**만 말한다.
	if len(facts.Top) != 2 || facts.Top[0] != intervene.CategoryHangsPiece {
		t.Errorf("Top = %v", facts.Top)
	}
	// 이름은 카드와 같은 어휘다.
	if stats.Categories[0].NameJa != explain.CategoryJa(intervene.CategoryHangsPiece) {
		t.Errorf("NameJa = %q", stats.Categories[0].NameJa)
	}
}

// 개입이 없으면 사실도 숫자도 비어야 한다. 여기서 뭔가 지어내면 총평이 없는 실수를 말한다.
func TestNoInterventionsSaysNothingInvented(t *testing.T) {
	facts, stats := factsOf(recordFor("b", 30), intervene.Beginner)
	if len(facts.Top) != 0 || facts.Phase != explain.PhaseNone || facts.Trend != explain.TrendUnknown {
		t.Errorf("facts = %+v", facts)
	}
	if stats.Interventions != 0 || len(stats.Categories) != 0 {
		t.Errorf("stats = %+v", stats)
	}
}

// **과반이 아니면 구간을 말하지 않는다.** 최다 구간을 그냥 말하면 4·3·3에서도
// 「주로 서반」이 된다.
//
// 그리고 **짧은 판에는 종반이 없다.** 판을 삼등분하던 때 3手째 개입이 「終盤」으로 나가
// 화면이 거짓을 말했다(§49) — 지금은 手数의 절대값으로 가른다.
func TestPhaseUsesAbsolutePly(t *testing.T) {
	for _, c := range []struct {
		name string
		ivs  []store.RecordedIntervention
		want explain.Phase
	}{
		{"짧은 판의 첫 수는 서반이다", []store.RecordedIntervention{iv(3, "other")}, explain.PhaseEarly},
		{"서반에 몰렸다", []store.RecordedIntervention{iv(5, "other"), iv(9, "other"), iv(20, "other"), iv(95, "other")}, explain.PhaseEarly},
		{"중반에 몰렸다", []store.RecordedIntervention{iv(40, "other"), iv(50, "other"), iv(60, "other"), iv(5, "other")}, explain.PhaseMiddle},
		{"종반에 몰렸다", []store.RecordedIntervention{iv(85, "other"), iv(95, "other"), iv(101, "other"), iv(5, "other")}, explain.PhaseLate},
		{"안 몰렸다", []store.RecordedIntervention{iv(5, "other"), iv(50, "other"), iv(95, "other")}, explain.PhaseEven},
		{"경계는 서반 쪽이다", []store.RecordedIntervention{iv(30, "other")}, explain.PhaseEarly},
		{"경계 다음은 중반이다", []store.RecordedIntervention{iv(31, "other")}, explain.PhaseMiddle},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := phaseOf(c.ivs); got != c.want {
				t.Errorf("phaseOf = %q, want %q", got, c.want)
			}
		})
	}
	if got := phaseOf(nil); got != explain.PhaseNone {
		t.Errorf("개입 0: %q", got)
	}
}

// 짧은 판 전체를 한 번 통과시킨다. **이것이 §49에서 물린 그 판이다** — 2手 확정 + 3手째
// 개입 하나이고, 그때 화면에 「終盤で」가 나갔다.
func TestShortGameSaysEarlyNotLate(t *testing.T) {
	rec := recordFor("b", 2, iv(3, "hangs_piece"))
	rec.Result = store.ResultLoss

	facts, stats := factsOf(rec, intervene.Beginner)
	if facts.Phase != explain.PhaseEarly {
		t.Errorf("Phase = %q, want %q", facts.Phase, explain.PhaseEarly)
	}
	if stats.Interventions != 1 {
		t.Errorf("개입 수 = %d", stats.Interventions)
	}
	// 결정적 총평도 종반을 말하지 않아야 한다.
	if body := explain.RenderSummary(facts); strings.Contains(body, "終盤") {
		t.Errorf("결정적 총평이 종반을 말한다: %q", body)
	}
}

// 후반 추세는 **표본이 모자라면 말하지 않는다.** 한 건 차이로 「나아졌다」가 뒤집힌다.
func TestTrendNeedsSamplesAndAMargin(t *testing.T) {
	for _, c := range []struct {
		name string
		ivs  []store.RecordedIntervention
		want explain.Trend
	}{
		{"표본 부족", []store.RecordedIntervention{iv(2, "other"), iv(60, "other")}, explain.TrendUnknown},
		{"후반이 나아졌다", []store.RecordedIntervention{iv(2, "other"), iv(4, "other"), iv(6, "other"), iv(8, "other"), iv(70, "other")}, explain.TrendImproved},
		{"후반에 무너졌다", []store.RecordedIntervention{iv(2, "other"), iv(60, "other"), iv(70, "other"), iv(80, "other")}, explain.TrendWorsened},
		{"그대로", []store.RecordedIntervention{iv(2, "other"), iv(20, "other"), iv(60, "other"), iv(70, "other")}, explain.TrendSteady},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := trendOf(c.ivs, 90); got != c.want {
				t.Errorf("trendOf = %q, want %q", got, c.want)
			}
		})
	}
}

// 끝나지 않은 판도 총평이 나와야 한다 — 새로고침하면 판이 끝나므로(§46) 실제로 흔하다.
func TestUnfinishedGameStillSummarizes(t *testing.T) {
	rec := recordFor("b", 12, iv(3, "other"))
	rec.Result = store.ResultAbandoned

	facts, _ := factsOf(rec, intervene.Beginner)
	if facts.Outcome != explain.OutcomeUnfinished {
		t.Errorf("Outcome = %q", facts.Outcome)
	}
	// Summarizer 가 없어도 문장이 나온다.
	got := summarize(t.Context(), nil, rec, intervene.Beginner)
	if got.Body == "" {
		t.Error("빈 총평")
	}
	if got.Tier != explain.TierTemplate {
		t.Errorf("Tier = %d, want %d", got.Tier, explain.TierTemplate)
	}
	if got.Stats.Interventions != 1 {
		t.Errorf("Stats = %+v", got.Stats)
	}
}

// 짚는 자리는 **낙폭이 가장 큰 개입**이다. 회차 2 #2가 요구한 「국면을 짚어라」의 답이고,
// 판마다 하나만 고른다.
func TestFocusPicksTheBiggestDrop(t *testing.T) {
	got := focusOf([]store.RecordedIntervention{
		{Ply: 12, Category: "hangs_piece", DeltaWin: 0.21},
		{Ply: 82, Category: "lets_mate", DeltaWin: 0.64},
		{Ply: 40, Category: "hangs_piece", DeltaWin: 0.55},
	})
	if got == nil {
		t.Fatal("개입이 있는데 짚는 자리가 없다")
	}
	if got.Ply != 82 || got.Category != "lets_mate" {
		t.Errorf("%+v, want 82手 lets_mate", got)
	}
	if got.NameJa == "" {
		t.Error("이름이 비었다 — 화면이 코드를 그리게 된다")
	}
}

// 같은 낙폭이면 이른 手数. 무작위면 같은 판을 두 번 열 때 다른 자리를 짚는다.
func TestFocusIsStableOnTies(t *testing.T) {
	ivs := []store.RecordedIntervention{
		{Ply: 71, Category: "b", DeltaWin: 0.5},
		{Ply: 33, Category: "a", DeltaWin: 0.5},
		{Ply: 55, Category: "c", DeltaWin: 0.5},
	}
	for range 20 {
		got := focusOf(ivs)
		if got == nil || got.Ply != 33 {
			t.Fatalf("%+v, want 33手", got)
		}
	}
}

// 개입이 없으면 짚을 것도 없다. 화면이 그때 그 줄을 안 그린다.
func TestNoInterventionsNoFocus(t *testing.T) {
	if got := focusOf(nil); got != nil {
		t.Errorf("%+v, want nil", got)
	}
}

// 총평의 **문장 쪽**에는 手数가 없어야 한다. 들어가면 캐시 키가 판마다 갈리고
// (GameFacts.Key) LLM이 그 숫자를 옮겨 적을 길이 생긴다.
func TestFocusDoesNotReachTheSentence(t *testing.T) {
	rec := store.GameRecord{
		GameSummary:   store.GameSummary{ID: 1, MyColor: "b", Result: store.ResultLoss},
		Moves:         []store.RecordedMove{{Ply: 1, USI: "7g7f"}, {Ply: 2, USI: "3c3d"}},
		Interventions: []store.RecordedIntervention{{Ply: 3, Category: "hangs_piece", DeltaWin: 0.7}},
	}
	facts, stats := factsOf(rec, intervene.Beginner)
	if stats.Focus == nil || stats.Focus.Ply != 3 {
		t.Fatalf("숫자 쪽에 짚는 자리가 없다: %+v", stats.Focus)
	}

	// 같은 사실인데 手数만 다른 판이 **같은 캐시 키**여야 한다.
	//
	// 手数를 같은 구간 안에서 옮긴다(둘 다 序盤) — 구간이 갈리면 `Phase` 가 달라져서
	// 키도 달라지는 것이 **맞고**, 그건 이 테스트가 잡으려는 것이 아니다.
	other := rec
	other.Interventions = []store.RecordedIntervention{{Ply: 12, Category: "hangs_piece", DeltaWin: 0.7}}
	otherFacts, _ := factsOf(other, intervene.Beginner)
	if facts.Key() != otherFacts.Key() {
		t.Error("手数가 캐시 키를 갈랐다 — Tier 0이 영영 안 맞는다")
	}
}
