package shogi

import "slices"

// 詰め将棋 탐색(internal/tsume)이 마디마다 부르는 수 생성이다. 탐색이 마디 수백만 개를 여는
// 자리라, 모든 pseudo 수를 적용해 보는 legalMoves 를 그대로 쓰면 마디 하나에 30µs 쯤 든다.
// 여기 둘은 적용해 볼 후보를 판 모양으로 먼저 거른다. 거른 뒤의 판정은 legalMoves 와 같고,
// 순서도 같다(checks_test.go 가 대조한다).

// hasDelta 는 ds 에 그 delta 가 있는가다. ds 는 先手 기준이라 後手 말은 부르는 쪽이 dr 에
// 부호를 곱해서 묻는다.
func hasDelta(ds []delta, dc, dr int8) bool {
	for _, d := range ds {
		if d.dc == dc && d.dr == dr {
			return true
		}
	}
	return false
}

// attacked 는 sq 가 by 색에게 노려지고 있는가다. IsAttacked 와 같은 답을 sq 에서 바깥으로
// 훑어서 낸다(판 전체를 돌지 않는다).
func (pos *Position) attacked(sq int, by Color) bool {
	row, col := int8(sq/9), int8(sq%9)
	sign := int8(1)
	if by == White {
		sign = -1
	}
	// 여덟 방향으로 처음 만나는 말을 본다. 桂는 아래에서 따로 본다.
	for _, d := range kingSteps {
		r, c := row+d.dr, col+d.dc
		first := true
		for r >= 0 && r <= 8 && c >= 0 && c <= 8 {
			p := pos.Board[int(r)*9+int(c)]
			if p.Empty() {
				r += d.dr
				c += d.dc
				first = false
				continue
			}
			if p.Color() == by {
				// 말에서 sq 로 가는 방향은 -d 이고, 그 말의 delta 표기로는 dr 에 sign 이 곱해진다.
				t := p.Type()
				if first && hasDelta(stepsOf(t), -d.dc, -d.dr*sign) {
					return true
				}
				if hasDelta(slidesOf(t), -d.dc, -d.dr*sign) {
					return true
				}
			}
			break
		}
	}
	// 桂. by 의 桂가 sq 를 노리려면 sq 에서 그 桂의 진행 반대쪽으로 두 칸 떨어져 있어야 한다.
	for _, dc := range []int8{-1, 1} {
		r, c := row+2*sign, col+dc
		if r < 0 || r > 8 || c < 0 || c > 8 {
			continue
		}
		if pos.Board[int(r)*9+int(c)] == MakePiece(Knight, by) {
			return true
		}
	}
	return false
}

// attackersOf 는 sq 를 노리는 by 색 말의 칸들이다. attacked 와 같은 규칙으로 바깥으로 훑는다.
func (pos *Position) attackersOf(sq int, by Color, out *[81]bool) int {
	n := 0
	row, col := int8(sq/9), int8(sq%9)
	sign := int8(1)
	if by == White {
		sign = -1
	}
	// 여덟 방향으로 처음 만나는 말을 본다. 桂는 아래에서 따로 본다.
	for _, d := range kingSteps {
		r, c := row+d.dr, col+d.dc
		first := true
		for r >= 0 && r <= 8 && c >= 0 && c <= 8 {
			s := int(r)*9 + int(c)
			p := pos.Board[s]
			if p.Empty() {
				r += d.dr
				c += d.dc
				first = false
				continue
			}
			if p.Color() == by {
				t := p.Type()
				if first && hasDelta(stepsOf(t), -d.dc, -d.dr*sign) || hasDelta(slidesOf(t), -d.dc, -d.dr*sign) {
					out[s] = true
					n++
				}
			}
			break
		}
	}
	for _, dc := range []int8{-1, 1} {
		r, c := row+2*sign, col+dc
		if r < 0 || r > 8 || c < 0 || c > 8 {
			continue
		}
		if s := int(r)*9 + int(c); pos.Board[s] == MakePiece(Knight, by) {
			out[s] = true
			n++
		}
	}
	return n
}

// inCheck 는 InCheck 와 같다. 玉이 없으면 거짓이다.
func (pos *Position) inCheck(c Color) bool {
	k := pos.KingSquare(c)
	return k >= 0 && pos.attacked(k, c.Other())
}

