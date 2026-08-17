package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/jovid18/show-gi/apps/server/internal/auth"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 대인전 한 판을 **끝까지** 돌린다 — 방을 만들고, 둘이 붙고, 두고, 投了하고, 기록이
// 양쪽에 남는 것까지.
//
// **진짜 DB가 필요하다.** 한 판이 `games` 행 두 개로 남는다는 것이 이 기능의 기록 설계
// 전부인데(012_match_games.sql), 그건 가짜 store 로는 확인할 수 없다.
//
//	SHOWGI_TEST_DATABASE_URL=postgres://showgi:showgi@localhost:5432/showgi go test ./internal/server/
//
// **엔진이 필요 없다.** 대인전은 룰 엔진과 시계뿐이라(`internal/match`) 이 테스트가
// 엔진 없이 도는 것 자체가 그 사실의 증거다.
func TestTwoPeoplePlayAMatch(t *testing.T) {
	url := os.Getenv("SHOWGI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SHOWGI_TEST_DATABASE_URL 미설정 — DB 테스트 건너뜀")
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)

	// 두 사람. **진짜 행이어야 한다** — `games.user_id` 가 `users` 를 참조한다.
	alice, err := st.UpsertUser(t.Context(), "test", "match-alice", "アリス")
	if err != nil {
		t.Fatalf("upsert alice: %v", err)
	}
	bob, err := st.UpsertUser(t.Context(), "test", "match-bob", "ボブ")
	if err != nil {
		t.Fatalf("upsert bob: %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	const secret = "session-secret"
	opts := Options{
		Google:        auth.NewGoogle("client-id", "client-secret"),
		SessionSecret: secret,
		Store:         st,
		Level:         intervene.Beginner,
		Match:         NewMatch(ctx, st, intervene.Beginner),
	}
	srv := httptest.NewServer(Handler(opts))
	t.Cleanup(srv.Close)

	codec := auth.NewCodec(secret)
	cookieFor := func(id int64, name string) string {
		v, err := codec.Encode(id, name, time.Now())
		if err != nil {
			t.Fatalf("encode session: %v", err)
		}
		return sessionCookie + "=" + v
	}
	aliceCookie, bobCookie := cookieFor(alice, "アリス"), cookieFor(bob, "ボブ")

	// ── 방을 만든다 ────────────────────────────────────────
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+"/api/rooms?color=b", nil)
	req.Header.Set("Cookie", aliceCookie)
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("create room: %v", err)
	}
	var room roomPayload
	if err := json.NewDecoder(res.Body).Decode(&room); err != nil {
		t.Fatalf("decode room: %v", err)
	}
	res.Body.Close()
	if len(room.ID) != 22 {
		t.Fatalf("room id %q is %d chars, want 22", room.ID, len(room.ID))
	}

	// ── 둘이 붙는다 ────────────────────────────────────────
	wsURL := strings.Replace(srv.URL, "http://", "ws://", 1) + "/ws/match?room=" + room.ID
	aliceWS := dialMatch(t, wsURL, aliceCookie)
	bobWS := dialMatch(t, wsURL, bobCookie)

	// 방을 만든 사람은 붙자마자 「기다리는 중」을 받는다. **판보다 먼저 온다** —
	// 초대 링크를 그릴 것이 그것뿐이다.
	if got := readMatch(t, aliceWS); got.Type != "waiting" {
		t.Fatalf("alice's first frame is %q, want waiting", got.Type)
	}

	// 손님이 붙으면 그 자리에서 판이 선다. **`waiting` 뒤에 `snapshot` 이 온다.**
	first := readUntilSnapshot(t, aliceWS)
	if first.YourColor != "b" || !first.YourTurn {
		t.Fatalf("alice is %s and yourTurn=%v, want b/true", first.YourColor, first.YourTurn)
	}
	if first.OpponentName != "ボブ" {
		t.Fatalf("alice's opponent is %q, want ボブ", first.OpponentName)
	}
	if first.TurnLimitMs <= 0 {
		t.Fatalf("no clock: turnLimitMs=%d", first.TurnLimitMs)
	}

	bobFirst := readUntilSnapshot(t, bobWS)
	if bobFirst.YourColor != "w" || bobFirst.YourTurn {
		t.Fatalf("bob is %s and yourTurn=%v, want w/false", bobFirst.YourColor, bobFirst.YourTurn)
	}
	// **상대 차례에는 합법수를 안 준다.** 주면 상대의 수를 화면에서 훑어볼 수 있다.
	if len(bobFirst.LegalMoves) != 0 {
		t.Fatalf("bob got %d legal moves on alice's turn", len(bobFirst.LegalMoves))
	}

	// ── 둔다 ───────────────────────────────────────────────
	writeMatch(t, aliceWS, map[string]any{"type": "move", "usi": "7g7f"})

	afterBob := readUntilPly(t, bobWS, 1)
	if !afterBob.YourTurn || len(afterBob.LegalMoves) == 0 {
		t.Fatalf("bob is not to move after alice played: yourTurn=%v n=%d", afterBob.YourTurn, len(afterBob.LegalMoves))
	}
	// 같은 수가 **보는 사람마다 다른 이름**으로 온다.
	if afterBob.Moves[0].By != "opponent" {
		t.Fatalf("bob sees alice's move as %q, want opponent", afterBob.Moves[0].By)
	}
	afterAlice := readUntilPly(t, aliceWS, 1)
	if afterAlice.Moves[0].By != "you" {
		t.Fatalf("alice sees her own move as %q, want you", afterAlice.Moves[0].By)
	}

	writeMatch(t, bobWS, map[string]any{"type": "move", "usi": "3c3d"})
	readUntilPly(t, aliceWS, 2)

	// ── 投了 ───────────────────────────────────────────────
	writeMatch(t, bobWS, map[string]any{"type": "resign"})

	aliceEnd := readUntilOver(t, aliceWS)
	if aliceEnd.Status != "resigned" || aliceEnd.Winner != "you" {
		t.Fatalf("alice sees %s/%s, want resigned/you", aliceEnd.Status, aliceEnd.Winner)
	}
	bobEnd := readUntilOver(t, bobWS)
	if bobEnd.Winner != "opponent" {
		t.Fatalf("bob sees winner=%s, want opponent", bobEnd.Winner)
	}

	// ── 기록이 **양쪽에** 남는다 ───────────────────────────
	aliceGame := readUntilRecord(t, aliceWS)
	bobGame := readUntilRecord(t, bobWS)
	if aliceGame == bobGame {
		t.Fatalf("both sides got the same game id %d — one match must leave two rows", aliceGame)
	}

	aliceRec, err := st.GameRecord(t.Context(), aliceGame, &alice)
	if err != nil {
		t.Fatalf("alice's record: %v", err)
	}
	bobRec, err := st.GameRecord(t.Context(), bobGame, &bob)
	if err != nil {
		t.Fatalf("bob's record: %v", err)
	}

	// 같은 판이라는 표식이 둘에 다 있어야 나중에 묶을 수 있다.
	if aliceRec.MatchID != room.ID || bobRec.MatchID != room.ID {
		t.Fatalf("match ids are %q and %q, want both %q", aliceRec.MatchID, bobRec.MatchID, room.ID)
	}
	if aliceRec.Result != store.ResultWin || bobRec.Result != store.ResultLoss {
		t.Fatalf("results are %s and %s, want win and loss", aliceRec.Result, bobRec.Result)
	}
	if aliceRec.MyColor != "b" || bobRec.MyColor != "w" {
		t.Fatalf("colors are %s and %s, want b and w", aliceRec.MyColor, bobRec.MyColor)
	}
	// **기보는 한 벌이고 양쪽에 같이 들어간다.**
	for _, rec := range []store.GameRecord{aliceRec, bobRec} {
		if len(rec.Moves) != 2 || rec.Moves[0].USI != "7g7f" || rec.Moves[1].USI != "3c3d" {
			t.Fatalf("game %d has %+v, want 7g7f then 3c3d", rec.ID, rec.Moves)
		}
		// **개입도 평가치도 없다.** 엔진을 안 부르는 판이라서다 — 그 사실이 총평과 퀴즈를
		// 닫는 근거다(review.go · quiz.go).
		if len(rec.Interventions) != 0 {
			t.Fatalf("game %d has %d interventions, want none", rec.ID, len(rec.Interventions))
		}
	}

	// **남의 판은 안 열린다.** 대인전이라고 소유 검사가 느슨해지지 않는다.
	if _, err := st.GameRecord(t.Context(), aliceGame, &bob); err == nil {
		t.Fatal("bob could read alice's row of the same match")
	}

	// **총평과 퀴즈는 닫혀 있다.** 개입이 0건인 것을 「잘 둔 판」으로 그리면 거짓이 된다.
	sum := get(t, srv, "/api/games/"+itoa(aliceGame)+"/summary", aliceCookie)
	if sum != http.StatusNotFound {
		t.Fatalf("summary of a match game = %d, want %d", sum, http.StatusNotFound)
	}
	quizBody := getBody(t, srv, "/api/games/"+itoa(aliceGame)+"/quiz", aliceCookie)
	if !strings.Contains(quizBody, `"ready":true`) {
		t.Fatalf("quiz of a match game says %s, want ready:true (there will never be items)", quizBody)
	}
}

