package tag

import (
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 회차 1 의 63手 국면 그대로다. 金矢倉이 59手에 깨진 뒤 G*6h 하나로 좌표가 채워져
// 「左美濃」가 한 手 동안 떴다 — 玉8八·金7八·金6八·銀7七이 전부 맞는데도 그렇다.
const hidariMinoByDropSFEN = "r5k1l/2+P3gs1/4ppn2/p4bpp1/1p1L4P/P1sPP1PP1/1PS1bPS2/1KGG3R1/LN5NL w N3Pg 64"

func castleCode(in Input) string {
	for _, t := range Detect(in) {
		if t.Kind == KindCastle {
			return t.Code
		}
	}
	return ""
}

// 打って 채운 囲い에는 이름을 안 붙인다.
//
// 같은 국면에 수순만 둘로 갈라 넣는다 — 이 규칙은 그것만으로 이름을 가른다.
func TestCastleCompletedByADropIsNotNamed(t *testing.T) {
	pos, err := shogi.ParseSFEN(hidariMinoByDropSFEN)
	if err != nil {
		t.Fatal(err)
	}

	if got := castleCode(Input{Pos: pos, Color: shogi.Black, PlayerMoves: []string{"G*6h"}}); got != "" {
		t.Errorf("打으로 채운 6八인데 %s 가 붙었다", got)
	}
	// 같은 칸에 옮겨 왔으면 지은 것이다.
	if got := castleCode(Input{Pos: pos, Color: shogi.Black, PlayerMoves: []string{"6i6h"}}); got != "hidari_mino" {
		t.Errorf("옮겨 온 6八인데 left_mino 가 아니라 %q", got)
	}
}

// 打った 칸에 나중에 駒가 옮겨 오면 그 칸은 다시 「지은 것」이 된다.
//
// 이 확인이 없으면 표식이 그 판 내내 남아, 한 번 打았던 칸을 지나는 囲い가 영원히
// 이름을 못 받는다.
func TestDropMarkIsClearedWhenAPieceMovesOnto(t *testing.T) {
	pos, err := shogi.ParseSFEN(hidariMinoByDropSFEN)
	if err != nil {
		t.Fatal(err)
	}

	in := Input{Pos: pos, Color: shogi.Black, PlayerMoves: []string{"G*6h", "6h5g", "5g6h"}}
	if got := castleCode(in); got != "hidari_mino" {
		t.Errorf("옮겨 와서 덮은 6八인데 %q", got)
	}
}

// 打은 駒가 필수 칸 밖이면 상관없다. 규칙이 「打이 있었나」가 아니라 「그 칸이 打으로
// 채워졌나」라는 것을 못박는다 — 넓게 걸면 종반에 持ち駒를 쓰는 순간 이름이 전부 꺼진다.
func TestDropAwayFromTheCastleDoesNotSuppressIt(t *testing.T) {
	pos, err := shogi.ParseSFEN(hidariMinoByDropSFEN)
	if err != nil {
		t.Fatal(err)
	}

	in := Input{Pos: pos, Color: shogi.Black, PlayerMoves: []string{"6i6h", "P*5e"}}
	if got := castleCode(in); got != "hidari_mino" {
		t.Errorf("囲い 밖의 打인데 %q", got)
	}
}
