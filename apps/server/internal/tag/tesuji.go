package tag

import "github.com/jovid18/show-gi/apps/server/internal/shogi"

// 手筋은 **관계**다 — 囲い처럼 「어느 칸에 무엇이 있나」가 아니라 「이 駒가 무엇을
// 동시에 노리는가」다. 그래서 좌표 집합으로 못 적고, 대신 룰 엔진에 물어야 한다.
//
// 両取り 하나가 **이름 넷을 덮는다** — ふんどしの桂·割打ちの銀·十字飛車·角による両取り는
// 「어느 駒가 두 개를 동시에 노리는가」의 駒 종류만 다른 같은 手筋이다. 그래서 술어를
// 하나 쓰고 이름을 駒로 고른다.
//
// **串刺し(田楽刺し)는 술어가 다르다.** 両取り는 두 방향으로 갈라 노리는 것이라 상대가
// 하나를 구할 수 있는데, 串刺し는 한 줄에 둘이 겹쳐 있어 앞을 치우면 뒤가 드러난다.
// 같은 함수로 묶으면 「둘을 노린다」가 되어 그 차이가 사라진다([09-tags.md](../../../../docs/09-tags.md)).
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
	if !forkSurvives(pos, sq, c, mine) {
		return Tag{}, false
	}
	return name, true
}

// forkSurvives 는 **両取り가 실제로 성립하는지**를 본다 — 상대가 그 駒를 치워 버리고
// 끝낼 수 있으면 성립하지 않는다.
//
// 처음에는 タダ捨て와 같은 질문(「딸 수 있고 되딸 수 없는가」)으로 물었는데 **틀렸다.**
// 桂로 金 둘을 노렸지만 그 桂가 상대 歩에 잡히는 자리였던 경우가 통과했다 — 내가 되딸
// 수 있으니 「공짜」는 아니어서다. 그런데 両取り의 성립은 그것과 다른 문제다:
//
//	상대 차례가 먼저 온다. 저쪽은 歩로 桂를 따고, 나는 桂(4)를 주고 歩(1)를 얻는다.
//	되따는 것과 両取り가 성립하는 것은 별개다 — **노린 둘 중 아무것도 못 딴다.**
//
// 그래서 조건이 두 갈래다. 상대가 그 駒를 딸 수 있을 때,
//
//	싼 駒로 딴다   → 되따도 손해다. 両取り가 아니다
//	비싼 駒로 딴다 → 되딸 수 있으면 저쪽이 손해라 안 딴다. 両取り는 살아 있다
//	              → 되딸 수 없으면 그냥 공짜다. 両取り가 아니다
//
// **같은 값으로 따는 것도 탈락이다.** 銀으로 銀을 따고 내가 되따면 駒는 본전이지만
// 노린 둘은 그대로 살아 있어서, 「両取りです」라고 말한 것이 결과적으로 거짓이 된다.
func forkSurvives(pos shogi.Position, sq int, c shogi.Color, mine int) bool {
	after := pos
	after.Turn = c.Other()

	for _, m := range after.LegalMoves() {
		if int(m.To) != sq {
			continue
		}
		// **打은 여기 올 수 없다.** 打는 빈 칸에만 놓으므로 내 駒가 선 sq 를 겨냥할 수
		// 없고, 위의 `m.To != sq` 에서 이미 걸러진다. 그런데 `m.From` 은 打일 때 -1 이라
		// 방어 없이 `Board[m.From]` 을 읽으면 **범위 밖 접근으로 죽는다** — 지금은 도달할
		// 수 없어 안전할 뿐이라, 그 사실을 조건으로 적어 둔다.
		if m.IsDrop() {
			continue
		}
		if taker := shogi.PieceValue(after.Board[m.From].Type()); taker <= mine {
			return false // 싸거나 같은 駒로 치워진다 — 노린 둘이 다 살아남는다
		}
		// 비싼 駒로 딴다. 되딸 수 없으면 공짜로 잃는 것이라 이것도 아니다.
		next := after.Apply(m)
		recapture := false
		for _, back := range next.LegalMoves() {
			if int(back.To) == sq {
				recapture = true
				break
			}
		}
		if !recapture {
			return false
		}
	}
	return true
}

