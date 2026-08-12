package auth

import (
	"net/url"
	"testing"
)

// SSM이 빈 문자열을 거부해서 아직 발급 안 된 키가 `unset` 으로 들어가 있다
// (06-status.md §3). 그것을 값으로 보면 로그인 버튼이 뜬 뒤 Google이 400을 준다.
func TestNewGoogleTreatsUnsetAsMissing(t *testing.T) {
	for _, c := range [][2]string{
		{"", ""},
		{"id", ""},
		{"", "secret"},
		{"unset", "unset"},
		{"id", "unset"},
		{"unset", "secret"},
	} {
		if NewGoogle(c[0], c[1]) != nil {
			t.Errorf("NewGoogle(%q, %q) is not nil", c[0], c[1])
		}
	}
	if NewGoogle("id", "secret") == nil {
		t.Error("NewGoogle with both values is nil")
	}
}

func TestAuthURL(t *testing.T) {
	g := NewGoogle("client-id", "secret")
	raw := g.AuthURL("https://show-gi.com/api/auth/google/callback", "state-value")

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	want := map[string]string{
		"client_id":     "client-id",
		"redirect_uri":  "https://show-gi.com/api/auth/google/callback",
		"response_type": "code",
		"state":         "state-value",
		"scope":         "openid email profile",
	}
	for k, v := range want {
		if got := q.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	// 시크릿은 사람이 보는 주소로 나가지 않는다.
	if q.Get("client_secret") != "" {
		t.Error("the auth URL carries the client secret")
	}
}

func TestDisplayName(t *testing.T) {
	for _, c := range []struct {
		id   Identity
		want string
	}{
		{Identity{Name: "さとし", Email: "satoshi@example.com"}, "さとし"},
		{Identity{Email: "satoshi@example.com"}, "satoshi"},
		{Identity{}, "プレイヤー"},
		{Identity{Email: "@example.com"}, "プレイヤー"},
	} {
		if got := c.id.DisplayName(); got != c.want {
			t.Errorf("DisplayName(%+v) = %q, want %q", c.id, got, c.want)
		}
	}
}

// aud 가 남의 클라이언트인 토큰을 받으면, 그쪽 앱에서 발급된 토큰으로 우리 계정에
// 들어올 수 있다. 우리 흐름에서는 도달하지 않지만 설정 실수는 이 모양으로 온다.
func TestIdentityFromChecksAudience(t *testing.T) {
	g := NewGoogle("client-id", "secret")

	if _, err := g.identityFrom(idToken(t, `{"sub":"1","aud":"someone-else"}`)); err == nil {
		t.Error("an id_token for another client was accepted")
	}
	if _, err := g.identityFrom(idToken(t, `{"aud":"client-id"}`)); err == nil {
		t.Error("an id_token without sub was accepted")
	}
	id, err := g.identityFrom(idToken(t, `{"sub":"1","aud":"client-id","name":"さとし"}`))
	if err != nil {
		t.Fatalf("identityFrom: %v", err)
	}
	if id.Sub != "1" || id.Name != "さとし" {
		t.Errorf("identity = %+v", id)
	}
}

func TestIdentityFromRejectsNonJWT(t *testing.T) {
	g := NewGoogle("client-id", "secret")
	for _, v := range []string{"", "a.b", "a.b.c.d", "header.!!!.sig"} {
		if _, err := g.identityFrom(v); err == nil {
			t.Errorf("identityFrom(%q) was accepted", v)
		}
	}
}

// idToken 은 본문만 진짜인 JWT를 만든다. 서명을 안 보므로 그것으로 충분하다.
func idToken(t *testing.T, claims string) string {
	t.Helper()
	return "header." + base64url(claims) + ".signature"
}

func base64url(s string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	var out []byte
	for i := 0; i < len(s); i += 3 {
		var buf [3]byte
		n := copy(buf[:], s[i:])
		v := uint32(buf[0])<<16 | uint32(buf[1])<<8 | uint32(buf[2])
		chunk := []byte{
			alphabet[v>>18&0x3f], alphabet[v>>12&0x3f],
			alphabet[v>>6&0x3f], alphabet[v&0x3f],
		}
		out = append(out, chunk[:n+1]...)
	}
	return string(out)
}
