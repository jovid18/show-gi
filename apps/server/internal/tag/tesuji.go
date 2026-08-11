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
// **이득인지는 여기서 묻지 않는다.** 이 패키지가 정하는 것은 **형태와 이름**뿐이고,
// 「그래서 得인가」는 `game` 의 엔진 게이트가 정한다(`internal/game/tesuji.go`).
//
// 원래는 여기서 손으로 읽었다 — 상대가 그 駒를 딸 수 있나, 내가 되딸 수 있나를 값 표로
// 비교하는 `forkSurvives` 가 있었다. **그게 1수 읽기를 손으로 쓴 것이고, 실제로 틀렸다**:
// 桂로 金 둘을 노렸지만 그 桂가 歩에 잡히는 자리였던 국면을 통과시켰다. 엔진은 같은 것을
// depth 12로 하고, 매 수 `Analyst.Judge` 가 이미 그것을 돌린다 —
// [09-tags.md §5](../../../../docs/09-tags.md).
//
// 자리를 나누면 **이 패키지가 엔진 없이 테스트된다.** `intervene` 이 엔진을 모르고 이미
// 구해진 평가치만 받는 것과 같은 구조이고, AND는 둘 다 손에 든 `game` 에서 걸린다.

const (
	KindTesuji Kind = "tesuji" // 手筋 — 駒 사이의 관계
)

// forkNames 는 両取り를 건 駒마다의 이름이다.
//
// 이름이 駒로 갈리는 것이 이 手筋의 성질이다. 「桂で両取り」에는 ふんどしの桂라는 고유한
// 이름이 있고, 초심자에게는 그 이름이 곧 학습 단위다([01-core.md §7.1](../../../../docs/01-core.md)의
// 계보형이 같은 이야기다) — 「両取り」라고만 말하면 다음에 그 형태를 못 알아본다.
//
// **成った 駒는 표에 있는 것만 든다.** 기준은 하나다 — **이름이 말하는 성질이 남아
// 있는가**(腹銀에서 成銀을 뺀 것과 같은 기준이다, placement.go).
//
//	龍·馬   든다.  飛의 縦横 · 角의 斜め를 그대로 갖는다. 다만 **더 갖는 것**이 있어서
//	              (한 칸씩의 덤) 방향은 forkShape 에서 따로 본다
//	成桂·成銀  뺀다.  둘 다 金의 움직임이 되어 「桂가 둘로 뛴다」도 「銀이 사이에 打つ」도
//	              성립하지 않는다. 이름만 남고 이유가 사라진 자리다
//
// 실제로 실 기보에서 `▲5二成銀` 에 「割打ちの銀」이 떴다(06-status.md §34 ⑤).
var forkNames = map[shogi.PieceType]Tag{
	shogi.Knight:     {Code: "fundoshi_no_kei", NameJa: "ふんどしの桂", Kind: KindTesuji},
	shogi.Silver:     {Code: "wariuchi_no_gin", NameJa: "割打ちの銀", Kind: KindTesuji},
	shogi.Rook:       {Code: "juji_bisha", NameJa: "十字飛車", Kind: KindTesuji},
	shogi.PromRook:   {Code: "juji_bisha", NameJa: "十字飛車", Kind: KindTesuji},
	shogi.Bishop:     {Code: "kaku_ryodori", NameJa: "角による両取り", Kind: KindTesuji},
	shogi.PromBishop: {Code: "kaku_ryodori", NameJa: "角による両取り", Kind: KindTesuji},
}

// targetSquares 는 그 駒가 딸 수 있는 상대 **駒가 선 칸들**이다.
//
// 수를 세면 거짓말이 된다 — 같은 駒를 成·不成으로 딸 수 있으면 수가 둘이지만 노리는
// 것은 하나다. `explain` 이 매수를 셀 때 같은 것에 물렸고, 그때도 에러가 안 났다(§27).
//
// **歩는 대상에서 뺀다.** 이것은 손익 계산이 아니라 **이름의 관례**다 — 歩 둘을 노리는
// 형태에는 両取り라는 이름이 붙지 않는다. 값 비교(「내 駒보다 싼가」)는 여기서 하지
// 않고 엔진이 한다. 둘을 섞으면 형태와 이득이 다시 한 함수에 뭉친다.
//
// **手番을 이 색으로 맞춰서 묻는다.** `LegalMoves` 는 `pos.Turn` 쪽의 수만 내므로,
// 상대 차례인 국면에서 그대로 물으면 조용히 빈 결과가 온다 — 에러가 아니라 「両取りが
// 없다」로 보이는 종류의 버그다.
func targetSquares(pos shogi.Position, sq int, c shogi.Color) []int {
	pos.Turn = c

	seen := map[int]bool{}
	var targets []int
	for _, m := range pos.LegalMoves() {
		if m.IsDrop() || int(m.From) != sq || seen[int(m.To)] {
			continue
		}
		if cap := pos.Board[m.To]; !cap.Empty() && cap.Type() != shogi.Pawn {
			seen[int(m.To)] = true
			targets = append(targets, int(m.To))
		}
	}
	return targets
}