// ── 도구 ──────────────────────────────────────────────────

func dialMatch(t *testing.T, url, cookie string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.Dial(t.Context(), url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": []string{cookie}},
	})
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

// matchFrame 은 받은 프레임 하나다. `matchServerMsg` 를 그대로 안 쓰는 것은 **화면이
// 보는 모양으로** 읽기 위해서다 — json 태그가 곧 계약이라, 필드 이름이 바뀌면 여기가 깨져야 한다.
type matchFrame struct {
	Type     string `json:"type"`
	GameID   int64  `json:"gameId"`
	Snapshot snap   `json:"snapshot"`
}

// snap 은 스냅샷의 **화면이 읽는 몫**이다.
type snap struct {
	Ply          int      `json:"ply"`
	YourColor    string   `json:"yourColor"`
	YourTurn     bool     `json:"yourTurn"`
	LegalMoves   []string `json:"legalMoves"`
	Status       string   `json:"status"`
	Winner       string   `json:"winner"`
	OpponentName string   `json:"opponentName"`
	TurnLimitMs  int      `json:"turnLimitMs"`
	Moves        []struct {
		USI string `json:"usi"`
		By  string `json:"by"`
	} `json:"moves"`
}

func readMatch(t *testing.T, conn *websocket.Conn) matchFrame {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var got matchFrame
	if err := wsjson.Read(ctx, conn, &got); err != nil {
		t.Fatalf("read: %v", err)
	}
	return got
}

