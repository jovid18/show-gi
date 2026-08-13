package explain

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// Tier 1 사전 생성 — **키가 유한한 층을 미리 만들어 캐시에 넣는다.**
//
// 국면 고유의 사실이 문장에 없는 층이라(Facts.Tier) 키가 손으로 셀 수 있을 만큼 적고,
// 그러면 런타임에 만들 이유가 없다 — 만들어 두면 그 층의 개입은 LLM 왕복이 0이 된다.
// 「Tier 2가 대부분이라 이득이 적다」고 봤다가 **1.7~4.7초라는 실측이 그 판단을 뒤집었다**
// (06-status.md §28).
//
// 생성기가 **테스트인 것이 의도다.** 새 커맨드를 만들면 배선이 `cmd/api` 로 가고, 거기는
// 로직을 두지 않는 자리다. `SHOWGI_MEASURE`·`SHOWGI_USI_CMD` 와 같은 방식으로 환경변수에
// 잠가 둔다 — 없으면 조용히 건너뛴다(apps/server/README.md 의 표).
//
//	set -a && . ../../.env && set +a
//	SHOWGI_GENERATE_TIER1=1 go test ./internal/explain/ -run GenerateTier1 -v
//
// 나머지 둘은 게이트가 없다. **만들어 둔 파일이 코드와 갈리는 것을 기계가 잡는다** —
// `promptVersion` 이 올라가면 키가 전부 바뀌고, 그때 이 파일은 아무도 찾지 않는 행이
// 된다. 조용히 그렇게 되면 「사전 생성했다」는 말만 남고 히트는 0이다.

// tier1MigrationPath 는 생성기가 떨어뜨리는 자리다. 실행은 사람이 DB 클라이언트로 한다
// (deploy/README.md §4) — 배포는 DDL도 시드도 돌리지 않는다.
const tier1MigrationPath = "../store/migrations/004_explain_cache_tier1.sql"

// tier1Facts 는 **런타임에 실제로 나타날 수 있는 Tier 1 사실 모양 전부**다.
//
// 문서가 오래 「카테고리 8 × 레벨 3 = 24」라고 적어 왔는데, 실제로 세어 보면 **21이다.**
// 8×3은 (카테고리, 레벨) 쌍의 수이지 키의 수가 아니었다 — 세 곳이 어긋난다.
//
//	hangs_piece      **언제나 Tier 2다.** 이 카테고리로 분류되려면 `LandsAttacked` 가 참이어야
//	                 하고(intervene.Features.HangsPiece), 그 값과 `Attackers` 는 game.moveFacts
//	                 에서 **같은 목록**으로 나온다. 잡을 수 있는 수가 있으면 매수는 1 이상이다
//	greedy_capture   같은 이유로 언제나 Tier 2다. `CapturedValue > 0` 이 분류 조건인데
//	                 딴 駒의 이름(`Captured`)이 그 값과 같은 자리에서 나온다
//	other            반대로 **키가 둘이다.** 판을 못 읽으면 Known=false 로 떨어지고(그때
//	                 카테고리도 other 가 된다), 읽었지만 반박 수순의 첫 수가 駒를 안 따면
//	                 Known=true·thr="" 다. 프롬프트는 같은데 키가 갈린다
//
// missed_mate 는 `LostMate` 가 언제나 참이다 — classify 가 종반 경로에서만 그 값을 준다.
// 그래서 (missed_mate, mate=false) 라는 키는 없다.
//
// **없는 모양을 만들지 않는다.** 안 생기는 키에 문장을 넣으면 돈만 쓰고 히트는 0인데,
// 그 행이 `ExplainCacheStats.entries` 를 늘려 **히트율을 실제보다 낮게** 보이게 한다.
func tier1Facts() []Facts {
	levels := []intervene.Level{intervene.Beginner, intervene.Novice, intervene.Intermediate}
	// 사실이 하나도 안 붙는 카테고리들 — `used` 의 default 가지다. 카테고리 자체가 이미
	// 구체적이라 붙일 것이 없고, 그래서 문장이 국면을 안 짚는다.
	plain := []intervene.Category{
		intervene.CategoryShallowTrap,
		intervene.CategoryUnpromoted,
		intervene.CategoryIdleCheck,
		intervene.CategoryKingExposed,
	}

	var out []Facts
	for _, lv := range levels {
		out = append(out, Facts{
			Kind: intervene.KindBlunder, Category: intervene.CategoryMissedMate,
			Level: lv, LostMate: true,
		})
		for _, c := range plain {
			out = append(out, Facts{Kind: intervene.KindBlunder, Category: c, Level: lv})
		}
		// 판을 못 읽은 쪽. 여기로 오는 것은 우리 버그이고(game.EngineAnalyst 가 로그를
		// 남긴다) 그래도 카드는 떠야 한다.
		out = append(out, Facts{Kind: intervene.KindBlunder, Category: intervene.CategoryOther, Level: lv})
		// 읽었지만 잡히는 駒가 없는 쪽. 흔한 경로다 — 반박 수순의 첫 수가 따는 수일 때만
		// `Threatened` 가 붙는다(game.refutationLine).
		out = append(out, Facts{
			Kind: intervene.KindBlunder, Category: intervene.CategoryOther,
			Level: lv, Known: true,
		})
	}
	return out
}

