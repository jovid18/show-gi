package explain

import (
	"fmt"
	"strings"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// systemPrompt 는 「판단은 엔진, 표현만 LLM」을 프롬프트에 그대로 적은 것이다.
//
// **일본어인 이유가 둘이다.** 출력이 일본어여야 하고(사용자가 일본인이다), 프롬프트에 한글이
// 섞이면 출력이 한글로 새면서 `temperature=0` 캐시 키까지 갈라진다(docs/04-llm.md §1).
//
// 「지시를 어기면 버린다」가 코드 쪽에 있다(clean). 프롬프트는 부탁이고 검증이 규칙이다.
const systemPrompt = `あなたは将棋の初心者向け学習アプリの文章係です。
与えられた事実だけを、やさしい日本語の短い文にしてください。

- 事実に書かれていないことは足さない。推測も評価も加えない
- 指し手は書かない。次の一手・最善手・具体的なマス（例：7六）を挙げてはいけない
- 全体で2文以内、60字以内
- 「です・ます」調。将棋の用語（利き・成る・持ち駒）はそのまま使う
- 説明だけを出力する。前置きも引用符も付けない`

// categoryJa 는 카테고리를 프롬프트에 적을 일본어로 옮긴다.
//
// 영어 식별자(`hangs_piece`)를 그대로 넣지 않는 이유는, 그것이 우리 코드의 이름이지 사실의
// 서술이 아니기 때문이다. **`other` 는 「이유를 특정하지 못했다」까지 말한다** — 모른다고
// 적어야 LLM이 이유를 지어내지 않는다. 이 줄이 없으면 3분의 2의 개입에서 그럴듯한 거짓말이
// 나온다(06-status.md §17).
var categoryJa = map[intervene.Category]string{
	intervene.CategoryMissedMate:    "詰みがあったのに逃した",
	intervene.CategoryHangsPiece:    "置いた駒がそのまま取られる（取り返せない）",
	intervene.CategoryShallowTrap:   "一手先だけ得に見えて、その先で形勢が入れ替わる",
	intervene.CategoryUnpromoted:    "動き自体は正しいが、成っていない",
	intervene.CategoryGreedyCapture: "駒は取れるが、払う代償のほうが大きい",
	intervene.CategoryIdleCheck:     "王手はかかるが続きがなく、手番を渡すだけ",
	intervene.CategoryKingExposed:   "自玉のまわりが手薄になった",
	intervene.CategoryOther:         "形勢を大きく損ねる。ただし理由は特定できていない",
}

// levelJa 는 읽는 사람의 실력 구간이다. 어휘를 여기에 맞추는 것이 LLM이 하는 일이다.
var levelJa = map[intervene.Level]string{
	intervene.Beginner:     "入門者（駒の動きを覚えたばかり）",
	intervene.Novice:       "初級者",
	intervene.Intermediate: "中級者",
}

// userPrompt 는 사실을 줄 단위로 적는다.
//
// **여기 적히지 않은 것은 LLM이 모른다.** 그래서 이 함수가 곧 「무엇을 말해도 되는가」의
// 경계이고, 목록은 `used` 가 정한다 — 프롬프트에만 있는 사실은 언젠가 문장으로 새어 나온다.
func userPrompt(f Facts) string {
	u := f.used()

	var b strings.Builder
	b.WriteString("次の事実を説明してください。\n\n")

	// 제지형은 「이미 되물러졌다」는 상황까지 말해줘야 문장의 시점이 맞는다. 지금 이 수를
	// 두려는 것이 아니라 둬 봤고 되돌려진 것이다.
	if u.Kind == intervene.KindBlunder {
		b.WriteString("状況: 初心者が指そうとした手を、アプリが指す前に止めて戻した\n")
	}
	fmt.Fprintf(&b, "読み手: %s\n", levelJa[u.Level])
	fmt.Fprintf(&b, "問題: %s\n", categoryJa[u.Category])

	if u.LostMate {
		b.WriteString("補足: 終盤で、こちらに詰みがあった局面\n")
	}
	if u.MovedPiece != "" {
		fmt.Fprintf(&b, "動かした駒: %s\n", u.MovedPiece)
	}
	if u.Captured != "" {
		fmt.Fprintf(&b, "取った駒: %s\n", u.Captured)
	}
	if u.Attackers > 0 {
		fmt.Fprintf(&b, "その駒を取れる相手の駒: %d枚\n", u.Attackers)
		if u.Defended {
			b.WriteString("取り返す駒: ある\n")
		} else {
			b.WriteString("取り返す駒: なし\n")
		}
	}
	if u.Threatened != "" {
		fmt.Fprintf(&b, "この手のあと相手が取れる駒: %s\n", u.Threatened)
	}
	return b.String()
}
