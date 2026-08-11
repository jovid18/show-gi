package explain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// Facts 는 문장으로 바꿀 **이미 정해진 사실들**이다.
//
// 전부 결정적으로 구해진 값이고, 판·SFEN·cp·평가치는 여기 없다. `intervene.Features` 가
// 카테고리를 정하기 위해 좁혀진 입력이라면 이쪽은 **말할 수 있는 것**으로 좁혀진 입력이다.
//
// 없는 것 둘이 설계다.
//
// **칸이 없다**(8四 같은 것). 문장이 칸을 말하지 않기 때문이다 — 어느 칸인지는 판이 빛으로,
// 카드가 棋譜(▲8四銀)로 이미 말한다. 그래서 문장은 「その銀」까지만 말하면 되고, 대신
// 캐시 키가 81배로 늘지 않는다.
//
// **Δ승률이 없다.** 카드에 「勝率 −N%」 막대가 따로 있고, 숫자를 프롬프트에 주면 LLM이
// 그것을 문장에 옮겨 적어 **막대와 두 벌**이 된다. 어긋났을 때 어느 쪽이 맞는지 알 수 없다.
type Facts struct {
	// Kind 는 제지형(blunder)인가 제안형(tesuji)인가다.
	//
	// 지금은 제지형뿐인데도 키에 들어간다. 「なぜ悪いか」와 「ここに何かある」는 문장이
	// 아예 다르므로, 제안형이 붙는 날 같은 키로 묶이면 캐시가 엉뚱한 문장을 돌려준다.
	Kind intervene.Kind
	// Category 는 왜 나쁜가다. 이 패키지는 그 값을 **읽기만** 한다.
	Category intervene.Category
	// Level 은 읽는 사람의 실력 구간이다. 문장의 어휘가 여기서 갈린다.
	Level intervene.Level
	// LostMate 는 종반 판정(詰み을 놓쳤다)으로 걸렸는가.
	LostMate bool

	// Known 이 false면 판을 못 읽어 아래 사실들이 없는 것이다. **지어내지 않는다** —
	// 카테고리도 그때 other 로 떨어진다.
	Known bool

	// MovedPiece 는 움직인 駒의 한자다(성했으면 「成銀」처럼 성한 이름).
	MovedPiece string
	// Captured 는 이 수로 딴 駒의 한자. 안 땄으면 빈 문자열.
	Captured string
	// Attackers 는 놓인 칸의 駒를 **실제로 딸 수 있는 상대 駒의 매수**다.
	//
	// 「노리는 매수」가 아니다 — 핀에 묶여 못 움직이는 駒는 세지 않는다. 그리고 **수가
	// 아니라 매수**다. 같은 駒의 成·不成은 두 수지만 한 장이라, 수로 세면 화면이
	// 「2枚あり」라고 거짓을 말한다.
	Attackers int
	// Defended 는 따인 뒤에 되딸 수 있는가.
	Defended bool
	// Threatened 는 반박 수순의 **첫 수로 상대가 딸 수 있는 내 駒**의 한자다. 없으면 빈 값.
	//
	// 카테고리가 이유를 못 대는 3분의 2에 「무엇을 잃는가」를 주는 자리다(06-status.md §25).
	// 「잃습니다」가 아니라 「取れます」로 말한다 — 실제로 그렇게 될지는 상대가 정하지만,
	// 그 수가 합법이라는 것은 룰 엔진이 확인한 사실이다.
	Threatened string
}

// promptVersion 은 프롬프트나 사실 목록이 바뀌면 올린다.
//
// **이게 없으면 프롬프트를 고쳐도 캐시가 옛 문장을 영원히 돌려준다.** 캐시가 있는 계층에서
// 프롬프트를 고치는 것은 곧 키를 바꾸는 일이다. 마이그레이션도 비우는 일도 필요 없어진다 —
// 옛 행은 아무도 찾지 않는 키로 남고, 「그때 어떤 문장을 냈나」의 기록이 된다.
//
//	v2  systemPrompt 에 「アプリが止めた・戻したは書かない」이 붙었다 (06-status.md §38).
//	    사전 생성이 이 값을 실제로 쓴다 — 004_explain_cache_tier1.sql 의 키가 전부 v2 다
const promptVersion = 2

