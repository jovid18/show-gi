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
