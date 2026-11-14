package tsume

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// HisshiLimits 는 1手必至 한 번에 쓸 수 있는 몫이다. 시간이 아니라 마디 수다.
type HisshiLimits struct {
	// MateNodes 는 뿌리 국면이 이미 詰む 국면인지 가리는 데 쓰는 마디 상한이다. 결론이 나지 않으면
	// 必至를 찾지 않고 Unknown 이다. 詰む 국면의 첫 王手를 必至로 내지 않기 위해서다.
	MateNodes int64
	// ReplyNodes 는 수비 응수 하나 뒤의 詰み를 증명하는 데 쓰는 마디 상한이다. 다 쓰면 그 응수는
	// 「모른다」이고, 그 공격 수는 必至로 인정하지 않는다.
	ReplyNodes int64
	// TotalNodes 는 한 번 푸는 데 쓰는 마디 전부의 상한이다. 닿으면 Unknown 이다.
	TotalNodes int64
	// Slots 는 치환표 칸 수의 상한이다. 모든 응수의 풀이가 표 하나를 나눠 쓴다.
	Slots int
	// Tree 는 정답 트리에서 응수마다 詰み 수순을 지을 때의 몫이다. Tree.TotalNodes 는 트리를 짓는
	// 동안의 마디 합의 상한이고, 닿으면 ErrTooLarge 다.
	Tree Limits
}

// DefaultHisshiLimits 는 서버가 쓰는 값이다.
//
// [미확정] 책의 1手必至 두 문제로만 정했다(journal §156). MateNodes 는 詰め将棋의 Nodes 와 같다.
var DefaultHisshiLimits = HisshiLimits{
	MateNodes:  8_000_000,
	ReplyNodes: 300_000,
	TotalNodes: 40_000_000,
	Slots:      1 << 23,
	Tree: Limits{
		ShortenNodes: 200_000,
		BranchNodes:  50_000,
		TotalNodes:   20_000_000,
		MaxMoves:     5000,
	},
}

// ErrAttackerKing 은 공격 쪽에도 玉이 있는 국면이다. 수비 쪽의 逆王手를 이 풀이는 다루지 않는다.
var ErrAttackerKing = errors.New("tsume: hisshi with the attacking king on the board")

// HisshiStatus 는 1手必至 풀이의 결과 종류다.
type HisshiStatus int

const (
	// HisshiUnknown 은 상한 안에서 결론을 내지 못했다.
	HisshiUnknown HisshiStatus = iota
	// HisshiFound 는 1手必至가 있다.
	HisshiFound
	// HisshiNone 은 어느 수를 두어도 詰みを逃れる 응수가 있다.
	HisshiNone
	// HisshiMate 는 이미 詰む 국면이다. 必至가 아니라 詰め将棋다.
	HisshiMate
)

// HisshiResult 는 1手必至 풀이의 결과다.
type HisshiResult struct {
	Status HisshiStatus
	// First 는 정답 수다. Replies 는 수비 쪽 응수 전부(긴 것부터)이고, 응수마다 詰み 수순 한 줄이
	// 이어진다. HisshiFound 일 때만 있다.
	First *Move
	// Answers 는 必至인 수 전부의 棋譜 표기다. 수의 순서(LegalMoves)대로다.
	Answers []string
	// Incomplete 는 상한에 걸려 必至인지 가리지 못한 수가 있었다는 표시다. 그러면 Answers 가 必至인
	// 수 전부라고 말할 수 없다.
	Incomplete bool
	// Nodes 는 쓴 마디 수다.
	Nodes int64
}

