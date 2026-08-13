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

// forkOneMoveAway 는 **한 수 뒤에** 両取り가 되는 국면이다.
//
//	6三金 · 4三金   後手
//	6七桂           先手 — 5五로 뛰면 두 金을 동시에 노린다(ふんどしの桂)
//
// 玉을 양쪽 다 넣는 것은 `LegalMoves` 가 王手 회피를 따지기 때문이다(tag/tesuji_test.go).
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

// 스캐너가 **아직 안 둔 수**의 이름을 찾는다. 이것이 착수 후에 이름을 붙이는
// `namedTesuji` 와 갈리는 지점 전부다.
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

// **이미 서 있는 형태는 후보가 아니다.** 桂를 5五에 미리 놓아 두면 両取り가 이미
// 성립해 있고, 그 국면에서 아무 수나 두는 것이 手筋이 되어서는 안 된다 —
// 06-status.md §34 ⑦이 잡은 「두 수 뒤 조용한 수가 이름을 받는다」와 같은 자리다.
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

// **상대 차례에는 후보가 없다.** `LegalMoves` 가 `pos.Turn` 쪽 수만 내므로, 갈라 두지
// 않으면 「手筋이 없다」와 「물어볼 차례가 아니다」가 같은 빈 결과가 된다.
func TestTesujiOptionsNeedsItToBeThatColorsTurn(t *testing.T) {
	pos := mustSFEN(t, strings.Replace(forkOneMoveAway, " b ", " w ", 1))

	if opts := tesujiOptions(pos, shogi.Black); len(opts) != 0 {
		t.Errorf("後手 차례인데 先手 후보가 나왔다: %+v", opts)
	}
}

// scriptedSearch 는 **수순 길이별로** 답을 돌려준다. 착수 전 국면과 후보를 둔 뒤 국면이
// 서로 다른 답을 줘야 게이트를 실제로 재는 것이 된다 — `stubMulti` 는 늘 같은 답이다.
type scriptedSearch struct {
	before usi.SearchResult
	after  map[string]usi.SearchResult
	err    error
	sawK   int
}

func (s *scriptedSearch) SearchMultiPV(_ context.Context, _ string, moves []string, _, multiPV int) (usi.SearchResult, error) {
	s.sawK = multiPV
	if s.err != nil {
		return usi.SearchResult{}, s.err
	}
	if len(moves) == 0 {
		return s.before, nil
	}
	return s.after[moves[len(moves)-1]], nil
}

func gateOne(t *testing.T, s MultiSearcher, opts []TesujiOption) ([]TesujiOption, int) {
	t.Helper()
	kept, dropped, err := gateTesujiOptions(t.Context(), s, 12, shogi.StartSFEN, nil, opts, shogi.Black)
	if err != nil {
		t.Fatalf("gateTesujiOptions: %v", err)
	}
	return kept, dropped
}

var oneOption = []TesujiOption{{USI: "6g5e"}}

// **잃는 수에는 이름을 안 붙인다.** 先手 관점으로 +100 이던 국면이 착수 뒤 −200 이
// 되었으므로 낙폭 300cp — `TesujiLossCp` 를 넘는다.
//
// 착수 후 값은 **상대 관점**으로 들어온다. 부호를 한 번 뒤집는 자리가 여기다.
func TestGateDropsAMoveTheEngineCallsALoss(t *testing.T) {
	s := &scriptedSearch{
		before: usi.SearchResult{ScoreCp: 100},
		after:  map[string]usi.SearchResult{"6g5e": {ScoreCp: 200}}, // 상대가 +200 = 내가 −200
	}

	if kept, _ := gateOne(t, s, oneOption); len(kept) != 0 {
		t.Errorf("낙폭 300cp 인데 이름이 붙었다: %+v", kept)
	}
}

// **성립하는 捨て駒는 살린다.** 낙폭이 상한 안이면 통과다 — 腹銀처럼 잡히는 것이
// 정상인 寄せ 手筋을 죽이지 않는 것이 이 게이트의 값이다(tesuji.go 의 `enginePaidOff`).
func TestGateKeepsAMoveWithinTheLossCap(t *testing.T) {
	s := &scriptedSearch{
		before: usi.SearchResult{ScoreCp: 100},
		after:  map[string]usi.SearchResult{"6g5e": {ScoreCp: -100}}, // 내가 여전히 +100
	}

	kept, _ := gateOne(t, s, oneOption)
	if len(kept) != 1 || kept[0].USI != "6g5e" {
		t.Errorf("낙폭 0cp 인데 떨어졌다: %+v", kept)
	}
	if s.sawK != TesujiHintMultiPV {
		t.Errorf("MultiPV %d 를 요청해야 한다: %d", TesujiHintMultiPV, s.sawK)
	}
}

