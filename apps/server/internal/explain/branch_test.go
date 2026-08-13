package explain

import (
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// otherWithBranches 는 갈래가 붙은 `other` 하나. 세 갈래가 전부 채워져 있다.
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

// **우리가 준 수는 써도 되고, 그 밖의 표기는 하나라도 있으면 버린다.**
// 「指し手は書かない」를 「우리가 준 수만」으로 좁힌 것이지 푼 것이 아니다.
func TestCleanBranchesAllowsOnlyTheMovesWeGave(t *testing.T) {
	allowed := otherWithBranches().Notations()

	tests := []struct {
		name string
		in   string
		ok   bool
	}{
		{"준 수는 그대로 써도 된다", "△3三角成が厳しいです。\n▲5八金なら△2二馬で-350です。", true},
		{"안 준 수를 쓰면 버린다", "△3三角成が厳しいです。\n▲7六歩と指すべきでした。", false},
		{"안 준 칸을 쓰면 버린다", "△3三角成のあと、8四の銀が取られます。", false},
		{"수를 하나도 안 써도 된다", "この手のあとは形勢が大きく傾きます。", true},
		{"한글이 섞이면 버린다", "△3三角成が厳しいです。 다음 수를 보세요。", false},
		{"너무 길면 버린다", strings.Repeat("長", BranchMaxRunes+1), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := CleanBranches(tt.in, allowed)
			if ok != tt.ok {
				t.Fatalf("ok=%v, want %v (got %q)", ok, tt.ok, got)
			}
		})
	}
}

// **줄을 살린다.** 갈래 셋이 한 줄로 이어지면 어느 수가 어느 결말에 걸리는지가 안 읽히고,
// 이 문구는 그 대응이 전부다.
func TestCleanBranchesKeepsTheLines(t *testing.T) {
	got, ok := CleanBranches("一行目です。\n\n  二行目です。  \n三行目です。", nil)
	if !ok {
		t.Fatal("버려졌다")
	}
	if want := "一行目です。\n二行目です。\n三行目です。"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// 갈래가 없는 개입 문구는 **한 줄이고 수를 못 쓴다.** 규칙이 한쪽에서 느슨해지지 않았는지 본다.
func TestCleanStillRejectsMovesWithoutBranches(t *testing.T) {
	if _, ok := Clean("▲7六歩と指すべきでした。", MaxRunes); ok {
		t.Error("갈래 없는 문구가 수를 적었는데 통과했다")
	}
}

// LLM이 죽어도 **같은 사실이 같은 순서로** 나간다. 잃는 것은 문장의 결이지 사실이 아니다.
func TestRenderCarriesEveryBranch(t *testing.T) {
	body := Render(otherWithBranches())

	for _, want := range []string{"△3三角成", "▲5八金", "△2二馬", "-350", "▲7八銀", "-600", "▲同歩", "5手で自分が詰まされる"} {
		if !strings.Contains(body, want) {
			t.Errorf("%q 가 빠졌다: %q", want, body)
		}
	}
	// 결정적 문구도 화면이 `pre-line` 으로 받는 모양이어야 한다(CleanBranches 와 같은 줄 수).
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

// **다른 국면이 같은 키를 가지면 안 된다.** 그러면 다른 판의 수순이 문장으로 돌아온다 —
// 캐시가 안 맞는 것보다 나쁜 유일한 결과다.
func TestKeySplitsOnTheBranches(t *testing.T) {
	a := otherWithBranches()
	b := otherWithBranches()
	b.Branches[0].Cp = -360

	if a.Key() == b.Key() {
		t.Error("갈래가 다른데 키가 같다")
	}
	if a.Tier() != 2 {
		t.Errorf("Tier=%d, want 2", a.Tier())
	}
}

// **갈래가 없는 사실의 키는 한 글자도 안 달라져야 한다.** 004_explain_cache_tier1.sql 에
// 사전 생성해 둔 21행이 여기에 걸려 있다.
func TestKeyIsUntouchedWithoutBranches(t *testing.T) {
	f := Facts{Kind: intervene.KindBlunder, Category: intervene.CategoryOther, Level: intervene.Beginner, Known: true, Threatened: "角"}
	want := "v2|blunder|other|0|mate=false|known=true|moved=|cap=|atk=0|def=false|thr=角"

	if got := f.keyMaterial(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// 다른 카테고리는 갈래를 **안 받는다.** 받기 시작하면 「최선수를 보여주지 않는다」가
// 카테고리마다 갈린다.
func TestOnlyOtherKeepsTheBranches(t *testing.T) {
	f := otherWithBranches()
	f.Category = intervene.CategoryHangsPiece
	f.MovedPiece = "銀"

	if u := f.used(); len(u.Branches) != 0 || u.OpponentBest != "" {
		t.Errorf("hangs_piece 가 갈래를 들고 있다: %+v", u)
	}
}
