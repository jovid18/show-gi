package shogi

import (
	"math/rand"
	"slices"
	"testing"
)

// slowAttacked 는 attacked 이전의 IsAttacked 다. 판 전체의 말에서 attackTargets 를 따라가므로
// 규칙을 한 번 더 적지 않고, 바깥으로 훑는 쪽의 대조군이 된다.
func slowAttacked(pos *Position, sq int, by Color) bool {
	for s := 0; s < 81; s++ {
		p := pos.Board[s]
		if p.Empty() || p.Color() != by {
			continue
		}
		hit := false
		pos.attackTargets(s, func(to int) bool {
			hit = to == sq
			return !hit
		})
		if hit {
			return true
		}
	}
	return false
}

// playouts 는 무작위로 둔 국면들이다. 잡는 수를 자주 골라 持ち駒가 쌓이게 한다. 투입과
// 王手가 섞인 국면이 많아야 Checks·Evasions 의 거르기를 시험할 수 있다.
func playouts(t *testing.T, games, plies int) []Position {
	t.Helper()
	rng := rand.New(rand.NewSource(1))
	var out []Position
	for g := 0; g < games; g++ {
		pos := StartPosition()
		for i := 0; i < plies; i++ {
			moves := pos.LegalMoves()
			if len(moves) == 0 {
				break
			}
			m := moves[rng.Intn(len(moves))]
			for _, c := range moves {
				if !c.IsDrop() && !pos.Board[c.To].Empty() && rng.Intn(2) == 0 {
					m = c
					break
				}
			}
			pos = pos.Apply(m)
			out = append(out, pos)
		}
	}
	return out
}

func TestAttackedMatchesFullScan(t *testing.T) {
	for _, pos := range playouts(t, 200, 160) {
		for sq := 0; sq < 81; sq++ {
			for _, by := range []Color{Black, White} {
				if got, want := pos.attacked(sq, by), slowAttacked(&pos, sq, by); got != want {
					t.Fatalf("%s: attacked(%s, %s) = %v, full scan says %v", pos.SFEN(), SquareUSI(sq), by, got, want)
				}
			}
		}
	}
}

func TestChecksAndEvasionsMatchLegalMoves(t *testing.T) {
	var checks, evasions int
	for _, pos := range playouts(t, 300, 160) {
		legal := pos.LegalMoves()
		var want []Move
		for _, m := range legal {
			np := pos.Apply(m)
			if np.InCheck(pos.Turn.Other()) {
				want = append(want, m)
			}
		}
		if got := pos.Checks(); !slices.Equal(got, want) {
			t.Fatalf("%s: Checks = %v, LegalMoves filtered = %v", pos.SFEN(), got, want)
		}
		if pos.HasCheck() != (len(want) > 0) {
			t.Fatalf("%s: HasCheck = %v with %d checks", pos.SFEN(), pos.HasCheck(), len(want))
		}
		checks += len(want)
		if pos.InCheck(pos.Turn) {
			if got := pos.Evasions(); !slices.Equal(got, legal) {
				t.Fatalf("%s: Evasions = %v, LegalMoves = %v", pos.SFEN(), got, legal)
			}
			evasions++
		}
	}
	// 대조할 것이 없으면 위의 비교는 아무것도 시험하지 않는다.
	if checks < 1000 || evasions < 300 {
		t.Fatalf("playouts too tame: %d checks, %d positions in check", checks, evasions)
	}
	t.Logf("%d checks, %d positions in check", checks, evasions)
}