// tier1Checks 는 **문장이 사실을 지키는지**를 카테고리마다 본다.
//
// 런타임 문장은 한 번 뜨고 사라지지만 **여기서 만든 것은 캐시에 박혀 계속 나간다.** 그래서
// 생성기는 런타임보다 엄해야 한다 — `clean` 은 형식(길이·한글·지어낸 칸)만 보고, 뜻이
// 뒤집힌 문장은 형식이 멀쩡하다.
//
// 목록은 `real_router_test.go` 의 `mustMention`·`mustNotMention` 과 같은 성격이고, 실제로
// **여기서 한 번 걸렸다.** `temperature=0` 인데도 같은 프롬프트가 두 번째 실행에서
// 「自分に詰ませる手があったのに」로 나왔다 — 詰ませる 쪽이 나인데 「自分に」로 적으면
// 내가 詰まされる 것으로 읽힌다. §28에서 고쳤던 그 실수가 모양만 바꿔 돌아온 것이고,
// 그때는 사람이 잡았지만 이번에는 캐시에 그대로 들어갈 뻔했다.
var tier1Checks = map[intervene.Category]struct{ must, mustNot []string }{
	intervene.CategoryMissedMate: {
		must:    []string{"詰"},
		mustNot: []string{"相手に詰み", "相手の詰み", "自分に詰", "詰まされ"},
	},
	// 이 카테고리가 생긴 이유가 「敵陣から出る手も成れる」다(08-playtest.md §6-6).
	intervene.CategoryUnpromoted:  {must: []string{"出", "成"}},
	intervene.CategoryIdleCheck:   {must: []string{"王手"}},
	intervene.CategoryKingExposed: {must: []string{"玉"}},
}

// tier1MustNot 은 카테고리와 무관하게 못 쓰는 말이다.
//
// 앱이 무엇을 했는지는 화면이 이미 말한다. 그 자리는 왜 나쁜지가 들어갈 자리다(§38).
var tier1MustNot = []string{"アプリ"}

// checkTier1Body 는 문장이 규칙을 어겼으면 그 이유를 준다. 지켰으면 빈 문자열이다.
func checkTier1Body(c intervene.Category, body string) string {
	for _, bad := range tier1MustNot {
		if strings.Contains(body, bad) {
			return fmt.Sprintf("%q 가 들어 있다", bad)
		}
	}
	rule := tier1Checks[c]
	for _, want := range rule.must {
		if !strings.Contains(body, want) {
			return fmt.Sprintf("우리가 준 사실 %q 가 문장에 없다", want)
		}
	}
	for _, bad := range rule.mustNot {
		if strings.Contains(body, bad) {
			return fmt.Sprintf("뜻이 뒤집힌 표현 %q 가 들어 있다", bad)
		}
	}
	return ""
}

// tier1Attempts 는 한 문장에 몇 번까지 물어보는가다. 넘으면 파일을 안 쓰고 멈춘다.
const tier1Attempts = 4

// tier1KeyCount 는 위 열거의 키 수다. **문서가 인용하는 숫자라 여기에 박아 둔다.**
//
// 바뀌면 이 상수와 함께 04-llm.md §2·06-status.md §5·§28·§38이 같이 틀린다. 그때 테스트가
// 먼저 깨지는 편이, 문서만 조용히 어긋나 있는 것보다 낫다(CLAUDE.md).
const tier1KeyCount = 21

