package game

import (
	"context"
	"fmt"
	"log"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// MateSearcher 는 詰み 탐색이다. **탐색부와 다른 바이너리**라 따로 받는다
// (docs/02-architecture.md §3). nil이면 종반 판정 없이 승률 낙폭만 본다.
type MateSearcher interface {
	SearchMate(ctx context.Context, startSFEN string, moves []string) (usi.MateResult, error)
}

type engineAnalyst struct {
	search Searcher
	mate   MateSearcher
	depth  int
	level  intervene.Level
}

// JudgeDepth 는 개입 판정에 쓰는 탐색 깊이다.
//
// 실측(06-status.md)에서 `depth 10 × k=1`이 최장 144ms, 12가 400ms다. 판정은 정밀도보다
// 속도가 중요하고 **착수 직후에 도는 유일한 탐색**이라 여기를 짧게 잡는다.
const JudgeDepth = 12

// ShallowDepth 는 **초보자의 시야를 모사하는** 깊이다.
//
// 「얕은 이득에 낚임」은 여기서 좋아 보이는데 JudgeDepth 에서 나쁜 수다(01-core.md §3).
// 2인 이유는 捨て駒가 얕게 보면 반드시 손해로 보이기 때문이다 — 그게 捨て駒의 정의이고,
// 그 거울상이 「한 수만 보면 이득」이다.
const ShallowDepth = 2

// NewEngineAnalyst 는 엔진으로 판정하는 Analyst 를 만든다.
//
// mate 가 nil이면 종반 판정을 건너뛴다 — 승률 낙폭은 그대로 돌므로 대국은 된다.
func NewEngineAnalyst(s Searcher, mate MateSearcher, level intervene.Level) Analyst {
	return &engineAnalyst{search: s, mate: mate, depth: JudgeDepth, level: level}
}

func (a *engineAnalyst) Judge(ctx context.Context, startSFEN string, moves []string, _ int) (Judgement, error) {
	if len(moves) == 0 {
		return Judgement{}, fmt.Errorf("judge: no move to judge")
	}
	before := moves[:len(moves)-1]

	// 착수 **전** 국면의 최선수. 두는 쪽(=사람) 관점이다.
	best, err := a.search.SearchDepth(ctx, startSFEN, before, a.depth)
	if err != nil {
		return Judgement{}, fmt.Errorf("judge: search before: %w", err)
	}

	// 착수 **후** 국면. 엔진은 늘 수번 측 관점으로 답하므로 지금은 상대 관점이다.
	after, err := a.search.SearchDepth(ctx, startSFEN, moves, a.depth)
	if err != nil {
		return Judgement{}, fmt.Errorf("judge: search after: %w", err)
	}

	in := intervene.Input{
		BestCp:  best.ScoreCp,
		AfterCp: -after.ScoreCp, // 사람 관점으로 뒤집는다
		Level:   a.level,
	}

	// 카테고리에 쓸 국면 사실. **판정 자체는 여기에 매이지 않는다** — 못 읽으면
	// Known 이 false로 남고 카테고리만 other 가 된다. 개입은 그대로 걸린다.
	// 둔 쪽의 색. 평가치를 先手 관점으로 옮기는 데 쓴다 — 판을 못 읽으면 안 적는다.
	mover, moverKnown := shogi.Black, false
	// 설명에 쓸 사실. 판정용과 **같은 자리에서 한 번에** 나온다(moveFacts).
	var facts explain.Facts

	if pos, m, err := replay(startSFEN, moves); err == nil {
		mover, moverKnown = pos.Turn, true
		in.Features, facts = moveFacts(pos, m)
		in.Features.UnpromotedOnly = UnpromotedOnly(m, best.Best)
		// 얕은 평가는 **이미 받아 둔 info 라인**에 있다. PvInterval=0 덕에 depth 12
		// 탐색 한 번이 depth 1~12를 전부 돌려주므로 추가 탐색이 없다(01-core.md §4).
		if cp, ok := after.ScoreAtDepth(ShallowDepth); ok {
			in.Features.ShallowCp, in.Features.HasShallow = -cp, true // 사람 관점
		}
		facts.Tags = detectTags(pos.Apply(m), mover, startSFEN, moves)
	} else {
		// 판을 못 읽은 것은 우리 버그다. 판정은 계속하되 조용히 넘기지 않는다.
		log.Printf("game: could not replay for features, category will be other: %v", err)
	}

	if a.mate != nil {
		// 착수 **전**에 내가 詰み을 가지고 있었는가. solver 는 수번 측(=사람)의 詰み을
		// 모든 응수에 대해 증명하므로 이 질문에 정확히 답한다.
		if r, err := a.mate.SearchMate(ctx, startSFEN, before); err == nil && r.Found() {
			in.MateBefore = len(r.Moves)
		}
	}

	// 착수 **후**에도 남았는가는 solver 로 물으면 안 된다 — 그 국면의 수번은 상대라
	// `go mate` 가 「상대의 詰み」을 답한다. 내가 알아야 하는 것과 반대다.
	//
	// 대신 이미 구해둔 탐색 결과를 쓴다. 착수 후 국면이 **수번 측에게 불리한 mate**로
	// 나오면(MateIn < 0) 그것이 곧 「상대가 詰まされる」 = 내 詰み이 남았다는 뜻이다.
	if in.MateBefore > 0 && after.IsMate && after.MateIn < 0 {
		in.MateAfter = -after.MateIn
	}

	// **반대 부호가 반대 카테고리다.** `MateIn > 0` 은 착수 후 국면의 수번(=상대)이 詰ます
	// 쪽이라는 뜻이므로 「상대가 나를 詰ます」다. 위의 `MateIn < 0` 과 같은 값에서 갈리는
	// 두 질문이고, 아래쪽 답을 여기서 **버리고 있었다** — 그래서 종반 개입의 절반이
	// 이유 없이 `other` 로 갔다(06-status.md §40).
	mateLine := a.opponentMate(ctx, startSFEN, moves, before, best.Best, after)
	in.Features.OpponentMatePlies = len(mateLine)

	v := intervene.Judge(in)
	j := Judgement{Verdict: v, BestUSI: best.Best, Threshold: a.level.Threshold()}

	// **판정에 쓴 두 탐색이 그대로 기보의 평가치가 된다.** 추가 탐색이 없다.
	// 앞쪽은 착수 **전** 국면이라 그것이 곧 **직전 상대 수 뒤**의 평가치다 —
	// 상대가 둘 때는 그 값을 아는 코드가 없으므로 여기서 한 수 늦게 채워진다.
	if moverKnown {
		j.SenteCpBefore = senteCp(best.ScoreCp, mover)
		j.SenteCpAfter = senteCp(-after.ScoreCp, mover) // after 는 상대 관점이다
		j.HasEvals = true
	}
	if v.Kind != intervene.KindNone {
		// 「상대는 이렇게 벌한다」의 수순이고, **출처가 셋이다.**
		//
		// 기본은 이미 손에 든 착수 후 탐색의 PV다 — 공짜이고 분류도 필요 없어서, 카테고리가
		// 이유를 못 대는 3분의 2(06-status.md §17)가 여기서 설명을 갖는다.
		//
		// **詰まされる 국면은 증명된 詰み 수순을 쓴다.** PV는 깊이 12에서의 읽기라 뒤로 갈수록
		// 확실하지 않은데, 詰み 수순은 모든 응수에 대해 증명된 것이라 **끝까지 참이다.**
		//
		// **`other` 는 카드와 같은 질문을 다시 던진다**(cardPV). 그 카테고리만 문장에 수를
		// 적으므로, 거기서만 「화면이 한 국면의 최선수를 둘로 말한다」가 성립한다(§58).
		pv, full := after.PV, false
		switch {
		case len(mateLine) > 0:
			pv, full = mateLine, true
		case v.Category == intervene.CategoryOther && facts.Known:
			// **문장이 수를 적는 카테고리가 이것뿐이다**(explain.Facts.used). 그래서 여기만
			// 카드와 같은 질문의 답을 쓴다 — 이유를 이름으로 대는 카테고리는 수를 안 적으므로
			// 갈릴 자리가 없고, 탐색을 하나 더 걸 이유도 없다.
			//
			// **`Known` 을 보는 것도 같은 이유다.** 판을 못 읽으면 카테고리가 `other` 로
			// 떨어지는데(intervene.classify) 그때는 `used` 가 이 수를 지워서 문장에 안 나간다 —
			// 말하지 않을 것을 위해 탐색을 걸지 않는다.
			if top := a.cardPV(ctx, startSFEN, moves); len(top) > 0 {
				pv = top
			}
		}
		r := refutationLine(startSFEN, moves, pv, RefutationPlies, full)
		j.RetractedSFEN, j.RetractedChecks = r.retractedSFEN, r.checks
		if full {
			// **화면에 나가는 수순은 증명된 詰み뿐이다.** PV를 잘라 내보내던 자리인데,
			// 어디서 자를지가 국면마다 달라(trimRefutation) 두 수에서 끊기기도 했고
			// 읽는 사람이 「그래서 뭐」로 남았다. 그 자리는 이제 후보 셋을 직접 둬 보는
			// 쪽이 맡는다(06-status.md §54). 詰み 수순만 남기는 것은 그것이 **끝까지
			// 참**이기 때문이다 — 자를 필요가 없어서 애매한 자리가 아예 없다.
			j.Refutation = r.line
		}

		// 설명이 쓸 사실을 여기서 닫는다. **판정이 끝난 뒤여야 한다** — 무엇을 말해도
		// 되는지가 카테고리에 달려 있고(explain.Facts.used), 카테고리는 방금 정해졌다.
		facts.Kind, facts.Category, facts.Level, facts.LostMate = v.Kind, v.Category, a.level, v.LostMate
		facts.Threatened = r.threatened
		facts.MatePlies = in.Features.OpponentMatePlies
		if v.Category == intervene.CategoryOther {
			// **위에서 정한 그 PV다.** `after.PV` 를 여기서 다시 읽으면 문장의 첫 수와
			// 「무엇을 취할 수 있는가」(r.threatened)가 서로 다른 수의 것이 된다.
			facts.OpponentBest, facts.Branches = a.otherBranches(ctx, startSFEN, moves, pv)
		}
		j.Facts = facts
	}
	return j, nil
}

// opponentMate 는 **그 수가 상대에게 詰み을 줬는가**이고, 줬으면 그 수순을 돌려준다(len 이 手数).
// ②「최선수 뒤에는 詰み이 없다」가 없으면 이미 詰んでいた 국면에 그 수의 죄를 씌운다 — 아직
// 시험된 적이 없어서 남긴다(06-status.md §40 ③). 비용과 게이트 적중은 같은 절 ⑥.
func (a *engineAnalyst) opponentMate(
	ctx context.Context, startSFEN string, moves, before []string, bestUSI string, after usi.SearchResult,
) []string {
	if a.mate == nil || bestUSI == "" {
		return nil
	}
	// 게이트. 이 값은 위에서 이미 구한 탐색의 것이라 여기서 엔진을 부르지 않는다.
	if !after.IsMate || after.MateIn <= 0 {
		return nil
	}

	// ① 둔 수 뒤. 이 국면의 수번은 상대이므로 `go mate` 가 **상대의 詰み**을 답한다 —
	// 위쪽 `MateBefore` 가 같은 호출을 반대 국면에 쓰는 것과 정확히 대칭이다.
	played, err := a.mate.SearchMate(ctx, startSFEN, moves)
	if err != nil || !played.Found() {
		return nil
	}

	// ② 최선수 뒤. 여기서도 詰まされる면 이미 진 국면이라 그 수의 죄가 아니다.
	bestLine := append(append([]string(nil), before...), bestUSI)
	if b, err := a.mate.SearchMate(ctx, startSFEN, bestLine); err != nil || b.Found() {
		// **탐색이 실패해도 안 붙인다.** ②를 확인하지 못한 채 붙이면 그 문구가 거짓일 수
		// 있고, 모를 때는 말하지 않는 쪽이 이 제품의 규칙이다.
		return nil
	}
	return played.Moves
}

// OtherBranches 는 `other` 설명이 펼치는 갈래의 수다. **화면의 후보 목록과 같은 셋**이라,
// 문장과 목록이 같은 것을 말한다 — 그래서 `cardPV` 도 이 k로 묻는다(server.whatifCandidates).
const OtherBranches = 3

// cardPV 는 물러진 수 뒤 국면의 **정본 PV**다. 못 구하면 nil.
//
// **개입 카드가 후보 목록을 얻는 것과 같은 질문이다** — 같은 국면·같은 깊이·k=OtherBranches.
// 판정이 손에 든 착수 후 탐색은 k=1이고, **같은 국면·같은 깊이라도 k가 다르면 1위가 갈린다**
// (06-status.md §34 ②). 그 PV를 문장에 쓰고 있었던 것이 화면이 한 국면의 최선수를 둘로 말한
// 이유 전부다 — 얼마나 자주 갈리는지는 §58의 실측 표.
//
// **엔진을 부르는 총 횟수는 그대로다.** 여기서 거는 탐색이 곧 화면이 물을 그 탐색이고, 결과가
// `positions` 에 남아 카드의 요청이 캐시에서 답한다(internal/archive · server.evalOf). 늘어난
// 것은 **카드가 뜨기 전에 도는 몫**이다.
//
// **판정 자체는 안 건드린다** — 착수 직후에 도는 유일한 탐색을 무겁게 하면 개입이 안 걸린
// 수까지 느려진다(JudgeDepth).
func (a *engineAnalyst) cardPV(ctx context.Context, startSFEN string, moves []string) []string {
	multi, ok := a.search.(MultiSearcher)
	if !ok {
		return nil
	}
	res, err := multi.SearchMultiPV(ctx, startSFEN, moves, a.depth, OtherBranches)
	if err != nil {
		// **못 물었으면 k=1 PV로 돌아간다.** 그때는 카드의 요청도 같은 이유로 튕겨 목록이
		// 아예 안 서므로(server.whatifNodeOf), 화면에 모순이 남지는 않는다.
		log.Printf("game: could not read the card position, the sentence falls back to k=1: %v", err)
		return nil
	}
	ranked := res.Ranked()
	if len(ranked) == 0 || len(ranked[0].PV) == 0 {
		return nil
	}
	return ranked[0].PV
}

// otherBranches 는 「그 수를 두면 이렇게 된다」를 세 갈래로 만든다. 첫 값은 상대의 최선수다.
//
// `other` 는 **이유를 못 대는 자리**이고, 그때 남는 정직한 설명이 「그래서 어떻게 되는가」
// 하나다. 상대의 최선수는 **주어진 pv의 첫 수**이고 — 부르는 쪽이 카드와 같은 질문으로
// 구해다 준다(cardPV) — 여기서 하는 탐색은 **A+B 국면의 MultiPV 한 번뿐**이다. 그 한 번이
// 내 후보 셋과 각 줄의 PV(=상대의 응수)와 결말 cp를 함께 준다.
//
// **못 구하면 그 갈래를 안 준다.** 반쪽짜리 갈래는 문장에서 곧 거짓이 되고, 여기 없는 것을
// 설명 계층이 지어내지 못한다는 것이 이 구조의 요점이다(explain 패키지 doc).
func (a *engineAnalyst) otherBranches(
	ctx context.Context, startSFEN string, moves, pv []string,
) (string, []explain.Branch) {
	multi, ok := a.search.(MultiSearcher)
	if !ok || len(pv) == 0 || len(moves) == 0 {
		return "", nil
	}
	pos, err := positionAfter(startSFEN, moves)
	if err != nil {
		return "", nil
	}
	last, err := shogi.ParseUSIMove(moves[len(moves)-1])
	if err != nil {
		return "", nil
	}

	// 상대의 최선수. **엔진 출력을 룰 엔진으로 검증한다** — refutationLine 과 같은 자리다.
	reply, err := shogi.ParseUSIMove(pv[0])
	if err != nil || pos.ValidateMove(reply) != nil {
		return "", nil
	}
	bestJa := pos.MoveJa(reply, int(last.To))
	mine := pos.Apply(reply)
	prevTo := int(reply.To)

	line := append(append([]string(nil), moves...), pv[0])
	res, err := multi.SearchMultiPV(ctx, startSFEN, line, a.depth, OtherBranches)
	if err != nil {
		return bestJa, nil
	}

	// **점수는 이 국면의 수번 관점**이고, 그 수번은 사람이다(A가 사람의 수이고 B가 상대의
	// 응수다). 그래서 뒤집지 않는다 — 뒤집으면 문장의 부호가 통째로 거짓말이 된다.
	out := make([]explain.Branch, 0, OtherBranches)
	for _, l := range res.Lines {
		if len(out) == OtherBranches {
			break
		}
		// 상대의 응수까지 있어야 「그래서 어떻게 되는가」가 닫힌다. 결말 cp만 적으면
		// 무엇 때문에 그렇게 되는지가 빠진다.
		if len(l.PV) < 2 {
			continue
		}
		mv, err := shogi.ParseUSIMove(l.PV[0])
		if err != nil || mine.ValidateMove(mv) != nil {
			continue
		}
		b := explain.Branch{PlayerJa: mine.MoveJa(mv, prevTo)}
		if l.IsMate {
			b.MateIn = l.MateIn
		} else {
			b.Cp = l.ScoreCp
		}

		next := mine.Apply(mv)
		back, err := shogi.ParseUSIMove(l.PV[1])
		if err != nil || next.ValidateMove(back) != nil {
			continue
		}
		b.ReplyJa = next.MoveJa(back, int(mv.To))
		out = append(out, b)
	}
	return bestJa, out
}

// detectTags 는 착수 후 국면의 囲い·전법·戦型을 감지해 태그 코드 배열로 돌려준다.
// kb_chunks 검색 키가 된다.
func detectTags(afterPos shogi.Position, mover shogi.Color, startSFEN string, moves []string) []string {
	pm, om := splitMoves(startSFEN, moves, mover)
	tags := tag.Detect(tag.Input{
		Pos:           afterPos,
		Color:         mover,
		PlayerMoves:   pm,
		OpponentMoves: om,
	})
	if len(tags) == 0 {
		return nil
	}
	codes := make([]string, len(tags))
	for i, t := range tags {
		codes[i] = t.Code
	}
	return codes
}

// splitMoves 는 수순을 플레이어와 상대의 것으로 나눈다.
func splitMoves(startSFEN string, moves []string, player shogi.Color) (playerMoves, opponentMoves []string) {
	startPos, err := shogi.ParseSFEN(startSFEN)
	if err != nil {
		return nil, nil
	}
	turn := startPos.Turn
	for _, m := range moves {
		if turn == player {
			playerMoves = append(playerMoves, m)
		} else {
			opponentMoves = append(opponentMoves, m)
		}
		turn = turn.Other()
	}
	return playerMoves, opponentMoves
}

// senteCp 는 **둔 쪽 관점** cp를 先手 관점으로 옮긴다.
//
// 저장 관점을 先手로 고정하는 것은 `edges.eval_by_depth` 와 같은 규약이다
// (02-architecture.md §4). 대국마다 사람의 색이 달라지므로 「플레이어 관점」으로 적으면
// 색이 다른 두 판을 나란히 못 놓는다.
func senteCp(moverCp int, mover shogi.Color) int {
	if mover == shogi.Black {
		return moverCp
	}
	return -moverCp
}

// RefutationPlies 는 반박 수순의 **상한**이다. 실제 길이는 국면이 정한다(trimRefutation).
//
// 깊이 12 탐색의 PV는 뒤로 갈수록 확실하지 않고, 화면에서는 「왜 나쁜가」가 아니라
// 강의가 된다. 여기는 그 두 가지를 막는 한도이고, 보통은 이보다 훨씬 앞에서 잘린다.
const RefutationPlies = 8

// refutationLine 은 착수 후 PV를 棋譜 표기·국면이 붙은 수순으로 옮긴다. 첫 값은 물러진 수 직후.
//
// **엔진 출력을 믿지 않는다** — 각 수를 룰 엔진으로 검증하고 못 두는 수에서 끊는다. 표기·국면을
// 서버가 만드는 이유는 06-status.md §6 ④.
//
// **`full`(증명된 詰み)이면 자르지 않는다.** 그때 `limit` 은 안 본다 — 상한은 solver 의
// `DepthLimit` 이 이미 걸었다.
func refutationLine(startSFEN string, moves []string, pv []string, limit int, full bool) refutation {
	if full {
		limit = len(pv)
	}
	if len(pv) == 0 || limit <= 0 {
		return refutation{}
	}

	pos, err := positionAfter(startSFEN, moves)
	if err != nil {
		log.Printf("game: could not replay for refutation: %v", err)
		return refutation{}
	}
	out := refutation{retractedSFEN: pos.SFEN(), checks: checkLines(pos)}
	last, err := shogi.ParseUSIMove(moves[len(moves)-1])
	if err != nil {
		return refutation{}
	}
	// 물러진 수의 도착 칸. 벌하는 수는 대개 그 자리를 되따는 수라 「同」이 여기서 나온다.
	prevTo := int(last.To)

	// 판정하는 수는 늘 사람의 수이므로, 그 다음은 어느 색을 잡았든 상대다.
	by := SideEngine

	line := make([]RefutationMove, 0, limit)
	var steps []refutationStep // 자르는 자리를 여기서 찾는다
	for _, u := range pv {
		if len(line) == limit {
			break
		}
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			break
		}
		if err := pos.ValidateMove(m); err != nil {
			break
		}
		step := refutationStep{captureSq: -1}
		if !m.IsDrop() && !pos.Board[m.To].Empty() {
			step.captureSq = int(m.To)
			// **첫 수가 따는 수면 그것이 「무엇을 잃는가」다.** 첫 수는 언제나 상대의
			// 수이고(판정하는 것은 늘 사람의 수다), 거기서 따이는 것은 내 駒다.
			// 두 번째 수부터는 내 되따기가 섞여 「내가 무엇을 잃는가」가 아니게 된다.
			if len(line) == 0 {
				out.threatened = shogi.PieceJa(pos.Board[m.To].Type())
			}
		}
		mv := RefutationMove{USI: u, Ja: pos.MoveJa(m, prevTo), By: by}

		pos = pos.Apply(m)
		prevTo = int(m.To)
		mv.SFEN = pos.SFEN()
		mv.Checks = checkLines(pos)
		line = append(line, mv)

		step.gaveCheck = len(mv.Checks) > 0
		step.settles = step.captureSq >= 0 || step.gaveCheck
		steps = append(steps, step)
		if by == SideEngine {
			by = SideHuman
		} else {
			by = SideEngine
		}
	}
	if len(line) == 0 {
		return refutation{}
	}
	// 증명된 詰み 수순은 그대로 간다. `steps` 는 여기서 안 쓰인다 — 자를 자리를 찾는
	// 값이고, 자르지 않기로 한 자리다.
	if full {
		out.line = line
		return out
	}
	out.line = line[:trimRefutation(steps)]
	return out
}

