package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// scriptedOpponent 는 정해진 수만 둔다. WS 계층만 보려는 것이라 엔진을 띄우지 않는다.
type scriptedOpponent struct{ moves []string }

func (o *scriptedOpponent) Choose(_ context.Context, _ string, moves []string) (string, error) {
	// 사람 수 뒤에 오므로 우리 차례는 (len(moves)-1)/2 번째다.
	i := len(moves) / 2
	if i >= len(o.moves) {
		return "resign", nil
	}
	return o.moves[i], nil
}

func dialGame(t *testing.T, moves ...string) (*websocket.Conn, context.Context) {
	t.Helper()
	h := Handler(Options{
		NewOpponent: func() game.Opponent { return &scriptedOpponent{moves: moves} },
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/game"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn, ctx
}

func read(t *testing.T, ctx context.Context, conn *websocket.Conn) serverMsg {
	t.Helper()
	var msg serverMsg
	if err := wsjson.Read(ctx, conn, &msg); err != nil {
		t.Fatalf("Read: %v", err)
	}
	return msg
}

// 조건을 만족하는 메시지가 올 때까지 읽는다.
func readUntil(t *testing.T, ctx context.Context, conn *websocket.Conn, cond func(serverMsg) bool, what string) serverMsg {
	t.Helper()
	for range 10 {
		msg := read(t, ctx, conn)
		if cond(msg) {
			return msg
		}
	}
	t.Fatalf("%s 를 못 받음", what)
	return serverMsg{}
}

func TestWSPlaysAGame(t *testing.T) {
	conn, ctx := dialGame(t, "3c3d")

	// 붙자마자 초기 스냅샷이 온다 — 클라이언트가 따로 물어보지 않아도 판을 그릴 수 있어야 한다.
	first := read(t, ctx, conn)
	if first.Type != "snapshot" || first.Snapshot == nil {
		t.Fatalf("첫 메시지 = %+v", first)
	}
	if !first.Snapshot.YourTurn || len(first.Snapshot.LegalMoves) != 30 {
		t.Fatalf("초기 스냅샷 = %+v", first.Snapshot)
	}

	if err := wsjson.Write(ctx, conn, clientMsg{Type: "move", USI: "7g7f"}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := readUntil(t, ctx, conn, func(m serverMsg) bool {
		return m.Type == "snapshot" && m.Snapshot.Ply == 2
	}, "엔진 응수")
	if got.Snapshot.Moves[0].Ja != "▲7六歩" || got.Snapshot.Moves[1].Ja != "△3四歩" {
		t.Fatalf("기보 = %+v", got.Snapshot.Moves)
	}
	if !got.Snapshot.YourTurn {
		t.Fatal("엔진이 두고 나면 사람 차례여야 한다")
	}
}

// 불법수는 코드와 일본어 문구를 함께 돌려준다. 판은 그대로여야 한다.
func TestWSRejectsIllegalMove(t *testing.T) {
	conn, ctx := dialGame(t, "3c3d")
	read(t, ctx, conn) // 초기 스냅샷

	// 2七의 歩는 2六까지만 간다
	if err := wsjson.Write(ctx, conn, clientMsg{Type: "move", USI: "2g2d"}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	msg := readUntil(t, ctx, conn, func(m serverMsg) bool { return m.Type == "error" }, "거절")
	if msg.Reason != "unreachable square" {
		t.Fatalf("사유 = %q", msg.Reason)
	}
	if msg.Message == "" || hasHangul(msg.Message) {
		t.Fatalf("화면 문구 = %q", msg.Message)
	}

	// 거절된 뒤에도 대국은 계속된다
	if err := wsjson.Write(ctx, conn, clientMsg{Type: "move", USI: "7g7f"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	readUntil(t, ctx, conn, func(m serverMsg) bool {
		return m.Type == "snapshot" && m.Snapshot.Ply == 2
	}, "거절 후 정상 착수")
}

func TestWSResign(t *testing.T) {
	conn, ctx := dialGame(t, "3c3d")
	read(t, ctx, conn)

	if err := wsjson.Write(ctx, conn, clientMsg{Type: "resign"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	msg := readUntil(t, ctx, conn, func(m serverMsg) bool {
		return m.Type == "snapshot" && m.Snapshot.Status != game.StatusPlaying
	}, "투료 반영")
	if msg.Snapshot.Status != game.StatusResigned || msg.Snapshot.Winner != game.SideEngine {
		t.Fatalf("투료 결과 = %+v", msg.Snapshot)
	}
}

func TestWSUnknownMessageType(t *testing.T) {
	conn, ctx := dialGame(t, "3c3d")
	read(t, ctx, conn)

	if err := wsjson.Write(ctx, conn, clientMsg{Type: "nonsense"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	msg := readUntil(t, ctx, conn, func(m serverMsg) bool { return m.Type == "error" }, "거절")
	if msg.Reason != "bad_move" || msg.Message == "" {
		t.Fatalf("거절 = %+v", msg)
	}
}

// 엔진이 없으면 대국만 막고 나머지는 살린다. 죽으면 ECS가 재시작을 돌며 사이트가 통째로 내려간다.
func TestWSUnavailableWithoutEngine(t *testing.T) {
	srv := httptest.NewServer(Handler(Options{}))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/ws/game")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", res.StatusCode)
	}

	// /healthz 는 그대로 떠 있어야 한다
	health, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer health.Body.Close()
	if health.StatusCode != http.StatusOK {
		t.Fatalf("엔진이 없다고 /healthz 가 죽었다: %d", health.StatusCode)
	}
}

// TestWSAgainstRealEngine 은 진짜 USI 엔진을 붙여 대국을 끝까지 굴린다.
//
// 여기까지 와야 D2의 완료 기준("사람 vs 엔진 한 판")이 증명된다 — 가짜 상대로는
// 우리가 적은 수가 돌아오는 것뿐이라, 엔진이 실제로 국면을 받아 두는지를 알 수 없다.
//
// SHOWGI_USI_CMD 가 없으면 건너뛴다. CI 러너에는 엔진이 없다.
//
//	SHOWGI_USI_CMD=fairy-stockfish go test ./internal/server/ -run RealEngine -v
func TestWSAgainstRealEngine(t *testing.T) {
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" {
		t.Skip("SHOWGI_USI_CMD 미설정 — 실엔진 대국 건너뜀")
	}

	pool, err := usi.NewPool(2, cmd, nil)
	if err != nil {
		t.Fatalf("엔진 풀 기동 실패: %v", err)
	}
	defer pool.Close()

	srv := httptest.NewServer(Handler(Options{
		NewOpponent: func() game.Opponent {
			return game.NewEngineOpponent(pool, 10)
		},
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/game", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.CloseNow()

	snap := read(t, ctx, conn).Snapshot
	if snap == nil {
		t.Fatal("초기 스냅샷이 없다")
	}

	// 사람은 항상 스냅샷이 준 합법수 중 하나를 둔다 — 클라이언트가 할 일과 같다.
	for ply := 0; ply < 12; ply++ {
		if snap.Status != game.StatusPlaying {
			break
		}
		if len(snap.LegalMoves) == 0 {
			t.Fatalf("%d수째: 사람 차례인데 합법수가 없다: %+v", ply, snap)
		}
		mine := snap.LegalMoves[0]
		if err := wsjson.Write(ctx, conn, clientMsg{Type: "move", USI: mine}); err != nil {
			t.Fatalf("Write: %v", err)
		}

		want := snap.Ply + 2 // 내 수 + 엔진 응수
		got := readUntil(t, ctx, conn, func(m serverMsg) bool {
			if m.Type == "error" {
				t.Fatalf("합법수 %s 가 거절됨: %s (%s)", mine, m.Reason, m.Message)
			}
			return m.Snapshot.Ply >= want || m.Snapshot.Status != game.StatusPlaying
		}, "엔진 응수")
		snap = got.Snapshot
	}

	if len(snap.Moves) < 4 {
		t.Fatalf("수가 너무 적다: %+v", snap.Moves)
	}
	for i, m := range snap.Moves {
		if m.Ja == "" {
			t.Fatalf("%d수째 棋譜 표기가 비었다: %+v", i+1, m)
		}
	}
	// 국면이 실제로 진행됐는지 — 초기 국면과 달라야 한다
	if snap.SFEN == shogi.StartSFEN {
		t.Fatal("판이 그대로다")
	}
	t.Logf("%d수 진행, 상태=%s", len(snap.Moves), snap.Status)
	for _, m := range snap.Moves {
		t.Logf("  %s (%s) %s", m.Ja, m.USI, m.By)
	}
}

// 화면에 나가는 문구는 전부 일본어다. 사람 눈으로 지키면 결국 샌다.
func TestRejectMessagesAreJapanese(t *testing.T) {
	for reason, msg := range rejectMessages {
		if msg == "" {
			t.Errorf("%s: 문구가 비었다", reason)
		}
		if hasHangul(msg) {
			t.Errorf("%s 의 문구에 한글: %q", reason, msg)
		}
	}
	// rejection 이 아는 코드는 전부 문구가 있어야 한다
	for _, reason := range []string{"not_your_turn", "finished", "bad_move", "internal"} {
		if _, ok := rejectMessages[reason]; !ok {
			t.Errorf("%s 에 문구가 없다", reason)
		}
	}
}

func hasHangul(s string) bool {
	for _, r := range s {
		switch {
		case r >= 0xAC00 && r <= 0xD7A3,
			r >= 0x1100 && r <= 0x11FF,
			r >= 0x3130 && r <= 0x318F:
			return true
		}
	}
	return false
}
