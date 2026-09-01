package shogi

import "fmt"

// 棋譜 표기 생성: ▲2四歩, △同銀, ▲7七同金上, ▲5三桂不成.
//
// 엔진이 돌려주는 수순은 USI 뿐인데(7g7f), 그대로는 사람이 읽을 수 없다.
// 개입 화면과 리뷰 화면에서 "왜 그 수가 나쁜가"를 말하려면 수를 부를 이름이 있어야 한다.
//
// 출력이 처음부터 일본어라 UI에 그대로 나간다 — 여기서 만든 문자열은 번역 대상이 아니다.

var kanjiPiece = map[PieceType]string{
	Pawn: "歩", Lance: "香", Knight: "桂", Silver: "銀", Gold: "金",
	Bishop: "角", Rook: "飛", King: "玉",
	PromPawn: "と", PromLance: "成香", PromKnight: "成桂", PromSilver: "成銀",
	PromBishop: "馬", PromRook: "龍",
}

var kanjiRank = [...]string{"", "一", "二", "三", "四", "五", "六", "七", "八", "九"}

// PieceJa 는 駒 종류의 한자다 — 「銀」「成銀」「と」.
// 棋譜와 같은 표(kanjiPiece)를 본다 — 카드가 「▲8四銀不成」인데 문장이 그 駒를 달리 부르면
// 초심자는 둘이 같은 것인 줄 모른다.
func PieceJa(t PieceType) string { return kanjiPiece[t] }

// SquareJa 는 「2四」 형식으로 칸을 적는다. 棋譜와 같은 표기다 — 문장이 칸을 달리 부르면
// 초심자는 그것이 카드에 적힌 칸과 같은 칸인지 모른다(PieceJa 와 같은 이유).
func SquareJa(sq int) string {
	return fmt.Sprintf("%d%s", FileOf(sq), kanjiRank[RankOf(sq)])
}

// movers 는 to 로 갈 수 있는 같은 종류·같은 편의 반상 말들을 모은다(자기 자신 포함).
func (pos Position) movers(t PieceType, to int, c Color) []int {
	var out []int
	for _, lm := range pos.LegalMoves() {
		if lm.IsDrop() || int(lm.To) != to {
			continue
		}
		p := pos.Board[lm.From]
		if p.Color() != c || p.Type() != t {
			continue
		}
		// 같은 출발칸이 成/不成으로 두 번 나올 수 있다.
		dup := false
		for _, o := range out {
			if o == int(lm.From) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, int(lm.From))
		}
	}
	return out
}

// 이동 방향 — 두는 쪽 기준. 先手는 위(段 감소)가 전진이다.
func vertJa(from, to int, c Color) string {
	df := RankOf(to) - RankOf(from)
	if c == White {
		df = -df
	}
	switch {
	case df < 0:
		return "上"
	case df > 0:
		return "引"
	}
	return "寄"
}

// 좌우 — 두는 쪽에서 봤을 때 어느 쪽에서 왔는가.
// 先手는 1筋이 오른쪽이므로 筋 번호가 작을수록 오른쪽이다.
//
// 같은 筋(d==0)은 좌도 우도 아니라서 빈 문자열이다. 그 자리를 「左」로 메우면 진짜 왼쪽에서
// 온 駒와 라벨이 같아져 disambiguate 가 둘을 못 가르고, 그러면 같은 표기가 두 수를 가리킨다 —
// 実 코퍼스 296판 중 16판에서 실제로 나왔다(journal §126). 그 자리는 直이나 상하가 맡는다.
func horizJa(from, to int, c Color) string {
	d := FileOf(from) - FileOf(to)
	if c == White {
		d = -d
	}
	switch {
	case d < 0:
		return "右"
	case d > 0:
		return "左"
	}
	return ""
}

// MoveJa 는 수 하나를 棋譜 표기로 적는다.
// prevTo 는 직전 수의 목적칸(없으면 -1) — 같으면 「同」을 쓴다.
func (pos Position) MoveJa(m Move, prevTo int) string {
	mark := "▲"
	if pos.Turn == White {
		mark = "△"
	}
	dest := SquareJa(int(m.To))
	if prevTo >= 0 && int(m.To) == prevTo {
		dest = "同"
	}

	if m.IsDrop() {
		s := mark + dest + kanjiPiece[m.Drop]
		// 반상의 같은 말도 그 칸에 갈 수 있으면 打 를 붙여 구분한다.
		if len(pos.movers(m.Drop, int(m.To), pos.Turn)) > 0 {
			s += "打"
		}
		return s
	}

	t := pos.Board[m.From].Type()
	s := mark + dest + kanjiPiece[t]

	// 같은 칸에 갈 수 있는 동종 말이 둘 이상이면 어느 쪽인지 밝힌다.
	cands := pos.movers(t, int(m.To), pos.Turn)
	if len(cands) > 1 {
		s += disambiguate(int(m.From), int(m.To), cands, pos.Turn)
	}

	if m.Promote {
		s += "成"
	} else if t.CanPromote() && (inPromoZone(int(m.From), pos.Turn) || inPromoZone(int(m.To), pos.Turn)) {
		s += "不成"
	}
	return s
}

