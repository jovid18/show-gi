package match

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// fakeRecorder 는 한 사람 몫의 기록을 모은다. 테이블 goroutine 이 부르므로 잠금이 있다.
type fakeRecorder struct {
	mu      sync.Mutex
	color   shogi.Color
	moves   []string
	result  Result
	started bool
}

func (f *fakeRecorder) Started(_ string, myColor shogi.Color) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started, f.color = true, myColor
}

func (f *fakeRecorder) Moved(_ int, usi string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moves = append(f.moves, usi)
}

func (f *fakeRecorder) Finished(r Result) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.result = r
}

func (f *fakeRecorder) snapshot() (moves []string, result Result) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.moves...), f.result
}

func newTestTable(t *testing.T, limit time.Duration) (*Table, *fakeRecorder, *fakeRecorder) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	black, white := &fakeRecorder{}, &fakeRecorder{}
	table, err := NewTable(ctx, Config{
		Black:     alice,
		White:     bob,
		Recorders: map[shogi.Color]Recorder{shogi.Black: black, shogi.White: white},
		TurnLimit: limit,
	})
	if err != nil {
		t.Fatalf("new table: %v", err)
	}
	return table, black, white
}

// 수번이 아닌 쪽은 못 둔다. 어느 쪽인지를 클라이언트가 안 보내므로 이건 서버가 자리에서
// 정한 쪽 하나로 갈린다(Hub.Enter).
func TestOnlyTheSideToMoveCanPlay(t *testing.T) {
	table, _, _ := newTestTable(t, time.Minute)
	ctx := context.Background()

	if _, err := table.Play(ctx, shogi.White, "3c3d"); !errors.Is(err, ErrNotYourTurn) {
		t.Fatalf("white moved first: got %v, want ErrNotYourTurn", err)
	}
	if _, err := table.Play(ctx, shogi.Black, "7g7f"); err != nil {
		t.Fatalf("black's first move: %v", err)
	}
	if _, err := table.Play(ctx, shogi.Black, "2g2f"); !errors.Is(err, ErrNotYourTurn) {
		t.Fatalf("black moved twice: got %v, want ErrNotYourTurn", err)
	}
}

// 상대 차례에는 합법수 목록을 안 준다. 주면 그 사람이 상대의 수를 화면에서 훑어볼 수
// 있고, 대인전에서 그건 그냥 부정행위 보조다.
func TestLegalMovesGoOnlyToTheSideToMove(t *testing.T) {
	table, _, _ := newTestTable(t, time.Minute)
	ctx := context.Background()

	black, err := table.Snapshot(ctx, shogi.Black)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !black.YourTurn || len(black.LegalMoves) == 0 {
		t.Fatalf("the side to move got no legal moves: yourTurn=%v n=%d", black.YourTurn, len(black.LegalMoves))
	}

	white, err := table.Snapshot(ctx, shogi.White)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if white.YourTurn || len(white.LegalMoves) != 0 {
		t.Fatalf("the side not to move got %d legal moves", len(white.LegalMoves))
	}
}

// 기보는 보는 사람 기준으로 갈린다. 같은 수가 한쪽에는 you, 다른 쪽에는
// opponent 여야 두 화면이 판을 각자 자기 쪽에서 그린다.
func TestKifuIsWrittenFromEachViewer(t *testing.T) {
	table, _, _ := newTestTable(t, time.Minute)
	ctx := context.Background()

	if _, err := table.Play(ctx, shogi.Black, "7g7f"); err != nil {
		t.Fatalf("play: %v", err)
	}

	black, _ := table.Snapshot(ctx, shogi.Black)
	white, _ := table.Snapshot(ctx, shogi.White)
	if len(black.Moves) != 1 || black.Moves[0].By != SideYou {
		t.Fatalf("black sees %+v, want one move by 'you'", black.Moves)
	}
	if len(white.Moves) != 1 || white.Moves[0].By != SideOpponent {
		t.Fatalf("white sees %+v, want one move by 'opponent'", white.Moves)
	}
	if black.OpponentName != bob.Name || white.OpponentName != alice.Name {
		t.Fatalf("opponent names crossed: black=%q white=%q", black.OpponentName, white.OpponentName)
	}
}

