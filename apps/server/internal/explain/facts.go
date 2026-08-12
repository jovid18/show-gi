package explain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// Facts 는 문장으로 바꿀 **이미 정해진 사실들**이다. 전부 결정적으로 구해진 값이고,
// 판·SFEN·cp·평가치는 여기 없다 — **말할 수 있는 것**으로 좁혀진 입력이다.
//
// **칸과 Δ승률이 없는 것이 설계다**(04-llm.md §2) — 둘 다 판과 카드가 이미 말하고 있어서,
// 문장이 옮겨 적으면 두 벌이 되고 칸은 캐시 키를 81배로 늘린다.
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
	// MatePlies 는 이 수 뒤에 **상대가 내 玉을 詰ます 手数**다. 없으면 0.
	// 詰ます 쪽이 처음과 끝을 두므로 **늘 홀수**이고 상한은 solver 의 `DepthLimit`(11)이다.
	//
	// **증명된 것만 온다** — 탐색의 mate 점수는 증명이 아니라 틀린 手数를 말하고,
	// 틀린 手数를 적으면 초심자는 검증할 수단이 없다(06-status.md §40).
	MatePlies int

	// Threatened 는 반박 수순의 **첫 수로 상대가 딸 수 있는 내 駒**의 한자다. 없으면 빈 값.
	//
	// 카테고리가 이유를 못 대는 3분의 2에 「무엇을 잃는가」를 주는 자리다(06-status.md §25).
	// 「잃습니다」가 아니라 「取れます」로 말한다 — 실제로 그렇게 될지는 상대가 정하지만,
	// 그 수가 합법이라는 것은 룰 엔진이 확인한 사실이다.
	Threatened string

	// Tags 는 이 국면에서 감지된 囲い·전법·戦型의 태그 코드다(`tag.Detect`가 준다).
	// `kb_chunks` 를 찾는 키가 되고 캐시 키에도 들어간다.
	Tags []string
}

// promptVersion 은 **나가는 문장 자체가 달라질 때만** 올린다.
//
// 캐시가 있는 계층에서 프롬프트를 고치는 것은 곧 키를 바꾸는 일이라, 안 올리면 옛 문장이
// 영원히 돌아온다. **반대로 함부로 올리면** 004_explain_cache_tier1.sql 에 사전 생성해 둔
// 21행이 통째로 죽어, 공짜였던 Tier 1 설명이 조용히 유료 LLM 호출로 되돌아간다 —
// 카테고리가 하나 늘어난 정도로는 올리지 않는다. v2가 무엇이었는지는 06-status.md §38.
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

// keyMaterial 은 해시하기 **전**의, 사람이 읽을 수 있는 키다. 사전 생성이 이 값을 행마다
// 주석으로 달아서(004_explain_cache_tier1.sql), 키가 왜 갈렸는지를 사람이 대조할 수 있다.
func (f Facts) keyMaterial() string {
	u := f.used()
	// **`mp` 가 키에 있어야 한다.** 문장이 手数를 말하므로 빠지면 3手와 9手가 같은 키가 되고,
	// 캐시가 9手 국면에 「3手で」를 돌려준다 — 초심자는 검증할 수단이 없어 그대로 배운다.
	//
	// **없던 칸을 끝에 더하면 옛 키가 전부 죽는다.** 그래서 `lets_mate` 일 때만 붙인다 —
	// 나머지는 `used` 가 `MatePlies` 를 지워 키가 한 글자도 안 달라지고, 사전 생성해 둔
	// 행이 계속 맞는다(06-status.md §40).
	base := fmt.Sprintf("v%d|%s|%s|%d|mate=%t|known=%t|moved=%s|cap=%s|atk=%d|def=%t|thr=%s",
		promptVersion, u.Kind, u.Category, u.Level, u.LostMate,
		u.Known, u.MovedPiece, u.Captured, u.Attackers, u.Defended, u.Threatened)
	if u.MatePlies > 0 {
		base += fmt.Sprintf("|mp=%d", u.MatePlies)
	}
	if len(u.Tags) > 0 {
		sorted := make([]string, len(u.Tags))
		copy(sorted, u.Tags)
		sort.Strings(sorted)
		base += "|tags=" + strings.Join(sorted, ",")
	}
	return base
}

// Tier 는 이 사실들이 어느 층으로 가는지다. **문장에 국면 고유의 숫자나 駒가 들어가는가**로
// 갈린다. 안 들어가면 키가 **21가지**뿐이라 한 번 만든 문장이 계속 재사용되고(사전 생성해 둔
// 행이 그 21이다), 들어가면 키가 넓어지는 대신 문장이 그 국면을 짚는다(04-llm.md §2).
func (f Facts) Tier() int {
	u := f.used()
	if u.Known && (u.Attackers > 0 || u.Captured != "" || u.Threatened != "" || u.MatePlies > 0) {
		return 2
	}
	return 1
}

// used 는 **이 카테고리의 문장이 쓸 수 있는 사실만** 남기는 허용 목록이다.
//
// 키·프롬프트·결정적 문구가 전부 이 함수를 지난다 — 갈라두면 안 쓰는 사실로 캐시가 갈려
// 히트율이 조용히 떨어지거나, 프롬프트에만 있는 사실이 언젠가 문장으로 새어 나온다.
// **카테고리마다 말할 수 있는 것이 다른 것이 요점이다**(04-llm.md §2).
func (f Facts) used() Facts {
	// 판단에 쓰인 값들은 카테고리와 무관하게 남는다. Tags도 국면에 매인 값이라 함께 간다.
	u := Facts{Kind: f.Kind, Category: f.Category, Level: f.Level, LostMate: f.LostMate, Tags: f.Tags}
	if !f.Known {
		return u
	}

	switch f.Category {
	case intervene.CategoryHangsPiece:
		// 그냥 잡히는 駒가 무엇이고 몇 장이 노리는가. 플레이 기록이 정확히 이것을
		// 요구했다 — 「어느 駒인지, 몇 개가 지키는지를 숫자로」(08-playtest.md §7).
		u.Known = true
		u.MovedPiece, u.Attackers, u.Defended = f.MovedPiece, f.Attackers, f.Defended

	case intervene.CategoryLetsMate:
		// **手数만 말한다.** 「무엇이 몇 장에게 노려지는가」는 여기서 의미가 없다 — 玉이
		// 죽는 국면에서 駒의 매수를 말하면 읽는 사람이 駒를 지키러 간다. 그리고 이 카테고리는
		// 「몇 手 뒤에 죽는가」가 그대로 급함의 크기라, 그 하나가 문장을 완성한다.
		u.Known = true
		u.MatePlies = f.MatePlies

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
