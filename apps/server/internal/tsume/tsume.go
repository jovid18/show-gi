// Package tsume 은 詰め将棋를 푼다. 공격 쪽은 王手만 두고, 수비 쪽은 모든 응수를 둔다.
//
// 엔진을 쓰지 않는다. 같은 국면에 같은 트리가 나와야 하고(마디 상한도 시간 대신 마디 수로 건다),
// 증명이 끝난 치환표에 수비 쪽 응수 전부의 증명이 이미 들어 있어 트리를 그대로 꺼낼 수 있다.
// 엔진의 go mate 는 수순 한 줄만 준다.
//
// 詰め将棋의 관례를 따른다. 공격 쪽은 가장 짧은 수순을 고르고, 無駄合い(거는 말로 잡고 그 말을
// 쓰지 않고도 같은 수로 詰む 合駒)는 手数에 넣지 않는다. 持ち駒는 판에 놓인 그대로 쓴다. 玉方가
// 남은 말 전부를 가진다는 관례(残り駒全部玉方持ち)는 따르지 않는다.
package tsume

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// Limits 는 한 번 푸는 데 쓸 수 있는 몫이다. 시간이 아니라 마디 수라서 결과가 서버의 부하에
// 매이지 않는다.
type Limits struct {
	// Nodes 는 詰み를 처음 찾을 때까지의 마디 상한이다.
	Nodes int64
	// ShortenNodes 는 뿌리에서 더 짧은 詰み를 찾는 데 쓰는 마디 상한이다. 다 쓰면 그때까지 찾은
	// 가장 짧은 것을 내고 Shortest 를 거짓으로 둔다.
	ShortenNodes int64
	// BranchNodes 는 트리의 공격 마디 하나에서 더 짧은 詰み를 찾는 데 쓰는 마디 상한이다.
	BranchNodes int64
	// Slots 는 치환표 칸 수의 상한이다(2의 거듭제곱). 칸 하나가 24바이트다.
	Slots int
	// TotalNodes 는 한 번 푸는 데 쓰는 마디 전부의 상한이다. 위의 몫은 단계마다 새로 주므로 트리가
	// 크면 합이 끝없이 늘어난다. 이 값에 닿으면 ErrTooLarge 다.
	TotalNodes int64
	// MaxMoves 는 트리에 담을 수의 상한이다. 넘으면 ErrTooLarge 다.
	MaxMoves int
}

// DefaultLimits 는 서버가 쓰는 값이다.
//
// [미확정] 王座戦 第74期 第2局의 실전 국면 넷으로만 정했다(journal §147). 가장 긴 것(25手詰め를
// 트리까지)이 8.3M 마디였다. TotalNodes 는 로컬에서 3분쯤이다.
var DefaultLimits = Limits{
	Nodes:        8_000_000,
	ShortenNodes: 8_000_000,
	BranchNodes:  200_000,
	Slots:        1 << 23,
	TotalNodes:   20_000_000,
	MaxMoves:     5000,
}

var (
	// ErrNoKing 은 수비 쪽(수번이 아닌 쪽) 玉이 없는 국면이다.
	ErrNoKing = errors.New("tsume: the defending king is missing")
	// ErrTooLarge 는 트리가 MaxMoves 나 TotalNodes 를 넘은 것이다. 詰む 것은 알지만 트리를 내지 못한다.
	ErrTooLarge = errors.New("tsume: the tree is too large to show")
)

// Status 는 푼 결과의 종류다.
type Status int

const (
	// Unknown 은 상한 안에서 결론을 내지 못했다.
	Unknown Status = iota
	// Mate 는 詰む.
	Mate
	// NoMate 는 王手를 어떻게 이어도 詰まない.
	NoMate
)

// Result 는 푼 결과다.
type Result struct {
	Status Status
	// Plies 는 詰みまでの手数다(無駄合い를 넣지 않는다). Mate 일 때만 뜻이 있다.
	Plies int
	// Shortest 는 Plies 보다 짧은 詰み가 없음을 확인했는가다.
	Shortest bool
	// First 는 공격 쪽의 첫 수다. Mate 일 때만 있다.
	First *Move
	// Nodes 는 쓴 마디 수다.
	Nodes int64
}

// Move 는 트리의 수 하나다. 공격 쪽 수의 Replies 는 수비 쪽 응수 전부이고, 수비 쪽 수의 Replies 는
// 공격 쪽의 다음 수 하나다. 詰み를 거는 수는 Replies 가 비어 있다.
type Move struct {
	USI string
	// Ja 는 棋譜 표기다(▲2二金). 바로 앞 수와 같은 칸이면 「同」으로 적는다.
	Ja string
	// SFEN 은 이 수를 둔 뒤의 국면이다. 화면이 수를 누르면 이 판을 그린다.
	SFEN string
	// Rest 는 이 수를 둔 뒤 詰みまでの手数다. 無駄合い는 세지 않는다.
	Rest int
	// Futile 은 이 응수가 無駄合い라는 표시다. 그 뒤를 펼치지 않는다(Replies 가 비어 있다).
	Futile  bool
	Replies []*Move
}