// forkShape 는 **그 이름이 말하는 형태인가**를 본다.
//
// 두 이름이 방향을 말한다. 「十字」는 縦과 横이 **교차**하는 것이고, 「角」은 斜め다.
// 세는 것을 「둘 이상」으로만 두면 같은 段의 둘을 노리는 飛도 十字飛車가 되고, 龍이 덤으로
// 얻은 한 칸(斜め)으로 노린 것까지 十字가 된다 — 둘 다 화면에 그대로 나가는 거짓말이다.
// 이것이 룰 층에 값 비교 대신 남아야 하는 종류의 조건이다: **이름이 조건을 정한다.**
//
// 桂·銀은 방향을 말하지 않는다(ふんどし는 「여럿을 늘어놓는 것」, 割打ち는 「사이에 打つ 것」).
// 그래서 그쪽은 둘이면 된다.
func forkShape(pt shogi.PieceType, sq int, targets []int) bool {
	file, rank := shogi.FileOf(sq), shogi.RankOf(sq)

	switch pt.Base() {
	case shogi.Rook:
		var vertical, horizontal bool
		for _, to := range targets {
			switch {
			case shogi.FileOf(to) == file:
				vertical = true
			case shogi.RankOf(to) == rank:
				horizontal = true
			}
		}
		return vertical && horizontal

	case shogi.Bishop:
		n := 0
		for _, to := range targets {
			if abs(shogi.FileOf(to)-file) == abs(shogi.RankOf(to)-rank) {
				n++
			}
		}
		return n >= 2

	default:
		return len(targets) >= 2
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Fork 는 sq 의 駒가 지금 両取り의 **형태**를 이루고 있는지 본다 — 이름이 있는 駒가
// 상대 駒 두 칸 이상을 합법수로 딸 수 있다.
//
// **「그래서 得인가」는 묻지 않는다.** 그것을 여기서 물었을 때 정확히 한 번 틀렸고
// (파일 머리의 `forkSurvives`), 그 질문은 미래를 읽는 일이라 엔진의 자리다. 이 함수의
// 답은 국면만 보고 결정적으로 나오는 「이 형태의 이름」이다.
//
// 그래서 이 결과를 **그대로 화면에 실으면 안 된다.** 공짜로 잡히는 角으로 金 둘을
// 노린 국면도 여기서는 「角による両取り」로 나오고, 개입 쪽은 같은 수를 블런더라고
// 한다 — 두 판정이 어긋나면 화면이 「その角は取り返せない場所にあります」라고
// 가르쳐놓고 힌트는 「両取りがあります」라고 말한다. 그 AND를 `game` 이 건다.
func Fork(pos shogi.Position, sq int, c shogi.Color) (Tag, bool) {
	p := pos.Board[sq]
	if p.Empty() || p.Color() != c {
		return Tag{}, false
	}
	name, named := forkNames[p.Type()]
	if !named {
		return Tag{}, false
	}
	if !forkShape(p.Type(), sq, targetSquares(pos, sq, c)) {
		return Tag{}, false
	}
	return name, true
}

var dengaku = Tag{Code: "dengaku_zashi", NameJa: "田楽刺し", Kind: KindTesuji}

// Skewer 는 田楽刺し를 본다 — **香가 한 筋의 상대 駒 둘을 串刺し로 꿰는 것.**
//
// 両取り와 형태가 다르다. 両取り는 두 방향으로 갈라 노리는 것이라 상대가 하나를 구할 수
// 있는데, 串刺し는 **앞의 駒를 치우면 뒤가 그대로 드러난다** — 뒤의 駒는 도망갈 데가
// 없으면 못 구한다. 그래서 조건이 「둘을 노린다」가 아니라 **「한 줄에 둘이 있다」**다.
//
// 앞뒤를 나눠 보는 것은 남는다. 앞은 무엇이든 되지만 **뒤는 歩가 아니어야 한다** —
// 香를 던져 얻는 것이 뒤의 駒라서, 뒤가 歩면 꿰어도 이름이 붙는 형태가 아니다.
// 両取り가 歩를 대상에서 빼는 것과 같은 관례이고(targetSquares), **香와의 값 비교는
// 하지 않는다** — 「그래서 得인가」는 엔진이 답한다.
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

	if found[1].Type() == shogi.Pawn {
		return Tag{}, false // 뒤가 歩면 꿰는 형태에 이름이 안 붙는다
	}
	return dengaku, true
}

// tesujiFinders 는 자리·관계로 정해지는 手筋 전부다. 추가하면 FindTesuji 가 자동으로 훑는다.
var tesujiFinders = []func(shogi.Position, int, shogi.Color) (Tag, bool){
	Fork, Skewer, BellySilver, KnightHeadSilver, BottomPawn,
}

// FindTesuji 는 그 색이 지금 걸고 있는 手筋의 **이름 전부**를 낸다.
//
// **「둘 수 있는 手筋」이 아니라 「이미 걸린 手筋」이다.** 후보 수를 전부 둬 보며 찾는
// 쪽이 제안형 힌트가 원하는 것이지만(착수 前에 알려야 한다), 그것은 합법수마다 판을
// 만들어 술어를 돌리는 것이라 비용이 다르다. 지금 화면에 나가는 것은 **방금 만든 형태에
// 이름을 붙이는 것**이라 이쪽이면 된다 — 붙일지 말지는 엔진이 정한다(`game/tesuji.go`).
//
// 답이 국면에만 매여 있어서 같은 국면이면 같은 결과가 온다. **부르는 쪽이 들고 있어도
// 되고**, 세션이 그렇게 한다 — 이름을 통과시키는 엔진 값이 국면과 함께만 유효해서이지
// 이 함수가 비싸서가 아니다(初期配置에서 90µs 남짓이다).
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
