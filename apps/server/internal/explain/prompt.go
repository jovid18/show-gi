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
- アプリが手を止めたこと・戻したことは書かない。画面がすでに伝えている。その手の何が悪いかだけを書く
- 説明だけを出力する。前置きも引用符も付けない`

// categoryJa 는 카테고리를 프롬프트에 적을 일본어로 옮긴다.
//
// 영어 식별자(`hangs_piece`)를 그대로 넣지 않는 이유는, 그것이 우리 코드의 이름이지 사실의
// 서술이 아니기 때문이다. **`other` 는 「이유를 특정하지 못했다」까지 말한다** — 모른다고
// 적어야 LLM이 이유를 지어내지 않는다. 이 줄이 없으면 3분의 2의 개입에서 그럴듯한 거짓말이
// 나온다(06-status.md §17).
//
// **결정적 문구와 같은 사실을 들고 있어야 한다.** 실모델로 재보니 여기가 템플릿보다 빈약한
// 자리에서 모델이 그 빈 곳을 채우면서 **틀린 것을 썼다**(06-status.md §28). 프롬프트가 사실을
// 덜 주면 「사실을 문장으로 바꾼다」가 성립하지 않는다 — 남은 자리를 모델이 메운다.
var categoryJa = map[intervene.Category]string{
	// **주체를 못 박는다.** 「詰みがあったのに逃した」로만 줬더니 실모델이 「終盤で相手に詰みが
	// ある局面です」로 뒤집어 썼다. 내가 상대를 詰ませる 수를 놓친 것인데 내가 詰まされる 것으로
	// 읽히고, 방향이 반대인 설명은 초심자가 검증할 수단이 없다.
	intervene.CategoryMissedMate: "自分が相手を詰ませられる手があったのに、この手で逃してしまう",
	// **여기도 주체를 못 박는다 — `missed_mate` 의 거울상이라 같은 함정이 있다.** 그쪽은
	// 「내가 詰ませる 기회를 놓쳤다」인데 실모델이 「내가 詰まされる」로 뒤집어 썼다(§28).
	// 이쪽은 진짜로 내가 詰まされる 쪽이므로, 두 카테고리가 프롬프트에서 서로 반대라는 것을
	// 문장으로 다 적어야 방향이 안 섞인다.
	intervene.CategoryLetsMate:    "この手のせいで自玉が詰まされる（自分が相手を詰ませる話ではない）",
	intervene.CategoryHangsPiece:  "置いた駒がそのまま取られる（取り返せない）",
	intervene.CategoryShallowTrap: "一手先だけ得に見えて、その先で形勢が入れ替わる",
	// **「敵陣から出る手も成れる」까지 준다.** 이 카테고리가 생긴 이유가 그 규칙을 놓친
	// 실수였는데(08-playtest.md §6-6), 안 주면 실모델이 「敵陣に入ったときに成れる」로 쓴다 —
	// 놓친 오해를 그대로 남기는 설명이다.
	intervene.CategoryUnpromoted:    "動き自体は正しいが、成っていない。敵陣から出る手も成れるのを見落としている",
	intervene.CategoryGreedyCapture: "駒は取れるが、払う代償のほうが大きい",
	intervene.CategoryIdleCheck:     "王手はかかるが続きがなく、手番を渡すだけ",
	// **「相手の攻めが届く」까지 준다.** 여기만 결정적 문구보다 사실이 하나 적었고, 사전
	// 생성으로 세 레벨을 나란히 놓아 보니 셋 다 「自玉のまわりが手薄になりました」에서
	// 끝났다 — 템플릿이 말하는 것을 LLM 쪽이 못 말한 자리다(06-status.md §38). 판정도
	// 두 값을 같이 본다(ShieldLoss > 0 かつ ThreatGain > 0).
	intervene.CategoryKingExposed: "自玉のまわりの守りが減り、同時に相手の攻めの利きが増えた",
	intervene.CategoryOther:       "形勢を大きく損ねる。ただし理由は特定できていない",
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
//
// knowledge 는 `kb_chunks` 에서 태그로 찾은 참고 지식이다. 비어 있으면 안 붙는다 —
// 그때의 프롬프트는 이 함수의 이전 버전과 **글자 한 자 다르지 않다.** 기존 캐시가
// 그래서 무효화되지 않는다(태그 없는 키는 바뀌지 않았다).
func userPrompt(f Facts, knowledge []KbSnippet) string {
	u := f.used()

	var b strings.Builder
	b.WriteString("次の事実を説明してください。\n\n")

	// 제지형은 「이미 되물러졌다」는 상황까지 말해줘야 문장의 시점이 맞는다. 지금 이 수를
	// 두려는 것이 아니라 둬 봤고 되돌려진 것이다.
	//
	// **주고 나서 쓰지 말라고 한다.** 시점을 정하는 데는 필요한데 문장에 옮겨 적으면
	// 60자의 절반이 앱 동작 설명이 된다 — 실제로 8개 중 5개가 「アプリが手を戻しました」로
	// 끝났고, 그 자리는 왜 나쁜지가 들어갈 자리였다(06-status.md §38). 그래서 사실로는
	// 주되 systemPrompt 가 출력에서 막는다.
	if u.Kind == intervene.KindBlunder {
		b.WriteString("状況: 初心者が指そうとした手を、アプリが指す前に止めて戻した\n")
	}
	fmt.Fprintf(&b, "読み手: %s\n", levelJa[u.Level])
	fmt.Fprintf(&b, "問題: %s\n", categoryJa[u.Category])

	if u.LostMate {
		// 「こちらに詰みがあった」로 적었더니 모델이 그 「こちら」를 상대로 읽었다.
		// 누가 누구를 詰ませるのか를 문장으로 다 적는다.
		b.WriteString("補足: 終盤。自分が相手を詰ませられる局面だった（自分が詰まされる話ではない）\n")
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
	if u.MatePlies > 0 {
		// **手数를 주지 않으면 키만 갈리고 문장은 같아진다.** 3手와 9手가 다른 키인데
		// 프롬프트가 같으면 캐시가 두 배로 늘면서 내용은 안 늘고, 결정적 문구만 手数를
		// 말하는 상태가 된다 — 「프롬프트가 사실을 덜 주면 남은 자리를 모델이 메운다」의
		// 반대쪽 실패다.
		fmt.Fprintf(&b, "詰まされるまでの手数: %d手（証明済み。受けが必要）\n", u.MatePlies)
	}
	if len(knowledge) > 0 {
		b.WriteString("\n参考知識（この局面に該当するもの）:\n")
		for _, k := range knowledge {
			fmt.Fprintf(&b, "・%s — %s\n", k.Title, k.Body)
		}
	}
	return b.String()
}
