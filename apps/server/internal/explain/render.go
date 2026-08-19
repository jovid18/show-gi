package explain

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// baseMessages 는 국면 사실이 없을 때 나가는 카테고리별 문구다.
//
// 최선수를 말하지 않는다(01-core.md §1) — 「왜 나쁜가」와 「무엇을 보라」까지다. 짚어주는
// 순간 플레이어가 생각을 멈춘다. idle_check 와 unpromoted 는 플레이 기록이 검증한
// 문장이라 손대지 않는다(08-playtest.md §7).
var baseMessages = map[intervene.Category]string{
	intervene.CategoryMissedMate: "詰みがありました。今の手で逃してしまいます。",
	// 「逃す」를 안 쓴다. 詰み은 그대로 있고 길어졌을 뿐이라, 놓쳤다고 말하면 이기고
	// 있는 사람에게 거짓을 가르친다(journal §76).
	intervene.CategorySlowerMate:  "詰みがありましたが、この手だと遠回りになります。",
	intervene.CategoryLetsMate:    "この手だと自玉が詰まされます。まず受けを考えてみてください。",
	intervene.CategoryHangsPiece:  "その駒は取り返せない場所に置かれています。相手の利きを確かめてみてください。",
	intervene.CategoryShallowTrap: "一手だけ見ると得に見えますが、その先で形勢が入れ替わります。",
	// 잡는 것도 가는 곳도 맞았다고 먼저 말한다. 「その手は」로 시작하면 플레이어는 이동
	// 자체를 의심하고, 실제로 그렇게 세 수를 헤맸다(08-playtest.md §8).
	intervene.CategoryUnpromoted:    "その一手で合っていますが、成っていません。敵陣から出る手も成れます。",
	intervene.CategoryGreedyCapture: "駒は取れますが、払う代償のほうが大きくなります。",
	intervene.CategoryIdleCheck:     "王手はかかりますが続きがなく、手番を渡すだけになります。",
	intervene.CategoryKingExposed:   "自玉のまわりが手薄になり、相手の攻めが届きます。",
}

// unknownMessage 는 미분류일 때다.
//
// 틀린 이유를 지어내지 않는다 — 형세가 나빠졌다는 것만은 확실하고 그 이상은 모르므로
// 그 이상 말하지 않는다. 플레이 기록은 이 문장을 「아무것도 알려주지 않는다」고 적었고
// (16회 전부 같은 문장이었다), 그래서 사실이 있으면 아래에서 한 줄이 붙는다.
const unknownMessage = "その手は形勢を大きく損ねます。もう一度考えてみてください。"

