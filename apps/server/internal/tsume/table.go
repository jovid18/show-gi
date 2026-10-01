package tsume

import "github.com/jovid18/show-gi/apps/server/internal/shogi"

// hashOf 는 국면의 64비트 지문이다(FNV-1a). 판·持ち駒·手番을 담고 手数는 뺀다.
//
// 지문이 겹치면 다른 국면의 답을 쓴다. 표가 수백만 칸일 때 겹칠 확률은 1e-6 쯤이고, 국면을 통째로
// 키로 두면 칸마다 96바이트가 더 들어 같은 메모리에 담을 수 있는 칸 수가 1/5 로 준다.
// 0 은 빈 칸의 표시라 지문으로 쓰지 않는다.
func hashOf(pos *shogi.Position) uint64 {
	const prime = 1099511628211
	h := uint64(14695981039346656037)
	for _, p := range pos.Board {
		h = (h ^ uint64(uint8(p))) * prime
	}
	for c := 0; c < 2; c++ {
		for t := shogi.Pawn; t <= shogi.Rook; t++ {
			h = (h ^ uint64(uint8(pos.Hands[c][t]))) * prime
		}
	}
	h = (h ^ uint64(pos.Turn)) * prime
	if h == 0 {
		h = 1
	}
	return h
}

// entry 는 한 국면에 대해 지금까지 안 것이다.
//
// 증명과 반증은 남은 수(remaining)에 매여 있다. mate 는 찾아낸 詰み의 길이라 그보다 남은 수가
// 많으면 언제나 참이고, noMate 는 「이 수 안에는 詰まない」라 그보다 적으면 언제나 참이다.
// pn·dn 은 at 에서 잰 값이고, 다른 remaining 에서는 쓰지 않는다.
type entry struct {
	pn, dn uint32
	at     int16 // pn·dn 을 잰 remaining
	mate   int16 // 찾은 詰み의 길이. 없으면 -1
	noMate int16 // 이 remaining 까지는 詰まない. 모르면 -1
}

var fresh = entry{pn: 1, dn: 1, at: -1, mate: -1, noMate: -1}

type slot struct {
	h uint64
	e entry
}

// table 은 열린 주소법의 치환표다. 상한에 닿으면 결론이 나지 않은 칸을 한꺼번에 비우고(sweep),
// 그래도 반이 넘게 차 있으면 full 을 세운다. 비우는 기준이 표의 내용뿐이라 같은 국면을 풀면
// 언제나 같은 자리에서 같은 칸이 비워진다.
type table struct {
	slots []slot
	used  int
	max   int // slots 가 커질 수 있는 상한
	full  bool
	// sweeps 는 비운 횟수다. 재는 자리에서만 본다.
	sweeps int
}

func newTable(maxSlots int) *table {
	return &table{slots: make([]slot, min(1<<16, maxSlots)), max: maxSlots}
}

func (t *table) find(h uint64) int {
	mask := len(t.slots) - 1
	i := int(h) & mask
	for t.slots[i].h != 0 && t.slots[i].h != h {
		i = (i + 1) & mask
	}
	return i
}

func (t *table) get(h uint64) (entry, bool) {
	s := t.slots[t.find(h)]
	if s.h == 0 {
		return fresh, false
	}
	return s.e, true
}

func (t *table) put(h uint64, e entry) {
	i := t.find(h)
	if t.slots[i].h == h {
		t.slots[i].e = e
		return
	}
	// 4분의 3을 넘기면 늘린다. 상한이면 새 국면을 적지 않는다(탐색은 full 을 보고 멈춘다).
	if (t.used+1)*4 > len(t.slots)*3 {
		if len(t.slots)*2 <= t.max {
			t.grow()
		} else if t.sweep(); t.used*2 > len(t.slots) {
			t.full = true
			return
		}
		i = t.find(h)
	}
	t.slots[i] = slot{h, e}
	t.used++
}

func (t *table) grow() {
	old := t.slots
	t.slots = make([]slot, len(old)*2)
	for _, s := range old {
		if s.h != 0 {
			t.slots[t.find(s.h)] = s
		}
	}
}

// sweep 은 증명도 반증도 되지 않은 칸을 비운다. 그 칸들의 pn·dn 은 다시 열면 다시 셀 수 있는
// 값이고, 증명·반증은 트리를 지을 때 다시 쓰는 결론이라 남긴다.
func (t *table) sweep() {
	old := t.slots
	t.slots = make([]slot, len(old))
	t.used = 0
	for _, s := range old {
		if s.h != 0 && (s.e.mate >= 0 || s.e.noMate >= 0) {
			t.slots[t.find(s.h)] = s
			t.used++
		}
	}
	t.sweeps++
}
