package server

// 되짚기 화면(review.go)과 가정 수순(branch.go)이 같이 쓰는 판 헬퍼들.
// 전부 기록에서 다시 둔다 — 클라이언트가 보낸 국면을 믿지 않는다.

import "github.com/jovid18/show-gi/apps/server/internal/shogi"

// startSFENOf 는 기록된 시작 국면이다. 비어 있으면 평수 초기 국면이다 —
// 세션은 기본 국면도 문자열로 적지만(session.go), 002 이전에 열린 판에는 칸이 비어 있다.
func startSFENOf(recorded string) string {
	if recorded == "" {
		return shogi.StartSFEN
	}
	return recorded
}

// advance 는 한 수를 두어 본다. 읽을 수 없거나 합법이 아니면 ok=false.
//
// 여기서 판정을 새로 하지 않는다. 기록에 남은 수는 이미 그때 룰 엔진을 통과한
// 것이고, 이 검사는 기록이 깨졌는지를 보는 것이다.
func advance(pos shogi.Position, prevTo int, usi string) (shogi.Position, string, bool) {
	m, err := shogi.ParseUSIMove(usi)
	if err != nil {
		return pos, "", false
	}
	if err := pos.ValidateMove(m); err != nil {
		return pos, "", false
	}
	return pos.Apply(m), pos.MoveJa(m, prevTo), true
}

// checkedSquare 는 王手를 받고 있는 玉의 칸이다(5a). 王手가 아니면 빈 값.
//
// 화면이 스스로 안 구한다. 王手인지는 규칙을 알아야 알고, 규칙은 서버만 갖는다.
// 玉이 없는 국면(기록된 SFEN이 그럴 수 있다)에서 -1이 오고, 그때는 안 짚는다.
func checkedSquare(pos shogi.Position) string {
	if !pos.InCheck(pos.Turn) {
		return ""
	}
	sq := pos.KingSquare(pos.Turn)
	if sq < 0 {
		return ""
	}
	return shogi.SquareUSI(sq)
}

// lastTo 는 그 수의 도착 칸이다. 「同」 표기가 이 값을 본다.
func lastTo(usi string) int {
	m, err := shogi.ParseUSIMove(usi)
	if err != nil {
		return -1
	}
	return int(m.To)
}
