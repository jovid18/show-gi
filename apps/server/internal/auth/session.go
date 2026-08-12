// Package auth 는 로그인한 사람이 누구인지를 정한다.
//
// **세션을 DB에 두지 않는다.** 담는 것이 user_id·이름·만료 셋뿐이고, 서버가 세션을
// 골라 끊어야 하는 제품이 아니다. 대신 쿠키 자체를 HMAC로 서명한다 — 표가 없으므로
// 마이그레이션도, 로그인마다의 쓰기도, 만료된 행을 치우는 일도 없다.
//
// 그래서 이 패키지는 HTTP 핸들러를 갖지 않는다. 표면은 internal/server 가 갖고
// (auth.go), 여기 있는 것은 **서명과 Google 왕복** 둘뿐이다.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SessionTTL 은 로그인이 유지되는 기간이다.
//
// 서명 쿠키는 **발급한 뒤에 끊을 수 없다.** 그래서 이 값이 곧 「기기를 잃어버렸을 때
// 남의 손에 로그인이 살아 있는 시간」이고, 짧게 잡을 이유가 그것뿐이다. 30일은
// 연습 대국 앱에서 다시 로그인하라고 하지 않을 만큼 길고, 그 위험이 학습 기록에
// 한정되므로 감당할 만큼 짧다.
const SessionTTL = 30 * 24 * time.Hour

// ErrNoSession 은 쿠키가 없거나, 서명이 안 맞거나, 만료됐을 때다.
//
// **셋을 구별해서 돌려주지 않는다.** 부르는 쪽이 할 일이 어느 경우든 「로그인 안 한
// 사람」으로 같고, 구별해서 화면에 말하면 서명 위조를 시도하는 쪽에 힌트가 된다.
var ErrNoSession = errors.New("auth: no valid session")

// Session 은 쿠키에 담기는 전부다.
//
// 이름을 같이 담는 이유는 **화면 한 줄 때문에 매 요청이 users 를 읽지 않게** 하려는
// 것이다. 서명돼 있으므로 위조되지 않고, 낡을 수 있는 것은 사용자가 Google 쪽에서
// 이름을 바꿨을 때뿐인데 그때는 다음 로그인에 따라온다.
type Session struct {
	UserID  int64  `json:"uid"`
	Name    string `json:"name"`
	Expires int64  `json:"exp"` // Unix 초
}

// Codec 은 세션을 쿠키 값으로 옮긴다. 키가 비면 nil이고, 그러면 로그인이 꺼진다.
type Codec struct{ key []byte }

// NewCodec 은 서명 키를 받는다. 키가 비어 있으면 nil을 돌려준다 —
// **서명 없는 세션을 만드느니 로그인을 끄는 쪽이다.** 서명이 없으면 쿠키 한 줄로
// 아무 user_id 나 될 수 있고, 그건 로그인이 없는 것보다 나쁘다.
func NewCodec(secret string) *Codec {
	if secret == "" {
		return nil
	}
	return &Codec{key: []byte(secret)}
}

// Encode 는 서명한 쿠키 값을 만든다. 만료는 지금부터 SessionTTL 뒤다.
func (c *Codec) Encode(userID int64, name string, now time.Time) (string, error) {
	body, err := json.Marshal(Session{
		UserID:  userID,
		Name:    name,
		Expires: now.Add(SessionTTL).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("auth: encode session: %w", err)
	}
	payload := base64.RawURLEncoding.EncodeToString(body)
	return payload + "." + c.sign(payload), nil
}

// Decode 는 쿠키 값을 되읽는다. 서명이 안 맞거나 만료됐으면 ErrNoSession 이다.
func (c *Codec) Decode(value string, now time.Time) (Session, error) {
	payload, sig, ok := cut(value)
	if !ok {
		return Session{}, ErrNoSession
	}
	// **먼저 서명을 본다.** 서명이 확인되기 전의 payload 는 공격자가 쓴 문자열이므로
	// JSON으로 풀어보는 것조차 그쪽이 고른 입력을 파서에 먹이는 일이다.
	if !hmac.Equal([]byte(sig), []byte(c.sign(payload))) {
		return Session{}, ErrNoSession
	}
	body, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return Session{}, ErrNoSession
	}
	var s Session
	if err := json.Unmarshal(body, &s); err != nil {
		return Session{}, ErrNoSession
	}
	if s.UserID <= 0 || now.Unix() >= s.Expires {
		return Session{}, ErrNoSession
	}
	return s, nil
}

func (c *Codec) sign(payload string) string {
	m := hmac.New(sha256.New, c.key)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// cut 은 마지막 점에서 자른다. base64url 알파벳에 점이 없으므로 갈라지는 자리가 하나다.
func cut(v string) (payload, sig string, ok bool) {
	for i := len(v) - 1; i >= 0; i-- {
		if v[i] == '.' {
			return v[:i], v[i+1:], true
		}
	}
	return "", "", false
}