// TestTier1FactsAreCanonical 은 열거가 **키를 만드는 코드와 같은 모양인지** 본다.
//
// 키를 만드는 것은 Go이지 SQL이 아니다. 그래서 사전 생성의 위험은 문장 품질이 아니라
// **키가 한 칸 어긋나는 것**이고, 그러면 아무 일도 일어나지 않는 행 21개가 프로덕션에
// 남는다 — 실패가 조용해서 눈에 안 띈다.
func TestTier1FactsAreCanonical(t *testing.T) {
	seen := make(map[string]Facts)
	for _, f := range tier1Facts() {
		// `used` 를 지난 모양이어야 한다. 아니면 내가 적어 둔 사실과 키에 실제로 들어간
		// 사실이 다르고, 마이그레이션 주석이 거짓말을 하게 된다.
		if got := f.used(); !reflect.DeepEqual(got, f) {
			t.Errorf("정규형이 아니다 — used() 가 %+v 를 %+v 로 바꾼다", f, got)
		}
		if tier := f.Tier(); tier != 1 {
			t.Errorf("%s: Tier=%d — Tier 1이 아닌 모양이 열거에 들어 있다", f.keyMaterial(), tier)
		}
		if prev, dup := seen[f.Key()]; dup {
			t.Errorf("키가 겹친다: %+v 와 %+v", prev, f)
		}
		seen[f.Key()] = f
	}
	if len(seen) != tier1KeyCount {
		t.Errorf("키가 %d개다 — %d개로 알고 있었다. 문서의 숫자도 같이 고칠 것(04-llm.md §2 · 06-status.md §5·§28)",
			len(seen), tier1KeyCount)
	}
}

// TestTier1MigrationMatchesFacts 는 **만들어 둔 파일이 지금 코드의 키와 맞는지** 본다.
//
// 여기가 `promptVersion` 의 감시자다. 프롬프트를 고치면 키가 전부 바뀌는데, 그때 옛 행은
// 아무도 찾지 않는 채로 남고 개입은 다시 매번 라우터를 부른다 — **느려진 것으로만 나타나서
// 원인을 짚기 어렵다.** 그리고 문장 자체도 런타임과 같은 규칙(clean)으로 한 번 더 본다:
// 파일이 캐시로 들어가면 그 문장은 화면에 **그대로** 나가고, 그 앞에 아무 검사도 없다.
func TestTier1MigrationMatchesFacts(t *testing.T) {
	raw, err := os.ReadFile(tier1MigrationPath)
	if err != nil {
		t.Fatalf("사전 생성 파일을 못 읽었다 (%s): %v", tier1MigrationPath, err)
	}
	sql := string(raw)

	want := make(map[string]Facts)
	for _, f := range tier1Facts() {
		want[f.Key()] = f
	}

	got := make(map[string]bool)
	for _, row := range tier1Rows(sql) {
		got[row.key] = true
		f, ok := want[row.key]
		if !ok {
			continue // 아래에서 「코드가 만들지 않는 키」로 잡는다
		}

		// **런타임이 거는 규칙을 그대로 건다.** 캐시에서 나온 문장 앞에는 아무 검사도 없다 —
		// `clean` 은 라우터가 답했을 때만 지나므로, 여기 들어간 것은 그대로 화면에 나간다.
		body, usable := Clean(row.body, MaxRunes)
		if !usable {
			t.Errorf("%s: clean 이 버릴 문장이다: %q", f.keyMaterial(), row.body)
			continue
		}
		if body != row.body {
			t.Errorf("%s: clean 이 손대는 문장이다 — 그대로 나갈 것이 아니다: %q", f.keyMaterial(), row.body)
		}
		for _, r := range row.body {
			if unicode.Is(unicode.Hangul, r) {
				t.Errorf("%s: 한글이 섞였다: %q", f.keyMaterial(), row.body)
				break
			}
		}
		if why := checkTier1Body(f.Category, row.body); why != "" {
			t.Errorf("%s: %s: %q", f.keyMaterial(), why, row.body)
		}
	}

	for key, f := range want {
		if !got[key] {
			t.Errorf("키가 파일에 없다 — %s\n  다시 만들 것: SHOWGI_GENERATE_TIER1=1 go test ./internal/explain/ -run GenerateTier1", f.keyMaterial())
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("코드가 만들지 않는 키가 파일에 있다: %s — promptVersion 이 올라갔거나 열거가 바뀌었다", key)
		}
	}
	if !strings.Contains(sql, "ON CONFLICT (key) DO NOTHING") {
		t.Error("ON CONFLICT (key) DO NOTHING 이 없다 — 같은 키를 덮어쓰면 「같은 실수에는 같은 설명」이 깨진다")
	}
}

