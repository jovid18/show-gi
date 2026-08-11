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

// **값 비교는 여기서 안 한다.** 飛로 桂 둘을 노리는 것도 형태는 十字飛車이고, 「그래서
// 得인가」는 엔진이 답한다(game/tesuji.go). 원래는 이 조건을 여기서 걸었다.
func TestCheaperTargetsAreStillTheShapeOfAFork(t *testing.T) {
	// 5五飛가 5三桂와 8五桂를 노린다. 桂(4) < 飛(10)
	pos := forkBoard(t, "8k/9/4n4/9/1n2R4/9/9/9/8K b - 1")

	if got, ok := Fork(pos, shogi.SquareOf(5, 5), shogi.Black); !ok || got.Code != "juji_bisha" {
		t.Errorf("형태는 十字飛車다: %s/%v", got.Code, ok)
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

	got := FindTesuji(pos, shogi.Black)
	if len(got) != 1 || got[0].Code != "fundoshi_no_kei" {
		t.Fatalf("[fundoshi_no_kei] 를 기대했는데 %v", codes(got))
	}
	if none := FindTesuji(shogi.StartPosition(), shogi.Black); len(none) != 0 {
		t.Errorf("初期配置에 両取り가 있다고 한다: %v", codes(none))
	}
}

// **이 국면이 자리를 나눈 이유다.** 桂로 金 둘을 노렸는데 그 桂가 상대 歩에 잡히는
// 자리였고, 손으로 쓴 1수 읽기가 그것을 통과시켰다(사람이 짚어 줘서 알았다).
//
// 그래서 안전을 여기서 묻지 않기로 했다. **룰은 형태만 말한다** — 이 국면에서
// ふんどしの桂가 뜨는 것이 맞고, 이름을 화면에 내보내지 않는 일은 엔진 게이트가 한다
// (`game.TestForkThatHangsIsNotNamed` 가 같은 국면을 그쪽에서 다시 잰다).
//
// 룰이 지웠던 조건을 다시 여기 넣으면 두 층이 같은 질문을 두 번 하고, 그때 답이
// 갈리는 쪽은 언제나 얕게 읽는 이쪽이다.
func TestTheRuleLayerDoesNotAskWhetherTheForkerSurvives(t *testing.T) {
	// 4三金·6三金을 5五桂가 노린다. 5四에 後手 歩가 있어 桂를 공짜에 가깝게 딴다.
	pos := forkBoard(t, "8k/9/3g1g3/4p4/4N4/9/9/9/8K b - 1")

	got, ok := Fork(pos, shogi.SquareOf(5, 5), shogi.Black)
	if !ok || got.Code != "fundoshi_no_kei" {
		t.Errorf("형태는 ふんどしの桂다: %s/%v", got.Code, ok)
	}
}

// **「十字」는 縦과 横이 교차한다는 뜻이다.** 같은 段의 둘을 노리는 飛는 両取り이긴 해도
// 十字飛車가 아니고, 그 이름을 붙이면 초심자는 다음에 그 형태를 못 알아본다.
func TestJujiBishaNeedsBothDirections(t *testing.T) {
	// 5五飛가 3五金·8五金을 노린다 — 둘 다 같은 段이다
	flat := forkBoard(t, "8k/9/9/9/1g2R1g2/9/9/9/8K b - 1")
	if got, ok := Fork(flat, shogi.SquareOf(5, 5), shogi.Black); ok {
		t.Errorf("같은 段의 둘인데 %s 가 떴다", got.Code)
	}

	// 5三金(縦)과 8五金(横)이면 十字다
	cross := forkBoard(t, "8k/9/4g4/9/1g2R4/9/9/9/8K b - 1")
	if got, ok := Fork(cross, shogi.SquareOf(5, 5), shogi.Black); !ok || got.Code != "juji_bisha" {
		t.Errorf("縦과 横이면 十字飛車다: %s/%v", got.Code, ok)
	}
}

// **龍·馬가 덤으로 얻은 한 칸은 그 이름의 방향이 아니다.**
//
// `Base()` 로 이름을 고르므로 龍은 十字飛車, 馬는 角による両取り로 온다. 그것 자체는
// 맞는데(縦横·斜め를 그대로 갖는다), 덤으로 얻은 한 칸까지 세면 **十字가 아닌 것에
// 十字라는 이름이 붙는다.**
func TestPromotedRookExtraStepIsNotACross(t *testing.T) {
	// 5五龍이 5三金을 縦으로 노리고, 4四金은 덤으로 얻은 斜め 한 칸이다
	pos := forkBoard(t, "8k/9/4g4/5g3/4+R4/9/9/9/8K b - 1")

	if got, ok := Fork(pos, shogi.SquareOf(5, 5), shogi.Black); ok {
		t.Errorf("縦 하나 + 斜め 하나인데 %s 가 떴다", got.Code)
	}
}

// **成桂·成銀에는 桂·銀의 이름을 안 붙인다.** 둘 다 金의 움직임이 되어 「桂가 둘로 뛴다」도
// 「銀이 사이에 打つ」도 성립하지 않는다 — 腹銀에서 成銀을 뺀 것과 같은 기준이고, 실 기보의
// `▲5二成銀` 에 「割打ちの銀」이 떴던 자리다(06-status.md §34 ⑤).
//
// 龍·馬는 반대로 **든다.** 飛의 縦横과 角의 斜め가 그대로 남아 있어서다.
func TestPromotedPiecesKeepOnlyTheNamesTheyStillEarn(t *testing.T) {
	for _, tc := range []struct {
		name, sfen string
		want       string // 빈 값이면 이름이 붙으면 안 된다
	}{
		{"成桂", "8k/9/9/3g1g3/4+N4/9/9/9/8K b - 1", ""},
		{"成銀", "8k/9/9/3g1g3/4+S4/9/9/9/8K b - 1", ""},
		{"龍", "8k/9/4g4/9/1g2+R4/9/9/9/8K b - 1", "juji_bisha"},
		{"馬", "8k/9/2g1g4/9/4+B4/9/9/9/8K b - 1", "kaku_ryodori"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Fork(forkBoard(t, tc.sfen), shogi.SquareOf(5, 5), shogi.Black)
			if tc.want == "" && ok {
				t.Errorf("이름이 붙으면 안 되는데 %s 가 떴다", got.Code)
			}
			if tc.want != "" && (!ok || got.Code != tc.want) {
				t.Errorf("%s 를 기대했는데 %s/%v", tc.want, got.Code, ok)
			}
		})
	}
}
