package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/auth"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// storePlaceholder 는 「DB가 있다」만 나타낸다. 아래 테스트는 질의에 닿지 않는다 —
// 닿는 경로(콜백의 성공)는 진짜 DB가 필요해 internal/store 쪽에서 잡는다.
var storePlaceholder store.Store

// 로그인이 꺼진 배포에서도 /api/me 는 있어야 한다. 404면 화면이 「로그인이 없는
// 배포」와 「서버가 고장난 것」을 구별할 수 없다.
func TestMeWithoutSignIn(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler(Options{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/me", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body struct {
		Enabled bool `json:"enabled"`
		User    any  `json:"user"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Enabled {
		t.Error("enabled = true without a Google client")
	}
	if body.User != nil {
		t.Errorf("user = %v, want null", body.User)
	}
}

// 키가 없으면 로그인 경로 자체가 없다. 있으면 눌렀을 때 Google이 400을 준다.
func TestSignInRoutesAreAbsentWithoutKeys(t *testing.T) {
	h := Handler(Options{})
	for _, c := range [][2]string{
		{http.MethodGet, "/api/auth/google/start"},
		{http.MethodGet, callbackPath},
		{http.MethodPost, "/api/auth/logout"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(c[0], c[1], nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want %d", c[0], c[1], rec.Code, http.StatusNotFound)
		}
	}
}

// Store 가 없으면 로그인해도 그 판이 익명으로 남는다. 그러면 버튼이 아무 일도
// 안 하는 버튼이 되므로 표면을 아예 열지 않는다.
func TestSignInNeedsStore(t *testing.T) {
	h := &authHandler{google: auth.NewGoogle("id", "secret"), codec: auth.NewCodec("secret")}
	if h.enabled() {
		t.Error("enabled() = true without a store")
	}
}

// 서명 키가 없으면 쿠키를 위조할 수 있다. 클라이언트가 있어도 열지 않는다.
func TestSignInNeedsSessionSecret(t *testing.T) {
	h := &authHandler{google: auth.NewGoogle("id", "secret"), codec: auth.NewCodec("")}
	if h.enabled() {
		t.Error("enabled() = true without a session secret")
	}
}

func TestStartRedirectsToGoogle(t *testing.T) {
	h := signedInHandler()
	rec := httptest.NewRecorder()
	h.start(rec, httptest.NewRequest(http.MethodGet, "http://localhost:5173/api/auth/google/start", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusFound)
	}
	u, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if u.Host != "accounts.google.com" {
		t.Errorf("host = %q, want accounts.google.com", u.Host)
	}

	// state 는 쿠키와 주소 양쪽에 있어야 짝을 맞출 수 있다.
	state := cookieOf(rec, stateCookie)
	if state == nil {
		t.Fatal("no state cookie")
	}
	if state.Value == "" || state.Value != u.Query().Get("state") {
		t.Errorf("state cookie %q != query %q", state.Value, u.Query().Get("state"))
	}
	if !state.HttpOnly {
		t.Error("the state cookie is readable from JavaScript")
	}
	// 콜백은 accounts.google.com 에서 오는 최상위 이동이다. Strict 면 그때 안 실린다.
	if state.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", state.SameSite)
	}
}

// state 가 안 맞는 콜백은 우리가 시작하지 않은 것이다 — 남의 계정으로 로그인시키는
// 공격이 정확히 이 모양으로 온다. 세션을 굽지 않고 조용히 화면으로 돌려보낸다.
func TestCallbackRejectsStateMismatch(t *testing.T) {
	h := signedInHandler()

	for _, c := range []struct {
		name   string
		cookie string
		query  string
	}{
		{"쿠키 없음", "", "abc"},
		{"주소에 없음", "abc", ""},
		{"다른 값", "abc", "xyz"},
		{"둘 다 빈 값", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://localhost:5173"+callbackPath+"?code=x&state="+c.query, nil)
			if c.cookie != "" {
				r.AddCookie(&http.Cookie{Name: stateCookie, Value: c.cookie})
			}
			rec := httptest.NewRecorder()
			h.callback(rec, r)

			if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
				t.Errorf("status %d → %q, want 302 → /", rec.Code, rec.Header().Get("Location"))
			}
			if c := cookieOf(rec, sessionCookie); c != nil && c.Value != "" {
				t.Errorf("a session cookie was issued: %q", c.Value)
			}
		})
	}
}

// 로그아웃은 쿠키를 지운다. 서버에는 지울 것이 없다.
func TestLogoutClearsCookie(t *testing.T) {
	h := signedInHandler()
	rec := httptest.NewRecorder()
	h.logout(rec, httptest.NewRequest(http.MethodPost, "http://localhost:5173/api/auth/logout", nil))

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	c := cookieOf(rec, sessionCookie)
	if c == nil || c.MaxAge >= 0 {
		t.Errorf("session cookie = %+v, want MaxAge < 0", c)
	}
}

func TestViewerReadsSignedCookie(t *testing.T) {
	h := signedInHandler()
	value, err := h.codec.Encode(7, "さとし", time.Now())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: value})
	s, ok := h.viewer(r)
	if !ok || s.UserID != 7 || s.Name != "さとし" {
		t.Fatalf("viewer = %+v, %v", s, ok)
	}

	// 한 글자만 바꿔도 서명이 깨진다.
	r2 := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	r2.AddCookie(&http.Cookie{Name: sessionCookie, Value: value + "x"})
	if _, ok := h.viewer(r2); ok {
		t.Error("a tampered cookie was accepted")
	}
}

// 로그인이 꺼진 배포에서는 쿠키가 있어도 사람이 없다. 켜고 끄는 것이 한 자리여야
// 「어떤 경로만 예전 쿠키를 본다」가 안 생긴다.
func TestViewerIgnoresCookieWhenDisabled(t *testing.T) {
	value, err := auth.NewCodec("session-secret").Encode(7, "さとし", time.Now())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: value})

	h := &authHandler{codec: auth.NewCodec("session-secret")} // google·store 없음
	if _, ok := h.viewer(r); ok {
		t.Error("a viewer was returned while sign-in is off")
	}
}

// 프로덕션은 ALB → Caddy 두 겹이고 Caddy가 X-Forwarded-Proto 를 자기 것(평문)으로
// 덮는다. 그걸 믿으면 redirect_uri 가 http:// 가 되어 Google이 거부하고, 쿠키에서
// Secure 가 빠진다.
func TestOriginIsHTTPSOutsideLocalhost(t *testing.T) {
	h := signedInHandler()
	for _, c := range []struct{ host, want string }{
		{"show-gi.com", "https://show-gi.com"},
		{"localhost:5173", "http://localhost:5173"},
		{"127.0.0.1:8080", "http://127.0.0.1:8080"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		r.Host = c.host
		// X-Forwarded-Proto 는 안 본다. 있어도 결과가 같아야 한다.
		r.Header.Set("X-Forwarded-Proto", "http")
		if got := h.origin(r); got != c.want {
			t.Errorf("origin(%q) = %q, want %q", c.host, got, c.want)
		}
		wantSecure := strings.HasPrefix(c.want, "https://")
		if h.secure(r) != wantSecure {
			t.Errorf("secure(%q) = %v, want %v", c.host, h.secure(r), wantSecure)
		}
	}
}

func TestPublicOriginWins(t *testing.T) {
	h := signedInHandler()
	h.publicOrigin = "https://staging.show-gi.com/"

	r := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	r.Host = "localhost:5173"
	if got := h.redirectURI(r); got != "https://staging.show-gi.com"+callbackPath {
		t.Errorf("redirectURI = %q", got)
	}
}

// signedInHandler 는 켜진 로그인 표면이다. store 는 nil이 아니기만 하면 되는
// 자리라 콜백의 성공 경로는 여기서 안 탄다(DB가 필요하다).
func signedInHandler() *authHandler {
	return &authHandler{
		google: auth.NewGoogle("client-id", "client-secret"),
		codec:  auth.NewCodec("session-secret"),
		store:  &storePlaceholder,
	}
}

func cookieOf(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// owner 는 되짚기·가정 수순이 둘 다 쓰는 자리다. 여기가 틀리면 두 표면이 같이 샌다.
func TestOwnerFollowsTheSession(t *testing.T) {
	h := signedInHandler()

	if got := h.owner(httptest.NewRequest(http.MethodGet, "/api/games", nil)); got != nil {
		t.Errorf("로그인 안 한 요청의 owner = %v, want nil (익명 판)", *got)
	}

	value, err := h.codec.Encode(7, "さとし", time.Now())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/games", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: value})
	got := h.owner(r)
	if got == nil || *got != 7 {
		t.Errorf("owner = %v, want 7", got)
	}

	// 위조된 쿠키는 익명으로 떨어진다 — 남의 판이 아니라 익명 판을 본다.
	r2 := httptest.NewRequest(http.MethodGet, "/api/games", nil)
	r2.AddCookie(&http.Cookie{Name: sessionCookie, Value: value + "x"})
	if got := h.owner(r2); got != nil {
		t.Errorf("위조 쿠키의 owner = %v, want nil", *got)
	}
}

// 로그인이 없는 배포에서도 되짚기는 돌아야 한다. nil 핸들러에서 죽으면 그 순간
// /api/games 가 500이 된다.
func TestOwnerOnNilHandler(t *testing.T) {
	var h *authHandler
	if got := h.owner(httptest.NewRequest(http.MethodGet, "/api/games", nil)); got != nil {
		t.Errorf("owner = %v, want nil", *got)
	}
}
