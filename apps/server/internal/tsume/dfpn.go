package tsume

import (
	"context"
	"errors"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// inf 는 증명수·반증수의 무한대다. 합이 넘치지 않도록 uint32 의 절반에 둔다.
const inf uint32 = 1 << 30

// unbounded 는 「수 제한 없이」의 remaining 이다. 이 값에서는 내려가도 줄지 않아, 다른 깊이에서
// 만난 같은 국면이 pn·dn 을 나눠 쓴다.
const unbounded int16 = 1 << 14

func below(remaining int16) int16 {
	if remaining >= unbounded {
		return unbounded
	}
	return remaining - 1
}

// ErrBudget 은 마디나 표의 상한 안에서 결론을 내지 못한 것이다. 「詰まない」와 다르다.
var ErrBudget = errors.New("tsume: search budget exhausted")

// solver 는 국면 하나를 푸는 동안의 상태다. 한 goroutine 만 쓴다.
type solver struct {
	tt     *table
	nodes  int64
	budget int64
	// path 는 지금 내려온 수순의 국면이다. 같은 국면으로 돌아오는 王手 연속은 連続王手の千日手라
	// 공격 쪽이 진다. 수순에 매인 판정이라 표에 남기지 않는다.
	path map[uint64]bool
	// ctx 가 끝나면 탐색을 멈춘다. 마디 1024개마다 본다.
	ctx       context.Context
	cancelled bool
	// total 은 마디 전부의 상한이다(Limits.TotalNodes). 0 이면 없다.
	total int64
}

func newSolver(ctx context.Context, budget int64, maxSlots int) *solver {
	return &solver{tt: newTable(maxSlots), budget: budget, path: map[uint64]bool{}, ctx: ctx}
}

func (s *solver) exhausted() bool {
	if s.nodes&1023 == 0 && !s.cancelled && s.ctx.Err() != nil {
		s.cancelled = true
	}
	return s.cancelled || s.nodes >= s.budget || s.total > 0 && s.nodes >= s.total || s.tt.full
}

// look 은 remaining 에서 이 국면의 pn·dn 이다.
func (s *solver) look(h uint64, remaining int16) (uint32, uint32) {
	e, ok := s.tt.get(h)
	if !ok {
		return 1, 1
	}
	if e.mate >= 0 && e.mate <= remaining {
		return 0, inf
	}
	if e.noMate >= remaining {
		return inf, 0
	}
	if e.at == remaining {
		return e.pn, e.dn
	}
	return 1, 1
}

// prove 는 공격 쪽 수번인 pos 가 remaining 수 안에 詰むか를 정한다.
func (s *solver) prove(pos shogi.Position, remaining int16) (bool, error) {
	return s.decide(pos, remaining, true)
}

// proveAfter 는 공격 쪽이 둔 뒤의 pos(수비 쪽 수번)가 remaining 수 안에 詰むか를 정한다.
func (s *solver) proveAfter(pos shogi.Position, remaining int16) (bool, error) {
	return s.decide(pos, remaining, false)
}

func (s *solver) decide(pos shogi.Position, remaining int16, or bool) (bool, error) {
	h := hashOf(&pos)
	for {
		pn, dn := s.look(h, remaining)
		if pn == 0 {
			return true, nil
		}
		if dn == 0 {
			return false, nil
		}
		if s.exhausted() {
			return false, ErrBudget
		}
		s.mid(pos, h, remaining, inf-1, inf-1, or)
	}
}

type child struct {
	pos shogi.Position
	h   uint64
}

// mid 는 df-pn 의 한 마디다. or 이면 공격 쪽(王手만), 아니면 수비 쪽(모든 응수)이다.
//
// pn 은 언제나 「공격 쪽이 詰ます」의 증명수다. 공격 마디는 자식 pn 의 최소와 dn 의 합,
// 수비 마디는 pn 의 합과 dn 의 최소다.
func (s *solver) mid(pos shogi.Position, h uint64, remaining int16, thpn, thdn uint32, or bool) {
	s.nodes++

	var moves []shogi.Move
	if or {
		if remaining < 1 {
			s.disprove(h, remaining)
			return
		}
		moves = pos.Checks()
	} else {
		moves = pos.Evasions()
		if len(moves) == 0 {
			s.mated(h)
			return
		}
		if remaining < 1 {
			s.disprove(h, remaining)
			return
		}
	}
	if len(moves) == 0 {
		s.disprove(h, unbounded)
		return
	}

	kids := make([]child, len(moves))
	for i, m := range moves {
		np := pos.Apply(m)
		kids[i] = child{np, hashOf(&np)}
		s.seed(kids[i], !or)
	}

	s.path[h] = true
	defer delete(s.path, h)

	next := below(remaining)
	for {
		pn, dn, best, pn2, dn2 := s.gather(kids, next, or)
		if pn == 0 {
			s.settle(h, kids, remaining, or)
			return
		}
		if dn == 0 {
			s.disprove(h, remaining)
			return
		}
		e, _ := s.tt.get(h)
		e.pn, e.dn, e.at = pn, dn, remaining
		s.tt.put(h, e)
		if pn >= thpn || dn >= thdn || s.exhausted() {
			return
		}
		c := kids[best]
		cpn, cdn := s.childLook(c.h, next, or)
		var tp, td uint32
		if or {
			tp = min(thpn, epsilon(pn2))
			td = sat(thdn-dn, cdn)
		} else {
			td = min(thdn, epsilon(dn2))
			tp = sat(thpn-pn, cpn)
		}
		s.mid(c.pos, c.h, next, tp, td, !or)
	}
}

// seed 는 처음 보는 자식에 출발값을 준다. 수비 마디는 응수 수를 pn 으로 둔다(응수가 많을수록
// 증명이 멀다). 응수가 없으면 그 자리에서 詰み이고, 王手가 없는 공격 마디는 그 자리에서 반증이다.
// 이 둘은 수순과 깊이에 매이지 않는 사실이라 remaining 과 무관하게 적는다.
//
// 공격 마디의 출발값은 적지 않는다. 표에 없으면 1·1 로 읽히고, 칸을 아낀다.
func (s *solver) seed(c child, or bool) {
	if _, ok := s.tt.get(c.h); ok {
		return
	}
	if or {
		if !c.pos.HasCheck() {
			s.disprove(c.h, unbounded)
		}
		return
	}
	n := len(c.pos.Evasions())
	if n == 0 {
		s.mated(c.h)
		return
	}
	e := fresh
	e.pn, e.at = uint32(n), unbounded
	s.tt.put(c.h, e)
}

// childLook 은 자식의 pn·dn 이다. 공격 마디의 자식이 지금 수순에 이미 있으면 千日手라 반증이다.
func (s *solver) childLook(h uint64, remaining int16, parentOr bool) (uint32, uint32) {
	if !parentOr && s.path[h] {
		return inf, 0
	}
	return s.look(h, remaining)
}

// gather 는 자식들로 이 마디의 pn·dn 을 모은다. best 는 다음에 열 자식이고, pn2·dn2 는 둘째의
// 값이다(문턱을 정하는 데 쓴다). 같으면 앞의 것을 고른다. 고르는 순서가 결과를 정하므로 수의
// 순서(shogi.LegalMoves)를 바꾸면 고른 수순이 바뀔 수 있다.
func (s *solver) gather(kids []child, remaining int16, or bool) (pn, dn uint32, best int, pn2, dn2 uint32) {
	if or {
		pn, dn, pn2 = inf, 0, inf
		for i, c := range kids {
			cpn, cdn := s.childLook(c.h, remaining, true)
			dn = sat(dn, cdn)
			if cpn < pn {
				pn2 = pn
				pn, best = cpn, i
			} else if cpn < pn2 {
				pn2 = cpn
			}
		}
		return pn, dn, best, pn2, 0
	}
	pn, dn, dn2 = 0, inf, inf
	for i, c := range kids {
		cpn, cdn := s.childLook(c.h, remaining, false)
		pn = sat(pn, cpn)
		if cdn < dn {
			dn2 = dn
			dn, best = cdn, i
		} else if cdn < dn2 {
			dn2 = cdn
		}
	}
	return pn, dn, best, 0, dn2
}

// settle 은 증명된 마디에 詰み의 길이를 적는다. 공격 마디는 가장 짧은 증명된 자식, 수비 마디는
// 가장 긴 자식을 따른다.
func (s *solver) settle(h uint64, kids []child, remaining int16, or bool) {
	next := below(remaining)
	var length int16 = -1
	for _, c := range kids {
		ce, ok := s.tt.get(c.h)
		if !ok || ce.mate < 0 || ce.mate > next {
			continue
		}
		if or && (length < 0 || ce.mate < length) || !or && ce.mate > length {
			length = ce.mate
		}
	}
	e, _ := s.tt.get(h)
	// 찾은 詰み는 어느 것이든 참이다. 더 짧은 것을 이미 알면 그것을 둔다.
	if e.mate < 0 || length+1 < e.mate {
		e.mate = length + 1
	}
	e.pn, e.dn, e.at = 0, inf, remaining
	s.tt.put(h, e)
}

func (s *solver) mated(h uint64) {
	e, _ := s.tt.get(h)
	e.mate = 0
	e.pn, e.dn = 0, inf
	s.tt.put(h, e)
}

func (s *solver) disprove(h uint64, remaining int16) {
	e, _ := s.tt.get(h)
	if remaining > e.noMate {
		e.noMate = remaining
	}
	e.pn, e.dn, e.at = inf, 0, remaining
	s.tt.put(h, e)
}

// epsilon 은 둘째 자식 값에서 정하는 문턱이다(1+ε 기법). 둘째보다 조금 더 열어 두어야 가장
// 좋은 자식과 둘째 사이를 한 마디마다 오가지 않는다.
func epsilon(v uint32) uint32 {
	if v >= inf {
		return inf
	}
	return min(inf, v+1+v/4)
}

func sat(a, b uint32) uint32 {
	if a >= inf || b >= inf || a+b >= inf {
		return inf
	}
	return a + b
}
