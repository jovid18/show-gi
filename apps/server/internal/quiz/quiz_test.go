package quiz

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// mate1SFEN 은 先手의 1手詰め이다. `G*5b` 로 詰む — 玉은 4a·6a로 못 가고(5b의 金이 짚는다)
// 5b의 金은 4c·6c의 金이 받친다.
//
// **玉이 아직 王手를 받고 있지 않은 국면이라야 한다.** 4c·6c에 둔 것이 그래서다 —
// 4b·6b에 두면 그 金이 5a를 짚어 後手가 王手를 방치한 불법 국면이 된다.
const mate1SFEN = "4k4/9/3G1G3/9/9/9/9/9/4K4 b G 1"

// mateIn 은 **테스트용** 詰み 탐색이다. 攻方은 王手만 걸고 玉方은 최장 방어를 고른다.
//
// 트리를 짓는 쪽과 셈이 다르다 — 여기는 攻方 수에 대한 최솟값을 재귀로 구하고, 저쪽은
// 응수의 최댓값에 2를 더한다. 그래서 이것으로 저쪽을 견주는 것이 자기 자신과의 대조가
// 아니다.
func mateIn(pos shogi.Position, limit int) int {
	if limit <= 0 {
		return 0
	}
	best := 0
	for _, m := range checkingMoves(pos) {
		np := pos.Apply(m)
		if np.IsCheckmate() {
			return 1
		}

		longest, escaped := 0, false
		for _, r := range np.LegalMoves() {
			d := mateIn(np.Apply(r), limit-2)
			if d == 0 {
				escaped = true
				break
			}
			if d > longest {
				longest = d
			}
		}
		if escaped {
			continue
		}
		if total := 2 + longest; best == 0 || total < best {
			best = total
		}
	}
	return best
}

// fakeMate 는 위 탐색을 `MateSearcher` 모양으로 씌운 것이다. **엔진 없이 트리를 짓는다.**
type fakeMate struct {
	limit int
	calls int
	// unproven 이 참이면 늘 「모른다」로 답한다 — 그때 문항을 버리는지 보는 자리다.
	unproven bool
}

func (f *fakeMate) SearchMate(_ context.Context, sfen string, _ []string) (usi.MateResult, error) {
	f.calls++
	if f.unproven {
		return usi.MateResult{}, nil
	}
	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		return usi.MateResult{}, err
	}
	d := mateIn(pos, f.limit)
	if d == 0 {
		return usi.MateResult{Proven: true}, nil
	}
	// 手数만 쓰인다(mateSolver.distance). 수순의 내용은 트리가 스스로 만든다.
	return usi.MateResult{Moves: make([]string, d), Proven: true}, nil
}

func TestCheckingMovesAreChecksOnly(t *testing.T) {
	pos, err := shogi.ParseSFEN(mate1SFEN)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	checks := checkingMoves(pos)
	if len(checks) == 0 {
		t.Fatal("no checking moves in a position that has a mate in one")
	}
	them := pos.Turn.Other()
	for _, m := range checks {
		np := pos.Apply(m)
		if !np.InCheck(them) {
			t.Errorf("%s is not a check", m.USI())
		}
	}
	// 王手가 아닌 합법수가 섞이지 않았는가 — 개수로 견준다.
	want := 0
	for _, m := range pos.LegalMoves() {
		np := pos.Apply(m)
		if np.InCheck(them) {
			want++
		}
	}
	if len(checks) != want {
		t.Errorf("checkingMoves gave %d, want %d", len(checks), want)
	}
}

