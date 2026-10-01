package game

import (
	"context"
	"fmt"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 반박 트리. other 로 떨어진 수에 「그렇게 받아도 이렇게 끝난다」를 끝점까지 증명한다
// (journal §146).
//
// 상대의 최선수 하나로는 플레이어가 납득하지 않는다. 「그럼 B로 두면?」이 남는다. 그래서
// 플레이어 차례마다 둘 만한 응수(엔진 상위 OtherBranches 수와 되따기)를 전부 펼치고,
// 모든 가지가 판에서 보이는 손해에 닿을 때만 말한다. 하나라도 닿지 못하면 아무것도
// 주장하지 않는다.

// ProofLoss 는 끝점으로 치는 駒損의 하한이다. material 차라 따인 駒 한 장이 가치의 두 배로
// 잡힌다. 8은 香·桂 한 장이다.
const ProofLoss = 8

// ProofTurns 는 플레이어가 응수하는 차례의 상한이다.
const ProofTurns = 3

// ProofSearches 는 트리 하나에 거는 엔진 호출의 상한이다. 넘으면 증명하지 않는다.
//
// 트리는 개입 카드가 뜨기 전에 돈다. 이 상한이 카드 지연의 상한이다.
const ProofSearches = 16

// material 은 c 관점의 駒 가치 합(판 + 持ち駒, 玉 제외)의 차다.
func material(pos shogi.Position, c shogi.Color) int {
	sum := 0
	for _, p := range pos.Board {
		if p.Empty() || p.Type() == shogi.King {
			continue
		}
		if p.Color() == c {
			sum += pieceValue(p.Type())
		} else {
			sum -= pieceValue(p.Type())
		}
	}
	for t := shogi.Pawn; t <= shogi.Rook; t++ {
		sum += int(pos.Hands[c][t]-pos.Hands[c.Other()][t]) * pieceValue(t)
	}
	return sum
}

// see 는 수번 측이 sq 위의 상대 駒를 따기 시작했을 때 교환 끝까지 벌어지는 material 차다.
//
// material 과 같은 눈금이다. 딴 駒는 상대 판에서 빠지고 내 持ち駒(성하기 전 이름)로
// 들어오므로 한 장이 두 번 잡힌다. 한 번만 세면 角交換 같은 맞교환이 駒損으로 보인다.
//
// 따지 않는 것도 고를 수 있어서 0 아래로 내려가지 않는다. 합법수로만 따므로 핀에 묶인
// 駒는 따지 않는다(moveFacts 와 같은 판단).
func see(pos shogi.Position, sq int) int {
	target := pos.Board[sq]
	if target.Empty() || target.Color() == pos.Turn || target.Type() == shogi.King {
		return 0
	}
	best := 0
	for _, c := range legalCapturesOn(pos, sq) {
		gain := pieceValue(target.Type()) + pieceValue(target.Type().Base())
		if c.Promote {
			t := pos.Board[c.From].Type()
			gain += pieceValue(t.Promoted()) - pieceValue(t)
		}
		if v := gain - see(pos.Apply(c), sq); v > best {
			best = v
		}
	}
	return best
}

// settledLoss 는 플레이어 차례의 국면에서, 한 번 되딸 수 있는 만큼 되딴 뒤에도 남는 손해다.
//
// m0 은 물러진 수를 두기 전의 material 이다. 되딸 수 있는 도중에 멈추면 「取り返せば?」가
// 바로 나온다.
func settledLoss(pos shogi.Position, me shogi.Color, m0 int) int {
	regain := 0
	for sq, p := range pos.Board {
		if p.Empty() || p.Color() == me {
			continue
		}
		if g := see(pos, sq); g > regain {
			regain = g
		}
	}
	return m0 - material(pos, me) - regain
}

// prover 는 트리 하나를 펼치는 동안의 상태다.
type prover struct {
	// why 는 증명하지 못한 이유다. 측정만 읽는다.
	why    string
	a      *engineAnalyst
	multi  MultiSearcher
	ctx    context.Context
	start  string
	me     shogi.Color
	m0     int
	base   shogi.Position
	budget int
}

func (p *prover) spend() bool {
	p.budget--
	if p.budget < 0 {
		p.why = "budget"
		return false
	}
	return true
}

// proofTurns·proofSearches 는 측정이 상한을 넓혀 보려고 바꾸는 자리다. 프로덕션은 상수 그대로다.
var proofTurns, proofSearches = ProofTurns, ProofSearches

// proveLoss 는 물러진 수(moves 의 마지막) 뒤에 상대가 reply 를 두면 플레이어가 어떻게
// 받아도 손해로 끝나는지 본다. 끝났으면 응수마다 끝점을 돌려준다.
//
// reply 는 카드와 같은 질문의 첫 수다(cardPV). 여기서 다시 구하면 문장과 카드가 한 국면의
// 최선수를 둘로 말한다(journal §58).
func (a *engineAnalyst) proveLoss(
	ctx context.Context, startSFEN string, moves []string, reply string,
) (string, []explain.Loss, bool) {
	before, err := positionAfter(startSFEN, moves[:max(0, len(moves)-1)])
	if err != nil || len(moves) == 0 {
		return "", nil, false
	}
	best, losses, ok, _ := a.prove(ctx, startSFEN, moves, reply, before.Turn, before)
	return best, losses, ok
}

// prove 는 moves 뒤에 reply 가 두어졌을 때 me 가 어떻게 받아도 base 보다 駒損으로 끝나는지
// 본다. 증명하지 못한 이유를 함께 돌려준다.
//
// proveLoss 는 me 가 물러진 수를 둔 쪽이고 base 가 그 수 전이다. 거울상(놓친 이득)은 me 가
// 상대이고 reply 가 플레이어의 최선수다.
func (a *engineAnalyst) prove(
	ctx context.Context, startSFEN string, moves []string, reply string, me shogi.Color, base shogi.Position,
) (string, []explain.Loss, bool, string) {
	multi, ok := a.search.(MultiSearcher)
	if !ok {
		return "", nil, false, "setup"
	}
	pos, err := positionAfter(startSFEN, moves)
	if err != nil {
		return "", nil, false, "setup"
	}
	prevTo := -1
	if len(moves) > 0 {
		last, err := shogi.ParseUSIMove(moves[len(moves)-1])
		if err != nil {
			return "", nil, false, "setup"
		}
		prevTo = int(last.To)
	}
	r, err := shogi.ParseUSIMove(reply)
	if err != nil || pos.ValidateMove(r) != nil || pos.Turn == me {
		return "", nil, false, "setup"
	}

	p := &prover{
		a: a, multi: multi, ctx: ctx, start: startSFEN,
		me: me, m0: material(base, me), base: base, budget: proofSearches,
	}
	replyJa := pos.MoveJa(r, prevTo)
	taken := takenBy(pos, r, nil)
	next := pos.Apply(r)
	line := append(append([]string(nil), moves...), reply)

	if end := (explain.Loss{Taken: taken}); p.ended(next, &end) {
		return replyJa, []explain.Loss{end}, true, ""
	}
	losses, ok := p.answers(line, next, int(r.To), nil, taken, proofTurns)
	if !ok {
		return "", nil, false, p.why
	}
	return replyJa, losses, true, ""
}

// ended 는 me 차례의 국면이 끝점인지 보고, 끝점이면 그 줄의 끝을 채운다.
//
// 끝점은 둘이다. 되딴 뒤에도 ProofLoss 이상 駒損이거나, 상대가 base 에 없던 龍·馬를 만들었고
// 내가 그것을 딸 수 없다. 龍·馬는 material 로 +3 이라 앞의 기준에 걸리지 않지만 판에서
// 바로 보이고, 초심자가 「成り込まれた」로 그대로 받아들인다(journal §146).
func (p *prover) ended(pos shogi.Position, l *explain.Loss) bool {
	if settledLoss(pos, p.me, p.m0) >= ProofLoss {
		return true
	}
	if t, ok := newPromotedMajor(p.base, pos, p.me); ok {
		l.Promoted = shogi.PieceJa(t)
		return true
	}
	return false
}

// newPromotedMajor 는 me 의 상대가 base 보다 많이 가진 龍·馬 중 me 가 이득으로 딸 수 없는
// 것이 있으면 그 종류를 돌려준다. 龍을 먼저 본다.
func newPromotedMajor(base, pos shogi.Position, me shogi.Color) (shogi.PieceType, bool) {
	for _, t := range []shogi.PieceType{shogi.PromRook, shogi.PromBishop} {
		if safeMajors(pos, t, me) > safeMajors(base, t, me) {
			return t, true
		}
	}
	return 0, false
}

// safeMajors 는 me 의 상대가 가진 t 중 me 가 이득으로 딸 수 없는 것의 수다. 수번을 me 로 두고
// 센다. base 는 me 의 수 전 국면이라 수번이 다르다.
func safeMajors(pos shogi.Position, t shogi.PieceType, me shogi.Color) int {
	q := pos
	q.Turn = me
	n := 0
	for sq, pc := range q.Board {
		if !pc.Empty() && pc.Color() != me && pc.Type() == t && see(q, sq) == 0 {
			n++
		}
	}
	return n
}

// answers 는 플레이어 차례의 국면에서 둘 만한 응수를 전부 펼친다. 응수마다 한 줄을 돌려준다.
//
// 응수는 엔진 상위 OtherBranches 수와, 방금 움직인 상대 駒를 가장 싼 駒로 되따는 수다.
// 되따기는 엔진이 순위에 넣지 않아도 사람이 가장 먼저 떠올리는 수라 따로 넣는다.
func (p *prover) answers(
	line []string, pos shogi.Position, prevTo int, path, taken []string, turns int,
) ([]explain.Loss, bool) {
	if turns == 0 {
		p.why = fmt.Sprintf("turns(loss %d)", settledLoss(pos, p.me, p.m0))
		return nil, false
	}
	if !p.spend() {
		return nil, false
	}
	res, err := p.multi.SearchMultiPV(p.ctx, p.start, line, p.a.depth, OtherBranches)
	if err != nil {
		return nil, false
	}

	var tries []shogi.Move
	seen := map[shogi.Move]bool{}
	for _, l := range res.Ranked() {
		// 플레이어가 詰ます 수가 있으면 그 수는 손해가 아니다.
		if n, ok := l.Score.MateIn(); ok && n > 0 {
			p.why = "player mates"
			return nil, false
		}
		m, err := shogi.ParseUSIMove(l.Move)
		if err != nil || pos.ValidateMove(m) != nil || seen[m] {
			continue
		}
		seen[m] = true
		tries = append(tries, m)
	}
	if m, ok := cheapestRecapture(pos, prevTo); ok && !seen[m] {
		tries = append(tries, m)
	}
	if len(tries) == 0 {
		return nil, false
	}

	out := make([]explain.Loss, 0, len(tries))
	for _, m := range tries {
		next := pos.Apply(m)
		l, ok := p.reply(
			append(append([]string(nil), line...), m.USI()), next, int(m.To),
			appendCopy(path, pos.MoveJa(m, prevTo)), taken, turns,
		)
		if !ok {
			return nil, false
		}
		out = append(out, l)
	}
	return out, true
}

// reply 는 상대 차례의 국면에서 상대의 최선수를 두고 끝점인지 본다. 끝점이 아니면 다시
// 플레이어의 응수를 펼치고, 첫 응수(엔진 최선)의 줄을 이 가지의 줄로 삼는다.
//
// 화면에는 가지마다 한 줄만 나간다. 그 아래 다른 응수도 전부 끝점에 닿았다는 것은
// answers 가 확인한다.
func (p *prover) reply(
	line []string, pos shogi.Position, prevTo int, path, taken []string, turns int,
) (explain.Loss, bool) {
	if !p.spend() {
		return explain.Loss{}, false
	}
	res, err := p.a.search.SearchDepth(p.ctx, p.start, line, p.a.depth)
	if err != nil || res.Best == "" {
		return explain.Loss{}, false
	}

	// 詰み은 탐색의 mate 점수로 말하지 않는다. solver 가 증명한 것만 쓴다(journal §40 ⑤).
	if n, ok := res.Score.MateIn(); ok && n > 0 {
		if p.a.mate == nil || !p.spend() {
			return explain.Loss{}, false
		}
		mr, err := p.a.mate.SearchMate(p.ctx, p.start, line)
		if err != nil || !mr.Found() {
			p.why = "mate unproven"
			return explain.Loss{}, false
		}
		return explain.Loss{Moves: path, Taken: taken, MatePlies: len(mr.Moves)}, true
	}

	r, err := shogi.ParseUSIMove(res.Best)
	if err != nil || pos.ValidateMove(r) != nil {
		return explain.Loss{}, false
	}
	path = appendCopy(path, pos.MoveJa(r, prevTo))
	taken = takenBy(pos, r, taken)
	next := pos.Apply(r)
	if end := (explain.Loss{Moves: path, Taken: taken}); p.ended(next, &end) {
		return end, true
	}

	subs, ok := p.answers(append(append([]string(nil), line...), res.Best), next, int(r.To), path, taken, turns-1)
	if !ok {
		return explain.Loss{}, false
	}
	return subs[0], true
}

// cheapestRecapture 는 sq 의 상대 駒를 가장 싼 내 駒로 따는 합법수다.
func cheapestRecapture(pos shogi.Position, sq int) (shogi.Move, bool) {
	target := pos.Board[sq]
	if target.Empty() || target.Color() == pos.Turn {
		return shogi.Move{}, false
	}
	var best shogi.Move
	bestV := -1
	for _, c := range legalCapturesOn(pos, sq) {
		if v := pieceValue(pos.Board[c.From].Type()); bestV < 0 || v < bestV {
			best, bestV = c, v
		}
	}
	return best, bestV >= 0
}

// takenBy 는 상대의 수 m 이 플레이어의 駒를 따면 그 한자를 덧붙인다. 같은 이름은 한 번만
// 적는다. pos 는 m 을 두기 전 국면이고 수번이 상대여야 한다.
func takenBy(pos shogi.Position, m shogi.Move, taken []string) []string {
	got := pos.Board[m.To]
	if m.IsDrop() || got.Empty() || got.Color() == pos.Turn {
		return taken
	}
	name := shogi.PieceJa(got.Type())
	for _, t := range taken {
		if t == name {
			return taken
		}
	}
	return appendCopy(taken, name)
}

// appendCopy 는 원본을 건드리지 않고 덧붙인다. 가지마다 경로가 갈려서 공유하면 안 된다.
func appendCopy(s []string, v string) []string {
	return append(append(make([]string, 0, len(s)+1), s...), v)
}
