package tag

import "github.com/jovid18/show-gi/apps/server/internal/shogi"

// 打つ 手筋 — 어떻게 놓였는가가 곧 이름이다. 다른 手筋과 입력이 갈린다: 4四의 歩가
// 打った 것인지 4五에서 걸어온 것인지가 판에 안 남는데 그 차이가 곧 이름이다. 그래서
// FindTesuji 가 아니라 방금 둔 수를 받는다. 이득을 안 묻는 것은 다른 手筋과 같다
// ([09-tags.md §5](../../../../docs/09-tags.md)).
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

// DropTesuji 는 방금 둔 打이 만든 手筋의 이름을 낸다. pos 는 그 수를 둔 뒤의 국면이다.
//
//	叩きの歩  金·銀의 머리에 打つ. 받게 만들어 형태를 흩뜨린다
//	垂れ歩    적진 한 칸 앞에 打つ. 다음에 成って と金을 만드는 것이 노림이다
//
// 둘은 같은 歩打이고 앞 칸에 무엇이 있느냐로만 갈린다. 그래서 한 함수에 둔다 —
// 나누면 「앞 칸」을 두 곳에서 읽게 되고, 그 둘이 어긋나면 한 打에 두 이름이 붙는다.
//
// 叩き의 대상을 金·銀으로 좁힌다. 「駒の頭」로 넓히면 玉頭·飛頭까지 걸리는데 그쪽에는
// 각자 다른 이름이 있고, 이름은 화면에 그대로 나가는 단언이라 넓히려면 근거가 있어야
// 한다. 歩의 머리도 뺀다 — 그것은 合わせの歩라는 다른 手筋이다.
//
// と金·成銀의 머리는 지금 안 센다. 움직임이 金과 같으니 같은 手筋이 성립할 텐데,
// 그것은 「이 이름이 무엇을 말하는가」가 아니라 넓혀도 되는가의 문제라 근거가 따로
// 필요하다. 좁게 두면 안 뜰 뿐이고, 넓게 두면 틀린 이름이 뜬다.
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
		// 적진 바로 앞이라야 「다음 수로 成る」가 노림이 된다. 그보다 뒤면 그냥 歩打이고,
		// 적진 안이면 이미 成れる 자리라 「垂らす」가 아니다.
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
