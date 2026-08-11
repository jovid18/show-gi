package explain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DefaultBaseURL 은 OrcaRouter의 호스팅 엔드포인트다. 자체 호스팅이면 여기를 바꾼다.
const DefaultBaseURL = "https://api.orcarouter.ai/v1"

// DefaultModel 은 **실측으로 고른 모델**이다(06-status.md §38).
//
// `orcarouter/auto` 를 기본값으로 두면 안 된다. 실제로 보내 보니 추론 모델로 라우팅되어
// 한 문장에 **추론 토큰 1000개 이상**을 쓰고 15~23초가 걸렸다 — 라우터는 「요구 능력을
// 충족하는 가장 싼 모델」을 고르지만, 우리 요구는 능력이 아니라 **짧고 빠른 것**이라 그
// 기준과 어긋난다.
//
// **그리고 하루 만에 `anthropic/claude-haiku-4.5` 가 못 쓰게 됐다**(§38). 그 채널이
// 지금 우리 프롬프트 앞에 **2만 2천 토큰짜리 시스템 프롬프트를 끼워 넣는다** — 우리가
// 보내는 200토큰이 23,275토큰이 되고, 우리 지시(60자·2문·전치 없음)가 그 밑에 깔려
// 見出し가 붙은 400~550자 답이 돌아온다. 그러면 `clean` 이 전부 버려서 **개입 설명이
// 100% 결정적 문구로 떨어진다.** 라우터가 무엇을 얹는지는 우리가 정하지 못한다.
//
// 다시 잰 값(같은 프롬프트, Tier 1·2 각 1회):
//
//	google/gemini-3.5-flash-lite    1.0~1.3초   ✅ 프롬프트 193토큰. 사실을 다 싣고 60자를 지킨다
//	openai/gpt-4.1-mini             1.0~2.7초   쓸 만하다. 다음 후보
//	openai/gpt-5.4-nano             1.6~1.8초   「アプリが止めて戻しました」까지 문장에 옮긴다
//	google/gemini-3.6-flash         2.1~2.4초   추론 토큰이 max_tokens 를 다 먹고 **잘려서 온다**
//	anthropic/claude-haiku-4.5      11~17초     위의 주입. 어제까지 기본값이었다
//
// **그래서 이 상수는 고정이 아니라 실측의 스냅샷이다.** 라우터 쪽이 바뀌면 또 갈아탄다 —
// `TestRealRouter` 가 그것을 잡는 자리이고, 실패하면 프롬프트가 아니라 채널을 먼저 의심한다.
//
// Tier 1·2가 같은 모델인 것은 실측 결과다. **Tier를 가르는 것은 모델 크기가 아니라 키의
// 재사용성**이고(Tier 1은 21가지뿐이다), 지금 문장 품질에 모델을 키울 이유가 안 보였다.
// **[미확정]** Tier 2에 더 큰 모델이 필요한 국면이 있는지.
const DefaultModel = "google/gemini-3.5-flash-lite"

// DefaultUSDJPY 는 `usage.cost_usd` 를 `interventions.cost_yen` 으로 옮기는 환율이다.
//
// 칸이 엔이라 어딘가에서 한 번은 곱해야 한다. 달러로 적고 질의에서 곱하는 대안은 칸 이름을
// 거짓으로 만들고, 실시간 환율을 받아오는 것은 이 제품의 일이 아니다.
// **[미확정]** 어림값이다. `ORCA_USDJPY` 로 덮는다.
const DefaultUSDJPY = 150.0

// maxTokens 는 답의 상한이다.
//
// 프롬프트가 60자를 넘기지 말라고 이미 말하지만, 그것은 부탁이고 이쪽은 **돈과 지연의
// 상한**이다. 안 걸어두면 지시를 안 듣는 모델 하나가 카드 하나에 수천 토큰을 쓴다.
const maxTokens = 200

// Client 는 OrcaRouter에 문장 하나를 물어본다.
//
// OpenAI 호환 HTTP라 SDK가 필요 없다. 우리가 쓰는 것은 `/chat/completions` 하나뿐이다.
type Client struct {
	http    *http.Client
	baseURL string
	apiKey  string
	small   string
	large   string
	usdJPY  float64
}

// NewClient 는 라우터 클라이언트를 만든다. **apiKey 가 비면 nil을 준다** — 부르는 쪽이
// nil을 그대로 NewLayered 에 넘기면 결정적 문구만 나가는 배선이 된다.
func NewClient(apiKey, baseURL, small, large string, usdJPY float64) *Client {
	if apiKey == "" {
		return nil
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if small == "" {
		small = DefaultModel
	}
	if large == "" {
		large = DefaultModel
	}
	if usdJPY <= 0 {
		usdJPY = DefaultUSDJPY
	}
	return &Client{
		http:    &http.Client{}, // 시한은 ctx가 건다(Deadline). 여기서 또 걸면 두 벌이 된다
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		small:   small,
		large:   large,
		usdJPY:  usdJPY,
	}
}

// completion 은 한 번의 호출에서 얻은 것 전부다.
type completion struct {
	body         string
	model        string
	costYen      float64
	routerCached bool
}

type chatRequest struct {
	Model string `json:"model"`
	// **temperature=0.** 같은 사실에 같은 문장이 나와야 캐시가 성립하고, 라우터의 크로스
	// 프로바이더 프롬프트 캐시도 이 조건에서 듣는다(docs/04-llm.md §3).
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	Messages    []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message chatMessage `json:"message"`
		// FinishReason 이 `length` 면 **max_tokens 에서 잘린 것**이다. 그 답은 짧아서
		// MaxRunes 검사를 그냥 통과한다 — 「王手はか」가 화면에 나갈 수 있다.
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		// CostUSD 는 상위 프로바이더가 준 실비다. **없을 수 있다.**
		CostUSD *float64 `json:"cost_usd"`
	} `json:"usage"`
	// OrcaMeta 는 라우터가 붙이는 자리다. 비용이 여기로 오는 경로도 있다.
	OrcaMeta *struct {
		CostUSD *float64 `json:"cost_usd"`
	} `json:"_orca_meta"`
}

