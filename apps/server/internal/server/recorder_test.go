package server

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 진짜 postgres에 붙는다. 기록은 SQL 위에서만 성립하므로 가짜로는 증명이 안 된다.
//
//	SHOWGI_TEST_DATABASE_URL=postgres://showgi:showgi@localhost:5432/showgi \
//	  go test ./internal/server/ -run Record -v
func openStoreForTest(t *testing.T) *store.Store {
	t.Helper()
	url := os.Getenv("SHOWGI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SHOWGI_TEST_DATABASE_URL 미설정")
	}
	s, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// 끝나지 않고 연결이 끊긴 판은 `abandoned` 로 남아야 한다.
//
// 빈 result 로 두면 **「아직 두는 중인 판」과 구별이 안 된다.** 기록을 나중에 훑을 때
// 그 둘이 섞이면 어느 판이 실제 대국인지 셀 수 없다.
func TestRecordAbandonsOnDisconnect(t *testing.T) {
	st := openStoreForTest(t)

	before, err := st.CountGames(t.Context())
	if err != nil {
		t.Fatalf("CountGames: %v", err)
	}

	srv := httptest.NewServer(Handler(Options{
		NewOpponent: func() game.Opponent { return &scriptedOpponent{moves: []string{"3c3d"}} },
		Store:       st,
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/game", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	read(t, ctx, conn) // 초기 스냅샷

	if err := wsjson.Write(ctx, conn, clientMsg{Type: "move", USI: "7g7f"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	readUntil(t, ctx, conn, func(m serverMsg) bool {
		return m.Type == "snapshot" && m.Snapshot.Ply == 2
	}, "상대 응수")

	// **투료하지 않고 그냥 끊는다.** 실제로 탭을 닫는 것과 같다.
	_ = conn.CloseNow()

	gameID := waitForNewGame(t, st, before)
	waitForResult(t, st, gameID, string(store.ResultAbandoned))

	// 기보는 남아 있어야 한다 — 끊겼다고 지우면 실력 추정의 원본이 사라진다.
	var moves int
	row := st.Pool().QueryRow(t.Context(), `SELECT count(*) FROM game_moves WHERE game_id = $1`, gameID)
	if err := row.Scan(&moves); err != nil {
		t.Fatalf("기보 조회: %v", err)
	}
	if moves != 2 {
		t.Errorf("기보 %d수 — 2수여야 한다", moves)
	}
	t.Cleanup(func() { deleteGame(t, st, gameID) })
}

// 투료로 끝난 판은 사람 기준 결과로 남는다.
func TestRecordFinishesOnResign(t *testing.T) {
	st := openStoreForTest(t)

	before, err := st.CountGames(t.Context())
	if err != nil {
		t.Fatalf("CountGames: %v", err)
	}

	srv := httptest.NewServer(Handler(Options{
		NewOpponent: func() game.Opponent { return &scriptedOpponent{moves: []string{"3c3d"}} },
		Store:       st,
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/game", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.CloseNow()
	read(t, ctx, conn)

	if err := wsjson.Write(ctx, conn, clientMsg{Type: "resign"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	readUntil(t, ctx, conn, func(m serverMsg) bool {
		return m.Type == "snapshot" && m.Snapshot.Status != game.StatusPlaying
	}, "투료 반영")

	gameID := waitForNewGame(t, st, before)
	waitForResult(t, st, gameID, string(store.ResultLoss)) // 사람이 투료했으므로 패
	t.Cleanup(func() { deleteGame(t, st, gameID) })
}

func waitForNewGame(t *testing.T, st *store.Store, before int64) int64 {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		n, err := st.CountGames(context.Background())
		if err == nil && n > before {
			var id int64
			row := st.Pool().QueryRow(context.Background(), `SELECT max(id) FROM games`)
			if err := row.Scan(&id); err == nil {
				return id
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("대국이 기록되지 않았다")
	return 0
}

func waitForResult(t *testing.T, st *store.Store, gameID int64, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var got *string
	for time.Now().Before(deadline) {
		row := st.Pool().QueryRow(context.Background(), `SELECT result FROM games WHERE id = $1`, gameID)
		if err := row.Scan(&got); err == nil && got != nil {
			if *got == want {
				return
			}
			t.Fatalf("result = %q, want %q", *got, want)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("result 이 %q 로 안 찍혔다 (still null)", want)
}

func deleteGame(t *testing.T, st *store.Store, id int64) {
	t.Helper()
	if _, err := st.Pool().Exec(context.Background(), `DELETE FROM games WHERE id = $1`, id); err != nil {
		t.Errorf("정리: %v", err)
	}
}
