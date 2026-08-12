package explain

import (
	"fmt"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// baseMessages 는 국면 사실이 없을 때 나가는 카테고리별 문구다.
//
// **최선수를 말하지 않는다**(01-core.md §1) — 「왜 나쁜가」와 「무엇을 보라」까지다. 짚어주는
// 순간 플레이어가 생각을 멈춘다. `idle_check` 와 `unpromoted` 는 플레이 기록이 검증한
// 문장이라 손대지 않는다(08-playtest.md §7).
var baseMessages = map[intervene.Category]string{
	intervene.CategoryMissedMate:  "詰みがありました。今の手で逃してしまいます。",
	intervene.CategoryLetsMate:    "この手だと自玉が詰まされます。まず受けを考えてみてください。",
	intervene.CategoryHangsPiece:  "その駒は取り返せない場所に置かれています。相手の利きを確かめてみてください。",
	intervene.CategoryShallowTrap: "一手だけ見ると得に見えますが、その先で形勢が入れ替わります。",
	// **잡는 것도 가는 곳도 맞았다고 먼저 말한다.** 「その手は」로 시작하면 플레이어는 이동
	// 자체를 의심하고, 실제로 그렇게 세 수를 헤맸다(08-playtest.md §8).
	intervene.CategoryUnpromoted:    "その一手で合っていますが、成っていません。敵陣から出る手も成れます。",
	intervene.CategoryGreedyCapture: "駒は取れますが、払う代償のほうが大きくなります。",
	intervene.CategoryIdleCheck:     "王手はかかりますが続きがなく、手番を渡すだけになります。",
	intervene.CategoryKingExposed:   "自玉のまわりが手薄になり、相手の攻めが届きます。",
}

// unknownMessage 는 미분류일 때다.
//
// **틀린 이유를 지어내지 않는다** — 형세가 나빠졌다는 것만은 확실하고 그 이상은 모르므로
// 그 이상 말하지 않는다. 플레이 기록은 이 문장을 「아무것도 알려주지 않는다」고 적었고
// (16회 전부 같은 문장이었다), 그래서 사실이 있으면 아래에서 한 줄이 붙는다.
const unknownMessage = "その手は形勢を大きく損ねます。もう一度考えてみてください。"

// Render 는 사실을 **결정적으로** 일본어 문장으로 바꾼다. LLM이 없을 때·느릴 때·못 쓸 문장을
// 돌려줄 때 나가고, 여기에 사실이 실려 있어서 **이것만으로도 제품이 선다**(04-llm.md §2).
//
// 레벨을 안 본다 — 어휘를 사람에 맞추는 것이 LLM의 일이다. **[미확정]** 레벨별 문구가 필요한지.
func Render(f Facts) string {
	u := f.used()

	switch u.Category {
	case intervene.CategoryHangsPiece:
		// 「取り返せない」의 주어가 모호하다는 지적을 여기서 받는다 — 내가 되딸 駒가 없다는
		// 뜻이었는데 상대가 못 잡는다고도 읽혔다(08-playtest.md §7). 그리고 몇 장이 노리는지를
		// 숫자로 말한다. 플레이어의 실수는 거의 전부 「利き을 한 개 빠뜨림」이었다(§6).
		if u.MovedPiece != "" && u.Attackers > 0 && !u.Defended {
			return fmt.Sprintf("その%sを取れる相手の駒が%d枚あり、取り返す駒がありません。", u.MovedPiece, u.Attackers)
		}

	case intervene.CategoryLetsMate:
		if u.MatePlies > 0 {
			return fmt.Sprintf("この手だと%d手で自玉が詰まされます。まず受けを考えてみてください。", u.MatePlies)
		}

	case intervene.CategoryGreedyCapture:
		// 「駒は」를 그 駒의 이름으로 바꾼다. 무엇과 무엇을 바꾼 셈인지가 그래야 보인다.
		if u.Captured != "" {
			return fmt.Sprintf("%sは取れますが、払う代償のほうが大きくなります。", u.Captured)
		}

	case intervene.CategoryOther:
		// 이유는 모르지만 **무엇을 잡히는지는 안다.** 반박 수순의 첫 수가 그것이고, 그 수가
		// 합법이라는 것은 룰 엔진이 확인했다. 「잃습니다」가 아니라 「取れます」인 것은
		// 실제로 그렇게 둘지는 상대가 정하기 때문이다.
		if u.Threatened != "" {
			return fmt.Sprintf("その手は形勢を大きく損ねます。相手は%sを取れます。", u.Threatened)
		}
	}

	if m, ok := baseMessages[u.Category]; ok {
		return m
	}
	return unknownMessage
}
