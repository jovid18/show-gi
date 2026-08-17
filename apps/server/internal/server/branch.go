package server

// 「가정 수순 한 걸음」의 계산부. **HTTP 핸들러의 것이 아니다** — 대국 화면(ws.go)과
// 되짚기 화면(whatif.go)이 같은 `whatifNodeOf` 를 부르고 뿌리를 얻는 곳만 다르다.

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/jovid18/show-gi/apps/server/internal/archive"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// whatifNodeOf 는 분기를 한 걸음 진행시킨다. **세션을 안 탄다** — 뿌리를 손에 들고 있는
// 채로 도는 함수라, 엔진 하나만 손으로 만들어 넣으면 전부 확인할 수 있다(cache는 nil로 둔다).
func whatifNodeOf(
	ctx context.Context,
	root whatifRoot,
	req whatifRequest,
	search Searcher,
	cache Cache,
) (whatifNode, error) {
	start, err := shogi.ParseSFEN(startSFENOf(root.StartSFEN))
	if err != nil {
		// 시작 국면을 못 읽으면 **한 수도 두지 않는다.** 평수 초기 국면으로 대신 두면
		// 한 번도 없었던 국면 위에서 가정을 세우게 된다(detailOf 와 같은 판단이다).
		return whatifNode{}, fmt.Errorf("%w: start sfen %q: %v", errWhatifPly, root.StartSFEN, err)
	}

	human := root.Human

	pos, prevTo, err := replayTo(start, root.Moves, req.Ply)
	if err != nil {
		return whatifNode{}, err
	}

	// 엔진에 보낼 수순. **뿌리까지의 실제 수순을 그대로 앞에 둔다** — 국면만 넘기면
	// 千日手를 세는 근거가 사라진다.
	line := make([]string, 0, req.Ply+len(req.Moves)+1)
	line = append(line, root.Moves[:req.Ply]...)

	node := whatifNode{
		BasePly:    req.Ply,
		Ply:        req.Ply,
		Status:     game.StatusPlaying,
		Line:       make([]whatifMove, 0, len(req.Moves)+1),
		Candidates: []whatifCandidate{},
	}

	for _, u := range req.Moves {
		mv, next, ok := step(pos, prevTo, node.Ply, u, human)
		if !ok {
			return whatifNode{}, fmt.Errorf("%w: %q", errWhatifMove, u)
		}
		node.Line = append(node.Line, mv)
		pos, prevTo, node.Ply = next, lastTo(u), node.Ply+1
		line = append(line, u)
	}

	node.SFEN = pos.SFEN()
	node.Checked = checkedSquare(pos)
	// SFEN·기록과 같은 한 글자를 쓴다(`games.my_color` 도 이것이다). `Color.String()` 은
	// `sente`/`gote` 라 여기 쓰면 화면이 세 번째 어휘를 갖는다.
	node.Turn = "b"
	if pos.Turn == shogi.White {
		node.Turn = "w"
	}
	node.YourTurn = pos.Turn == human

	legal := pos.LegalMoves()
	if len(legal) == 0 {
		// **千日手는 여기서 안 본다.** 그건 같은 국면이 네 번 나왔는가라서 수순 전체를
		// 세야 하는데, 분기는 실제 대국이 아니라 「둬 보는 것」이고 거기까지 가는 일이
		// 거의 없다. 없는 것을 절반만 세느니 안 센다.
		node.Status = game.StatusCheckmate
		if !pos.InCheck(pos.Turn) {
			node.Status = game.StatusStalemate
		}
		return node, nil
	}

	node.LegalMoves = make([]string, 0, len(legal))
	for _, m := range legal {
		node.LegalMoves = append(node.LegalMoves, m.USI())
	}

	// **탐색은 한 번이고, 이미 잰 국면이면 0번이다.** 이 하나가 세 가지를 준다 —
	// 이 국면의 값, 수번 쪽의 최선수(화면의 초록 화살표), 그리고 그 다음 후보들.
	cands, err := evalOf(ctx, search, cache, pos, start.SFEN(), line, min(whatifCandidates, len(legal)))
	if err != nil {
		return whatifNode{}, fmt.Errorf("%w: %w", errWhatifEngine, err)
	}
	if len(cands) == 0 {
		// 합법수가 있는데 후보가 하나도 없다. 엔진이 답을 준 적이 없다는 뜻이라,
		// 값을 지어내지 않고 「모른다」로 내보낸다.
		return node, nil
	}

	// 캐시의 cp는 수번 측 관점이다(store.Candidate). 여기서 뒤집는다 — 패키지 doc 참조.
	cp := playerCp(cands[0].Cp, pos.Turn, human)
	node.EvalCp = &cp
	node.MateIn = playerCp(cands[0].MateIn, pos.Turn, human)
	node.Candidates = candidatesOf(pos, prevTo, cands)
	return node, nil
}

