package explain

import (
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// otherWithBranches 는 갈래가 붙은 other 하나. 세 갈래가 전부 채워져 있다.
func otherWithBranches() Facts {
	return Facts{
		Kind:         intervene.KindBlunder,
		Category:     intervene.CategoryOther,
		Level:        intervene.Beginner,
		Known:        true,
		OpponentBest: "△3三角成",
		Branches: []Branch{
			{PlayerJa: "▲5八金", ReplyJa: "△2二馬", Cp: -350},
			{PlayerJa: "▲7八銀", ReplyJa: "△4四馬", Cp: -600},
			{PlayerJa: "▲同歩", ReplyJa: "△5五角", MateIn: -5},
		},
	}
}

// 받은 갈래를 하나도 빠뜨리지 않는다. 카테고리가 이유를 못 대는 자리라, 이 갈래들이
// 그 문구가 말할 수 있는 가장 구체적인 것이다.
func TestRenderCarriesEveryBranch(t *testing.T) {
	body := Render(otherWithBranches())

	for _, want := range []string{"△3三角成", "▲5八金", "△2二馬", "-350", "▲7八銀", "-600", "▲同歩", "5手で自分が詰まされる"} {
		if !strings.Contains(body, want) {
			t.Errorf("%q 가 빠졌다: %q", want, body)
		}
	}
	// 화면이 pre-line 으로 받는다. 한 줄로 이어지면 어느 수가 어느 결말에 걸리는지가
	// 안 읽히고, 이 문구는 그 대응이 전부다.
	if got := strings.Count(body, "\n"); got != 3 {
		t.Errorf("줄이 %d개다: %q", got+1, body)
	}
}

// 詰み을 cp로 말하지 않는다. 30000은 평가치가 아니라 환산값이다.
func TestBranchScoreJaNeverPrintsTheMateCp(t *testing.T) {
	if got := BranchScoreJa(Branch{MateIn: 3}); got != "3手で相手を詰ませられる" {
		t.Errorf("got %q", got)
	}
	if got := BranchScoreJa(Branch{Cp: 350}); got != "+350" {
		t.Errorf("got %q", got)
	}
	if got := BranchScoreJa(Branch{Cp: -350}); got != "-350" {
		t.Errorf("got %q", got)
	}
}

// 다른 카테고리는 갈래를 안 받는다. 받기 시작하면 「최선수를 보여주지 않는다」가
// 카테고리마다 갈린다.
func TestOnlyOtherKeepsTheBranches(t *testing.T) {
	f := otherWithBranches()
	f.Category = intervene.CategoryHangsPiece
	f.MovedPiece = "銀"

	if u := f.used(); len(u.Branches) != 0 || u.OpponentBest != "" {
		t.Errorf("hangs_piece 가 갈래를 들고 있다: %+v", u)
	}
}
