package shogi

// 국면 하나가 그 자체로 성립하는가. 수의 합법성(ValidateMove)과 다른 물음이다.
//
// 이 파일이 있는 이유는 밖에서 들어온 국면 때문이다. 다른 표면은 뿌리를 서버가 만들고
// 수순을 한 수씩 ValidateMove 로 지나가므로 「재생이 곧 보증인」인데, 사진에서 읽어 온
// 국면은 재생할 수순이 없다 — 그 보증인을 여기가 대신한다(journal §129).
//
// 잡는 것은 룰이 금지하는 모양뿐이다. 銀을 成銀으로 잘못 읽은 판은 여전히 합법적인
// 국면이라 여기서 안 걸린다 — 그쪽의 검증자는 사람이고, 그래서 확인 화면이 있다.

import "fmt"

// PositionReason 은 국면이 성립하지 않는 사유다.
//
// Reason(수의 사유)과 갈라 둔다. 한 벌로 묶으면 「二歩」가 두 뜻을 갖는다 — 저쪽은
// 「그 수를 두면 二歩가 된다」이고 여기는 「이미 二歩인 판이다」다.
type PositionReason int

const (
	PositionUnknown PositionReason = iota
	// PositionPieceExcess: 한 벌을 넘은 말이 있다.
	PositionPieceExcess
	// PositionKingCount: 한쪽의 玉이 하나가 아니다.
	PositionKingCount
	// PositionNifu: 같은 筋에 성하지 않은 자기 歩가 둘 이상이다.
	PositionNifu
	// PositionDeadPiece: 두 번 다시 움직일 수 없는 자리에 말이 있다(行き所のない駒).
	PositionDeadPiece
	// PositionCheckIgnored: 수번이 아닌 쪽이 王手를 받고 있다.
	PositionCheckIgnored
)

var positionReasonNames = map[PositionReason]string{
	PositionUnknown:      "illegal position",
	PositionPieceExcess:  "piece excess",
	PositionKingCount:    "king count",
	PositionNifu:         "nifu",
	PositionDeadPiece:    "dead piece",
	PositionCheckIgnored: "check ignored",
}

func (r PositionReason) String() string {
	if name, ok := positionReasonNames[r]; ok {
		return name
	}
	return positionReasonNames[PositionUnknown]
}

// PositionFault 는 어긴 규칙 하나와 그것이 어디인가다.
//
// 칸을 든다. 확인 화면이 그 칸에 표시를 하므로, 사유만 주면 사람이 81칸에서 二歩를
// 눈으로 찾아야 한다.
type PositionFault struct {
	Reason PositionReason
	// Color 는 어긴 쪽이다.
	Color Color
	// Square 는 문제가 된 칸이다. 칸으로 짚을 수 없는 사유(말 수·玉 수)면 -1.
	Square int
	// Type 은 문제가 된 말 종류다. 없으면 NoPieceType.
	Type PieceType
	// Count 는 사유가 세는 값이다 — 말 수의 초과분, 玉의 개수. 나머지는 0.
	Count int
}

// Error 는 로그용이다 — 영어. 화면에는 Message 쪽이 나간다.
func (f PositionFault) Error() string {
	s := fmt.Sprintf("%s: %s", f.Color, f.Reason)
	if f.Type != NoPieceType {
		s += " " + string(typeLetters[f.Type.Base()])
	}
	if f.Square >= 0 {
		s += " at " + SquareJa(f.Square)
	}
	if f.Count != 0 {
		s += fmt.Sprintf(" (%d)", f.Count)
	}
	return s
}

// Message 는 사용자에게 보여줄 일본어 문구다.
//
// 「누구의」를 안 적는다. 이 문구가 나가는 자리가 사진에서 읽어 온 판을 확인하는 화면
// 하나뿐이고, 거기서는 아래쪽이 언제나 자기 편이라 화면이 칸을 짚어 보여 준다.
func (f PositionFault) Message() string {
	switch f.Reason {
	case PositionPieceExcess:
		return fmt.Sprintf("%sが%d枚多いようです。読み取りを直してください。", PieceJa(f.Type), f.Count)
	case PositionKingCount:
		if f.Count == 0 {
			return "玉が見つかりません。どちらにも玉が一枚ずつ必要です。"
		}
		return fmt.Sprintf("玉が%d枚あります。どちらにも一枚ずつです。", f.Count)
	case PositionNifu:
		return fmt.Sprintf("%sの筋に歩が二枚あります（二歩）。", SquareJa(f.Square))
	case PositionDeadPiece:
		return fmt.Sprintf("%sの%sは、そこから動かすことができません。", SquareJa(f.Square), PieceJa(f.Type))
	case PositionCheckIgnored:
		// 手番을 잘못 고른 자리가 여기로 온다. 사진은 手番을 말해 주지 않으므로
		// 사람이 고르는 값이고, 王手를 받고 있는 쪽이 곧 手番이다.
		return "王手がかかっている側の手番のはずです。手番の選択を確かめてください。"
	}
	return "この局面は成り立ちません。"
}

