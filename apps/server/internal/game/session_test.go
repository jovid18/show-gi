package game

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// scriptedOpponent 는 정해진 수를 순서대로 둔다. 엔진 없이 대국 흐름만 본다.
type scriptedOpponent struct {
	moves []string
	i     int
	delay time.Duration
	err   error
}

func (o *scriptedOpponent) Choose(ctx context.Context, _ string, _ []string) (string, error) {
	if o.delay > 0 {
		select {
		case <-time.After(o.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if o.err != nil {
		return "", o.err
	}
	if o.i >= len(o.moves) {
		return "resign", nil
	}
	m := o.moves[o.i]
	o.i++
	return m, nil
}

// legalOpponent 는 합법수 중 첫 번째를 둔다. 끝까지 두는 대국에 쓴다.
type legalOpponent struct{}

func (legalOpponent) Choose(_ context.Context, startSFEN string, moves []string) (string, error) {
	pos, err := shogi.ParseSFEN(startSFEN)
	if err != nil {
		return "", err
	}
	for _, u := range moves {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			return "", err
		}
		pos = pos.Apply(m)
	}
	legal := pos.LegalMoves()
	if len(legal) == 0 {
		return "resign", nil
	}
	return legal[0].USI(), nil
}

func newSession(t *testing.T, cfg Config) *Session {
	t.Helper()
	s, err := New(t.Context(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

// waitFor 는 조건을 만족하는 스냅샷이 올 때까지 구독 채널을 읽는다.
func waitFor(t *testing.T, ch <-chan Snapshot, cond func(Snapshot) bool, what string) Snapshot {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case snap, ok := <-ch:
			if !ok {
				t.Fatalf("%s 전에 세션이 닫힘", what)
			}
			if cond(snap) {
				return snap
			}
		case <-deadline:
			t.Fatalf("%s 대기 시간 초과", what)
		}
	}
}

func TestHumanMoveThenEngineReplies(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	snap, err := s.Play(t.Context(), "7g7f")
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if snap.Ply != 1 || snap.Moves[0].Ja != "▲7六歩" {
		t.Fatalf("사람 착수 반영 안 됨: %+v", snap.Moves)
	}

	got := waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "엔진 응수")
	if got.Moves[1].Ja != "△3四歩" || got.Moves[1].By != SideEngine {
		t.Fatalf("엔진 수 = %+v", got.Moves[1])
	}
	if !got.YourTurn || got.Thinking {
		t.Fatalf("엔진 응수 후에는 사람 차례여야 한다: %+v", got)
	}
	if len(got.LegalMoves) == 0 {
		t.Fatal("사람 차례인데 합법수가 비었다")
	}
}

// 클라이언트가 합법수만 보여줄 수 있어야 한다. 그게 반칙이 실사용자에게 안 닿는 이유다.
func TestSnapshotCarriesLegalMovesOnlyOnHumanTurn(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}, delay: 300 * time.Millisecond}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	snap, _ := s.Snapshot(t.Context())
	if len(snap.LegalMoves) != 30 {
		t.Fatalf("초기 국면 합법수 = %d (30 기대)", len(snap.LegalMoves))
	}

	// 엔진이 생각하는 동안에는 사람 차례가 아니다
	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	thinking, err := s.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if thinking.YourTurn || len(thinking.LegalMoves) != 0 {
		t.Fatalf("엔진 차례인데 합법수를 내보냈다: %+v", thinking)
	}
	if !thinking.Thinking {
		t.Fatal("엔진이 생각 중이라고 표시되지 않음")
	}
}

// 상태를 소유한 goroutine은 엔진이 생각하는 동안에도 막히면 안 된다.
func TestSessionAnswersWhileEngineThinks(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}, delay: time.Second}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}

	start := time.Now()
	if _, err := s.Snapshot(t.Context()); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, err := s.Resign(t.Context()); err != nil {
		t.Fatalf("Resign: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("탐색이 세션을 막았다: %v", elapsed)
	}
}

// 투료로 국면이 바뀐 뒤 도착한 탐색 결과는 그 국면에 대한 답이 아니다. 기보에 붙으면 안 된다.
// 판정 기준은 걸린 시간이 아니라 국면이다 — 오래 걸려도 국면이 그대로면 유효하다.
func TestStaleEngineResultIsDropped(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}, delay: 200 * time.Millisecond}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	snap, err := s.Resign(t.Context())
	if err != nil {
		t.Fatalf("Resign: %v", err)
	}
	if snap.Status != StatusResigned || snap.Winner != SideEngine {
		t.Fatalf("투료 결과 = %+v", snap)
	}

	time.Sleep(400 * time.Millisecond) // 탐색이 끝나고도 남을 시간
	after, err := s.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if after.Ply != 1 {
		t.Fatalf("투료 뒤에 엔진 수가 붙었다: %+v", after.Moves)
	}
	if after.Status != StatusResigned {
		t.Fatalf("상태가 바뀌었다: %s", after.Status)
	}
}

func TestIllegalMoveIsRejectedWithReason(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}}
	s := newSession(t, Config{
		Opponent:   opp,
		HumanColor: shogi.Black,
		StartSFEN:  "k8/9/9/9/9/9/4P4/9/4K4 b P 1",
	})

	_, err := s.Play(t.Context(), "P*5e")
	var ime *shogi.IllegalMoveError
	if !errors.As(err, &ime) {
		t.Fatalf("*IllegalMoveError 기대, got %v", err)
	}
	if ime.Reason != shogi.ReasonNifu {
		t.Fatalf("사유 = %v", ime.Reason)
	}
	if ime.Message() == "" {
		t.Fatal("화면에 내보낼 문구가 비었다")
	}

	// 거절된 수는 기보에 남지 않는다
	snap, _ := s.Snapshot(t.Context())
	if snap.Ply != 0 {
		t.Fatalf("불법수가 기보에 남았다: %+v", snap.Moves)
	}
}

