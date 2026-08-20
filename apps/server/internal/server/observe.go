package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/metrics"
)

// requestIDHeader 는 요청 하나를 로그 여러 줄과 잇는 이름이다. 응답에도 같이 실어
// 보내므로 사람이 「이 화면이 이상하다」와 함께 그 값을 가져올 수 있다.
const requestIDHeader = "X-Request-Id"

// maxRequestIDLen 은 밖에서 온 요청 ID 를 받아 줄 길이다. 우리가 만드는 것은 16자다.
const maxRequestIDLen = 64

// routeOther 는 라우팅에 안 걸린 요청의 route 라벨이다.
//
// 안 걸린 경로를 그대로 라벨로 쓰면 스캐너 하나가 계열을 무한히 늘리고, 그 메모리는
// 프로세스가 사는 동안 안 돌아온다.
const routeOther = "other"

// statusClientGone 은 부르는 쪽이 먼저 끊은 요청의 status 라벨이다. nginx 의 499 를 쓴다 —
// HTTP 표준에 없는 값이지만 「서버가 깨진 것이 아니다」를 5xx 와 가르는 자리가 필요하다.
const statusClientGone = "499"

// ctxKey 는 이 패키지가 ctx 에 넣는 값의 키 타입이다.
type ctxKey struct{}

// requestIDKey 는 ctx 에 실린 요청 ID 의 키다.
var requestIDKey = ctxKey{}

// RequestIDOf 는 ctx 에 실린 요청 ID 다. 없으면 빈 문자열이다.
func RequestIDOf(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// LogHandler 는 ctx 에 실린 요청 ID 를 모든 로그 줄에 붙인다.
//
// 핸들러가 slog 의 ...Context 함수를 쓰면 request_id 를 직접 넘기지 않아도 붙는다 —
// 넘기게 두면 어느 줄에서든 빠뜨릴 수 있고, 빠진 줄은 요청 하나를 되짚을 때 바로 그
// 없는 줄이 된다.
func LogHandler(inner slog.Handler) slog.Handler { return logHandler{inner} }

type logHandler struct{ slog.Handler }

func (h logHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestIDOf(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h logHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return logHandler{h.Handler.WithAttrs(attrs)}
}

func (h logHandler) WithGroup(name string) slog.Handler {
	return logHandler{h.Handler.WithGroup(name)}
}

// observe 는 요청 하나를 로그와 지표로 남긴다. reg 가 nil 이면 로그만 남는다.
//
// mux 를 안쪽에 두는 것이 조건이다. route 라벨로 쓰는 r.Pattern 은 ServeMux 가 요청에
// 직접 채우므로, 감싸는 쪽에서 읽으려면 mux.ServeHTTP 가 돌아온 뒤여야 한다.
func observe(reg *metrics.Registry, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := requestID()
		w.Header().Set(requestIDHeader, id)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey, id))

		rec := &recorder{ResponseWriter: w}
		start := time.Now()

		// 요청 한 줄을 defer 로 남긴다. 핸들러가 panic 하면 net/http 가 연결만 끊는데,
		// 그러면 상태 코드도 로그 줄도 지표도 안 남는다 — 가장 흔한 장애가 지표에서
		// 안 보이고 5xx 알람이 영원히 조용하다.
		defer func() {
			p := recover()
			if p != nil && p != http.ErrAbortHandler {
				// 아직 아무것도 안 썼으면 500을 준다. 업그레이드된 연결(101)에는
				// 쓸 수 없으므로 그때는 상태만 남기고 지나간다.
				if rec.code == 0 {
					rec.WriteHeader(http.StatusInternalServerError)
				}
			}
			observed(reg, r, rec, time.Since(start), p)
			if p == http.ErrAbortHandler {
				// net/http 의 규약이다. 핸들러가 응답을 일부러 끊는 신호라 그대로 올려보낸다.
				panic(p)
			}
		}()

		next.ServeHTTP(rec, r)
	})
}

