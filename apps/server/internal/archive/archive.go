// Package archive 는 **모든 탐색을 데이터로 만든다.**
//
// 이 레포는 엔진을 **다섯 자리**에서 부른다 — 상대의 수, 개입 판정, 대국 중 가정 수순,
// 되짚는 판의 가정 수순, 手筋 제안 힌트. 다섯 곳에 기록 코드를 흩뿌리면 **반드시 하나가
// 빠지고**, 빠진 것은 「데이터가 왜 이것만 있지」로 한참 뒤에 나타난다. 그래서 기록을 탐색
// 그 자체에 붙인다 — `usi.Pool` 을 한 겹 감싸고, 부르는 쪽은 감싼 줄도 모른다.
//
// **기준이 같아야 재활용이 된다.** 깊이는 다섯 자리가 모두 12이고(game.DefaultDepth ·
// JudgeDepth · whatifDepth), 갈리는 것은 MultiPV뿐이다 — 그건 「같은 깊이면 후보가 많은
// 쪽이 이긴다」로 질의가 정리한다(query/positions.sql).
//
// 무엇이 어디에 남는지는 `positions`·`edges` 의 DDL(001_init.sql)과 02-architecture.md §4.
package archive

import (
	"context"
	"errors"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// writeTimeout 은 기록 한 건에 주는 시간이다.
//
// **대국을 기다리게 하지 않는다.** 기록은 탐색이 끝난 뒤 따로 도는 goroutine이고,
// 요청 ctx를 물려받지 않는다 — 물려받으면 사람이 다음 수를 두는 순간(개입 회차가
// 끝나 ctx가 닫히는 순간) 방금 잰 분석이 그대로 버려진다.
const writeTimeout = 5 * time.Second

// Store 는 분석이 쌓이고 **다시 꺼내지는** 곳. `*store.Store` 가 만족한다.
type Store interface {
	GetPosition(ctx context.Context, sfenKey string) (store.Position, error)
	Edges(ctx context.Context, parentKey string) ([]store.Edge, error)
	PutPosition(ctx context.Context, p store.Position) (bool, error)
	PutEdge(ctx context.Context, e store.Edge) error
}

// Engine 은 감싸는 대상이다. `*usi.Pool` 이 만족한다.
type Engine interface {
	SearchMultiPV(ctx context.Context, startSFEN string, moves []string, depth, multiPV int) (usi.SearchResult, error)
}

// Searcher 는 탐색을 그대로 넘기고 결과를 남긴다.
//
// `game.MultiSearcher` · `game.Searcher` · `server.Searcher` 를 한꺼번에 만족한다 —
// 세 인터페이스가 같은 하나를 받아야 「다섯 자리 중 하나가 안 감싸졌다」가 생기지 않는다.
type Searcher struct {
	inner Engine
	store Store

	// wg 는 떠 있는 기록들이다. 종료할 때 이것만 기다리면 방금 잰 분석이 안 버려진다.
	wg sync.WaitGroup
}

// Wrap 은 탐색에 기록을 붙인다. st 가 nil이면 아무것도 안 쌓고 그대로 넘긴다 —
// DB가 없어도 대국은 된다는 이 레포의 판단과 같은 자리다.
func Wrap(inner Engine, st Store) *Searcher {
	return &Searcher{inner: inner, store: st}
}

// SearchDepth 는 후보 하나짜리 탐색이다. `game.Searcher` 가 이 모양을 요구한다.
func (a *Searcher) SearchDepth(ctx context.Context, startSFEN string, moves []string, depth int) (usi.SearchResult, error) {
	return a.SearchMultiPV(ctx, startSFEN, moves, depth, 1)
}

func (a *Searcher) SearchMultiPV(
	ctx context.Context,
	startSFEN string,
	moves []string,
	depth, multiPV int,
) (usi.SearchResult, error) {
	// **이미 잰 국면이면 엔진을 안 부른다.** 여기가 §12의 캐시를 실제로 쓰는 자리다 —
	// 상대의 수는 k=10으로 2초쯤 걸리고, 사람의 수를 판정하는 「착수 전」 탐색은 방금
	// 그 상대가 이미 잰 그 국면이다.
	if a.store != nil {
		if pos, err := positionAfter(startSFEN, moves); err == nil {
			if hit, ok := a.lookup(ctx, pos, depth, multiPV); ok {
				// **히트에도 「이 국면에 오게 한 수」는 남긴다.** 국면은 이미 있어도 그
				// 국면으로 **오는 길**은 새것일 수 있다(전치가 그것이다) — 안 남기면 그
				// 간선이 영원히 비어 있고, A→B를 쌓는다는 말이 반만 사실이 된다.
				line := slices.Clone(moves)
				a.wg.Add(1)
				go func() {
					defer a.wg.Done()
					a.recordPath(startSFEN, line, hit)
				}()
				return hit, nil
			}
		}
	}

	res, err := a.inner.SearchMultiPV(ctx, startSFEN, moves, depth, multiPV)
	if err != nil || a.store == nil {
		return res, err
	}
	// **부르는 쪽이 준 슬라이스를 들고 가지 않는다.** 대국 루프는 수를 계속 덧붙이므로
	// 그 배열이 기록 도중에 바뀐다 — 롤백이 있으면 줄어들기까지 한다.
	line := slices.Clone(moves)
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.record(startSFEN, line, res)
	}()
	return res, nil
}