func TestMateItemFindsMateInOne(t *testing.T) {
	fm := &fakeMate{limit: 7}
	b := NewBuilder(fm, nil, 12)

	// 판이 그 국면에서 시작해 아직 한 수도 안 두어진 모양으로 넣는다.
	in := Input{StartSFEN: mate1SFEN, Human: shogi.Black}
	q, complete := b.Build(context.Background(), in)
	if !complete {
		t.Error("complete = false, but the solver answered everything")
	}

	if q.Mate == nil {
		t.Fatal("no mate item")
	}
	if q.Mate.Plies != 1 {
		t.Fatalf("plies = %d, want 1", q.Mate.Plies)
	}
	if q.Mate.Converted {
		t.Error("converted = true, but the game never played the mate")
	}
	if len(q.Mate.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1 (a mate in one has no deeper node)", len(q.Mate.Nodes))
	}

	pos, _ := shogi.ParseSFEN(mate1SFEN)
	node := q.Mate.Nodes[pos.RepetitionKey()]
	if node.Plies != 1 {
		t.Errorf("node plies = %d, want 1", node.Plies)
	}

	// **이 국면에는 1手詰め이 여럿이다**(`G*5b` 도 `4c5b` 도 詰む). 실전 국면에서 余詰은
	// 예외가 아니라 보통이고, 그래서 「하나가 정답」이라고 단정하지 않는다 — 대신
	// **결정적으로 고르는지**를 본다. 규약은 詰み 우선, 그다음 USI 순서다.
	if want := smallestMate(pos); node.Best != want {
		t.Errorf("best = %q, want %q (mate first, then USI order)", node.Best, want)
	}
	if got := q.Mate.Nodes[pos.RepetitionKey()].Best; got != node.Best {
		t.Errorf("best is not stable across reads: %q then %q", node.Best, got)
	}

	// 王手 전부가 판정을 갖고, 詰み인 것만 정답이다.
	for u, v := range node.Moves {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			t.Fatalf("tree holds an unparseable move %q: %v", u, err)
		}
		mated := pos.Apply(m).IsCheckmate()
		if v.Mated != mated {
			t.Errorf("%s: mated = %v, want %v", u, v.Mated, mated)
		}
		if v.Correct != mated {
			t.Errorf("%s: correct = %v, want %v (a 1-ply problem is only solved by mate)", u, v.Correct, mated)
		}
	}
	if got := node.Moves["G*5b"]; !got.Mated || !got.Correct {
		t.Errorf("G*5b = %+v, want mated and correct", got)
	}
}

// smallestMate 는 그 국면의 詰み 수 중 USI가 가장 앞인 것이다 — `pickBestMate` 의 규약을
// 테스트가 **다시 세어** 견주는 자리다.
func smallestMate(pos shogi.Position) string {
	best := ""
	for _, m := range checkingMoves(pos) {
		if !pos.Apply(m).IsCheckmate() {
			continue
		}
		if u := m.USI(); best == "" || u < best {
			best = u
		}
	}
	return best
}

func TestMateItemDroppedWhenSolverCannotProve(t *testing.T) {
	// 「모른다」를 「없다」로 쓰면 있는 詰み을 놓치고, 그 위에서 채점하면 정답을 오답이라고
	// 말한다. 그래서 증명되지 않으면 문항이 아예 없어야 한다.
	fm := &fakeMate{limit: 7, unproven: true}
	q, _ := NewBuilder(fm, nil, 12).Build(context.Background(), Input{StartSFEN: mate1SFEN, Human: shogi.Black})
	if q.Mate != nil {
		t.Fatalf("got a mate item from an unproven search: %+v", q.Mate)
	}
}

func TestMateItemSkipsTheOpponentsTurn(t *testing.T) {
	// 같은 국면을 後手의 문항으로 물으면 안 된다 — 詰ます 쪽은 先手다.
	fm := &fakeMate{limit: 7}
	q, _ := NewBuilder(fm, nil, 12).Build(context.Background(), Input{StartSFEN: mate1SFEN, Human: shogi.White})
	if q.Mate != nil {
		t.Fatalf("got a mate item for the side that is not to move: %+v", q.Mate)
	}
	if fm.calls != 0 {
		t.Errorf("asked the solver %d times, want 0 — the turn check is free", fm.calls)
	}
}

