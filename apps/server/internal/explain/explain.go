// Package explain 은 **이미 정해진 판단을 일본어 문장으로 바꾼다.**
//
// 여기서 판단하지 않는다. 무엇이 블런더이고 왜 그런지는 `intervene` 과 `game` 이 결정적
// 룰과 엔진 평가치로 이미 정했고, 이 패키지에 오는 것은 그 결과뿐이다 — 판도 SFEN도 cp도
// 들어오지 않는다. 판을 통째로 넣고 「이 수 어때?」를 묻는 코드가 들어오는 순간 이 레포의
// 이유가 사라진다(CLAUDE.md).
//
// 입력을 좁히는 것이 곧 안전장치다. LLM이 틀릴 수 있는 범위가 **문장뿐**이라, 사실을
// 지어내면 그것은 프롬프트가 준 목록을 어긴 것이므로 눈으로 잡힌다. 카테고리도 임계치도
// 그 문장 위에서 돌지 않는다.
//
// 계층은 Tier 0(캐시 히트) → 1(카테고리·레벨) → 2(국면 고유 사실)이고, **어느 층도 못 되면
// Render 가 나간다** — 결정적 문구도 사실을 전부 담으므로 라우터가 죽어도 제품이 그대로 선다.
// LLM은 필수 부품이 아니라 문장의 품질을 올리는 층이다(docs/04-llm.md §2).
package explain