// refutation 은 반박 수순 하나와, 그것을 그리고 설명하는 데 필요한 것들이다.
// **PV를 한 번 재생하며 공짜로 얻는 것들**이라 묶었다 — 하나를 위해 다시 재생하면 둘이 어긋난다.
type refutation struct {
	// retractedSFEN 은 물러진 수를 둔 직후의 국면. 수순을 넘겨 볼 때의 첫 장면이다.
	retractedSFEN string
	// checks 는 그 국면에서 玉을 잡으러 오는 말들.
	checks []Attack
	// line 은 「상대는 이렇게 벌한다」.
	line []RefutationMove
	// threatened 는 그 수순의 **첫 수로 상대가 딸 수 있는 내 駒**다. 없으면 빈 값.
	threatened string
}

// checkLines 는 지금 수번인 쪽의 玉을 잡으러 오는 말들을 판 위의 선으로 옮긴다.
//
// **「王手다」와 「누가 걸고 있는가」는 다른 질문이다.** 앞은 국면만 봐도 알지만 뒤는
// 규칙을 알아야 하고, 그건 클라이언트가 갖지 않기로 한 것이다(D2). 両王手가 여기서 두 줄로
// 나오고, 그 둘이 곧 「먹어서 풀 수 없다」의 이유다 — 실제로 그 물음이 나왔다(§20).
func checkLines(pos shogi.Position) []Attack {
	king := pos.KingSquare(pos.Turn)
	if king < 0 {
		return nil
	}
	from := pos.Attackers(king, pos.Turn.Other())
	if len(from) == 0 {
		return nil
	}
	out := make([]Attack, 0, len(from))
	to := shogi.SquareUSI(king)
	for _, sq := range from {
		out = append(out, Attack{From: shogi.SquareUSI(sq), To: to})
	}
	return out
}