// Faults 는 이 국면이 어긴 규칙 전부다. 비면 성립하는 국면이다.
//
// 하나에서 멈추지 않는다. 잘못 읽은 사진은 여러 자리가 함께 틀리고, 한 번에 하나만
// 말하면 사람이 고치고 다시 누르기를 사유 수만큼 반복한다.
//
// **말이 부족한 것은 여기서 안 본다.** 詰将棋처럼 말이 빠진 국면이 정상인 경우가 있어
// InventoryExcess 가 이미 그렇게 갈라 두었고, 사진에서 온 판의 「39枚」는 거절이 아니라
// 경고로 화면에 나간다(InventoryShortage).
func (pos Position) Faults() []PositionFault {
	var out []PositionFault

	// 한 벌을 넘은 말. 넘치는 판에 엔진이 무엇을 돌려줄지는 정의되어 있지 않다
	// (InventoryExcess). 말 종류 순으로 돈다 — 맵을 그대로 훑으면 같은 판이 요청마다
	// 다른 순서의 목록을 준다.
	excess := pos.InventoryExcess()
	for t := Pawn; t <= King; t++ {
		if n, ok := excess[t]; ok {
			out = append(out, PositionFault{
				Reason: PositionPieceExcess, Square: -1, Type: t, Count: n,
			})
		}
	}

	for c := range 2 {
		color := Color(c)

		// 玉은 양쪽에 하나씩이다. 없으면 InCheck 이 언제나 거짓이 되어 아래 王手
		// 검사가 조용히 통과하고, 둘이면 엔진 쪽이 정의되어 있지 않다.
		if n := kingCount(pos, color); n != 1 {
			out = append(out, PositionFault{
				Reason: PositionKingCount, Color: color, Square: -1, Type: King, Count: n,
			})
		}

		out = append(out, nifuFaults(pos, color)...)
		out = append(out, deadPieceFaults(pos, color)...)
	}

	// 수번이 아닌 쪽이 王手를 받고 있으면 그 쪽이 직전에 자기 玉을 잡히게 두고
	// 넘긴 판이다. 玉 수가 이미 틀렸으면 안 묻는다 — KingSquare 가 -1을 주어
	// 언제나 거짓이고, 그 거짓이 「王手가 없다」로 읽힌다.
	if kingCount(pos, pos.Turn.Other()) == 1 && pos.InCheck(pos.Turn.Other()) {
		out = append(out, PositionFault{
			Reason: PositionCheckIgnored, Color: pos.Turn.Other(), Square: pos.KingSquare(pos.Turn.Other()),
		})
	}

	return out
}

func kingCount(pos Position, c Color) int {
	n := 0
	for _, p := range pos.Board {
		if !p.Empty() && p.Color() == c && p.Type() == King {
			n++
		}
	}
	return n
}

// nifuFaults 는 같은 筋의 성하지 않은 歩를 둘째부터 짚는다. と金은 二歩가 아니다.
func nifuFaults(pos Position, c Color) []PositionFault {
	var out []PositionFault
	for col := range 9 {
		seen := 0
		for row := range 9 {
			sq := row*9 + col
			p := pos.Board[sq]
			if p.Empty() || p.Color() != c || p.Type() != Pawn {
				continue
			}
			seen++
			// 첫 장은 정상이다. 둘째부터가 二歩이고, 짚는 칸이 그 둘째 장이라
			// 화면이 「지울 후보」를 가리킨다.
			if seen >= 2 {
				out = append(out, PositionFault{
					Reason: PositionNifu, Color: c, Square: sq, Type: Pawn,
				})
			}
		}
	}
	return out
}

// deadPieceFaults 는 두 번 다시 움직일 수 없는 자리의 말을 짚는다(行き所のない駒).
//
// 승격을 안 한 채로는 갈 수 없는 자리다. 성한 말은 뒤로도 가므로 여기 안 걸린다.
func deadPieceFaults(pos Position, c Color) []PositionFault {
	var out []PositionFault
	for sq, p := range pos.Board {
		if p.Empty() || p.Color() != c {
			continue
		}
		// 앞으로 남은 段 수. 先手는 row 가 줄어드는 쪽이 앞이다(패키지 doc).
		ahead := sq / 9
		if c == White {
			ahead = 8 - ahead
		}
		var dead bool
		switch p.Type() {
		case Pawn, Lance:
			dead = ahead == 0
		case Knight:
			dead = ahead <= 1
		}
		if dead {
			out = append(out, PositionFault{
				Reason: PositionDeadPiece, Color: c, Square: sq, Type: p.Type(),
			})
		}
	}
	return out
}

// InventoryShortage 는 한 벌에서 빠진 말 종류와 그 수다(비면 40장이 다 있다).
//
// 거절 사유가 아니다. 사진에서 읽어 온 판에서는 이것이 곧 「한 장을 놓쳤다」의 신호이지만
// (실물 한 판은 언제나 40장이다) 駒台가 잘려 나간 사진도 정상이라, 화면이 경고로만 쓴다.
func (pos Position) InventoryShortage() map[PieceType]int {
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
	short := map[PieceType]int{}
	for t, lim := range pieceComplement {
		if n := count[t]; n < lim {
			short[t] = lim - n
		}
	}
	return short
}