// 投了는 상대의 승리다. 그리고 기록기 둘이 서로 반대의 결과를 받아야 한 판이
// 두 행으로 남았을 때 두 사람의 전적이 맞는다.
func TestResignGivesTheOpponentTheWin(t *testing.T) {
	table, black, white := newTestTable(t, time.Minute)
	ctx := context.Background()

	if _, err := table.Play(ctx, shogi.Black, "7g7f"); err != nil {
		t.Fatalf("play: %v", err)
	}
	snap, err := table.Resign(ctx, shogi.White)
	if err != nil {
		t.Fatalf("resign: %v", err)
	}
	if snap.Status != StatusResigned || snap.Winner != SideOpponent {
		t.Fatalf("the resigning side sees %s/%s, want resigned/opponent", snap.Status, snap.Winner)
	}

	got, err := table.Snapshot(ctx, shogi.Black)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if got.Winner != SideYou {
		t.Fatalf("the other side sees winner=%s, want you", got.Winner)
	}

	_, bResult := black.snapshot()
	_, wResult := white.snapshot()
	if bResult != ResultWin || wResult != ResultLoss {
		t.Fatalf("records say black=%s white=%s, want win/loss", bResult, wResult)
	}
}

// 기보는 양쪽 기록기에 같이 들어간다. 한쪽에만 넣으면 그 판이 두 사람에게
// 다른 판으로 남는다.
func TestBothRecordsGetEveryMove(t *testing.T) {
	table, black, white := newTestTable(t, time.Minute)
	ctx := context.Background()

	for _, usi := range []string{"7g7f", "3c3d", "2g2f"} {
		by := shogi.Black
		if usi == "3c3d" {
			by = shogi.White
		}
		if _, err := table.Play(ctx, by, usi); err != nil {
			t.Fatalf("play %s: %v", usi, err)
		}
	}

	bMoves, _ := black.snapshot()
	wMoves, _ := white.snapshot()
	want := []string{"7g7f", "3c3d", "2g2f"}
	for _, got := range [][]string{bMoves, wMoves} {
		if len(got) != len(want) {
			t.Fatalf("record has %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("record has %v, want %v", got, want)
			}
		}
	}
}

// 시간을 넘기면 진다. 이 하나가 「판이 끝나기는 하는가」의 답이다 — 상대가 탭을
// 닫아도 그 판은 여기서 닫힌다.
func TestRunningOutOfTimeLosesTheGame(t *testing.T) {
	table, black, white := newTestTable(t, 250*time.Millisecond)

	// 한 수는 둬야 판이 있었던 것이다. 0手 판은 승패를 안 만든다(아래 테스트).
	if _, err := table.Play(context.Background(), shogi.Black, "7g7f"); err != nil {
		t.Fatalf("play: %v", err)
	}

	// Done 이 아니라 Finished 를 기다린다. 끝난 판은 한동안 더 답하므로
	// (finishedGrace) Done 은 그만큼 늦게 닫힌다.
	select {
	case <-table.Finished():
	case <-time.After(5 * time.Second):
		t.Fatal("the table never finished after the turn limit")
	}

	// 後手가 응수를 안 했으므로 後手의 시간패다.
	_, bResult := black.snapshot()
	_, wResult := white.snapshot()
	if bResult != ResultWin || wResult != ResultLoss {
		t.Fatalf("records say black=%s white=%s, want win/loss", bResult, wResult)
	}
}