// lookup 은 이미 잰 국면을 탐색 결과의 모양으로 되돌린다. 못 쓰면 ok=false.
//
// **되돌릴 것이 둘이다** — 후보 목록(`Lines`)은 `positions`, 부르는 쪽이 보는 깊이별
// 값(`History`)은 `edges.eval_by_depth` 다. 쓰는 조건도 둘이라 **깊이와 후보 수를 둘 다**
// 넘어야 한다. 어느 하나를 빠뜨렸을 때 무엇이 조용히 사라지는지는 journal §37.
func (a *Searcher) lookup(ctx context.Context, pos shogi.Position, depth, multiPV int) (usi.SearchResult, bool) {
	key := Key(pos)
	p, err := a.store.GetPosition(ctx, key)
	if err != nil {
		if !errors.Is(err, store.ErrNoPosition) {
			log.Printf("archive: read position: %v", err)
		}
		return usi.SearchResult{}, false
	}
	if p.ComputedDepth < depth || len(p.Candidates) < wanted(pos, multiPV) {
		return usi.SearchResult{}, false
	}

	// 깊이별 값은 수마다 다른 행에 있다. 한 번에 읽어 수로 묶는다.
	byMove := map[string][]int{}
	if edges, err := a.store.Edges(ctx, key); err == nil {
		for _, e := range edges {
			if len(e.EvalByDepth) > 0 {
				byMove[e.USI] = e.EvalByDepth
			}
		}
	} else {
		log.Printf("archive: read edges: %v", err)
	}

	res := usi.SearchResult{Depth: p.ComputedDepth}
	for i, c := range p.Candidates {
		if i == 0 {
			res.Best, res.ScoreCp = c.USI, c.Cp
			res.IsMate, res.MateIn = c.MateIn != 0, c.MateIn
			res.PV = c.PV
		}
		line := usi.SearchLine{
			Depth: p.ComputedDepth, MultiPV: i + 1, Move: c.USI, ScoreCp: c.Cp,
			IsMate: c.MateIn != 0, MateIn: c.MateIn, PV: c.PV,
		}
		res.Lines = append(res.Lines, line)

		// 저장은 先手 관점이고 탐색 결과는 **수번 관점**이다. 되돌리는 것을 빠뜨리면
		// 後手로 잡은 판에서만 부호가 뒤집히고, 에러는 안 난다.
		for d, cp := range byMove[c.USI] {
			res.History = append(res.History, usi.SearchLine{
				Depth: d + 1, MultiPV: i + 1, Move: c.USI, ScoreCp: senteCp(cp, pos.Turn),
			})
		}
	}
	if res.Best == "" {
		return usi.SearchResult{}, false
	}
	return res, true
}

