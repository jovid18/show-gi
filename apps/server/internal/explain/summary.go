package explain

import (
	"fmt"
	"strings"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// 대국 후 총평. 개입 문구와 같은 규약 위에 선다 — 재료를 결정적으로 세고, 그 재료만으로
// 문장을 만든다. 다른 것은 재료와 문장의 길이뿐이다.
//
// 무엇을 재료로 주는지가 곧 이 기능의 정직성이다. 아래 GameFacts 주석이 그 목록이다.

// SummaryMaxRunes 는 총평의 상한이다. RenderSummary 가 이 안에 들어오는지를 테스트가
// 조합 전수로 확인한다 — 절이 최대 넷까지 이어 붙으므로 문구를 하나 늘리면 넘을 수 있고,
// 그 이상은 사람이 대국 직후에 읽지 않는다.
const SummaryMaxRunes = 160

// Outcome 은 판이 어떻게 끝났는가다. 화면 문자열이 아니라 식별자라 영어로 둔다.
type Outcome string

const (
	OutcomeWon        Outcome = "won"
	OutcomeLost       Outcome = "lost"
	OutcomeDrawn      Outcome = "drawn"
	OutcomeUnfinished Outcome = "unfinished" // 끝나지 않고 끊긴 판
)

// Phase 는 개입이 몰린 구간이다. 手数가 아니라 구간으로 적는다 — 「23手目」은 사람이
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

// Weight 는 얼마나 자주 걸렸나를 등급으로 말한 것이다.
//
// 정확한 횟수가 아니라 등급이다. 숫자는 화면의 표가 그리고(summaryStats) 여기는
// 문장이 과장하지 않을 만큼만 안다 — 한 번 걸린 판에 「何度も」라고 쓰는 것을 막는 자리다.
// 등급과 표는 어긋날 수 없다: 등급이 횟수에서 나온다.
type Weight string

const (
	WeightNone Weight = "none"
	WeightOnce Weight = "once" // 한 번
	WeightSome Weight = "some" // 몇 번
	WeightMany Weight = "many" // 여러 번
)

// Standing 은 판이 끝난 시점의 형세다. 사람 관점이고, 마지막으로 채워진 평가치에서 온다.
//
// 결과(Outcome)만으로는 못 말하는 것이 하나 있어서 둔다 — 이기고 있는데 投了한 판이다.
// 회차 1이 그랬다: 295手에 +1782인데 사람이 던졌고, 총평은 「負けました…後半に崩れた」로
// 말했다. 형세를 안 보면 「졌다 = 무너졌다」가 되어, 사실은 이기고 있던 판을 그렇게 배운다.
//
// 投了를 기록에서 직접 읽지 않는다. games 에 종료 사유 칸이 없고, 있어도 이 문장이
// 필요한 것은 사유가 아니라 형세다. 詰まされた 판은 마지막 평가치가 자기 쪽으로 크게
// 기울 수 없으므로, lost 이면서 Ahead 인 것은 던진 것이다.
type Standing string

const (
	StandingUnknown Standing = "unknown" // 평가치가 없거나 너무 오래된 것뿐이다
	StandingAhead   Standing = "ahead"
	StandingLevel   Standing = "level"
	StandingBehind  Standing = "behind"
)

// StandingAheadRate 는 「분명히 이기고 있었다」로 부를 승률이다. cp가 아니라 승률로
// 적는다 — cp는 우세 구간에서 의미가 압축되어 같은 값이 국면마다 다른 뜻이 된다
// (intervene.WinRate 의 그 이유). K=600에서 0.85는 +1041cp 언저리다.
//
// [미확정] 0.85는 초기값이다. 이 값이 하는 일은 하나뿐이라 위험이 좁다 — 넘으면
// 문장 하나가 갈리고, 못 넘으면 지금까지와 같은 문장이 나간다.
const StandingAheadRate = 0.85

// StandingMaxLag 는 마지막 평가치가 판의 끝에서 몇 手까지 떨어져 있어도 되는가다.
//
// 평가치는 수보다 늦게 오므로 마지막 몇 수가 비어 있을 수 있다(store.RecordedMove).
// 그것을 안 보면 40手 전의 형세로 「有利でした」를 말하게 된다. 한 왕복까지만 받는다.
const StandingMaxLag = 2

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
// 숫자를 안 담는다. 手数도 개입 횟수도 화면이 따로 그리고, 같은 수를 문장에도 적으면
// 두 벌이 되어 어긋났을 때 어느 쪽이 맞는지 알 수 없다 — Facts 가 Δ승률을 안 싣는 것과
// 같은 이유다.
//
// 판도 수도 없다. 총평은 판 전체의 모양을 말하는 자리이고, 한 수를 짚는 것은 되짚기
// 화면의 일이다.
//
// 이 값들은 전부 기록에서 결정적으로 세어 나온다(server/summary.go).
type GameFacts struct {
	Outcome Outcome
	// Top 은 가장 많이 걸린 카테고리다. 최대 둘이고, 개입이 없었으면 비어 있다.
	Top    []intervene.Category
	Weight Weight
	Phase  Phase
	Trend  Trend
	// Level 은 문장의 눈높이다. 개입 임계치를 정한 그 값이다.
	Level intervene.Level
	// Standing 은 판이 끝난 시점의 형세다. 빈 값은 StandingUnknown 과 같게 다룬다.
	Standing Standing
}

// RenderSummary 는 한 판의 총평이다. 재료(GameFacts)에 있는 것만 말하고, 없는 것은
// 말하지 않는다 — 총평이 판 전체를 말하는 자리라 과장이 가장 쉽게 새는 곳이다.
func RenderSummary(f GameFacts) string {
	var b strings.Builder
	b.WriteString(openingJa(f))

	if len(f.Top) == 0 {
		// 개입이 없었다. 칭찬으로 끝내지 않는다 — 한 판에서 안 걸렸다는 것이 실력의
		// 증거가 못 되고(skill 패키지의 FallRate 와 같은 판단), 다음 판을 두게 만드는 것이
		// 이 화면의 일이다.
		//
		// 「형세 손해가 없었다」로 말하지 않는다. 그건 재지 않은 것이다(journal §52) —
		// 개입이 안 걸린 것은 임계치를 넘지 않았다는 뜻이고, 손해가 없었다는 뜻이 아니다.
		// 화면의 표가 말하는 戻した回数 0 과 같은 것을 말하는 문장이다.
		b.WriteString("手を戻す場面はありませんでした。もう一局、少し長い将棋も試してみましょう。")
		return b.String()
	}

	b.WriteString(phraseTop(f.Top, f.Weight))
	if p, ok := phaseJa[f.Phase]; ok && p != "" {
		b.WriteString(p)
	}
	// 「崩れた」는 이기고 있던 판에 못 쓴다. 그 판에서 배울 것은 무너진 것이 아니라
	// 더 둘 수 있었다는 것이고, 둘은 정반대의 조언이다.
	if t, ok := trendJa[f.Trend]; ok && t != "" && !(f.Standing == StandingAhead && f.Trend == TrendWorsened) {
		b.WriteString(t)
	}
	return b.String()
}

// openingJa 는 판의 결과를 여는 한 문장이다.
//
// 이기고 있는데 진 판을 따로 말한다(Standing). 회차 1이 그 판이었고, 총평이 「負けました。
// …後半に崩れた」로 말했다 — 사실은 +1782에서 던진 판이다.
func openingJa(f GameFacts) string {
	if f.Standing == StandingAhead && f.Outcome == OutcomeLost {
		return "有利な局面でしたが、ここで投了となりました。"
	}
	return outcomeJa[f.Outcome]
}

var outcomeJa = map[Outcome]string{
	OutcomeWon:        "勝ちました。",
	OutcomeLost:       "負けました。",
	OutcomeDrawn:      "引き分けでした。",
	OutcomeUnfinished: "この対局は途中までです。",
}

// phaseJa 는 구간을 말하는 절이다. even 과 none 은 비운다 — 「どこでもつまずいた」는
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

// phraseTop 은 「무엇으로 걸렸나」다. 이름은 개입 카드와 같은 어휘를 쓴다(CategoryJa) —
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
