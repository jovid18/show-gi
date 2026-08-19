package shogi

import (
	"fmt"
	"slices"
)

// Reason 은 수가 불법인 사유다.
//
// 문구를 에러에 담지 않는다 — 룰 엔진이 표현까지 들고 있으면 판정과 문구가 한 덩어리가 되어
// 화면 언어(일본어)를 바꿀 수 없다. 판정은 코드로 내고, 문구는 Message() 한 곳에서만 만든다.
type Reason int

const (
	ReasonUnknown Reason = iota
	ReasonOffBoard
	ReasonNotDroppable
	ReasonNoPieceInHand
	ReasonSquareOccupied
	ReasonNifu          // 二歩
	ReasonDeadPieceDrop // 行き所のない駒 (투입)
	ReasonMustPromote   // 行き所のない駒 (이동 — 승격 강제)
	ReasonUchifuzume    // 打ち歩詰め
	ReasonEmptySquare
	ReasonNotYourPiece
	ReasonUnreachable
	ReasonOwnPieceAtDest
	ReasonCannotPromote
	ReasonOutsidePromoZone
	ReasonMustResolveCheck // 王手放置 — 이미 王手를 받고 있다
	ReasonLeavesKingInCheck
)

// reasonNames 는 로그·에러 문자열용이다. 사람이 보는 화면에는 쓰지 않는다.
var reasonNames = map[Reason]string{
	ReasonUnknown:           "illegal",
	ReasonOffBoard:          "off board",
	ReasonNotDroppable:      "not droppable",
	ReasonNoPieceInHand:     "no piece in hand",
	ReasonSquareOccupied:    "square occupied",
	ReasonNifu:              "nifu",
	ReasonDeadPieceDrop:     "dead piece drop",
	ReasonMustPromote:       "must promote",
	ReasonUchifuzume:        "uchifuzume",
	ReasonEmptySquare:       "empty square",
	ReasonNotYourPiece:      "not your piece",
	ReasonUnreachable:       "unreachable square",
	ReasonOwnPieceAtDest:    "own piece at destination",
	ReasonCannotPromote:     "cannot promote",
	ReasonOutsidePromoZone:  "outside promotion zone",
	ReasonMustResolveCheck:  "must resolve check",
	ReasonLeavesKingInCheck: "leaves king in check",
}

// reasonMessages 는 화면에 나갈 수 있는 문구라 일본어다.
//
// 정상 경로에서는 쓰이지 않는다 — 클라이언트가 서버에서 받은 합법수만 고르므로, 여기까지 오는 것은
// 국면이 어긋났다는 뜻(우리 버그)이다. 그래도 반칙 이름만 던지면 "二歩ってなに" 에서 막히므로
// 무엇이 문제인지까지 적는다.
var reasonMessages = map[Reason]string{
	ReasonUnknown:           "指すことのできない手です。",
	ReasonOffBoard:          "盤の外のマスです。",
	ReasonNotDroppable:      "その駒は打つことができません。",
	ReasonNoPieceInHand:     "その駒は持ち駒にありません。",
	ReasonSquareOccupied:    "駒は空いているマスにしか打てません。",
	ReasonNifu:              "二歩です。同じ筋にすでに自分の歩がいます。",
	ReasonDeadPieceDrop:     "行き所のない駒です。そこに打つと二度と動かせません。",
	ReasonMustPromote:       "行き所のない駒になります。この手は必ず成らなければなりません。",
	ReasonUchifuzume:        "打ち歩詰めです。歩を打って詰ますことはできません。",
	ReasonEmptySquare:       "そのマスに駒がありません。",
	ReasonNotYourPiece:      "相手の駒は動かせません。",
	ReasonUnreachable:       "その駒はそのマスへ動くことができません。",
	ReasonOwnPieceAtDest:    "自分の駒がいるマスへは動けません。",
	ReasonCannotPromote:     "その駒は成ることができません。",
	ReasonOutsidePromoZone:  "成れるのは、敵陣（相手側の三段）に入るときか、敵陣から出るときだけです。",
	ReasonMustResolveCheck:  "王手がかかっています。まず王手を解消してください。",
	ReasonLeavesKingInCheck: "その手を指すと自分の玉が取られてしまいます。",
}

// String 은 사유의 영어 이름이다. 로그와 API 응답의 코드로 쓴다.
// 화면에 보일 문구는 IllegalMoveError.Message() 쪽이다.
func (r Reason) String() string {
	if name, ok := reasonNames[r]; ok {
		return name
	}
	return reasonNames[ReasonUnknown]
}

// IllegalMoveError 는 불법수와 그 사유다.
type IllegalMoveError struct {
	Reason Reason
	Move   Move
}

// Error 는 로그용이다 — 영어. 이 문자열을 화면에 그대로 내보내지 않는다.
func (e *IllegalMoveError) Error() string {
	name, ok := reasonNames[e.Reason]
	if !ok {
		name = reasonNames[ReasonUnknown]
	}
	return fmt.Sprintf("illegal move %s: %s", e.Move.USI(), name)
}

