package kifu

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

type GameResult int

const (
	ResultUnknown GameResult = iota
	ResultSenteWin
	ResultGoteWin
	ResultDraw
)

type ParsedGame struct {
	StartSFEN string
	Moves     []string
	Result    GameResult
	Sente     string
	Gote      string
	Source    string
}

var kanjiToRank = map[rune]int{
	'一': 1, '二': 2, '三': 3, '四': 4, '五': 5,
	'六': 6, '七': 7, '八': 8, '九': 9,
}

var pieceFromKanji = map[string]shogi.PieceType{
	"歩": shogi.Pawn, "香": shogi.Lance, "桂": shogi.Knight,
	"銀": shogi.Silver, "金": shogi.Gold, "角": shogi.Bishop,
	"飛": shogi.Rook, "玉": shogi.King, "王": shogi.King,
	"と": shogi.PromPawn, "龍": shogi.PromRook, "竜": shogi.PromRook,
	"馬":  shogi.PromBishop,
	"成香": shogi.PromLance, "成桂": shogi.PromKnight, "成銀": shogi.PromSilver,
}

var dropLetter = map[shogi.PieceType]byte{
	shogi.Pawn: 'P', shogi.Lance: 'L', shogi.Knight: 'N',
	shogi.Silver: 'S', shogi.Gold: 'G', shogi.Bishop: 'B', shogi.Rook: 'R',
}

var csaToPiece = map[string]shogi.PieceType{
	"FU": shogi.Pawn, "KY": shogi.Lance, "KE": shogi.Knight,
	"GI": shogi.Silver, "KI": shogi.Gold, "KA": shogi.Bishop,
	"HI": shogi.Rook, "OU": shogi.King,
	"TO": shogi.PromPawn, "NY": shogi.PromLance, "NK": shogi.PromKnight,
	"NG": shogi.PromSilver, "UM": shogi.PromBishop, "RY": shogi.PromRook,
}

func fullWidthFile(r rune) (int, bool) {
	if r >= '１' && r <= '９' {
		return int(r-'１') + 1, true
	}
	return 0, false
}

// ── KIF ─────────────────────────────────────────────────────

var kifMoveRe = regexp.MustCompile(`^\s*(\d+)\s+`)
var originRe = regexp.MustCompile(`\((\d)(\d)\)`)
var timeRe = regexp.MustCompile(`\(\s*\d+:\d+[^)]*\)\s*$`)

func ParseKIF(input string) (ParsedGame, error) {
	g := ParsedGame{StartSFEN: shogi.StartSFEN}
	pos := shogi.StartPosition()
	prevTo := -1

	sc := bufio.NewScanner(strings.NewReader(input))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")

		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "*") {
			continue
		}
		if strings.HasPrefix(line, "先手：") || strings.HasPrefix(line, "下手：") {
			g.Sente = strings.TrimSpace(line[len("先手："):])
			continue
		}
		if strings.HasPrefix(line, "後手：") || strings.HasPrefix(line, "上手：") {
			g.Gote = strings.TrimSpace(line[len("後手："):])
			continue
		}
		if strings.HasPrefix(line, "手合割") || strings.HasPrefix(line, "手数") {
			continue
		}

		if !kifMoveRe.MatchString(line) {
			continue
		}

		loc := kifMoveRe.FindStringIndex(line)
		rest := line[loc[1]:]

		rest = timeRe.ReplaceAllString(rest, "")
		rest = strings.TrimSpace(rest)

		if rest == "" {
			continue
		}

		switch {
		case strings.HasPrefix(rest, "投了"):
			if pos.Turn == shogi.Black {
				g.Result = ResultGoteWin
			} else {
				g.Result = ResultSenteWin
			}
			continue
		case strings.HasPrefix(rest, "中断"):
			g.Result = ResultUnknown
			continue
		case strings.HasPrefix(rest, "千日手"):
			g.Result = ResultDraw
			continue
		case strings.HasPrefix(rest, "持将棋"):
			g.Result = ResultDraw
			continue
		}

		m, err := parseKIFMove(rest, pos, prevTo)
		if err != nil {
			return g, fmt.Errorf("move %d: %w", len(g.Moves)+1, err)
		}
		if err := pos.ValidateMove(m); err != nil {
			return g, fmt.Errorf("move %d (%s): %w", len(g.Moves)+1, m.USI(), err)
		}
		g.Moves = append(g.Moves, m.USI())
		prevTo = int(m.To)
		pos = pos.Apply(m)
	}
	return g, sc.Err()
}

