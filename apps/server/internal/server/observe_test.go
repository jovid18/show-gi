package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/metrics"
)

func TestRequestIDIsGeneratedAndEchoed(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	id := rec.Header().Get(requestIDHeader)
	if len(id) != 16 {
		t.Fatalf("%s=%q — 16자 hex 를 기대했다", requestIDHeader, id)
	}
}

// 밖에서 온 ID 를 채택하지 않는다. 채택하면 누구나 같은 값을 계속 보내 request_id 가
// 요청 하나를 못 가리키게 만들 수 있다 — 장애를 되짚어야 하는 바로 그때.
func TestRequestIDFromCallerIsNotAdopted(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set(requestIDHeader, "abc-123_x.y")
	rec := httptest.NewRecorder()
	Handler(Options{}).ServeHTTP(rec, req)

	got := rec.Header().Get(requestIDHeader)
	if got == "abc-123_x.y" {
		t.Fatalf("밖에서 준 값을 그대로 썼다")
	}
	if len(got) != 16 {
		t.Fatalf("%s=%q — 우리가 만든 16자 hex 여야 한다", requestIDHeader, got)
	}
}

// 다만 버리지는 않는다. 로그에 한 필드로 남아 앞단의 ID 로도 되짚을 수 있다.
func TestClientRequestIDIsLogged(t *testing.T) {
	var buf syncBuffer
	restore := swapLogger(t, &buf)
	defer restore()

	req := httptest.NewRequest(http.MethodGet, "/api/openings", nil)
	req.Header.Set(requestIDHeader, "front-abc")
	Handler(Options{}).ServeHTTP(httptest.NewRecorder(), req)

	line := requestLine(t, &buf, "/api/openings")
	if line["client_request_id"] != "front-abc" {
		t.Fatalf("client_request_id=%v", line["client_request_id"])
	}
	if line["request_id"] == "front-abc" {
		t.Fatal("우리 ID 가 밖에서 온 값으로 덮였다")
	}
}

func TestUnsafeClientRequestIDIsDropped(t *testing.T) {
	// 길이도 글자도 제한한다. 로그를 보는 사람이 값의 끝을 알 수 있어야 한다.
	for _, given := range []string{
		strings.Repeat("a", maxRequestIDLen+1),
		"has space",
		"quote\"inside",
		"newline\nhere",
	} {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set(requestIDHeader, given)
		if got := clientRequestID(req); got != "" {
			t.Errorf("%q 를 그대로 남겼다", given)
		}
	}
}