// Message 는 사용자에게 보여줄 일본어 문구다.
func (e *IllegalMoveError) Message() string {
	if msg, ok := reasonMessages[e.Reason]; ok {
		return msg
	}
	return reasonMessages[ReasonUnknown]
}

func illegal(m Move, r Reason) error {
	return &IllegalMoveError{Reason: r, Move: m}
}

// 표준 쇼기 한 벌의 말 수.
var pieceComplement = map[PieceType]int{
	Pawn: 18, Lance: 4, Knight: 4, Silver: 4, Gold: 4, Bishop: 2, Rook: 2, King: 2,
}

// InventoryExcess 는 한 벌을 넘어선 말 종류와 그 초과분을 돌려준다(비면 정상).
//
// 밖에서 들어온 SFEN을 그대로 엔진에 넘기지 않기 위한 검사다 — 말이 넘치는 판에 엔진이
// 무엇을 돌려줄지는 정의되어 있지 않다.
// "부족"은 검사하지 않는다 — 詰将棋처럼 말이 빠진 국면이 정상인 경우가 있다.
func (pos Position) InventoryExcess() map[PieceType]int {
	count := map[PieceType]int{}
	for _, p := range pos.Board {
		if p.Empty() {
			continue
		}
		count[p.Type().Base()]++
	}
	for c := range 2 {
		for t := Pawn; t <= Rook; t++ {
			count[t] += int(pos.Hands[c][t])
		}
	}
	excess := map[PieceType]int{}
	for t, n := range count {
		if lim, ok := pieceComplement[t]; ok && n > lim {
			excess[t] = n - lim
		}
	}
	return excess
}

// ValidateMove 는 수의 합법성을 검사하고, 불법이면 사유를 담은 *IllegalMoveError 를 돌려준다.
//
// 합법 여부의 진실은 LegalMoves 하나뿐이다. 아래의 긴 분기는 판정이 아니라 진단 —
// "왜 안 되는지"를 초심자에게 말해주기 위한 것이고, 판정 결과를 바꾸지 않는다.
func (pos Position) ValidateMove(m Move) error {
	me := pos.Turn

	if slices.Contains(pos.LegalMoves(), m) {
		return nil
	}

	// 이하는 진단: 왜 불법인지 이유를 찾는다.
	if m.To < 0 || m.To > 80 {
		return illegal(m, ReasonOffBoard)
	}

	if m.IsDrop() {
		if m.Drop < Pawn || m.Drop > Rook {
			return illegal(m, ReasonNotDroppable)
		}
		if pos.Hands[me][m.Drop] == 0 {
			return illegal(m, ReasonNoPieceInHand)
		}
		if !pos.Board[m.To].Empty() {
			return illegal(m, ReasonSquareOccupied)
		}
		if m.Drop == Pawn && pos.nifu(int(m.To)%9, me) {
			return illegal(m, ReasonNifu)
		}
		if mustPromoteAt(m.Drop, int(m.To), me) {
			return illegal(m, ReasonDeadPieceDrop)
		}
		np := pos.Apply(m)
		if np.InCheck(me) {
			return illegal(m, ReasonLeavesKingInCheck)
		}
		if m.Drop == Pawn && np.InCheck(me.Other()) && len(np.legalMoves(false)) == 0 {
			return illegal(m, ReasonUchifuzume)
		}
		return illegal(m, ReasonUnknown)
	}

	if m.From < 0 || m.From > 80 {
		return illegal(m, ReasonOffBoard)
	}
	p := pos.Board[m.From]
	if p.Empty() {
		return illegal(m, ReasonEmptySquare)
	}
	if p.Color() != me {
		return illegal(m, ReasonNotYourPiece)
	}

	reachable := false
	pos.attackTargets(int(m.From), func(to int) bool {
		if to == int(m.To) {
			reachable = true
			return false
		}
		return true
	})
	if !reachable {
		return illegal(m, ReasonUnreachable)
	}
	if !pos.Board[m.To].Empty() && pos.Board[m.To].Color() == me {
		return illegal(m, ReasonOwnPieceAtDest)
	}
	t := p.Type()
	if m.Promote {
		if !t.CanPromote() {
			return illegal(m, ReasonCannotPromote)
		}
		if !inPromoZone(int(m.From), me) && !inPromoZone(int(m.To), me) {
			return illegal(m, ReasonOutsidePromoZone)
		}
	} else if mustPromoteAt(t, int(m.To), me) {
		return illegal(m, ReasonMustPromote)
	}

	np := pos.Apply(m)
	if np.InCheck(me) {
		if pos.InCheck(me) {
			return illegal(m, ReasonMustResolveCheck)
		}
		return illegal(m, ReasonLeavesKingInCheck)
	}

	return illegal(m, ReasonUnknown)
}
