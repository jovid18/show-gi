package server

import (
	"context"
	"sort"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 대국 후 총평의 **세는 쪽**이다. 문장으로 바꾸는 일은 `explain` 이 하고(Summarize),
// 여기는 기록에서 사실을 결정적으로 뽑는다 — LLM은 세지 않는다.
//
// 화면에 나가는 것이 둘이다: **숫자(summaryStats)와 문장.** 갈라 둔 이유는 같은 수를 두
// 벌로 두지 않기 위해서다 — 그래서 `explain.GameFacts` 에는 숫자가 아예 없다.

// summaryStats 는 화면이 그대로 그리는 숫자다. 문장은 이 값들을 말하지 않는다.
type summaryStats struct {
	// PlayerMoves 는 사람이 **확정한** 수의 개수다. 물러진 수는 기보에 없다(game.Recorder).
	PlayerMoves int `json:"playerMoves"`
	// Interventions 는 물러진 횟수. 같은 국면에서 여러 번 물러지면 그만큼 센다.
	Interventions int `json:"interventions"`
	// Categories 는 많은 순서다. 화면이 그 순서대로 그린다.
	Categories []categoryCount `json:"categories,omitempty"`
}

type categoryCount struct {
	Code string `json:"code"`
	// NameJa 는 개입 카드와 **같은 어휘**다(explain.CategoryJa). 화면이 코드를 일본어로
	// 바꾸기 시작하면 어휘가 두 벌이 된다.
	NameJa string `json:"nameJa"`
	Count  int    `json:"count"`
}

// gameSummaryPayload 는 WS와 되짚기가 같이 쓰는 모양이다.
type gameSummaryPayload struct {
	// Body 는 화면에 그대로 나가는 일본어다. **절대 비지 않는다**(explain.Result).
	Body string `json:"body"`
	// Tier 는 문장이 어디서 왔는가다. 0=캐시, 1=LLM, -1=결정적 문구.
	Tier  int          `json:"tier"`
	Stats summaryStats `json:"stats"`
}

// summarize 는 기록 하나를 총평으로 바꾼다. **Summarizer 가 nil이면 결정적 문구**가 나간다 —
// 개입 문구와 같은 규약이다(Options.Explainer).
func summarize(ctx context.Context, sum explain.Summarizer, rec store.GameRecord, level intervene.Level) gameSummaryPayload {
	facts, stats := factsOf(rec, level)

	body := explain.RenderSummary(facts)
	tier := explain.TierTemplate
	if sum != nil {
		r := sum.Summarize(ctx, facts)
		body, tier = r.Body, r.Tier
	}
	return gameSummaryPayload{Body: body, Tier: tier, Stats: stats}
}

// factsOf 는 기록에서 사실과 숫자를 한 번에 센다. **한 함수인 이유는 같은 세기에서 나와야
// 하기 때문이다** — 갈라 두면 문장이 말하는 카테고리와 화면의 표가 어긋날 수 있다.
func factsOf(rec store.GameRecord, level intervene.Level) (explain.GameFacts, summaryStats) {
	f := explain.GameFacts{Outcome: outcomeOf(rec.Result), Level: level, Phase: explain.PhaseNone, Trend: explain.TrendUnknown}

	// 사람이 둔 수만 센다. `Moves` 는 확정된 수 전부라 상대의 것이 섞여 있다.
	//
	// **手数의 짝으로 가른다.** 기록에 「누가 뒀나」가 없고(game_moves 에는 ply와 usi뿐),
	// 手数는 1부터 번갈아 붙으므로 사람의 색이 곧 홀짝이다.
	humanOdd := rec.MyColor == "b"
	var stats summaryStats
	last := 0
	for _, m := range rec.Moves {
		if m.Ply > last {
			last = m.Ply
		}
		if (m.Ply%2 == 1) == humanOdd {
			stats.PlayerMoves++
		}
	}

	stats.Interventions = len(rec.Interventions)
	if stats.Interventions == 0 {
		return f, stats
	}

	// **물러진 수는 기보에 없다**(game.Recorder). 그래서 개입의 手数가 마지막 확정 수보다
	// 클 수 있고, 그 값을 안 보면 판이 실제보다 짧은 것으로 세어진다.
	for _, iv := range rec.Interventions {
		if iv.Ply > last {
			last = iv.Ply
		}
	}

	counts := map[intervene.Category]int{}
	for _, iv := range rec.Interventions {
		counts[intervene.Category(iv.Category)]++
	}
	stats.Categories = rankCategories(counts)
	for i, c := range stats.Categories {
		if i == 2 {
			break // 문장이 말하는 것은 최대 둘이다(GameFacts.Top)
		}
		f.Top = append(f.Top, intervene.Category(c.Code))
	}

	f.Weight = weightOf(stats.Interventions)
	f.Phase = phaseOf(rec.Interventions)
	f.Trend = trendOf(rec.Interventions, last)
	return f, stats
}

// rankCategories 는 많은 순으로 줄 세운다. **같은 수면 코드 순이다** — 무작위면 같은 판을
// 두 번 요약할 때 다른 문장이 나오고, 그건 캐시 키가 흔들린다는 뜻이기도 하다.
func rankCategories(counts map[intervene.Category]int) []categoryCount {
	out := make([]categoryCount, 0, len(counts))
	for c, n := range counts {
		out = append(out, categoryCount{Code: string(c), NameJa: explain.CategoryJa(c), Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Code < out[j].Code
	})
	return out
}

// weightOf 는 개입 횟수를 등급으로 옮긴다(explain.Weight).
//
// **[미확정]** 경계 둘은 초기값이다. 109手 한 판에서 24건이었으므로(08-playtest.md) 5건은
// 「何度も」의 아래쪽에 있다.
func weightOf(n int) explain.Weight {
	switch {
	case n <= 0:
		return explain.WeightNone
	case n == 1:
		return explain.WeightOnce
	case n <= 4:
		return explain.WeightSome
	default:
		return explain.WeightMany
	}
}

// 序盤·中盤·終盤의 경계다. **手数의 절대값으로 가른다.**
//
// 판을 삼등분하던 것을 바꿨다 — 3手째에 걸린 개입이 「終盤」이 됐고(전체가 3手였다) 화면이
// 그것을 문장으로 내보냈다. **판의 길이로 나누면 짧은 판에서 첫 수가 종반이 된다.**
// 20手에 끝난 판에는 종반이 없는 것이 맞고, 그때는 아무 구간도 말하지 않는 것이 맞다.
//
// **[미확정]** 두 값은 초기값이다. 109手 한 판(08-playtest.md)에서 눈으로 잡았다.
const (
	earlyUntil  = 30
	middleUntil = 80
)

// phaseOf 는 개입이 몰린 구간이다.
//
// **몰렸다고 말할 기준을 둔다** — 과반이 아니면 `even` 이다. 최다 구간을 그냥 말하면
// 4·3·3에서도 「주로 서반」이 되고, 그건 사실이 아니다.
func phaseOf(ivs []store.RecordedIntervention) explain.Phase {
	if len(ivs) == 0 {
		return explain.PhaseNone
	}
	third := [3]int{}
	for _, iv := range ivs {
		switch {
		case iv.Ply <= earlyUntil:
			third[0]++
		case iv.Ply <= middleUntil:
			third[1]++
		default:
			third[2]++
		}
	}
	best, at := 0, 0
	for i, n := range third {
		if n > best {
			best, at = n, i
		}
	}
	if best*2 <= len(ivs) {
		return explain.PhaseEven
	}
	return [3]explain.Phase{explain.PhaseEarly, explain.PhaseMiddle, explain.PhaseLate}[at]
}

// trendMinSamples 는 전후반을 견주기 전에 필요한 개입 수다. 둘 이하면 「후반이 나아졌다」가
// 우연 하나에 뒤집힌다. **[미확정]** — skill.MinSamples 와 같은 성질의 초기값이다.
const trendMinSamples = 4

// trendOf 는 후반이 나아졌는가다. **개입 밀도로 본다** — 낙폭의 평균은 물러진 수에만 있는
// 값이라(§39 ⑥) 통과한 수가 늘어난 것을 못 본다.
func trendOf(ivs []store.RecordedIntervention, lastPly int) explain.Trend {
	if lastPly <= 0 || len(ivs) < trendMinSamples {
		return explain.TrendUnknown
	}
	half := lastPly / 2
	early, late := 0, 0
	for _, iv := range ivs {
		if iv.Ply <= half {
			early++
		} else {
			late++
		}
	}
	switch {
	// 절반 이하로 줄었나 / 두 배 이상 늘었나. 한 건 차이로 말하지 않는다.
	case late*2 <= early:
		return explain.TrendImproved
	case early*2 <= late:
		return explain.TrendWorsened
	default:
		return explain.TrendSteady
	}
}

func outcomeOf(result store.GameResult) explain.Outcome {
	switch result {
	case store.ResultWin:
		return explain.OutcomeWon
	case store.ResultLoss:
		return explain.OutcomeLost
	case store.ResultDraw:
		return explain.OutcomeDrawn
	default:
		// 빈 값(아직 두는 중)도 여기로 온다 — 총평은 끝난 판에만 부르므로 실제로는
		// `abandoned` 뿐이다.
		return explain.OutcomeUnfinished
	}
}
