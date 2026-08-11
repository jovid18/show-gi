package explain

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// **진짜 OrcaRouter에 붙는 유일한 테스트다.** `ORCA_API_KEY` 가 없으면 건너뛴다.
//
// 가짜 라우터(orca_test.go)는 우리가 적은 것을 그대로 돌려주므로 **모델이 지시를 지키는지에
// 대한 증거가 못 된다.** `usi` 의 `TestRealEngine` 과 정확히 같은 자리이고, 거기서 실제로
// `PvInterval` 문제를 잡았다(06-status.md §10). **프롬프트를 고치면 여기가 첫 관문이다.**
//
//	set -a && . ../../../.env && set +a
//	go test ./internal/explain/ -run RealRouter -v
//
// 돈이 든다. 카테고리 여덟에 한 번씩이라 한 번 돌리는 값이 개입 여덟 번과 같다.
func TestRealRouter(t *testing.T) {
	key := os.Getenv("ORCA_API_KEY")
	if key == "" {
		t.Skip("ORCA_API_KEY 미설정 — 실라우터 테스트 건너뜀")
	}

	client := NewClient(key, os.Getenv("ORCA_BASE_URL"), os.Getenv("ORCA_MODEL_SMALL"), os.Getenv("ORCA_MODEL_LARGE"), DefaultUSDJPY)
	if client == nil {
		t.Fatal("키가 있는데 클라이언트가 nil이다")
	}
	// 캐시 없이 라우터만 본다. 시한은 실측이 목적이므로 넉넉히 준다 —
	// **Deadline 이 맞는지가 이 테스트가 답할 질문**이라 그 값으로 자르면 안 된다.
	l := &Layered{client: client, deadline: 30 * time.Second}

	square := regexp.MustCompile(`[0-9０-９][一二三四五六七八九]`)

	for _, tc := range realRouterCases() {
		t.Run(string(tc.facts.Category), func(t *testing.T) {
			start := time.Now()
			got := l.Explain(t.Context(), tc.facts)
			elapsed := time.Since(start)

			t.Logf("tier=%d %v model=%q cost=%.6f엔 cached=%v\n  %s",
				got.Tier, elapsed.Round(time.Millisecond), got.Model, got.CostYen, got.RouterCached, got.Body)

			if got.Body == "" {
				t.Fatal("문장이 비었다")
			}
			// **템플릿으로 떨어졌으면 실패다.** 이 테스트의 목적이 라우터가 실제로 쓸 만한
			// 문장을 주는지 보는 것이라, 조용히 폴백되면 아무것도 확인하지 못한다.
			if got.Tier == TierTemplate {
				t.Fatalf("라우터가 못 쓸 답을 줬거나 실패했다 — 로그를 볼 것. 나간 문장: %q", got.Body)
			}
			if got.Tier != tc.wantTier {
				t.Errorf("tier=%d, want %d", got.Tier, tc.wantTier)
			}

			// clean 이 이미 걸러낸 것들이지만, **여기서 다시 본다.** 저쪽이 통과시키면
			// 화면까지 그대로 가므로, 규칙이 두 곳에서 지켜지는 편이 싸다.
			for _, r := range got.Body {
				if unicode.Is(unicode.Hangul, r) {
					t.Errorf("한글이 섞였다: %q", got.Body)
					break
				}
			}
			if square.MatchString(got.Body) {
				t.Errorf("칸을 지어냈다 (%q): %q", square.FindString(got.Body), got.Body)
			}
			if strings.ContainsAny(got.Body, "▲△") {
				t.Errorf("수를 지어냈다: %q", got.Body)
			}
			// **앱 동작을 문장에 옮기지 않는다.** 되물러졌다는 것은 사실로 주지만(시점을
			// 정하는 데 필요하다) 화면이 이미 그것을 보여주고 있어서, 문장에 다시 적으면
			// 60자의 절반이 사라진다 — 실제로 8개 중 5개가 그랬다(06-status.md §38).
			if strings.Contains(got.Body, "アプリ") {
				t.Errorf("앱 동작을 문장에 옮겼다 — 그 자리는 왜 나쁜지가 들어갈 자리다: %q", got.Body)
			}
			if n := len([]rune(got.Body)); n > MaxRunes {
				t.Errorf("%d자 — MaxRunes(%d) 초과", n, MaxRunes)
			}

			// **지연은 단정하지 않고 기록한다.** Deadline 은 재는 값이 아니라 UX 예산이고,
			// 넘는 국면은 결정적 문구로 떨어지는 것이 설계다(그래서 카드는 늘 뜬다).
			// 여기서 잡을 것은 「라우터가 쓸 수 없을 만큼 느려졌다」쪽뿐이다.
			if elapsed > 2*Deadline {
				t.Errorf("%v 걸렸다 — Deadline(%v)의 두 배를 넘었다. 이 모델로는 대국에서 거의 항상 템플릿이 나간다",
					elapsed.Round(time.Millisecond), Deadline)
			}
			if elapsed > Deadline {
				t.Logf("  ⚠ Deadline(%v) 초과 — 실제 대국에서는 이 문장이 안 나가고 템플릿이 나간다", Deadline)
			}

			// 우리가 준 사실이 문장에 남아 있는가. 사실을 버리고 일반론만 쓰면
			// **템플릿보다 나쁘다** — 그 사실이 이 카드의 전부다.
			for _, want := range tc.mustMention {
				if !strings.Contains(got.Body, want) {
					t.Errorf("우리가 준 사실 %q 가 문장에 없다: %q", want, got.Body)
				}
			}
			for _, bad := range tc.mustNotMention {
				if strings.Contains(got.Body, bad) {
					t.Errorf("틀린 표현 %q 가 돌아왔다 — 프롬프트가 그 사실을 덜 준 것이다: %q", bad, got.Body)
				}
			}
		})
	}
}