// Solve 는 수번 쪽을 공격 쪽으로 보고 詰め将棋를 푼다. ctx 가 끝나면 ctx.Err() 를 돌려준다.
func Solve(ctx context.Context, pos shogi.Position, lim Limits) (Result, error) {
	if pos.KingSquare(pos.Turn.Other()) < 0 {
		return Result{}, ErrNoKing
	}
	s := newSolver(ctx, lim.Nodes, lim.Slots)
	s.total = lim.TotalNodes
	res, err := s.solve(pos, lim)
	if s.cancelled {
		return Result{}, ctx.Err()
	}
	return res, err
}

func (s *solver) solve(pos shogi.Position, lim Limits) (Result, error) {
	ok, err := s.prove(pos, unbounded)
	if errors.Is(err, ErrBudget) {
		return Result{Status: Unknown, Nodes: s.nodes}, nil
	}
	if !ok {
		return Result{Status: NoMate, Nodes: s.nodes}, nil
	}

	length, shortest := s.shorten(pos, unbounded, lim.ShortenNodes)
	b := builder{s: s, lim: lim}
	// 뿌리는 실제 수로 가장 짧은 것보다 合駒 칸 일곱 개만큼(14수) 긴 수순까지 후보로 본다(attack).
	first, err := b.attack(pos, length+rootSlack, -1)
	if err != nil {
		return Result{}, err
	}
	return Result{Status: Mate, Plies: first.Rest + 1, Shortest: shortest, First: first, Nodes: s.nodes}, nil
}

// shorten 은 bound 안에서 詰む 것을 아는 pos 에서 더 짧은 詰み를 찾는다. 두 번째 값은 그보다
// 짧은 것이 없음을 확인했는가다. 마디는 budget 까지만 더 쓴다.
func (s *solver) shorten(pos shogi.Position, bound int16, budget int64) (int16, bool) {
	h := hashOf(&pos)
	s.budget = s.nodes + budget
	e, _ := s.tt.get(h)
	if e.mate < 0 || e.mate > bound {
		// 표가 비워졌거나(sweep) 더 긴 詰み만 적혀 있다. bound 안의 詰み를 다시 찾는다.
		if ok, err := s.prove(pos, bound); err != nil || !ok {
			return bound, false
		}
		e, _ = s.tt.get(h)
	}
	length := e.mate
	for length >= 3 {
		ok, err := s.prove(pos, length-2)
		if err != nil {
			return length, false
		}
		if !ok {
			return length, true
		}
		e, _ = s.tt.get(h)
		length = e.mate
	}
	return length, true
}

// rootSlack 은 뿌리에서 無駄合い 때문에 실제로 더 길어지는 수순을 얼마나 더 보는가다. 한 줄의
// 合駒 칸은 많아야 일곱이고 한 칸에 두 수(合駒와 그것을 잡는 수)다.
const rootSlack = 14

type builder struct {
	s     *solver
	lim   Limits
	moves int
}

func (b *builder) count() error {
	b.moves++
	if b.moves > b.lim.MaxMoves {
		return ErrTooLarge
	}
	return nil
}

// attack 은 bound 수 안에 詰む pos(공격 쪽 수번)에서 공격 쪽의 수를 고르고 그 뒤를 펼친다.
// prevTo 는 바로 앞 수의 목적칸이다(「同」 표기).
//
// 탐색은 수를 실제로 센다. 無駄合い를 빼고 세면 더 짧은 수가 있을 수 있어서, 멀리서 거는 王手는
// 合駒가 들 칸 하나마다 두 수씩 더 길어도 후보로 펼쳐 보고 관례의 手数(Rest)가 가장 짧은 것을
// 고른다. 같으면 수의 순서(shogi.Checks)에서 앞의 것이다.
func (b *builder) attack(pos shogi.Position, bound int16, prevTo int) (*Move, error) {
	length, _ := b.s.shorten(pos, bound, b.lim.BranchNodes)
	var best *Move
	plain := false // 合駒가 들 수 없는 王手 중 가장 짧은 것을 이미 펼쳤는가
	for _, m := range pos.Checks() {
		after := pos.Apply(m)
		gap := int16(interpositions(after))
		if gap == 0 && plain {
			continue
		}
		// 실제 수의 상한은 부모에게서 받은 bound 보다 언제나 작다. 그래야 王手가 돌고 도는 수순에서도
		// 펼치기가 끝난다.
		limit := min(length-1+2*gap, bound-1)
		b.s.budget = b.s.nodes + b.lim.BranchNodes
		ok, err := b.s.proveAfter(after, limit)
		if err != nil || !ok {
			continue
		}
		kept := b.moves
		mv := &Move{USI: m.USI(), Ja: pos.MoveJa(m, prevTo), SFEN: after.SFEN()}
		if err := b.count(); err != nil {
			return nil, err
		}
		if err := b.defend(after, limit, int(m.To), mv); err != nil {
			return nil, err
		}
		if gap == 0 {
			plain = true
		}
		if best == nil || mv.Rest < best.Rest {
			if best != nil {
				b.moves -= best.size()
			}
			best = mv
		} else {
			b.moves = kept
		}
	}
	if best == nil {
		// 마디가 모자라 후보를 하나도 증명하지 못했다. 상한이 남아 있었는데 그렇다면 치환표가 틀린 것이다.
		if b.s.total > 0 && b.s.nodes >= b.s.total || b.s.tt.full {
			return nil, ErrTooLarge
		}
		return nil, errors.New("tsume: a proven position has no mating check")
	}
	return best, nil
}

