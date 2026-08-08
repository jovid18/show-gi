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