// evalOf 는 이 국면의 상위 후보들이다. **캐시가 먼저다** — 조건이 둘이라 깊이와 **후보 수**를
// 함께 본다(journal §37). 감싼 층(internal/archive)도 캐시를 읽지만, 이 표면은 후보 셋을
// 약속하고 히트면 되짚어 만드는 일까지 건너뛴다 — 手数를 옮길 때마다 이 자리를 지난다.
func evalOf(
	ctx context.Context,
	search Searcher,
	cache Cache,
	pos shogi.Position,
	startSFEN string,
	line []string,
	want int,
) ([]store.Candidate, error) {
	if cache != nil {
		p, err := cache.GetPosition(ctx, archive.Key(pos))
		switch {
		case err == nil && p.ComputedDepth >= whatifDepth && len(p.Candidates) >= want:
			return p.Candidates, nil
		case err != nil && !errors.Is(err, store.ErrNoPosition):
			// 캐시가 고장 나도 탐색은 된다. 조용히 넘기지 않고 다시 잰다.
			log.Printf("whatif: read cache: %v", err)
		}
	}

	res, err := search.SearchMultiPV(ctx, startSFEN, line, whatifDepth, whatifCandidates)
	if err != nil {
		return nil, err
	}
	// **여기서 쓰지 않는다.** 탐색을 감싼 쪽이 이미 남겼다(internal/archive) — 두 자리에서
	// 쓰면 한 자리가 빠지거나 두 벌이 어긋난다.
	return archive.Candidates(res), nil
}

// playerCp 는 수번 측 값을 플레이어 관점으로 옮긴다(패키지 doc의 규약).
func playerCp(moverCp int, turn, human shogi.Color) int {
	if turn == human {
		return moverCp
	}
	return -moverCp
}

// candidatesOf 는 탐색의 후보들을 화면이 그릴 수 있는 모양으로 옮긴다.
//
// **여기서도 엔진 출력을 검증한다.** 못 두는 수가 하나 섞이면 그 줄만 버린다 —
// 화면에서 「이렇게 뒀어야 한다」는 단언이라 틀린 것을 그리느니 적게 그린다.
func candidatesOf(pos shogi.Position, prevTo int, cands []store.Candidate) []whatifCandidate {
	out := make([]whatifCandidate, 0, whatifCandidates)

	for _, l := range cands {
		if len(out) == whatifCandidates {
			break
		}
		m, err := shogi.ParseUSIMove(l.USI)
		if err != nil || pos.ValidateMove(m) != nil {
			continue
		}
		c := whatifCandidate{USI: l.USI, Ja: pos.MoveJa(m, prevTo), EvalCp: l.Cp, MateIn: l.MateIn}
		// 낙폭은 **최선수 대비**다. 화면이 뺄셈을 하지 않는다 — 두 값을 나란히 두면
		// 어느 쪽이 기준인지가 흐려진다.
		//
		// **詰み이 한쪽에라도 있으면 안 적는다.** 그 줄의 cp는 환산값(±MateCp)이라 뺄셈이
		// 29000 같은 수를 내놓고, 그것은 낙폭이 아니라 자가 다른 두 값의 차다.
		if len(out) > 0 && out[0].MateIn == 0 && c.MateIn == 0 {
			c.LossCp = out[0].EvalCp - c.EvalCp
		}
		out = append(out, c)
	}
	return out
}

// replayTo 는 정본 수순을 ply 手目까지 다시 둔다. 두 번째 값은 그 手数의 도착 칸이다
// (「同」 표기가 본다).
//
// **범위를 넘으면 거절한다.** 뿌리가 구멍에서 끊겨 있으면(rootOf) 그 뒤의 手数가 여기서
// 「기록 밖」으로 걸린다.
func replayTo(start shogi.Position, moves []string, ply int) (shogi.Position, int, error) {
	if ply < 0 || ply > len(moves) {
		return start, -1, fmt.Errorf("%w: ply %d is not in the first %d moves", errWhatifPly, ply, len(moves))
	}
	pos, prevTo := start, -1
	for _, u := range moves[:ply] {
		next, _, ok := advance(pos, prevTo, u)
		if !ok {
			return pos, prevTo, fmt.Errorf("%w: cannot replay %s", errWhatifPly, u)
		}
		pos, prevTo = next, lastTo(u)
	}
	return pos, prevTo, nil
}

// step 은 한 수를 두어 본다. 못 두는 수면 ok=false — **사람의 수도 엔진의 수도 여기를 지난다.**
func step(pos shogi.Position, prevTo, ply int, u string, human shogi.Color) (whatifMove, shogi.Position, bool) {
	m, err := shogi.ParseUSIMove(u)
	if err != nil {
		return whatifMove{}, pos, false
	}
	if err := pos.ValidateMove(m); err != nil {
		return whatifMove{}, pos, false
	}
	by := game.SideEngine
	if pos.Turn == human {
		by = game.SideHuman
	}
	// 표기는 **두기 전** 국면에서 나온다. 두고 나면 그 駒가 이미 도착 칸에 서 있어서
	// 어느 駒가 갔는지 구별이 안 된다.
	ja := pos.MoveJa(m, prevTo)
	next := pos.Apply(m)
	return whatifMove{
		Ply:     ply + 1,
		USI:     u,
		Ja:      ja,
		By:      by,
		SFEN:    next.SFEN(),
		Checked: checkedSquare(next),
	}, next, true
}