// TestGenerateTier1 은 **문장을 실제로 만들어 마이그레이션으로 떨어뜨린다.**
//
// 돈이 든다. 프롬프트가 겹치는 것을 빼고 18번 부르므로 한 번 돌리는 값이 개입 18번과 같다.
func TestGenerateTier1(t *testing.T) {
	if os.Getenv("SHOWGI_GENERATE_TIER1") == "" {
		t.Skip("SHOWGI_GENERATE_TIER1 미설정 — Tier 1 사전 생성 건너뜀")
	}
	key := os.Getenv("ORCA_API_KEY")
	if key == "" {
		// **여기서는 조용히 넘어가지 않는다.** 키가 없으면 문구가 결정적 템플릿으로 도는데,
		// 그걸 캐시에 적으면 「LLM이 쓴 문장」이라고 거짓말하는 행이 프로덕션에 남고
		// `explain_tier` 가 0(캐시 히트)으로 세어진다 — 애초에 안 부른 것과 구별이 사라진다.
		t.Fatal("ORCA_API_KEY 가 없다 — 사전 생성은 실제 라우터가 쓴 문장을 넣는 일이라 키 없이는 의미가 없다")
	}

	client := NewClient(key, os.Getenv("ORCA_BASE_URL"), os.Getenv("ORCA_MODEL_SMALL"), os.Getenv("ORCA_MODEL_LARGE"), DefaultUSDJPY)
	if client == nil {
		t.Fatal("키가 있는데 클라이언트가 nil이다")
	}
	// **런타임과 같은 경로로 만든다**(Layered.Explain). 문장을 여기서 따로 만들면 clean 을
	// 안 지나는 문장이 캐시에만 생길 수 있고, 그건 화면 앞에 아무 검사도 없는 자리다.
	// store 는 nil 이다 — 만든 것을 DB가 아니라 파일로 떨어뜨리는 것이 이 생성기의 일이다.
	//
	// 시한만 다르다. Deadline(5초)은 **플레이어를 기다리게 하는 예산**인데 여기서 만든
	// 문장은 런타임에 캐시에서 나오므로 그 예산과 무관하다 — 느리게 나온 좋은 문장을
	// 버릴 이유가 없다. 넉넉한 것은 실제로 필요해서다: 같은 프롬프트가 1.2초에 오다가
	// **31.6초**가 걸린 적이 있다(§38). 런타임이라면 그건 템플릿행이다.
	l := &Layered{client: client, deadline: 60 * time.Second}

	var rows []tier1Row
	bodies := make(map[string]Result) // 프롬프트가 같으면 한 번만 부른다
	calls := 0
	start := time.Now()

	for _, f := range tier1Facts() {
		prompt := userPrompt(f, nil)
		res, done := bodies[prompt]
		if !done {
			// **다시 물어본다.** 두 가지가 이따금 어긋난다 — 라우터가 30초 넘게 멎고(35회
			// 중 2회), `temperature=0` 인데도 같은 프롬프트가 다른 문장을 준다(§38). 런타임
			// 이라면 둘 다 그 한 번으로 끝이지만, 여기서 나온 문장은 캐시에 박힌다.
			var why string
			for attempt := 1; attempt <= tier1Attempts; attempt++ {
				res = l.Explain(t.Context(), f)
				calls++
				if res.Tier == TierTemplate {
					why = "템플릿으로 떨어졌다"
				} else {
					why = checkTier1Body(f.Category, res.Body)
				}
				if why == "" {
					break
				}
				t.Logf("%s: %d번째 시도 — %s: %q", f.keyMaterial(), attempt, why, res.Body)
			}
			// **못 쓸 문장을 적느니 아무것도 안 적는다.** 결정적 문구를 캐시에 넣으면
			// 런타임이 영원히 그것을 Tier 0으로 돌려주면서 LLM을 안 부르고, 뜻이 뒤집힌
			// 문장을 넣으면 초심자가 그것을 검증할 수단 없이 배운다.
			if why != "" {
				t.Fatalf("%s: %d번 시도해도 못 쓸 문장이다 (%s): %q", f.keyMaterial(), tier1Attempts, why, res.Body)
			}
			bodies[prompt] = res
			t.Logf("%s\n  %s", f.keyMaterial(), res.Body)
		}
		if res.Tier != 1 {
			t.Fatalf("%s: tier=%d — Tier 1이 아닌 것을 만들고 있다", f.keyMaterial(), res.Tier)
		}
		rows = append(rows, tier1Row{facts: f, body: res.Body, model: res.Model})
	}

	if err := os.WriteFile(tier1MigrationPath, []byte(tier1SQL(rows)), 0o644); err != nil {
		t.Fatalf("마이그레이션을 못 썼다: %v", err)
	}
	t.Logf("%d키 · 프롬프트 %d개 · 호출 %d회 · %v — %s",
		len(rows), len(bodies), calls, time.Since(start).Round(time.Millisecond), tier1MigrationPath)
}

