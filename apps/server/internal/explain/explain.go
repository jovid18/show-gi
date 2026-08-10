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
// 계층은 셋이다(docs/04-llm.md §2).
//
//	Tier 0  캐시 히트        HTTP가 아예 안 나간다. 0엔
//	Tier 1  카테고리·레벨    국면 사실이 문장에 없다 — 키가 24가지뿐이라 히트가 흔하다
//	Tier 2  국면 고유 사실   利き 매수·잡히는 駒가 들어간다. 키가 그만큼 넓다
//
// **어느 층도 못 되면 Render 가 나간다.** 카드에 문장이 비는 것이 최악이고, 결정적 문구도
// 사실을 전부 담는다 — 키가 없어도, 라우터가 죽어도, 느려도 제품은 그대로 선다. 그래서
// LLM은 이 제품의 필수 부품이 아니라 문장의 품질을 올리는 층이다.
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
//
// 0으로 적으면 「캐시 히트」와 구별이 안 되는데, 그 둘은 비용 계측에서 정반대의 뜻이다 —
// 히트는 부를 것을 아껴서 0엔이고, 템플릿은 애초에 부르지 않은 것이다. 섞이면 「호출을
// 구조적으로 줄였다」는 발표의 숫자가 「LLM을 안 붙였다」와 같은 값이 된다.
const TierTemplate = -1

// Deadline 은 LLM 한 번에 주는 시간이다.
//
// 판정 goroutine 안에서 돌기 때문에 이 시간이 그대로 **카드가 뜨는 지연**에 더해진다.
// 판정 자체가 depth 12 탐색 두 번(≈800ms)이고 거기에 이만큼까지 붙는다.
//
// **이 제품에서는 기다림이 손실이 아니다.** 쇼기 상대가 몇 초 생각하는 것은 자연스럽고,
// 화면이 이미 「判定中」을 띄우고 있어서 그 시간이 **고민으로 읽힌다.** 다른 종류의 앱이라면
// 3초가 고장으로 보이겠지만 여기서는 그렇지 않다 — 그래서 예산을 실측을 덮을 만큼 넉넉하게
// 잡았다(실측 1.7~4.7초, 06-status.md §28). 넘으면 기다리지 않고 결정적 문구로 간다.
//
// **[미확정]** 어디까지가 「고민」이고 어디부터 「멈췄다」인지는 사람이 보고 정할 값이다.
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
	// Model 은 실제로 답한 모델(`x-orca-resolved-model`)이다. `model=auto` 로 보내므로
	// 우리가 고른 것이 아니라 라우터가 고른 것이고, 그래서 받아 적어 둔다.
	Model string
	// RouterCached 는 라우터 쪽 프롬프트 캐시에 맞았는가(`x-orca-cache: HIT`)다.
	//
	// **우리 Tier 0과 다른 층이다.** 우리 캐시는 HTTP를 아예 안 내보내고, 이쪽은 요청은
	// 나갔지만 상위 모델을 안 부른 것이다. 발표의 「절감 2단계」가 정확히 이 둘이다.
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
// **메모리 캐시를 두지 않는다.** 앞에 map을 하나 두면 그 히트가 `explain_cache.hits` 에
// 안 세어지고, 그러면 발표에 나가는 히트율이 실제보다 낮게 나온다. Postgres 왕복은 1ms
// 남짓이고 그 옆에서 판정이 800ms를 쓴다 — 정확한 계측을 살 값으로 싸다.
type Layered struct {
	store  Store
	client *Client
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

	tier := f.Tier()
	call, cancel := context.WithTimeout(ctx, l.timeout())
	defer cancel()

	out, err := l.client.complete(call, tier, f)
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

// clean 은 **LLM 출력을 믿지 않는다.**
//
// 엔진이 돌려준 수를 룰 엔진으로 검증하는 것과 같은 자리다(06-status.md §6 ③). 저쪽은
// 못 두는 수를 걸러내고 이쪽은 화면에 나갈 수 없는 문장을 걸러낸다 — 사람이 매번 읽고
// 확인할 수 없는 출력이라, 규칙을 코드가 지켜야 한다.
//
// 자르지 않고 버리는 이유는 **반쪽 문장이 틀린 문장**이기 때문이다. 결정적 문구는 언제나
// 준비돼 있으므로 버리는 쪽의 대가가 없다.
func clean(s string) (string, bool) {
	// 모델이 앞뒤에 붙이는 인용부호와 공백을 떼어낸다. 이것만은 문장을 바꾸지 않는다.
	body := strings.TrimSpace(s)
	body = strings.Trim(body, "「」\"'")
	body = strings.TrimSpace(body)

	if body == "" {
		return "", false
	}
	if len([]rune(body)) > MaxRunes {
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
	// **칸이나 수가 적혀 있으면 버린다 — 지어낸 것이다.**
	//
	// `Facts` 에는 칸도 수도 없다. 준 적이 없으므로 문장에 나타났다면 모델이 만든 것이고,
	// 초심자는 그것을 검증할 수단이 없다. 프롬프트가 「指し手は書かない」로 부탁하지만
	// **부탁은 규칙이 아니다.**
	//
	// 이것이 「최선수를 보여주지 않는다」(01-core.md §1)가 LLM 쪽으로 내려온 형태다 —
	// 모델은 최선수를 모르지만 그럴듯한 수를 적을 수는 있고, 그게 더 나쁘다.
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
