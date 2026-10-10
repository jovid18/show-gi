package kifu

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// ErrNoBOD 는 글에 局面図(BOD)가 없을 때의 오류다. 읽다 깨진 것과 가른다.
var ErrNoBOD = errors.New("bod: no board diagram")

// BODError 는 局面図의 어느 줄을 읽지 못했는지다. Rank 가 0 이면 판 밖의 줄(持駒)이거나,
// Line 까지 비었으면 段이 아홉보다 많은 판이다.
type BODError struct {
	Rank int
	Line string
}

func (e *BODError) Error() string {
	if e.Rank == 0 {
		return fmt.Sprintf("bod: cannot read %q", e.Line)
	}
	return fmt.Sprintf("bod: cannot read rank %d %q", e.Rank, e.Line)
}

// bodPieces 는 局面図의 한 칸 글자다. 성한 駒는 한 글자로 줄여 적는다(圭=成桂·杏=成香·全=成銀).
var bodPieces = map[rune]string{
	'玉': "K", '王': "K",
	'飛': "R", '龍': "+R", '竜': "+R",
	'角': "B", '馬': "+B",
	'金': "G",
	'銀': "S", '全': "+S",
	'桂': "N", '圭': "+N",
	'香': "L", '杏': "+L",
	'歩': "P", 'と': "+P",
}

// handOrder 는 SFEN 의 持ち駒 순서다.
var handOrder = []struct {
	ja     rune
	letter string
}{{'飛', "R"}, {'角', "B"}, {'金', "G"}, {'銀', "S"}, {'桂', "N"}, {'香', "L"}, {'歩', "P"}}

// ParseBOD 는 KIF 의 局面図를 SFEN 으로 옮긴다. 수순이 같이 있어도 보지 않는다.
//
// 판 줄은 위 段부터, 한 줄 안에서는 9筋부터 적힌다. SFEN 의 칸 순서와 같아 좌표 계산이 없다.
// 手番은 「後手番」 줄이 있을 때만 後手다. 없으면 先手로 읽는 것이 KIF 의 관례다.
func ParseBOD(text string) (string, error) {
	var (
		rows   []string
		hands  [2]string
		turn   = "b"
		inside bool
		seen   bool
	)
	for raw := range strings.Lines(text) {
		line := strings.TrimSpace(raw)
		if side, v, ok := handLine(line); ok {
			h, err := bodHand(v)
			if err != nil {
				return "", &BODError{Line: line}
			}
			hands[side] = h
			continue
		}
		switch {
		case strings.HasPrefix(line, "+-"):
			// 위 테두리에서 열고 아래 테두리에서 닫는다. 둘째 局面図는 보지 않는다.
			switch {
			case inside:
				inside = false
			case !seen:
				inside, seen = true, true
			}
		case inside && strings.HasPrefix(line, "|"):
			row, err := bodRow(line)
			if err != nil {
				return "", &BODError{Rank: len(rows) + 1, Line: line}
			}
			rows = append(rows, row)
		case line == "後手番" || line == "上手番":
			turn = "w"
		case line == "先手番" || line == "下手番":
			turn = "b"
		}
	}
	if !seen {
		return "", ErrNoBOD
	}
	// 모자라면 빠진 첫 段을, 넘치면 段을 짚지 않는다.
	if len(rows) < 9 {
		return "", &BODError{Rank: len(rows) + 1}
	}
	if len(rows) > 9 {
		return "", &BODError{}
	}

	hand := hands[0] + strings.ToLower(hands[1])
	if hand == "" {
		hand = "-"
	}
	pos, err := shogi.ParseSFEN(strings.Join(rows, "/") + " " + turn + " " + hand + " 1")
	if err != nil {
		return "", err
	}
	return pos.SFEN(), nil
}

// bodRow 는 「|v香v角 ・ … v香|一」 한 줄을 SFEN 의 한 段으로 옮긴다.
//
// 칸마다 앞 글자가 색이다. v 가 後手, 공백이 先手다. 칸이 아홉이 아니면 실패다.
func bodRow(line string) (string, error) {
	body := line[1:]
	end := strings.LastIndex(body, "|")
	if end < 0 {
		return "", errors.New("bod: row has no closing bar")
	}
	body = body[:end]

	var (
		b     strings.Builder
		cells int
		empty int
		gote  bool
	)
	for _, r := range body {
		switch {
		case r == 'v' || r == 'V':
			gote = true
		case r == ' ' || r == '　':
		case r == '・':
			if gote {
				return "", errors.New("bod: v before an empty square")
			}
			cells++
			empty++
		default:
			p, ok := bodPieces[r]
			if !ok {
				return "", fmt.Errorf("bod: unknown piece %q", r)
			}
			if empty > 0 {
				b.WriteString(strconv.Itoa(empty))
				empty = 0
			}
			if gote {
				p = strings.ToLower(p)
			}
			b.WriteString(p)
			cells++
			gote = false
		}
	}
	if cells != 9 || gote {
		return "", fmt.Errorf("bod: row has %d squares", cells)
	}
	if empty > 0 {
		b.WriteString(strconv.Itoa(empty))
	}
	return b.String(), nil
}

// handLine 은 持駒 줄이면 어느 쪽(0=先手·1=後手)인지와 값을 준다.
//
// 駒落ち에서는 下手가 先手, 上手가 後手 자리다.
func handLine(line string) (int, string, bool) {
	for side, keys := range [2][]string{{"先手の持駒", "下手の持駒"}, {"後手の持駒", "上手の持駒"}} {
		if v, ok := headerName(line, keys...); ok {
			return side, v, true
		}
	}
	return 0, "", false
}

// bodHand 는 「飛　角　金三　歩四」를 SFEN 의 持ち駒(大文字)로 옮긴다. 「なし」는 빈 값이다.
func bodHand(v string) (string, error) {
	counts := map[rune]int{}
	for _, tok := range strings.Fields(v) {
		if tok == "なし" {
			continue
		}
		piece, size := utf8.DecodeRuneInString(tok)
		n, ok := kanjiCount(tok[size:])
		if !ok {
			return "", fmt.Errorf("bod: bad count in %q", tok)
		}
		known := false
		for _, h := range handOrder {
			if h.ja == piece {
				known = true
			}
		}
		if !known {
			return "", fmt.Errorf("bod: %q cannot be in hand", piece)
		}
		counts[piece] += n
	}
	var b strings.Builder
	for _, h := range handOrder {
		switch n := counts[h.ja]; {
		case n == 1:
			b.WriteString(h.letter)
		case n > 1:
			b.WriteString(strconv.Itoa(n) + h.letter)
		}
	}
	return b.String(), nil
}

// kanjiCount 는 持駒 뒤의 수다. 없으면 1 이다. 「十八」까지 한자로 오고, 아라비아 숫자를
// 쓰는 도구도 있어 둘 다 받는다.
func kanjiCount(s string) (int, bool) {
	if s == "" {
		return 1, true
	}
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return n, true
	}
	digits := map[rune]int{'一': 1, '二': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	rs := []rune(s)
	switch {
	case len(rs) == 1 && rs[0] == '十':
		return 10, true
	case len(rs) == 1:
		n, ok := digits[rs[0]]
		return n, ok
	case len(rs) == 2 && rs[0] == '十':
		n, ok := digits[rs[1]]
		return 10 + n, ok
	}
	return 0, false
}
