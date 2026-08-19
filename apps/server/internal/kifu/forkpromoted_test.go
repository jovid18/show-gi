package kifu

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
)

// 両取り 이름이 生駒에서 왔나 成駒에서 왔나를 가른다. §44 가 341판에서 절반이 成駒라고
// 재 놓고 「龍·馬를 뺄지」를 [미확정] 으로 남긴 자리다(회차 1 #14).
type forkHit struct {
	code  string
	prom  bool
	ply   int
	plies int
}

func forkScanOne(path string) ([]forkHit, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	g, err := ParseCSA(string(raw))
	if err != nil {
		return nil, err
	}
	pos, err := shogi.ParseSFEN(g.StartSFEN)
	if err != nil {
		return nil, err
	}

	var out []forkHit
	for i, u := range g.Moves {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			return nil, err
		}
		mover := pos.Turn
		pos = pos.Apply(m)

		for sq := range pos.Board {
			p := pos.Board[sq]
			if p.Empty() || p.Color() != mover {
				continue
			}
			t, ok := tag.Fork(pos, sq, mover)
			if !ok {
				continue
			}
			pt := p.Type()
			out = append(out, forkHit{
				code:  t.Code,
				prom:  pt == shogi.PromRook || pt == shogi.PromBishop,
				ply:   i + 1,
				plies: len(g.Moves),
			})
		}
	}
	return out, nil
}

func TestScanForkPromotedSplit(t *testing.T) {
	if os.Getenv("SHOWGI_KIFU_SCAN") == "" {
		t.Skip("SHOWGI_KIFU_SCAN 미설정")
	}
	files := floodgateFiles(t, scanSeed(t), scanCount(t))

	hits := make([][]forkHit, len(files))
	var wg sync.WaitGroup
	for i, f := range files {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := forkScanOne(f)
			if err != nil {
				t.Logf("✗ %s — %v", filepath.Base(f), err)
				return
			}
			hits[i] = h
		}()
	}
	wg.Wait()

	type acc struct {
		n, promN  int
		plies     []int
		promPlies []int
	}
	by := map[string]*acc{}
	for _, hs := range hits {
		for _, h := range hs {
			a := by[h.code]
			if a == nil {
				a = &acc{}
				by[h.code] = a
			}
			a.n++
			a.plies = append(a.plies, h.ply)
			if h.prom {
				a.promN++
				a.promPlies = append(a.promPlies, h.ply)
			}
		}
	}
	med := func(v []int) int {
		if len(v) == 0 {
			return 0
		}
		s := append([]int(nil), v...)
		for i := 1; i < len(s); i++ {
			for j := i; j > 0 && s[j] < s[j-1]; j-- {
				s[j], s[j-1] = s[j-1], s[j]
			}
		}
		return s[len(s)/2]
	}
	for code, a := range by {
		t.Logf("%-18s 전체 %5d · 成駒 %5d (%.0f%%) · 중앙 手数 전체 %3d / 成駒 %3d",
			code, a.n, a.promN, 100*float64(a.promN)/float64(a.n), med(a.plies), med(a.promPlies))
	}
}
