package server

import (
	"crypto/rand"
	"encoding/base64"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/auth"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 로그인 표면. 흐름은 셋뿐이다 — Google로 보내고, 돌아온 것을 받아 쿠키를 굽고, 지운다.
//
// **로그인은 대국의 전제가 아니다.** 키가 없어도, DB가 없어도, 사람이 로그인을 안 해도
// 지금처럼 익명으로 둘 수 있다(games.user_id 가 nullable인 이유, 06-status.md §18).
// 로그인이 바꾸는 것은 그 판이 **누구의 것으로 남느냐** 하나다.

const (
	// sessionCookie 는 로그인 상태다. 서명돼 있고 서버에 짝이 없다(internal/auth).
	sessionCookie = "showgi_session"

	// stateCookie 는 Google 왕복 한 번에만 산다. CSRF 막이라 콜백에서 지운다.
	stateCookie = "showgi_oauth_state"

	// stateTTL 은 사람이 Google 화면에서 계정을 고르는 데 주는 시간이다.
	stateTTL = 10 * time.Minute

	// callbackPath 는 Google에 등록한 리디렉션 경로다. **`/api` 아래인 것이 중요하다** —
	// Caddy와 Vite 둘 다 그 접두사만 서버로 넘긴다(apps/web/Caddyfile).
	callbackPath = "/api/auth/google/callback"
)

// authHandler 는 로그인 세 경로를 갖는다. google 이나 codec 이 nil이면 라우팅되지 않는다.
type authHandler struct {
	google *auth.Google
	codec  *auth.Codec
	store  *store.Store
	// publicOrigin 이 비면 요청에서 되짚는다(originOf).
	publicOrigin string
}

// enabled 는 로그인 표면을 열 수 있는지다. 셋이 다 있어야 한다 —
// **store 까지 필요하다.** 사용자를 남길 곳이 없으면 로그인해도 그 판이 익명으로
// 남고, 그러면 로그인 버튼이 아무것도 안 하는 버튼이 된다.
func (h *authHandler) enabled() bool {
	return h != nil && h.google != nil && h.codec != nil && h.store != nil
}

// start 는 Google로 보낸다.
func (h *authHandler) start(w http.ResponseWriter, r *http.Request) {
	state, err := randomState()
	if err != nil {
		// 난수가 안 나오는 것은 프로세스가 이상한 것이다. 로그인만 접는다.
		log.Printf("auth: cannot make state: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "internal", "message": "ログインを開始できませんでした。",
		})
		return
	}
	secure := h.secure(r)
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookie,
		Value:    state,
		Path:     "/",
		MaxAge:   int(stateTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		// **Lax 여야 한다.** 콜백은 accounts.google.com 에서 오는 최상위 GET 이동이라
		// Strict 로 두면 그때 쿠키가 안 실려 로그인이 매번 실패한다.
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, h.google.AuthURL(h.redirectURI(r), state), http.StatusFound)
}