// readUntil 은 조건에 맞는 스냅샷이 올 때까지 읽는다. **접속 표시 때문에 같은 국면의
// 스냅샷이 여러 번 온다** — 상대가 붙고 떨어지는 것도 방송되기 때문이다.
func matchUntil(t *testing.T, conn *websocket.Conn, ok func(snap) bool) snap {
	t.Helper()
	for range 20 {
		got := readMatch(t, conn)
		if got.Type == "snapshot" && ok(got.Snapshot) {
			return got.Snapshot
		}
	}
	t.Fatal("the expected snapshot never arrived")
	return snap{}
}

func readUntilSnapshot(t *testing.T, conn *websocket.Conn) snap {
	t.Helper()
	return matchUntil(t, conn, func(snap) bool { return true })
}

func readUntilPly(t *testing.T, conn *websocket.Conn, ply int) snap {
	t.Helper()
	return matchUntil(t, conn, func(s snap) bool { return s.Ply >= ply })
}

func readUntilOver(t *testing.T, conn *websocket.Conn) snap {
	t.Helper()
	return matchUntil(t, conn, func(s snap) bool { return s.Status != "playing" })
}

func readUntilRecord(t *testing.T, conn *websocket.Conn) int64 {
	t.Helper()
	for range 20 {
		if got := readMatch(t, conn); got.Type == "record" {
			return got.GameID
		}
	}
	t.Fatal("the game id never arrived")
	return 0
}

func writeMatch(t *testing.T, conn *websocket.Conn, msg any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, conn, msg); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func get(t *testing.T, srv *httptest.Server, path, cookie string) int {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, nil)
	req.Header.Set("Cookie", cookie)
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	res.Body.Close()
	return res.StatusCode
}

func getBody(t *testing.T, srv *httptest.Server, path, cookie string) string {
	t.Helper()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, nil)
	req.Header.Set("Cookie", cookie)
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

func itoa(n int64) string {
	return strconv.FormatInt(n, 10)
}
