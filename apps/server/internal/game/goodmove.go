package game

import (
	"context"
	"log"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// GoodCandidates 는 好手를 가릴 때 묻는 후보 수다. 최선수와 차선수만 있으면 된다.
const GoodCandidates = 2

// Obvious 는 最善이어도 好手로 부르지 않는 수의 종류다. 초심자도 보는 수다.
type Obvious string

const (
	ObviousNone Obvious = ""
	// ObviousEvasion 은 王手를 받는 수다. 받을 수가 몇 개뿐이라 고른 것이 아니다.
	ObviousEvasion Obvious = "evasion"
	// ObviousRecapture 는 직전에 상대가 온 칸에서 駒를 되잡는 수(同)다.
	ObviousRecapture Obvious = "recapture"
	// ObviousFreeCapture 는 되잡히지 않는 駒를 따는 수다.
	ObviousFreeCapture Obvious = "free-capture"
	// ObviousGainingCapture 는 움직인 駒보다 비싼 駒를 따는 수다. 되잡혀도 한 수만 보면
	// 이득이 보인다.
	ObviousGainingCapture Obvious = "gaining-capture"
)

// ObviousMove 는 그 수가 누구나 보는 수인지 본다. 룰 엔진만 쓴다.
//
// 01-core.md §7.1 이 手筋 힌트에서 뺀 것과 같은 부류다. 엔진이 1위로 보고 2위와의 차도
// 크지만, 초심자가 가장 먼저 찾는 수라 칭찬할 거리가 되지 않는다.
//
// 되잡힘은 LegalMoves 로 센다. 利き으로 세면 핀에 묶인 駒까지 센다(journal §15).
// prevTo 는 직전 상대 수의 도착 칸이고 없으면 -1이다.
func ObviousMove(before shogi.Position, m shogi.Move, prevTo int) Obvious {
	if before.InCheck(before.Turn) {
		return ObviousEvasion
	}
	to := int(m.To)
	taken := before.Board[to]
	if taken.Empty() || taken.Color() == before.Turn {
		return ObviousNone
	}
	if to == prevTo {
		return ObviousRecapture
	}
	if pieceValue(taken.Type()) > pieceValue(before.Board[m.From].Type()) {
		return ObviousGainingCapture
	}
	for _, r := range before.Apply(m).LegalMoves() {
		if int(r.To) == to {
			return ObviousNone
		}
	}
	return ObviousFreeCapture
}

// goodQuery 는 好手인지 물을 한 수다. 싼 조건을 다 지난 수에만 판정이 채운다.
type goodQuery struct {
	startSFEN  string
	before     []string
	played     string
	baselineCp int
}

// goodAsker 는 好手를 판정과 따로 묻는 Analyst 다. engineAnalyst 가 만족한다.
//
// 따로 두는 것은 대국이 이 탐색을 기다리지 않게 하기 위해서다. 好手는 대국 화면에 나가지
// 않으므로, 착수를 확정한 뒤에 물어도 된다(state.maybeAskGood).
type goodAsker interface {
	askGood(ctx context.Context, q goodQuery) goodCheck
}

// goodCheck 는 好手 판정에 쓴 값이다. 묻지 않았으면 Asked 가 false 다.
type goodCheck struct {
	Asked bool
	Gap   float64
	Good  bool
}

// CheckGood 은 판정이 남긴 물음이 있으면 그 자리에서 묻고 j 에 채운다.
//
// 대국 밖에서 판정을 쓰는 자리(가져온 기보 분석·기보 임포트·측정)가 부른다. 대국은
// 기다리지 않도록 확정 뒤에 따로 묻는다.
func CheckGood(ctx context.Context, a Analyst, j *Judgement) {
	asker, ok := a.(goodAsker)
	if !ok || j.goodQuery == nil {
		return
	}
	g := asker.askGood(ctx, *j.goodQuery)
	j.GoodAsked, j.GoodGap, j.Good = g.Asked, g.Gap, g.Good
}

// askGood 은 최선수를 둔 수가 好手인지 MultiPV 2로 다시 묻는다.
//
// 판정의 k=1 탐색으로는 차선수를 모른다. 그래서 싼 조건(최선수와 같은가·누구나 보는
// 수인가)을 먼저 보고 남은 수만 묻는다. 답은 k=2 탐색의 1위가 둔 수와 같을 때만 쓴다.
// k=1 과 k=2 는 같은 국면에서 1위가 갈릴 수 있다(journal §58).
func (a *engineAnalyst) askGood(ctx context.Context, q goodQuery) goodCheck {
	multi, ok := a.search.(MultiSearcher)
	if !ok {
		return goodCheck{}
	}
	res, err := multi.SearchMultiPV(ctx, q.startSFEN, q.before, a.depth, GoodCandidates)
	if err != nil {
		log.Printf("game: could not ask for the second candidate, no good move: %v", err)
		return goodCheck{}
	}
	ranked := res.Ranked()
	if len(ranked) < 2 || ranked[0].Move != q.played {
		return goodCheck{Asked: true}
	}
	in := intervene.GoodInput{Best: ranked[0].Score, Second: ranked[1].Score, BaselineCp: q.baselineCp}
	return goodCheck{Asked: true, Gap: intervene.GoodGap(in), Good: intervene.IsGood(in)}
}

// prevDest 는 수순 마지막 수 직전 수의 도착 칸이다. 없으면 -1.
func prevDest(moves []string) int {
	if len(moves) < 2 {
		return -1
	}
	m, err := shogi.ParseUSIMove(moves[len(moves)-2])
	if err != nil {
		return -1
	}
	return int(m.To)
}
