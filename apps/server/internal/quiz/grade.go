package quiz

import (
	"errors"
	"fmt"
	"sort"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// ErrBadMove 는 낼 수 없는 수가 왔을 때다 — 불법이거나, 이미 끝난 문항에 수를 더 낸 것이다.
//
// **오답과 다르다.** 오답은 채점의 결과이고 이쪽은 요청이 틀린 것이다. 화면이 王手만
// 빛내므로 여기 오는 것은 프론트 버그이거나 조작된 요청이고, 둘을 같은 응답으로 뭉치면
// 버그가 오답으로 위장해 안 보인다(§53).
var ErrBadMove = errors.New("quiz: the move cannot be played here")

// MateOutcome 는 詰み 문항에서 마지막 수가 어떻게 되었나다.
type MateOutcome string

const (
	// MateOngoing 은 정답이고 아직 詰んでいない.
	MateOngoing MateOutcome = "ongoing"
	// MateSolved 는 詰ました. 문항이 끝난다.
	MateSolved MateOutcome = "solved"
	// MateWrong 은 오답이다. 문항이 끝나고 정답 수를 보여준다.
	MateWrong MateOutcome = "wrong"
	// MateNotCheck 는 王手가 아닌 수다. **오답으로 세지 않고 되돌린다** — 초심자의 첫
	// 본능이 대개 그것이고, 규약을 모른 채 오답 처리되는 것이 배움을 막는다.
	//
	// 화면이 王手만 빛내므로 정상 경로에서는 오지 않는다. 그래도 남겨 두는 것은 서버가
	// 스스로 판정할 수 있어야 하기 때문이다.
	MateNotCheck MateOutcome = "not_check"
)

// MateProgress 는 詰み 문항의 지금 상태다. 화면이 그리는 것 전부가 여기 있다.
type MateProgress struct {
	// Line 은 판 위에서 실제로 진행된 수다 — 사용자 수와 玉方 응수가 번갈아 들어 있다.
	Line []string
	// SFEN 은 지금 판이다.
	SFEN string
	// Defense 는 **직전 사용자 수에 玉方이 답한 수**다. 없으면 빈 값 — 화면이 그 수를
	// 짚어 줘야 사람이 무엇이 달라졌는지 안다.
	Defense string
	// Legal 은 지금 둘 수 있는 수 전부(=王手인 수)다. 문항이 끝났으면 비어 있다.
	Legal []string
	// Plies 는 지금 국면에서 詰みまでの手数다. 끝났으면 0.
	Plies int
	// Outcome 은 마지막 수의 결과다. 수를 하나도 안 냈으면 MateOngoing 이다.
	Outcome MateOutcome
	// Rest·Best 는 **오답일 때만** 채워진다.
	//
	// Rest 는 그 수 뒤에 남는 詰みまでの手数다. 0이면 詰み을 놓치는 수이고, 아니면 詰み이
	// 늘어지는 수다 — 문구가 여기서 갈린다.
	Rest int
	// Best 는 그 국면의 정답 수다.
	Best string
	// BestFrom 은 **Best 가 성립하는 국면**이고 `SFEN` 과 다를 수 있다.
	//
	// 오답이면 판이 그 수만큼 나아가 있어서, 거기서 Best 를 두어 보면 불법이라 棋譜 표기가
	// 통째로 사라진다 — 그러면 오답 문구에서 **「正解は○でした」가 빠진다.** 오답에 가장
	// 중요한 한 조각이 그것이다.
	BestFrom string
	// BestPrev 는 BestFrom 직전에 두어진 수다. 「同」 표기가 그 도착 칸을 본다. 없으면 빈 값.
	BestPrev string
}

// GradeMate 는 사용자가 낸 수들을 처음부터 되짚어 지금 상태를 만든다.
//
// **사용자의 수만 받는다.** 玉方의 응수는 트리에 있으므로 서버가 다시 만든다 — 그래서
// 화면과 서버가 어긋날 수 없고, 화면이 상태를 들고 있지 않아도 된다. 가정 수순이 상대의
// 수까지 함께 받는 것과 갈리는 자리이고(whatif.go), 갈리는 이유는 저쪽 응수가 엔진 탐색이라
// 같은 국면에서 같은 답을 준다는 보장이 없다는 것이다.
func GradeMate(item MateItem, moves []string) (MateProgress, error) {
	pos, err := shogi.ParseSFEN(item.SFEN)
	if err != nil {
		return MateProgress{}, fmt.Errorf("quiz: mate item sfen: %w", err)
	}

	out := MateProgress{Outcome: MateOngoing}
	for _, u := range moves {
		// 이미 끝난 문항에 수를 더 내는 것은 요청이 틀린 것이다.
		if out.Outcome != MateOngoing {
			return MateProgress{}, ErrBadMove
		}

		node, ok := item.Nodes[pos.RepetitionKey()]
		if !ok {
			// 트리에 없는 국면. 정답 수와 트리의 응수만 따라오면 여기 올 수 없다.
			return MateProgress{}, ErrBadMove
		}

		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			return MateProgress{}, ErrBadMove
		}
		if err := pos.ValidateMove(m); err != nil {
			return MateProgress{}, ErrBadMove
		}

		// **직전 수의 응수를 물려받지 않는다.** 여기서 안 지우면 王手가 아닌 수를 낸 응답이
		// 한 수 앞의 응수를 들고 나가고, 화면은 그것을 「방금 상대가 이렇게 받았다」로 그린다.
		out.Defense = ""

		// 이 노드의 국면과 그 앞의 수. **수를 두기 전에 잡아 둔다** — 오답이면 아래에서
		// 판이 한 수 나아가고, 정답 수의 표기는 그 뒤에서 못 만든다.
		nodeSFEN, nodePrev := pos.SFEN(), ""
		if n := len(out.Line); n > 0 {
			nodePrev = out.Line[n-1]
		}

		v, known := node.Moves[u]
		if !known {
			// 합법이지만 트리에 없다 = 王手가 아니다. 판을 **안 움직이고** 되돌린다.
			out.Outcome = MateNotCheck
			out.Best, out.BestFrom, out.BestPrev = node.Best, nodeSFEN, nodePrev
			break
		}

		pos = pos.Apply(m)
		out.Line = append(out.Line, u)

		switch {
		case v.Mated:
			out.Outcome = MateSolved
		case !v.Correct:
			out.Outcome = MateWrong
			out.Rest = v.Rest
			out.Best, out.BestFrom, out.BestPrev = node.Best, nodeSFEN, nodePrev
		default:
			d, err := shogi.ParseUSIMove(v.Defense)
			if err != nil {
				return MateProgress{}, fmt.Errorf("quiz: mate tree defense %q: %w", v.Defense, err)
			}
			pos = pos.Apply(d)
			out.Line = append(out.Line, v.Defense)
			out.Defense = v.Defense
		}
	}

	out.SFEN = pos.SFEN()

	// **문항이 아직 안 끝났으면 둘 수 있는 수를 준다.** 「王手가 아니다」도 여기 들어간다 —
	// 그때 판은 그대로이고 사람은 다시 둬야 하는데, 이 둘을 안 채워 보내면 화면이 문제
	// 국면으로 되돌아가서 **그때까지 맞힌 수가 사라진 것처럼 보인다.**
	if out.Outcome == MateOngoing || out.Outcome == MateNotCheck {
		node, ok := item.Nodes[pos.RepetitionKey()]
		if !ok {
			return MateProgress{}, fmt.Errorf("quiz: mate tree has no node for %s", out.SFEN)
		}
		out.Plies = node.Plies
		out.Legal = sortedKeys(node.Moves)
	}
	return out, nil
}