type realRouterCase struct {
	facts       Facts
	wantTier    int
	mustMention []string
	// mustNotMention 은 **실모델이 실제로 틀리게 쓴 표현**이다. 프롬프트를 고쳐 막았고,
	// 여기 남겨 두어 되돌아오면 잡는다 — 사람 눈으로는 두 번 넘긴 종류의 오류다.
	mustNotMention []string
}

// realRouterCases 는 카테고리 전부를 한 번씩 지난다. 사실이 붙는 셋은 그 사실까지 본다.
func realRouterCases() []realRouterCase {
	base := func(c intervene.Category) Facts {
		return Facts{Kind: intervene.KindBlunder, Category: c, Level: intervene.Beginner}
	}

	hangs := base(intervene.CategoryHangsPiece)
	hangs.Known, hangs.MovedPiece, hangs.Attackers = true, "銀", 2

	greedy := base(intervene.CategoryGreedyCapture)
	greedy.Known, greedy.Captured = true, "飛"

	other := base(intervene.CategoryOther)
	other.Known, other.Threatened = true, "桂"

	mate := base(intervene.CategoryMissedMate)
	mate.LostMate = true

	return []realRouterCase{
		{facts: hangs, wantTier: 2, mustMention: []string{"銀", "2"}},
		{facts: greedy, wantTier: 2, mustMention: []string{"飛"}},
		{facts: other, wantTier: 2, mustMention: []string{"桂"}},
		// 詰み을 가진 쪽은 **나**다. 「相手に詰みがある」로 쓰면 방향이 반대인 설명이 된다.
		{facts: mate, wantTier: 1, mustMention: []string{"詰"}, mustNotMention: []string{"相手に詰み", "相手の詰み"}},
		// 「敵陣から**出る**手も成れる」가 이 카테고리가 생긴 이유다. 「入ったとき」만 말하면
		// 플레이어가 놓친 오해를 그대로 남긴다.
		{facts: base(intervene.CategoryUnpromoted), wantTier: 1, mustMention: []string{"出"}},
		{facts: base(intervene.CategoryIdleCheck), wantTier: 1},
		{facts: base(intervene.CategoryShallowTrap), wantTier: 1},
		{facts: base(intervene.CategoryKingExposed), wantTier: 1},
	}
}