// reaches 는 c 색의 t 가 from 에 있을 때 target 을 노리는가다. vacated 칸은 비어 있는 것으로
// 본다(그 칸의 말이 지금 움직이는 중이다). 막는 말이 없는지까지 본다.
func (pos *Position) reaches(t PieceType, c Color, from, target, vacated int) bool {
	dr := int8(target/9 - from/9)
	dc := int8(target%9 - from%9)
	sign := int8(1)
	if c == White {
		sign = -1
	}
	if hasDelta(stepsOf(t), dc, dr*sign) {
		return true
	}
	slides := slidesOf(t)
	if len(slides) == 0 {
		return false
	}
	// 같은 줄 위인가. 가로·세로·대각선만 슬라이드가 된다.
	if dr != 0 && dc != 0 && dr != dc && dr != -dc {
		return false
	}
	ur, uc := unit(dr), unit(dc)
	if !hasDelta(slides, uc, ur*sign) {
		return false
	}
	r, cc := int8(from/9)+ur, int8(from%9)+uc
	for int(r)*9+int(cc) != target {
		s := int(r)*9 + int(cc)
		if s != vacated && !pos.Board[s].Empty() {
			return false
		}
		r += ur
		cc += uc
	}
	return true
}

func unit(x int8) int8 {
	switch {
	case x > 0:
		return 1
	case x < 0:
		return -1
	}
	return 0
}

// discoverers 는 그 칸의 말이 비키면 c 색의 슬라이드가 상대 玉 k 에 닿는 칸들이다.
func (pos *Position) discoverers(k int, c Color) [81]bool {
	var out [81]bool
	row, col := int8(k/9), int8(k%9)
	sign := int8(1)
	if c == White {
		sign = -1
	}
	for _, d := range kingSteps {
		r, cc := row+d.dr, col+d.dc
		blocker := -1
		for r >= 0 && r <= 8 && cc >= 0 && cc <= 8 {
			s := int(r)*9 + int(cc)
			p := pos.Board[s]
			if !p.Empty() {
				if blocker < 0 {
					if p.Color() != c {
						break
					}
					blocker = s
				} else {
					if p.Color() == c && hasDelta(slidesOf(p.Type()), -d.dc, -d.dr*sign) {
						out[blocker] = true
					}
					break
				}
			}
			r += d.dr
			cc += d.dc
		}
	}
	return out
}

// Checks 는 현재 수번이 둘 수 있는 王手 전부다. 순서는 LegalMoves 와 같다.
//
// 王手인지를 판 모양으로 먼저 본다. 옮긴 말이 玉을 노리거나 비킨 자리로 슬라이드가 열리는
// 수만 적용해 본다.
func (pos Position) Checks() []Move { return pos.checks(false) }

// HasCheck 는 王手가 하나라도 있는가다. 하나를 찾는 대로 멈춘다.
func (pos Position) HasCheck() bool { return len(pos.checks(true)) > 0 }

func (pos Position) checks(first bool) []Move {
	me := pos.Turn
	them := me.Other()
	k := pos.KingSquare(them)
	if k < 0 {
		return nil
	}
	disc := pos.discoverers(k, me)
	var out []Move
	consider := func(m Move) {
		if first && len(out) > 0 {
			return
		}
		np := pos.Apply(m)
		if !np.inCheck(them) || np.inCheck(me) {
			return
		}
		if m.IsDrop() && m.Drop == Pawn && !np.canEvade() {
			return // 打ち歩詰め
		}
		out = append(out, m)
	}
	for from := 0; from < 81; from++ {
		p := pos.Board[from]
		if p.Empty() || p.Color() != me {
			continue
		}
		t := p.Type()
		pos.pseudoBoardMoves(from, func(m Move) {
			nt := t
			if m.Promote {
				nt = t.Promoted()
			}
			if disc[from] || pos.reaches(nt, me, int(m.To), k, from) {
				consider(m)
			}
		})
	}
	// 투입으로 王手가 되는 칸은 玉에서 뻗은 빈 줄 위이거나 桂 자리뿐이다. 그 칸만 본다.
	near := pos.dropReach(k)
	for t := Pawn; t <= Rook; t++ {
		if pos.Hands[me][t] == 0 {
			continue
		}
		for sq := 0; sq < 81; sq++ {
			if !near[sq] || mustPromoteAt(t, sq, me) || t == Pawn && pos.nifu(sq%9, me) {
				continue
			}
			if pos.reaches(t, me, sq, k, -1) {
				consider(Move{From: -1, To: int8(sq), Drop: t})
			}
		}
	}
	return out
}

