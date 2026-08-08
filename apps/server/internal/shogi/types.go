// Package shogi 는 쇼기 룰 엔진이다: 국면 표현, SFEN/USI 표기, 합법수 생성, 반칙 검증.
//
// 이 패키지는 DB도 엔진도 모른다. 순수 함수 덩어리이고, 그래서 테스트가 싸다.
// 블런더 판정의 바닥에 깔리는 층이라 여기서 틀리면 위에서 전부 틀린다.
//
// 좌표계: sq = row*9 + col (0..80).
//   - row 0..8 = 段(rank) 1..9, 위(후수 진영)에서 아래(선수 진영)로.
//   - col 0..8 = 9筋(왼쪽)에서 1筋(오른쪽)으로. 즉 file = 9 - col.
//
// 선수(Black, 先手)는 위쪽(row 감소)으로 전진한다.
package shogi

import "fmt"

type Color int8

const (
	Black Color = 0 // 先手 (sente)
	White Color = 1 // 後手 (gote)
)

func (c Color) Other() Color { return 1 - c }

func (c Color) String() string {
	if c == Black {
		return "sente"
	}
	return "gote"
}

type PieceType int8

const (
	NoPieceType PieceType = iota
	Pawn                  // 歩
	Lance                 // 香
	Knight                // 桂
	Silver                // 銀
	Gold                  // 金
	Bishop                // 角
	Rook                  // 飛
	King                  // 王
	PromPawn              // と
	PromLance             // 成香
	PromKnight            // 成桂
	PromSilver            // 成銀
	PromBishop            // 馬
	PromRook              // 龍
)

// CanPromote 는 이 말이 승격 가능한 종류인지 (이미 승격했거나 金/王이면 false).
func (t PieceType) CanPromote() bool {
	switch t {
	case Pawn, Lance, Knight, Silver, Bishop, Rook:
		return true
	}
	return false
}

func (t PieceType) Promoted() PieceType {
	switch t {
	case Pawn:
		return PromPawn
	case Lance:
		return PromLance
	case Knight:
		return PromKnight
	case Silver:
		return PromSilver
	case Bishop:
		return PromBishop
	case Rook:
		return PromRook
	}
	return t
}

// Base 는 승격을 벗긴 원래 말 종류 (잡혔을 때 持ち駒로 돌아가는 형태).
func (t PieceType) Base() PieceType {
	switch t {
	case PromPawn:
		return Pawn
	case PromLance:
		return Lance
	case PromKnight:
		return Knight
	case PromSilver:
		return Silver
	case PromBishop:
		return Bishop
	case PromRook:
		return Rook
	}
	return t
}

var typeLetters = map[PieceType]byte{
	Pawn: 'P', Lance: 'L', Knight: 'N', Silver: 'S',
	Gold: 'G', Bishop: 'B', Rook: 'R', King: 'K',
}

var letterTypes = map[byte]PieceType{
	'P': Pawn, 'L': Lance, 'N': Knight, 'S': Silver,
	'G': Gold, 'B': Bishop, 'R': Rook, 'K': King,
}

// Piece: 0 = 빈 칸, 양수 = 선수 말, 음수 = 후수 말 (절대값 = PieceType).
type Piece int8

func MakePiece(t PieceType, c Color) Piece {
	if c == Black {
		return Piece(t)
	}
	return Piece(-int8(t))
}

func (p Piece) Empty() bool { return p == 0 }

func (p Piece) Type() PieceType {
	if p < 0 {
		return PieceType(-p)
	}
	return PieceType(p)
}

func (p Piece) Color() Color {
	if p < 0 {
		return White
	}
	return Black
}

// SquareOf: 표기 좌표(筋 file 1..9, 段 rank 1..9) → 내부 인덱스.
func SquareOf(file, rank int) int { return (rank-1)*9 + (9 - file) }

func FileOf(sq int) int { return 9 - sq%9 }
func RankOf(sq int) int { return sq/9 + 1 }

// Move: From == -1 이면 持ち駒 투입(Drop에 말 종류).
type Move struct {
	From    int8
	To      int8
	Drop    PieceType
	Promote bool
}

func (m Move) IsDrop() bool { return m.From < 0 }

func sqUSI(sq int8) string {
	return fmt.Sprintf("%d%c", FileOf(int(sq)), byte('a'+RankOf(int(sq))-1))
}

// USI 는 표준 USI 수 표기를 돌려준다: 7g7f, 8h2b+, P*5e.
func (m Move) USI() string {
	if m.IsDrop() {
		return fmt.Sprintf("%c*%s", typeLetters[m.Drop], sqUSI(m.To))
	}
	s := sqUSI(m.From) + sqUSI(m.To)
	if m.Promote {
		s += "+"
	}
	return s
}

func parseUSISquare(file, rank byte) (int8, error) {
	if file < '1' || file > '9' || rank < 'a' || rank > 'i' {
		return 0, fmt.Errorf("invalid square %c%c", file, rank)
	}
	return int8(SquareOf(int(file-'0'), int(rank-'a'+1))), nil
}

func ParseUSIMove(s string) (Move, error) {
	if len(s) == 4 && s[1] == '*' {
		t, ok := letterTypes[s[0]]
		if !ok || t == King {
			return Move{}, fmt.Errorf("invalid drop piece %q", s)
		}
		to, err := parseUSISquare(s[2], s[3])
		if err != nil {
			return Move{}, err
		}
		return Move{From: -1, To: to, Drop: t}, nil
	}
	if len(s) != 4 && !(len(s) == 5 && s[4] == '+') {
		return Move{}, fmt.Errorf("invalid USI move %q", s)
	}
	from, err := parseUSISquare(s[0], s[1])
	if err != nil {
		return Move{}, err
	}
	to, err := parseUSISquare(s[2], s[3])
	if err != nil {
		return Move{}, err
	}
	return Move{From: from, To: to, Promote: len(s) == 5}, nil
}
