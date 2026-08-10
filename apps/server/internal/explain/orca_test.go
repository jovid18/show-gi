package explain

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
)

// fakeStore 는 Tier 0 캐시를 대신한다.
type fakeStore struct {
	mu      sync.Mutex
	entries map[string]string
	saved   map[string]string
	models  map[string]string
	gets    int
	getErr  error
	saveErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{entries: map[string]string{}, saved: map[string]string{}, models: map[string]string{}}
}

func (s *fakeStore) CachedExplanation(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets++
	if s.getErr != nil {
		return "", false, s.getErr
	}
	body, ok := s.entries[key]
	return body, ok, nil
}

func (s *fakeStore) SaveExplanation(_ context.Context, key, body, model string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved[key], s.models[key] = body, model
	s.entries[key] = body
	return nil
}

func (s *fakeStore) savedBody(key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	body, ok := s.saved[key]
	return body, ok
}

// routerStub 은 OrcaRouter 를 흉내낸다. 헤더 이름과 본문 모양은 실제 라우터 소스에서 확인한
// 것이다 — `x-orca-cache` · `x-orca-resolved-model`, 그리고 비용은 `usage.cost_usd`.
type routerStub struct {
	*httptest.Server
	mu       sync.Mutex
	calls    int
	requests []chatRequest
}

type stubReply struct {
	status   int
	content  string
	costUSD  *float64
	metaCost *float64
	cache    string
	model    string
	delay    time.Duration
	raw      string
}

func newRouterStub(t *testing.T, reply stubReply) *routerStub {
	t.Helper()
	s := &routerStub{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/chat/completions" {
			t.Errorf("경로가 다르다: %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization 이 다르다: %q", got)
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("요청을 못 읽었다: %v", err)
		}
		s.mu.Lock()
		s.calls++
		s.requests = append(s.requests, req)
		s.mu.Unlock()

		if reply.delay > 0 {
			select {
			case <-time.After(reply.delay):
			case <-r.Context().Done():
				return // 클라이언트가 시한으로 끊었다
			}
		}

		if reply.cache != "" {
			w.Header().Set("x-orca-cache", reply.cache)
		}
		if reply.model != "" {
			w.Header().Set("x-orca-resolved-model", reply.model)
		}
		status := reply.status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)

		if reply.raw != "" {
			_, _ = io.WriteString(w, reply.raw)
			return
		}
		body := map[string]any{
			"model":   "stub-model",
			"choices": []any{map[string]any{"message": map[string]string{"role": "assistant", "content": reply.content}}},
			"usage":   map[string]any{"prompt_tokens": 120, "completion_tokens": 30},
		}
		if reply.costUSD != nil {
			body["usage"].(map[string]any)["cost_usd"] = *reply.costUSD
		}
		if reply.metaCost != nil {
			body["_orca_meta"] = map[string]any{"cost_usd": *reply.metaCost}
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *routerStub) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *routerStub) lastRequest(t *testing.T) chatRequest {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		t.Fatal("요청이 한 번도 안 왔다")
	}
	return s.requests[len(s.requests)-1]
}

func layeredFor(stub *routerStub, st Store, small, large string) *Layered {
	c := NewClient("test-key", stub.URL+"/v1", small, large, 100)
	l := NewLayered(st, c)
	l.deadline = 2 * time.Second
	return l
}

var hangsPiece = Facts{
	Kind: intervene.KindBlunder, Category: intervene.CategoryHangsPiece, Level: intervene.Beginner,
	Known: true, MovedPiece: "銀", Attackers: 2,
}

// 캐시에 있으면 **HTTP가 아예 안 나간다.** 그것이 Tier 0이고, 발표의 「절감 1단계」다.
func TestCacheHitNeverCallsTheRouter(t *testing.T) {
	stub := newRouterStub(t, stubReply{content: "呼ばれてはいけない"})
	st := newFakeStore()
	st.entries[hangsPiece.Key()] = "その銀は2枚に狙われています。"

	got := layeredFor(stub, st, "", "").Explain(t.Context(), hangsPiece)

	if got.Tier != 0 {
		t.Errorf("Tier=%d, want 0", got.Tier)
	}
	if got.Body != "その銀は2枚に狙われています。" {
		t.Errorf("캐시 문장이 안 나왔다: %q", got.Body)
	}
	if got.CostYen != 0 {
		t.Errorf("CostYen=%v, want 0", got.CostYen)
	}
	if n := stub.callCount(); n != 0 {
		t.Errorf("캐시에 있는데 라우터를 %d번 불렀다", n)
	}
}