import (
	"context"
	"log"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// TierTemplate 은 LLM을 거치지 않았다는 표시다. **DB에는 NULL로 들어간다.**
// 0으로 적으면 「캐시 히트」와 구별이 안 되고, 그 둘은 비용 계측에서 정반대다(04-llm.md §5).
const TierTemplate = -1

// Deadline 은 LLM 한 번에 주는 시간이다. 판정 goroutine 안에서 돌아 그대로 **카드가 뜨는
// 지연**에 더해지고, 넘으면 기다리지 않고 결정적 문구로 간다.
// 실측을 덮을 만큼 넉넉히 잡았다 — 이 제품에서 기다림은 손실이 아니다(06-status.md §28).
const Deadline = 5 * time.Second

// MaxRunes 는 받아들일 문장의 상한이다.
//
// 카드가 360px에서도 읽혀야 하고, 프롬프트가 60자를 넘기지 말라고 이미 말한다. 여기는
// 그 지시를 안 듣는 경우를 막는 자리라 넉넉하게 둔다 — 넘으면 자르지 않고 **버린다.**
// 중간에서 자른 일본어 문장은 뜻이 바뀌고, 초심자는 그것을 검증할 수단이 없다.
const MaxRunes = 120

// Result 는 문장 하나와 그것이 어디서 왔는지다.
type Result struct {
	// Body 는 화면에 그대로 나가는 일본어다. **절대 비지 않는다.**
	Body string
	// Tier 는 0·1·2, 또는 LLM을 안 거쳤으면 TierTemplate 이다.
	Tier int
	// CostYen 은 이 문장 하나에 든 돈. 캐시 히트와 템플릿은 0이다.
	CostYen float64
	// Model 은 실제로 답한 모델이다. 우리가 보낸 이름과 다를 수 있어(라우터 폴백) 요청값이
	// 아니라 응답이 말한 것을 받아 적는다.
	Model string
	// RouterCached 는 라우터 쪽 프롬프트 캐시에 맞았는가(`x-orca-cache: HIT`)다.
	// **우리 Tier 0과 다른 층이다** — 이쪽은 요청이 나갔다. 발표의 「절감 2단계」가 이 둘이다.
	RouterCached bool
}

// Explainer 는 사실을 문장으로 바꾼다.
//
// **에러를 돌려주지 않는다.** 부르는 쪽이 「실패하면 무엇을 띄울지」를 정하게 두면 그 판단이
// 세션 상태머신으로 새는데, 답은 언제나 하나다 — 결정적 문구를 띄운다. 그래서 실패는 여기서
// 삼키고 로그로만 남긴다.
type Explainer interface {
	Explain(ctx context.Context, f Facts) Result
}

// KbSnippet 는 `kb_chunks` 에서 꺼낸 지식 한 조각이다.
type KbSnippet struct {
	Title string
	Body  string
}

// KnowledgeLookup 은 태그로 `kb_chunks` 를 찾는 함수다. `Layered` 에 콜백으로 꽂는다.
//
// `Store` 인터페이스에 넣지 않는 이유는 **import cycle** 이다 — `store` → `explain` →
// `intervene` 이 되면 `intervene` 의 테스트가 `store` 를 import할 때 사이클이 생긴다.
// 콜백이면 `store` 가 `explain` 을 모른 채로 `cmd/api/main.go` 가 연결한다.
type KnowledgeLookup func(ctx context.Context, tags []string) ([]KbSnippet, error)

// Store 는 Tier 0 캐시다. `explain_cache` 테이블 두 줄에 대응한다.
//
// `game` 이 DB를 모르는 것과 같은 이유로 여기서도 인터페이스로 둔다 — 이 패키지는 SQL을
// 모르고, 옮기는 일은 `internal/store` 가 한다.
type Store interface {
	// CachedExplanation 은 키에 걸린 문장을 준다. 없으면 두 번째 값이 false 다.
	//
	// **히트 수를 세는 것도 여기다.** 그 숫자가 발표의 캐시 히트율이 된다.
	CachedExplanation(ctx context.Context, key string) (string, bool, error)
	// SaveExplanation 은 만든 문장을 남긴다. 같은 키가 이미 있으면 덮지 않는다.
	SaveExplanation(ctx context.Context, key, body, model string) error
}

// Layered 는 Tier 0 → 1·2 → 결정적 문구로 내려가는 Explainer 다.
//
// **메모리 캐시를 두지 않는다** — 앞에 map을 하나 두면 그 히트가 `explain_cache.hits` 에
// 안 세어져 발표에 나가는 히트율이 실제보다 낮게 나온다(04-llm.md §5).
type Layered struct {
	store    Store
	client   *Client
	lookupKB KnowledgeLookup
	// deadline 이 0이면 Deadline 을 쓴다. 테스트가 짧게 줄이는 통로다.
	deadline time.Duration
}

// NewLayered 는 캐시와 라우터를 얹은 Explainer 를 만든다.
//
// **둘 다 nil이어도 된다.** store 가 없으면 캐시가 없어 매번 부르고, client 가 없으면
// 결정적 문구만 나간다 — 후자가 `ORCA_API_KEY` 가 비어 있는 지금의 상태다.
func NewLayered(store Store, client *Client) *Layered {
	return &Layered{store: store, client: client}
}

// WithKnowledge 는 태그 기반 `kb_chunks` 검색을 꽂는다. nil이면 지식 없이 간다.
func (l *Layered) WithKnowledge(fn KnowledgeLookup) *Layered {
	l.lookupKB = fn
	return l
}

// TemplateOnly 는 LLM을 아예 안 부르는 Explainer 다. 키가 없을 때의 배선이다.
func TemplateOnly() *Layered { return &Layered{} }

func (l *Layered) Explain(ctx context.Context, f Facts) Result {
	f = f.used() // 이 카테고리의 문장이 쓸 수 있는 사실만 남긴다. 키와 프롬프트가 같은 목록을 본다
	key := f.Key()

	if l.store != nil {
		if body, ok, err := l.store.CachedExplanation(ctx, key); err != nil {
			// 캐시가 고장 난 것으로 설명을 멈추지 않는다. 아래로 내려간다.
			log.Printf("explain: cache lookup failed for %s: %v", key, err)
		} else if ok {
			return Result{Body: body, Tier: 0}
		}
	}

	if l.client == nil {
		return Result{Body: Render(f), Tier: TierTemplate}
	}

	var knowledge []KbSnippet
	if l.lookupKB != nil && len(f.Tags) > 0 {
		if kb, err := l.lookupKB(ctx, f.Tags); err != nil {
			log.Printf("explain: knowledge lookup failed for %v: %v", f.Tags, err)
		} else {
			knowledge = kb
		}
	}

	tier := f.Tier()
	call, cancel := context.WithTimeout(ctx, l.timeout())
	defer cancel()

	out, err := l.client.complete(call, tier, f, knowledge)
	if err != nil {
		// 여기서 기다리다 실패한 것이라 이미 지연을 냈다. 그래도 문장은 나간다.
		log.Printf("explain: tier %d failed, falling back to the template: %v", tier, err)
		return Result{Body: Render(f), Tier: TierTemplate}
	}

	body, ok := clean(out.body)
	if !ok {
		log.Printf("explain: tier %d returned an unusable sentence (%d runes), falling back to the template", tier, len([]rune(out.body)))
		return Result{Body: Render(f), Tier: TierTemplate}
	}

	if l.store != nil {
		// **부른 ctx를 쓰지 않는다.** 위에서 시한을 걸었으므로 여기까지 왔을 때 이미
		// 만료됐을 수 있고, 그러면 방금 돈을 내고 만든 문장을 저장하지 못한다.
		save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if err := l.store.SaveExplanation(save, key, body, out.model); err != nil {
			log.Printf("explain: could not cache %s: %v", key, err)
		}
	}

	return Result{Body: body, Tier: tier, CostYen: out.costYen, Model: out.model, RouterCached: out.routerCached}
}

func (l *Layered) timeout() time.Duration {
	if l.deadline > 0 {
		return l.deadline
	}
	return Deadline
}

func clean(s string) (string, bool) { return Clean(s, MaxRunes) }

// Clean 은 **LLM 출력을 믿지 않는다.** 화면에 나갈 수 없는 문장을 코드가 거른다 — 엔진이
// 돌려준 수를 룰 엔진으로 검증하는 것과 같은 자리다(06-status.md §6 ③).
// 자르지 않고 버리는 이유는 **반쪽 문장이 틀린 문장**이기 때문이다. 결정적 문구가 늘 준비돼 있다.
//
// 길이를 인자로 받는 것은 총평이 개입 문구보다 길기 때문이다(SummaryMaxRunes). **검사 자체를
// 두 벌로 두지 않는다** — 한글 혼입과 지어낸 칸은 두 자리에서 같은 규칙이어야 하고, 갈라
// 두면 한쪽을 고칠 때 다른 쪽이 조용히 낡는다.
func Clean(s string, maxRunes int) (string, bool) {
	// 모델이 앞뒤에 붙이는 인용부호와 공백을 떼어낸다. 이것만은 문장을 바꾸지 않는다.
	body := strings.TrimSpace(s)
	body = strings.Trim(body, "「」\"'")
	body = strings.TrimSpace(body)

	if body == "" {
		return "", false
	}
	if len([]rune(body)) > maxRunes {
		return "", false
	}
	// **한글이 한 글자라도 있으면 버린다.** 프롬프트가 전부 일본어라 여기 올 일이 없지만,
	// 새는 순간 화면이 「번역이 덜 된 앱」이 되고 그것을 볼 사람이 없다(CLAUDE.md).
	// 룰 엔진의 사유 문구에 한글이 없는지 기계로 확인하는 테스트와 같은 판단이다.
	for _, r := range body {
		if unicode.Is(unicode.Hangul, r) {
			return "", false
		}
	}
	// **칸이나 수가 적혀 있으면 버린다 — 지어낸 것이다.** `Facts` 에 칸도 수도 없으므로
	// 문장에 나타났다면 모델이 만든 것이다. 프롬프트가 「指し手は書かない」로 부탁하지만
	// **부탁은 규칙이 아니다** — 「최선수를 보여주지 않는다」(01-core.md §1)의 LLM판이다.
	if invented.MatchString(body) {
		return "", false
	}
	// 줄바꿈은 카드 레이아웃을 깨뜨린다. 한 줄로 만든다.
	body = strings.Join(strings.Fields(body), " ")
	return body, true
}

// invented 는 **우리가 준 적 없는 것**의 모양이다 — 칸(`8四`)과 棋譜의 수번 기호(`▲`·`△`).
//
// 칸은 「숫자 + 段」으로 본다. 段의 한자만 찾으면 「一手」「二枚換え」에 걸리고, 숫자만 찾으면
// 「2枚」에 걸린다 — 붙어 있는 것만이 칸이다.
//
// **[미확정]** 「八四」처럼 筋까지 한자로 적는 표기는 안 걸린다. 그렇게 적는 기보가 드물어
// 지금은 두는데, 段만으로 거르면 정상 문장이 대량으로 버려진다.
var invented = regexp.MustCompile(`[0-9０-９][一二三四五六七八九]|[▲△]`)