// 한 수도 안 둔 채 시간이 다 되면 승패가 없다(journal §83).
func TestATimeoutWithNoMovesIsNotALoss(t *testing.T) {
	table, black, white := newTestTable(t, 60*time.Millisecond)

	select {
	case <-table.Finished():
	case <-time.After(5 * time.Second):
		t.Fatal("the table never finished after the turn limit")
	}

	_, bResult := black.snapshot()
	_, wResult := white.snapshot()
	if bResult != ResultAbandoned || wResult != ResultAbandoned {
		t.Fatalf("records say black=%s white=%s, want both abandoned", bResult, wResult)
	}

	snap, err := table.Snapshot(context.Background(), shogi.Black)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	// aborted 가 아니라 expired 다. 화면이 할 말이 정반대라 갈라 뒀다 —
	// 저쪽은 「서버 사정」이고 이쪽은 「아무도 안 뒀다」다.
	if snap.Status != StatusExpired || snap.Winner != "" {
		t.Fatalf("the screen sees %s/%q, want expired with no winner", snap.Status, snap.Winner)
	}
}

// 착수는 시계를 다시 시작한다. 안 그러면 두 번째 수부터 남은 시간이 이어져
// 판이 첫 제한시간 안에 통째로 끝난다.
func TestPlayingRestartsTheClock(t *testing.T) {
	table, _, _ := newTestTable(t, 300*time.Millisecond)
	ctx := context.Background()

	time.Sleep(150 * time.Millisecond)
	if _, err := table.Play(ctx, shogi.Black, "7g7f"); err != nil {
		t.Fatalf("play: %v", err)
	}

	snap, err := table.Snapshot(ctx, shogi.White)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	// 방금 다시 시작했으므로 절반보다는 많이 남아 있어야 한다.
	if snap.TurnLeftMs < 200 {
		t.Fatalf("the clock kept running: %dms left of %dms", snap.TurnLeftMs, snap.TurnLimitMs)
	}
}

// 끝난 판도 한동안 답한다. 投了를 받은 쪽이 그 순간 새로고침하는 것은 흔한 일이고,
// 그때 결과 대신 오류가 뜨면 그 사람은 무슨 일이 났는지 모른다.
func TestAFinishedTableStillAnswers(t *testing.T) {
	table, _, _ := newTestTable(t, time.Minute)
	ctx := context.Background()

	if _, err := table.Resign(ctx, shogi.Black); err != nil {
		t.Fatalf("resign: %v", err)
	}

	snaps, unsubscribe, err := table.Subscribe(ctx, shogi.White)
	if err != nil {
		t.Fatalf("subscribing to a finished table: %v", err)
	}
	defer unsubscribe()

	select {
	case snap, ok := <-snaps:
		if !ok {
			t.Fatal("the subscription closed before the result arrived")
		}
		if snap.Status != StatusResigned || snap.Winner != SideYou {
			t.Fatalf("a late viewer sees %s/%s, want resigned/you", snap.Status, snap.Winner)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a late viewer got no snapshot")
	}

	// 그래도 두지는 못한다.
	if _, err := table.Play(ctx, shogi.White, "3c3d"); !errors.Is(err, ErrFinished) {
		t.Fatalf("a finished table accepted a move: %v", err)
	}
}

// 상대가 붙어 있는지가 화면에 나간다. 판은 그 값과 무관하게 돈다 — 나가 있어도
// 시계는 흐른다.
func TestPresenceShowsTheOpponent(t *testing.T) {
	table, _, _ := newTestTable(t, time.Minute)
	ctx := context.Background()

	snap, _ := table.Snapshot(ctx, shogi.Black)
	if snap.OpponentOnline {
		t.Fatal("the opponent is online before anyone subscribed")
	}

	_, unsubscribe, err := table.Subscribe(ctx, shogi.White)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	snap, _ = table.Snapshot(ctx, shogi.Black)
	if !snap.OpponentOnline {
		t.Fatal("the opponent subscribed but does not show as online")
	}

	unsubscribe()
	snap, _ = table.Snapshot(ctx, shogi.Black)
	if snap.OpponentOnline {
		t.Fatal("the opponent still shows as online after leaving")
	}
	if snap.Status != StatusPlaying {
		t.Fatalf("the game ended when a side left: %s", snap.Status)
	}
}