func parseKIFMove(text string, pos shogi.Position, prevTo int) (shogi.Move, error) {
	runes := []rune(text)
	idx := 0

	var toFile, toRank int

	if runes[idx] == '同' {
		if prevTo < 0 {
			return shogi.Move{}, fmt.Errorf("同 without previous move")
		}
		toFile = shogi.FileOf(prevTo)
		toRank = shogi.RankOf(prevTo)
		idx++
		for idx < len(runes) && (runes[idx] == '　' || runes[idx] == ' ') {
			idx++
		}
	} else {
		f, ok := fullWidthFile(runes[idx])
		if !ok {
			return shogi.Move{}, fmt.Errorf("expected file digit, got %c", runes[idx])
		}
		toFile = f
		idx++
		if idx >= len(runes) {
			return shogi.Move{}, fmt.Errorf("unexpected end after file")
		}
		r, ok := kanjiToRank[runes[idx]]
		if !ok {
			return shogi.Move{}, fmt.Errorf("expected rank kanji, got %c", runes[idx])
		}
		toRank = r
		idx++
	}
	to := shogi.SquareOf(toFile, toRank)

	if idx >= len(runes) {
		return shogi.Move{}, fmt.Errorf("unexpected end after destination")
	}

	var pt shogi.PieceType
	if idx+1 < len(runes) {
		if p, ok := pieceFromKanji[string(runes[idx:idx+2])]; ok {
			pt = p
			idx += 2
		}
	}
	if pt == 0 {
		p, ok := pieceFromKanji[string(runes[idx:idx+1])]
		if !ok {
			return shogi.Move{}, fmt.Errorf("unknown piece: %s", string(runes[idx:idx+1]))
		}
		pt = p
		idx++
	}

	promote := false
	isDrop := false
	if idx < len(runes) {
		switch {
		case runes[idx] == '成':
			promote = true
			idx++
		case idx+1 < len(runes) && runes[idx] == '不' && runes[idx+1] == '成':
			idx += 2
		case runes[idx] == '打':
			isDrop = true
			idx++
		}
	}

	if isDrop {
		base := pt.Base()
		if _, ok := dropLetter[base]; !ok {
			return shogi.Move{}, fmt.Errorf("cannot drop %v", pt)
		}
		return shogi.Move{From: -1, To: int8(to), Drop: base}, nil
	}

	remaining := string(runes[idx:])
	om := originRe.FindStringSubmatch(remaining)
	if om == nil {
		return shogi.Move{}, fmt.Errorf("expected origin (XY) in %q", remaining)
	}
	fromFile := int(om[1][0] - '0')
	fromRank := int(om[2][0] - '0')
	from := shogi.SquareOf(fromFile, fromRank)

	return shogi.Move{From: int8(from), To: int8(to), Promote: promote}, nil
}

// ── CSA ─────────────────────────────────────────────────────

func ParseCSA(input string) (ParsedGame, error) {
	g := ParsedGame{StartSFEN: shogi.StartSFEN}
	pos := shogi.StartPosition()

	sc := bufio.NewScanner(strings.NewReader(input))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if len(line) == 0 {
			continue
		}

		switch {
		case strings.HasPrefix(line, "N+"):
			g.Sente = line[2:]
		case strings.HasPrefix(line, "N-"):
			g.Gote = line[2:]
		case strings.HasPrefix(line, "'"), strings.HasPrefix(line, "V"),
			strings.HasPrefix(line, "P"), strings.HasPrefix(line, "T"),
			strings.HasPrefix(line, "$"):
			continue
		// **投了와 시간초과는 같은 결과다** — 手番 쪽이 진다.
		//
		// `%TIME_UP` 은 floodgate 실 코퍼스에서 나왔다(341판 중 3판). 없으면 그 판이
		// `ResultUnknown` 으로 떨어져 K 실측의 승패 표본에서 조용히 빠진다.
		case strings.HasPrefix(line, "%TORYO"), strings.HasPrefix(line, "%TIME_UP"):
			if pos.Turn == shogi.Black {
				g.Result = ResultGoteWin
			} else {
				g.Result = ResultSenteWin
			}
		case strings.HasPrefix(line, "%CHUDAN"):
			g.Result = ResultUnknown
		case strings.HasPrefix(line, "%SENNICHITE"):
			g.Result = ResultDraw
		case strings.HasPrefix(line, "%JISHOGI"):
			g.Result = ResultDraw
		case (line[0] == '+' || line[0] == '-') && len(line) >= 7:
			m, err := parseCSAMove(line, pos)
			if err != nil {
				return g, fmt.Errorf("CSA move %d: %w", len(g.Moves)+1, err)
			}
			if err := pos.ValidateMove(m); err != nil {
				return g, fmt.Errorf("CSA move %d (%s): %w", len(g.Moves)+1, m.USI(), err)
			}
			g.Moves = append(g.Moves, m.USI())
			pos = pos.Apply(m)
		}
	}
	return g, sc.Err()
}

func parseCSAMove(line string, pos shogi.Position) (shogi.Move, error) {
	if len(line) < 7 {
		return shogi.Move{}, fmt.Errorf("too short: %q", line)
	}
	fromFile := int(line[1] - '0')
	fromRank := int(line[2] - '0')
	toFile := int(line[3] - '0')
	toRank := int(line[4] - '0')
	pieceCode := line[5:7]

	pt, ok := csaToPiece[pieceCode]
	if !ok {
		return shogi.Move{}, fmt.Errorf("unknown piece: %q", pieceCode)
	}
	to := shogi.SquareOf(toFile, toRank)

	if fromFile == 0 && fromRank == 0 {
		return shogi.Move{From: -1, To: int8(to), Drop: pt.Base()}, nil
	}

	from := shogi.SquareOf(fromFile, fromRank)
	boardPiece := pos.Board[from]
	promote := !boardPiece.Empty() && boardPiece.Type().CanPromote() && pt == boardPiece.Type().Promoted()

	return shogi.Move{From: int8(from), To: int8(to), Promote: promote}, nil
}
