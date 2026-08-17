package explain

import (
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// allCategories 는 카테고리 전부다. 새 카테고리가 붙으면 여기에 더한다 —
// 아래 테스트들이 「전부에 대해」를 단정하므로, 빠뜨리면 그 단정이 조용히 약해진다.
var allCategories = []intervene.Category{
	intervene.CategoryMissedMate,
	intervene.CategoryLetsMate,
	intervene.CategoryHangsPiece,
	intervene.CategoryShallowTrap,
	intervene.CategoryUnpromoted,
	intervene.CategoryGreedyCapture,
	intervene.CategoryIdleCheck,
	intervene.CategoryKingExposed,
	intervene.CategoryOther,
}

var allLevels = []intervene.Level{intervene.Beginner, intervene.Novice, intervene.Intermediate}

// 결정적 문구가 **사실을 담는다.** 이것이 안 되면 LLM이 없을 때 정보가 같이 사라지고,
// 「LLM은 문장의 품질을 올리는 층」이라는 이 패키지의 전제가 거짓이 된다.
func TestRenderCarriesTheFacts(t *testing.T) {
	tests := []struct {
		name  string
		facts Facts
		want  []string
	}{{
		// 08-playtest.md §7: 「어느 駒인지, 몇 개가 노리는지를 숫자로」.
		name: "タダ捨ては駒と枚数を言う",
		facts: Facts{
			Category: intervene.CategoryHangsPiece, Known: true,
			MovedPiece: "銀", Attackers: 2, Defended: false,
		},
		want: []string{"銀", "2枚", "取り返す駒がありません"},
	}, {
		name: "取った駒を名前で呼ぶ",
		facts: Facts{
			Category: intervene.CategoryGreedyCapture, Known: true, Captured: "飛",
		},
		want: []string{"飛は取れますが"},
	}, {
		// journal §25: 이유를 못 대는 3분의 2에 「무엇을 잡히는가」를 준다.
		name: "理由がわからないときは失うものを言う",
		facts: Facts{
			Category: intervene.CategoryOther, Known: true, Threatened: "桂",
		},
		want: []string{"桂を取れます"},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Render(tt.facts)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("%q 가 없다: %q", w, got)
				}
			}
		})
	}
}

// 사실이 없으면 **지어내지 않는다.** 카테고리별 기본 문구로 떨어지고, 그 문구는 숫자를
// 말하지 않는다 — 「0枚」이나 「그 駒」처럼 비어 있는 값이 문장에 새면 그것이 곧 거짓이다.
func TestRenderInventsNothingWithoutFacts(t *testing.T) {
	for _, c := range allCategories {
		got := Render(Facts{Category: c})
		if got == "" {
			t.Fatalf("%s: 문장이 비었다", c)
		}
		for _, bad := range []string{"0枚", "%!", "<nil>"} {
			if strings.Contains(got, bad) {
				t.Errorf("%s: 빈 값이 문장에 샜다 (%q): %q", c, bad, got)
			}
		}
	}
}

// **결정적 문구는 수를 짚지 않는다**(01-core.md §1). 짚어주는 순간 플레이어가 생각을 멈춘다.
//
// 칸은 「숫자 + 段」의 모양이다(`8四`). 段의 한자만 찾으면 「一手」의 一에 걸리므로
// 붙어 있는 것만 본다 — 「2枚」는 段이 아니라서 안 걸린다.
//
// Facts 에 칸이 아예 없으므로 지금은 나올 수 없고, 이 테스트는 **누가 칸을 Facts 에
// 더하는 날** 그것을 잡는다.
func TestRenderNamesNoSquare(t *testing.T) {
	square := regexp.MustCompile(`[0-9０-９][一二三四五六七八九]`)
	full := Facts{
		Known: true, MovedPiece: "銀", Captured: "飛", Attackers: 2, Threatened: "桂",
	}
	for _, c := range allCategories {
		f := full
		f.Category = c
		if got := Render(f); square.MatchString(got) {
			t.Errorf("%s: 문장이 칸을 부른다 (%q): %q", c, square.FindString(got), got)
		}
	}
}

// **한글이 한 글자도 없어야 한다.**
//
// 사람 눈으로 지키면 결국 샌다(`shogi` 의 사유 문구에 같은 테스트가 있다). 문구가 코드에
// 박혀 있으니 기계가 전수로 볼 수 있고, 그러면 새는 길이 남지 않는다.
func TestNoKoreanReachesTheUser(t *testing.T) {
	texts := map[string]string{"unknownMessage": unknownMessage}
	for c, m := range baseMessages {
		texts["baseMessages["+string(c)+"]"] = m
	}
	for c, m := range categoryNames {
		texts["categoryNames["+string(c)+"]"] = m
	}
	full := Facts{Known: true, MovedPiece: "銀", Captured: "飛", Attackers: 2, Threatened: "桂"}
	for _, c := range allCategories {
		for _, l := range allLevels {
			f := full
			f.Category, f.Level, f.Kind = c, l, intervene.KindBlunder
			texts["Render/"+string(c)] = Render(f)
		}
	}

	for where, s := range texts {
		for _, r := range s {
			if unicode.Is(unicode.Hangul, r) {
				t.Errorf("%s 에 한글이 있다 (%q): %q", where, r, s)
				break
			}
		}
	}
}
