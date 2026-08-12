package explain

import (
	"fmt"
	"strings"
)

// summaryPromptVersion 은 프롬프트가 바뀌면 오른다. 올리면 캐시의 옛 총평이 통째로 죽는다 —
// `promptVersion`(facts.go)과 갈라 둔 것은 두 프롬프트가 따로 바뀌기 때문이고, 한 값으로
// 묶으면 개입 문구를 고칠 때 총평 캐시까지 버려진다.
const summaryPromptVersion = 2

// summarySystemPrompt 는 개입 문구의 것과 **같은 원칙, 다른 일**이다.
//
// 저쪽은 한 수가 왜 나빴는지를 말하고 이쪽은 한 판이 어떤 모양이었는지를 말한다. 공통은
// 「준 사실만 쓴다」와 「수를 쓰지 않는다」이고, 그 둘을 안 지키면 `Clean` 이 버린다.
//
// **「무엇을 연습하라」까지 말하게 둔다.** 개입 문구에는 없던 자리인데, 여기는 판이 이미
// 끝나서 답을 알려주는 것이 아니고 그 한 줄이 다음 판을 두게 만드는 것이다.
const summarySystemPrompt = `あなたは将棋の初心者向け学習アプリの文章係です。
一局が終わったあとの短いふりかえりを書いてください。

- 与えられた事実だけを使う。手数・回数・勝率などの数字は書かない（画面が別に出しています）
- 指し手・具体的なマス（例：7六）・棋譜（例：▲7六歩）は書かない
- 全体で2文以内、80字以内
- 「です・ます」調。やさしい言葉で。将棋の用語（利き・成る・持ち駒）はそのまま使う
- 責めない。次に何を意識すればよいかを最後に一言そえる
- ふりかえりだけを出力する。前置きも引用符も付けない`

// summaryUserPrompt 는 사실을 줄 단위로 적는다. **여기 적히지 않은 것은 LLM이 모른다** —
// `userPrompt` 와 같은 규약이고, 그래서 이 함수가 곧 총평의 「말해도 되는 것」의 경계다.
func summaryUserPrompt(f GameFacts) string {
	var b strings.Builder
	b.WriteString("次の事実で、一局のふりかえりを書いてください。\n\n")

	b.WriteString(fmt.Sprintf("- 結果: %s\n", summaryOutcomeJa[f.Outcome]))
	b.WriteString(fmt.Sprintf("- 相手のレベル設定: %s\n", levelJa[f.Level]))

	if len(f.Top) == 0 {
		// **「없었다」를 적어 준다.** 안 적으면 모델이 실수를 지어낸다 — `other` 에
		// 「이유를 특정하지 못했다」를 적는 것과 같은 자리다(prompt.go).
		b.WriteString("- 大きな形勢損はなかった\n")
	} else {
		b.WriteString("- つまずいた形: ")
		names := make([]string, 0, len(f.Top))
		for _, c := range f.Top {
			names = append(names, fmt.Sprintf("%s（%s）", CategoryJa(c), categoryJa[c]))
		}
		b.WriteString(strings.Join(names, "、"))
		b.WriteString("\n")
		// **양을 등급으로 준다.** 안 주면 한 번 걸린 판에도 「何度も」라고 쓴다 —
		// 실제로 그렇게 나갔다(06-status.md §49).
		b.WriteString(fmt.Sprintf("- つまずいた量: %s\n", summaryWeightJa[f.Weight]))
	}

	if s := summaryPhaseJa[f.Phase]; s != "" {
		b.WriteString(fmt.Sprintf("- つまずいた時期: %s\n", s))
	}
	if s := summaryTrendJa[f.Trend]; s != "" {
		b.WriteString(fmt.Sprintf("- 後半の様子: %s\n", s))
	}
	return b.String()
}

var summaryOutcomeJa = map[Outcome]string{
	OutcomeWon:        "勝ち",
	OutcomeLost:       "負け",
	OutcomeDrawn:      "引き分け",
	OutcomeUnfinished: "途中で終わった（最後まで指していない）",
}

// summaryWeightJa 는 등급의 말이다. **「1回」처럼 숫자로 적지 않는다** — 화면의 표가 숫자를
// 그리고, 문장에는 「一度だけ」로 충분하다.
var summaryWeightJa = map[Weight]string{
	WeightOnce: "一度だけ",
	WeightSome: "数回",
	WeightMany: "何度も",
	WeightNone: "なし",
}

var summaryPhaseJa = map[Phase]string{
	PhaseEarly:  "主に序盤",
	PhaseMiddle: "主に中盤",
	PhaseLate:   "主に終盤",
	PhaseEven:   "", // 특징이 없으면 안 적는다. 없는 특징을 주면 모델이 그것을 말한다
	PhaseNone:   "",
}

var summaryTrendJa = map[Trend]string{
	TrendImproved: "後半のほうが落ち着いていた",
	TrendWorsened: "後半のほうが崩れた",
	TrendSteady:   "",
	TrendUnknown:  "",
}