// SolveHisshi 는 수번 쪽을 공격 쪽으로 보고 1手必至를 찾는다.
//
// 공격 쪽의 합법수 전부를 王手에 한정하지 않고 본다. 그 수 뒤 수비 쪽의 합법 응수 전부에 대해
// 공격 쪽에 詰み가 있을 때만 必至로 인정한다. 응수 하나라도 상한 안에 증명하지 못하면 인정하지
// 않으므로, 틀린다면 必至를 놓치는 쪽으로만 틀린다. 바로 詰む 수는 必至로 세지 않는다.
func SolveHisshi(ctx context.Context, pos shogi.Position, lim HisshiLimits) (HisshiResult, error) {
	if pos.KingSquare(pos.Turn.Other()) < 0 {
		return HisshiResult{}, ErrNoKing
	}
	if pos.KingSquare(pos.Turn) >= 0 {
		return HisshiResult{}, ErrAttackerKing
	}
	s := newSolver(ctx, lim.MateNodes, lim.Slots)
	s.total = lim.TotalNodes

	// 이미 詰む 국면은 必至 문제가 아니다. 가리지 못했는데 必至를 찾으면 詰み 수순의 첫 王手가
	// 必至로 나갈 수 있으므로 거기서 그만둔다.
	ok, err := s.prove(pos, unbounded)
	switch {
	case s.cancelled:
		return HisshiResult{}, ctx.Err()
	case errors.Is(err, ErrBudget):
		return HisshiResult{Status: HisshiUnknown, Nodes: s.nodes}, nil
	case err != nil:
		return HisshiResult{}, err
	case ok:
		return HisshiResult{Status: HisshiMate, Nodes: s.nodes}, nil
	}

	var proven []shogi.Move
	unknown := false
	for _, m := range pos.LegalMoves() {
		after := pos.Apply(m)
		if after.IsCheckmate() {
			continue
		}
		switch ok, err := s.holds(after, lim.ReplyNodes); {
		case s.cancelled:
			return HisshiResult{}, ctx.Err()
		case errors.Is(err, ErrBudget):
			unknown = true
		case ok:
			proven = append(proven, m)
		}
		if s.total > 0 && s.nodes >= s.total || s.tt.full {
			return HisshiResult{Status: HisshiUnknown, Nodes: s.nodes}, nil
		}
	}
	if len(proven) == 0 {
		if unknown {
			return HisshiResult{Status: HisshiUnknown, Nodes: s.nodes}, nil
		}
		return HisshiResult{Status: HisshiNone, Nodes: s.nodes}, nil
	}

	answers := make([]string, len(proven))
	for i, m := range proven {
		answers[i] = pos.MoveJa(m, -1)
	}
	// 트리는 응수마다 몫을 새로 받으므로 따로 합의 상한을 건다.
	s.total = 0
	if lim.Tree.TotalNodes > 0 {
		s.total = s.nodes + lim.Tree.TotalNodes
	}
	first, err := s.hisshiTree(pos, proven[0], lim.Tree)
	if s.cancelled {
		return HisshiResult{}, ctx.Err()
	}
	if err != nil {
		return HisshiResult{}, err
	}
	return HisshiResult{Status: HisshiFound, First: first, Answers: answers, Incomplete: unknown, Nodes: s.nodes}, nil
}

