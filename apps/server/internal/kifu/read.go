package kifu

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// Notation 은 무엇으로 읽었는가다. games.imported_from 에 그대로 들어가고, 그 칸이
// 「취해 온 기보인가」와 「LLM 이 손댔는가」를 같이 말한다(020_imported_games.sql).
type Notation string

const (
	NotationKIF Notation = "kif"
	NotationKI2 Notation = "ki2"
	NotationCSA Notation = "csa"
	NotationUSI Notation = "usi"
	// NotationPlain 은 표식도 手数도 없이 표기만 늘어놓은 것이다 — 블로그나 채팅에서
	// 복사해 온 모양이고, 사람이 붙여 넣는 것 중 가장 흔한 「형식이 아닌 형식」이다.
	NotationPlain Notation = "plain"
	// NotationLLM 은 결정적 파서가 전부 실패해 정규화 계층을 지난 판이다
	// (internal/kifunorm). **Read 는 이 값을 안 준다** — 옮겨 적힌 뒤 다시 이 패키지를
	// 지나므로(ParseMoves) 그 판도 수는 룰 엔진이 만든 것이지만, 표기를 누가 손댔는지는
	// 기록에 남아야 한다.
	NotationLLM Notation = "llm"
)

// MoveError 는 몇 手目에서 못 읽었는가다.
//
// 번호를 들고 다니는 이유는 화면이다. 「読み取れませんでした」만으로는 사람이 자기
// 기보의 어디를 고쳐야 하는지 모르고, 문자열에서 번호를 다시 뽑는 코드는 오류 문구를
// 고치는 날 조용히 낡는다.
type MoveError struct {
	Ply  int
	Text string
	Err  error
}

func (e *MoveError) Error() string {
	if e.Text == "" {
		return fmt.Sprintf("move %d: %v", e.Ply, e.Err)
	}
	return fmt.Sprintf("move %d (%s): %v", e.Ply, e.Text, e.Err)
}

func (e *MoveError) Unwrap() error { return e.Err }

// ErrNoMoves 는 형식은 알아봤는데 수가 하나도 없는 자리다.
var ErrNoMoves = errors.New("no moves")

// readers 의 순서가 곧 시도 순서다. **좁은 형식이 먼저다** — USI 는 나머지가 흉내 낼 수
// 없는 모양이고, 평문은 가장 느슨해서(공백으로 끊은 표기면 다 본다) 마지막이라야 남의
// 형식을 가로채지 않는다.
var readers = []struct {
	name Notation
	read func(string) (ParsedGame, error)
}{
	{NotationUSI, ParseUSI},
	{NotationCSA, ParseCSA},
	{NotationKIF, ParseKIF},
	{NotationKI2, ParseKI2},
	{NotationPlain, ParsePlain},
}

// Read 는 결정적 파서들을 차례로 대 보고 처음 읽히는 것을 준다.
//
// **여기가 성공하면 LLM 은 안 부른다.** 같은 기보가 언제나 같은 결과를 주는 것이
// 기본값이고, 정규화 계층은 그 기본값이 안 되는 자리에만 선다(internal/kifunorm).
//
// 수를 하나라도 읽은 뒤에 깨진 형식은 그 자리에서 답이 된다. 그 오류가 「이 기보는
// 98手目가 이상하다」라서, 뒤의 파서가 0手로 실패한 오류보다 사람에게 값이 크다.
func Read(text string) (ParsedGame, Notation, error) {
	for _, r := range readers {
		g, err := r.read(text)
		if len(g.Moves) == 0 {
			continue
		}
		return g, r.name, err
	}
	return ParsedGame{}, "", ErrNoMoves
}

// ── USI ─────────────────────────────────────────────────────

// usiMoveRe 는 USI 수 하나다. 반상 이동(7g7f·2b3c+)과 投入(P*5e) 둘.
var usiMoveRe = regexp.MustCompile(`^(?:[1-9][a-i][1-9][a-i]\+?|[PLNSGBR]\*[1-9][a-i])$`)

