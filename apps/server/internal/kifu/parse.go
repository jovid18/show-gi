// Package kifu 는 실 기보(KIF·KI2·CSA·USI)를 읽어 프로덕션과 같은 경로로 다시 둔다.
//
// 부르는 데가 둘이다. cmd/importkifu 는 태그가 남의 대국에서도 맞게 붙는지를 넓게 보고
// (journal §44) 상수를 기록으로 다시 채점한다(§39). 서버는 사람이 자기 기보를 취해 올 때
// 부른다(§126) — 어느 쪽이든 game.NamedTesuji · intervene.Judge 를 그대로 지난다.
// 여기서 지름길을 내면 재는 대상이 프로덕션이 아니게 된다.
//
// 네트워크도 비밀도 모른다. 읽을 수 없는 형식을 옮겨 주는 계층은 밖에 있고
// (internal/kifunorm) 그쪽이 낸 것도 결국 이 파서를 지나야 수가 된다.
//
// 기보 파일은 레포에 커밋하지 않는다(남의 대국 기록이고 하루치가 5MB 넘는다).
// 받는 법과 env 게이트는 apps/server/README.md.
package kifu

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"

	"github.com/jovid18/show-gi/apps/server/internal/handicap"
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

// parseFile 은 목적칸의 筋 숫자다. 전각(２)이 KIF 의 것이고 반각(2)은 사람이 쓴 평문과
// shogi.SquareJa 가 내는 모양이다 — 둘 다 받아야 이 패키지가 자기 렌더러의 출력을 다시 읽는다.
func parseFile(r rune) (int, bool) {
	switch {
	case r >= '１' && r <= '９':
		return int(r-'１') + 1, true
	case r >= '1' && r <= '9':
		return int(r-'1') + 1, true
	}
	return 0, false
}

// headerValue 는 「先手：名前」 같은 헤더 줄에서 값을 떼어 낸다.
//
// 콜론이 두 가지다. 전각(：)이 KIF 의 것이지만 반각(:)으로 쓰는 도구가 있고, 어느 쪽이든
// 못 읽으면 그 줄이 통째로 없는 것이 된다.
func headerValue(line, key string) (string, bool) {
	if !strings.HasPrefix(line, key) {
		return "", false
	}
	rest := line[len(key):]
	for _, sep := range []string{"：", ":"} {
		if v, ok := strings.CutPrefix(rest, sep); ok {
			return strings.TrimSpace(v), true
		}
	}
	return "", false
}

// headerName 은 이름이 여럿인 헤더를 읽는다 — 先手 와 下手 가 같은 자리다.
func headerName(line string, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := headerValue(line, k); ok {
			return v, true
		}
	}
	return "", false
}

// startOf 는 手合割 이름으로 0手目를 만든다. 이름을 모르면 실패한다 — 모르는 手合을
// 平手로 읽으면 첫 수부터 반칙이 되고, 그 오류가 手合 때문이라는 것을 아무도 못 본다.
func startOf(name string) (shogi.Position, string, error) {
	if name == "" || name == "平手" {
		return shogi.StartPosition(), shogi.StartSFEN, nil
	}
	h, ok := handicap.FindByName(name)
	if !ok {
		return shogi.Position{}, "", fmt.Errorf("unsupported handicap: %s", name)
	}
	pos, err := shogi.ParseSFEN(h.SFEN)
	if err != nil {
		return shogi.Position{}, "", fmt.Errorf("handicap %s: %w", name, err)
	}
	return pos, h.SFEN, nil
}

// ── KIF ─────────────────────────────────────────────────────

var kifMoveRe = regexp.MustCompile(`^\s*(\d+)\s+`)
var originRe = regexp.MustCompile(`\((\d)(\d)\)`)
var timeRe = regexp.MustCompile(`\(\s*\d+:\d+[^)]*\)\s*$`)

