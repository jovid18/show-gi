package explain

import (
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

func slowerMateFacts(before int) Facts {
	return Facts{
		Kind:       intervene.KindBlunder,
		Category:   intervene.CategorySlowerMate,
		Level:      intervene.Beginner,
		LostMate:   true,
		Known:      true,
		MateBefore: before,
		// 이 카테고리가 **말하지 않기로 한** 사실들이다. 채워도 문장과 키에 안 새야 한다.
		MovedPiece: "歩",
		Captured:   "銀",
		Attackers:  2,
		Threatened: "飛",
	}
}

// **「逃す」가 문장에 있으면 안 된다.** 詰み은 남아 있고 길어졌을 뿐이라, 놓쳤다고 말하면
// 이기고 있는 사람에게 거짓을 가르친다 — 실제로 이긴 판에서 그 문장이 나갔다(§76).
func TestSlowerMateNeverSaysTheMateWasLost(t *testing.T) {
	got := Render(slowerMateFacts(5))
	if !strings.Contains(got, "5手") {
		t.Errorf("착수 전 手数를 안 말한다: %q", got)
	}
	if !strings.Contains(got, "遠回り") {
		t.Errorf("무엇이 나빴는지를 안 말한다: %q", got)
	}
	if strings.Contains(got, "逃") {
		t.Errorf("詰み이 남아 있는데 놓쳤다고 말한다: %q", got)
	}
	// 사실이 없을 때 나가는 문구도 같은 규칙을 지켜야 한다 — 되짚기는 카테고리만 들고
	// 문장을 다시 만든다(BaseMessage).
	base := BaseMessage(intervene.CategorySlowerMate)
	if strings.Contains(base, "逃") || !strings.Contains(base, "遠回り") {
		t.Errorf("카테고리만으로 낸 문구가 어긋난다: %q", base)
	}
}

// **착수 後의 手数는 아예 들고 다니지 않는다.** 그 값은 solver 가 아니라 탐색이 준 것이라
// 증명이 아니고, 같은 국면에 14·16·「없음」이 나왔다(06-status.md §76).
func TestSlowerMateSaysNoNumberForTheAfterSide(t *testing.T) {
	f := slowerMateFacts(5)
	if strings.Contains(userPrompt(f, nil), "この手のあとの手数: 8") {
		t.Error("프롬프트가 착수 후 手数를 숫자로 준다 — 문장으로 새어 나간다")
	}
	if !strings.Contains(userPrompt(f, nil), "数字を書かない") {
		t.Errorf("모델에게 그 숫자를 쓰지 말라고 안 한다:\n%s", userPrompt(f, nil))
	}
}

// `lets_mate` 의 `mp` 와 같은 이유다 — 문장이 手数를 말하므로 키가 갈려야 한다.
func TestMateBeforeSplitsTheCacheKey(t *testing.T) {
	three, five := slowerMateFacts(3), slowerMateFacts(5)
	if three.Key() == five.Key() {
		t.Error("3手와 5手가 같은 키다 — 캐시가 틀린 手数의 문장을 돌려준다")
	}
	if !strings.Contains(five.keyMaterial(), "mb=5") {
		t.Errorf("키에 手数가 없다: %s", five.keyMaterial())
	}
	if three.Key() != slowerMateFacts(3).Key() {
		t.Error("같은 사실이 다른 키를 냈다")
	}
	// 국면 고유의 숫자를 말하므로 Tier 2다 — 사전 생성해 둔 21행에 낄 문장이 아니다.
	if got := five.Tier(); got != 2 {
		t.Errorf("Tier %d 다 — 手数를 말하는 문장은 미리 만들어 둘 수 없다", got)
	}
}

// **없던 칸을 더하면 옛 키가 죽는다.** 004_explain_cache_tier1.sql 의 행이 그 키로 들어
// 있어서, `mb` 는 이 카테고리에서만 붙어야 한다(§38).
func TestMateBeforeDoesNotDisturbOtherKeys(t *testing.T) {
	for _, c := range []intervene.Category{
		intervene.CategoryMissedMate, intervene.CategoryLetsMate, intervene.CategoryHangsPiece,
		intervene.CategoryShallowTrap, intervene.CategoryUnpromoted, intervene.CategoryGreedyCapture,
		intervene.CategoryIdleCheck, intervene.CategoryKingExposed, intervene.CategoryOther,
	} {
		f := Facts{Kind: intervene.KindBlunder, Category: c, Level: intervene.Novice, Known: true}
		with := f
		with.MateBefore = 5
		if f.Key() != with.Key() {
			t.Errorf("%s: MateBefore 가 키를 흔들었다 — 사전 생성해 둔 행이 죽는다", c)
		}
	}
}