func TestGradeMateSolvesInOne(t *testing.T) {
	fm := &fakeMate{limit: 7}
	q, _ := NewBuilder(fm, nil, 12).Build(context.Background(), Input{StartSFEN: mate1SFEN, Human: shogi.Black})
	if q.Mate == nil {
		t.Fatal("no mate item")
	}

	got, err := GradeMate(*q.Mate, []string{"G*5b"})
	if err != nil {
		t.Fatalf("grade: %v", err)
	}
	if got.Outcome != MateSolved {
		t.Errorf("outcome = %q, want %q", got.Outcome, MateSolved)
	}
	if len(got.Legal) != 0 {
		t.Errorf("legal = %v, want none once the problem is over", got.Legal)
	}
}

func TestGradeMateRejectsAMoveAfterTheProblemEnds(t *testing.T) {
	fm := &fakeMate{limit: 7}
	q, _ := NewBuilder(fm, nil, 12).Build(context.Background(), Input{StartSFEN: mate1SFEN, Human: shogi.Black})

	if _, err := GradeMate(*q.Mate, []string{"G*5b", "G*5b"}); err == nil {
		t.Fatal("a move after mate was accepted")
	}
}

func TestGradeMateOnAWrongCheck(t *testing.T) {
	fm := &fakeMate{limit: 7}
	q, _ := NewBuilder(fm, nil, 12).Build(context.Background(), Input{StartSFEN: mate1SFEN, Human: shogi.Black})

	// 詰み이 아닌 王手를 하나 찾는다.
	pos, _ := shogi.ParseSFEN(mate1SFEN)
	wrong := ""
	for u, v := range q.Mate.Nodes[pos.RepetitionKey()].Moves {
		if !v.Correct {
			wrong = u
			break
		}
	}
	if wrong == "" {
		t.Skip("this position has no non-mating check")
	}

	got, err := GradeMate(*q.Mate, []string{wrong})
	if err != nil {
		t.Fatalf("grade: %v", err)
	}
	if got.Outcome != MateWrong {
		t.Fatalf("outcome = %q, want %q", got.Outcome, MateWrong)
	}
	if want := smallestMate(pos); got.Best != want {
		t.Errorf("best = %q, want %q — a wrong answer has to be told what did work", got.Best, want)
	}
}

func TestGradeMateOnAMoveThatIsNotACheck(t *testing.T) {
	fm := &fakeMate{limit: 7}
	q, _ := NewBuilder(fm, nil, 12).Build(context.Background(), Input{StartSFEN: mate1SFEN, Human: shogi.Black})

	// 王手가 아닌 합법수. 玉의 반대쪽 끝에 있는 자기 玉을 움직인다.
	got, err := GradeMate(*q.Mate, []string{"5i5h"})
	if err != nil {
		t.Fatalf("grade: %v", err)
	}
	if got.Outcome != MateNotCheck {
		t.Fatalf("outcome = %q, want %q", got.Outcome, MateNotCheck)
	}
	if len(got.Line) != 0 {
		t.Errorf("line = %v, want empty — a non-check must not move the board", got.Line)
	}
}

func TestGradeMateRejectsAnIllegalMove(t *testing.T) {
	fm := &fakeMate{limit: 7}
	q, _ := NewBuilder(fm, nil, 12).Build(context.Background(), Input{StartSFEN: mate1SFEN, Human: shogi.Black})

	if _, err := GradeMate(*q.Mate, []string{"1a1b"}); err == nil {
		t.Fatal("an illegal move was accepted")
	}
}

// mate3SFEN 은 先手의 3手詰め이다 — `G*5b` △同金 `▲同金`.
//
// 1手로는 안 되는 이유가 **後手 金 하나**다(6b). 5b에 놓은 金을 그 金이 딸 수 있어서 한 번에
// 끝나지 않고, 되따면 다시 王手가 되어 그때 詰む. mate1SFEN 에 방어 駒를 4a·6a에 두었더니
// 그 駒가 玉의 도주로를 막아 오히려 1手詰め이 됐다 — 6b는 5b를 지키면서 도주로는 막지 않는다.
const mate3SFEN = "4k4/3g5/3G1G3/9/9/9/9/9/4K4 b G 1"