// ParseKIF 는 手数와 원위치가 적힌 표기를 읽는다 — 「1 ７六歩(77)」.
//
// 手合割 줄이 시작 국면을 정한다. 표에 없는 手合은 실패다(startOf).
//
// ParseCSA 는 아직 平手만 읽는다. P 행을 안 보고 언제나 shogi.StartSFEN 에서 시작해서,
// 駒落ち CSA 는 파싱이 아니라 ValidateMove 에서 엉뚱한 手数에 터진다.
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
		// 駒落ち는 下手/上手로 적는다. 下手가 先手 자리다 — 접어 준 上手가 먼저 두지만
		// 판 위의 색은 그대로다(internal/handicap 의 SFEN 주석).
		if v, ok := headerName(line, "先手", "下手"); ok {
			g.Sente = v
			continue
		}
		if v, ok := headerName(line, "後手", "上手"); ok {
			g.Gote = v
			continue
		}
		// 手合割은 시작 국면을 정한다. 이 줄을 안 읽으면 駒落ち 기보가 平手 위에서 읽히다가
		// 엉뚱해 보이는 반칙으로 죽는다.
		//
		// 수를 하나라도 읽은 뒤에 오면 무시한다 — 그때 국면을 갈아 끼우면 앞의 수들이
		// 다른 판의 것이 된다.
		if name, ok := headerValue(line, "手合割"); ok {
			if len(g.Moves) > 0 {
				continue
			}
			p, sfen, err := startOf(name)
			if err != nil {
				return g, err
			}
			g.StartSFEN, pos = sfen, p
			continue
		}
		if strings.HasPrefix(line, "手数") {
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

		// 판이 끝난 사유는 수가 아니다. 投了가 던지는 쪽의 手番에 적히는 규약까지 endOf 가 든다.
		if end, ok := endOf(rest); ok {
			g.Result = end(pos.Turn)
			continue
		}

		m, err := parseKIFMove(rest, pos, prevTo)
		if err != nil {
			return g, &MoveError{Ply: len(g.Moves) + 1, Text: rest, Err: err}
		}
		if err := pos.ValidateMove(m); err != nil {
			return g, &MoveError{Ply: len(g.Moves) + 1, Text: rest, Err: err}
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
		f, ok := parseFile(runes[idx])
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

	// 수식어(右左直寄引上)를 먼저 걷는다. 成 을 먼저 보면 「３三銀右成」에서 승격이 조용히
	// 빠지고, 결과가 합법수라 ValidateMove 도 안 잡는다.
	//
	// 걷은 것을 버리지 않는다 — 원위치가 안 적힌 표기에서는 이것이 출발칸을 정하는 유일한 단서다.
	modStart := idx
	for idx < len(runes) && shogi.IsOriginModifier(runes[idx]) {
		idx++
	}
	mods := string(runes[modStart:idx])

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
		return dropAt(pt, to)
	}

	// 원위치가 적혀 있으면 그것이 답이다. KIF 가 그쪽이다.
	remaining := string(runes[idx:])
	if om := originRe.FindStringSubmatch(remaining); om != nil {
		fromFile := int(om[1][0] - '0')
		fromRank := int(om[2][0] - '0')
		// (00) 은 投入을 적는 또 하나의 방식이다. 出発칸이 없다는 뜻이라 打 와 같다.
		if fromFile == 0 && fromRank == 0 {
			return dropAt(pt, to)
		}
		from := shogi.SquareOf(fromFile, fromRank)
		return shogi.Move{From: int8(from), To: int8(to), Promote: promote}, nil
	}

	// 안 적혀 있으면 룰 엔진이 되찾는다. KI2 와 사람이 쓴 평문이 그쪽이다.
	from, err := pos.ResolveOrigin(pt, to, mods)
	if err != nil {
		// 打 를 안 적은 投入이다. KI2 는 반상의 같은 駒가 그 칸에 못 갈 때 打 를 생략한다.
		if d, dropErr := dropAt(pt, to); dropErr == nil && pos.ValidateMove(d) == nil {
			return d, nil
		}
		return shogi.Move{}, err
	}
	return shogi.Move{From: int8(from), To: int8(to), Promote: promote}, nil
}

// dropAt 은 投入 수를 만든다. 玉과 成한 駒는 持ち駒로 못 든다 — 「と打」같은 표기는 없다.
func dropAt(pt shogi.PieceType, to int) (shogi.Move, error) {
	if _, ok := dropLetter[pt]; !ok {
		return shogi.Move{}, fmt.Errorf("cannot drop %v", pt)
	}
	return shogi.Move{From: -1, To: int8(to), Drop: pt}, nil
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
				return g, &MoveError{Ply: len(g.Moves) + 1, Text: line, Err: err}
			}
			if err := pos.ValidateMove(m); err != nil {
				return g, &MoveError{Ply: len(g.Moves) + 1, Text: line, Err: err}
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
	// CSA는 착수 후의 駒를 적어(+8822UM) 표기에 成 이 없다 — 출발 칸에 서 있던 駒와 견줘 역산한다.
	// parseCSAMove 가 pos 를 받는 이유가 이 줄이고, 그래서 CSA는 앞에서부터 순서대로만 읽을 수 있다.
	boardPiece := pos.Board[from]
	promote := !boardPiece.Empty() && boardPiece.Type().CanPromote() && pt == boardPiece.Type().Promoted()

	return shogi.Move{From: int8(from), To: int8(to), Promote: promote}, nil
}