// size 는 이 수와 그 아래 수의 개수다.
func (m *Move) size() int {
	n := 1
	for _, r := range m.Replies {
		n += r.size()
	}
	return n
}

// interpositions 는 王手를 받은 pos 에서 合駒가 들 수 있는 칸 수다. 거는 말이 하나이고 떨어져서
// 줄로 걸 때만 0 이 아니다.
func interpositions(pos shogi.Position) int {
	k := pos.KingSquare(pos.Turn)
	checkers := pos.Attackers(k, pos.Turn.Other())
	if len(checkers) != 1 {
		return 0
	}
	c := checkers[0]
	dr, dc := k/9-c/9, k%9-c%9
	if dr != 0 && dc != 0 && dr != dc && dr != -dc {
		return 0 // 桂
	}
	return max(abs(dr), abs(dc)) - 1
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// defend 는 공격 쪽이 mv 를 둔 뒤의 pos 에서 수비 쪽 응수 전부를 펼친다. pos 는 bound 수 안에
// 詰む.
func (b *builder) defend(pos shogi.Position, bound int16, prevTo int, mv *Move) error {
	for _, r := range pos.Evasions() {
		if err := b.count(); err != nil {
			return err
		}
		after := pos.Apply(r)
		reply := &Move{USI: r.USI(), Ja: pos.MoveJa(r, prevTo), SFEN: after.SFEN()}
		if r.IsDrop() && b.futile(after, r, bound-1) {
			reply.Futile = true
			mv.Replies = append(mv.Replies, reply)
			continue
		}
		next, err := b.attack(after, bound-1, int(r.To))
		if err != nil {
			return err
		}
		reply.Rest = next.Rest + 1
		reply.Replies = []*Move{next}
		mv.Replies = append(mv.Replies, reply)
	}
	// 긴 응수가 먼저다. 수비 쪽은 가장 길게 버티므로 첫 응수가 本手順이 된다. 같은 길이는 수의
	// 순서 그대로, 無駄合い는 맨 뒤다.
	slices.SortStableFunc(mv.Replies, func(x, y *Move) int {
		if x.Futile != y.Futile {
			if x.Futile {
				return 1
			}
			return -1
		}
		return cmp.Compare(y.Rest, x.Rest)
	})
	if len(mv.Replies) > 0 && !mv.Replies[0].Futile {
		mv.Rest = mv.Replies[0].Rest + 1
	}
	return nil
}

// futile 은 合駒 drop 이 無駄合い인가다. after 는 合駒를 둔 뒤(공격 쪽 수번)이고 bound 수 안에
// 詰む.
//
// 그 칸의 말을 잡는 王手가 가장 짧은 詰み의 첫 수이고, 잡은 말을 持ち駒에서 빼도 그 길이로
// 詰めば 無駄合い다. 「合駒를 잡아도 局面이 合駒 전과 다르지 않다」는 관례의 정의를 「잡은 말이
// 없어도 같은 수로 詰む」로 옮긴 것이다.
func (b *builder) futile(after shogi.Position, drop shogi.Move, bound int16) bool {
	attacker := after.Turn
	bound, _ = b.s.shorten(after, bound, b.lim.BranchNodes)
	for _, m := range after.Checks() {
		if m.To != drop.To {
			continue
		}
		taken := after.Apply(m)
		taken.Hands[attacker][drop.Drop]--
		b.s.budget = b.s.nodes + b.lim.BranchNodes
		if ok, err := b.s.proveAfter(taken, bound-1); err == nil && ok {
			return true
		}
	}
	return false
}
