package shogi

type delta struct{ dc, dr int8 }

// 선수(Black, 위로 전진 = row 감소) 기준. 후수는 dr 부호 반전.
var (
	goldSteps   = []delta{{0, -1}, {-1, -1}, {1, -1}, {-1, 0}, {1, 0}, {0, 1}}
	kingSteps   = []delta{{0, -1}, {-1, -1}, {1, -1}, {-1, 0}, {1, 0}, {0, 1}, {-1, 1}, {1, 1}}
	silverSteps = []delta{{0, -1}, {-1, -1}, {1, -1}, {-1, 1}, {1, 1}}
	knightSteps = []delta{{-1, -2}, {1, -2}}
	pawnSteps   = []delta{{0, -1}}
	orthoDirs   = []delta{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
	diagDirs    = []delta{{-1, -1}, {1, -1}, {-1, 1}, {1, 1}}
	lanceDirs   = []delta{{0, -1}}
)

func stepsOf(t PieceType) []delta {
	switch t {
	case Pawn:
		return pawnSteps
	case Knight:
		return knightSteps
	case Silver:
		return silverSteps
	case Gold, PromPawn, PromLance, PromKnight, PromSilver:
		return goldSteps
	case King:
		return kingSteps
	case PromBishop:
		return orthoDirs // 馬 = 角 슬라이드 + 십자 한 칸
	case PromRook:
		return diagDirs // 龍 = 飛 슬라이드 + 대각 한 칸
	}
	return nil
}

func slidesOf(t PieceType) []delta {
	switch t {
	case Lance:
		return lanceDirs
	case Bishop, PromBishop:
		return diagDirs
	case Rook, PromRook:
		return orthoDirs
	}
	return nil
}

// attackTargets 는 sq의 말이 노리는 칸들(자기 말이 있는 칸 포함 — 방어 판정에 사용)을 fn에 전달.
// fn이 false를 반환하면 중단.
func (pos *Position) attackTargets(sq int, fn func(to int) bool) {
	p := pos.Board[sq]
	if p.Empty() {
		return
	}
	t := p.Type()
	c := p.Color()
	row, col := int8(sq/9), int8(sq%9)

	sign := int8(1)
	if c == White {
		sign = -1
	}

	for _, d := range stepsOf(t) {
		r, cl := row+sign*d.dr, col+d.dc
		if r < 0 || r > 8 || cl < 0 || cl > 8 {
			continue
		}
		if !fn(int(r)*9 + int(cl)) {
			return
		}
	}
	for _, d := range slidesOf(t) {
		r, cl := row+sign*d.dr, col+d.dc
		for r >= 0 && r <= 8 && cl >= 0 && cl <= 8 {
			to := int(r)*9 + int(cl)
			if !fn(to) {
				return
			}
			if !pos.Board[to].Empty() {
				break
			}
			r += sign * d.dr
			cl += d.dc
		}
	}
}

// IsAttacked: sq가 by 색의 말에게 공격받고 있는가.
func (pos *Position) IsAttacked(sq int, by Color) bool {
	for s := 0; s < 81; s++ {
		p := pos.Board[s]
		if p.Empty() || p.Color() != by {
			continue
		}
		attacked := false
		pos.attackTargets(s, func(to int) bool {
			if to == sq {
				attacked = true
				return false
			}
			return true
		})
		if attacked {
			return true
		}
	}
	return false
}

// AttackCount: sq를 노리는 by 색 말의 개수.
//
// IsAttacked 가 「있는가」라면 이쪽은 「몇 개인가」다. 玉 주변의 攻め와 守り를 견줄 때
// bool로는 「지키던 말이 하나 줄었다」가 안 보인다 — 0이 되기 전까지 아무 일도 없는 것이
// 되어버린다.
//
// **자기 말이 있는 칸도 센다**(attackTargets 와 같은 규칙). 방어 利き을 세는 것이
// 이 함수의 용도이므로, 지키는 말 위의 利き을 빼면 셀 것이 없어진다.
//
// 핀은 보지 않는다. 「그 말이 실제로 갈 수 있는가」가 아니라 「노리고 있는가」를 세는
// 것이고, 후자가 玉의 안전을 재는 데 쓰는 값이다.
func (pos *Position) AttackCount(sq int, by Color) int {
	n := 0
	for s := 0; s < 81; s++ {
		p := pos.Board[s]
		if p.Empty() || p.Color() != by {
			continue
		}
		pos.attackTargets(s, func(to int) bool {
			if to == sq {
				n++
				return false
			}
			return true
		})
	}
	return n
}

// Attackers 는 sq 를 노리는 by 색 말이 **어느 칸에** 있는지다.
//
// AttackCount 가 「몇 개인가」라면 이쪽은 「어디인가」다. 王手를 화면에 그리려면 그 값이
// 필요하다 — 「王手다」까지는 InCheck 가 말하지만, **어느 말이 걸고 있는지**를 모르면
// 초심자는 판에서 그것을 찾아야 하고, 両王手인지 아닌지도 알 수 없다.
//
// 핀은 보지 않는다. AttackCount 와 같은 규칙이고, 王手를 거는 말은 애초에 핀에 걸릴 수 없다.
func (pos *Position) Attackers(sq int, by Color) []int {
	var out []int
	for s := 0; s < 81; s++ {
		p := pos.Board[s]
		if p.Empty() || p.Color() != by {
			continue
		}
		pos.attackTargets(s, func(to int) bool {
			if to == sq {
				out = append(out, s)
				return false
			}
			return true
		})
	}
	return out
}

// SquareUSI 는 칸 번호를 USI 좌표(`7g`)로 적는다. 화면이 칸을 짚을 때 쓰는 표기다.
func SquareUSI(sq int) string { return sqUSI(int8(sq)) }

// Neighbors8 은 sq 를 둘러싼 8칸이다. 판 밖은 빠지므로 모서리에서는 3칸이다.
//
// 玉 주변의 넓이를 여기서 한 번만 정한다 — 부르는 쪽마다 8방향을 다시 적으면
// 「玉 주변」의 뜻이 조금씩 갈린다.
func Neighbors8(sq int) []int {
	row, col := sq/9, sq%9
	out := make([]int, 0, 8)
	for dr := -1; dr <= 1; dr++ {
		for dc := -1; dc <= 1; dc++ {
			if dr == 0 && dc == 0 {
				continue
			}
			r, c := row+dr, col+dc
			if r < 0 || r > 8 || c < 0 || c > 8 {
				continue
			}
			out = append(out, r*9+c)
		}
	}
	return out
}

func (pos *Position) KingSquare(c Color) int {
	target := MakePiece(King, c)
	for s := 0; s < 81; s++ {
		if pos.Board[s] == target {
			return s
		}
	}
	return -1
}

// InCheck: c의 왕이 王手를 받고 있는가.
func (pos *Position) InCheck(c Color) bool {
	k := pos.KingSquare(c)
	if k < 0 {
		return false
	}
	return pos.IsAttacked(k, c.Other())
}

// Apply 는 수를 적용한 새 국면을 돌려준다 (합법성 검증 없음 — 호출 측 책임).
func (pos Position) Apply(m Move) Position {
	np := pos
	me := pos.Turn
	if m.IsDrop() {
		np.Board[m.To] = MakePiece(m.Drop, me)
		np.Hands[me][m.Drop]--
	} else {
		p := np.Board[m.From]
		if cap := np.Board[m.To]; !cap.Empty() {
			// 왕은 持ち駒가 될 수 없다 (합법 대국에선 잡히지 않지만 인위 국면 방어)
			if bt := cap.Type().Base(); bt != King {
				np.Hands[me][bt]++
			}
		}
		if m.Promote {
			p = MakePiece(p.Type().Promoted(), me)
		}
		np.Board[m.To] = p
		np.Board[m.From] = 0
	}
	np.Turn = me.Other()
	np.MoveNum++
	return np
}

// 승격 가능 존: 선수 row 0..2 (1~3단), 후수 row 6..8 (7~9단).
func inPromoZone(sq int, c Color) bool {
	row := sq / 9
	if c == Black {
		return row <= 2
	}
	return row >= 6
}

// mustPromoteAt: 그 칸에 두면 다시 움직일 수 없어 승격이 강제되는가 (行き所のない駒).
func mustPromoteAt(t PieceType, sq int, c Color) bool {
	row := sq / 9
	switch t {
	case Pawn, Lance:
		if c == Black {
			return row == 0
		}
		return row == 8
	case Knight:
		if c == Black {
			return row <= 1
		}
		return row >= 7
	}
	return false
}

// nifu: 같은 筋(세로줄)에 자기 미승격 歩가 이미 있는가.
func (pos *Position) nifu(col int, c Color) bool {
	target := MakePiece(Pawn, c)
	for row := 0; row < 9; row++ {
		if pos.Board[row*9+col] == target {
			return true
		}
	}
	return false
}

// pseudoBoardMoves: from의 말이 갈 수 있는 수들(승격 변형 포함, 왕 안전 미검증).
func (pos *Position) pseudoBoardMoves(from int, emit func(Move)) {
	p := pos.Board[from]
	me := p.Color()
	t := p.Type()
	pos.attackTargets(from, func(to int) bool {
		if !pos.Board[to].Empty() && pos.Board[to].Color() == me {
			return true // 자기 말 위로는 못 감
		}
		m := Move{From: int8(from), To: int8(to)}
		if t.CanPromote() && (inPromoZone(from, me) || inPromoZone(to, me)) {
			emit(Move{From: int8(from), To: int8(to), Promote: true})
			if mustPromoteAt(t, to, me) {
				return true // 강제 승격: 미승격 변형 없음
			}
		}
		emit(m)
		return true
	})
}

// pseudoDrops: 持ち駒 투입 후보 (二歩·行き所 검사 포함, 왕 안전·打ち歩詰め 미검증).
func (pos *Position) pseudoDrops(emit func(Move)) {
	me := pos.Turn
	for t := Pawn; t <= Rook; t++ {
		if pos.Hands[me][t] == 0 {
			continue
		}
		for sq := 0; sq < 81; sq++ {
			if !pos.Board[sq].Empty() {
				continue
			}
			if mustPromoteAt(t, sq, me) {
				continue // 行き所のない駒
			}
			if t == Pawn && pos.nifu(sq%9, me) {
				continue // 二歩
			}
			emit(Move{From: -1, To: int8(sq), Drop: t})
		}
	}
}

// legalMoves: 합법수 전체. checkUchifuzume=false 는 내부 재귀용(打ち歩詰め 판정 시
// 상대 응수 존재 여부만 보면 되므로 상대의 打ち歩詰め까지 따질 필요 없음).
func (pos *Position) legalMoves(checkUchifuzume bool) []Move {
	me := pos.Turn
	var out []Move
	consider := func(m Move) {
		np := pos.Apply(m)
		if np.InCheck(me) {
			return // 王手放置 / 자살수
		}
		if checkUchifuzume && m.IsDrop() && m.Drop == Pawn {
			if np.InCheck(me.Other()) && len(np.legalMoves(false)) == 0 {
				return // 打ち歩詰め
			}
		}
		out = append(out, m)
	}
	for from := 0; from < 81; from++ {
		p := pos.Board[from]
		if p.Empty() || p.Color() != me {
			continue
		}
		pos.pseudoBoardMoves(from, consider)
	}
	pos.pseudoDrops(consider)
	return out
}

// LegalMoves 는 현재 수번의 모든 합법수를 돌려준다.
func (pos Position) LegalMoves() []Move { return pos.legalMoves(true) }

// NoLegalMoves: 합법수가 하나도 없는가 (쇼기에서는 곧 패배 — 대부분 詰み).
func (pos Position) NoLegalMoves() bool { return len(pos.legalMoves(true)) == 0 }

// IsCheckmate: 수번 측이 王手를 받고 있고 벗어날 수 없는가.
func (pos Position) IsCheckmate() bool {
	return pos.InCheck(pos.Turn) && pos.NoLegalMoves()
}
