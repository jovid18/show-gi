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

// 평가치가 **실제로 DB까지 간다.**
//
// 사람의 수 뒤와 **그 직전 상대 수 뒤** 두 행이 채워진다. 앞쪽은 판정의 「착수 전」
// 국면이라 상대 수의 평가치가 한 수 늦게 들어가는 구조다(session.recordEvals).
//
// 세션·store 는 각자 테스트가 있지만 **그 사이의 이벤트 배선은 여기서만 지켜진다** —
// dbRecorder 가 evEvaluated 를 흘리면 아무 데서도 안 터지고 칸만 계속 NULL 로 남는다.
// 실제로 그 상태로 109수 한 판이 기록됐다(docs/08-playtest.md §11).
func TestRecordFillsEvalTrajectory(t *testing.T) {
	st := openStoreForTest(t)

	before, err := st.CountGames(t.Context())
	if err != nil {
		t.Fatalf("CountGames: %v", err)
	}

	srv := httptest.NewServer(Handler(Options{
		NewOpponent: func() game.Opponent { return &scriptedOpponent{moves: []string{"3c3d", "8c8d"}} },
		// 평가치는 판정이 들고 온다. 개입은 안 걸리게 두고 값만 흘린다.
		NewAnalyst: func() game.Analyst { return &evalOnlyAnalyst{} },
		Store:      st,
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/game", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	read(t, ctx, conn)

	// **한 수씩 응수를 기다리고 보낸다.** 판정은 세션 밖 goroutine이라 사람의 수는
	// 판정이 끝나기 전에 반환된다(state.playHuman). 응수를 안 기다리고 이어 보내면
	// 판정 중에 도착한 수가 not_your_turn 으로 거절되고, 아무도 다시 보내지 않으니
	// 그 뒤 국면이 영영 안 온다.
	for i, u := range []string{"7g7f", "2g2f"} {
		if err := wsjson.Write(ctx, conn, clientMsg{Type: "move", USI: u}); err != nil {
			t.Fatalf("Write %s: %v", u, err)
		}
		wantPly := 2 * (i + 1) // 사람의 수 + 상대의 응수
		readUntil(t, ctx, conn, func(m serverMsg) bool {
			return m.Type == "snapshot" && m.Snapshot.Ply == wantPly
		}, "상대 응수")
	}

	gameID := waitForNewGame(t, st, before)
	t.Cleanup(func() { deleteGame(t, st, gameID) })

	// 기록은 비동기라 채워질 때까지 기다린다.
	deadline := time.Now().Add(10 * time.Second)
	var filled int
	for time.Now().Before(deadline) {
		row := st.Pool().QueryRow(t.Context(),
			`SELECT count(*) FROM game_moves WHERE game_id = $1 AND eval_cp IS NOT NULL`, gameID)
		if err := row.Scan(&filled); err != nil {
			t.Fatalf("조회: %v", err)
		}
		if filled >= 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	// 1·2·3수가 채워진다. 4수째(상대의 마지막 수)는 그 뒤에 사람의 수가 없어서 비어 있다.
	if filled < 3 {
		t.Fatalf("평가치가 %d행만 채워졌다 — 3행이어야 한다", filled)
	}

	// 부호가 뒤집혀 들어가면 궤적이 상하로 뒤집힌다. 사람이 先手이므로 그대로여야 한다.
	var cp int
	row := st.Pool().QueryRow(t.Context(), `SELECT eval_cp FROM game_moves WHERE game_id=$1 AND ply=1`, gameID)
	if err := row.Scan(&cp); err != nil {
		t.Fatalf("1수 평가치: %v", err)
	}
	if cp != evalOnlyAfterSente {
		t.Fatalf("1수 평가치 %+d — %+d 여야 한다", cp, evalOnlyAfterSente)
	}
}

// evalOnlyAfterSente 는 evalOnlyAnalyst 가 돌려주는 착수 후 평가치(先手 관점)다.
const evalOnlyAfterSente = 120

// evalOnlyAnalyst 는 개입 없이 평가치만 돌려준다. 엔진을 띄우지 않는다.
type evalOnlyAnalyst struct{}

func (evalOnlyAnalyst) Judge(_ context.Context, _ string, _ []string, _ int) (game.Judgement, error) {
	return game.Judgement{SenteCpBefore: 40, SenteCpAfter: evalOnlyAfterSente, HasEvals: true}, nil
}
