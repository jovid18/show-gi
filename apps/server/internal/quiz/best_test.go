package quiz

import (
	"context"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// gameMoves 는 사람(先手)이 넷의 차례를 갖는 짧은 판이다. 문항 후보가 되는 자리는
// `i ∈ {2,4,6,8}` — `i` 는 그 국면까지 둔 手数이고, `i=0` 은 앞의 평가치가 없어 빠진다.
var gameMoves = []string{"7g7f", "3c3d", "2g2f", "8c8d", "6g6f", "4c4d", "3g3f", "7c7d", "5g5f"}

// fakeSearch 는 국면마다 미리 정한 후보를 돌려준다. **엔진 없이 문항 고르기를 본다.**
type fakeSearch struct {
	lines map[string][]usi.SearchLine
	// asked 는 실제로 물어본 국면이다. 「엔진을 쓰기 **전에** 걸러졌는가」를 보는 자리다.
	asked map[string]bool
}

func (f *fakeSearch) SearchMultiPV(
	_ context.Context, startSFEN string, _ []string, _, _ int,
) (usi.SearchResult, error) {
	if f.asked == nil {
		f.asked = make(map[string]bool)
	}
	f.asked[startSFEN] = true
	return usi.SearchResult{Lines: f.lines[startSFEN]}, nil
}

// line 은 후보 하나다. cp는 수번 관점이고, 그 국면의 수번이 사람이라 곧 사람 관점이다.
func line(move string, cp int) usi.SearchLine {
	return usi.SearchLine{Move: move, ScoreCp: cp, PV: []string{move}}
}

func mateLine(move string, in int) usi.SearchLine {
	return usi.SearchLine{Move: move, ScoreCp: usi.MateCp - 10*in, IsMate: true, MateIn: in, PV: []string{move}}
}

// gameInput 은 위 수순으로 만든 Input 이다. 평가치는 전 手数에 있고 낙폭은 手数가 늦을수록 크다 —
// 후보를 고르는 순서를 시험이 알고 있어야 한다.
func gameInput() Input {
	evals := make([]*int, len(gameMoves))
	for i := range evals {
		cp := -20 * i // 先手 관점으로 계속 나빠진다 = 사람의 낙폭이 매 수 20cp
		evals[i] = &cp
	}
	return Input{Moves: gameMoves, Human: shogi.Black, EvalCp: evals}
}

// build 는 「최선수는?」 문항만 뽑는다. `Build` 가 값 둘을 주므로 시험이 매번 풀어 쓰지
// 않도록 여기서 한 번만 편다.
func build(fs MultiSearcher, in Input) ([]BestItem, bool) {
	q, complete := NewBuilder(nil, fs, 12).Build(context.Background(), in)
	return q.Best, complete
}

// positions 는 그 판의 手数별 국면이다. 시험이 SFEN으로 후보를 지정하려면 필요하다.
func positions(t *testing.T, in Input) []shogi.Position {
	t.Helper()
	posAt := replay(in)
	if len(posAt) != len(in.Moves)+1 {
		t.Fatalf("replay stopped at %d of %d moves — the test line is not legal", len(posAt)-1, len(in.Moves))
	}
	return posAt
}

func TestBestItemTakesTheGapPosition(t *testing.T) {
	in := gameInput()
	posAt := positions(t, in)

	fs := &fakeSearch{lines: map[string][]usi.SearchLine{
		posAt[4].SFEN(): {line("2f2e", 120), line("6f6e", -180)},
	}}
	items, _ := build(fs, in)

	if len(items) != 1 {
		t.Fatalf("items = %d, want 1: %+v", len(items), items)
	}
	got := items[0]
	if got.Ply != 4 {
		t.Errorf("ply = %d, want 4", got.Ply)
	}
	if got.Answer != "2f2e" {
		t.Errorf("answer = %q, want 2f2e", got.Answer)
	}
	if got.Gap() != 300 {
		t.Errorf("gap = %d, want 300", got.Gap())
	}
	if got.Played != gameMoves[4] {
		t.Errorf("played = %q, want %q — the item has to point back at the actual game", got.Played, gameMoves[4])
	}
	if got.SFEN != posAt[4].SFEN() {
		t.Errorf("sfen = %q, want %q", got.SFEN, posAt[4].SFEN())
	}
}

func TestBestItemSkipsASmallGap(t *testing.T) {
	// 차가 작으면 정답이 사실상 여럿이다. 그런 국면을 내면 좋은 수를 둔 사람이 「不正解」를 받는다.
	in := gameInput()
	posAt := positions(t, in)

	fs := &fakeSearch{lines: map[string][]usi.SearchLine{
		posAt[4].SFEN(): {line("2f2e", 40), line("6f6e", -40)}, // gap 80 < BestMinGapCp
	}}
	if items, _ := build(fs, in); len(items) != 0 {
		t.Fatalf("items = %+v, want none", items)
	}
}

func TestBestItemSkipsMateScores(t *testing.T) {
	// 「츠메 관련 제외」. cp로 환산된 mate 점수로 gap을 재면 手数 차가 cp 차로 보인다.
	in := gameInput()
	posAt := positions(t, in)

	fs := &fakeSearch{lines: map[string][]usi.SearchLine{
		posAt[4].SFEN(): {mateLine("2f2e", 5), line("6f6e", 100)},
	}}
	if items, _ := build(fs, in); len(items) != 0 {
		t.Fatalf("items = %+v, want none", items)
	}
}

func TestBestItemSkipsWhatThePlayerAlreadyFound(t *testing.T) {
	in := gameInput()
	posAt := positions(t, in)

	// 정답이 사람이 실제로 둔 수와 같다 — 「あなたの手は正解でした」를 문제로 내는 셈이 된다.
	fs := &fakeSearch{lines: map[string][]usi.SearchLine{
		posAt[4].SFEN(): {line(gameMoves[4], 500), line("6f6e", 100)},
	}}
	if items, _ := build(fs, in); len(items) != 0 {
		t.Fatalf("items = %+v, want none", items)
	}
}

func TestBestItemsAreSortedByGapAndCapped(t *testing.T) {
	in := gameInput()
	posAt := positions(t, in)

	fs := &fakeSearch{lines: map[string][]usi.SearchLine{
		posAt[2].SFEN(): {line("2f2e", 250), line("x", 0)},  // gap 250
		posAt[4].SFEN(): {line("6f6e", 900), line("x", 0)},  // gap 900
		posAt[6].SFEN(): {line("3f3e", 500), line("x", 0)},  // gap 500
		posAt[8].SFEN(): {line("5f5e", 1200), line("x", 0)}, // gap 1200
	}}
	items, _ := build(fs, in)

	if len(items) != BestMaxItems {
		t.Fatalf("items = %d, want %d", len(items), BestMaxItems)
	}
	wantPlies := []int{8, 4, 6} // gap 1200 · 900 · 500
	for i, want := range wantPlies {
		if items[i].Ply != want {
			t.Errorf("items[%d].ply = %d, want %d (gap order)", i, items[i].Ply, want)
		}
	}
}

// **詰み 문항이 쓰는 국면 하나만 뺀다.**
//
// 한때 그 手数부터 뒤를 통째로 잘랐는데, 詰み 문항은 판에서 **가장 이른** 詰み이라
// (§53) 이른 자리에서 하나 나오면 中盤과 終盤이 통째로 후보에서 사라졌다 — 진짜
// 블런더가 있는 구간이 그쪽이다.
func TestBestItemsSkipOnlyTheMatePosition(t *testing.T) {
	in := gameInput()
	posAt := positions(t, in)

	fs := &fakeSearch{lines: map[string][]usi.SearchLine{
		posAt[2].SFEN(): {line("2f2e", 900), line("x", 0)},
		posAt[4].SFEN(): {line("6f6e", 900), line("x", 0)},
		posAt[6].SFEN(): {line("3f3e", 900), line("x", 0)},
	}}
	b := NewBuilder(nil, fs, 12)
	items, _ := b.bestItems(context.Background(), in, posAt, &MateItem{Ply: 4})

	if len(items) != 2 {
		t.Fatalf("items = %d, want 2: %+v", len(items), items)
	}
	for _, it := range items {
		if it.Ply == 4 {
			t.Errorf("ply 4 is the mate item's own position — the two questions would ask the same thing")
		}
	}
	// 詰み 뒤의 자리가 살아 있어야 한다.
	if items[0].Ply != 2 && items[1].Ply != 6 {
		t.Errorf("items = %+v, want plies 2 and 6", items)
	}
	// **엔진에는 물어봤어야 한다** — 詰み 뒤라고 미리 자르지 않는다.
	if !fs.asked[posAt[6].SFEN()] {
		t.Error("the engine was never asked about a position after the mate item")
	}
}

func TestBestItemsSkipTheOpeningBook(t *testing.T) {
	// 10수 만에 投了한 판에서 문항 셋이 전부 오프닝이 되는 것을 막는 자리다.
	in := gameInput()
	in.OpeningPlies = 6
	posAt := positions(t, in)

	fs := &fakeSearch{lines: map[string][]usi.SearchLine{
		posAt[2].SFEN(): {line("2f2e", 900), line("x", 0)},
		posAt[8].SFEN(): {line("5f5e", 900), line("x", 0)},
	}}
	items, _ := build(fs, in)

	if len(items) != 1 {
		t.Fatalf("items = %d, want 1: %+v", len(items), items)
	}
	if items[0].Ply != 8 {
		t.Errorf("ply = %d, want 8 — ply 2 is inside the opening", items[0].Ply)
	}
	// **엔진을 쓰기 전에 걸러야 한다.** 뒤에서 거르면 정석 구간의 국면마다 956ms를 쓰고
	// 버리는 셈이 된다.
	if fs.asked[posAt[2].SFEN()] {
		t.Error("the engine was asked about a position inside the opening")
	}
}

func TestGradeBest(t *testing.T) {
	in := gameInput()
	posAt := positions(t, in)
	item := BestItem{Ply: 4, SFEN: posAt[4].SFEN(), Answer: "2f2e", Played: gameMoves[4]}

	ok, err := GradeBest(item, "2f2e")
	if err != nil || !ok {
		t.Errorf("the answer graded as (%v, %v), want (true, nil)", ok, err)
	}

	ok, err = GradeBest(item, "1g1f")
	if err != nil || ok {
		t.Errorf("a legal wrong move graded as (%v, %v), want (false, nil)", ok, err)
	}

	// 불법수는 **오답이 아니라 요청 오류**다. 뭉치면 프론트 버그가 오답으로 위장해 안 보인다.
	// `1a1b` 는 後手의 香을 움직이는 수라 先手 차례에 불법이다.
	if _, err := GradeBest(item, "1a1b"); err == nil {
		t.Error("an illegal move graded as an answer")
	}
}

func TestLegalMovesAtIsNotRestrictedToChecks(t *testing.T) {
	// 「최선수는?」 문항은 王手로 좁히지 않는다 — 그 규약은 詰み 문항만의 것이다.
	got, err := LegalMovesAt(shogi.StartSFEN)
	if err != nil {
		t.Fatalf("legal moves: %v", err)
	}
	pos, _ := shogi.ParseSFEN(shogi.StartSFEN)
	if len(got) != len(pos.LegalMoves()) {
		t.Errorf("legal = %d moves, want %d", len(got), len(pos.LegalMoves()))
	}
}

// flakySearch 는 한 국면만 실패한다. **한쪽의 실패가 다른 쪽을 지우지 않는가**를 보는 자리다.
type flakySearch struct {
	fakeSearch
	failOn string
}

func (f *flakySearch) SearchMultiPV(
	ctx context.Context, startSFEN string, moves []string, depth, k int,
) (usi.SearchResult, error) {
	if startSFEN == f.failOn {
		return usi.SearchResult{}, errFake
	}
	return f.fakeSearch.SearchMultiPV(ctx, startSFEN, moves, depth, k)
}

// **못 잰 후보가 있어도 잰 것은 그대로 참이다.**
//
// 두 사실을 한 깃발로 묶어 통째로 버리면, 후보 하나를 못 잰 것이 멀쩡한 문항을 지운다 —
// 생성이 판이 끝날 때 한 번뿐이라 그 판은 영영 문항을 못 갖는다(server/ws.go generateQuiz).
func TestBestItemsSurviveAFailureElsewhere(t *testing.T) {
	in := gameInput()
	posAt := positions(t, in)

	fs := &flakySearch{
		fakeSearch: fakeSearch{lines: map[string][]usi.SearchLine{
			posAt[8].SFEN(): {line("5f5e", 900), line("x", 0)},
		}},
		failOn: posAt[6].SFEN(),
	}
	items, complete := build(fs, in)

	if complete {
		t.Error("complete = true, but one candidate search failed")
	}
	if len(items) != 1 || items[0].Ply != 8 {
		t.Fatalf("items = %+v, want the one that was measured", items)
	}
}

func TestQuizEmpty(t *testing.T) {
	if !(Quiz{}).Empty() {
		t.Error("a quiz with nothing in it is not empty")
	}
	if (Quiz{Mate: &MateItem{}}).Empty() {
		t.Error("a quiz with a mate item is empty")
	}
	if (Quiz{Best: []BestItem{{}}}).Empty() {
		t.Error("a quiz with a best item is empty")
	}
}

// **두어지지 않은 수는 문항이 안 된다.**
//
// `replay` 는 읽을 수 없는 수에서 멈추므로 마지막 국면은 있어도 그 자리의 수는 판에 없다.
// 거기까지 후보로 삼으면 `Played` 가 없던 수가 되고, 「사람이 이미 최선수를 뒀다」를 그 수와
// 견주게 되어 실제로 최선수를 둔 국면이 문항으로 나간다.
func TestBestItemsStopAtTheEndOfTheReplay(t *testing.T) {
	in := gameInput()
	// 마지막 자리를 읽을 수 없는 수로 바꾼다 — 그 앞까지만 재현된다.
	in.Moves = append([]string(nil), gameMoves[:8]...)
	in.Moves = append(in.Moves, "zzzz")

	posAt := replay(in)
	if len(posAt) != 9 {
		t.Fatalf("replay produced %d positions, want 9", len(posAt))
	}

	// 그 국면(8手目)에 후보가 서면 안 된다.
	fs := &fakeSearch{lines: map[string][]usi.SearchLine{
		posAt[8].SFEN(): {line("5f5e", 900), line("x", 0)},
	}}
	items, _ := build(fs, in)

	for _, it := range items {
		if it.Ply == 8 {
			t.Errorf("ply 8 became a question, but its move never reached the board (played %q)", it.Played)
		}
	}
	if fs.asked[posAt[8].SFEN()] {
		t.Error("the engine was asked about a position whose move is not in the game")
	}
}
