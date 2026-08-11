package game

import (
	"context"
	"errors"
	"os"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// scriptedMate 는 정해진 手数의 詰み을 돌려준다. solver 없이 게이지 배선만 본다.
//
// **무엇을 물었는지도 남긴다.** 게이지의 요점이 「어느 국면을 묻는가」라서, 세기만
// 확인하면 한 수 어긋난 국면을 물어도 테스트가 초록으로 남는다.
type scriptedMate struct {
	mu    sync.Mutex
	plies int
	err   error
	delay time.Duration
	asked [][]string
}

func (m *scriptedMate) SearchMate(ctx context.Context, _ string, moves []string) (usi.MateResult, error) {
	m.mu.Lock()
	plies, err, delay := m.plies, m.err, m.delay
	m.asked = append(m.asked, slices.Clone(moves))
	m.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return usi.MateResult{}, ctx.Err()
		}
	}
	if err != nil {
		return usi.MateResult{}, err
	}
	line := make([]string, plies)
	for i := range line {
		line[i] = "1a1b" + strconv.Itoa(i) // 길이만 쓰인다
	}
	return usi.MateResult{Moves: line, Proven: true}, nil
}

func (m *scriptedMate) lastAsked() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.asked) == 0 {
		return nil
	}
	return m.asked[len(m.asked)-1]
}

func (m *scriptedMate) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.asked)
}

// mateInOneSFEN 은 **사람(先手)에게 1手詰이 있는** 국면이다. `G*5b` 하나로 끝난다.
//
// 玉5a의 도망 칸은 4a·6a가 자기 香로 막혀 있고 4b·6b는 打った 金이 덮는다. 金을 玉으로
// 되따지 못하는 것은 4三의 銀이 5b를 받치기 때문이고, 香는 直進뿐이라 5b에 못 닿는다.
//
// **銀으로 받치는 것이 요점이다.** 처음에 5筋의 香로 받쳤더니 그 香가 玉에게도 닿아
// 「先手 차례인데 後手가 이미 王手를 받는」 국면이 됐다. 아래 테스트가 그것을 잡는다.
const mateInOneSFEN = "3lkl3/9/5S3/9/9/9/9/9/8K b G 1"

// 위 국면이 **정말로 1手詰인지**를 엔진 없이 먼저 못박는다.
//
// 손으로 만든 국면이라 이 확인이 없으면, 게이지가 안 켜졌을 때 배선이 틀린 건지 국면이
// 틀린 건지를 못 가른다 — 囲い 좌표를 룰 엔진으로 재검증하는 것과 같은 자리다(09-tags.md §1).
func TestMateInOneSFENReallyIsMateInOne(t *testing.T) {
	pos, err := shogi.ParseSFEN(mateInOneSFEN)
	if err != nil {
		t.Fatalf("ParseSFEN: %v", err)
	}
	if pos.Turn != shogi.Black {
		t.Fatalf("先手 차례여야 한다, got %v", pos.Turn)
	}
	if pos.InCheck(shogi.White) {
		t.Fatal("先手 차례인데 後手가 王手를 받고 있으면 애초에 성립하지 않는 국면이다")
	}

	m, err := shogi.ParseUSIMove("G*5b")
	if err != nil {
		t.Fatalf("ParseUSIMove: %v", err)
	}
	if err := pos.ValidateMove(m); err != nil {
		t.Fatalf("G*5b 가 합법수여야 한다: %v", err)
	}

	after := pos.Apply(m)
	if !after.InCheck(shogi.White) {
		t.Fatal("G*5b 는 王手여야 한다")
	}
	if !after.NoLegalMoves() {
		t.Fatal("G*5b 뒤에 後手의 합법수가 없어야 한다 — 1手詰이 아니다")
	}
}

// 手数가 세기로 옮겨지는 자리. 구간은 01-core.md §7의 「11→7→5→3→1」이다.
func TestMateHeatBuckets(t *testing.T) {
	cases := []struct {
		plies int
		want  int
	}{
		{0, 0}, // 詰み 없음 — 게이지가 꺼진다
		{1, 5},
		{3, 4},
		{5, 3},
		{7, 2},
		{9, 1},
		{11, 1},
		{99, 1}, // DepthLimit 을 환경변수로 올려도 답이 있어야 한다
	}
	for _, c := range cases {
		if got := mateHeat(c.plies); got != c.want {
			t.Errorf("mateHeat(%d) = %d, want %d", c.plies, got, c.want)
		}
	}
	if mateHeat(1) != MateHeatMax {
		t.Errorf("가장 가까운 詰み이 상한이어야 한다: mateHeat(1)=%d, MateHeatMax=%d", mateHeat(1), MateHeatMax)
	}
}

// 사람이 先手면 첫 수 전부터 게이지가 걸린다.
func TestMateGaugeLightsOnPlayerTurn(t *testing.T) {
	mate := &scriptedMate{plies: 3}
	s := newSession(t, Config{Opponent: &scriptedOpponent{}, HumanColor: shogi.Black, Mate: mate})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	snap := waitFor(t, ch, func(s Snapshot) bool { return s.MateHeat > 0 }, "게이지가 켜지기")
	if snap.MateHeat != 4 {
		t.Errorf("3手詰이면 세기가 4여야 한다, got %d", snap.MateHeat)
	}
	if got := mate.lastAsked(); len(got) != 0 {
		t.Errorf("初期局面을 물어야 한다, got %v", got)
	}
}