// TestMateTreeWalksThreePlies 는 트리의 **재귀**를 본다 — 1手詰め에는 노드가 하나뿐이라
// `expand` 가 다음 층으로 내려가는 자리가 전혀 안 돌았다.
func TestMateTreeWalksThreePlies(t *testing.T) {
	fm := &fakeMate{limit: 9}
	q, _ := NewBuilder(fm, nil, 12).Build(context.Background(), Input{StartSFEN: mate3SFEN, Human: shogi.Black})

	if q.Mate == nil {
		t.Fatal("no mate item")
	}
	if q.Mate.Plies != 3 {
		t.Fatalf("plies = %d, want 3", q.Mate.Plies)
	}
	if len(q.Mate.Nodes) < 2 {
		t.Fatalf("nodes = %d, want the root plus at least one deeper node", len(q.Mate.Nodes))
	}

	root, _ := shogi.ParseSFEN(mate3SFEN)
	rootNode := q.Mate.Nodes[root.RepetitionKey()]

	// **노드의 手数는 그 국면의 성질이다.** 트리가 세어 넣은 값을 시험이 다시 재서 견준다.
	for key, node := range q.Mate.Nodes {
		pos, ok := positionOfKey(t, q.Mate, key)
		if !ok {
			continue
		}
		if want := mateIn(pos, 9); node.Plies != want {
			t.Errorf("node %s: plies = %d, want %d", key, node.Plies, want)
		}
		// 판정도 다시 센다 — 정답이면 手数가 2 이상 줄고, 아니면 안 줄어든다.
		for u, v := range node.Moves {
			m, err := shogi.ParseUSIMove(u)
			if err != nil {
				t.Fatalf("tree holds an unparseable move %q", u)
			}
			np := pos.Apply(m)
			if np.IsCheckmate() {
				if !v.Mated || !v.Correct {
					t.Errorf("%s at %s: %+v, want mated and correct", u, key, v)
				}
				continue
			}
			if v.Correct != (v.Rest > 0 && 2+v.Rest <= node.Plies) {
				t.Errorf("%s at %s: correct = %v with rest %d and plies %d", u, key, v.Correct, v.Rest, node.Plies)
			}
		}
	}

	// 정답 수마다 **그 응수 뒤의 국면이 트리에 있어야** 한다. 없으면 채점이 거기서 막힌다.
	corrections := 0
	for u, v := range rootNode.Moves {
		if !v.Correct || v.Mated {
			continue
		}
		corrections++
		if v.Defense == "" {
			t.Errorf("%s is correct but has no defense", u)
			continue
		}
		m, _ := shogi.ParseUSIMove(u)
		d, err := shogi.ParseUSIMove(v.Defense)
		if err != nil {
			t.Fatalf("defense %q is unparseable", v.Defense)
		}
		next := root.Apply(m).Apply(d)
		if _, ok := q.Mate.Nodes[next.RepetitionKey()]; !ok {
			t.Errorf("%s + %s leads to a position the tree does not hold", u, v.Defense)
		}
	}
	if corrections == 0 {
		t.Fatal("no correct non-mating move at the root — the recursion never ran")
	}

	// 끝까지 풀린다: 뿌리의 정답 → (서버가 두는 응수) → 그 노드의 정답 → 詰み.
	first := rootNode.Best
	mid, err := GradeMate(*q.Mate, []string{first})
	if err != nil {
		t.Fatalf("grade %s: %v", first, err)
	}
	if mid.Outcome != MateOngoing {
		t.Fatalf("%s: outcome = %q, want ongoing", first, mid.Outcome)
	}
	if mid.Plies != 1 {
		t.Errorf("after %s: plies = %d, want 1", first, mid.Plies)
	}
	second := q.Mate.Nodes[mustKey(t, mid.SFEN)].Best
	end, err := GradeMate(*q.Mate, []string{first, second})
	if err != nil {
		t.Fatalf("grade %s %s: %v", first, second, err)
	}
	if end.Outcome != MateSolved {
		t.Fatalf("%s %s: outcome = %q, want solved", first, second, end.Outcome)
	}
}

