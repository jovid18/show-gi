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
		// 이 카테고리가 말하지 않기로 한 사실들이다. 채워도 문장과 키에 안 새야 한다.
		MovedPiece: "歩",
		Captured:   "銀",
		Attackers:  2,
		Threatened: "飛",
	}
}

// 「逃す」가 문장에 있으면 안 된다. 詰み은 남아 있고 길어졌을 뿐이라, 놓쳤다고 말하면
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

// 착수 後의 手数는 문장에 하나도 안 나온다. 그 값은 solver 가 아니라 탐색이 준 것이라
// 증명이 아니고, 같은 국면에 14·16·「없음」이 나왔다(journal §76). Facts 에 그 칸이
// 아예 없는 것이 첫 번째 보증이고, 이 테스트가 두 번째다 — 문장이 다른 데서 숫자를 끌어
// 오는 날을 잡는다.
func TestSlowerMateSaysNoNumberForTheAfterSide(t *testing.T) {
	got := Render(slowerMateFacts(5))
	for _, r := range got {
		if r >= '0' && r <= '9' && r != '5' {
			t.Errorf("착수 전 手数(5) 말고 다른 숫자가 있다 (%q): %q", r, got)
		}
	}
	// 이 카테고리가 말하지 않기로 한 사실들도 실려 있는데(slowerMateFacts), 문장에는 안 나온다.
	for _, bad := range []string{"歩", "銀", "飛", "枚"} {
		if strings.Contains(got, bad) {
			t.Errorf("말하지 않기로 한 사실 %q 가 문장에 샜다: %q", bad, got)
		}
	}
}
