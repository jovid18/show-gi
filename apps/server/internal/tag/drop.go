package tag

import "github.com/jovid18/show-gi/apps/server/internal/shogi"

// 打つ 手筋 — 어떻게 놓였는가가 곧 이름이다. 4四의 歩가 打った 것인지 4五에서 걸어온
// 것인지는 판에 남지 않아서, FindTesuji 대신 방금 둔 수를 받는다. 이득을 묻지 않는
// 것은 다른 手筋과 같다(09-tags.md §5).
var (
	tatakiNoFu = Tag{Code: "tataki_no_fu", NameJa: "叩きの歩", Kind: KindTesuji}
	tareFu     = Tag{Code: "tare_fu", NameJa: "垂れ歩", Kind: KindTesuji}
)

// enemyCampEdge 는 그 색에게 적진의 첫 段이다 (先手 3段 · 後手 7段).
func enemyCampEdge(c shogi.Color) int {
	if c == shogi.Black {
		return 3
	}
	return 7
}

// DropTesuji 는 방금 둔 打이 만든 手筋의 이름을 짚는다. pos 는 그 수를 둔 뒤의 국면이다.
//
//	叩きの歩  金·銀의 머리에 打つ. 받게 만들어 형태를 흩뜨린다
//	垂れ歩    적진 한 칸 앞에 打つ. 다음에 成って と金을 만드는 것이 노림이다
//
// 둘은 같은 歩打이고 앞 칸에 무엇이 있느냐로만 갈려서 한 함수에 둔다. 나누면 「앞 칸」을
// 두 곳에서 읽게 되고, 그 둘이 어긋나면 한 打에 두 이름이 붙는다.
//
// 叩き의 대상은 金·銀뿐이다. 「駒の頭」로 넓히면 玉頭·飛頭까지 걸리는데 그쪽에는 각자
// 다른 이름이 있다. 歩의 머리는 合わせの歩라는 다른 手筋이고, と金·成銀의 머리는
// 넓혀도 되는가의 근거가 따로 필요해서 아직 세지 않는다.
func DropTesuji(pos shogi.Position, last shogi.Move, c shogi.Color) []Tag {
	if !last.IsDrop() {
		return nil
	}
	sq := int(last.To)
	if p := pos.Board[sq]; p.Empty() || p.Color() != c || p.Type() != shogi.Pawn {
		return nil
	}

	file, rank := shogi.FileOf(sq), shogi.RankOf(sq)+forwardStep(c)
	if !onBoard(file, rank) {
		return nil
	}
	front := pos.Board[shogi.SquareOf(file, rank)]

	switch {
	case front.Empty():
		// 적진 바로 앞이라야 「다음 수로 成る」가 노림이 된다. 적진 안이면 이미 成れる
		// 자리라 「垂らす」라고 부르지 않는다.
		if rank == enemyCampEdge(c) {
			return []Tag{tareFu}
		}
	case front.Color() != c:
		if t := front.Type(); t == shogi.Gold || t == shogi.Silver {
			return []Tag{tatakiNoFu}
		}
	}
	return nil
}
