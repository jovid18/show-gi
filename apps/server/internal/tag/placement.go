package tag

import "github.com/jovid18/show-gi/apps/server/internal/shogi"

// 자리로 정해지는 手筋 — **駒를 어디에 두었는가**가 곧 이름이다.
//
// 両取り(tesuji.go)와 조건이 하나 갈린다. 저쪽은 駒의 손익 이야기라 「자기가 싼 駒에
// 치워지지 않아야」가 조건인데, **여기는 寄せ 手筋이라 駒損을 전제로 둔다** — 腹銀은
// 玉의 옆에 銀을 붙이는 수이고 그 銀은 잡히는 것이 보통이다. 같은 안전 조건을 걸면
// 실제 手筋이 전부 탈락한다.
//
// 그래서 조건을 「자기가 안전한가」가 아니라 **「그 자리가 그 자리인가」**로 둔다.
// 桂頭の銀이 手筋인 이유가 「桂는 뛰기만 해서 머리의 駒를 못 딴다」인 것처럼, 이 부류는
// 자리 자체에 이유가 들어 있다. (**그래도 이득은 엔진이 정한다** — 잡히는 것을 전제로
// 두는 것과 그냥 던지는 것은 다르고, 그 둘은 depth 12라야 갈린다: `game/tesuji.go`.)
//
// **成銀은 銀이 아니다.** 처음에는 `Base()` 로 물어서 成銀도 걸렸는데, 실전 국면에서
// `▲6二成銀` 에 「腹銀」이 떴다(06-status.md §34). 이름이 銀이라고 말하는데 成銀은 金의
// 움직임이라, 그 자리가 그 자리인 이유가 통째로 다르다 — 붙일 이름이 있다면 腹金이지
// 腹銀이 아니다. 화면에 그대로 나가는 단언이라 그 차이를 지운 채로 두면 틀린 것을 가르친다.

var (
	haraGin    = Tag{Code: "hara_gin", NameJa: "腹銀", Kind: KindTesuji}
	keitouGin  = Tag{Code: "keitou_no_gin", NameJa: "桂頭の銀", Kind: KindTesuji}
	soko_no_fu = Tag{Code: "soko_no_fu", NameJa: "底歩", Kind: KindTesuji}
)

// forwardStep 은 그 색이 전진하는 段 방향이다. 先手는 段이 줄어드는 쪽으로 간다.
func forwardStep(c shogi.Color) int {
	if c == shogi.Black {
		return -1
	}
	return 1
}

// backRank 는 그 색의 자기 진영 맨 아래 段이다 (先手 9段 · 後手 1段).
func backRank(c shogi.Color) int {
	if c == shogi.Black {
		return 9
	}
	return 1
}

// onBoard 는 (筋, 段)이 판 안인지.
func onBoard(file, rank int) bool {
	return file >= 1 && file <= 9 && rank >= 1 && rank <= 9
}

// BellySilver 는 腹銀을 본다 — **銀이 상대 玉의 옆(같은 段, 한 筋 차이)에 붙어 있다.**
//
// 玉의 「배」에 붙는다는 이름 그대로다. 위나 아래가 아니라 **옆**인 것이 요점이다 —
// 玉頭(위)에 두는 것은 다른 手筋이고, 옆은 玉의 도망갈 筋을 막는다.
//
// **안전을 묻지 않는다.** 붙인 銀은 잡히는 것이 보통이고, 그래도 寄せ가 성립하는 것이
// 이 手筋의 내용이다. 잡히면 안 된다고 요구하면 실전의 腹銀이 하나도 안 걸린다.
func BellySilver(pos shogi.Position, sq int, c shogi.Color) (Tag, bool) {
	p := pos.Board[sq]
	if p.Empty() || p.Color() != c || p.Type() != shogi.Silver {
		return Tag{}, false
	}

	king := pos.KingSquare(c.Other())
	if king < 0 {
		return Tag{}, false
	}
	if shogi.RankOf(king) != shogi.RankOf(sq) {
		return Tag{}, false
	}
	if d := shogi.FileOf(king) - shogi.FileOf(sq); d == 1 || d == -1 {
		return haraGin, true
	}
	return Tag{}, false
}

// KnightHeadSilver 는 桂頭の銀을 본다 — **銀이 상대 桂의 바로 앞 칸에 있다.**
//
// 手筋인 이유가 桂의 움직임에 있다. **桂는 뛰기만 해서 자기 머리의 駒를 딸 수 없다** —
// 그래서 그 자리에 놓인 銀은 桂에게 안전하고, 桂는 도망가거나 잡히거나를 고른다.
// 「머리」는 桂 주인의 전진 방향 한 칸이다.
func KnightHeadSilver(pos shogi.Position, sq int, c shogi.Color) (Tag, bool) {
	p := pos.Board[sq]
	if p.Empty() || p.Color() != c || p.Type() != shogi.Silver {
		return Tag{}, false
	}

	// 銀의 한 칸 **뒤**(상대 쪽에서 보면 앞)에 상대 桂가 서 있으면 그 桂의 머리다.
	enemy := c.Other()
	file, rank := shogi.FileOf(sq), shogi.RankOf(sq)-forwardStep(enemy)
	if !onBoard(file, rank) {
		return Tag{}, false
	}

	q := pos.Board[shogi.SquareOf(file, rank)]
	if q.Empty() || q.Color() != enemy || q.Type() != shogi.Knight {
		return Tag{}, false
	}
	return keitouGin, true
}

// BottomPawn 은 底歩(金底の歩)를 본다 — **자기 진영 맨 아래 段의 歩가 그 위의 金을
// 받치고 있다.**
//
// 「金底の歩、岩より堅し」라는 말이 있는 자리다. 金 아래에 歩가 있으면 **飛로 그 段을
// 찔러도 金이 밀려나지 않는다** — 歩가 받치고 있어 打ち込み이 통하지 않는다.
//
// 金만 본다. 銀 아래의 歩는 같은 말을 하지 않고, 이름도 「金底の歩」다.
func BottomPawn(pos shogi.Position, sq int, c shogi.Color) (Tag, bool) {
	p := pos.Board[sq]
	if p.Empty() || p.Color() != c || p.Type() != shogi.Pawn {
		return Tag{}, false
	}
	if shogi.RankOf(sq) != backRank(c) {
		return Tag{}, false
	}

	file, rank := shogi.FileOf(sq), shogi.RankOf(sq)+forwardStep(c)
	if !onBoard(file, rank) {
		return Tag{}, false
	}

	q := pos.Board[shogi.SquareOf(file, rank)]
	if q.Empty() || q.Color() != c || q.Type() != shogi.Gold {
		return Tag{}, false
	}
	return soko_no_fu, true
}