// **트리는 결정적이어야 한다.** 玉方의 최장 방어가 동률일 때 매번 다르게 고르면 같은 문제가
// 열 때마다 다르게 흘러가고, 사람은 그것을 고장으로 읽는다.
func TestMateTreeIsDeterministic(t *testing.T) {
	build := func() string {
		q, _ := NewBuilder(&fakeMate{limit: 9}, nil, 12).
			Build(context.Background(), Input{StartSFEN: mate3SFEN, Human: shogi.Black})
		raw, err := json.Marshal(q)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(raw)
	}
	if a, b := build(), build(); a != b {
		t.Error("two builds of the same position gave different trees")
	}
}

// positionOfKey 는 트리의 키에 해당하는 국면을 되만든다. 키는 手数를 뗀 SFEN이라 手数를
// 하나 붙이면 그대로 읽힌다.
func positionOfKey(t *testing.T, item *MateItem, key string) (shogi.Position, bool) {
	t.Helper()
	pos, err := shogi.ParseSFEN(key + " 1")
	if err != nil {
		t.Errorf("tree key %q is not a position: %v", key, err)
		return shogi.Position{}, false
	}
	return pos, true
}

func mustKey(t *testing.T, sfen string) string {
	t.Helper()
	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		t.Fatalf("parse %q: %v", sfen, err)
	}
	return pos.RepetitionKey()
}

// **王手가 아닌 수를 낸 뒤에도 진행이 남아 있어야 한다.**
//
// 판은 안 움직이지만 그 자리는 **문제 국면이 아니라 지금까지 진행된 국면**이다. 둘 수 있는
// 수를 안 채워 보내면 화면이 문항 쪽으로 되돌아가서 맞힌 수가 사라진 것처럼 보인다.
func TestGradeMateKeepsProgressAfterANonCheck(t *testing.T) {
	fm := &fakeMate{limit: 9}
	q, _ := NewBuilder(fm, nil, 12).Build(context.Background(), Input{StartSFEN: mate3SFEN, Human: shogi.Black})
	if q.Mate == nil {
		t.Fatal("no mate item")
	}
	root, _ := shogi.ParseSFEN(mate3SFEN)
	first := q.Mate.Nodes[root.RepetitionKey()].Best

	mid, err := GradeMate(*q.Mate, []string{first})
	if err != nil {
		t.Fatalf("grade %s: %v", first, err)
	}

	// 그 국면에서 王手가 아닌 합법수를 하나 찾는다.
	pos, err := shogi.ParseSFEN(mid.SFEN)
	if err != nil {
		t.Fatalf("parse %q: %v", mid.SFEN, err)
	}
	checks := make(map[string]bool)
	for _, m := range checkingMoves(pos) {
		checks[m.USI()] = true
	}
	quiet := ""
	for _, m := range pos.LegalMoves() {
		if !checks[m.USI()] {
			quiet = m.USI()
			break
		}
	}
	if quiet == "" {
		t.Skip("every legal move here is a check")
	}

	got, err := GradeMate(*q.Mate, []string{first, quiet})
	if err != nil {
		t.Fatalf("grade %s %s: %v", first, quiet, err)
	}
	if got.Outcome != MateNotCheck {
		t.Fatalf("outcome = %q, want %q", got.Outcome, MateNotCheck)
	}
	if got.SFEN != mid.SFEN {
		t.Errorf("sfen = %q, want the position we were already at (%q)", got.SFEN, mid.SFEN)
	}
	if len(got.Legal) == 0 {
		t.Error("legal is empty — the player has nowhere to go and the problem looks over")
	}
	if got.Plies != mid.Plies {
		t.Errorf("plies = %d, want %d", got.Plies, mid.Plies)
	}
	// 진행된 수순은 그대로 남는다 — 「王手가 아니다」는 되돌리는 것이지 지우는 것이 아니다.
	if len(got.Line) != len(mid.Line) {
		t.Errorf("line = %v, want %v", got.Line, mid.Line)
	}
	// **직전 응수를 물려받지 않는다.** 물려받으면 화면이 「방금 상대가 이렇게 받았다」를 두 번 말한다.
	if got.Defense != "" {
		t.Errorf("defense = %q, want empty — nothing was answered this time", got.Defense)
	}
}

