package explain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// 대국 후 총평. **개입 문구와 같은 규약 위에 선다** — 캐시·검증·결정적 폴백이 저쪽과
// 한 벌이고(Clean), 다른 것은 재료와 문장의 길이뿐이다.
//
// 로드맵 5번의 「LLM이 사람을 요약하는 첫 자리」이고, 그래서 **무엇을 재료로 주는지가 곧
// 이 기능의 정직성**이다. 아래 GameFacts 주석이 그 목록이다.

// SummaryMaxRunes 는 총평의 상한이다. 개입 문구(MaxRunes)보다 긴 것은 한 판을 두 문장으로
// 말해야 하기 때문이고, 그 이상은 사람이 대국 직후에 읽지 않는다.
const SummaryMaxRunes = 160

// Outcome 은 판이 어떻게 끝났는가다. **화면 문자열이 아니라 식별자**라 영어로 둔다.
type Outcome string

const (
	OutcomeWon        Outcome = "won"
	OutcomeLost       Outcome = "lost"
	OutcomeDrawn      Outcome = "drawn"
	OutcomeUnfinished Outcome = "unfinished" // 끝나지 않고 끊긴 판
)

// Phase 는 개입이 몰린 구간이다. **手数가 아니라 구간으로 적는다** — 「23手目」은 사람이
// 되짚을 수 있는 정보이지만 총평은 판 전체의 모양을 말하는 자리이고, 그 한 수는 이미
// 되짚기 화면이 짚어 준다.
type Phase string

const (
	PhaseNone   Phase = "none" // 개입이 없었다
	PhaseEarly  Phase = "early"
	PhaseMiddle Phase = "middle"
	PhaseLate   Phase = "late"
	PhaseEven   Phase = "even" // 어느 구간에도 안 몰렸다
)

// Weight 는 **얼마나 자주 걸렸나를 등급으로** 말한 것이다.
//
// 정확한 횟수가 아니라 등급인 것이 요점이다. 숫자는 화면의 표가 그리고(summaryStats) 여기는
// 문장이 과장하지 않을 만큼만 안다 — 한 번 걸린 판에 「何度も」라고 쓰는 것을 막는 자리다.
// 등급과 표는 어긋날 수 없다: 등급이 횟수에서 나온다.
type Weight string

const (
	WeightNone Weight = "none"
	WeightOnce Weight = "once" // 한 번
	WeightSome Weight = "some" // 몇 번
	WeightMany Weight = "many" // 여러 번
)

// Trend 는 후반이 나아졌는가다.
type Trend string

const (
	TrendUnknown  Trend = "unknown" // 표본이 모자라 말할 수 없다
	TrendImproved Trend = "improved"
	TrendWorsened Trend = "worsened"
	TrendSteady   Trend = "steady"
)

// GameFacts 는 한 판을 문장으로 바꿀 재료 전부다.
//
// **숫자를 안 담는다.** 手数도 개입 횟수도 화면이 따로 그리고, 같은 수를 문장에도 적으면
// 두 벌이 되어 어긋났을 때 어느 쪽이 맞는지 알 수 없다 — `Facts` 가 Δ승률을 안 싣는 것과
// 같은 이유다(04-llm.md §2).
//
// **판도 수도 없다.** 그래서 문장에 칸이나 棋譜 표기가 나타났다면 모델이 지어낸 것이고,
// `Clean` 이 그것을 버린다.
//
// 이 값들은 전부 **기록에서 결정적으로 세어 나온다**(server/summary.go). LLM은 세지 않는다.
type GameFacts struct {
	Outcome Outcome
	// Top 은 가장 많이 걸린 카테고리다. 최대 둘이고, 개입이 없었으면 비어 있다.
	Top    []intervene.Category
	Weight Weight
	Phase  Phase
	Trend  Trend
	// Level 은 문장의 눈높이다. 개입 임계치를 정한 그 값이다.
	Level intervene.Level
}

// Summarizer 는 한 판을 문장으로 바꾼다. `Explainer` 와 같은 이유로 **에러를 안 돌려준다** —
// 답은 언제나 하나이고(결정적 총평) 실패는 여기서 삼킨다.
type Summarizer interface {
	Summarize(ctx context.Context, f GameFacts) Result
}

// Key 는 캐시 키다.
//
// **개입 문구의 키와 접두사로 갈라 둔다.** 같은 표를 쓰는데(`explain_cache`) 키 공간이
// 섞이면 총평 하나가 개입 문구 자리에 나올 수 있고, 그건 화면에서 「왜 이 수가 나쁜가」에
// 판 전체의 감상이 뜨는 것이다.
//
// **Tier 0이 자주 맞지는 않는다.** 조합이 수백 가지라 히트율이 낮다 — 그래도 두는 이유는
// 총평이 **판마다 한 번**이라 비용이 개입 문구와 비교가 안 되게 작고, 같은 사람이 같은
// 방식으로 무너지는 판은 실제로 반복되기 때문이다.
func (f GameFacts) Key() string {
	tops := make([]string, 0, len(f.Top))
	for _, c := range f.Top {
		tops = append(tops, string(c))
	}
	material := fmt.Sprintf("summary|v%d|%s|%s|%s|%s|%s|%d",
		summaryPromptVersion, f.Outcome, strings.Join(tops, ","), f.Weight, f.Phase, f.Trend, f.Level)
	sum := sha256.Sum256([]byte(material))
	return "s" + hex.EncodeToString(sum[:16])
}