// holds 는 after(수비 쪽 수번)의 모든 응수 뒤에 詰み가 있는가다. 응수 하나를 반증하면 바로
// false, 증명하지 못한 응수가 있으면 ErrBudget 이다.
func (s *solver) holds(after shogi.Position, perReply int64) (bool, error) {
	replies := after.LegalMoves()
	if len(replies) == 0 {
		return false, nil
	}
	var budgetErr error
	for _, r := range replies {
		s.budget = s.nodes + perReply
		ok, err := s.prove(after.Apply(r), unbounded)
		if s.cancelled {
			return false, ErrBudget
		}
		if errors.Is(err, ErrBudget) {
			// 다른 응수가 반증되면 그 수는 必至가 아니라고 확정할 수 있으므로 끝까지 본다.
			budgetErr = ErrBudget
			continue
		}
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	if budgetErr != nil {
		return false, budgetErr
	}
	return true, nil
}

// hisshiTree 는 必至 수 m 의 트리를 짓는다. 응수마다 공격 쪽의 詰み 수순을 이어 붙이되, 그
// 안의 갈래는 本手順(첫 응수)과 無駄合い만 남긴다.
//
// 응수가 수백 개라 그대로는 읽히지 않는다(대부분 玉과 무관한 打ち다). 공격 쪽 수순이 같은
// 응수끼리 묶어 대표 하나만 Replies 에 두고 나머지는 대표의 Same 에 둔다.
func (s *solver) hisshiTree(pos shogi.Position, m shogi.Move, lim Limits) (*Move, error) {
	after := pos.Apply(m)
	root := &Move{USI: m.USI(), Ja: pos.MoveJa(m, -1), SFEN: after.SFEN()}
	b := builder{s: s, lim: lim, memo: map[memoKey]*Move{}}
	groups := map[string]*Move{}
	var reps []*Move
	for _, r := range after.LegalMoves() {
		next := after.Apply(r)
		line, err := b.mateLine(next, int(r.To))
		if err != nil {
			return nil, err
		}
		reply := &Move{
			USI: r.USI(), Ja: after.MoveJa(r, int(m.To)), SFEN: next.SFEN(),
			Rest: line.Rest + 1, Replies: []*Move{line},
		}
		key := attackKey(line)
		rep, ok := groups[key]
		switch {
		case !ok:
			groups[key] = reply
			reps = append(reps, reply)
		case rep.isDrop() && !reply.isDrop():
			// 대표는 판 위의 수가 먼저다. 打ち보다 받는 뜻이 읽히기 쉽다.
			reply.Same = append([]*Move{stripped(rep)}, rep.Same...)
			rep.Same = nil
			*rep = *reply
		default:
			rep.Same = append(rep.Same, stripped(reply))
		}
	}
	if key, ok := b.threatKey(after); ok {
		if rep, ok := groups[key]; ok {
			rep.Threat = true
		}
	}
	// 긴 그룹이 먼저다. 화면은 첫 그룹을 本手順으로 펼친다. 같은 길이는 큰 그룹이 먼저다.
	slices.SortStableFunc(reps, func(x, y *Move) int {
		if c := cmp.Compare(y.Rest, x.Rest); c != 0 {
			return c
		}
		return cmp.Compare(len(y.Same), len(x.Same))
	})
	root.Replies = reps
	if len(reps) > 0 {
		root.Rest = reps[0].Rest + 1
	}
	return root, nil
}

// mateLine 은 공격 쪽 수번인 pos 의 詰み 수순(本手順 한 줄)이다.
func (b *builder) mateLine(pos shogi.Position, prevTo int) (*Move, error) {
	length, _ := b.s.shorten(pos, unbounded, b.lim.ShortenNodes)
	line, err := b.attack(pos, length+rootSlack, prevTo, math.MaxInt)
	if err != nil {
		return nil, err
	}
	return mainLine(line), nil
}

// threatKey 는 after(必至 수 뒤, 수비 쪽 수번)에서 수비가 손을 뺐을 때의 詰み 수순의 키다.
// 必至 수가 王手면 손을 뺄 수 없으므로 없다.
func (b *builder) threatKey(after shogi.Position) (string, bool) {
	if after.InCheck(after.Turn) {
		return "", false
	}
	null := after
	null.Turn = after.Turn.Other()
	b.s.budget = b.s.nodes + b.lim.ShortenNodes
	if ok, err := b.s.prove(null, unbounded); err != nil || !ok {
		return "", false
	}
	line, err := b.mateLine(null, -1)
	if err != nil {
		return "", false
	}
	return attackKey(line), true
}

// attackKey 는 本手順 한 줄에서 공격 쪽 수만 이은 것이다. 응수 그룹의 키다.
func attackKey(line *Move) string {
	key := ""
	for m, attacker := line, true; m != nil; attacker = !attacker {
		if attacker {
			key += m.USI + " "
		}
		if len(m.Replies) == 0 || m.Replies[0].Futile {
			break
		}
		m = m.Replies[0]
	}
	return key
}

func (m *Move) isDrop() bool { return len(m.USI) > 1 && m.USI[1] == '*' }

// stripped 는 그룹의 나머지로 둘 때의 사본이다. 뒤를 펼치지 않으므로 수순을 뗀다.
func stripped(m *Move) *Move {
	return &Move{USI: m.USI, Ja: m.Ja, SFEN: m.SFEN, Rest: m.Rest}
}

// mainLine 은 공격 마디 m 에서 本手順만 남긴 사본이다. memo 가 마디를 공유하므로 원본을 고치지 않는다.
func mainLine(m *Move) *Move {
	out := *m
	out.Replies = nil
	for i, r := range m.Replies {
		if r.Futile {
			out.Replies = append(out.Replies, r)
			continue
		}
		if i > 0 {
			continue
		}
		reply := *r
		reply.Replies = nil
		for _, next := range r.Replies {
			reply.Replies = append(reply.Replies, mainLine(next))
		}
		out.Replies = append(out.Replies, &reply)
	}
	return &out
}
