package shogi

// PieceValue 는 駒交換의 손익을 견주기 위한 눈금이다(歩=1).
//
// 평가치가 아니다 — 형세는 엔진이 잰다. cp로 바꾸려 들면 엔진 평가와 두 벌이 되고,
// 어긋났을 때 어느 쪽이 맞는지 알 수 없다.
//
// shogi 에 두는 이유는 표를 한 벌만 두기 위해서다: 개입 판정(game.moveFacts)과 手筋 판정(tag)이
// 둘 다 쓰는데 tag 가 game 을 import하면 순환이 된다.
func PieceValue(t PieceType) int { return pieceValues[t] }

var pieceValues = map[PieceType]int{
	Pawn: 1, Lance: 4, Knight: 4, Silver: 6,
	Gold: 6, Bishop: 8, Rook: 10,
	PromPawn: 6, PromLance: 6, PromKnight: 6, PromSilver: 6,
	PromBishop: 11, PromRook: 13,
	King: 100,
}
