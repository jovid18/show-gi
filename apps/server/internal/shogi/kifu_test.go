package shogi

import "testing"

// 두 金이 같은 칸에 갈 수 있는 국면. 하나는 바로 아래에서 곧장 올라온다.
const twoGoldsSFEN = "8k/9/9/9/9/9/9/9/3GG3K b - 1"

// 표기 하나가 두 수를 가리키면 안 된다.
//
// 같은 筋에서 온 駒에 「左」를 붙이던 동안 그 일이 실제로 있었다 — 왼쪽에서 온 駒와
// 라벨이 같아져 disambiguate 가 둘을 못 가르고, 그러면 두 수가 같은 이름으로 화면에
// 나간다. 실 코퍼스 296판 중 16판에서 나왔다(journal §126).
func TestNotationNamesOneMove(t *testing.T) {
	pos, err := ParseSFEN(twoGoldsSFEN)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]Move{}
	for _, m := range pos.LegalMoves() {
		ja := pos.MoveJa(m, -1)
		if prev, dup := seen[ja]; dup {
			t.Errorf("%q names two moves: %s and %s", ja, prev.USI(), m.USI())
			continue
		}
		seen[ja] = m
	}
}

// 적은 표기가 같은 국면에서 그대로 되읽혀야 한다. 되짚기 화면이 부르는 표기와 취해 온
// 기보가 읽는 표기가 갈리면, 이쪽이 쓴 것을 저쪽이 못 읽는다(internal/kifu 의 왕복 시험이
// 실 코퍼스로 같은 것을 건다).
func TestResolveOriginIsTheInverseOfDisambiguate(t *testing.T) {
	pos, err := ParseSFEN(twoGoldsSFEN)
	if err != nil {
		t.Fatal(err)
	}
	tried := 0
	for _, m := range pos.LegalMoves() {
		if m.IsDrop() {
			continue
		}
		pt := pos.Board[m.From].Type()
		cands := pos.movers(pt, int(m.To), pos.Turn)
		mods := ""
		if len(cands) > 1 {
			mods = disambiguate(int(m.From), int(m.To), cands, pos.Turn)
		}
		from, err := pos.ResolveOrigin(pt, int(m.To), mods)
		if err != nil {
			t.Errorf("%s (%s%s): %v", m.USI(), PieceJa(pt), mods, err)
			continue
		}
		if from != int(m.From) {
			t.Errorf("%s (%s%s): resolved to %s", m.USI(), PieceJa(pt), mods, SquareJa(from))
		}
		tried++
	}
	if tried == 0 {
		t.Fatal("no move was tried")
	}
}

// 수식어로도 안 갈리면 고르지 않는다. 골라 버리면 그 뒤의 수순이 통째로 다른 판이 되는데,
// 남는 수가 합법수라 ValidateMove 도 안 잡는다.
func TestResolveOriginRefusesWhenItCannotTell(t *testing.T) {
	// 6八金과 4八金이 둘 다 5八로 갈 수 있다. 「金」만으로는 안 정해진다.
	pos, err := ParseSFEN("8k/9/9/9/9/9/9/3G1G3/K8 b - 1")
	if err != nil {
		t.Fatal(err)
	}
	to := SquareOf(5, 8)
	if from, err := pos.ResolveOrigin(Gold, to, ""); err == nil {
		t.Fatalf("picked %s instead of refusing", SquareJa(from))
	}
	from, err := pos.ResolveOrigin(Gold, to, "右")
	if err != nil {
		t.Fatal(err)
	}
	if want := SquareOf(4, 8); from != want {
		t.Errorf("右 resolved to %s, want %s", SquareJa(from), SquareJa(want))
	}
}
