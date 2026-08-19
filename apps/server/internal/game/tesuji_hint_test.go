package game

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// forkOneMoveAway 는 한 수 뒤에 両取り가 되는 국면이다.
//
//	6三金 · 4三金   後手
//	6七桂           先手 — 5五로 뛰면 두 金을 동시에 노린다(ふんどしの桂)
//
// 玉을 양쪽 다 넣는 것은 LegalMoves 가 王手 회피를 따지기 때문이다(tag/tesuji_test.go).
const forkOneMoveAway = "8k/9/3g1g3/9/9/9/3N5/9/8K b - 1"

func mustSFEN(t *testing.T, sfen string) shogi.Position {
	t.Helper()
	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		t.Fatalf("SFEN 파싱 실패: %v", err)
	}
	return pos
}

func optionFor(opts []TesujiOption, usiMove string) (TesujiOption, bool) {
	for _, o := range opts {
		if o.USI == usiMove {
			return o, true
		}
	}
	return TesujiOption{}, false
}

// 스캐너가 아직 안 둔 수의 이름을 찾는다. 이것이 착수 후에 이름을 붙이는
// namedTesuji 와 갈리는 지점 전부다.
func TestTesujiOptionsFindsAMoveThatWouldFork(t *testing.T) {
	opts := tesujiOptions(mustSFEN(t, forkOneMoveAway), shogi.Black)

	got, ok := optionFor(opts, "6g5e")
	if !ok {
		t.Fatalf("6g5e 가 ふんどしの桂를 만드는데 후보에 없다: %+v", opts)
	}
	if len(got.Tags) != 1 || got.Tags[0].Code != "fundoshi_no_kei" {
		t.Errorf("ふんどしの桂를 기대했는데 %+v", got.Tags)
	}
}

// 이미 서 있는 형태는 후보가 아니다. 桂를 5五에 미리 놓아 두면 両取り가 이미
// 성립해 있고, 그 국면에서 아무 수나 두는 것이 手筋이 되어서는 안 된다 —
// journal §34 ⑦이 잡은 「두 수 뒤 조용한 수가 이름을 받는다」와 같은 자리다.
func TestTesujiOptionsIgnoresShapesAlreadyOnTheBoard(t *testing.T) {
	const alreadyForking = "8k/9/3g1g3/9/4N4/9/9/9/8K b - 1"

	for _, o := range tesujiOptions(mustSFEN(t, alreadyForking), shogi.Black) {
		for _, tg := range o.Tags {
			if tg.Code == "fundoshi_no_kei" {
				t.Errorf("이미 서 있는 両取り를 %s 가 새로 만든 것으로 셌다", o.USI)
			}
		}
	}
}

// 상대 차례에는 후보가 없다. LegalMoves 가 pos.Turn 쪽 수만 내므로, 갈라 두지
// 않으면 「手筋이 없다」와 「물어볼 차례가 아니다」가 같은 빈 결과가 된다.
func TestTesujiOptionsNeedsItToBeThatColorsTurn(t *testing.T) {
	pos := mustSFEN(t, strings.Replace(forkOneMoveAway, " b ", " w ", 1))

	if opts := tesujiOptions(pos, shogi.Black); len(opts) != 0 {
		t.Errorf("後手 차례인데 先手 후보가 나왔다: %+v", opts)
	}
}

// rootSearch 는 착수 前 국면의 줄들을 돌려준다. 게이트가 한 탐색의 형제 줄을
// 견주므로 착수 후 국면을 따로 흉내낼 것이 없다.
type rootSearch struct {
	lines []usi.SearchLine
	err   error
	sawK  int
}

func (s *rootSearch) SearchMultiPV(_ context.Context, _ string, _ []string, _, multiPV int) (usi.SearchResult, error) {
	s.sawK = multiPV
	if s.err != nil {
		return usi.SearchResult{}, s.err
	}
	return usi.SearchResult{Lines: s.lines}, nil
}

// rootLine 은 뿌리 줄 하나다. 점수는 뿌리에서 수번인 쪽 관점이라 게이트가 부호를
// 안 뒤집는다.
//
// 순위를 받는다. Ranked 가 같은 순위를 하나로 접으므로(중복 제거) 전부 1위로 만들면
// 줄이 한 개로 줄어든다 — adaptive_test.go 의 line 이 그렇게 생겼고, 저쪽은 Lines 를
// 그대로 읽어서 안 걸린다.
func rootLine(rank int, move string, cp int) usi.SearchLine {
	return usi.SearchLine{Depth: 12, MultiPV: rank, Move: move, ScoreCp: cp}
}