// 만든 문장은 **캐시에 남는다.** 안 남으면 같은 실수마다 같은 돈을 다시 낸다.
func TestGeneratedSentenceIsCachedAndPriced(t *testing.T) {
	cost := 0.002 // USD
	stub := newRouterStub(t, stubReply{
		content: "その銀を取れる相手の駒が2枚あります。", costUSD: &cost, model: "tiny-jp", cache: "MISS",
	})
	st := newFakeStore()

	got := layeredFor(stub, st, "", "").Explain(t.Context(), hangsPiece)

	if got.Tier != 2 {
		t.Errorf("Tier=%d, want 2 (국면 사실이 문장에 들어간다)", got.Tier)
	}
	if got.Model != "tiny-jp" {
		t.Errorf("Model=%q — x-orca-resolved-model 을 안 읽었다", got.Model)
	}
	if got.RouterCached {
		t.Error("MISS 인데 RouterCached 가 참이다")
	}
	// usdJPY=100 으로 만들었으므로 0.002 USD = 0.2엔이다.
	if got.CostYen < 0.199 || got.CostYen > 0.201 {
		t.Errorf("CostYen=%v, want 0.2 (usage.cost_usd × 환율)", got.CostYen)
	}
	if body, ok := st.savedBody(hangsPiece.Key()); !ok || body != got.Body {
		t.Errorf("캐시에 안 남았다: %q ok=%v", body, ok)
	}
}

// 비용이 `_orca_meta` 로 오는 경로도 읽는다. 라우터 자신이 두 곳을 다 본다.
func TestCostAlsoComesFromOrcaMeta(t *testing.T) {
	cost := 0.001
	stub := newRouterStub(t, stubReply{content: "その銀が取られます。", metaCost: &cost})
	got := layeredFor(stub, newFakeStore(), "", "").Explain(t.Context(), hangsPiece)
	if got.CostYen < 0.0999 || got.CostYen > 0.1001 {
		t.Errorf("CostYen=%v, want 0.1", got.CostYen)
	}
}

// **라우터 캐시에 맞으면 0엔이다.** 본문의 usage 는 원래 호출의 것이 그대로 실려 오므로,
// 그대로 곱하면 같은 돈을 두 번 센다 — 발표에 나가는 숫자가 부풀어 오른다.
func TestRouterCacheHitCostsNothing(t *testing.T) {
	cost := 0.002
	stub := newRouterStub(t, stubReply{content: "その銀が取られます。", costUSD: &cost, cache: "HIT"})

	got := layeredFor(stub, newFakeStore(), "", "").Explain(t.Context(), hangsPiece)

	if !got.RouterCached {
		t.Error("HIT 을 안 읽었다")
	}
	if got.CostYen != 0 {
		t.Errorf("CostYen=%v — 라우터 캐시 히트는 0엔이다", got.CostYen)
	}
}

// 비용이 아예 안 오면 **0으로 적는다.** 토큰 수와 가격표로 추정해 채우면 발표에 나가는
// 「1회당 ○엔」이 우리가 만든 숫자가 된다.
func TestMissingCostIsZeroNotEstimated(t *testing.T) {
	stub := newRouterStub(t, stubReply{content: "その銀が取られます。"})
	got := layeredFor(stub, newFakeStore(), "", "").Explain(t.Context(), hangsPiece)
	if got.CostYen != 0 {
		t.Errorf("CostYen=%v, want 0 — 없는 비용을 추정하지 않는다", got.CostYen)
	}
	if got.Tier != 2 {
		t.Errorf("Tier=%d — 비용이 없다고 계층이 바뀌지는 않는다", got.Tier)
	}
}

// 실패·지연·못 쓸 문장은 전부 **결정적 문구로** 떨어진다. 카드가 비는 경로가 없다.
func TestEveryFailureFallsBackToTheTemplate(t *testing.T) {
	tests := []struct {
		name  string
		reply stubReply
	}{
		{"라우터가 500", stubReply{status: http.StatusInternalServerError, raw: `{"error":"boom"}`}},
		{"키가 틀렸다", stubReply{status: http.StatusUnauthorized, raw: `{"error":"bad key"}`}},
		{"본문이 JSON이 아니다", stubReply{raw: "not json"}},
		{"choices 가 비었다", stubReply{raw: `{"choices":[]}`}},
		{"빈 문장", stubReply{content: "   "}},
		{"한글이 섞였다", stubReply{content: "その銀은 取られます。"}},
		{"너무 길다", stubReply{content: strings.Repeat("長", MaxRunes+1)}},
	}
	want := Render(hangsPiece)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := newRouterStub(t, tt.reply)
			st := newFakeStore()

			got := layeredFor(stub, st, "", "").Explain(t.Context(), hangsPiece)

			if got.Body != want {
				t.Errorf("결정적 문구가 안 나왔다: %q", got.Body)
			}
			if got.Tier != TierTemplate {
				t.Errorf("Tier=%d, want %d", got.Tier, TierTemplate)
			}
			if got.CostYen != 0 {
				t.Errorf("CostYen=%v — 실패에 값을 매기지 않는다", got.CostYen)
			}
			// **못 쓸 문장을 캐시에 넣지 않는다.** 넣으면 그 문장이 계속 나온다.
			if _, ok := st.savedBody(hangsPiece.Key()); ok {
				t.Error("버린 문장이 캐시에 들어갔다")
			}
		})
	}
}