// dropReach 는 k 를 노릴 수 있는 빈 칸들이다. k 에서 여덟 방향으로 처음 말을 만나기 전까지의
// 빈 칸과, 桂가 k 를 노리는 네 칸 중 빈 것이다. 어느 편의 말이 노리는지는 가리지 않는다.
func (pos *Position) dropReach(k int) [81]bool {
	var out [81]bool
	row, col := int8(k/9), int8(k%9)
	for _, d := range kingSteps {
		r, c := row+d.dr, col+d.dc
		for r >= 0 && r <= 8 && c >= 0 && c <= 8 && pos.Board[int(r)*9+int(c)].Empty() {
			out[int(r)*9+int(c)] = true
			r += d.dr
			c += d.dc
		}
	}
	for _, dr := range []int8{-2, 2} {
		for _, dc := range []int8{-1, 1} {
			r, c := row+dr, col+dc
			if r >= 0 && r <= 8 && c >= 0 && c <= 8 && pos.Board[int(r)*9+int(c)].Empty() {
				out[int(r)*9+int(c)] = true
			}
		}
	}
	return out
}

// canEvade 는 王手를 받는 수번에게 합법수가 하나라도 있는가다. 打ち歩詰め 판정이 묻는다.
//
// 상대의 打ち歩詰め까지는 보지 않는다(legalMoves(false) 와 같은 규약). 응수인 歩 투입이
// 다시 打ち歩詰め인지를 물으면 끝없이 내려간다.
func (pos Position) canEvade() bool {
	return len(pos.evasions(false, true)) > 0
}

// Evasions 는 王手를 받고 있는 수번의 합법수 전부다. 순서는 LegalMoves 와 같다.
// 王手가 아니면 LegalMoves 를 그대로 준다.
//
// 玉이 움직이는 수, 거는 말을 잡는 수, 사이에 드는 수만 적용해 본다. 両王手면 玉만 움직인다.
func (pos Position) Evasions() []Move { return pos.evasions(true, false) }

// evasions 는 Evasions 의 본체다. uchifuzume 가 거짓이면 응수 쪽의 打ち歩詰め를 보지 않고,
// first 가 참이면 하나를 찾는 대로 멈춘다.
func (pos Position) evasions(uchifuzume, first bool) []Move {
	me := pos.Turn
	them := me.Other()
	k := pos.KingSquare(me)
	if k < 0 || !pos.attacked(k, them) {
		return pos.legalMoves(uchifuzume)
	}
	var checkers [81]bool
	nCheckers := pos.attackersOf(k, them, &checkers)
	// target 은 응수가 닿아야 하는 칸이다. 両王手면 비어 있어 玉만 움직인다.
	var target [81]bool
	if nCheckers == 1 {
		c := slices.Index(checkers[:], true)
		target[c] = true
		dr, dc := int8(k/9-c/9), int8(k%9-c%9)
		if dr == 0 || dc == 0 || dr == dc || dr == -dc {
			ur, uc := unit(dr), unit(dc)
			r, cc := int8(c/9)+ur, int8(c%9)+uc
			for int(r)*9+int(cc) != k {
				target[int(r)*9+int(cc)] = true
				r += ur
				cc += uc
			}
		}
	}
	// movers 는 target 중 한 칸이라도 노리는 내 말이다. 玉 말고는 이 말들만 수를 만들어 본다.
	var movers [81]bool
	movers[k] = true
	for sq := 0; sq < 81; sq++ {
		if target[sq] {
			pos.attackersOf(sq, me, &movers)
		}
	}
	var out []Move
	consider := func(m Move) {
		if first && len(out) > 0 {
			return
		}
		np := pos.Apply(m)
		if np.inCheck(me) {
			return
		}
		if uchifuzume && m.IsDrop() && m.Drop == Pawn {
			if np.inCheck(them) && !np.canEvade() {
				return // 打ち歩詰め
			}
		}
		out = append(out, m)
	}
	for from := 0; from < 81; from++ {
		if !movers[from] {
			continue
		}
		king := from == k
		pos.pseudoBoardMoves(from, func(m Move) {
			if king || target[m.To] {
				consider(m)
			}
		})
	}
	if nCheckers == 1 {
		for t := Pawn; t <= Rook; t++ {
			if pos.Hands[me][t] == 0 {
				continue
			}
			for sq := 0; sq < 81; sq++ {
				if !target[sq] || !pos.Board[sq].Empty() || mustPromoteAt(t, sq, me) || t == Pawn && pos.nifu(sq%9, me) {
					continue
				}
				consider(Move{From: -1, To: int8(sq), Drop: t})
			}
		}
	}
	return out
}