// wanted 는 그 국면에서 **실제로 있을 수 있는** 후보 수다.
//
// **합법수가 k보다 적으면 후보도 k개가 안 된다.** 그걸 「모자란다」로 보면 그 자리는
// 영원히 캐시를 못 쓰고 매번 다시 잰다(journal §37). 합법수를 세는 것은 룰 엔진
// 몫이라 엔진을 안 부른다.
func wanted(pos shogi.Position, multiPV int) int {
	if multiPV <= 1 {
		return multiPV
	}
	return min(multiPV, len(pos.LegalMoves()))
}

// positionAfter 는 시작 국면에서 그 수순을 둔 국면이다. 못 두면 에러다 —
// **그 위에 데이터를 쌓거나 꺼내면 없던 국면을 다루게 된다.**
func positionAfter(startSFEN string, moves []string) (shogi.Position, error) {
	pos, err := shogi.ParseSFEN(startSFEN)
	if err != nil {
		return pos, err
	}
	for _, u := range moves {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			return pos, err
		}
		if err := pos.ValidateMove(m); err != nil {
			return pos, err
		}
		pos = pos.Apply(m)
	}
	return pos, nil
}

// Wait 은 떠 있는 기록이 끝날 때까지 기다린다. 종료 순서에서 부른다.
func (a *Searcher) Wait() { a.wg.Wait() }

// recordPath 는 **그 국면에 오게 한 수**만 남긴다. 캐시가 답한 자리에서 쓴다 —
// 국면과 후보는 이미 있고, 새것일 수 있는 것은 오는 길뿐이다.
func (a *Searcher) recordPath(startSFEN string, moves []string, res usi.SearchResult) {
	if len(moves) == 0 {
		return // 뿌리다. 오게 한 수가 없다
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	parent, err := positionAfter(startSFEN, moves[:len(moves)-1])
	if err != nil {
		log.Printf("archive: replay for path: %v", err)
		return
	}
	m, err := shogi.ParseUSIMove(moves[len(moves)-1])
	if err != nil {
		return
	}
	child := parent.Apply(m)
	moverMoves, otherMoves := splitMovesBySide(startSFEN, moves, parent.Turn)
	a.link(ctx, parent, len(moves)-1, moves[len(moves)-1], child, Key(child), Candidates(res), moverMoves, otherMoves)
}

// record 는 탐색 하나를 데이터로 옮긴다.
//
// **실패해도 대국에 영향이 없다.** 여기서 나는 에러는 전부 로그로 끝난다 — 분석을
// 남기지 못한 것과 대국이 서지 않는 것의 값이 다르다.
func (a *Searcher) record(startSFEN string, moves []string, res usi.SearchResult) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	pos, err := shogi.ParseSFEN(startSFEN)
	if err != nil {
		log.Printf("archive: start sfen %q: %v", startSFEN, err)
		return
	}
	// 마지막 수 직전 국면. **이 국면에 오게 한 수**의 부모라, 도착 국면과 이름이 여기서 붙는다.
	var parent *shogi.Position
	var blackMoves, whiteMoves []string
	for i, u := range moves {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			log.Printf("archive: cannot read %q: %v", u, err)
			return
		}
		if err := pos.ValidateMove(m); err != nil {
			// 여기 오는 수는 이미 룰 엔진을 지난 것이다. 어긋났으면 우리 버그이고,
			// 그 위에 데이터를 쌓으면 **없던 국면**이 캐시에 들어간다.
			log.Printf("archive: %q is not legal at move %d: %v", u, i+1, err)
			return
		}
		if pos.Turn == shogi.Black {
			blackMoves = append(blackMoves, u)
		} else {
			whiteMoves = append(whiteMoves, u)
		}
		before := pos
		parent = &before
		pos = pos.Apply(m)
	}

	key := Key(pos)
	cands := Candidates(res)

	// ① 국면. **후보와 도달 깊이가 함께 간다** — 깊이만 맞고 후보가 얕은 행은 다음
	// 호출자가 못 쓴다(질의가 그래서 후보 수를 견준다).
	if _, err := a.store.PutPosition(ctx, store.Position{
		SFENKey:       key,
		SideToMove:    sideOf(pos.Turn),
		PlyHint:       len(moves),
		Candidates:    cands,
		ComputedDepth: res.Depth,
	}); err != nil {
		log.Printf("archive: put position: %v", err)
		return // 자식 국면이 없으면 아래의 간선이 FK에 걸린다
	}

	// ② 후보마다의 **깊이별 평가치**. 추가 탐색이 없다 — `PvInterval=0` 덕에 depth N
	// 탐색 한 번이 1..N을 전부 돌려준다. 얕은 값과 깊은 값이 갈리는 자리가 곧 함정이고,
	// 그게 개입 판정의 입력이다(01-core.md §3).
	for _, c := range cands {
		byDepth := res.EvalByDepth(c.USI)
		if len(byDepth) == 0 {
			continue
		}
		cps := make([]int, 0, len(byDepth))
		for _, d := range byDepth {
			cps = append(cps, senteCp(d.Cp, pos.Turn))
		}
		if err := a.store.PutEdge(ctx, store.Edge{ParentKey: key, USI: c.USI, EvalByDepth: cps}); err != nil {
			log.Printf("archive: put edge %s %s: %v", key, c.USI, err)
			return
		}
	}

	// ③ 이 국면에 **오게 한 수**. 도착 국면과 「그 수가 만든 이름」이 여기서 붙는다.
	if parent != nil {
		moverMoves, otherMoves := sideMoves(blackMoves, whiteMoves, parent.Turn)
		a.link(ctx, *parent, len(moves)-1, moves[len(moves)-1], pos, key, cands, moverMoves, otherMoves)
	}
}

