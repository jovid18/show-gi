package tag

import "github.com/jovid18/show-gi/apps/server/internal/shogi"

// 打つ 手筋 — **어떻게 놓였는가가 곧 이름이다.**
//
// 다른 手筋과 입력이 갈리는 자리다. 囲い는 배치에서, 両取り는 관계에서 나오는데 이쪽은
// **판만 봐서는 알 수 없다** — 4四의 歩가 打った 것인지 4五에서 걸어온 것인지가 판에
// 남지 않고, 그 차이가 곧 이름이다. 걸어온 歩는 그냥 歩이고 打った 歩라야 垂れ歩다.
//
// 그래서 시그니처가 다르고(`FindTesuji` 가 아니라 방금 둔 수를 받는다), 그 때문에
// 이 둘이 [09-tags.md §3](../../../../docs/09-tags.md)에서 미뤄져 있었다.
//
// **이득은 여기서도 안 묻는다.** 두 手筋 다 歩를 던지는 것이 내용이라 「잡히지 않는가」로
// 물으면 정의상 전부 탈락한다 — 손으로 쓴 안전 판정이 이 부류를 열지 못했던 이유이고,
// 그것을 엔진에게 넘긴 것이 이 둘이 들어올 수 있게 된 이유다(`game/tesuji.go`).
var (
	tatakiNoFu = Tag{Code: "tataki_no_fu", NameJa: "叩きの歩", Kind: KindTesuji}
	tareFu     = Tag{Code: "tare_fu", NameJa: "垂れ歩", Kind: KindTesuji}
)

// enemyCampEdge 는 그 색에게 **적진의 첫 段**이다 (先手 3段 · 後手 7段).
func enemyCampEdge(c shogi.Color) int {
	if c == shogi.Black {
		return 3
	}
	return 7
}

// DropTesuji 는 **방금 둔 打**이 만든 手筋의 이름을 낸다. pos 는 그 수를 둔 **뒤**의 국면이다.
//
//	叩きの歩  金·銀의 **머리**에 打つ. 받게 만들어 형태를 흩뜨린다
//	垂れ歩    적진 **한 칸 앞**에 打つ. 다음에 成って と金을 만드는 것이 노림이다
//
// 둘은 같은 歩打이고 **앞 칸에 무엇이 있느냐**로만 갈린다. 그래서 한 함수에 둔다 —
// 나누면 「앞 칸」을 두 곳에서 읽게 되고, 그 둘이 어긋나면 한 打에 두 이름이 붙는다.
//
// **叩き의 대상을 金·銀으로 좁힌다.** 「駒の頭」로 넓히면 玉頭·飛頭까지 걸리는데 그쪽에는
// 각자 다른 이름이 있고, 이름은 화면에 그대로 나가는 단언이라 넓히려면 근거가 있어야
// 한다. 歩의 머리도 뺀다 — 그것은 合わせの歩라는 다른 手筋이다.
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