var dengaku = Tag{Code: "dengaku_zashi", NameJa: "田楽刺し", Kind: KindTesuji}

// Skewer 는 田楽刺し를 본다 — **香가 한 筋의 상대 駒 둘을 串刺し로 꿰는 것.**
//
// 両取り와 형태가 다르다. 両取り는 두 방향으로 갈라 노리는 것이라 상대가 하나를 구할 수
// 있는데, 串刺し는 **앞의 駒를 치우면 뒤가 그대로 드러난다** — 뒤의 駒는 도망갈 데가
// 없으면 못 구한다. 그래서 조건이 「둘을 노린다」가 아니라 **「한 줄에 둘이 있다」**다.
//
// 값을 앞뒤로 나눠 본다. 앞은 香보다 싸도 되지만 **뒤는 香보다 비싸야 한다** — 香를
// 던져 얻는 것이 뒤의 駒라서, 그것이 香보다 싸면 꿰어도 남는 것이 없다. 실제 田楽刺し가
// 飛나 金을 뒤에 두고 걸리는 이유가 그것이다.
func Skewer(pos shogi.Position, sq int, c shogi.Color) (Tag, bool) {
	p := pos.Board[sq]
	if p.Empty() || p.Color() != c || p.Type() != shogi.Lance {
		return Tag{}, false
	}

	step := -1 // 先手 香는 段이 줄어드는 쪽으로 간다
	if c == shogi.White {
		step = 1
	}

	var found []shogi.Piece
	for rank := shogi.RankOf(sq) + step; rank >= 1 && rank <= 9; rank += step {
		q := pos.Board[shogi.SquareOf(shogi.FileOf(sq), rank)]
		if q.Empty() {
			continue
		}
		if q.Color() == c {
			break // 자기 駒에 막힌다. 그 뒤는 안 보인다
		}
		if found = append(found, q); len(found) == 2 {
			break
		}
	}
	if len(found) < 2 {
		return Tag{}, false
	}

	mine := shogi.PieceValue(shogi.Lance)
	if shogi.PieceValue(found[1].Type()) <= mine {
		return Tag{}, false // 뒤가 香보다 싸면 꿰어도 남는 것이 없다
	}
	if !forkSurvives(pos, sq, c, mine) {
		return Tag{}, false
	}
	return dengaku, true
}

// tesujiFinders 는 자리·관계로 정해지는 手筋 전부다. 추가하면 FindTesuji 가 자동으로 훑는다.
var tesujiFinders = []func(shogi.Position, int, shogi.Color) (Tag, bool){
	Fork, Skewer, BellySilver, KnightHeadSilver, BottomPawn,
}

// FindTesuji 는 그 색이 지금 걸고 있는 手筋 전부를 낸다.
//
// **「둘 수 있는 手筋」이 아니라 「이미 걸린 手筋」이다.** 후보 수를 전부 둬 보며 찾는
// 쪽이 제안형 힌트가 원하는 것이지만(착수 前에 알려야 한다), 그것은 합법수마다 판을
// 만들어 술어를 돌리는 것이라 비용이 다르고 빈도 게이트가 함께 있어야 한다.
// 지금 필요한 것은 술어가 맞는지이고, 그건 이쪽으로 잰다.
func FindTesuji(pos shogi.Position, c shogi.Color) []Tag {
	var out []Tag
	seen := map[string]bool{}

	for sq := range pos.Board {
		for _, find := range tesujiFinders {
			t, ok := find(pos, sq, c)
			if !ok || seen[t.Code] {
				continue
			}
			seen[t.Code] = true
			out = append(out, t)
		}
	}
	return out
}