// link 는 부모 국면에서 이 국면으로 온 한 수를 남긴다.
//
// **이름은 여기서만 붙는다**(namesFor). 판단에 부모와 자식이 둘 다 필요한데 그 둘을
// 한꺼번에 손에 든 자리가 여기뿐이다.
func (a *Searcher) link(
	ctx context.Context,
	parent shogi.Position,
	parentPly int,
	usiMove string,
	child shogi.Position,
	childKey string,
	childCands []store.Candidate,
	moverMoves, otherMoves []string,
) {
	parentKey := Key(parent)

	// 부모 행이 없으면 FK가 간선을 거절한다. 후보를 모르는 채로 **자리만** 만들어 둔다 —
	// `computed_depth=0` 이라 나중에 실제로 잰 값이 그대로 덮는다.
	if _, err := a.store.PutPosition(ctx, store.Position{
		SFENKey:    parentKey,
		SideToMove: sideOf(parent.Turn),
		PlyHint:    parentPly,
	}); err != nil {
		log.Printf("archive: put parent position: %v", err)
		return
	}

	edge := store.Edge{ParentKey: parentKey, USI: usiMove, ChildKey: childKey}

	if names := a.namesFor(ctx, parent, usiMove, child, childCands, moverMoves, otherMoves); len(names) > 0 {
		edge.Tags = names
	}
	if err := a.store.PutEdge(ctx, edge); err != nil {
		log.Printf("archive: link %s %s: %v", parentKey, usiMove, err)
	}
}