func gateOne(t *testing.T, s MultiSearcher, opts []TesujiOption) ([]TesujiOption, int) {
	t.Helper()
	return gateOneK(t, s, TesujiHintRootK, opts)
}

// gateOneK 는 k를 짚어 준다. 줄 밖을 확정 탈락으로 말하려면 k줄을 다 받아야 하므로
// (gateTesujiOptions), 줄을 몇 개만 세우는 테스트는 k도 그만큼으로 줘야 그 국면이 된다.
func gateOneK(t *testing.T, s MultiSearcher, k int, opts []TesujiOption) ([]TesujiOption, int) {
	t.Helper()
	kept, dropped, err := gateTesujiOptions(t.Context(), s, 12, k, shogi.StartSFEN, nil, opts, shogi.Black)
	if err != nil {
		t.Fatalf("gateTesujiOptions: %v", err)
	}
	return kept, dropped
}

var oneOption = []TesujiOption{{USI: "6g5e"}}

// 잃는 수에는 이름을 안 붙인다. 최선 줄이 +100 인데 이 수의 줄이 −200 이므로
// 낙폭 300cp — TesujiLossCp 를 넘는다.
func TestGateDropsAMoveTheEngineCallsALoss(t *testing.T) {
	s := &rootSearch{lines: []usi.SearchLine{
		rootLine(1, "7g7f", 100),
		rootLine(2, "6g5e", -200),
	}}

	if kept, _ := gateOne(t, s, oneOption); len(kept) != 0 {
		t.Errorf("낙폭 300cp 인데 이름이 붙었다: %+v", kept)
	}
}

// 성립하는 捨て駒는 살린다. 낙폭이 상한 안이면 통과다 — 腹銀처럼 잡히는 것이
// 정상인 寄せ 手筋을 죽이지 않는 것이 이 게이트의 값이다(tesuji.go 의 enginePaidOff).
func TestGateKeepsAMoveWithinTheLossCap(t *testing.T) {
	s := &rootSearch{lines: []usi.SearchLine{
		rootLine(1, "7g7f", 100),
		rootLine(2, "6g5e", 0), // 낙폭 100cp — 상한과 같다
	}}

	kept, _ := gateOne(t, s, oneOption)
	if len(kept) != 1 || kept[0].USI != "6g5e" {
		t.Errorf("낙폭이 상한과 같은데 떨어졌다: %+v", kept)
	}
	if s.sawK != TesujiHintRootK {
		t.Errorf("MultiPV %d 를 요청해야 한다: %d", TesujiHintRootK, s.sawK)
	}
}

// 後手로 잡은 판에서도 같은 방향이다. senteCp·cpFor 가 한 번씩 도는 자리라,
// 부호가 뒤집히면 지는 수에 이름이 붙는다 — 에러가 안 나고 조용하다(tesuji.go).
func TestGateReadsTheLossFromThePlayersSide(t *testing.T) {
	s := &rootSearch{lines: []usi.SearchLine{
		rootLine(1, "7g7f", 100),
		rootLine(2, "6g5e", -200),
	}}

	kept, _, err := gateTesujiOptions(t.Context(), s, 12, TesujiHintRootK, shogi.StartSFEN, nil, oneOption, shogi.White)
	if err != nil {
		t.Fatalf("gateTesujiOptions: %v", err)
	}
	if len(kept) != 0 {
		t.Errorf("後手 관점에서도 낙폭 300cp 다: %+v", kept)
	}
}

// Lines[0] 을 최선으로 읽지 않는다. 아직 안 온 순위가 빈 줄로 남으므로, 그것을
// 그대로 1위로 쓰면 최선이 0cp가 되고 낙폭이 통째로 어긋난다(usi.SearchResult.Ranked).
func TestGateIgnoresAnEmptyRank(t *testing.T) {
	s := &rootSearch{lines: []usi.SearchLine{
		{MultiPV: 1}, // 안 온 순위
		rootLine(2, "7g7f", -800),
		rootLine(3, "6g5e", -850),
	}}

	kept, _ := gateOne(t, s, oneOption)
	if len(kept) != 1 {
		t.Errorf("최선(−800) 대비 낙폭 50cp 인데 떨어졌다: %+v", kept)
	}
}

// 모르면 이름을 붙이지 않는다. 엔진이 없으면 룰만 남는데, 룰만으로 통과시키면
// 게이트가 없는 것과 같아진다.
func TestGateWithoutASearcherNamesNothing(t *testing.T) {
	if kept, _ := gateOne(t, nil, oneOption); len(kept) != 0 {
		t.Errorf("탐색기가 없는데 이름이 붙었다: %+v", kept)
	}
}