// **모르면 이름을 붙이지 않는다.** 엔진이 없으면 룰만 남는데, 룰만으로 통과시키면
// 게이트가 없는 것과 같아진다.
func TestGateWithoutASearcherNamesNothing(t *testing.T) {
	if kept, _ := gateOne(t, nil, oneOption); len(kept) != 0 {
		t.Errorf("탐색기가 없는데 이름이 붙었다: %+v", kept)
	}
}

// **자른 것을 센다.** 안 세면 「手筋이 없었다」와 「못 봤다」가 같은 화면이 된다.
func TestGateReportsCandidatesItNeverAsked(t *testing.T) {
	var many []TesujiOption
	for _, u := range []string{"1a1b", "2a2b", "3a3b", "4a4b", "5a5b", "6a6b", "7a7b", "8a8b"} {
		many = append(many, TesujiOption{USI: u})
	}

	s := &scriptedSearch{before: usi.SearchResult{ScoreCp: 0}, after: map[string]usi.SearchResult{}}
	kept, dropped := gateOne(t, s, many)

	if want := len(many) - TesujiHintMaxCandidates; dropped != want {
		t.Errorf("자른 후보 %d 개를 기대했는데 %d", want, dropped)
	}
	if len(kept) > TesujiHintMaxCandidates {
		t.Errorf("상한을 넘겨 물었다: %d", len(kept))
	}
}

// 이름은 **중복 없이** 편다. 같은 이름을 만드는 수가 둘이어도 알릴 것은 하나다.
func TestTesujiHintTagsAreDeduped(t *testing.T) {
	pos := mustSFEN(t, forkOneMoveAway)
	opts := tesujiOptions(pos, shogi.Black)
	opts = append(opts, opts...) // 같은 후보를 두 벌로

	if got := tesujiHintTags(opts); len(got) != 1 {
		t.Errorf("이름 하나를 기대했는데 %+v", got)
	}
}

// countingSearch 는 게이트를 늘 통과시키면서 **몇 번 불렸는지**만 센다.
type countingSearch struct {
	mu    sync.Mutex
	calls int
}

func (s *countingSearch) SearchMultiPV(context.Context, string, []string, int, int) (usi.SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	return usi.SearchResult{ScoreCp: 0}, nil // 낙폭 0 — 언제나 통과
}

func (s *countingSearch) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// **쿨다운은 「물어본 자리」에서 잰다.** 띄운 자리에서만 재면 게이트가 한 번도 안 열리는
// 판에서 이 탐색이 사람 차례마다 돌고, 그 판이 실제로 멈췄다(06-status.md §56).
func TestTesujiHintGateWaitsForTheCooldownEvenAfterAMiss(t *testing.T) {
	search := &countingSearch{}
	st := &state{
		cfg:    Config{TesujiHint: search, HumanColor: shogi.Black},
		status: StatusPlaying,
		pos:    mustSFEN(t, forkOneMoveAway),
	}
	done := make(chan tesujiHintResult, 4)

	// **안 물어본 회차는 기다릴 것이 없다.** `tesujiHinting` 이 곧 「지금 띄웠다」다.
	ask := func(ply int) {
		t.Helper()
		st.moves = make([]Move, ply)
		st.searchGen++
		st.maybeTesujiHint(t.Context(), done)
		if st.tesujiHinting {
			st.applyTesujiHint(<-done)
		}
	}

	ask(0) // 先手의 첫 차례. **0手目라 「아직 안 물어봤다」와 겹치던 자리다**
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

// 세션 끝에서 본다 — 사람 차례가 되면 手筋 이름이 **스냅샷에 실려 나간다.**
//
// 비동기라 첫 스냅샷에는 없고 몇 밀리초 뒤에 합류한다. 그것이 `waitFor` 를 쓰는 이유다.
func TestSessionAnnouncesATesujiThePlayerCouldMake(t *testing.T) {
	search := &scriptedSearch{
		before: usi.SearchResult{ScoreCp: 0},
		after:  map[string]usi.SearchResult{"6g5e": {ScoreCp: 0}}, // 낙폭 0 — 게이트 통과
	}
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

// **탐색기가 없으면 手筋 힌트가 꺼진다.** 대국은 그대로 서야 한다 — 엔진·DB가 없을 때와
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

// 계단 ②③ — 열려 있는 계단이 **手筋을 짚는다.** 단계 값은 갇힘 힌트의 것을 그대로 쓴다.
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
