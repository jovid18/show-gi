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
//
// 개입 문구가 駒를 이름으로 부를 때 쓴다. **棋譜와 같은 표를 본다**: 카드가 「▲8四銀不成」
// 이라고 적어놓고 문장이 그 駒를 다른 이름으로 부르면 초심자는 둘이 같은 것인 줄 모른다.
func PieceJa(t PieceType) string { return kanjiPiece[t] }

// squareJa 는 「2四」 형식으로 칸을 적는다.
func squareJa(sq int) string {
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
func horizJa(from, to int, c Color) string {
	d := FileOf(from) - FileOf(to)
	if c == White {
		d = -d
	}
	if d < 0 {
		return "右"
	}
	return "左"
}

// MoveJa 는 수 하나를 棋譜 표기로 적는다.
// prevTo 는 직전 수의 목적칸(없으면 -1) — 같으면 「同」을 쓴다.
func (pos Position) MoveJa(m Move, prevTo int) string {
	mark := "▲"
	if pos.Turn == White {
		mark = "△"
	}
	dest := squareJa(int(m.To))
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
