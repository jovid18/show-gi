package explain

import (
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

func letsMateFacts(plies int) Facts {
	return Facts{
		Kind:      intervene.KindBlunder,
		Category:  intervene.CategoryLetsMate,
		Level:     intervene.Beginner,
		Known:     true,
		MatePlies: plies,
		// 아래는 이 카테고리가 **말하지 않기로 한** 사실들이다. 채워 넣어도 문장과 키에
		// 안 새는지를 같이 본다.
		MovedPiece: "銀",
		Captured:   "歩",
		Attackers:  2,
		Threatened: "飛",
	}
}

// **手数가 키에 있어야 한다.** 문장이 手数를 말하므로, 빠지면 캐시가 9手 국면에
// 「3手で」를 돌려준다 — 초심자는 검증할 수단이 없어 그대로 배운다.
func TestMatePliesSplitsTheCacheKey(t *testing.T) {
	three, nine := letsMateFacts(3), letsMateFacts(9)
	if three.Key() == nine.Key() {
		t.Error("3手와 9手가 같은 키다 — 캐시가 틀린 手数의 문장을 돌려준다")
	}
	if !strings.Contains(three.keyMaterial(), "mp=3") {
		t.Errorf("키에 手数가 없다: %s", three.keyMaterial())
	}
	// 같은 手数면 같은 키여야 캐시가 듣는다.
	if three.Key() != letsMateFacts(3).Key() {
		t.Error("같은 사실이 다른 키를 냈다")
	}
}

// **다른 카테고리의 키는 한 글자도 안 달라져야 한다.** 004_explain_cache_tier1.sql 에
// 사전 생성해 둔 행이 그 키로 들어 있고, 끝에 칸을 더하면 전부 죽는다(§38).
func TestMatePliesDoesNotDisturbOtherKeys(t *testing.T) {
	for _, c := range []intervene.Category{
		intervene.CategoryMissedMate, intervene.CategoryHangsPiece, intervene.CategoryShallowTrap,
		intervene.CategoryUnpromoted, intervene.CategoryGreedyCapture, intervene.CategoryIdleCheck,
		intervene.CategoryKingExposed, intervene.CategoryOther,
	} {
		f := Facts{Kind: intervene.KindBlunder, Category: c, Level: intervene.Novice, Known: true}
		// 엉뚱한 카테고리에 手数가 실려 와도 `used` 가 지우므로 키가 안 갈린다.
		withMate := f
		withMate.MatePlies = 5
		if f.Key() != withMate.Key() {
			t.Errorf("%s: 手数가 키를 갈랐다 — 사전 생성된 행이 죽는다", c)
		}
		if strings.Contains(f.keyMaterial(), "mp=") {
			t.Errorf("%s: 키에 mp 칸이 붙었다: %s", c, f.keyMaterial())
		}
	}
}

func TestLetsMateSaysThePlyCount(t *testing.T) {
	got := Render(letsMateFacts(3))
	if !strings.Contains(got, "3手") {
		t.Errorf("手数가 문장에 없다: %q", got)
	}
	// **駒 이야기를 하지 않는다.** 玉이 죽는 국면에서 駒를 말하면 초심자는 駒를 지키고
	// 그 다음 수에 詰まされる.
	for _, bad := range []string{"銀", "歩", "飛", "2枚"} {
		if strings.Contains(got, bad) {
			t.Errorf("駒 이야기가 섞였다 (%s): %q", bad, got)
		}
	}
	// 최선수를 짚지 않는다(01-core.md §1).
	if strings.Contains(got, "べき") || strings.Contains(got, "正解") {
		t.Errorf("최선수를 짚었다: %q", got)
	}
}

// 手数가 없으면 카테고리만으로 나가는 문구로 떨어진다. 지어내지 않는다.
func TestLetsMateWithoutPlyCountFallsBack(t *testing.T) {
	f := letsMateFacts(0)
	got := Render(f)
	if strings.Contains(got, "0手") {
		t.Errorf("0手 를 문장에 적었다: %q", got)
	}
	if !strings.Contains(got, "詰まされ") {
		t.Errorf("詰まされる 는 말해야 한다: %q", got)
	}
}

func TestLetsMateIsTier2(t *testing.T) {
	if tier := letsMateFacts(3).Tier(); tier != 2 {
		t.Errorf("Tier = %d, want 2 — 手数는 국면 고유의 숫자다", tier)
	}
}

func TestLetsMateHasAShortName(t *testing.T) {
	name := CategoryJa(intervene.CategoryLetsMate)
	if name == "" || name == categoryNames[intervene.CategoryOther] {
		t.Errorf("짧은 이름이 미분류와 같거나 비었다: %q", name)
	}
	for _, r := range name {
		if r >= 0xAC00 && r <= 0xD7A3 {
			t.Errorf("한글이 섞였다: %q", name)
		}
	}
}

// BaseMessage 는 리뷰 화면이 쓴다 — 기록에는 카테고리만 남으므로 手数 없이 불린다.
func TestLetsMateBaseMessageStandsAlone(t *testing.T) {
	m := BaseMessage(intervene.CategoryLetsMate)
	if m == "" || m == unknownMessage {
		t.Errorf("결정적 문구가 없다: %q", m)
	}
	if strings.Contains(m, "%d") || strings.Contains(m, "手で") {
		t.Errorf("手数가 없는데 手数 자리를 남겼다: %q", m)
	}
}