// 시한을 넘기면 기다리지 않는다. **카드가 늦게 뜨는 것이 문장이 좋아지는 것보다 나쁘다.**
func TestSlowRouterFallsBackToTheTemplate(t *testing.T) {
	stub := newRouterStub(t, stubReply{content: "遅すぎる文", delay: 400 * time.Millisecond})
	l := NewLayered(newFakeStore(), NewClient("test-key", stub.URL+"/v1", "", "", 100))
	l.deadline = 30 * time.Millisecond

	start := time.Now()
	got := l.Explain(t.Context(), hangsPiece)
	elapsed := time.Since(start)

	if got.Tier != TierTemplate {
		t.Errorf("Tier=%d, want %d", got.Tier, TierTemplate)
	}
	if got.Body != Render(hangsPiece) {
		t.Errorf("결정적 문구가 안 나왔다: %q", got.Body)
	}
	if elapsed > 300*time.Millisecond {
		t.Errorf("시한(%v)을 안 지켰다: %v 기다렸다", l.deadline, elapsed)
	}
}

// 캐시가 고장 나도 **설명은 나간다.** 캐시는 비용을 아끼는 층이지 정합성의 층이 아니다.
func TestBrokenCacheDoesNotStopTheExplanation(t *testing.T) {
	stub := newRouterStub(t, stubReply{content: "その銀が取られます。"})
	st := newFakeStore()
	st.getErr = io.ErrUnexpectedEOF
	st.saveErr = io.ErrUnexpectedEOF

	got := layeredFor(stub, st, "", "").Explain(t.Context(), hangsPiece)

	if got.Body != "その銀が取られます。" {
		t.Errorf("LLM 문장이 안 나왔다: %q", got.Body)
	}
	if got.Tier != 2 {
		t.Errorf("Tier=%d, want 2", got.Tier)
	}
}

// 요청이 규약대로 나간다 — `temperature=0`(캐시가 성립하는 조건), 상한, 그리고 계층별 모델.
func TestRequestFollowsTheCachingContract(t *testing.T) {
	stub := newRouterStub(t, stubReply{content: "その銀が取られます。"})
	l := layeredFor(stub, newFakeStore(), "small-model", "large-model")

	l.Explain(t.Context(), hangsPiece) // 국면 사실이 있으므로 Tier 2
	req := stub.lastRequest(t)
	if req.Temperature != 0 {
		t.Errorf("temperature=%v, want 0 — 캐시가 성립하지 않는다", req.Temperature)
	}
	if req.MaxTokens != maxTokens {
		t.Errorf("max_tokens=%d, want %d", req.MaxTokens, maxTokens)
	}
	if req.Model != "large-model" {
		t.Errorf("Tier 2가 %q 로 갔다", req.Model)
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != "system" {
		t.Fatalf("메시지 모양이 다르다: %+v", req.Messages)
	}

	// Tier 1은 국면 사실이 없는 카테고리다.
	l.Explain(t.Context(), Facts{
		Kind: intervene.KindBlunder, Category: intervene.CategoryIdleCheck, Level: intervene.Beginner,
	})
	if req := stub.lastRequest(t); req.Model != "small-model" {
		t.Errorf("Tier 1이 %q 로 갔다", req.Model)
	}
}

// 키가 없으면 클라이언트를 만들지 않는다 — 배선이 그 nil을 보고 템플릿 전용으로 간다.
func TestNoKeyMeansNoClient(t *testing.T) {
	if c := NewClient("", "", "", "", 0); c != nil {
		t.Error("키가 없는데 클라이언트가 만들어졌다")
	}
}

// 캐시가 없어도(store nil) 돈다. 대신 매번 다시 산다.
func TestWorksWithoutACache(t *testing.T) {
	stub := newRouterStub(t, stubReply{content: "その銀が取られます。"})
	l := layeredFor(stub, nil, "", "")

	for range 2 {
		if got := l.Explain(t.Context(), hangsPiece); got.Tier != 2 {
			t.Errorf("Tier=%d, want 2", got.Tier)
		}
	}
	if n := stub.callCount(); n != 2 {
		t.Errorf("호출이 %d번 — 캐시가 없으면 매번 나가야 한다", n)
	}
}
