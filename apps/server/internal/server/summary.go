package server

import (
	"sort"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/handicap"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 대국 후 총평의 세는 쪽이다. 문장으로 바꾸는 일은 explain.RenderSummary 가 하고,
// 여기는 기록에서 사실을 결정적으로 뽑는다.
//
// 화면에 나가는 것이 숫자(summaryStats)와 문장 둘인데, 같은 수를 두 벌로 두지 않으려고
// 갈라 뒀다 — 그래서 explain.GameFacts 에는 숫자가 아예 없다.

// summaryStats 는 화면이 그대로 그리는 숫자다. 문장은 이 값들을 말하지 않는다.
type summaryStats struct {
	// PlayerMoves 는 사람이 확정한 수의 개수다. 물러진 수는 기보에 없다(game.Recorder).
	PlayerMoves int `json:"playerMoves"`
	// Interventions 는 물러진 횟수. 같은 국면에서 여러 번 물러지면 그만큼 센다.
	Interventions int `json:"interventions"`
	// Categories 는 많은 순서다. 화면이 그 순서대로 그린다.
	Categories []categoryCount `json:"categories,omitempty"`
	// Focus 는 「이 국면을 다시 봐라」다. 개입이 없으면 nil.
	Focus *focusPoint `json:"focus,omitempty"`
}

// focusPoint 는 그 판에서 가장 크게 갈린 자리 하나다. 문장이 아니라 숫자 쪽에 둔다 —
// 이 파일의 규약이 그렇다(위 주석).
type focusPoint struct {
	// Ply 는 물러진 수의 手数다. 화면은 이 수를 두기 직전 국면을 연다 — 물러진 수는
	// 기보에 없으므로(game.Recorder) 그 자리가 「다시 생각할 국면」이다.
	Ply      int    `json:"ply"`
	Category string `json:"category"`
	// NameJa 는 개입 카드·총평·마이페이지와 같은 어휘다(explain.CategoryJa).
	NameJa string `json:"nameJa"`
}

type categoryCount struct {
	Code string `json:"code"`
	// NameJa 는 개입 카드와 같은 어휘다(explain.CategoryJa). 화면이 코드를 일본어로
	// 바꾸기 시작하면 어휘가 두 벌이 된다.
	NameJa string `json:"nameJa"`
	Count  int    `json:"count"`
}

// rankView 는 段級 하나를 화면이 그릴 수 있는 모양으로 옮긴 것이다. 이름을 같이 보낸다 —
// 화면이 Step 에서 이름을 만들면 어휘가 두 벌이 되고, 척도를 늘리는 날 한쪽만 늘어난다
// (skill.Rank).
type rankView struct {
	// Step 은 0..Max 이고 클수록 세다. 눈금을 그리는 데 쓴다.
	Step int `json:"step"`
	Max  int `json:"max"`
	// NameJa 는 「8級」·「初段」. 그대로 나간다.
	NameJa string `json:"nameJa"`
}

// skillChange 는 이 판에서 段級이 어떻게 움직였나다.
//
// 대국 중에는 안 보낸다. 자기 실력이 매 수 흔들리는 것을 보여 줄 이유가 없고
// (skill.RiseRate 가 비대칭이라 블런더 하나에 몇 계단이 움직인다), 사람이 알고 싶은
// 것은 한 판을 두고 나서의 결과다.
type skillChange struct {
	// Before 는 판을 시작할 때다. 첫 판이거나 익명이면 잰 적이 없어 비어 있다.
	Before *rankView `json:"before,omitempty"`
	After  rankView  `json:"after"`
}

// gameSummaryPayload 는 WS와 되짚기가 같이 쓰는 모양이다.
type gameSummaryPayload struct {
	// Body 는 화면에 그대로 나가는 일본어다. 비는 일이 없다(explain.RenderSummary).
	Body  string       `json:"body"`
	Stats summaryStats `json:"stats"`
	// GameID 는 이 판이 기록에 남은 번호다. 화면이 되짚기로 건너가는 데 쓴다 — 대국
	// 화면은 그때까지 자기 판의 번호를 모른다(기록은 WS 밖에서 비동기로 쓰인다).
	//
	// 되짚기가 부르는 쪽에서는 0이다 — 이미 그 판을 열고 있는 화면이라 쓸 데가 없다.
	GameID int64 `json:"gameId,omitempty"`
	// Skill 은 이 판의 段級 변화다. 되짚기에서는 언제나 nil 이다 — 추정치는 사람에게
	// 붙는 값이라 지난 판을 여는 지금은 이미 다른 값이고, 그때의 값을 남겨 두지 않았다.
	// 지난 판의 「그때 몇 급이었나」를 말하려면 판마다 저장해야 한다.
	Skill *skillChange `json:"skill,omitempty"`
}

// summarize 는 기록 하나를 총평으로 바꾼다. 문장과 표가 같은 세기에서 나온다(factsOf).
func summarize(rec store.GameRecord, level intervene.Level) gameSummaryPayload {
	facts, stats := factsOf(rec, level)
	return gameSummaryPayload{Body: explain.RenderSummary(facts), Stats: stats}
}

// factsOf 는 기록에서 사실과 숫자를 한 번에 센다. 갈라 두면 문장이 말하는 카테고리와
// 화면의 표가 어긋날 수 있어 한 함수다.
func factsOf(rec store.GameRecord, level intervene.Level) (explain.GameFacts, summaryStats) {
	f := explain.GameFacts{
		Outcome:  outcomeOf(rec.Result),
		Level:    level,
		Phase:    explain.PhaseNone,
		Trend:    explain.TrendUnknown,
		Standing: standingOf(rec),
	}

	// 사람이 둔 수만 센다. Moves 는 확정된 수 전부라 상대의 것이 섞여 있다.
	//
	// 手数의 짝으로 가른다. 기록에 「누가 뒀나」가 없고(game_moves 에는 ply 와 usi 뿐),
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

	// 물러진 수는 기보에 없다(game.Recorder). 그래서 개입의 手数가 마지막 확정 수보다
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
	stats.Focus = focusOf(rec.Interventions)
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

// focusOf 는 「이 국면을 다시 봐라」로 짚을 자리 하나다. 낙폭이 가장 큰 개입을 고른다.
//
// 카테고리를 안 본다 — 어느 종류가 더 배울 것이 많은지는 우리가 모르고, 그걸 정하는
// 순간 순위표가 하나 더 생긴다(intervene 이 카테고리를 스칼라로 받는 것과 같은 이유, §15).
//
// 같은 낙폭이면 이른 手数다. 무작위면 같은 판을 두 번 열 때 다른 자리를 짚는다
// (rankCategories 와 같은 판단).
func focusOf(ivs []store.RecordedIntervention) *focusPoint {
	var best *store.RecordedIntervention
	for i := range ivs {
		switch {
		case best == nil, ivs[i].DeltaWin > best.DeltaWin:
			best = &ivs[i]
		case ivs[i].DeltaWin == best.DeltaWin && ivs[i].Ply < best.Ply:
			best = &ivs[i]
		}
	}
	if best == nil {
		return nil
	}
	return &focusPoint{
		Ply:      best.Ply,
		Category: best.Category,
		NameJa:   explain.CategoryJa(intervene.Category(best.Category)),
	}
}

// rankCategories 는 많은 순으로 줄 세운다. 같은 수면 코드 순이다 — 무작위면 같은 판을
// 두 번 요약할 때 다른 문장이 나온다.
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
// [미확정] 경계 둘은 초기값이다. 109手 한 판에서 24건이었으므로(08-playtest.md) 5건은
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

// 序盤·中盤·終盤의 경계다. 手数의 절대값으로 가른다 — 판의 길이로 나누면 짧은 판에서
// 첫 수가 종반이 된다. 20手에 끝난 판에는 종반이 없는 것이 맞고, 그때는 아무 구간도
// 말하지 않는 것이 맞다.
//
// [미확정] 두 값은 초기값이다. 109手 한 판(08-playtest.md)에서 눈으로 잡았다.
const (
	earlyUntil  = 30
	middleUntil = 80
)

// phaseOf 는 개입이 몰린 구간이다. 과반이 아니면 even 이다 — 최다 구간을 그냥 말하면
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

// trendMinSamples 는 전후반을 견주기 전에 필요한 개입 수다. 둘 이하면 「후반이
// 나아졌다」가 우연 하나에 뒤집힌다. [미확정] — skill.MinSamples 와 같은 성질의 초기값이다.
const trendMinSamples = 4

// trendOf 는 후반이 나아졌는가다. 개입 밀도로 본다 — 낙폭의 평균은 물러진 수에만 있는
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

// standingOf 는 판이 끝난 시점의 형세를 사람 관점으로 읽는다.
//
// 마지막으로 채워진 평가치를 쓴다. 평가치는 수보다 늦게 오므로 마지막 몇 수가 비어
// 있을 수 있고(store.RecordedMove), 그것을 안 보면 한참 전의 형세로 말하게 된다 —
// 그래서 끝에서 StandingMaxLag 手 안의 것만 받는다.
//
// 부호를 뒤집는 자리다. EvalCp 는 先手 관점이고 여기서 필요한 것은 사람 관점이다.
// 手合割의 기준점도 여기서 뺀다 — 「이기고 있었나」는 手合割에 대해 묻는 것이라야 뜻이 있다.
func standingOf(rec store.GameRecord) explain.Standing {
	last, best := 0, -1
	var cp int
	for _, m := range rec.Moves {
		if m.Ply > last {
			last = m.Ply
		}
		if m.EvalCp != nil && m.Ply > best {
			best, cp = m.Ply, *m.EvalCp
		}
	}
	if best < 0 || last-best > explain.StandingMaxLag {
		return explain.StandingUnknown
	}
	// 手合割의 기준점을 뺀다. 둘 다 先手 관점이라 뒤집기 전에 뺀다(handicap.BaselineCp).
	// 안 빼면 二枚落ち에서 +1386을 +900까지 흘린 판이 「圧倒的に有利でした」로 나간다 —
	// 판정과 같은 좌표를 써야 총평도 같은 사실을 말한다.
	cp -= handicap.BaselineCp(rec.StartSFEN)
	if rec.MyColor != "b" {
		cp = -cp
	}

	switch rate := intervene.WinRate(cp); {
	case rate >= explain.StandingAheadRate:
		return explain.StandingAhead
	case rate <= 1-explain.StandingAheadRate:
		return explain.StandingBehind
	default:
		return explain.StandingLevel
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
		// abandoned 뿐이다.
		return explain.OutcomeUnfinished
	}
}