// Key 는 `explain_cache.key` 다. **국면이 아니라 사실의 모양이 키다.**
//
// 같은 모양의 실수에는 같은 문장이 나가는 것이 맞다 — 카테고리는 유한하고, 그래서 설명의
// 대부분이 캐시로 덮인다(docs/04-llm.md §2). 여기 들어가는 목록은 `used` 가 남긴 것과
// 정확히 같다.
func (f Facts) Key() string {
	sum := sha256.Sum256([]byte(f.keyMaterial()))
	return hex.EncodeToString(sum[:])
}

// keyMaterial 은 해시하기 **전**의, 사람이 읽을 수 있는 키다.
//
// 키가 왜 갈렸는지 재현할 수 있어야 한다. 사전 생성이 그것을 실제로 쓴다 — 만들어 둔 행이
// 어느 사실 모양의 것인지를 마이그레이션에 주석으로 적는다(004_explain_cache_tier1.sql).
// 해시만 적혀 있으면 그 파일을 사람이 읽고 확인할 방법이 없다.
func (f Facts) keyMaterial() string {
	u := f.used()
	return fmt.Sprintf("v%d|%s|%s|%d|mate=%t|known=%t|moved=%s|cap=%s|atk=%d|def=%t|thr=%s",
		promptVersion, u.Kind, u.Category, u.Level, u.LostMate,
		u.Known, u.MovedPiece, u.Captured, u.Attackers, u.Defended, u.Threatened)
}

// Tier 는 이 사실들이 어느 층으로 가는지다.
//
// **문장에 국면 고유의 숫자나 駒가 들어가는가**로 갈린다. 안 들어가면 남는 것은 카테고리와
// 레벨뿐이라 키가 스물넷(카테고리 8 × 레벨 3)이고, 한 번 만든 문장이 계속 재사용된다.
// 들어가면 그만큼 키가 넓어지는 대신 문장이 그 국면을 짚는다.
func (f Facts) Tier() int {
	u := f.used()
	if u.Known && (u.Attackers > 0 || u.Captured != "" || u.Threatened != "") {
		return 2
	}
	return 1
}

// used 는 **이 카테고리의 문장이 쓸 수 있는 사실만** 남긴다.
//
// 키·프롬프트·결정적 문구가 전부 이 함수를 지난다. 갈라두면 두 가지가 조용히 어긋난다 —
// 문장에 안 쓰는 사실 때문에 캐시가 갈려 히트율이 떨어지고, 반대로 프롬프트에만 있는
// 사실은 언젠가 문장으로 새어 나온다.
//
// **카테고리마다 말할 수 있는 것이 다른 것이 요점이다.** `unpromoted` 는 「이동은 맞았고
// 成하지 않았다」로 이미 완결이라 駒도 매수도 붙일 자리가 없고(#26에서 그 순서 하나로
// 오분류를 고쳤다), `other` 는 이유를 모르므로 말할 수 있는 것이 「무엇을 잡히는가」뿐이다.
func (f Facts) used() Facts {
	// 판단에 쓰인 값들은 카테고리와 무관하게 남는다.
	u := Facts{Kind: f.Kind, Category: f.Category, Level: f.Level, LostMate: f.LostMate}
	if !f.Known {
		return u
	}

	switch f.Category {
	case intervene.CategoryHangsPiece:
		// 그냥 잡히는 駒가 무엇이고 몇 장이 노리는가. 플레이 기록이 정확히 이것을
		// 요구했다 — 「어느 駒인지, 몇 개가 지키는지를 숫자로」(08-playtest.md §7).
		u.Known = true
		u.MovedPiece, u.Attackers, u.Defended = f.MovedPiece, f.Attackers, f.Defended

	case intervene.CategoryGreedyCapture:
		// 「駒は取れますが」의 그 駒를 이름으로 부른다.
		u.Known = true
		u.Captured = f.Captured

	case intervene.CategoryOther:
		// 이유를 모르는 자리다. 지어내지 않고, 대신 잡히는 것을 말한다.
		u.Known = true
		u.Threatened = f.Threatened

	default:
		// missed_mate · shallow_trap · unpromoted · idle_check · king_exposed.
		// 카테고리 자체가 이미 구체적이라 붙일 사실이 없다. Known 을 세우지 않는 것이
		// 곧 「이 문장은 국면을 안 짚는다」이고, 그래서 Tier 1로 간다.
	}
	return u
}