// **게이지가 묻는 것은 「지금」 국면이다.** 판정이 착수 **전** 국면을 묻는 것과 다른 자리라,
// 여기가 어긋나면 화면이 한 수 낡은 불꽃을 그린다.
func TestMateGaugeAsksTheCurrentPosition(t *testing.T) {
	mate := &scriptedMate{plies: 1}
	opp := &scriptedOpponent{moves: []string{"3c3d"}}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black, Mate: mate})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}

	// 상대가 두어 사람 차례로 돌아온 뒤의 게이지.
	waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 && s.MateHeat > 0 }, "두 수 뒤 게이지")

	want := []string{"7g7f", "3c3d"}
	if got := mate.lastAsked(); !slices.Equal(got, want) {
		t.Errorf("현재 국면을 물어야 한다: got %v, want %v", got, want)
	}
}

// 상대가 생각하는 동안에는 게이지가 꺼진다 — 그 세기는 이미 지나간 국면의 것이다.
func TestMateGaugeGoesDarkWhileOpponentThinks(t *testing.T) {
	mate := &scriptedMate{plies: 1}
	opp := &scriptedOpponent{moves: []string{"3c3d"}, delay: 300 * time.Millisecond}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black, Mate: mate})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	waitFor(t, ch, func(s Snapshot) bool { return s.MateHeat > 0 }, "첫 게이지")

	snap, err := s.Play(t.Context(), "7g7f")
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if snap.MateHeat != 0 {
		t.Errorf("착수 직후에는 게이지가 꺼져야 한다, got %d", snap.MateHeat)
	}
	if !snap.Thinking {
		t.Fatal("상대가 생각 중이어야 하는 국면이다")
	}
}

// solver 가 실패해도 대국은 그대로 간다. 테두리만 어두운 채로 남는다.
func TestMateGaugeFailureDoesNotStopTheGame(t *testing.T) {
	mate := &scriptedMate{err: errors.New("solver died")}
	opp := &scriptedOpponent{moves: []string{"3c3d"}}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black, Mate: mate})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	snap := waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "상대의 응수")
	if snap.MateHeat != 0 {
		t.Errorf("실패했으면 게이지가 꺼져 있어야 한다, got %d", snap.MateHeat)
	}
}

// Mate 가 없으면 게이지 없이 대국한다 — 엔진·DB와 같은 판단이다.
func TestNoMateSearcherMeansNoGauge(t *testing.T) {
	opp := &scriptedOpponent{moves: []string{"3c3d"}}
	s := newSession(t, Config{Opponent: opp, HumanColor: shogi.Black})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	snap := waitFor(t, ch, func(s Snapshot) bool { return s.Ply == 2 }, "상대의 응수")
	if snap.MateHeat != 0 {
		t.Errorf("solver 가 없으면 게이지가 꺼져 있어야 한다, got %d", snap.MateHeat)
	}
}

// 실제 solver 로 게이지가 끝까지 도는가.
//
// **가짜로는 안 잡히는 것이 하나 있다** — solver 가 「수번 측의 詰み」을 답한다는 전제다.
// 그 전제가 틀리면 게이지는 내 玉이 위험할 때 켜지는, 정확히 반대의 물건이 된다.
// `SHOWGI_MATE_CMD` 가 없으면 건너뛴다 — CI 러너에는 엔진이 없다(README).
//
//	SHOWGI_MATE_CMD=/opt/yaneuraou/run-mate go test ./internal/game/ -run RealMateEngine -v
func TestMateGaugeAgainstRealMateEngine(t *testing.T) {
	cmd := os.Getenv("SHOWGI_MATE_CMD")
	if cmd == "" {
		t.Skip("SHOWGI_MATE_CMD 미설정 — 실 solver 검증 건너뜀")
	}
	pool, err := usi.NewPool(1, cmd, map[string]string{"USI_Hash": "128", "Threads": "1", "DepthLimit": "11"})
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	defer pool.Close()

	s := newSession(t, Config{
		Opponent:   legalOpponent{},
		HumanColor: shogi.Black,
		StartSFEN:  mateInOneSFEN,
		Mate:       pool,
	})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	snap := waitFor(t, ch, func(s Snapshot) bool { return s.MateHeat > 0 }, "실 solver 게이지")
	if snap.MateHeat != MateHeatMax {
		t.Errorf("1手詰이면 세기가 상한이어야 한다: got %d, want %d", snap.MateHeat, MateHeatMax)
	}
}

// 되물러 사람 차례로 돌아오면 게이지도 그 국면의 것으로 다시 걸린다.
//
// 물러진 수를 둔 국면의 세기가 그대로 남으면, 판에 오지도 않은 수의 불꽃을 그리게 된다.
func TestMateGaugeIsAskedAgainAfterRollback(t *testing.T) {
	mate := &scriptedMate{plies: 5}
	opp := &scriptedOpponent{moves: []string{"3c3d"}}
	s := newSession(t, Config{
		Opponent:   opp,
		HumanColor: shogi.Black,
		Mate:       mate,
		Analyst:    &fixedAnalyst{verdict: blunder()},
	})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	waitFor(t, ch, func(s Snapshot) bool { return s.MateHeat > 0 }, "첫 게이지")
	before := mate.count()

	if _, err := s.Play(t.Context(), "7g7f"); err != nil {
		t.Fatalf("Play: %v", err)
	}
	snap := waitFor(t, ch, func(s Snapshot) bool { return s.Intervention != nil }, "되무르기")
	if snap.Ply != 0 {
		t.Fatalf("물러졌으면 초기 국면이어야 한다, ply=%d", snap.Ply)
	}

	waitFor(t, ch, func(s Snapshot) bool { return s.MateHeat > 0 }, "되무른 뒤의 게이지")
	if got := mate.count(); got <= before {
		t.Errorf("되무른 뒤에 다시 물어야 한다: %d → %d", before, got)
	}
	if got := mate.lastAsked(); len(got) != 0 {
		t.Errorf("되돌아온 국면을 물어야 한다, got %v", got)
	}
}
