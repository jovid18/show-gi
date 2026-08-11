package tag

import (
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// **田楽刺し.** 5九香가 5筋을 따라 5五金과 5三飛를 꿴다.
//
// 뒤가 飛(10)라 香(4)보다 비싸다 — 꿰어 얻는 것이 그것이다.
func TestLanceSkewerIsDengakuZashi(t *testing.T) {
	pos := forkBoard(t, "8k/9/4r4/9/4g4/9/9/9/4L3K b - 1")

	got, ok := Skewer(pos, shogi.SquareOf(5, 9), shogi.Black)
	if !ok {
		t.Fatal("香의 串刺し가 안 잡혔다")
	}
	if got.Code != "dengaku_zashi" || got.Kind != KindTesuji {
		t.Errorf("田楽刺し를 기대했는데 %s (%v)", got.Code, got.Kind)
	}
}

// 한 줄에 하나만 있으면 串刺し가 아니다. 그냥 노리는 것이다.
func TestOnePieceOnTheFileIsNotASkewer(t *testing.T) {
	pos := forkBoard(t, "8k/9/9/9/4r4/9/9/9/4L3K b - 1")

	if got, ok := Skewer(pos, shogi.SquareOf(5, 9), shogi.Black); ok {
		t.Errorf("한 개뿐인데 %s 가 떴다", got.Code)
	}
}

// **뒤가 歩면 꿰는 형태에 이름이 안 붙는다.** 香를 던져 얻는 것이 뒤의 駒다.
//
// 여기까지가 이름의 관례이고, **香와의 값 비교는 하지 않는다** — 뒤가 桂여도 형태는
// 田楽刺し이고 「그래서 得인가」는 엔진이 답한다(game/tesuji.go).
func TestASkewerNeedsSomethingWorthTakingBehind(t *testing.T) {
	// 뒤가 歩
	pos := forkBoard(t, "8k/9/4p4/9/4g4/9/9/9/4L3K b - 1")
	if got, ok := Skewer(pos, shogi.SquareOf(5, 9), shogi.Black); ok {
		t.Errorf("뒤가 歩인데 %s 가 떴다", got.Code)
	}

	// 뒤가 桂(4) — 香(4)와 같지만 형태는 형태다
	pos = forkBoard(t, "8k/9/4n4/9/4g4/9/9/9/4L3K b - 1")
	if _, ok := Skewer(pos, shogi.SquareOf(5, 9), shogi.Black); !ok {
		t.Error("뒤가 桂인 串刺し도 형태는 田楽刺し다")
	}
}

// **자기 駒에 막히면 그 뒤는 안 보인다.** 이것이 없으면 판을 뚫고 세어 거짓이 된다.
func TestMyOwnPieceBlocksTheSkewer(t *testing.T) {
	// 5八에 先手 歩. 그 위의 金·飛는 香에게 안 보인다
	pos := forkBoard(t, "8k/9/4r4/9/4g4/9/9/4P4/4L3K b - 1")

	if got, ok := Skewer(pos, shogi.SquareOf(5, 9), shogi.Black); ok {
		t.Errorf("자기 歩에 막혔는데 %s 가 떴다", got.Code)
	}
}

// 香가 아닌 駒에는 이 이름을 안 붙인다 — 飛도 縦으로 꿰지만 田楽刺し는 香의 이름이다.
func TestOnlyALanceCanBeDengakuZashi(t *testing.T) {
	pos := forkBoard(t, "8k/9/4r4/9/4g4/9/9/9/4R3K b - 1")

	if got, ok := Skewer(pos, shogi.SquareOf(5, 9), shogi.Black); ok {
		t.Errorf("飛인데 %s 가 떴다", got.Code)
	}
}

// 後手도 같은 규칙에서 나와야 한다 — 香의 진행 방향만 뒤집힌다.
func TestSkewerMirrorsForGote(t *testing.T) {
	// 5一後手香가 아래로 5五金·5七飛를 꿴다
	pos := forkBoard(t, "4l3k/9/9/9/4G4/9/4R4/9/8K w - 1")

	if _, ok := Skewer(pos, shogi.SquareOf(5, 1), shogi.White); !ok {
		t.Error("後手 香의 串刺し가 안 잡혔다")
	}
}
