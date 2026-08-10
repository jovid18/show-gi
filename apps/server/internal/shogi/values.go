package shogi

// PieceValue 는 駒交換의 손익을 견주기 위한 값이다. 歩를 1로 잡은 흔한 눈금이다.
//
// **평가치가 아니다.** 형세를 재는 것은 엔진이고 이 값이 하는 일은 「딴 것보다 놓은 것이
// 비싼가」를 묻는 것뿐이다. cp로 바꾸려 들면 엔진의 평가와 두 벌이 되고, 어긋났을 때
// 어느 쪽이 맞는지 알 수 없다.
//
// 玉은 잡히지 않으므로 값이 의미를 갖는 자리가 없다. 인위 국면 방어로만 크게 둔다.
//
// **여기 있는 이유는 표를 한 벌만 두기 위해서다.** 개입 판정(`game.moveFacts`)과 手筋
// 판정(`tag`)이 둘 다 이 값을 쓰는데, `tag` 가 `game` 을 import하면 순환이 된다.
// 두 벌로 두면 한쪽만 고치는 버그가 나고, 그러면 「그 駒가 잡힙니다」라고 개입한 판에서
// 手筋 쪽은 그 駒를 값없다고 세는 일이 생긴다.
func PieceValue(t PieceType) int { return pieceValues[t] }

var pieceValues = map[PieceType]int{
	Pawn: 1, Lance: 4, Knight: 4, Silver: 6,
	Gold: 6, Bishop: 8, Rook: 10,
	PromPawn: 6, PromLance: 6, PromKnight: 6, PromSilver: 6,
	PromBishop: 11, PromRook: 13,
	King: 100,
}