// ParseUSI 는 USI 수순을 읽는다. 엔진과 도구가 내는 가장 흔한 기계 출력이다.
//
// 받는 모양이 셋이다 — "position startpos moves ...", "position sfen <4칸> moves ...",
// 그리고 수만 공백으로 이어진 것. 앞의 둘은 접두어를 떼고 같은 자리로 흘려보낸다.
//
// 수가 아닌 낱말은 건너뛰지 않고 실패한다. USI 는 사람이 쓰는 표기가 아니라서 「모르는
// 낱말」이 곧 「이 텍스트는 USI 가 아니다」이고, 건너뛰면 남의 기보를 반쯤 읽는다.
func ParseUSI(input string) (ParsedGame, error) {
	g := ParsedGame{StartSFEN: shogi.StartSFEN}
	fields := strings.Fields(input)

	// "position" 다음은 startpos 하나이거나 sfen + 칸 네 개다. 그 뒤의 "moves" 부터 수다.
	if len(fields) > 0 && fields[0] == "position" {
		fields = fields[1:]
		switch {
		case len(fields) > 0 && fields[0] == "startpos":
			fields = fields[1:]
		case len(fields) >= 5 && fields[0] == "sfen":
			g.StartSFEN = strings.Join(fields[1:5], " ")
			fields = fields[5:]
		default:
			return g, fmt.Errorf("position: expected startpos or sfen")
		}
		if len(fields) > 0 && fields[0] == "moves" {
			fields = fields[1:]
		}
	} else if len(fields) > 0 && fields[0] == "startpos" {
		fields = fields[1:]
		if len(fields) > 0 && fields[0] == "moves" {
			fields = fields[1:]
		}
	}

	pos, err := shogi.ParseSFEN(g.StartSFEN)
	if err != nil {
		return ParsedGame{}, fmt.Errorf("start sfen: %w", err)
	}

	for _, f := range fields {
		if !usiMoveRe.MatchString(f) {
			return g, &MoveError{Ply: len(g.Moves) + 1, Text: f, Err: errors.New("not a USI move")}
		}
		m, err := shogi.ParseUSIMove(f)
		if err != nil {
			return g, &MoveError{Ply: len(g.Moves) + 1, Text: f, Err: err}
		}
		if err := pos.ValidateMove(m); err != nil {
			return g, &MoveError{Ply: len(g.Moves) + 1, Text: f, Err: err}
		}
		g.Moves = append(g.Moves, m.USI())
		pos = pos.Apply(m)
	}
	return g, nil
}

// ── KI2 ─────────────────────────────────────────────────────

// ki2MoveRe 는 표식으로 시작하는 낱말 하나다. 전각 공백(U+3000)은 \s 가 아니라서
// 「▲同　銀」이 한 낱말로 잡힌다 — KIF 가 그 자리에 넣는 공백이고 parseKIFMove 가 읽는다.
var ki2MoveRe = regexp.MustCompile(`[▲△▼▽]([^▲△▼▽\s]+)`)

// ParseKI2 는 원위치를 안 적는 표기를 읽는다 — 「▲７六歩 △３四歩」.
//
// 手数도 원위치도 없다. 순서는 낱말 순서가, 출발칸은 룰 엔진이 정한다
// (shogi.Position.ResolveOrigin) — 수식어로도 안 좁혀지면 실패하고 고르지 않는다.
//
// 표식이 手番과 어긋나면 실패한다. 수가 빠졌거나 分岐가 섞인 자리이고, 그대로 읽으면
// 남은 수순이 통째로 다른 판이 되는데 합법수라 ValidateMove 는 안 잡는다.
func ParseKI2(input string) (ParsedGame, error) {
	g := ParsedGame{StartSFEN: shogi.StartSFEN}
	pos := shogi.StartPosition()
	prevTo := -1

	for line := range strings.Lines(input) {
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "*") {
			continue
		}
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
		if v, ok := headerName(line, "先手", "下手"); ok {
			g.Sente = v
			continue
		}
		if v, ok := headerName(line, "後手", "上手"); ok {
			g.Gote = v
			continue
		}
		// 分岐는 안 읽는다. 본선에 이어 붙이면 그 뒤가 통째로 다른 판이 된다.
		if strings.HasPrefix(line, "変化") {
			break
		}

		for _, m := range ki2MoveRe.FindAllStringSubmatch(line, -1) {
			mark, text := []rune(m[0])[0], m[1]
			if end, ok := endOf(text); ok {
				g.Result = end(pos.Turn)
				continue
			}
			// ▲가 先手다. 駒落ち도 같다 — 판 위의 색이 手合割에 안 흔들린다.
			want := shogi.Black
			if mark == '△' || mark == '▽' {
				want = shogi.White
			}
			if want != pos.Turn {
				return g, &MoveError{Ply: len(g.Moves) + 1, Text: m[0], Err: errors.New("the mark is not the side to move")}
			}
			mv, err := parseKIFMove(text, pos, prevTo)
			if err != nil {
				return g, &MoveError{Ply: len(g.Moves) + 1, Text: m[0], Err: err}
			}
			if err := pos.ValidateMove(mv); err != nil {
				return g, &MoveError{Ply: len(g.Moves) + 1, Text: m[0], Err: err}
			}
			g.Moves = append(g.Moves, mv.USI())
			prevTo = int(mv.To)
			pos = pos.Apply(mv)
		}
	}
	return g, nil
}