type tier1Row struct {
	facts       Facts
	body, model string
}

// tier1SQL 은 만들어진 문장을 마이그레이션으로 적는다.
//
// **행마다 해시 앞에 사람이 읽을 수 있는 키를 주석으로 단다.** 안 그러면 리뷰어가 보는 것이
// 64자 16진수 21줄뿐이고, 그건 확인이 아니라 서명이다.
func tier1SQL(rows []tier1Row) string {
	var b strings.Builder
	b.WriteString(tier1Header)
	b.WriteString("\nBEGIN;\n\nINSERT INTO explain_cache (key, body, model) VALUES\n")
	for i, r := range rows {
		end := ","
		if i == len(rows)-1 {
			end = ""
		}
		fmt.Fprintf(&b, "-- %s\n('%s',\n '%s',\n '%s')%s\n",
			r.facts.keyMaterial(), r.facts.Key(), sqlQuote(r.body), sqlQuote(r.model), end)
	}
	b.WriteString("ON CONFLICT (key) DO NOTHING;\n\nCOMMIT;\n")
	return b.String()
}

const tier1Header = `-- Tier 1 문구를 미리 만들어 넣는다 — 이 층의 개입은 런타임 LLM 왕복이 0이 된다.
--
-- **손으로 고치지 않는다.** internal/explain 의 생성기가 만든 파일이고, 고칠 일이 생기면
-- 프롬프트를 고쳐 다시 만든다:
--
--     set -a && . ../../.env && set +a
--     SHOWGI_GENERATE_TIER1=1 go test ./internal/explain/ -run GenerateTier1 -v
--
-- 문장은 실제 라우터가 쓴 것이고 런타임과 **같은 경로**(Layered.Explain)로 만들어졌다 —
-- 같은 검사(explain.clean)를 지났고, 그래서 한글도 지어낸 칸도 들어 있지 않다.
--
-- 키는 Go가 만든다(explain.Facts.Key). 행마다 붙은 주석이 해시하기 전의 그 값이고,
-- 맨 앞의 v1 이 promptVersion 이다 — **프롬프트를 고치면 그 숫자가 올라가고 아래 행은
-- 전부 아무도 찾지 않는 키가 된다.** TestTier1MigrationMatchesFacts 가 그것을 잡는다.
--
-- **추가만 하는 마이그레이션이다.** ON CONFLICT DO NOTHING 이라 이미 런타임이 만들어 둔
-- 문장을 덮지 않고("같은 실수에는 같은 설명"), 두 번 돌려도 같은 상태가 된다.
--
-- 21행인 이유는 카테고리 8 × 레벨 3 = 24 에서 hangs_piece 와 greedy_capture 가 빠지고
-- (그 둘은 분류 조건이 곧 Tier 2 조건이라 Tier 1로 오지 않는다) other 가 둘로 갈리기
-- 때문이다. 자세한 것은 internal/explain/tier1_test.go 의 tier1Facts 주석.
--
-- 이 행들은 hits=0 으로 시작한다. explain_cache 의 entries 가 이제 「만들어 둔 것 +
-- 런타임이 만든 것」이라, 히트율을 그 두 값으로만 세면 실제보다 낮게 나온다.
`

// sqlQuote 는 작은따옴표만 겹친다. 문장은 clean 을 지나 줄바꿈도 제어문자도 없고,
// standard_conforming_strings 가 켜져 있어 역슬래시는 그냥 글자다.
func sqlQuote(s string) string { return strings.ReplaceAll(s, "'", "''") }

// tier1RowRe 는 생성기가 적는 행 하나다. **생성기가 쓰는 모양과 맞물려 있다** — 한쪽을
// 고치면 다른 쪽이 못 읽고, 그러면 이 파일을 검사하는 테스트가 아무것도 못 보게 된다.
var tier1RowRe = regexp.MustCompile(`\('([0-9a-f]{64})',\n '((?:[^']|'')*)',\n '((?:[^']|'')*)'\)`)

// tier1Rows 는 마이그레이션에서 (키, 문장, 모델)을 뽑는다.
func tier1Rows(sql string) []struct{ key, body, model string } {
	var out []struct{ key, body, model string }
	for _, m := range tier1RowRe.FindAllStringSubmatch(sql, -1) {
		out = append(out, struct{ key, body, model string }{
			key:   m[1],
			body:  strings.ReplaceAll(m[2], "''", "'"),
			model: strings.ReplaceAll(m[3], "''", "'"),
		})
	}
	return out
}
