package tag

import "github.com/jovid18/show-gi/apps/server/internal/shogi"

// 手筋은 **관계**다 — 囲い처럼 「어느 칸에 무엇이 있나」가 아니라 「이 駒가 무엇을
// 동시에 노리는가」다. 그래서 좌표 집합으로 못 적고, 대신 룰 엔진에 물어야 한다.
//
// 両取り 하나가 **将棋ウォーズ의 이름 넷을 덮는다** — ふんどしの桂·割打ちの銀·十字飛車·
// 角による両取り는 「어느 駒가 두 개를 동시에 노리는가」의 駒 종류만 다른 같은 手筋이다
// ([09-tags.md](../../../../docs/09-tags.md)). 그래서 술어를 하나 쓰고 이름을 駒로 고른다.
//
// **화면에 바로 안 붙인다.** 이것을 그대로 스냅샷에 실으면 힌트가 되는데, 제안형 힌트에는
// 빈도 상한과 레벨 연동이 조건으로 걸려 있고([01-core.md §7](../../../../docs/01-core.md))
// 그 게이트가 아직 없다. 감지기 없이 게이트를 만들면 켤 것이 없고, 게이트 없이 내보내면
// 남발한다 — 그래서 술어가 먼저다.

const (
	KindTesuji Kind = "tesuji" // 手筋 — 駒 사이의 관계
)

// forkNames 는 両取り를 건 駒마다의 이름이다.
//
// 이름이 駒로 갈리는 것이 이 手筋의 성질이다. 「桂で両取り」에는 ふんどしの桂라는 고유한
// 이름이 있고, 초심자에게는 그 이름이 곧 학습 단위다([01-core.md §7.1](../../../../docs/01-core.md)의
// 계보형이 같은 이야기다) — 「両取り」라고만 말하면 다음에 그 형태를 못 알아본다.
var forkNames = map[shogi.PieceType]Tag{
	shogi.Knight: {Code: "fundoshi_no_kei", NameJa: "ふんどしの桂", Kind: KindTesuji},
	shogi.Silver: {Code: "wariuchi_no_gin", NameJa: "割打ちの銀", Kind: KindTesuji},
	shogi.Rook:   {Code: "juji_bisha", NameJa: "十字飛車", Kind: KindTesuji},
	shogi.Bishop: {Code: "kaku_ryodori", NameJa: "角による両取り", Kind: KindTesuji},
}

// distinctSquares 는 딸 수 있는 **칸**의 수다.
//
// 수를 세면 거짓말이 된다 — 같은 駒를 成·不成으로 딸 수 있으면 수가 둘이지만 노리는
// 것은 하나다. `explain` 이 매수를 셀 때 같은 것에 물렸고, 그때도 에러가 안 났다(§27).
//
// **手番을 이 색으로 맞춰서 묻는다.** `LegalMoves` 는 `pos.Turn` 쪽의 수만 내므로,
// 상대 차례인 국면에서 그대로 물으면 조용히 빈 결과가 온다 — 에러가 아니라 「両取りが
// 없다」로 보이는 종류의 버그다.
func distinctSquares(pos shogi.Position, sq int, c shogi.Color) map[int]int {
	pos.Turn = c

	targets := map[int]int{}
	for _, m := range pos.LegalMoves() {
		if m.IsDrop() || int(m.From) != sq {
			continue
		}
		if cap := pos.Board[m.To]; !cap.Empty() && cap.Type() != shogi.Pawn {
			targets[int(m.To)] = shogi.PieceValue(cap.Type())
		}
	}
	return targets
}

// Fork 는 sq 의 駒가 지금 両取り를 걸고 있는지 본다.
//
// 조건 셋이고, **뒤의 둘이 없으면 両取り가 아닌 것을 両取り라고 말한다.**
//
//	① 값나가는 상대 駒 **두 칸 이상**을 합법수로 딸 수 있다
//	② 그중 둘 이상이 이 駒보다 **싸지 않다** — 銀으로 歩と桂를 노리는 것은 得이 아니다
//	③ 이 駒 자신이 **공짜로 잡히지 않는다** — 잡히면 両取り를 걸어도 손해다
//
// ③ 이 특히 중요하다. 角을 던져 金 둘을 노려도 그 角이 그냥 잡히면 手筋이 아니라 タダ捨て
// 이고, 같은 국면을 개입 쪽은 블런더라고 판정한다. 두 판정이 어긋나면 화면이 「その角は
// 取り返せない場所にあります」라고 가르쳐놓고 힌트는 「両取りがあります」라고 말한다.
func Fork(pos shogi.Position, sq int, c shogi.Color) (Tag, bool) {
	p := pos.Board[sq]
	if p.Empty() || p.Color() != c {
		return Tag{}, false
	}
	name, named := forkNames[p.Type().Base()]
	if !named {
		return Tag{}, false
	}

	mine := shogi.PieceValue(p.Type())
	worth := 0
	for _, v := range distinctSquares(pos, sq, c) {
		if v >= mine {
			worth++
		}
	}
	if worth < 2 {
		return Tag{}, false
	}
	if hangs(pos, sq, c) {
		return Tag{}, false
	}
	return name, true
}

// hangs 는 그 칸의 駒를 상대가 공짜로 딸 수 있는지 본다 — 딸 수 있고 되딸 수 없으면 참.
//
// タダ捨て 판정과 같은 질문이라 **정의가 갈리면 안 된다**([01-core.md §6](../../../../docs/01-core.md)이
// 적응형 상대의 자살수 필터에 대해 말하는 것과 같은 이유다). 여기서는 `game` 을 import할
// 수 없으므로(순환) 같은 방식으로 다시 묻는다 — 利き이 아니라 **합법수**로.
func hangs(pos shogi.Position, sq int, c shogi.Color) bool {
	after := pos
	after.Turn = c.Other()

	for _, m := range after.LegalMoves() {
		if int(m.To) != sq {
			continue
		}
		// 딴 뒤에 내가 되딸 수 있으면 공짜가 아니다.
		next := after.Apply(m)
		for _, back := range next.LegalMoves() {
			if int(back.To) == sq {
				return false
			}
		}
		return true
	}
	return false
}

// FindForks 는 그 색이 지금 걸고 있는 両取り 전부를 낸다.
//
// **「둘 수 있는 両取り」가 아니라 「이미 걸린 両取り」다.** 후보 수를 전부 둬 보며 찾는
// 쪽이 제안형 힌트가 원하는 것이지만(착수 前에 알려야 한다), 그것은 합법수마다 판을
// 만들어 이 술어를 돌리는 것이라 비용이 다르고 빈도 게이트가 함께 있어야 한다.
// 지금 필요한 것은 술어가 맞는지이고, 그건 이쪽으로 잰다.
func FindForks(pos shogi.Position, c shogi.Color) []Tag {
	var out []Tag
	seen := map[string]bool{}

	for sq := range pos.Board {
		t, ok := Fork(pos, sq, c)
		if !ok || seen[t.Code] {
			continue
		}
		seen[t.Code] = true
		out = append(out, t)
	}
	return out
}