// Render 는 사실을 결정적으로 일본어 문장으로 바꾼다. 개입 카드에 나가는 문장은 전부
// 여기서 나온다 — 같은 사실에는 같은 문장이고, 사실이 없으면 카테고리 문구까지다.
//
// 레벨을 안 본다. [미확정] 레벨별 문구가 필요한지.
func Render(f Facts) string {
	u := f.used()

	switch u.Category {
	case intervene.CategoryHangsPiece:
		// 「取り返せない」의 주어가 모호하다는 지적을 여기서 받는다 — 내가 되딸 駒가 없다는
		// 뜻이었는데 상대가 못 잡는다고도 읽혔다(08-playtest.md §7). 그리고 몇 장이 노리는지를
		// 숫자로 말한다. 플레이어의 실수는 거의 전부 「利き을 한 개 빠뜨림」이었다(08-playtest.md §6).
		if u.MovedPiece != "" && u.Attackers > 0 && !u.Defended {
			return fmt.Sprintf("その%sを取れる相手の駒が%d枚あり、取り返す駒がありません。", u.MovedPiece, u.Attackers)
		}

	case intervene.CategoryLetsMate:
		if u.MatePlies > 0 {
			return fmt.Sprintf("この手だと%d手で自玉が詰まされます。まず受けを考えてみてください。", u.MatePlies)
		}

	case intervene.CategorySlowerMate:
		// 앞의 手数만 숫자로 말한다. 착수 후의 手数는 증명이 아니라 탐색이 본 값이라
		// 쓰면 틀린 手数를 가르친다(Facts.MateBefore).
		if u.MateBefore > 0 {
			return fmt.Sprintf("%d手で詰ませられましたが、この手だと遠回りになります。", u.MateBefore)
		}

	case intervene.CategoryGreedyCapture:
		// 「駒は」를 그 駒의 이름으로 바꾼다. 무엇과 무엇을 바꾼 셈인지가 그래야 보인다.
		if u.Captured != "" {
			return fmt.Sprintf("%sは取れますが、払う代償のほうが大きくなります。", u.Captured)
		}

	case intervene.CategoryOther:
		// 이유는 모르지만 그래서 어떻게 되는지는 안다. 갈래가 있으면 그것이 이 문구가
		// 말할 수 있는 가장 구체적인 것이다.
		if u.namesMoves() {
			return renderBranches(u)
		}
		// 갈래를 못 구했으면 무엇을 잡히는지까지는 안다. 반박 수순의 첫 수가 그것이고,
		// 그 수가 합법이라는 것은 룰 엔진이 확인했다. 「잃습니다」가 아니라 「取れます」인
		// 것은 실제로 그렇게 둘지는 상대가 정하기 때문이다.
		if u.Threatened != "" {
			return fmt.Sprintf("その手は形勢を大きく損ねます。相手は%sを取れます。", u.Threatened)
		}
	}

	if m, ok := baseMessages[u.Category]; ok {
		return m
	}
	return unknownMessage
}

// renderBranches 는 갈래 셋을 줄 단위로 적는다. 화면이 pre-line 으로 받는다.
func renderBranches(u Facts) string {
	var b strings.Builder
	switch {
	case u.OpponentBest == "":
		b.WriteString("この手のあとはこうなります。")
	case len(u.Branches) == 0:
		// 갈래를 못 구해도 최선수 하나는 사실이다. 프롬프트에 그것만 나가는 경우가
		// 있으므로(탐색이 실패했거나 갈래가 전부 검증에서 떨어졌다) 여기도 같은 자리를 갖는다.
		fmt.Fprintf(&b, "この手には%sが厳しく、形勢を大きく損ねます。", u.OpponentBest)
		if u.Threatened != "" {
			fmt.Fprintf(&b, "相手は%sを取れます。", u.Threatened)
		}
		return b.String()
	default:
		fmt.Fprintf(&b, "この手には%sが厳しく、そのあとはこうなります。", u.OpponentBest)
	}
	// 줄은 문장이 아니라 표다. 조사로 이으면 「…で 5手で自分が詰まされる」처럼 で가 겹치고,
	// 그 자리를 피하려고 詰み과 cp의 말투를 가르면 같은 값이 두 어휘를 갖는다. 화살표는 그
	// 둘을 같은 모양으로 세운다 — 프롬프트가 사실을 적는 모양과도 같다.
	for _, br := range u.Branches {
		fmt.Fprintf(&b, "\n%s → %s → %s", br.PlayerJa, br.ReplyJa, BranchScoreJa(br))
	}
	return b.String()
}

// BranchScoreJa 는 갈래 하나의 결말을 적는다.
//
// 詰み을 cp로 말하지 않는다. 30000은 평가치가 아니라 환산값이고, 초심자에게 그 숫자는
// 아무것도 아니다 — 화면 쪽 scoreJa 와 같은 판단이다.
func BranchScoreJa(b Branch) string {
	switch {
	case b.MateIn > 0:
		return fmt.Sprintf("%d手で相手を詰ませられる", b.MateIn)
	case b.MateIn < 0:
		return fmt.Sprintf("%d手で自分が詰まされる", -b.MateIn)
	case b.Cp > 0:
		return "+" + strconv.Itoa(b.Cp)
	default:
		return strconv.Itoa(b.Cp)
	}
}