// **오답의 정답 수는 그 수가 성립하는 국면과 함께 와야 한다.**
//
// 오답이면 판이 그 수만큼 나아간다. 정답 표기를 나아간 국면에서 만들려 하면 그 수가 불법이라
// 표기가 비고, 그러면 문구에서 「正解は○でした」가 통째로 빠진다 — 오답에 가장 중요한 조각이다.
func TestGradeMateGivesTheAnswerWithItsOwnPosition(t *testing.T) {
	fm := &fakeMate{limit: 7}
	q, _ := NewBuilder(fm, nil, 12).Build(context.Background(), Input{StartSFEN: mate1SFEN, Human: shogi.Black})

	root, _ := shogi.ParseSFEN(mate1SFEN)
	wrong := ""
	for _, u := range sortedKeys(q.Mate.Nodes[root.RepetitionKey()].Moves) {
		if !q.Mate.Nodes[root.RepetitionKey()].Moves[u].Correct {
			wrong = u
			break
		}
	}
	if wrong == "" {
		t.Skip("this position has no non-mating check")
	}

	got, err := GradeMate(*q.Mate, []string{wrong})
	if err != nil {
		t.Fatalf("grade: %v", err)
	}
	if got.Outcome != MateWrong {
		t.Fatalf("outcome = %q, want %q", got.Outcome, MateWrong)
	}
	if got.BestFrom == got.SFEN {
		t.Fatal("bestFrom equals sfen — the board did not advance past the wrong move, so this test proves nothing")
	}

	// 정답 수가 **그 국면에서** 합법이어야 표기를 만들 수 있다.
	from, err := shogi.ParseSFEN(got.BestFrom)
	if err != nil {
		t.Fatalf("bestFrom %q: %v", got.BestFrom, err)
	}
	m, err := shogi.ParseUSIMove(got.Best)
	if err != nil {
		t.Fatalf("best %q: %v", got.Best, err)
	}
	if err := from.ValidateMove(m); err != nil {
		t.Errorf("the answer %s is not legal in the position it came with: %v", got.Best, err)
	}

	// 나아간 국면에서는 대개 불법이다 — 그것이 이 필드가 있는 이유다.
	after, err := shogi.ParseSFEN(got.SFEN)
	if err != nil {
		t.Fatalf("sfen %q: %v", got.SFEN, err)
	}
	if err := after.ValidateMove(m); err == nil {
		t.Logf("note: %s happens to stay legal after the wrong move here", got.Best)
	}
}

// **王手가 아닌 수에는 정답을 안 준다.**
//
// 그쪽은 오답이 아니라 다시 두라는 안내라 시도가 소진되지 않는다. 답을 실어 보내면 아무
// 조용한 수나 한 번 눌러서 답을 꺼낼 수 있고, 그러면 채점을 서버에 둔 이유가 사라진다.
func TestGradeMateDoesNotLeakTheAnswerOnANonCheck(t *testing.T) {
	fm := &fakeMate{limit: 7}
	q, _ := NewBuilder(fm, nil, 12).Build(context.Background(), Input{StartSFEN: mate1SFEN, Human: shogi.Black})

	got, err := GradeMate(*q.Mate, []string{"5i5h"})
	if err != nil {
		t.Fatalf("grade: %v", err)
	}
	if got.Outcome != MateNotCheck {
		t.Fatalf("outcome = %q, want %q", got.Outcome, MateNotCheck)
	}
	if got.Best != "" || got.BestFrom != "" {
		t.Errorf("best = %q (from %q), want nothing — a quiet move must not buy the answer", got.Best, got.BestFrom)
	}
}

