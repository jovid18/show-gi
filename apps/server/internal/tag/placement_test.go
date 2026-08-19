package tag

import (
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 腹銀 — 銀이 상대 玉의 옆에 붙는다. 5五玉(後手)의 옆 4五에 先手 銀.
func TestSilverBesideTheKingIsHaraGin(t *testing.T) {
	pos := forkBoard(t, "9/9/9/9/4kS3/9/9/9/8K b - 1") // 5五玉 · 4五銀

	got, ok := BellySilver(pos, shogi.SquareOf(4, 5), shogi.Black)
	if !ok {
		t.Fatal("腹銀이 안 잡혔다")
	}
	if got.Code != "hara_gin" || got.Kind != KindTesuji {
		t.Errorf("腹銀을 기대했는데 %s (%v)", got.Code, got.Kind)
	}
}

// 위나 아래는 腹銀이 아니다. 「腹」이 옆이라는 것이 이 이름의 내용이고, 玉頭에
// 두는 것은 다른 手筋이다. 이 음성 테스트가 없으면 술어가 그냥 「玉 옆에 있다」가 된다.
func TestSilverAboveTheKingIsNotHaraGin(t *testing.T) {
	// 5五玉 · 5四銀 — 같은 筋, 한 段 위
	pos := forkBoard(t, "9/9/9/4S4/4k4/9/9/9/8K b - 1")

	if got, ok := BellySilver(pos, shogi.SquareOf(5, 4), shogi.Black); ok {
		t.Errorf("玉頭인데 %s 가 떴다", got.Code)
	}
}

// 두 筋 떨어지면 붙은 것이 아니다.
func TestSilverTwoFilesAwayIsNotHaraGin(t *testing.T) {
	pos := forkBoard(t, "9/9/9/9/3k1S3/9/9/9/8K b - 1") // 6五玉 · 4五銀

	if got, ok := BellySilver(pos, shogi.SquareOf(4, 5), shogi.Black); ok {
		t.Errorf("두 筋 떨어졌는데 %s 가 떴다", got.Code)
	}
}

// 桂頭の銀 — 銀이 상대 桂의 바로 앞이다. 後手 桂는 段이 커지는 쪽으로 가므로
// 5五의 後手 桂에게 「앞」은 5六이다.
func TestSilverOnTheKnightHeadIsKeitouNoGin(t *testing.T) {
	pos := forkBoard(t, "4k4/9/9/9/4n4/4S4/9/9/8K b - 1") // 5五桂(後手) · 5六銀(先手)

	got, ok := KnightHeadSilver(pos, shogi.SquareOf(5, 6), shogi.Black)
	if !ok {
		t.Fatal("桂頭の銀이 안 잡혔다")
	}
	if got.Code != "keitou_no_gin" {
		t.Errorf("桂頭の銀을 기대했는데 %s", got.Code)
	}
}

// 방향이 뒤집히면 안 된다. 桂의 뒤에 놓인 銀은 桂頭が아니다 — 桂는 앞으로만 뛰므로
// 뒤의 駒에 대해서는 아무 성질도 없다. 방향을 반대로 적으면 이 테스트만 실패한다.
func TestSilverBehindTheKnightIsNotKeitouNoGin(t *testing.T) {
	pos := forkBoard(t, "4k4/9/9/4S4/4n4/9/9/9/8K b - 1") // 5四銀 · 5五桂 — 銀이 桂의 뒤

	if got, ok := KnightHeadSilver(pos, shogi.SquareOf(5, 4), shogi.Black); ok {
		t.Errorf("桂의 뒤인데 %s 가 떴다", got.Code)
	}
}

// 銀이 아니면 이 이름이 아니다. 金을 桂頭에 둬도 桂頭の銀은 아니다.
func TestOnlyASilverGetsTheKnightHeadName(t *testing.T) {
	pos := forkBoard(t, "4k4/9/9/9/4n4/4G4/9/9/8K b - 1")

	if got, ok := KnightHeadSilver(pos, shogi.SquareOf(5, 6), shogi.Black); ok {
		t.Errorf("金인데 %s 가 떴다", got.Code)
	}
}

// 底歩(金底の歩) — 자기 진영 맨 아래 段의 歩가 그 위의 金을 받친다.
// 先手는 9段이 맨 아래이고, 그 위는 8段이다.
func TestPawnUnderTheGoldIsSokoNoFu(t *testing.T) {
	pos := forkBoard(t, "4k4/9/9/9/9/9/9/4G4/4P3K b - 1") // 5八金 · 5九歩

	got, ok := BottomPawn(pos, shogi.SquareOf(5, 9), shogi.Black)
	if !ok {
		t.Fatal("底歩가 안 잡혔다")
	}
	if got.Code != "soko_no_fu" {
		t.Errorf("底歩를 기대했는데 %s", got.Code)
	}
}

// 金이어야 한다. 이름이 「金底の歩」이고, 銀 아래의 歩는 같은 것을 말하지 않는다.
func TestAPawnUnderASilverIsNotSokoNoFu(t *testing.T) {
	pos := forkBoard(t, "4k4/9/9/9/9/9/9/4S4/4P3K b - 1")

	if got, ok := BottomPawn(pos, shogi.SquareOf(5, 9), shogi.Black); ok {
		t.Errorf("銀 아래인데 %s 가 떴다", got.Code)
	}
}

// 맨 아래 段이 아니면 底歩가 아니다. 「底」가 그 뜻이다.
func TestAPawnNotOnTheBackRankIsNotSokoNoFu(t *testing.T) {
	pos := forkBoard(t, "4k4/9/9/9/9/9/4G4/4P4/8K b - 1") // 5七金 · 5八歩

	if got, ok := BottomPawn(pos, shogi.SquareOf(5, 8), shogi.Black); ok {
		t.Errorf("8段인데 %s 가 떴다", got.Code)
	}
}

// 後手도 같은 규칙에서 나와야 한다. 진영이 뒤집히면 「底」도 「앞」도 뒤집힌다 —
// 後手의 맨 아래는 1段이고 그 위는 2段이다.
func TestPlacementTesujiMirrorForGote(t *testing.T) {
	// 後手 底歩: 5一歩(後手) · 5二金(後手)
	soko := forkBoard(t, "4p4/4g4/9/9/9/9/9/9/4k3K w - 1")
	if _, ok := BottomPawn(soko, shogi.SquareOf(5, 1), shogi.White); !ok {
		t.Error("後手 底歩가 안 잡혔다")
	}

	// 後手 桂頭の銀: 5五桂(先手) · 5四銀(後手) — 先手 桂의 앞은 5四다
	keitou := forkBoard(t, "4k4/9/9/4s4/4N4/9/9/9/8K w - 1")
	if _, ok := KnightHeadSilver(keitou, shogi.SquareOf(5, 4), shogi.White); !ok {
		t.Error("後手 桂頭の銀이 안 잡혔다")
	}
}

// FindTesuji 가 새 술어들까지 훑는다. 추가하고 배선을 빠뜨리면 여기가 잡는다.
func TestFindTesujiCoversThePlacementTesuji(t *testing.T) {
	pos := forkBoard(t, "9/9/9/9/4kS3/9/9/9/8K b - 1")

	found := false
	for _, tg := range FindTesuji(pos, shogi.Black) {
		if tg.Code == "hara_gin" {
			found = true
		}
	}
	if !found {
		t.Error("FindTesuji 가 腹銀을 안 낸다 — tesujiFinders 에 안 걸렸다")
	}
}

// 成銀에는 이 이름들을 안 붙인다. 실전 국면의 ▲6二成銀 에 「腹銀」이 떴던 자리다
// (journal §34). 이름이 銀이라고 말하는데 成銀은 金의 움직임이라, 手筋의 이유가
// 통째로 다르다 — 붙일 이름이 있다면 腹金이지 腹銀이 아니다.
func TestPromotedSilverDoesNotGetTheSilverNames(t *testing.T) {
	// 5五玉(後手) 옆의 4五에 先手 成銀
	belly := forkBoard(t, "9/9/9/9/4k+S3/9/9/9/8K b - 1")
	if got, ok := BellySilver(belly, shogi.SquareOf(4, 5), shogi.Black); ok {
		t.Errorf("成銀인데 %s 가 떴다", got.Code)
	}

	// 5五桂(後手)의 머리 5六에 先手 成銀
	head := forkBoard(t, "4k4/9/9/9/4n4/4+S4/9/9/8K b - 1")
	if got, ok := KnightHeadSilver(head, shogi.SquareOf(5, 6), shogi.Black); ok {
		t.Errorf("成銀인데 %s 가 떴다", got.Code)
	}
}