// GradeBest 는 「この局面の最善手は?」 한 문항을 채점한다. **엔진이 필요 없다** —
// 정답이 문항에 들어 있고, 채점은 그것과 견주는 일뿐이다.
func GradeBest(item BestItem, move string) (bool, error) {
	pos, err := shogi.ParseSFEN(item.SFEN)
	if err != nil {
		return false, fmt.Errorf("quiz: best item sfen: %w", err)
	}
	m, err := shogi.ParseUSIMove(move)
	if err != nil {
		return false, ErrBadMove
	}
	if err := pos.ValidateMove(m); err != nil {
		return false, ErrBadMove
	}
	return move == item.Answer, nil
}

// LegalMovesAt 은 그 국면의 합법수 전부다. 「최선수는?」 문항이 쓰는 자리라 **王手로 좁히지
// 않는다** — 그쪽은 詰み 문항만의 규약이다.
func LegalMovesAt(sfen string) ([]string, error) {
	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		return nil, fmt.Errorf("quiz: sfen: %w", err)
	}
	ms := pos.LegalMoves()
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.USI())
	}
	sort.Strings(out)
	return out, nil
}

// sortedKeys 는 map의 키를 정렬해 준다. **map 순회는 순서가 없어서** 그대로 내보내면 같은
// 국면이 열 때마다 다른 순서로 오고, 화면이 그 순서를 쓰는 날 조용히 갈린다.
func sortedKeys(m map[string]MateVerdict) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