// **이겼다고 詰ました 것은 아니다.**
//
// 엔진은 投了하기도 하고 못 두는 수를 내놓기도 하는데, 둘 다 사람의 승리로 닫힌다. 手数만
// 보면 「あなたが決めた詰みです」가 두어진 적 없는 詰み을 두고 나간다.
func TestConvertedNeedsAnActualCheckmate(t *testing.T) {
	// 詰ましたら 마지막 국면이 詰み이다.
	mated := Input{
		StartSFEN: mate3SFEN, Human: shogi.Black, Won: true,
		Moves: []string{"G*5b", "6b5b", "4c5b"},
	}
	q, _ := NewBuilder(&fakeMate{limit: 9}, nil, 12).Build(context.Background(), mated)
	if q.Mate == nil {
		t.Fatal("no mate item")
	}
	if !q.Mate.Converted {
		t.Error("converted = false, but the game ended in checkmate")
	}

	// 같은 手数 안에 끝났지만 詰み이 아니다 — 상대가 던졌거나 못 두는 수를 냈다.
	resigned := mated
	resigned.Moves = []string{"G*5b", "6b5b"}
	q, _ = NewBuilder(&fakeMate{limit: 9}, nil, 12).Build(context.Background(), resigned)
	if q.Mate == nil {
		t.Fatal("no mate item")
	}
	if q.Mate.Converted {
		t.Error("converted = true, but the player never delivered the mate")
	}
}

// **엔진이 아무것도 못 답하면 「문항이 없다」가 아니다.**
//
// 배포가 생성 도중에 끼면 풀이 닫혀 모든 탐색이 즉시 실패하는데, 그때 나온 빈 결과를
// 저장하면 화면이 「이 판엔 문항이 없다」로 단정한다 — 생성이 판이 끝날 때 한 번뿐이라
// 그 거짓이 영구히 남는다.
func TestBuildReportsADegradedRun(t *testing.T) {
	in := Input{StartSFEN: mate1SFEN, Human: shogi.Black}

	q, complete := NewBuilder(&fakeMate{limit: 7, unproven: true}, nil, 12).Build(context.Background(), in)
	if q.Mate != nil {
		t.Fatalf("got a mate item from a solver that never answered: %+v", q.Mate)
	}
	if complete {
		t.Error("complete = true, but the solver answered nothing")
	}

	// 엔진이 아예 없는 배포는 **못 본 것이 아니다.** 그 배포에 문항이 없는 것은 사실이라
	// 그대로 적어도 된다.
	q, complete = NewBuilder(nil, nil, 12).Build(context.Background(), in)
	if q.Mate != nil || len(q.Best) != 0 {
		t.Fatalf("got items without an engine: %+v", q)
	}
	if !complete {
		t.Error("complete = false with no engine at all — that deployment simply has no questions")
	}
}

// 「최선수는?」 쪽도 같다 — 못 잰 것과 조건에 안 맞는 것은 다른 말이다.
func TestBuildReportsAFailedCandidateSearch(t *testing.T) {
	in := gameInput()
	if _, complete := build(&failingSearch{}, in); complete {
		t.Error("complete = true, but every candidate search failed")
	}
	// 조건에 안 맞아 빠지는 것은 온전한 회차다.
	if _, complete := build(&fakeSearch{}, in); !complete {
		t.Error("complete = false, but nothing failed — the positions just did not qualify")
	}
}

type failingSearch struct{}

func (failingSearch) SearchMultiPV(
	_ context.Context, _ string, _ []string, _, _ int,
) (usi.SearchResult, error) {
	return usi.SearchResult{}, errFake
}

var errFake = errors.New("quiz test: engine is down")
