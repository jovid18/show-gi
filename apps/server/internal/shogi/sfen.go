package shogi

import (
	"fmt"
	"strconv"
	"strings"
)

const StartSFEN = "lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1"

// Position 은 국면 전체: 판, 양측 持ち駒, 수번(手番), 수 번호.
//
// 값 타입이다. Apply가 복사본을 돌려주므로 롤백은 이전 값을 들고 있기만 하면 된다 —
// 되돌리기가 제품 기능인 이상 이 성질이 설계의 핵심이다.
type Position struct {
	Board [81]Piece
	// Hands 는 PieceType 값 그대로 색인한다 — 0-based 오프셋이 아니다. 크기 8은 Rook=7 때문이고
	// index 0(NoPieceType)은 영구 미사용. 王은 잡혀도 持ち駒가 되지 않아 index 8이 없다.
	Hands   [2][8]int8 // [Color][PieceType Pawn..Rook]
	Turn    Color
	MoveNum int
}

func StartPosition() Position {
	p, err := ParseSFEN(StartSFEN)
	if err != nil {
		panic(err) // 상수이므로 불가능
	}
	return p
}

func ParseSFEN(s string) (Position, error) {
	var pos Position
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) < 3 {
		return pos, fmt.Errorf("sfen: need at least 3 fields, got %q", s)
	}

	ranks := strings.Split(fields[0], "/")
	if len(ranks) != 9 {
		return pos, fmt.Errorf("sfen: board must have 9 ranks, got %q", fields[0])
	}
	// SFEN 보드는 段一부터, 각 단 안에서 筋9→筋1 순이다 — 내부 인덱스(row*9+col)와 순서가 그대로
	// 맞아서 좌표 변환이 없다. 뒤집으면 파싱·출력이 함께 틀려 왕복 테스트로는 안 잡힌다.
	for row, rs := range ranks {
		col := 0
		promoted := false
		for i := 0; i < len(rs); i++ {
			ch := rs[i]
			switch {
			case ch >= '1' && ch <= '9':
				if promoted {
					return pos, fmt.Errorf("sfen: digit after '+' in %q", rs)
				}
				col += int(ch - '0')
			case ch == '+':
				promoted = true
			default:
				// SFEN은 대문자=선수, 소문자=후수다. 0x20이 ASCII 대소문자 비트라
				// &^ 로 종류를, >= 'a' 로 색을 본다 (이 파일에 여섯 자리).
				upper := ch &^ 0x20
				t, ok := letterTypes[upper]
				if !ok {
					return pos, fmt.Errorf("sfen: unknown piece %q", string(ch))
				}
				if promoted {
					if !t.CanPromote() {
						return pos, fmt.Errorf("sfen: piece +%c cannot be promoted", upper)
					}
					t = t.Promoted()
					promoted = false
				}
				c := Black
				if ch >= 'a' {
					c = White
				}
				if col > 8 {
					return pos, fmt.Errorf("sfen: rank %d exceeds 9 squares", row+1)
				}
				pos.Board[row*9+col] = MakePiece(t, c)
				col++
			}
		}
		if col != 9 {
			return pos, fmt.Errorf("sfen: rank %d has %d squares, want 9", row+1, col)
		}
	}

	switch fields[1] {
	case "b":
		pos.Turn = Black
	case "w":
		pos.Turn = White
	default:
		return pos, fmt.Errorf("sfen: side to move must be b or w, got %q", fields[1])
	}

	if fields[2] != "-" {
		count := 0
		for i := 0; i < len(fields[2]); i++ {
			ch := fields[2][i]
			if ch >= '0' && ch <= '9' {
				count = count*10 + int(ch-'0')
				continue
			}
			upper := ch &^ 0x20
			t, ok := letterTypes[upper]
			if !ok || t == King {
				return pos, fmt.Errorf("sfen: invalid piece in hand %q", string(ch))
			}
			c := Black
			if ch >= 'a' {
				c = White
			}
			if count == 0 {
				count = 1
			}
			pos.Hands[c][t] += int8(count)
			count = 0
		}
	}

	pos.MoveNum = 1
	if len(fields) >= 4 {
		n, err := strconv.Atoi(fields[3])
		if err != nil || n < 1 {
			return pos, fmt.Errorf("sfen: bad move number %q", fields[3])
		}
		pos.MoveNum = n
	}
	return pos, nil
}

func pieceSFEN(p Piece) string {
	t := p.Type()
	base := t.Base()
	letter := typeLetters[base]
	if p.Color() == White {
		letter |= 0x20
	}
	if t != base {
		return "+" + string(letter)
	}
	return string(letter)
}

// 持ち駒 표기 순서 (관례: 飛→角→金→銀→桂→香→歩, 선수 먼저)
var handOrder = []PieceType{Rook, Bishop, Gold, Silver, Knight, Lance, Pawn}

func (pos Position) SFEN() string {
	var b strings.Builder
	for row := 0; row < 9; row++ {
		if row > 0 {
			b.WriteByte('/')
		}
		empty := 0
		for col := 0; col < 9; col++ {
			p := pos.Board[row*9+col]
			if p.Empty() {
				empty++
				continue
			}
			if empty > 0 {
				b.WriteByte(byte('0' + empty))
				empty = 0
			}
			b.WriteString(pieceSFEN(p))
		}
		if empty > 0 {
			b.WriteByte(byte('0' + empty))
		}
	}

	if pos.Turn == Black {
		b.WriteString(" b ")
	} else {
		b.WriteString(" w ")
	}

	hasHand := false
	for _, c := range []Color{Black, White} {
		for _, t := range handOrder {
			n := pos.Hands[c][t]
			if n == 0 {
				continue
			}
			hasHand = true
			if n > 1 {
				b.WriteString(strconv.Itoa(int(n)))
			}
			letter := typeLetters[t]
			if c == White {
				letter |= 0x20
			}
			b.WriteByte(letter)
		}
	}
	if !hasHand {
		b.WriteByte('-')
	}

	b.WriteByte(' ')
	b.WriteString(strconv.Itoa(pos.MoveNum))
	return b.String()
}

// RepetitionKey 는 千日手 판정용 키 — SFEN에서 手数만 뗀다. 手番은 남긴다(배치가 같아도
// 둘 차례가 다르면 다른 국면이다).
//
// positions 테이블의 sfen_key 와 같은 형태다 — 전치(transposition)가 자연히 합쳐진다.
func (pos Position) RepetitionKey() string {
	s := pos.SFEN()
	return s[:strings.LastIndexByte(s, ' ')]
}
