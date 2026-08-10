package tag

import (
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// forkBoard 는 両取り를 재기 위한 최소 국면을 만든다.
//
// 양쪽 玉을 반드시 넣는다 — `LegalMoves` 는 王手 회피를 따지므로 玉이 없으면 합법수가
// 무엇인지 자체가 달라지고, 그러면 무엇을 재는지 알 수 없다.
func forkBoard(t *testing.T, sfen string) shogi.Position {
	t.Helper()
	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		t.Fatalf("SFEN 파싱 실패: %v", err)
	}
	return pos
}

// **ふんどしの桂.** 桂가 두 金을 동시에 노린다.
//
// 5五의 桂는 4三과 6三으로 뛴다. 그 두 칸에 後手 金을 놓으면 両取り다.
func TestKnightForkIsFundoshiNoKei(t *testing.T) {
	// 3段: 6三金, 4三金 / 5段: 5五桂 / 玉은 서로 멀리
	pos := forkBoard(t, "8k/9/3g1g3/9/4N4/9/9/9/8K b - 1")

	got, ok := Fork(pos, shogi.SquareOf(5, 5), shogi.Black)
	if !ok {
		t.Fatal("桂의 両取り가 안 잡혔다")
	}
	if got.Code != "fundoshi_no_kei" {
		t.Errorf("ふんどしの桂를 기대했는데 %s", got.Code)
	}
	if got.Kind != KindTesuji {
		t.Errorf("축이 手筋이 아니다: %v", got.Kind)
	}
}

// **한 개만 노리는 것은 両取り가 아니다.** 이 음성 테스트가 없으면 술어가 그냥
// 「값나가는 駒를 노린다」가 되고, 그건 거의 모든 국면에서 참이다.
func TestOneTargetIsNotAFork(t *testing.T) {
	pos := forkBoard(t, "8k/9/3g5/9/4N4/9/9/9/8K b - 1") // 6三金 하나만

	if got, ok := Fork(pos, shogi.SquareOf(5, 5), shogi.Black); ok {
		t.Errorf("한 개만 노리는데 %s 가 떴다", got.Code)
	}
}

// **歩 둘을 노리는 것은 両取り가 아니다.** 歩는 세지 않는다.
func TestPawnsAreNotForkTargets(t *testing.T) {
	pos := forkBoard(t, "8k/9/3p1p3/9/4N4/9/9/9/8K b - 1")

	if got, ok := Fork(pos, shogi.SquareOf(5, 5), shogi.Black); ok {
		t.Errorf("歩 둘을 노리는데 %s 가 떴다", got.Code)
	}
}

// **자기보다 싼 것만 노리는 것은 得이 아니다.** 飛로 桂 둘을 노려도 両取り라고
// 부르지 않는다 — 딴 쪽이 손해다.
func TestTargetsCheaperThanTheForkerDoNotCount(t *testing.T) {
	// 5五飛가 5三桂와 8五桂를 노린다. 桂(4) < 飛(10)
	pos := forkBoard(t, "8k/9/4n4/9/1n2R4/9/9/9/8K b - 1")

	if got, ok := Fork(pos, shogi.SquareOf(5, 5), shogi.Black); ok {
		t.Errorf("飛가 桂 둘을 노리는데 %s 가 떴다", got.Code)
	}
}

// **공짜로 잡히는 駒는 手筋이 아니라 タダ捨て다.** 이 조건이 없으면 같은 국면을
// 개입은 블런더라고 하고 힌트는 両取り라고 해서, 화면이 서로 반대를 가르친다.
func TestAForkerThatHangsIsNotATesuji(t *testing.T) {
	// 5五角이 3三金·7三金을 노리는데, 5四에 後手 歩가 있어 角을 공짜로 딴다.
	pos := forkBoard(t, "8k/9/2g3g2/4p4/4B4/9/9/9/8K b - 1")

	if !hangs(pos, shogi.SquareOf(5, 5), shogi.Black) {
		t.Fatal("전제가 깨졌다: 5五의 角이 공짜로 잡히지 않는다")
	}
	if got, ok := Fork(pos, shogi.SquareOf(5, 5), shogi.Black); ok {
		t.Errorf("공짜로 잡히는 角인데 %s 가 떴다", got.Code)
	}
}

// **手番이 상대일 때도 같은 답이 나와야 한다.** `LegalMoves` 는 `pos.Turn` 쪽 수만
// 내므로, 手番을 맞추지 않으면 조용히 「両取りが없다」가 된다 — 에러가 안 나는 버그다.
func TestForkIsFoundRegardlessOfWhoseTurnItIs(t *testing.T) {
	const board = "8k/9/3g1g3/9/4N4/9/9/9/8K "

	black := forkBoard(t, board+"b - 1")
	white := forkBoard(t, board+"w - 1")

	got1, ok1 := Fork(black, shogi.SquareOf(5, 5), shogi.Black)
	got2, ok2 := Fork(white, shogi.SquareOf(5, 5), shogi.Black)

	if !ok1 || !ok2 || got1.Code != got2.Code {
		t.Errorf("手番으로 답이 갈렸다: %v/%v vs %v/%v", got1.Code, ok1, got2.Code, ok2)
	}
}

// 상대 駒로는 내 手筋이 성립하지 않는다.
func TestForkNeedsMyOwnPiece(t *testing.T) {
	pos := forkBoard(t, "8k/9/3G1G3/9/4n4/9/9/9/8K b - 1") // 5五는 後手 桂

	if got, ok := Fork(pos, shogi.SquareOf(5, 5), shogi.Black); ok {
		t.Errorf("後手의 桂인데 先手 手筋로 %s 가 떴다", got.Code)
	}
}

// 両取り의 이름이 없는 駒는 붙이지 않는다 — 金으로 둘을 노려도 고유한 이름이 없다.
func TestOnlyNamedPiecesGetAForkName(t *testing.T) {
	pos := forkBoard(t, "8k/9/9/3g1g3/4G4/9/9/9/8K b - 1") // 5五金

	if got, ok := Fork(pos, shogi.SquareOf(5, 5), shogi.Black); ok {
		t.Errorf("金의 両取り에는 이름이 없는데 %s 가 떴다", got.Code)
	}
}

// FindForks 는 판 전체를 훑고 이름을 겹쳐 내지 않는다.
func TestFindForksScansTheBoardWithoutDuplicates(t *testing.T) {
	pos := forkBoard(t, "8k/9/3g1g3/9/4N4/9/9/9/8K b - 1")

	got := FindForks(pos, shogi.Black)
	if len(got) != 1 || got[0].Code != "fundoshi_no_kei" {
		t.Fatalf("[fundoshi_no_kei] 를 기대했는데 %v", codes(got))
	}
	if none := FindForks(shogi.StartPosition(), shogi.Black); len(none) != 0 {
		t.Errorf("初期配置에 両取り가 있다고 한다: %v", codes(none))
	}
}