func TestNotYourTurn(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}, delay: 300 * time.Millisecond}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if _, err := s.Play(t.Context(), "2g2f"); !errors.Is(err, ErrNotYourTurn) {
		t.Fatalf("ErrNotYourTurn 기대, got %v", err)
	}
}

// 詰み까지 실제로 두고 끝난다. D2의 완료 기준이 "한 판을 끝까지"다.
func TestGameEndsInCheckmate(t *testing.T) {
	// 후수 玉이 5一, 선수가 金을 5二에 두면 頭金으로 詰み
	opp := &scriptedOpponent{}
	s := newSession(t, Config{
		Opponent:   opp,
		HumanColor: shogi.Black,
		StartSFEN:  "4k4/9/4P4/9/9/9/9/9/4K4 b G 1",
	})

	snap, err := s.Play(t.Context(), "G*5b")
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if snap.Status != StatusCheckmate {
		t.Fatalf("詰み로 끝나야 한다: %+v", snap)
	}
	if snap.Winner != SideHuman {
		t.Fatalf("승자 = %q", snap.Winner)
	}
	if snap.YourTurn || len(snap.LegalMoves) != 0 {
		t.Fatalf("끝난 대국에 착수 여지가 남았다: %+v", snap)
	}
	if _, err := s.Play(t.Context(), "5i4i"); !errors.Is(err, ErrFinished) {
		t.Fatalf("ErrFinished 기대, got %v", err)
	}
}

// 엔진이 둘 수 없는 수를 돌려주면 기보를 깨뜨리지 않고 그 자리에서 끝낸다.
func TestUnplayableEngineMoveEndsGame(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"9i9h"}} // 그 칸에 선수 말이 없다
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	got := waitFor(t, ch, func(s Snapshot) bool { return s.Status != StatusPlaying }, "대국 종료")
	if got.Status != StatusResigned || got.Winner != SideHuman {
		t.Fatalf("결과 = %+v", got)
	}
	if got.Ply != 1 {
		t.Fatalf("둘 수 없는 수가 기보에 남았다: %+v", got.Moves)
	}
}

func TestEngineFailureEndsGame(t *testing.T) {
	opp := &scriptedOpponent{err: errors.New("boom")}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	got := waitFor(t, ch, func(s Snapshot) bool { return s.Status != StatusPlaying }, "대국 종료")
	if got.Winner != SideHuman {
		t.Fatalf("엔진 고장이면 사람이 이긴 것으로 끝난다: %+v", got)
	}
}

func TestEngineMovesFirstWhenHumanIsWhite(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"7g7f"}}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.White})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	got := waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 1 }, "엔진 선착")
	if got.Moves[0].Ja != "▲7六歩" || got.Moves[0].By != SideEngine {
		t.Fatalf("엔진 선착 = %+v", got.Moves[0])
	}
	if !got.YourTurn {
		t.Fatal("엔진이 두고 나면 사람 차례여야 한다")
	}
}

func TestClosedSessionRejectsCommands(t *testing.T) {
	opp := &scriptedOpponent{}
	s, err := New(t.Context(), Config{Opponent: opp, HumanColor: shogi.Black})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.Close()
	s.Close() // 두 번 닫아도 안전해야 한다

	if _, err := s.Snapshot(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("ErrClosed 기대, got %v", err)
	}
}

func TestNewRejectsMissingOpponent(t *testing.T) {
	if _, err := New(t.Context(), Config{}); err == nil {
		t.Fatal("Opponent 없이 세션이 만들어짐")
	}
	if _, err := New(t.Context(), Config{Opponent: legalOpponent{}, StartSFEN: "not-a-sfen"}); err == nil {
		t.Fatal("잘못된 SFEN이 통과함")
	}
}

// 엔진 없이 양쪽을 다 굴려 한 판이 실제로 끝나는지 본다.
func TestGameReachesAnEndWithoutHanging(t *testing.T) {
	s := newSession(t, Config{
		Opponent:   legalOpponent{},
		HumanColor: shogi.Black,
		StartSFEN:  "4k4/9/4P4/9/9/9/9/9/4K4 b G 1",
	})
	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "G*5b"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	got := waitFor(t, ch, func(s Snapshot) bool { return s.Status != StatusPlaying }, "대국 종료")
	if got.Status != StatusCheckmate {
		t.Fatalf("결과 = %+v", got)
	}
}

// engineOpponent 가 SearchResult.Best 를 그대로 돌려주는지.
type stubSearcher struct {
	res usi.SearchResult
	err error
}

func (s stubSearcher) SearchDepth(context.Context, string, []string, int) (usi.SearchResult, error) {
	return s.res, s.err
}

func TestEngineOpponentReturnsBest(t *testing.T) {
	o := NewEngineOpponent(stubSearcher{res: usi.SearchResult{Best: "7g7f"}}, 12)
	got, err := o.Choose(t.Context(), shogi.StartSFEN, nil)
	if err != nil || got != "7g7f" {
		t.Fatalf("Choose = %q, %v", got, err)
	}

	o2 := NewEngineOpponent(stubSearcher{err: errors.New("boom")}, 0)
	if _, err := o2.Choose(t.Context(), shogi.StartSFEN, nil); err == nil {
		t.Fatal("탐색 실패가 전달되지 않음")
	}
}