// 줄 밖이라고 다 「못 본 것」은 아니다. 마지막 줄이 이미 상한 밖이면 그보다 나쁜
// 것들은 확정 탈락이고, 안이면 모르는 것이다. 둘을 같은 침묵으로 섞으면
// 「手筋이 없었다」와 「못 봤다」가 같은 화면이 된다.
func TestGateCountsOnlyTheCandidatesItCouldNotDecide(t *testing.T) {
	outside := []TesujiOption{{USI: "1a1b"}, {USI: "2a2b"}}

	t.Run("마지막 줄이 이미 상한 밖이면 확정 탈락", func(t *testing.T) {
		s := &rootSearch{lines: []usi.SearchLine{
			rootLine(1, "7g7f", 0),
			rootLine(2, "8g8f", -500), // 낙폭 500cp — 그 밖은 더 나쁘다
		}}

		kept, dropped := gateOneK(t, s, 2, outside)
		if len(kept) != 0 {
			t.Errorf("줄에 없는 후보가 통과했다: %+v", kept)
		}
		if dropped != 0 {
			t.Errorf("확정 탈락을 「못 본 것」으로 셌다: %d", dropped)
		}
	})

	t.Run("k줄을 다 못 받았으면 밖은 모르는 것", func(t *testing.T) {
		// 상한 밖인 마지막 줄이지만 k=8 중 두 줄만 왔다. 안 온 순위가 그 사이에
		// 있을 수 있어서 「밖은 더 나쁘다」가 성립하지 않는다.
		s := &rootSearch{lines: []usi.SearchLine{
			rootLine(1, "7g7f", 0),
			rootLine(2, "8g8f", -500),
		}}

		if _, dropped := gateOne(t, s, outside); dropped != len(outside) {
			t.Errorf("덜 받은 줄로 확정 탈락이라고 했다: dropped %d", dropped)
		}
	})

	t.Run("마지막 줄이 아직 상한 안이면 모르는 것", func(t *testing.T) {
		s := &rootSearch{lines: []usi.SearchLine{
			rootLine(1, "7g7f", 0),
			rootLine(2, "8g8f", -50), // 낙폭 50cp — 그 밖에 통과할 것이 남아 있을 수 있다
		}}

		kept, dropped := gateOneK(t, s, 2, outside)
		if len(kept) != 0 {
			t.Errorf("모르는 후보에 이름이 붙었다: %+v", kept)
		}
		if dropped != len(outside) {
			t.Errorf("못 본 후보 %d 개를 기대했는데 %d", len(outside), dropped)
		}
	})
}

// 이름은 중복 없이 편다. 같은 이름을 만드는 수가 둘이어도 알릴 것은 하나다.
func TestTesujiHintTagsAreDeduped(t *testing.T) {
	pos := mustSFEN(t, forkOneMoveAway)
	opts := tesujiOptions(pos, shogi.Black)
	opts = append(opts, opts...) // 같은 후보를 두 벌로

	if got := tesujiHintTags(opts); len(got) != 1 {
		t.Errorf("이름 하나를 기대했는데 %+v", got)
	}
}

// countingSearch 는 몇 번 불렸는지만 센다. 통과 여부는 여기서 볼 것이 아니다.
type countingSearch struct {
	mu    sync.Mutex
	calls int
}

func (s *countingSearch) SearchMultiPV(context.Context, string, []string, int, int) (usi.SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return usi.SearchResult{}, nil // 줄이 없다 — 여기서 보는 것은 몇 번 물었나뿐이다
}

