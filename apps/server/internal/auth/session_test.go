package auth

import (
	"strings"
	"testing"
	"time"
)

const testSecret = "test-secret"

func TestSessionRoundTrip(t *testing.T) {
	c := NewCodec(testSecret)
	now := time.Unix(1_700_000_000, 0)

	v, err := c.Encode(42, "さとし", now)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	s, err := c.Decode(v, now)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if s.UserID != 42 || s.Name != "さとし" {
		t.Errorf("session = %+v, want uid 42 / さとし", s)
	}
}

// 서명 키가 없으면 코덱이 없다 — 서명 없는 세션을 만드느니 로그인을 끈다.
func TestNewCodecWithoutSecret(t *testing.T) {
	if NewCodec("") != nil {
		t.Error("NewCodec(\"\") is not nil")
	}
}

// 이 패키지가 지키는 것은 이 하나다. 본문을 고쳐 다른 user_id 가 되는 순간
// 쿠키 한 줄로 남이 될 수 있다.
func TestDecodeRejectsTamperedPayload(t *testing.T) {
	c := NewCodec(testSecret)
	now := time.Unix(1_700_000_000, 0)

	v, err := c.Encode(42, "さとし", now)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	payload, sig, _ := strings.Cut(v, ".")

	// 다른 사람의 세션을 손으로 만든다. 서명은 원래 것을 그대로 붙인다.
	forged, err := c.Encode(43, "さとし", now)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	otherPayload, _, _ := strings.Cut(forged, ".")
	if otherPayload == payload {
		t.Fatal("payloads are the same, the test proves nothing")
	}

	if _, err := c.Decode(otherPayload+"."+sig, now); err != ErrNoSession {
		t.Errorf("err = %v, want %v", err, ErrNoSession)
	}
}

// 다른 키로 서명된 쿠키가 통과하면 SESSION_SECRET 을 바꿔도 예전 쿠키가 산다.
func TestDecodeRejectsOtherKey(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	v, err := NewCodec("old-secret").Encode(42, "さとし", now)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := NewCodec(testSecret).Decode(v, now); err != ErrNoSession {
		t.Errorf("err = %v, want %v", err, ErrNoSession)
	}
}

func TestDecodeRejectsExpired(t *testing.T) {
	c := NewCodec(testSecret)
	now := time.Unix(1_700_000_000, 0)

	v, err := c.Encode(42, "さとし", now)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := c.Decode(v, now.Add(SessionTTL)); err != ErrNoSession {
		t.Errorf("at exactly the expiry: err = %v, want %v", err, ErrNoSession)
	}
	if _, err := c.Decode(v, now.Add(SessionTTL-time.Second)); err != nil {
		t.Errorf("one second before the expiry: err = %v, want nil", err)
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	c := NewCodec(testSecret)
	now := time.Unix(1_700_000_000, 0)

	for _, v := range []string{"", ".", "no-dot", "!!!.!!!", "eyJ1aWQiOjF9"} {
		if _, err := c.Decode(v, now); err != ErrNoSession {
			t.Errorf("Decode(%q) err = %v, want %v", v, err, ErrNoSession)
		}
	}
}

// user_id 0 은 「로그인 안 함」과 구별되지 않는다. 통과하면 그 세션이 유령이 된다.
func TestDecodeRejectsZeroUser(t *testing.T) {
	c := NewCodec(testSecret)
	now := time.Unix(1_700_000_000, 0)

	v, err := c.Encode(0, "", now)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if _, err := c.Decode(v, now); err != ErrNoSession {
		t.Errorf("err = %v, want %v", err, ErrNoSession)
	}
}