// disambiguate 는 후보들 중 from 을 특정하는 최소 수식어를 고른다.
// 상하(上/引/寄) → 直 → 좌우(右/左) → 좌우+상하 순으로 시도한다.
func disambiguate(from, to int, cands []int, c Color) string {
	try := func(label func(int) string) string {
		mine := label(from)
		for _, f := range cands {
			if f != from && label(f) == mine {
				return ""
			}
		}
		return mine
	}
	// 상하(上/寄/引)로 갈리면 그것을 쓴다. 이게 우선이다.
	if s := try(func(f int) string { return vertJa(f, to, c) }); s != "" {
		return s
	}
	// 直 은 둘 다 「위로 올라오는」 경우에만 쓴다 — 그중 바로 아래에서 곧장 올라온 쪽.
	if FileOf(from) == FileOf(to) && vertJa(from, to, c) == "上" {
		others := 0
		for _, f := range cands {
			if f != from && FileOf(f) == FileOf(to) && vertJa(f, to, c) == "上" {
				others++
			}
		}
		if others == 0 {
			return "直"
		}
	}
	if s := try(func(f int) string { return horizJa(f, to, c) }); s != "" {
		return s
	}
	if s := try(func(f int) string { return horizJa(f, to, c) + vertJa(f, to, c) }); s != "" {
		return s
	}
	return "" // 여기까지 와서 갈리지 않는 경우는 실질적으로 없다
}

// IsOriginModifier 는 그 글자가 원위치 수식어인가 — 右左上引寄直.
//
// disambiguate 가 붙이는 어휘와 같은 표를 본다. 읽는 쪽과 쓰는 쪽이 다른 표를 보면
// 이쪽이 만든 표기를 저쪽이 못 읽는 자리가 생긴다.
func IsOriginModifier(r rune) bool {
	switch r {
	case '右', '左', '上', '引', '寄', '直':
		return true
	}
	return false
}

// ResolveOrigin 은 원위치가 안 적힌 표기에서 출발칸을 되찾는다. disambiguate 의 반대 방향이다.
//
// KIF 는 「７六歩(77)」처럼 출발칸을 적지만 KI2 와 사람이 쓴 평문은 안 적는다. 후보는 룰
// 엔진이 뽑고(movers) mods 가 거른다.
//
// 하나로 안 좁혀지면 실패한다. 골라 버리면 그 뒤의 수순이 통째로 다른 판이 되고, 결과가
// 합법수라 ValidateMove 도 안 잡는다.
//
// mods 는 표기에 붙은 수식어를 순서 그대로 이어 붙인 것이다 — 「右上」이면 둘 다 건다.
func (pos Position) ResolveOrigin(t PieceType, to int, mods string) (int, error) {
	cands := pos.movers(t, to, pos.Turn)
	switch len(cands) {
	case 0:
		return 0, fmt.Errorf("no %s can reach %s", kanjiPiece[t], SquareJa(to))
	// 후보가 하나면 수식어는 뜻이 없다. 남이 굳이 붙여 놨어도 가리키는 곳이 하나다.
	case 1:
		return cands[0], nil
	}
	for _, r := range mods {
		if !IsOriginModifier(r) {
			return 0, fmt.Errorf("unknown modifier %q", string(r))
		}
	}

	// ① 이 패키지가 그 국면에서 쓸 표기와 글자까지 같은 후보. disambiguate 의 정확한
	//    반대라, 스스로 적은 표기는 언제나 여기서 걸린다.
	//
	//    독립 조건으로 거르는 것만으로는 모자란다 — 같은 筋의 駒는 좌우가 없어서
	//    「引」 하나로 적히는데, 그것을 「vertJa 가 引인 것」으로 읽으면 왼쪽에서 온
	//    「左引」의 駒까지 같이 걸린다(journal §126).
	if f, ok := onlyOne(cands, func(f int) bool {
		return disambiguate(f, to, cands, pos.Turn) == mods
	}); ok {
		return f, nil
	}

	// ② 남이 적은 표기. 같은 국면을 다른(그러나 옳은) 수식어로 적는 도구가 있어서,
	//    수식어를 하나씩 조건으로 걸어 좁힌다.
	if f, ok := onlyOne(cands, func(f int) bool {
		for _, r := range mods {
			if !matchesModifier(r, f, to, pos.Turn) {
				return false
			}
		}
		return true
	}); ok {
		return f, nil
	}

	return 0, fmt.Errorf("%d %s can reach %s, %q does not say which", len(cands), kanjiPiece[t], SquareJa(to), mods)
}

// onlyOne 은 조건에 맞는 후보가 정확히 하나일 때만 그것을 준다.
func onlyOne(cands []int, ok func(int) bool) (int, bool) {
	found, n := 0, 0
	for _, f := range cands {
		if ok(f) {
			found, n = f, n+1
		}
	}
	return found, n == 1
}

// matchesModifier 는 수식어 하나가 그 출발칸을 가리키는가. IsOriginModifier 가 참인
// 글자만 온다.
func matchesModifier(r rune, from, to int, c Color) bool {
	switch r {
	case '上', '引', '寄':
		return vertJa(from, to, c) == string(r)
	// 直 은 바로 아래에서 곧장 올라온 것이다. disambiguate 가 붙이는 조건과 같다.
	case '直':
		return FileOf(from) == FileOf(to) && vertJa(from, to, c) == "上"
	case '右', '左':
		return horizJa(from, to, c) == string(r)
	}
	return false
}

// LineJa 는 수순 전체를 棋譜 표기로 옮긴다. "pass"(手抜き)는 빈 문자열로 남긴다.
func (pos Position) LineJa(usis []string) ([]string, error) {
	out := make([]string, 0, len(usis))
	prevTo := -1
	for i, u := range usis {
		if u == "pass" {
			pos.Turn = pos.Turn.Other()
			out = append(out, "")
			continue
		}
		m, err := ParseUSIMove(u)
		if err != nil {
			return nil, fmt.Errorf("move %d %q: %w", i+1, u, err)
		}
		if err := pos.ValidateMove(m); err != nil {
			return nil, fmt.Errorf("move %d: %w", i+1, err)
		}
		out = append(out, pos.MoveJa(m, prevTo))
		prevTo = int(m.To)
		pos = pos.Apply(m)
	}
	return out, nil
}