// Summarize 는 Tier 0 → LLM → 결정적 총평으로 내려간다. `Explain` 과 같은 순서이고, 다른
// 것은 프롬프트와 길이 상한뿐이다.
func (l *Layered) Summarize(ctx context.Context, f GameFacts) Result {
	key := f.Key()

	if l.store != nil {
		if body, ok, err := l.store.CachedExplanation(ctx, key); err != nil {
			log.Printf("summary: cache lookup failed for %s: %v", key, err)
		} else if ok {
			return Result{Body: body, Tier: 0}
		}
	}

	if l.client == nil {
		return Result{Body: RenderSummary(f), Tier: TierTemplate}
	}

	call, cancel := context.WithTimeout(ctx, l.timeout())
	defer cancel()

	out, err := l.client.completeSummary(call, f)
	if err != nil {
		log.Printf("summary: failed, falling back to the template: %v", err)
		return Result{Body: RenderSummary(f), Tier: TierTemplate}
	}

	body, ok := Clean(out.body, SummaryMaxRunes)
	if !ok {
		log.Printf("summary: unusable sentence (%d runes), falling back to the template", len([]rune(out.body)))
		return Result{Body: RenderSummary(f), Tier: TierTemplate}
	}

	if l.store != nil {
		// **부른 ctx를 쓰지 않는다** — Explain 과 같은 이유다(그쪽 주석).
		save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if err := l.store.SaveExplanation(save, key, body, out.model); err != nil {
			log.Printf("summary: could not cache %s: %v", key, err)
		}
	}

	// 총평은 국면 고유 사실을 안 쓰므로 층이 하나다. Tier 1로 적는다 — 재사용성이 계층을
	// 가르는 기준이고(04-llm.md §2) 이 키는 카테고리 × 구간 × 결과로만 갈린다.
	return Result{Body: body, Tier: 1, CostYen: out.costYen, Model: out.model, RouterCached: out.routerCached}
}

// RenderSummary 는 **LLM 없이도 나가는 총평**이다.
//
// 사실을 전부 담는 것이 이 설계의 전제다(04-llm.md §2) — 라우터가 죽어도 화면이 비지 않고,
// 그래서 LLM은 「같은 사실을 더 좋은 문장으로」만 하게 된다.
func RenderSummary(f GameFacts) string {
	var b strings.Builder
	b.WriteString(outcomeJa[f.Outcome])

	if len(f.Top) == 0 {
		// 개입이 없었다. **칭찬으로 끝내지 않는다** — 한 판에서 안 걸렸다는 것이 실력의
		// 증거가 못 되고(skill 패키지의 FallRate 와 같은 판단), 다음 판을 두게 만드는 것이
		// 이 화면의 일이다.
		//
		// **「형세 손해가 없었다」로 말하지 않는다.** 그건 재지 않은 것이다(summaryUserPrompt
		// 의 같은 자리 · §52). 아래 표의 `戻した回数 0` 과 같은 것을 말하는 문장이다.
		b.WriteString("手を戻す場面はありませんでした。もう一局、少し長い将棋も試してみましょう。")
		return b.String()
	}

	b.WriteString(phraseTop(f.Top, f.Weight))
	if p, ok := phaseJa[f.Phase]; ok && p != "" {
		b.WriteString(p)
	}
	if t, ok := trendJa[f.Trend]; ok && t != "" {
		b.WriteString(t)
	}
	return b.String()
}

var outcomeJa = map[Outcome]string{
	OutcomeWon:        "勝ちました。",
	OutcomeLost:       "負けました。",
	OutcomeDrawn:      "引き分けでした。",
	OutcomeUnfinished: "この対局は途中までです。",
}

// phaseJa 는 구간을 말하는 절이다. **`even` 과 `none` 은 비운다** — 「どこでもつまずいた」는
// 정보가 아니고, 없는 특징을 말하면 문장이 길어지기만 한다.
var phaseJa = map[Phase]string{
	PhaseEarly:  "つまずいたのは主に序盤です。",
	PhaseMiddle: "つまずいたのは主に中盤です。",
	PhaseLate:   "つまずいたのは主に終盤です。",
	PhaseEven:   "",
	PhaseNone:   "",
}

var trendJa = map[Trend]string{
	TrendImproved: "後半は落ち着いて指せていました。",
	TrendWorsened: "後半に崩れたので、そこから見直すとよさそうです。",
	TrendSteady:   "",
	TrendUnknown:  "",
}

// phraseTop 은 「무엇으로 걸렸나」다. 이름은 개입 카드와 **같은 어휘**를 쓴다(CategoryJa) —
// 두 벌이 되면 같은 실수가 카드와 총평에서 다른 이름으로 불린다.
func phraseTop(top []intervene.Category, w Weight) string {
	first := CategoryJa(top[0])
	if len(top) == 1 {
		if w == WeightOnce {
			return fmt.Sprintf("「%s」で一度戻しました。", first)
		}
		return fmt.Sprintf("「%s」で戻したことが一番多かったです。", first)
	}
	return fmt.Sprintf("「%s」と「%s」で戻しました。", first, CategoryJa(top[1]))
}
