package explain

import (
	"fmt"
	"strings"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// 대국 후 총평. 개입 문구와 같은 규약 위에 있다. 재료를 결정적으로 세고 그 재료만으로
// 문장을 만든다. 무엇을 재료로 주는지가 곧 이 기능의 정직성이고, 그 목록이 GameFacts 다.

// SummaryMaxRunes 는 총평의 상한이다. 절이 최대 넷까지 이어 붙으므로 문구를 하나 늘리면
// 넘을 수 있고, 테스트가 조합 전수로 확인한다.
const SummaryMaxRunes = 160

// Outcome 은 판이 어떻게 끝났는가다. 화면에 나가지 않는 식별자라 영어로 둔다.
type Outcome string

const (
	OutcomeWon        Outcome = "won"
	OutcomeLost       Outcome = "lost"
	OutcomeDrawn      Outcome = "drawn"
	OutcomeUnfinished Outcome = "unfinished" // 끝나지 않고 끊긴 판
)

// Phase 는 개입이 몰린 구간이다. 手数 대신 구간으로 적는다. 「23手目」은 되짚기 화면이
// 이미 짚어 준다.
type Phase string

const (
	PhaseNone   Phase = "none" // 개입이 없었다
	PhaseEarly  Phase = "early"
	PhaseMiddle Phase = "middle"
	PhaseLate   Phase = "late"
	PhaseEven   Phase = "even" // 어느 구간에도 몰리지 않았다
)

// Weight 는 얼마나 자주 걸렸나를 등급으로 말한 것이다.
//
// 정확한 횟수는 화면의 표가 그리고(summaryStats), 여기는 한 번 걸린 판에 「何度も」라고
// 쓰지 않을 만큼만 안다. 등급이 횟수에서 나오므로 표와 어긋날 수 없다.
type Weight string

const (
	WeightNone Weight = "none"
	WeightOnce Weight = "once" // 한 번
	WeightSome Weight = "some" // 몇 번
	WeightMany Weight = "many" // 여러 번
)

// Standing 은 판이 끝난 시점의 형세다. 사람 관점이고, 마지막으로 채워진 평가치에서 온다.
//
// 결과(Outcome)만으로는 이기고 있는데 投了한 판을 말할 수 없다(journal §68). 형세를 보지
// 않으면 「졌다 = 무너졌다」가 되어, 사실은 이기고 있던 판을 그렇게 배운다.
//
// 投了를 기록에서 직접 읽지 않는다. games 에 종료 사유 칸이 없다. 詰まされた 판은 마지막
// 평가치가 자기 쪽으로 크게 기울 수 없으므로, lost 이면서 Ahead 인 것은 던진 것이다.
type Standing string

const (
	StandingUnknown Standing = "unknown" // 평가치가 없거나 너무 오래된 것뿐이다
	StandingAhead   Standing = "ahead"
	StandingLevel   Standing = "level"
	StandingBehind  Standing = "behind"
)

// StandingAheadRate 는 「분명히 이기고 있었다」로 부를 승률이다. cp 는 우세 구간에서
// 의미가 압축되므로 승률로 적는다(intervene.WinRate). K=600에서 0.85는 +1041cp 언저리다.
//
// [미확정] 0.85는 초기값이다. 넘으면 문장 하나가 갈리고, 넘지 못하면 지금까지와 같다.
const StandingAheadRate = 0.85

// StandingMaxLag 는 마지막 평가치가 판의 끝에서 몇 手까지 떨어져 있어도 되는가다.
//
// 평가치는 수보다 늦게 온다(store.RecordedMove). 보지 않으면 40手 전의 형세로
// 「有利でした」를 말하게 된다. 한 왕복까지만 받는다.
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
// 숫자도 판도 수도 담지 않는다. 手数와 개입 횟수는 화면이 따로 그리고, 한 수를 짚는 것은
// 되짚기 화면의 일이다. 같은 수를 문장에도 적으면 두 벌이 되어 어긋난다.
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
	// Intervened 는 그 판에서 개입이 돌았나다. 거짓이면 판정은 했지만 누구도 그 수를 막지
	// 않았고, 그 수는 기보에 남아 있다. 「戻す」로 말하는 문장 넷이 갈린다(journal §126).
	//
	// 제로값이 「막지 않았다」인 것이 안전한 쪽이다. 반대로 두면 가져온 판이 거짓을 말한다.
	Intervened bool
}

// RenderSummary 는 한 판의 총평이다. 재료(GameFacts)에 있는 것만 말한다.
func RenderSummary(f GameFacts) string {
	var b strings.Builder
	b.WriteString(openingJa(f))

	if len(f.Top) == 0 {
		// 개입이 없었다. 칭찬으로 끝내지 않는다. 한 판에서 걸리지 않은 것은 실력의 증거가
		// 되지 못한다. 「형세 손해가 없었다」로도 말하지 않는다. 임계치를 넘지 않았다는
		// 뜻일 뿐 손해가 없었다는 보장이 되지 못한다(journal §52).
		if f.Intervened {
			// 화면의 표가 말하는 戻した回数 0 과 같은 것을 말하는 문장이다.
			b.WriteString("手を戻す場面はありませんでした。")
		} else {
			b.WriteString("大きな悪手は見つかりませんでした。")
		}
		b.WriteString("もう一局、少し長い将棋も試してみましょう。")
		return b.String()
	}

	b.WriteString(phraseTop(f.Top, f.Weight, f.Intervened))
	if p, ok := phaseJa[f.Phase]; ok && p != "" {
		b.WriteString(p)
	}
	// 「崩れた」는 이기고 있던 판에 쓸 수 없다. 거기서 배울 것은 「더 둘 수 있었다」다.
	if t, ok := trendJa[f.Trend]; ok && t != "" && !(f.Standing == StandingAhead && f.Trend == TrendWorsened) {
		b.WriteString(t)
	}
	return b.String()
}

// openingJa 는 판의 결과를 여는 한 문장이다.
//
// 이기고 있는데 진 판을 따로 말한다(Standing). 회차 1이 +1782에서 던진 판이었고, 총평이
// 「負けました。…後半に崩れた」로 말했다.
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

// phaseJa 는 구간을 말하는 절이다. even 과 none 은 비운다. 없는 특징은 말하지 않는다.
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

// phraseTop 은 「무엇으로 걸렸나」다. 이름은 개입 카드와 같은 어휘를 쓴다(CategoryJa).
func phraseTop(top []intervene.Category, w Weight, intervened bool) string {
	first := CategoryJa(top[0])
	if !intervened {
		// 가져온 판. 그 수가 기보에 남아 있으므로 「戻す」 대신 「あった」로 말한다.
		if len(top) == 1 {
			if w == WeightOnce {
				return fmt.Sprintf("「%s」が一度ありました。", first)
			}
			return fmt.Sprintf("「%s」がいちばん多かったです。", first)
		}
		return fmt.Sprintf("「%s」と「%s」がありました。", first, CategoryJa(top[1]))
	}
	if len(top) == 1 {
		if w == WeightOnce {
			return fmt.Sprintf("「%s」で一度戻しました。", first)
		}
		return fmt.Sprintf("「%s」で戻したことが一番多かったです。", first)
	}
	return fmt.Sprintf("「%s」と「%s」で戻しました。", first, CategoryJa(top[1]))
}