func (s *countingSearch) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// 쿨다운은 「물어본 자리」에서 잰다. 띄운 자리에서만 재면 게이트가 한 번도 안 열리는
// 판에서 이 탐색이 사람 차례마다 돌고, 그 판이 실제로 멈췄다(journal §56).
func TestTesujiHintGateWaitsForTheCooldownEvenAfterAMiss(t *testing.T) {
	search := &countingSearch{}
	st := &state{
		cfg:    Config{TesujiHint: search, HumanColor: shogi.Black},
		status: StatusPlaying,
		pos:    mustSFEN(t, forkOneMoveAway),
	}
	done := make(chan tesujiHintResult, 4)

	// 안 물어본 회차는 기다릴 것이 없다. tesujiHinting 이 곧 「지금 띄웠다」다.
	ask := func(ply int) {
		t.Helper()
		st.moves = make([]Move, ply)
		st.searchGen++
		st.maybeTesujiHint(t.Context(), done)
		if st.tesujiHinting {
			st.applyTesujiHint(<-done)
		}
	}

	ask(0) // 先手의 첫 차례. 0手目라 「아직 안 물어봤다」와 겹치던 자리다
	first := search.count()
	if first == 0 {
		t.Fatal("첫 회차부터 안 물어봤다 — 이 국면에는 새 이름이 생기는 수가 있다")
	}

	// 쿨다운 안에서 사람 차례가 다시 온다. 국면은 움직이지만 手数는 아직 멀다.
	for ply := 2; ply < TagHintCooldown; ply += 2 {
		ask(ply)
	}
	if got := search.count(); got != first {
		t.Fatalf("쿨다운 안에서 %d번 더 물어봤다 (%d → %d)", got-first, first, got)
	}

	// 쿨다운을 넘기면 다시 묻는다. 안 그러면 이 방향이 힌트를 아예 죽인 것이 된다.
	ask(TagHintCooldown)
	if search.count() == first {
		t.Fatal("쿨다운을 넘겼는데 다시 안 물어봤다")
	}
}

func hasTag(tags []tag.Tag, code string) bool {
	for _, t := range tags {
		if t.Code == code {
			return true
		}
	}
	return false
}

// 세션 끝에서 본다 — 사람 차례가 되면 手筋 이름이 스냅샷에 실려 나간다.
//
// 비동기라 첫 스냅샷에는 없고 몇 밀리초 뒤에 합류한다. 그것이 waitFor 를 쓰는 이유다.
func TestSessionAnnouncesATesujiThePlayerCouldMake(t *testing.T) {
	search := &rootSearch{lines: []usi.SearchLine{
		rootLine(1, "6g5e", 0), // 최선 줄이 곧 手筋 — 낙폭 0
	}}
	s := newSession(t, Config{
		Opponent:   legalOpponent{},
		HumanColor: shogi.Black,
		StartSFEN:  forkOneMoveAway,
		TesujiHint: search,
	})

	ch, cancel, err := s.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	waitFor(t, ch, func(snap Snapshot) bool {
		return hasTag(snap.TagHints, "fundoshi_no_kei")
	}, "ふんどしの桂 힌트")
}

// 탐색기가 없으면 手筋 힌트가 꺼진다. 대국은 그대로 서야 한다 — 엔진·DB가 없을 때와
// 같은 판단이고, 룰만으로 이름을 붙이면 게이트가 없는 것과 같아진다.
func TestSessionWithoutASearcherStaysQuietAboutTesuji(t *testing.T) {
	s := newSession(t, Config{
		Opponent:   legalOpponent{},
		HumanColor: shogi.Black,
		StartSFEN:  forkOneMoveAway,
	})

	snap, err := s.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if hasTag(snap.TagHints, "fundoshi_no_kei") {
		t.Errorf("탐색기가 없는데 手筋 이름이 나갔다: %+v", snap.TagHints)
	}
	if snap.Status != StatusPlaying {
		t.Errorf("手筋 힌트가 없다고 대국이 멈췄다: %v", snap.Status)
	}
}

// 계단 ②③ — 열려 있는 계단이 手筋을 짚는다. 단계 값은 갇힘 힌트의 것을 그대로 쓴다.
func TestOpenLadderPointsAtTheTesuji(t *testing.T) {
	newState := func(stuck int) *state {
		return &state{
			stuck:      stuck,
			hint:       &Hint{Square: "9i"}, // 최선수로 지어진 계단
			tesujiOpts: []TesujiOption{{USI: "6g5e"}},
		}
	}

	t.Run("3회면 駒까지", func(t *testing.T) {
		st := newState(HintPieceAfter)
		st.pointHintAtTesuji()

		if st.hint.Square != "6g" {
			t.Errorf("手筋 駒의 칸을 기대했는데 %q", st.hint.Square)
		}
		if st.hint.USI != "" {
			t.Errorf("아직 수를 내려보내면 안 된다: %q", st.hint.USI)
		}
	})

	t.Run("5회면 수까지", func(t *testing.T) {
		st := newState(HintMoveAfter)
		st.pointHintAtTesuji()

		if st.hint.USI != "6g5e" {
			t.Errorf("手筋 수를 기대했는데 %q", st.hint.USI)
		}
	})

	t.Run("계단이 안 열렸으면 안 만든다", func(t *testing.T) {
		st := newState(HintPieceAfter)
		st.hint = nil
		st.pointHintAtTesuji()

		if st.hint != nil {
			t.Errorf("계단이 없는데 지어졌다: %+v", st.hint)
		}
	})
}