// refutationStep 은 반박 수순의 한 수에서 **자를 자리를 정하는 데 필요한 사실**이다.
type refutationStep struct {
	// settles 는 그 수에서 손익이 바뀌는가 — 駒를 따거나 王手를 건다.
	settles bool
	// captureSq 는 딴 칸. 안 땄으면 -1. **교환은 한 칸에서 벌어지는 것**이라 이어지는지를
	// 이 값이 정한다.
	captureSq int
	// gaveCheck 는 王手를 걸었는가. 王手는 응수가 강제라 **혼자 서지 못한다.**
	gaveCheck bool
}

// trimRefutation 은 **손익이 바뀌는 첫 수**(딴다·王手)에서 시작해 **같은 칸에서 주고받는 동안**
// 끊는다. 상수 길이가 국면마다 틀리고, 교환·王手는 반쪽만 보여주면 거짓이 되기 때문이다
// (규칙 비교와 실측 8수 사례는 06-status.md §20). 그런 수가 없으면 첫 수만 남긴다.
func trimRefutation(steps []refutationStep) int {
	for i, s := range steps {
		if !s.settles {
			continue
		}
		end := i + 1
		for end < len(steps) {
			prev, cur := steps[end-1], steps[end]
			switch {
			// 王手에는 응수가 강제다. 답을 빼고 보여주면 「먹으면 되지 않나」가 되는데,
			// 실측한 両王手 국면에서는 그 「먹는 수」가 아예 합법수가 아니었다.
			case prev.gaveCheck:
			// 이어지는 王手도 같은 수순이다. 連続王手는 거기서 이야기가 끝난다.
			case cur.gaveCheck:
			// 같은 칸에서 주고받는 동안이 교환이다.
			case cur.captureSq >= 0 && cur.captureSq == prev.captureSq:
			default:
				return end
			}
			end++
		}
		return end
	}
	return 1
}
