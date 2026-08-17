package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// scriptedOpponent 는 정해진 수만 둔다. WS 계층만 보려는 것이라 엔진을 띄우지 않는다.
type scriptedOpponent struct{ moves []string }

func (o *scriptedOpponent) Choose(_ context.Context, _ string, moves []string, _ skill.Estimate) (string, error) {
	// 사람 수 뒤에 오므로 우리 차례는 (len(moves)-1)/2 번째다.
	i := len(moves) / 2
	if i >= len(o.moves) {
		return "resign", nil
	}
	return o.moves[i], nil
}

func dialGame(t *testing.T, moves ...string) (*websocket.Conn, context.Context) {
	t.Helper()
	return dialWith(t, Options{
		NewOpponent: func() game.Opponent { return &scriptedOpponent{moves: moves} },
	})
}

func dialWith(t *testing.T, opts Options) (*websocket.Conn, context.Context) {
	t.Helper()
	h := Handler(opts)
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

// TestRealEngineIntervention 은 진짜 엔진으로 **블런더를 두면 물러지는지** 본다.
// D3의 완료 기준이고, 가짜 판정으로는 증명이 안 된다 — 우리가 적어둔 답이 돌아올 뿐이다.
//
// **실제 8급 대국의 중반 국면에서 시작한다.** 초기 국면부터 아무 수나 두면 20수 만에
// 절망적인 형세가 되는데, **지고 있을 때도 승률이 포화해** 개입이 안 걸린다 —
// 이기고 있을 때와 같은 이유다(01-core.md §2). 판정이 의미를 갖는 것은 형세가
// 팽팽한 구간이고, 그게 실제 사용자가 있는 곳이다.
//
//	SHOWGI_USI_CMD=/opt/yaneuraou/run go test ./internal/server/ -run RealEngineIntervention -v
func TestRealEngineIntervention(t *testing.T) {
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" {
		t.Skip("SHOWGI_USI_CMD 미설정")
	}
	pool, err := usi.NewPool(2, cmd, map[string]string{
		"USI_Hash": "128", "Threads": "1", "FV_SCALE": "24",
		"BookFile": "no_book", "USI_OwnBook": "false",
	})
	if err != nil {
		t.Fatalf("엔진 풀: %v", err)
	}
	defer pool.Close()

	// 대국 B의 30수째 — 서로 진영을 짜고 형세가 팽팽한 지점이다.
	pos := shogi.StartPosition()
	for _, u := range strings.Fields(kifuBOpening) {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			t.Fatalf("기보 파싱 %s: %v", u, err)
		}
		pos = pos.Apply(m)
	}
	start := pos.SFEN()
	t.Logf("시작 국면(B-30手): %s", start)

	srv := httptest.NewServer(Handler(Options{
		NewOpponent: func() game.Opponent { return game.NewEngineOpponent(pool, 8) },
		NewAnalyst: func() game.Analyst {
			return game.NewEngineAnalyst(pool, nil, intervene.Intermediate)
		},
		StartSFEN:    start,
		HumanColor:   pos.Turn,
		ObservePlies: -1, // 중반부터 시작하므로 관측 구간을 끈다
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/game", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.CloseNow()

	snap := read(t, ctx, conn).Snapshot
	if snap == nil || !snap.YourTurn {
		t.Fatalf("시작 스냅샷: %+v", snap)
	}

	// 엔진이 제일 싫어하는 수를 골라 일부러 블런더를 둔다.
	worst := worstMove(t, ctx, pool, start, snap)
	t.Logf("일부러 두는 수: %s", worst)
	if err := wsjson.Write(ctx, conn, clientMsg{Type: "move", USI: worst}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := readUntil(t, ctx, conn, func(m serverMsg) bool {
		if m.Type == "error" {
			t.Fatalf("합법수 %s 가 거절됨: %s", worst, m.Reason)
		}
		return m.Snapshot.Intervention != nil || (!m.Snapshot.Judging && m.Snapshot.Ply > 1)
	}, "판정 결과")

	iv := got.Snapshot.Intervention
	if iv == nil {
		t.Fatalf("엔진이 제일 싫어하는 수를 뒀는데 개입하지 않았다: %+v", got.Snapshot)
	}
	t.Logf("개입: %s (%s)  Δ승률=%.3f  詰み=%v", iv.RetractedJa, iv.RetractedUSI, iv.DeltaWin, iv.LostMate)
	t.Logf("카테고리: %s", iv.Category)
	t.Logf("문구: %s", iv.Message)

	// **여기서 나오는 것은 실제로 `other` 다.** 엔진이 제일 싫어하는 수가 ▲1七香 —
	// 駒를 던지지도, 王手를 걸지도, 玉을 열지도 않고 그냥 손해인 수다. 짚을 이유가
	// 없으므로 짚지 않는 것이 맞다(01-core.md §3). **억지로 끼워 맞추면 설명이 틀리고,
	// 그게 이 제품에서 가장 큰 실패다.** 그래서 값을 못 박지 않는다.
	//
	// 이유가 붙는 쪽은 TestRealEngineHangingPiece 가 본다 — 결과가 정해진 수로 묻는다.
	if iv.Category == "" {
		t.Error("개입했는데 카테고리가 비어 있다")
	}

	if iv.RetractedUSI != worst {
		t.Fatalf("다른 수가 물러졌다: %s", iv.RetractedUSI)
	}
	if got.Snapshot.Ply != 0 || !got.Snapshot.YourTurn {
		t.Fatalf("물러진 뒤 상태가 틀렸다: ply=%d yourTurn=%v", got.Snapshot.Ply, got.Snapshot.YourTurn)
	}
	if !iv.LostMate && iv.DeltaWin <= intervene.Intermediate.Threshold() {
		t.Fatalf("임계치를 안 넘었는데 걸렸다: Δ=%.3f", iv.DeltaWin)
	}
}

// TestRealEngineHangingPiece 는 **이유가 화면까지 가는지**를 본다.
//
// 앞 테스트는 「개입이 걸리는가」이고 여기는 「왜 나쁜지를 말하는가」다. 갈라 두는
// 이유는 최악수가 늘 짚을 만한 수는 아니기 때문이다 — 저쪽에서 나오는 ▲1七香은
// 정당하게 미분류다.
//
// 수는 프로덕션에서 실제로 걸린 것을 그대로 쓴다(journal §13). 角을 아무도
// 지켜주지 않는 3三에 던지는 수다.
//
// **국면을 ▲7六歩 △3四歩 뒤로 고정해서 시작한다.** 상대에게 한 수를 맡기면
// △4四歩로 8八–3三 대각선이 막혀 8h3c+ 가 아예 불법이 되고, 그때 나오는 것은
// 「거절됨」이라 서버 버그처럼 보인다. 엔진이 그 수를 고를 일은 거의 없지만
// **거의 없는 것을 테스트의 전제로 삼지 않는다.**
//
//	SHOWGI_USI_CMD=/opt/yaneuraou/run go test ./internal/server/ -run RealEngineHangingPiece -v
func TestRealEngineHangingPiece(t *testing.T) {
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" {
		t.Skip("SHOWGI_USI_CMD 미설정")
	}
	pool, err := usi.NewPool(2, cmd, map[string]string{
		"USI_Hash": "128", "Threads": "1", "FV_SCALE": "24",
		"BookFile": "no_book", "USI_OwnBook": "false",
	})
	if err != nil {
		t.Fatalf("엔진 풀: %v", err)
	}
	defer pool.Close()

	// ▲7六歩 △3四歩 뒤. 여기서 사람이 두는 첫 수가 곧 판정 대상이다.
	pos := shogi.StartPosition()
	for _, u := range []string{"7g7f", "3c3d"} {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			t.Fatalf("기보 파싱 %s: %v", u, err)
		}
		pos = pos.Apply(m)
	}

	srv := httptest.NewServer(Handler(Options{
		NewOpponent: func() game.Opponent { return game.NewEngineOpponent(pool, 8) },
		NewAnalyst: func() game.Analyst {
			return game.NewEngineAnalyst(pool, nil, intervene.Intermediate)
		},
		StartSFEN:  pos.SFEN(),
		HumanColor: pos.Turn,
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/game", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.CloseNow()

	snap := read(t, ctx, conn).Snapshot
	if snap == nil || !snap.YourTurn {
		t.Fatalf("시작 스냅샷: %+v", snap)
	}
	if !slices.Contains(snap.LegalMoves, "8h3c+") {
		t.Fatalf("8h3c+ 가 합법수 목록에 없다 — 시작 국면이 의도와 다르다: %s", pos.SFEN())
	}

	// ▲3三角成 — 角을 던진다.
	if err := wsjson.Write(ctx, conn, clientMsg{Type: "move", USI: "8h3c+"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := readUntil(t, ctx, conn, func(m serverMsg) bool {
		if m.Type == "error" {
			t.Fatalf("8h3c+ 가 거절됨: %s", m.Reason)
		}
		return m.Snapshot.Intervention != nil || (!m.Snapshot.Judging && m.Snapshot.Ply > 1)
	}, "판정 결과")

	iv := got.Snapshot.Intervention
	if iv == nil {
		t.Fatalf("角을 던졌는데 개입하지 않았다: %+v", got.Snapshot)
	}
	t.Logf("개입: %s  Δ승률=%.3f  카테고리=%s", iv.RetractedJa, iv.DeltaWin, iv.Category)
	t.Logf("문구: %s", iv.Message)

	if iv.Category != string(intervene.CategoryHangsPiece) {
		t.Errorf("카테고리 %q 기대, got %q", intervene.CategoryHangsPiece, iv.Category)
	}
	// 문구가 카테고리를 따라가야 한다. 여기가 갈리면 화면에는 미분류 문구가 그대로 나간다.
	//
	// **낱말 하나로 고정하지 않는다.** タダ捨て는 사실이 실리면 「取れる相手の駒が2枚」처럼
	// 숫자로 말하고, 없으면 「相手の利きを確かめて」로 간다(explain.Render). 둘 다 「상대가
	// 그 駒를 잡는다」는 같은 이야기인데, 낱말을 박아 두면 사실이 실리는 날 깨진다 —
	// **실제로 깨져 있었고 CI에 엔진이 없어 아무도 몰랐다**(journal §47).
	if !strings.Contains(iv.Message, "利き") && !strings.Contains(iv.Message, "取れる相手の駒") {
		t.Errorf("タダ捨て 문구가 아니다: %q", iv.Message)
	}

	// 카드가 그 국면을 연다 — **수순을 읊는 자리가 아니다**(journal §54). 여기가 비면
	// 화면은 「そのとき、こう指していたら」를 띄울 판이 없다.
	if iv.RetractedSFEN == "" {
		t.Fatal("물러진 수 직후의 국면이 안 왔다 — 카드가 열 판이 없다")
	}
	if iv.RetractedSFEN == got.Snapshot.SFEN {
		t.Fatalf("되돌아온 판을 그대로 보냈다: %q", iv.RetractedSFEN)
	}

	// 반박 수순은 **증명된 詰み일 때만** 온다. PV를 잘라 보내던 자리인데 어디서 자를지가
	// 국면마다 달랐다(§20 · §25 · §54). 이 국면은 詰み이 아니므로 비어 있는 것이 맞고,
	// 차 있으면 그 수순이 다시 새고 있다는 뜻이다.
	t.Logf("반박 수순: %+v", iv.Refutation)
	if len(iv.Refutation) > 0 {
		t.Errorf("詰み이 아닌 국면에 수순이 실렸다: %+v", iv.Refutation)
	}
}

// worstMove 는 합법수 중 엔진 평가가 제일 나쁜 것을 고른다.
func worstMove(t *testing.T, ctx context.Context, pool *usi.Pool, start string, snap *game.Snapshot) string {
	t.Helper()
	worst, worstCp := snap.LegalMoves[0], 1<<30
	for _, mv := range snap.LegalMoves {
		res, err := pool.SearchDepth(ctx, start, []string{mv}, 6)
		if err != nil {
			t.Fatalf("worstMove: %v", err)
		}
		// 착수 후 국면은 상대 관점이다. 상대에게 좋을수록 나에게 나쁘다.
		if mine := -res.ScoreCp; mine < worstCp {
			worst, worstCp = mv, mine
		}
	}
	return worst
}

// 将棋ウォーズ 8급 대국의 첫 30수. internal/usi 의 측정 기보와 같은 대국이고,
// 거기서는 전 수가 룰 엔진으로 검증된다(TestMeasureKifuIsLegal).
const kifuBOpening = `7g7f 8b4b 2h6h 4c4d 5i4h 3c3d 4h3h 2b3c 6i5h 3a3b
4i4h 7a7b 3i2h 3b4c 6g6f 4a5b 7i7h 5a6b 7h6g 6b7a
8h7g 3d3e 9g9f 4c3d 1g1f 3d4e 9f9e 3e3f 3g3f 4e3f`

// TestRealEngineStrengthReachesTheClient 는 **실력 추정이 화면까지 오는가**를 본다.
//
// 배선이 길다 — 판정 → 추정기 goroutine → 세션 → 스냅샷 → WS. 가짜 판정으로는 첫 칸을
// 못 채우고(낙폭이 우리가 적은 값이다), 단위 테스트로는 마지막 칸을 못 본다.
// **프로덕션과 같은 조립**이다: `NewAdaptiveOpponent` + `NewEngineAnalyst`(journal §47).
//
//	SHOWGI_USI_CMD=/opt/yaneuraou/run go test ./internal/server/ -run RealEngineStrength -v
func TestRealEngineStrengthReachesTheClient(t *testing.T) {
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" {
		t.Skip("SHOWGI_USI_CMD 미설정")
	}
	pool, err := usi.NewPool(2, cmd, map[string]string{
		"USI_Hash": "128", "Threads": "1", "FV_SCALE": "24",
		"BookFile": "no_book", "USI_OwnBook": "false",
	})
	if err != nil {
		t.Fatalf("엔진 풀: %v", err)
	}
	defer pool.Close()

	pos := shogi.StartPosition()
	for _, u := range strings.Fields(kifuBOpening) {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			t.Fatalf("기보 파싱 %s: %v", u, err)
		}
		pos = pos.Apply(m)
	}
	start := pos.SFEN()

	srv := httptest.NewServer(Handler(Options{
		NewOpponent: func() game.Opponent { return game.NewAdaptiveOpponent(pool, 8, game.DefaultBand) },
		NewAnalyst: func() game.Analyst {
			return game.NewEngineAnalyst(pool, nil, intervene.Intermediate)
		},
		StartSFEN:    start,
		HumanColor:   pos.Turn,
		ObservePlies: -1,
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/game", nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.CloseNow()

	snap := read(t, ctx, conn).Snapshot
	if snap == nil || !snap.YourTurn {
		t.Fatalf("시작 스냅샷: %+v", snap)
	}
	// 아무것도 보기 전에는 한복판이다 — 그리고 **0이 아니다**. 0은 「조절이 꺼졌다」는 뜻이다.
	if snap.OpponentStrength != 3 {
		t.Fatalf("첫 스냅샷의 강함이 한복판이 아니다: %d", snap.OpponentStrength)
	}

	// 같은 국면에서 같은 나쁜 수를 되풀이한다. 물러지면 국면이 그대로 돌아오므로
	// 그 수가 계속 합법이고, **되풀이 자체가 실제로 일어나는 일**이다(journal §17).
	worst := worstMove(t, ctx, pool, start, snap)
	t.Logf("일부러 두는 수: %s", worst)

	got := snap
	for i := range skill.MinSamples {
		if err := wsjson.Write(ctx, conn, clientMsg{Type: "move", USI: worst}); err != nil {
			t.Fatalf("%d번째 Write: %v", i+1, err)
		}

		// **먼저 「판정 중」을 기다린다.** 물러지면 `Ply` 가 0으로 돌아오므로 앞선 회차의
		// 스냅샷과 뜻으로 구별되지 않고, 그대로 「판정 결과」를 기다리면 **직전 회차가 남긴
		// 스냅샷이 즉시 맞아** 이 회차를 안 재고 넘어간다. `judging` 은 이 착수에만 켜진다.
		readUntil(t, ctx, conn, func(m serverMsg) bool {
			if m.Type == "error" {
				t.Fatalf("%s 가 거절됨: %s", worst, m.Reason)
			}
			return m.Snapshot != nil && m.Snapshot.Judging
		}, "판정 시작")

		got = readUntil(t, ctx, conn, func(m serverMsg) bool {
			return m.Snapshot != nil && !m.Snapshot.Judging &&
				(m.Snapshot.Intervention != nil || m.Snapshot.YourTurn)
		}, "판정 결과").Snapshot
		t.Logf("%d수째 판정 뒤: 강함=%d 개입=%v", i+1, got.OpponentStrength, got.Intervention != nil)
	}

	// 마지막 판정의 추정치는 스냅샷보다 늦게 올 수 있다 — 추정기가 세션 밖에서 돌기
	// 때문이고, 그래서 「기다리지 않는다」가 설계다(game.Opponent). 눈금이 내려간
	// 스냅샷을 기다린다.
	if got.OpponentStrength >= 3 {
		got = readUntil(t, ctx, conn, func(m serverMsg) bool {
			return m.Snapshot != nil && m.Snapshot.OpponentStrength > 0 && m.Snapshot.OpponentStrength < 3
		}, "강함이 내려간 스냅샷").Snapshot
	}
	t.Logf("나쁜 수 %d번 뒤의 강함: %d", skill.MinSamples, got.OpponentStrength)
}