func (c *Client) complete(ctx context.Context, tier int, f Facts) (completion, error) {
	model := c.small
	if tier == 2 {
		model = c.large
	}

	payload, err := json.Marshal(chatRequest{
		Model:       model,
		Temperature: 0,
		MaxTokens:   maxTokens,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt(f)},
		},
	})
	if err != nil {
		return completion{}, fmt.Errorf("explain: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return completion{}, fmt.Errorf("explain: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	res, err := c.http.Do(req)
	if err != nil {
		return completion{}, fmt.Errorf("explain: post: %w", err)
	}
	defer res.Body.Close()

	// 에러 본문을 조금 붙인다. 라우터는 어느 프로바이더에서 무엇이 틀렸는지를 본문에 적고,
	// 그게 없으면 401과 429와 모델 이름 오타가 화면에서 똑같이 보인다.
	if res.StatusCode/100 != 2 {
		snippet, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return completion{}, fmt.Errorf("explain: router returned %s: %s", res.Status, strings.TrimSpace(string(snippet)))
	}

	var out chatResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return completion{}, fmt.Errorf("explain: decode response: %w", err)
	}
	if len(out.Choices) == 0 {
		return completion{}, fmt.Errorf("explain: router returned no choices")
	}
	// **잘린 답은 버린다.** `clean` 은 긴 것을 잡지 짧은 것은 못 잡는데, `max_tokens` 에서
	// 잘린 문장은 짧게 온다 — 추론 모델이면 추론 토큰이 예산을 다 먹고 본문 몇 글자만
	// 남는다(`google/gemini-3.6-flash` 가 실제로 그랬다: 「王手はか」, §38).
	//
	// 자르지 않고 버리는 이유는 `clean` 과 같다 — **반쪽 일본어 문장은 뜻이 바뀌고**,
	// 초심자는 그것을 검증할 수단이 없다. 결정적 문구는 언제나 준비돼 있다.
	if out.Choices[0].FinishReason == "length" {
		return completion{}, fmt.Errorf("explain: router truncated the sentence at max_tokens (%d): %q",
			maxTokens, strings.TrimSpace(out.Choices[0].Message.Content))
	}

	// `x-orca-resolved-model` 이 **실제로 답한 모델**이다. `model=auto` 로 보내므로 우리가
	// 고른 것이 아니고, 라우터가 폴백하면 요청한 것과도 다르다(라우터 소스 확인).
	resolved := res.Header.Get("x-orca-resolved-model")
	if resolved == "" {
		resolved = out.Model
	}
	cached := strings.EqualFold(res.Header.Get("x-orca-cache"), "HIT")

	return completion{
		body:         out.Choices[0].Message.Content,
		model:        resolved,
		costYen:      c.costYen(out, cached),
		routerCached: cached,
	}, nil
}

// costYen 은 이 호출에 든 돈이다.
//
// **호스팅 라우터는 원당 비용을 안 준다 — 실측이다**(06-status.md §28). 헤더는
// `x-orca-request-id`·`x-orca-resolved-model`·`x-orca-router`·`x-orca-version` 이고
// (`x-orca-cache` 조차 안 온다), 본문 `usage` 에는 토큰 수만 있다. Lite 소스가 읽는
// `usage.cost_usd`·`_orca_meta.cost_usd` 경로는 남겨 뒀지만 **지금은 언제나 비어 있다.**
//
// 그래서 이 함수는 실질적으로 0을 돌려주고, `interventions.cost_yen` 도 0으로 남는다.
// **토큰 수와 가격표로 추정해 채우지 않는다** — 발표에 나가는 「개입 1회당 ○엔」이 우리가
// 만든 숫자가 되면 그 숫자는 근거가 없다. 총액은 라우터의 `/v1/analytics/spend` 가 답한다.
func (c *Client) costYen(out chatResponse, routerCached bool) float64 {
	// 라우터 캐시에 맞았으면 상위 모델을 안 불렀으므로 0이다. 이때 본문의 usage 는 원래
	// 호출의 것이 그대로 실려 오므로(라우터 소스), 그대로 곱하면 **같은 돈을 두 번 센다.**
	if routerCached {
		return 0
	}
	var usd float64
	switch {
	case out.Usage.CostUSD != nil:
		usd = *out.Usage.CostUSD
	case out.OrcaMeta != nil && out.OrcaMeta.CostUSD != nil:
		usd = *out.OrcaMeta.CostUSD
	default:
		return 0
	}
	if usd <= 0 {
		return 0
	}
	return usd * c.usdJPY
}
