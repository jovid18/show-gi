package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/auth"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/match"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 이 파일이 지키는 것은 **누가 그 방을 볼 수 있나** 하나다. 룰과 시계는 internal/match.

// matchTestServer 는 대인전 표면이 켜진 서버 하나와, 로그인 쿠키를 만드는 도구다.
func matchTestServer(t *testing.T) (http.Handler, *match.Hub, func(userID int64, name string) *http.Cookie) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// store 는 nil이다 — 대인전은 **DB 없이도 돈다**(기록만 안 남는다).
	m := NewMatch(ctx, nil, intervene.Beginner)
	opts := Options{
		Google:        auth.NewGoogle("client-id", "client-secret"),
		SessionSecret: "session-secret",
		Store:         &storePlaceholder,
		Match:         m,
	}

	codec := auth.NewCodec(opts.SessionSecret)
	signIn := func(userID int64, name string) *http.Cookie {
		value, err := codec.Encode(userID, name, time.Now())
		if err != nil {
			t.Fatalf("encode session: %v", err)
		}
		return &http.Cookie{Name: sessionCookie, Value: value}
	}
	return Handler(opts), m.hub, signIn
}

func do(h http.Handler, method, path string, c *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	if c != nil {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// **로그인하지 않으면 방을 못 만든다.** 익명은 서로 구별할 수단이 없어서
// (002_anonymous_games.sql) 정원 2명이라는 규칙이 성립하지 않는다.
func TestCreatingARoomNeedsSignIn(t *testing.T) {
	h, _, _ := matchTestServer(t)

	rec := do(h, http.MethodPost, "/api/rooms", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

// 방을 만들면 **유추할 수 없는 id** 하나가 나오고, 만든 사람이 고른 색이 그대로 온다.
func TestCreateRoomReturnsAnUnguessableID(t *testing.T) {
	h, _, signIn := matchTestServer(t)

	rec := do(h, http.MethodPost, "/api/rooms?color=w", signIn(1, "アリス"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var body roomPayload
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.ID) != 22 {
		t.Fatalf("room id %q is %d chars, want 22", body.ID, len(body.ID))
	}
	if body.YourColor != "w" {
		t.Fatalf("yourColor = %q, want w", body.YourColor)
	}
	if !body.Waiting {
		t.Error("a brand-new room is not waiting for anyone")
	}
}

// **로그인 안 한 사람에게는 404다 — 401이 아니다.**
//
// 401은 「로그인하면 볼 수 있다」는 뜻이라, 그것만으로 **그 방이 있다는 사실이 로그인 없이
// 새어 나간다.** 방 id 를 훑어보는 것이 성립하는 첫 걸음이 정확히 그 한 비트다.
func TestPeekingWithoutSignInLooksLikeAMissingRoom(t *testing.T) {
	h, _, signIn := matchTestServer(t)

	rec := do(h, http.MethodPost, "/api/rooms", signIn(1, "アリス"))
	var room roomPayload
	if err := json.NewDecoder(rec.Body).Decode(&room); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// 있는 방과 없는 방이 로그인 없이는 **같은 답**이어야 한다.
	real := do(h, http.MethodGet, "/api/rooms/"+room.ID, nil)
	fake := do(h, http.MethodGet, "/api/rooms/AAAAAAAAAAAAAAAAAAAAAA", nil)
	if real.Code != http.StatusNotFound || fake.Code != http.StatusNotFound {
		t.Fatalf("real = %d, fake = %d, want both %d", real.Code, fake.Code, http.StatusNotFound)
	}
	if real.Body.String() != fake.Body.String() {
		t.Fatalf("the two answers differ:\n real: %s\n fake: %s", real.Body.String(), fake.Body.String())
	}
}

// **세 번째 사람에게는 없는 방이다.** 링크가 새어도 관전조차 안 된다.
func TestAThirdPersonSeesNothing(t *testing.T) {
	h, hub, signIn := matchTestServer(t)

	rec := do(h, http.MethodPost, "/api/rooms", signIn(1, "アリス"))
	var room roomPayload
	if err := json.NewDecoder(rec.Body).Decode(&room); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// 밥이 손님 자리를 잡는다. 실제 입장은 WebSocket 이라 여기서는 hub 를 직접 부른다.
	if _, _, err := hub.Enter(room.ID, match.Player{UserID: 2, Name: "ボブ"}); err != nil {
		t.Fatalf("bob cannot enter: %v", err)
	}

	// 두 사람은 그대로 보이고, 캐롤에게는 안 보인다.
	for _, c := range []struct {
		who  *http.Cookie
		want int
	}{
		{signIn(1, "アリス"), http.StatusOK},
		{signIn(2, "ボブ"), http.StatusOK},
		{signIn(3, "キャロル"), http.StatusNotFound},
	} {
		got := do(h, http.MethodGet, "/api/rooms/"+room.ID, c.who)
		if got.Code != c.want {
			t.Errorf("peek by %s = %d, want %d", c.who.Value[:8], got.Code, c.want)
		}
	}
}

// 손님이 보는 것은 **방 주인의 이름과 자기 색**뿐이다. 段級도 전적도 안 나간다 —
// 실력 프로파일은 본인만 보는 값이다(02-architecture.md §7 위협 2).
func TestPeekTellsTheGuestTheirSide(t *testing.T) {
	h, _, signIn := matchTestServer(t)

	rec := do(h, http.MethodPost, "/api/rooms?color=b", signIn(1, "アリス"))
	var room roomPayload
	if err := json.NewDecoder(rec.Body).Decode(&room); err != nil {
		t.Fatalf("decode: %v", err)
	}

	got := do(h, http.MethodGet, "/api/rooms/"+room.ID, signIn(2, "ボブ"))
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", got.Code, http.StatusOK)
	}
	var body roomPayload
	if err := json.NewDecoder(got.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.HostName != "アリス" {
		t.Errorf("hostName = %q, want アリス", body.HostName)
	}
	if body.YourColor != "w" {
		t.Errorf("yourColor = %q, want w (the host took b)", body.YourColor)
	}
	if !body.Waiting {
		t.Error("waiting = false before the guest actually sat down")
	}
}

// **대인전 표면은 통째로 켜고 끈다.** 없으면 세 경로가 다 404여야 한다 — 반쯤 열려
// 있으면 화면이 「있는데 고장난 것」으로 읽는다.
func TestMatchRoutesAreAbsentWithoutAHub(t *testing.T) {
	h := Handler(Options{
		Google:        auth.NewGoogle("client-id", "client-secret"),
		SessionSecret: "session-secret",
		Store:         &storePlaceholder,
	})
	for _, c := range [][2]string{
		{http.MethodPost, "/api/rooms"},
		{http.MethodGet, "/api/rooms/AAAAAAAAAAAAAAAAAAAAAA"},
		{http.MethodGet, "/ws/match?room=AAAAAAAAAAAAAAAAAAAAAA"},
	} {
		rec := do(h, c[0], c[1], nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want %d", c[0], c[1], rec.Code, http.StatusNotFound)
		}
	}
}

// WebSocket 도 **업그레이드 전에** 거절한다. 뒤에서 거절하면 그 답을 프레임으로 말해야
// 하고, 화면이 그것을 「대국 중 오류」와 구별해야 한다(ws.go 의 resumeSetup 과 같은 규약).
func TestMatchSocketRejectsBeforeUpgrading(t *testing.T) {
	h, _, signIn := matchTestServer(t)

	for _, c := range []struct {
		name string
		who  *http.Cookie
	}{
		{"signed out", nil},
		{"signed in but no such room", signIn(1, "アリス")},
	} {
		rec := do(h, http.MethodGet, "/ws/match?room=AAAAAAAAAAAAAAAAAAAAAA", c.who)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want %d", c.name, rec.Code, http.StatusNotFound)
		}
	}
}

// 방을 만든 사람은 **자기 방의 손님이 될 수 없다.** 되면 혼자 두 색을 잡은 판이 선다.
func TestHostCannotFillTheGuestSeatOverHTTP(t *testing.T) {
	h, hub, signIn := matchTestServer(t)

	rec := do(h, http.MethodPost, "/api/rooms", signIn(1, "アリス"))
	var room roomPayload
	if err := json.NewDecoder(rec.Body).Decode(&room); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// 방 주인이 자기 링크를 몇 번 열어도 손님 자리는 안 찬다.
	for range 3 {
		if _, _, err := hub.Enter(room.ID, match.Player{UserID: 1, Name: "アリス"}); err != nil {
			t.Fatalf("host enter: %v", err)
		}
	}
	got := do(h, http.MethodGet, "/api/rooms/"+room.ID, signIn(2, "ボブ"))
	if got.Code != http.StatusOK {
		t.Fatalf("the guest seat is gone: status = %d", got.Code)
	}
	if _, c, err := hub.Enter(room.ID, match.Player{UserID: 2, Name: "ボブ"}); err != nil || c != shogi.White {
		t.Fatalf("guest enter: got (%v, %v), want (White, nil)", c, err)
	}
}
