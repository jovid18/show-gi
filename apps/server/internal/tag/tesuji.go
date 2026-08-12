package tag

import "github.com/jovid18/show-gi/apps/server/internal/shogi"

// 手筋은 좌표가 아니라 **관계**라 룰 엔진에 합법수로 묻는다. 이 패키지가 정하는 것은
// **형태와 이름**뿐이고 「그래서 得인가」는 `game` 의 엔진 게이트가 건다 — 경위는
// [09-tags.md §5](../../../../docs/09-tags.md).

const (
	KindTesuji Kind = "tesuji" // 手筋 — 駒 사이의 관계
)

// forkNames 는 両取り를 건 駒마다의 이름이다 — 이름이 駒로 갈리는 것이 이 手筋의 성질이라
// 「両取り」로 뭉치지 않는다. 成桂·成銀을 뺀 기준과 龍·馬의 **[미확정]** 은
// 06-status.md §34 ⑤ · §44.
var forkNames = map[shogi.PieceType]Tag{
	shogi.Knight:     {Code: "fundoshi_no_kei", NameJa: "ふんどしの桂", Kind: KindTesuji},
	shogi.Silver:     {Code: "wariuchi_no_gin", NameJa: "割打ちの銀", Kind: KindTesuji},
	shogi.Rook:       {Code: "juji_bisha", NameJa: "十字飛車", Kind: KindTesuji},
	shogi.PromRook:   {Code: "juji_bisha", NameJa: "十字飛車", Kind: KindTesuji},
	shogi.Bishop:     {Code: "kaku_ryodori", NameJa: "角による両取り", Kind: KindTesuji},
	shogi.PromBishop: {Code: "kaku_ryodori", NameJa: "角による両取り", Kind: KindTesuji},
}

// targetSquares 는 그 駒가 딸 수 있는 상대 駒가 **선 칸들**이다 — 수를 세면 成·不成이
// 둘로 샌다. 歩·玉은 이름의 관례로 뺀다(06-status.md §45).
// **手番을 c 로 안 맞추면** `LegalMoves` 가 상대 수만 내서 조용히 빈 결과를 준다.
func targetSquares(pos shogi.Position, sq int, c shogi.Color) []int {
	pos.Turn = c

	seen := map[int]bool{}
	var targets []int
	for _, m := range pos.LegalMoves() {
		if m.IsDrop() || int(m.From) != sq || seen[int(m.To)] {
			continue
		}
		if cap := pos.Board[m.To]; !cap.Empty() && cap.Type() != shogi.Pawn && cap.Type() != shogi.King {
			seen[int(m.To)] = true
			targets = append(targets, int(m.To))
		}
	}
	return targets
}

// forkShape 는 **그 이름이 말하는 형태인가**를 본다 — 이름이 방향을 말하면 그 방향을
// 요구한다: 「十字」는 縦과 横의 교차, 「角」은 斜め, 「割打ち」는 출처가 정한 **뒤쪽 두
// 대각**이다. 桂만 방향을 안 말한다 — 利き가 두 칸뿐이라 낄 방향이 하나다. 그래서 桂는
// 「둘」이면 된다. 이 조건이 없을 때 무엇이 잘못 떴는지는 06-status.md §34 ⑤ · §45.
func forkShape(pt shogi.PieceType, sq int, c shogi.Color, targets []int) bool {
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

	case shogi.Silver:
		// 뒤쪽 두 대각(先手면 段이 커지는 쪽의 좌우 한 칸)에 각각 표적이 서야 한다.
		backRank := rank - forwardStep(c)
		var left, right bool
		for _, to := range targets {
			if shogi.RankOf(to) != backRank {
				continue
			}
			switch shogi.FileOf(to) - file {
			case -1:
				left = true
			case 1:
				right = true
			}
		}
		return left && right

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

// Fork 는 sq 의 駒가 両取り의 **형태**를 이루는지만 본다 — 「그래서 得인가」는 안 묻는다.
// 그래서 이 결과를 그대로 화면에 실으면 안 되고, 엔진과의 AND는 `game/tesuji.go` 가 건다.
func Fork(pos shogi.Position, sq int, c shogi.Color) (Tag, bool) {
	p := pos.Board[sq]
	if p.Empty() || p.Color() != c {
		return Tag{}, false
	}
	name, named := forkNames[p.Type()]
	if !named {
		return Tag{}, false
	}
	if !forkShape(p.Type(), sq, c, targetSquares(pos, sq, c)) {
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