// namesFor 는 그 수가 **새로 만든** 囲い·전법·戦型·手筋의 코드다.
//
// 앞뒤를 견준다. 「지금 판에 서 있는 형태 전부」로 하면 한 번 껐던 이름이 두 수 뒤에
// 통과하는데(journal §34 ⑦), 저장은 그 실수를 영구히 남긴다.
func (a *Searcher) namesFor(
	ctx context.Context,
	parent shogi.Position,
	usiMove string,
	child shogi.Position,
	childCands []store.Candidate,
	moverMoves, otherMoves []string,
) []string {
	mover := parent.Turn

	// 手筋은 엔진이 값을 인정한 것만이다. 부모의 평가치를 캐시에서 꺼내 온다 —
	// 없으면 이 축은 통째로 건너뛴다.
	var names []string
	if len(childCands) > 0 {
		if p, err := a.store.GetPosition(ctx, Key(parent)); err == nil && len(p.Candidates) > 0 {
			// 부모의 값은 부모의 수번(=둔 쪽) 관점, 자식의 값은 상대 관점이다.
			before := senteCp(p.Candidates[0].Cp, mover)
			after := senteCp(childCands[0].Cp, child.Turn)
			for _, t := range game.NamedTesuji(parent, child, mover, usiMove, before, after) {
				names = append(names, t.Code)
			}
		} else if err != nil && !errors.Is(err, store.ErrNoPosition) {
			log.Printf("archive: read parent for names: %v", err)
		}
	}

	// 囲い·전법·戦型은 엔진을 안 본다. **형태가 성립했는가**뿐이라 판만 있으면 나온다.
	// 전법은 수순이 있어야 나온다 — 수순 없이 부르면 飛를 振った 것을 모른다.
	var prevMoverMoves []string
	if len(moverMoves) > 0 {
		prevMoverMoves = moverMoves[:len(moverMoves)-1]
	}

	had := map[string]bool{}
	for _, t := range tag.Detect(tag.Input{
		Pos: parent, Color: mover,
		PlayerMoves: prevMoverMoves, OpponentMoves: otherMoves,
	}) {
		had[t.Code] = true
	}
	for _, t := range tag.Detect(tag.Input{
		Pos: child, Color: mover,
		PlayerMoves: moverMoves, OpponentMoves: otherMoves,
	}) {
		if !had[t.Code] {
			names = append(names, t.Code)
		}
	}
	return names
}

func sideMoves(black, white []string, mover shogi.Color) (moverMoves, otherMoves []string) {
	if mover == shogi.Black {
		return black, white
	}
	return white, black
}

func splitMovesBySide(startSFEN string, moves []string, mover shogi.Color) (moverMoves, otherMoves []string) {
	pos, err := shogi.ParseSFEN(startSFEN)
	if err != nil {
		return nil, nil
	}
	for _, u := range moves {
		if pos.Turn == mover {
			moverMoves = append(moverMoves, u)
		} else {
			otherMoves = append(otherMoves, u)
		}
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			return nil, nil
		}
		pos = pos.Apply(m)
	}
	return
}

// Key 는 국면 하나를 가리키는 키다. **정의는 `shogi.PositionKey` 에 있다** — `internal/game`
// 도 같은 자를 써야 하는데 그쪽이 이 패키지를 못 들여오므로(store 가 딸려 온다), 정의를
// 한 단계 아래로 내렸다. 이 이름은 부르는 쪽이 이미 넷이라 남겨 둔다.
func Key(pos shogi.Position) string { return shogi.PositionKey(pos) }

// Candidates 는 탐색 결과를 캐시에 넣을 모양으로 옮긴다. **순위 순이고 cp는 수번 관점**이다.
//
// 공개해 둔 것은 **캐시에서 꺼낸 것과 방금 잰 것이 같은 모양이어야** 하기 때문이다
// (`internal/server/whatif.go`). 두 모양이 갈리면 히트율에 따라 나타나는 버그가 된다.
//
// **순서는 `usi.SearchResult.Ranked` 가 정한다.** 여기서 한 벌 더 세면 개입 문장이 보는
// 1위와 이 목록의 1위가 갈릴 수 있다(그 함수의 doc).
func Candidates(res usi.SearchResult) []store.Candidate {
	ranked := res.Ranked()
	out := make([]store.Candidate, 0, len(ranked))
	for _, l := range ranked {
		c := store.Candidate{USI: l.Move, Cp: l.ScoreCp, PV: l.PV}
		if l.IsMate {
			c.MateIn = l.MateIn
		}
		out = append(out, c)
	}
	return out
}

// senteCp 는 **수번 측 관점** cp를 先手 관점으로 옮긴다. `edges.eval_by_depth` 의 규약이고
// (001_init.sql) `game.senteCp` 와 같은 연산이다 — 색이 다른 두 판을 나란히 놓기 위한 것이다.
func senteCp(moverCp int, mover shogi.Color) int {
	if mover == shogi.Black {
		return moverCp
	}
	return -moverCp
}

func sideOf(c shogi.Color) string {
	if c == shogi.Black {
		return "b"
	}
	return "w"
}