// endOf 는 수가 아니라 판이 끝난 사유인 낱말을 가른다.
//
// 답을 手番의 함수로 준다. 投了는 던지는 쪽의 手番에 적히므로 그 자리에서 진 사람이
// 누구인지가 手番으로 정해진다(ParseKIF 의 같은 판단).
func endOf(text string) (func(shogi.Color) GameResult, bool) {
	switch {
	case strings.HasPrefix(text, "投了"), strings.HasPrefix(text, "詰み"), strings.HasPrefix(text, "切れ負け"):
		return func(turn shogi.Color) GameResult {
			if turn == shogi.Black {
				return ResultGoteWin
			}
			return ResultSenteWin
		}, true
	case strings.HasPrefix(text, "千日手"), strings.HasPrefix(text, "持将棋"):
		return func(shogi.Color) GameResult { return ResultDraw }, true
	case strings.HasPrefix(text, "中断"):
		return func(shogi.Color) GameResult { return ResultUnknown }, true
	}
	return nil, false
}

// ParsePlain 은 공백으로 떨어진 표기만 있는 텍스트를 읽는다 — 「７六歩 ３四歩 ２六歩」.
//
// 낱말 하나라도 못 읽으면 실패한다. 건너뛰면 아무 산문에서나 수처럼 생긴 조각을 주워
// 반쯤 읽은 기보를 만든다.
//
// 마지막에 대 보는 파서다. 가장 느슨해서 먼저 두면 남의 형식을 가로챈다.
func ParsePlain(input string) (ParsedGame, error) {
	return ParseMoves("", strings.Fields(input))
}

// ── 표기 낱말의 목록 ────────────────────────────────────────

// ParseMoves 는 手 하나씩 떨어진 표기 목록을 읽는다. 정규화 계층이 내는 모양이다
// (internal/kifunorm).
//
// **여기가 정규화 계층의 출력이 수가 되는 유일한 문이다.** 낱말 하나하나가 룰 엔진을
// 지나므로, 옮겨 적는 쪽이 지어낸 것은 여기서 걸린다.
//
// 手番은 국면이 든다 — 표식(▲△)이 없어도 되고, 붙어 있으면 떼고 읽는다. 手가 하나 빠지면
// 다음 낱말이 상대의 駒를 움직이는 것이 되어 대개 ValidateMove 가 잡는다.
func ParseMoves(handicapName string, moves []string) (ParsedGame, error) {
	pos, sfen, err := startOf(handicapName)
	if err != nil {
		return ParsedGame{}, err
	}
	g := ParsedGame{StartSFEN: sfen}
	prevTo := -1

	for _, raw := range moves {
		text := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(raw), "▲△▼▽"))
		if text == "" {
			continue
		}
		// 판이 끝난 사유가 낱말로 섞여 올 수 있다. 수가 아니므로 결과로만 받는다.
		if end, ok := endOf(text); ok {
			g.Result = end(pos.Turn)
			continue
		}

		m, err := oneMove(text, pos, prevTo)
		if err != nil {
			return g, &MoveError{Ply: len(g.Moves) + 1, Text: raw, Err: err}
		}
		if err := pos.ValidateMove(m); err != nil {
			return g, &MoveError{Ply: len(g.Moves) + 1, Text: raw, Err: err}
		}
		g.Moves = append(g.Moves, m.USI())
		prevTo = int(m.To)
		pos = pos.Apply(m)
	}
	return g, nil
}

// oneMove 는 낱말 하나를 수로 만든다. USI 와 일본어 표기가 섞여 올 수 있다 —
// 옮겨 적는 쪽이 원문의 표기를 그대로 두기 때문이다.
func oneMove(text string, pos shogi.Position, prevTo int) (shogi.Move, error) {
	if usiMoveRe.MatchString(text) {
		return shogi.ParseUSIMove(text)
	}
	return parseKIFMove(text, pos, prevTo)
}
