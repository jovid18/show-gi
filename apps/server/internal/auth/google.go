package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Provider 는 users.provider 에 적히는 값이다. 지금은 하나뿐이지만 칸이 이미 있다.
const Provider = "google"

// 엔드포인트는 상수로 둔다. Google의 discovery 문서를 매번 읽어 오는 쪽이 정석이지만,
// 그건 기동할 때마다 외부 호출이 하나 더 생기고 그 호출이 실패하면 로그인이 통째로
// 꺼진다 — 이 주소들은 10년 넘게 그대로다.
const (
	authEndpoint  = "https://accounts.google.com/o/oauth2/v2/auth"
	tokenEndpoint = "https://oauth2.googleapis.com/token"
)

// exchangeTimeout 은 토큰 교환에 주는 시간이다. 사람이 리디렉션 화면에서 기다리는
// 중이라 길게 잡을 수 없다.
const exchangeTimeout = 10 * time.Second

// Google 은 OAuth 2.0 authorization code 흐름의 우리 쪽이다.
//
// 암묵 흐름(id_token 을 브라우저로 직접 받는 것)을 쓰지 않는다. 그쪽은 토큰이
// 주소창과 브라우저 기록에 남고, 우리는 어차피 서버가 세션 쿠키를 발급해야 한다.
type Google struct {
	clientID     string
	clientSecret string
	http         *http.Client
}

// NewGoogle 은 클라이언트를 만든다. 둘 중 하나라도 비면 nil이다 — 그러면 로그인
// 표면이 통째로 꺼지고 익명 대국으로 남는다. 엔진·DB가 없을 때와 같은 판단이다.
//
// 자리표시자 unset 도 빈 것으로 본다. SSM이 빈 문자열을 거부해서 아직 발급되지
// 않은 키가 그 값으로 들어가 있고(06-status.md §3), 그대로 켜면 로그인 버튼이
// 뜬 뒤 Google이 400을 준다.
func NewGoogle(clientID, clientSecret string) *Google {
	if isUnset(clientID) || isUnset(clientSecret) {
		return nil
	}
	return &Google{
		clientID:     clientID,
		clientSecret: clientSecret,
		http:         &http.Client{Timeout: exchangeTimeout},
	}
}

func isUnset(v string) bool { return v == "" || v == "unset" }

// AuthURL 은 사람을 보낼 Google 주소다.
//
// state 는 CSRF 막이다. 같은 값을 쿠키에도 심어 두고 콜백에서 맞춰 본다 —
// 남이 자기 계정의 콜백 URL을 우리 사용자에게 열게 해서 남의 계정으로 로그인시키는
// 공격이 이것으로 막힌다.
func (g *Google) AuthURL(redirectURI, state string) string {
	q := url.Values{
		"client_id":     {g.clientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {"openid email profile"},
		"state":         {state},
		// 새로고침 토큰이 필요 없다. 우리가 Google API를 대신 부르는 일이 없고,
		// 받아 두면 보관 의무만 생긴다.
		"access_type": {"online"},
		// 계정이 여럿인 사람이 어느 계정인지 고를 수 있어야 한다. 없으면 브라우저에
		// 로그인된 첫 계정으로 조용히 지나간다.
		"prompt": {"select_account"},
	}
	return authEndpoint + "?" + q.Encode()
}

// Identity 는 Google이 말해 주는 그 사람이다.
type Identity struct {
	// Sub 는 Google 안에서 이 계정을 가리키는 불변 식별자다. users.provider_uid 가
	// 이것이고, 이메일이 아니다 — 이메일은 바뀌고 재사용된다.
	Sub   string
	Name  string
	Email string
}

// DisplayName 은 화면에 쓸 이름이다. 셋 다 없는 계정이 실제로 있다.
func (id Identity) DisplayName() string {
	if id.Name != "" {
		return id.Name
	}
	if local, _, ok := strings.Cut(id.Email, "@"); ok && local != "" {
		return local
	}
	return "プレイヤー"
}

// Exchange 는 콜백으로 받은 code 를 사람으로 바꾼다.
func (g *Google) Exchange(ctx context.Context, code, redirectURI string) (Identity, error) {
	form := url.Values{
		"code":          {code},
		"client_id":     {g.clientID},
		"client_secret": {g.clientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Identity{}, fmt.Errorf("auth: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	res, err := g.http.Do(req)
	if err != nil {
		return Identity{}, fmt.Errorf("auth: token request: %w", err)
	}
	defer res.Body.Close()

	// 본문을 상한까지만 읽는다. 상대가 Google이라도 무제한으로 읽을 이유가 없다.
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return Identity{}, fmt.Errorf("auth: read token response: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		// 본문에 client_secret 은 안 들어간다. 대신 error·error_description 이 있어
		// 「redirect_uri 가 등록된 것과 다르다」 같은 설정 실수를 그대로 말해 준다.
		return Identity{}, fmt.Errorf("auth: token endpoint %d: %s", res.StatusCode, body)
	}

	var tok struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return Identity{}, fmt.Errorf("auth: decode token response: %w", err)
	}
	if tok.IDToken == "" {
		return Identity{}, fmt.Errorf("auth: token response has no id_token")
	}
	return g.identityFrom(tok.IDToken)
}

// identityFrom 은 ID 토큰의 본문을 읽는다.
//
// 서명을 검증하지 않는다. 이 토큰은 브라우저를 거치지 않고 우리가 방금 TLS로
// 직접 부른 Google의 토큰 엔드포인트에서 왔다 — 가운데에 낄 수 있는 것이 없으므로
// 검증할 것이 없고, Google 문서도 그 경우를 예외로 못 박아 둔다. 브라우저가 들고 온
// 토큰이었다면 이야기가 정반대다.
//
// 그래서 JWT 라이브러리를 끌어오지 않는다. 여기서 하는 일은 base64 한 번과 JSON
// 한 번이고, 대신 aud 만 확인한다 — 서명이 아니라 설정 실수를 잡는 자리다.
func (g *Google) identityFrom(idToken string) (Identity, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return Identity{}, fmt.Errorf("auth: id_token is not a JWT")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Identity{}, fmt.Errorf("auth: decode id_token: %w", err)
	}
	var claims struct {
		Sub   string `json:"sub"`
		Aud   string `json:"aud"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &claims); err != nil {
		return Identity{}, fmt.Errorf("auth: decode id_token claims: %w", err)
	}
	if claims.Sub == "" {
		return Identity{}, fmt.Errorf("auth: id_token has no sub")
	}
	if claims.Aud != g.clientID {
		return Identity{}, fmt.Errorf("auth: id_token was issued for another client")
	}
	return Identity{Sub: claims.Sub, Name: claims.Name, Email: claims.Email}, nil
}