// observed 는 요청 하나를 지표와 로그에 남긴다. p 가 nil 이 아니면 panic 한 요청이다.
func observed(reg *metrics.Registry, r *http.Request, rec *recorder, took time.Duration, p any) {
	route := r.Pattern
	if route == "" {
		route = routeOther
	}
	status := rec.status()

	// 사람이 먼저 끊은 요청은 5xx 로 세지 않는다.
	//
	// 검토·가정 수순은 엔진을 기다리는 동안 요청 ctx 가 죽으면 그 에러를 503으로 답한다
	// (explore.go·whatif.go 의 default 갈래). 탐색이 몇 초라 「눌러 놓고 다른 화면으로
	// 가는」 것이 흔하고, 그것을 5xx 로 세면 알람이 정상 사용에 울린다. 499는 nginx 가
	// 쓰는 그 뜻이다 — 서버가 깨진 것이 아니라 부르는 쪽이 없어졌다.
	label := strconv.Itoa(status)
	canceled := status >= http.StatusInternalServerError && r.Context().Err() != nil
	if canceled {
		label = statusClientGone
	}
	reg.ObserveHTTP(route, label, took)

	attrs := []any{
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("route", route),
		slog.Int("status", status),
		slog.Duration("took", took),
	}
	if canceled {
		// 나간 상태 코드는 그대로 남긴다. 지표의 라벨과 갈리는 자리라 로그가 그 둘을
		// 다 들고 있어야 「왜 499로 세어졌나」를 되짚을 수 있다.
		attrs = append(attrs, slog.Bool("client_gone", true))
	}
	// ALB 가 붙이는 추적 ID. 있으면 같이 남긴다 — 우리 로그와 ALB 로그를 잇는
	// 유일한 값이고, 없는 환경(로컬·테스트)에서는 그냥 없다.
	if trace := r.Header.Get("X-Amzn-Trace-Id"); trace != "" {
		attrs = append(attrs, slog.String("trace_id", trace))
	}
	// 밖에서 온 ID. 우리 것을 대신하지 않고 한 필드로만 남는다.
	if given := clientRequestID(r); given != "" {
		attrs = append(attrs, slog.String("client_request_id", given))
	}

	if p != nil {
		reg.ObservePanic(route)
		// 스택을 같이 남긴다. panic 은 로그 한 줄로는 어디서 났는지 알 수 없고,
		// 여기서 안 남기면 net/http 가 자기 로거로 찍어 급이 INFO 가 된다.
		attrs = append(attrs, slog.Any("panic", p), slog.String("stack", string(debug.Stack())))
		slog.ErrorContext(r.Context(), "request panicked", attrs...)
		return
	}

	slog.Log(r.Context(), levelFor(r, status, canceled), "request", attrs...)
}

// levelFor 는 요청 한 줄을 어느 급으로 남길지 정한다.
//
// /healthz 가 Debug 인 것은 ALB 헬스체크가 몇 초마다 오기 때문이다. Info 로 두면
// 로그의 대부분이 그것이 되고, 그 로그가 곧 요금이다.
func levelFor(r *http.Request, status int, canceled bool) slog.Level {
	switch {
	case canceled:
		// 사람이 화면을 떠난 것이라 우리 잘못이 아니다. Error 로 남기면 로그를 급으로
		// 훑을 때 진짜 고장에 섞인다.
		return slog.LevelInfo
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case r.URL.Path == "/healthz":
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}

// requestID 는 이 요청의 ID 다. 언제나 우리가 만든다.
//
// 밖에서 온 값을 채택하지 않는다. Caddy 가 이 헤더를 세우지도 지우지도 않으므로
// (apps/web/Caddyfile) 누구나 같은 값을 계속 보낼 수 있고, 그러면 request_id 가
// 요청 하나를 가리키지 못한다 — 장애를 되짚어야 하는 바로 그때. 앞단이 이 헤더를
// 실제로 소유하는 날 여기를 바꾼다. 온 값은 버리지 않고 따로 남긴다(clientRequestID).
func requestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 난수가 없어도 요청은 처리한다. ID 가 없는 것이 요청이 안 되는 것보다 싸다.
		return ""
	}
	return hex.EncodeToString(b[:])
}

// clientRequestID 는 밖에서 온 요청 ID 다. 쓸 만하지 않으면 빈 문자열이다.
//
// 우리 ID 를 대신하지 않는다. 로그에 한 필드로만 실려서, 앞단이나 스크립트가 자기
// ID 로 요청을 되짚을 수 있게만 해 준다.
func clientRequestID(r *http.Request) string {
	given := r.Header.Get(requestIDHeader)
	if !safeRequestID(given) {
		return ""
	}
	return given
}

// safeRequestID 는 밖에서 온 ID 를 그대로 로그에 실어도 되는지 본다.
//
// 글자를 제한하는 것은 JSON 이 깨지는 것과는 무관하다(그건 인코더가 막는다) —
// 로그를 보는 사람이 값의 끝을 알 수 있어야 하고, 길이가 무제한이면 한 요청이
// 로그 한 줄을 통째로 차지할 수 있다.
func safeRequestID(v string) bool {
	if v == "" || len(v) > maxRequestIDLen {
		return false
	}
	for _, c := range v {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

// recorder 는 응답 상태를 엿본다.
//
// Unwrap 이 필수다. WebSocket 업그레이드는 감싼 ResponseWriter 를 이것으로 되짚어
// Hijacker 를 찾으므로(coder/websocket 의 hijacker), 없으면 대국이 통째로 안 열린다.
type recorder struct {
	http.ResponseWriter
	code int
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *recorder) WriteHeader(code int) {
	if r.code == 0 {
		r.code = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// status 는 실제로 나간 상태 코드다. 핸들러가 아무것도 안 썼으면 200이다(net/http 규약).
func (r *recorder) status() int {
	if r.code == 0 {
		return http.StatusOK
	}
	return r.code
}
