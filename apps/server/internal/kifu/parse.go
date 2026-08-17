// Package kifu 는 실 기보(KIF·CSA)를 읽어 **프로덕션과 같은 경로로 다시 둔다.**
//
// **서버는 이 패키지를 안 쓴다** — 부르는 것은 `cmd/importkifu` 뿐이다. 목적은 태그가
// 남의 대국에서도 맞게 붙는지를 넓게 보는 것과(journal §44) 상수를 기록으로 다시
// 채점하는 것이고(§39), 그래서 임포트는 `game.NamedTesuji` · `intervene.Judge` 를 **그대로**
// 지난다. 여기서 지름길을 내면 재는 대상이 프로덕션이 아니게 된다.
//
// **파서는 平手만 안다.** 駒落ち는 여기서 실패하지 않고 나중에 `shogi.ValidateMove` 에서
// 엉뚱해 보이는 반칙으로 죽는다.
//
// 기보 파일은 **레포에 커밋하지 않는다**(남의 대국 기록이고 하루치가 5MB 넘는다).
// 받는 법과 env 게이트는 apps/server/README.md.
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

// 값을 안 쓴다 — 「投入할 수 있는 駒인가」(즉 玉이 아닌가)만 본다.
// USI 문자는 shogi.Move.USI() 가 자기 표(shogi.typeLetters)로 만든다.
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

// ParseKIF·ParseCSA 는 **平手만 읽는다.** KIF의 手合割 도 CSA의 P 행도 안 보고 언제나 shogi.StartSFEN 에서 시작한다 —
// 駒落ち 기보를 넣으면 파싱이 아니라 아래 ValidateMove 에서 엉뚱한 手数에 터진다(下手：/上手： 를 받는 것과 무관하다).
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

		// 手数는 「수 줄인가」를 가리는 데만 쓰고 값은 안 믿는다 — 순서는 줄 순서가 정한다.
		// 그래서 KIF의 変化(분기) 블록은 헤더 줄만 걸러지고 그 아래 수들이 본선에 이어 붙어 ValidateMove 에서 터진다.
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
		// 投了는 **던지는 쪽의 手番**에 적힌다 — 여기서 Turn 이 先手면 先手가 던진 것이라 결과는 後手 승이다.
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

	// 成香·成桂·成銀 이 2글자라 2글자 조회가 먼저다. 뒤집으면 ４二成銀 이 「成 + 銀」으로 갈려 조회가 깨진다.
	// 銀成(승격 동작)은 표에 없어서 저절로 1글자 쪽으로 흘러간다 — 표에 2글자를 늘리면 이 대칭이 깨진다.
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

	// 아래 originRe 가 원위치 (XY)를 확정하므로 右左直寄引上 같은 수식어는 버려도 된다.
	// 다만 수식어가 成 앞에 오면(３三銀右成) 여기서 못 보고 승격이 조용히 빠진다 — 결과가 합법수라 ValidateMove 도 안 잡는다.
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
		// 投了와 시간초과는 같은 결과다 — 手番 쪽이 진다. %TIME_UP 을 받는 것은 실 코퍼스(journal §44의 341판)에 3판 있어서고,
		// 안 받으면 그 판이 Unknown 으로 떨어져 K 실측의 승패 표본에서 빠진다. %KACHI·%TSUMI·%ILLEGAL_* 는 아직 같은 이유로 안 받는다.
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
	// CSA는 착수 **후**의 駒를 적어(+8822UM) 표기에 成 이 없다 — 출발 칸에 서 있던 駒와 견줘 역산한다.
	// parseCSAMove 가 pos 를 받는 이유가 이 줄이고, 그래서 CSA는 앞에서부터 순서대로만 읽을 수 있다.
	boardPiece := pos.Board[from]
	promote := !boardPiece.Empty() && boardPiece.Type().CanPromote() && pt == boardPiece.Type().Promoted()

	return shogi.Move{From: int8(from), To: int8(to), Promote: promote}, nil
}