func TestRouteLabelIsThePattern(t *testing.T) {
	reg := metrics.New("api", "test")
	h := Handler(Options{Metrics: reg})

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/openings", nil))

	out := metricsText(t, h)
	for _, want := range []string{
		`http_requests_total{route="GET /healthz",status="200"} 1`,
		`http_requests_total{route="GET /api/openings",status="200"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q 가 없다:\n%s", want, out)
		}
	}
}

// 라우팅에 안 걸린 요청은 라벨 하나로 모인다. 실제 경로를 라벨로 쓰면 스캐너 하나가
// 계열을 무한히 늘리고, 그 메모리는 프로세스가 사는 동안 안 돌아온다.
func TestUnmatchedRoutesShareOneLabel(t *testing.T) {
	reg := metrics.New("api", "test")
	h := Handler(Options{Metrics: reg})

	for _, path := range []string{"/wp-login.php", "/.env", "/admin"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	out := metricsText(t, h)
	if !strings.Contains(out, `http_requests_total{route="other",status="404"} 3`) {
		t.Fatalf("안 걸린 경로가 한 라벨로 안 모였다:\n%s", out)
	}
	for _, path := range []string{"wp-login", ".env", "admin"} {
		if strings.Contains(out, path) {
			t.Errorf("경로 %q 가 라벨로 새어 나갔다", path)
		}
	}
}

// 업그레이드가 지연 분포에 들어가면 안 된다. 대국 한 판이 분 단위라 다른 경로의
// 백분위를 통째로 못 읽게 만든다.
func TestUpgradeIsCountedButNotTimed(t *testing.T) {
	reg := metrics.New("api", "test")
	opts := Options{
		Metrics:     reg,
		NewOpponent: func() game.Opponent { return &scriptedOpponent{} },
	}
	conn, _ := dialWith(t, opts)
	conn.CloseNow()

	// 요청 한 줄은 핸들러가 돌아온 뒤에 남는다. 업그레이드된 요청은 그 시점이
	// 연결이 끊긴 뒤라서, 닫자마자 세면 아직 0이다.
	upgrades := func() float64 {
		return reg.HTTPRequests.SumFunc(func(l map[string]string) bool {
			return l["route"] == "GET /ws/game" && l["status"] == "101"
		})
	}
	waitFor(t, func() bool { return upgrades() == 1 }, "업그레이드가 세어지는 것")
	if n := reg.HTTPDuration.Count(func(l map[string]string) bool {
		return l["route"] == "GET /ws/game"
	}); n != 0 {
		t.Fatalf("업그레이드가 지연 분포에 %d개 들어갔다", n)
	}
	if got := reg.WSSessionsOpened.Total(); got != 1 {
		t.Fatalf("열린 세션 수=%v", got)
	}
}

// 세션 게이지는 대국이 끝나면 0으로 돌아온다. 안 돌아오면 며칠 뒤 「세션이 안 닫힌다」로
// 보이는데, 그때는 그것이 지표 버그인지 서버 버그인지 가릴 수 없다.
func TestSessionGaugeReturnsToZero(t *testing.T) {
	reg := metrics.New("api", "test")
	conn, ctx := dialWith(t, Options{
		Metrics:     reg,
		NewOpponent: func() game.Opponent { return &scriptedOpponent{moves: []string{"3c3d"}} },
	})
	read(t, ctx, conn) // 첫 스냅샷을 받아 세션이 실제로 섰음을 본다
	if got := reg.WSSessions.Total(); got != 1 {
		t.Fatalf("대국 중 세션 수=%v", got)
	}

	conn.CloseNow()
	waitFor(t, func() bool { return reg.WSSessions.Total() == 0 }, "세션 게이지가 0으로 돌아오는 것")
}

func TestObserveKeepsRequestIDInLogs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(LogHandler(slog.NewJSONHandler(&buf, nil)))

	// 미들웨어가 ctx 에 넣은 값을 핸들러가 다시 넘기지 않아도 붙어야 한다.
	ctx := context.WithValue(context.Background(), requestIDKey, "deadbeef")
	logger.InfoContext(ctx, "test line", "k", "v")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("로그가 JSON 이 아니다: %v\n%s", err, buf.String())
	}
	if line["request_id"] != "deadbeef" {
		t.Fatalf("request_id=%v", line["request_id"])
	}
}

func TestLogHandlerKeepsAttrsAndGroups(t *testing.T) {
	// WithAttrs·WithGroup 이 감싼 것을 벗기면 그 뒤 줄에서 request_id 가 사라진다.
	var buf bytes.Buffer
	logger := slog.New(LogHandler(slog.NewJSONHandler(&buf, nil))).With("component", "ws")

	ctx := context.WithValue(context.Background(), requestIDKey, "cafe")
	logger.InfoContext(ctx, "test line")

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("로그가 JSON 이 아니다: %v", err)
	}
	if line["request_id"] != "cafe" || line["component"] != "ws" {
		t.Fatalf("With 뒤에 잃었다: %v", line)
	}
}

// 지표가 꺼진 배포에서도 요청은 그대로 돈다. Options.Metrics 가 nil 인 경로다.
func TestHandlerWorksWithoutMetrics(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	// 그때는 /metrics 자체가 없다 — 있는 척하고 빈 답을 주면 스크레이퍼가 0을 진짜로 읽는다.
	rec = httptest.NewRecorder()
	Handler(Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("지표 없이 /metrics 가 %d 를 줬다", rec.Code)
	}
}

// waitFor 는 조건이 참이 될 때까지 짧게 기다린다. 세션이 닫히는 것은 연결이 끊긴 뒤에
// goroutine 이 정리하는 일이라 그 자리에서 바로 참이 되지 않는다.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s 를 못 봤다", what)
}

func metricsText(t *testing.T, h http.Handler) string {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d", rec.Code)
	}
	return rec.Body.String()
}

// 핸들러가 panic 하면 500으로 답하고, 지표와 로그에 남아야 한다.
//
// 안 잡으면 net/http 가 연결만 끊는다 — 상태 코드도 요청 로그도 지표도 없이. 그러면
// 가장 흔한 장애에 5xx 알람이 영원히 조용하다.
func TestPanicBecomesFiveHundred(t *testing.T) {
	reg := metrics.New("api", "test")
	var buf syncBuffer
	restore := swapLogger(t, &buf)
	defer restore()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) {
		panic("터졌다")
	})
	h := observe(reg, mux)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d — panic 이 500으로 안 나갔다", rec.Code)
	}
	if got := reg.HTTPRequests.SumFunc(func(l map[string]string) bool {
		return l["status"] == "500"
	}); got != 1 {
		t.Errorf("5xx 지표=%v", got)
	}
	if got := reg.HTTPPanics.Total(); got != 1 {
		t.Errorf("panic 지표=%v", got)
	}

	line := requestLine(t, &buf, "/boom")
	if line["level"] != "ERROR" {
		t.Errorf("level=%v — panic 은 ERROR 여야 한다", line["level"])
	}
	if s, _ := line["stack"].(string); !strings.Contains(s, "observe_test.go") {
		t.Errorf("스택이 안 남았다: %v", line["stack"])
	}
}

// http.ErrAbortHandler 는 net/http 의 규약이라 그대로 올려보낸다. 응답을 일부러
// 끊는 신호인데 우리가 500으로 바꿔 쓰면 그 약속이 깨진다.
func TestAbortHandlerStaysAbort(t *testing.T) {
	reg := metrics.New("api", "test")
	restore := swapLogger(t, &syncBuffer{})
	defer restore()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /abort", func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	})
	h := observe(reg, mux)

	defer func() {
		if p := recover(); p != http.ErrAbortHandler {
			t.Fatalf("recover()=%v — ErrAbortHandler 를 삼켰다", p)
		}
		// 삼키지 않아도 지표에는 남아야 한다.
		if got := reg.HTTPPanics.Total(); got != 1 {
			t.Errorf("panic 지표=%v", got)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/abort", nil))
}

// syncBuffer 는 잠금을 두른 로그 통이다.
//
// 통 자체가 잠금을 들어야 한다. swapLogger 가 바꾸는 것은 전역 로거라, 앞 테스트에서
// 늦게 끝나는 핸들러가 지금 테스트의 통에 쓴다 — WebSocket 은 하이재킹된 연결이라
// httptest.Server.Close 가 그 핸들러를 안 기다린다(requestLine 이 경로로 고르는 이유).
// 경로로 고르는 것은 남의 줄을 안 읽게 하는 장치이고, 쓰기와 읽기가 겹치는 것은 그대로 남는다.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// Bytes 는 사본을 준다. 속의 슬라이스를 그대로 주면 잠금 밖에서 읽게 된다.
func (b *syncBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return bytes.Clone(b.buf.Bytes())
}

func (b *syncBuffer) String() string { return string(b.Bytes()) }

// requestLine 은 buf 에서 그 경로의 요청 줄을 꺼낸다.
//
// 버퍼에 줄이 하나뿐이라고 보면 안 된다. swapLogger 가 바꾸는 것은 전역 로거이고,
// WebSocket 은 하이재킹된 연결이라 httptest.Server.Close 가 그 핸들러를 안 기다린다 —
// 앞 테스트의 /ws/match 핸들러가 늦게 끝나며 이 버퍼에 쓴다. 경로로 고르면 무관해진다.
func requestLine(t *testing.T, buf *syncBuffer, path string) map[string]any {
	t.Helper()
	for _, raw := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var line map[string]any
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatalf("로그가 JSON 이 아니다: %v\n%s", err, raw)
		}
		if line["path"] == path {
			return line
		}
	}
	t.Fatalf("%s 의 요청 줄이 없다:\n%s", path, buf.String())
	return nil
}

// swapLogger 는 기본 로거를 buf 로 돌린다. 돌려주는 함수로 되돌린다.
func swapLogger(t *testing.T, buf *syncBuffer) func() {
	t.Helper()
	before := slog.Default()
	slog.SetDefault(slog.New(LogHandler(slog.NewJSONHandler(buf, nil))))
	return func() { slog.SetDefault(before) }
}

// 사람이 먼저 끊은 요청은 5xx 로 세지 않는다. 검토는 탐색이 몇 초라 「눌러 놓고 다른
// 화면으로 가는」 것이 흔하고(그때 503이 나간다), 그것을 세면 알람이 정상 사용에 울린다.
func TestClientGoneIsNotFiveHundred(t *testing.T) {
	reg := metrics.New("api", "test")
	restore := swapLogger(t, &syncBuffer{})
	defer restore()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		// 검토·가정 수순의 default 갈래가 하는 그대로다 — ctx 가 죽으면 503.
		<-r.Context().Done()
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "engine_unavailable"})
	})
	h := observe(reg, mux)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/slow", nil).WithContext(ctx)
	cancel()
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got := reg.HTTPRequests.SumFunc(func(l map[string]string) bool {
		return l["status"] == "499"
	}); got != 1 {
		t.Fatalf("499 지표=%v — 끊긴 요청이 5xx 로 세어졌다", got)
	}
	if got := reg.HTTPRequests.SumFunc(func(l map[string]string) bool {
		return strings.HasPrefix(l["status"], "5")
	}); got != 0 {
		t.Fatalf("5xx 지표=%v", got)
	}
}

// 끊기지 않은 5xx 는 그대로 5xx 다. 위 완화가 진짜 고장까지 덮으면 알람이 무의미해진다.
func TestRealFiveHundredStillCounts(t *testing.T) {
	reg := metrics.New("api", "test")
	restore := swapLogger(t, &syncBuffer{})
	defer restore()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /broken", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "engine_unavailable"})
	})
	h := observe(reg, mux)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/broken", nil))

	if got := reg.HTTPRequests.SumFunc(func(l map[string]string) bool {
		return l["status"] == "503"
	}); got != 1 {
		t.Fatalf("503 지표=%v", got)
	}
}
