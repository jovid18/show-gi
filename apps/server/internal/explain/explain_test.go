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
		// 06-status.md §25: 이유를 못 대는 3분의 2에 「무엇을 잡히는가」를 준다.
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

// **한글이 한 글자도 없어야 한다.** 화면·프롬프트 양쪽이다.
//
// 사람 눈으로 지키면 결국 샌다(`shogi` 의 사유 문구에 같은 테스트가 있다). 프롬프트 쪽이
// 특히 중요하다 — 프롬프트에 한글이 섞이면 **출력이 한글로 새고** temperature=0 캐시 키까지
// 갈라진다(docs/04-llm.md §1).
func TestNoKoreanReachesTheUser(t *testing.T) {
	texts := map[string]string{"systemPrompt": systemPrompt, "unknownMessage": unknownMessage}
	for c, m := range baseMessages {
		texts["baseMessages["+string(c)+"]"] = m
	}
	for c, m := range categoryJa {
		texts["categoryJa["+string(c)+"]"] = m
	}
	for l, m := range levelJa {
		texts[strings.Join([]string{"levelJa", string(rune('0' + l))}, "")] = m
	}
	full := Facts{Known: true, MovedPiece: "銀", Captured: "飛", Attackers: 2, Threatened: "桂"}
	for _, c := range allCategories {
		for _, l := range allLevels {
			f := full
			f.Category, f.Level, f.Kind = c, l, intervene.KindBlunder
			texts["Render/"+string(c)] = Render(f)
			texts["userPrompt/"+string(c)] = userPrompt(f)
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

// 프롬프트는 **말해도 되는 사실만** 싣는다.
//
// 여기 새면 문장으로도 샌다. `idle_check` 는 카테고리 자체가 이미 구체적이라 駒도 매수도
// 붙일 자리가 없는데, 프롬프트에 「動かした駒: 銀」이 들어가면 LLM은 그것을 문장에 쓴다 —
// 그리고 그 문장은 캐시 키에 그 사실이 없으므로 **다른 駒의 실수에도 그대로 나간다.**
func TestPromptCarriesOnlyWhatTheSentenceMayUse(t *testing.T) {
	full := Facts{
		Kind: intervene.KindBlunder, Known: true,
		MovedPiece: "銀", Captured: "飛", Attackers: 2, Threatened: "桂",
	}

	f := full
	f.Category = intervene.CategoryIdleCheck
	got := userPrompt(f)
	for _, bad := range []string{"銀", "飛", "桂", "2枚"} {
		if strings.Contains(got, bad) {
			t.Errorf("idle_check 프롬프트에 %q 가 들어갔다:\n%s", bad, got)
		}
	}

	f = full
	f.Category = intervene.CategoryHangsPiece
	got = userPrompt(f)
	if !strings.Contains(got, "銀") || !strings.Contains(got, "2枚") {
		t.Errorf("hangs_piece 프롬프트에 駒와 매수가 없다:\n%s", got)
	}
	// 그 카테고리가 쓰지 않는 사실은 여기서도 빠진다.
	if strings.Contains(got, "桂") {
		t.Errorf("hangs_piece 프롬프트에 threatened 가 샜다:\n%s", got)
	}
}

// **모른다고 말해야 지어내지 않는다.** `other` 는 이유를 특정하지 못한 카테고리이고,
// 프롬프트가 그것을 적지 않으면 LLM이 그럴듯한 이유를 만든다 — 개입의 3분의 2가 여기다.
func TestPromptAdmitsWhenTheReasonIsUnknown(t *testing.T) {
	got := userPrompt(Facts{Category: intervene.CategoryOther, Kind: intervene.KindBlunder})
	if !strings.Contains(got, "特定できていない") {
		t.Errorf("이유를 모른다는 말이 프롬프트에 없다:\n%s", got)
	}
}

// 키는 **문장이 쓰는 사실**로만 갈린다.
//
// 안 쓰는 사실로 갈리면 캐시가 조용히 안 듣는다(히트율이 떨어지는 것은 에러가 아니라 비용이다).
// 반대로 쓰는 사실로 안 갈리면 **다른 국면에 남의 문장이 나간다** — 그쪽이 훨씬 나쁘다.
func TestKeySplitsOnWhatTheSentenceSays(t *testing.T) {
	idle := func(piece string) Facts {
		return Facts{Category: intervene.CategoryIdleCheck, Known: true, MovedPiece: piece}
	}
	if idle("銀").Key() != idle("桂").Key() {
		t.Error("idle_check 이 문장에 안 쓰는 駒로 키가 갈렸다 — 캐시가 안 듣는다")
	}

	hangs := func(n int) Facts {
		return Facts{Category: intervene.CategoryHangsPiece, Known: true, MovedPiece: "銀", Attackers: n}
	}
	if hangs(1).Key() == hangs(2).Key() {
		t.Error("매수가 달라도 같은 키다 — 「2枚」라고 적힌 문장이 1枚 국면에 나간다")
	}

	base := Facts{Category: intervene.CategoryOther, Level: intervene.Beginner}
	other := base
	other.Level = intervene.Intermediate
	if base.Key() == other.Key() {
		t.Error("레벨이 달라도 같은 키다 — 어휘를 사람에 맞추는 층이 캐시에서 무너진다")
	}

	blunder, tesuji := base, base
	blunder.Kind, tesuji.Kind = intervene.KindBlunder, intervene.Kind("tesuji")
	if blunder.Key() == tesuji.Key() {
		t.Error("제지형과 제안형이 같은 키다 — 「なぜ悪いか」와 「ここに何かある」가 섞인다")
	}
}

// Tier는 **국면 고유의 값이 문장에 들어가는가**로 갈린다.
func TestTierRoutesOnPositionFacts(t *testing.T) {
	tests := []struct {
		name  string
		facts Facts
		want  int
	}{
		{"판을 못 읽으면 1", Facts{Category: intervene.CategoryOther}, 1},
		{"카테고리로 완결되면 1", Facts{Category: intervene.CategoryUnpromoted, Known: true, MovedPiece: "銀"}, 1},
		{"매수가 들어가면 2", Facts{Category: intervene.CategoryHangsPiece, Known: true, MovedPiece: "銀", Attackers: 1}, 2},
		{"잡은 駒가 들어가면 2", Facts{Category: intervene.CategoryGreedyCapture, Known: true, Captured: "飛"}, 2},
		{"잡히는 駒가 들어가면 2", Facts{Category: intervene.CategoryOther, Known: true, Threatened: "桂"}, 2},
		{"사실이 없는 other 는 1", Facts{Category: intervene.CategoryOther, Known: true}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.facts.Tier(); got != tt.want {
				t.Errorf("Tier=%d, want %d", got, tt.want)
			}
		})
	}
}

// **LLM 출력을 믿지 않는다.** 엔진이 돌려준 수를 룰 엔진으로 검증하는 것과 같은 자리다.
func TestCleanRejectsUnusableSentences(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"보통 문장", "その銀は取られます。", "その銀は取られます。", true},
		{"앞뒤 인용부호를 떼어낸다", "「その銀は取られます。」", "その銀は取られます。", true},
		{"줄바꿈은 한 줄로", "その銀は\n取られます。", "その銀は 取られます。", true},
		{"빈 문장은 버린다", "   ", "", false},
		{"한글이 섞이면 버린다", "その銀은 取られます。", "", false},
		{"너무 길면 자르지 않고 버린다", strings.Repeat("長", MaxRunes+1), "", false},
		// **칸과 수는 우리가 준 적이 없다.** 나타났다면 모델이 지어낸 것이다.
		{"칸을 지어내면 버린다", "8四の銀が取られます。", "", false},
		{"수를 지어내면 버린다", "▲7六歩と指すべきでした。", "", false},
		{"매수의 숫자는 칸이 아니다", "相手の駒が2枚利いています。", "相手の駒が2枚利いています。", true},
		{"一手는 段이 아니다", "その一手で合っています。", "その一手で合っています。", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := clean(tt.in)
			if ok != tt.ok {
				t.Fatalf("ok=%v, want %v (got %q)", ok, tt.ok, got)
			}
			if ok && got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// Explainer 가 아예 없어도 **문장은 나간다.** 카드가 비는 경로를 만들지 않는다.
func TestTemplateOnlyStillExplains(t *testing.T) {
	e := TemplateOnly()
	got := e.Explain(t.Context(), Facts{
		Category: intervene.CategoryHangsPiece, Known: true, MovedPiece: "銀", Attackers: 2,
	})
	if got.Tier != TierTemplate {
		t.Errorf("Tier=%d, want %d", got.Tier, TierTemplate)
	}
	if got.CostYen != 0 {
		t.Errorf("CostYen=%v, want 0", got.CostYen)
	}
	if !strings.Contains(got.Body, "2枚") {
		t.Errorf("사실이 빠진 문장이다: %q", got.Body)
	}
}