// callback 은 Google이 돌려보낸 사람을 받는다.
//
// **실패해도 화면으로 돌려보낸다.** 여기는 브라우저가 주소창으로 들어오는 자리라
// JSON을 쓰면 사람이 그 JSON을 보게 된다. 무엇이 틀렸는지는 로그가 갖는다.
func (h *authHandler) callback(w http.ResponseWriter, r *http.Request) {
	clearCookie(w, stateCookie, h.secure(r))

	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		// 사람이 동의 화면에서 취소한 경우가 대부분이다. 그대로 돌려보낸다.
		log.Printf("auth: google returned %q", e)
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	state, err := r.Cookie(stateCookie)
	if err != nil || state.Value == "" || state.Value != q.Get("state") {
		// 우리가 시작하지 않은 콜백이다. 남이 자기 계정으로 로그인시키려는 시도가
		// 정확히 이 모양으로 온다.
		log.Print("auth: state mismatch, ignoring callback")
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	id, err := h.google.Exchange(r.Context(), q.Get("code"), h.redirectURI(r))
	if err != nil {
		log.Printf("auth: %v", err)
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	userID, err := h.store.UpsertUser(r.Context(), auth.Provider, id.Sub, id.DisplayName())
	if err != nil {
		log.Printf("auth: %v", err)
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	value, err := h.codec.Encode(userID, id.DisplayName(), time.Now())
	if err != nil {
		log.Printf("auth: %v", err)
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		MaxAge:   int(auth.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.secure(r),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

// logout 은 쿠키를 지운다. 서버에는 지울 것이 없다 — 그것이 서명 쿠키의 값이자 한계다.
func (h *authHandler) logout(w http.ResponseWriter, r *http.Request) {
	clearCookie(w, sessionCookie, h.secure(r))
	w.WriteHeader(http.StatusNoContent)
}

// viewer 는 지금 로그인한 사람이다. 없으면 두 번째 값이 false 다.
//
// **핸들러가 이 함수 하나로만 사람을 안다.** 쿠키 이름과 서명 검증이 여기 한 곳에
// 있어야 「어떤 경로는 만료를 안 본다」가 생기지 않는다.
func (h *authHandler) viewer(r *http.Request) (auth.Session, bool) {
	if !h.enabled() {
		return auth.Session{}, false
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return auth.Session{}, false
	}
	s, err := h.codec.Decode(c.Value, time.Now())
	if err != nil {
		return auth.Session{}, false
	}
	return s, true
}

// me 는 화면이 헤더를 그리려고 부르는 곳이다.
//
// enabled 를 같이 준다. 화면은 「로그인 안 함」과 「로그인이라는 것이 이 배포에
// 아예 없음」을 갈라 그려야 한다 — 키가 없는 환경에서 눌러도 안 되는 버튼을
// 띄우면 그게 곧 고장으로 보인다.
//
// **skill_profile 은 여기 싣지 않는다.** 실력 프로파일은 본인만 보는 민감 정보이고
// (02-architecture.md §7 위협 2), 이 응답은 화면이 늘 부르는 자리다.
func (h *authHandler) me(w http.ResponseWriter, r *http.Request) {
	body := map[string]any{"enabled": h.enabled(), "user": nil}
	if s, ok := h.viewer(r); ok {
		body["user"] = map[string]any{"id": s.UserID, "name": s.Name}
	}
	writeJSON(w, http.StatusOK, body)
}

// redirectURI 는 Google에 등록해 둔 그 주소여야 한다. 한 글자만 달라도 거부된다.
func (h *authHandler) redirectURI(r *http.Request) string {
	return h.origin(r) + callbackPath
}

// origin 은 브라우저가 이 서버를 부르는 주소다.
//
// PUBLIC_ORIGIN 이 있으면 그것이 정본이다. 없으면 요청에서 되짚는데, **스킴을
// X-Forwarded-Proto 로 정하지 않는다** — 앞단이 ALB → Caddy 두 겹이고 Caddy는
// 신뢰 설정 없이는 그 헤더를 자기 것(평문 http)으로 덮는다. 그러면 프로덕션에서
// `http://show-gi.com/...` 이 되어 Google이 redirect_uri 불일치로 거부한다.
//
// 대신 호스트로 정한다. 이 사이트는 배포된 모든 자리에서 HTTPS이고 평문인 것은
// 로컬뿐이라, 규칙이 「localhost 면 http, 아니면 https」로 끝난다.
func (h *authHandler) origin(r *http.Request) string {
	if h.publicOrigin != "" {
		return strings.TrimSuffix(h.publicOrigin, "/")
	}
	host := r.Host
	scheme := "https"
	if isLocalHost(host) {
		scheme = "http"
	}
	return scheme + "://" + host
}

// secure 는 쿠키에 Secure 를 붙일지다. origin 과 같은 판단을 쓴다 — 갈리면 로컬에서
// 로그인이 조용히 안 되거나(평문에 Secure 쿠키), 프로덕션에서 쿠키가 평문으로 샌다.
func (h *authHandler) secure(r *http.Request) bool {
	return strings.HasPrefix(h.origin(r), "https://")
}

func isLocalHost(host string) bool {
	name, _, found := strings.Cut(host, ":")
	if !found {
		name = host
	}
	return name == "localhost" || name == "127.0.0.1" || name == "[::1]" || name == "::1"
}

func clearCookie(w http.ResponseWriter, name string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
