package kifu

import (
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
)

// 囲い 이름이 **몇 手 동안 살아 있었나**를 잰다.
//
//	SHOWGI_KIFU_SCAN=1 SHOWGI_KIFU_GAMES=317 go test ./internal/kifu/ -run ScanCastleSpans -v
type segment struct {
	code       string
	start, end int
}

type segScan struct {
	name string
	segs map[shogi.Color][]segment
	err  error
}

func castleOf(pos shogi.Position, c shogi.Color, mine, theirs []string) string {
	for _, tg := range tag.Detect(tag.Input{
		Pos: pos, Color: c, PlayerMoves: mine, OpponentMoves: theirs,
	}) {
		if tg.Kind == tag.KindCastle {
			return tg.Code
		}
	}
	return ""
}

func segScanOne(path string) segScan {
	out := segScan{name: filepath.Base(path), segs: map[shogi.Color][]segment{}}

	raw, err := os.ReadFile(path)
	if err != nil {
		out.err = err
		return out
	}
	g, err := ParseCSA(string(raw))
	if err != nil {
		out.err = err
		return out
	}
	pos, err := shogi.ParseSFEN(g.StartSFEN)
	if err != nil {
		out.err = err
		return out
	}

	moves := map[shogi.Color][]string{}
	cur := map[shogi.Color]string{}
	for i, u := range g.Moves {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			out.err = err
			return out
		}
		mover := pos.Turn
		pos = pos.Apply(m)
		moves[mover] = append(moves[mover], u)
		ply := i + 1

		for _, c := range []shogi.Color{shogi.Black, shogi.White} {
			got := castleOf(pos, c, moves[c], moves[c.Other()])
			if got == cur[c] {
				if n := len(out.segs[c]); n > 0 && got != "" {
					out.segs[c][n-1].end = ply
				}
				continue
			}
			cur[c] = got
			if got != "" {
				out.segs[c] = append(out.segs[c], segment{got, ply, ply})
			}
		}
	}
	return out
}

func TestScanCastleSpans(t *testing.T) {
	if os.Getenv("SHOWGI_KIFU_SCAN") == "" {
		t.Skip("SHOWGI_KIFU_SCAN 미설정")
	}
	files := floodgateFiles(t, scanSeed(t), scanCount(t))

	results := make([]segScan, len(files))
	var wg sync.WaitGroup
	for i, f := range files {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = segScanOne(f)
		}()
	}
	wg.Wait()

	var (
		games, failed int
		total, short  int
		noCastle      int
		shortByCode   = map[string]int{}
		allByCode     = map[string]int{}
		lens          []int
	)
	for _, r := range results {
		if r.err != nil {
			failed++
			continue
		}
		games++
		any := false
		for _, c := range []shogi.Color{shogi.Black, shogi.White} {
			for _, s := range r.segs[c] {
				any = true
				total++
				allByCode[s.code]++
				n := s.end - s.start + 1
				lens = append(lens, n)
				if n <= 2 {
					short++
					shortByCode[s.code]++
					if short <= 15 {
						t.Logf("짧음 %-18s %s %d~%d手 (%d手)", s.code, r.name, s.start, s.end, n)
					}
				}
			}
		}
		if !any {
			noCastle++
		}
	}
	sort.Ints(lens)
	med := 0
	if len(lens) > 0 {
		med = lens[len(lens)/2]
	}
	t.Logf("")
	t.Logf("판 %d (실패 %d) · 囲い 구간 %d개 · 2手 이하 %d개 (%.1f%%) · 길이 중앙 %d手",
		games, failed, total, short, 100*float64(short)/float64(max(total, 1)), med)
	t.Logf("양쪽 다 囲い가 안 붙은 판: %d / %d", noCastle, games)

	type kv struct {
		c     string
		n, sh int
	}
	var ks []kv
	for c, n := range allByCode {
		ks = append(ks, kv{c, n, shortByCode[c]})
	}
	sort.Slice(ks, func(i, j int) bool { return ks[i].n > ks[j].n })
	for _, e := range ks {
		t.Logf("  %-20s 전체 %4d · 2手 이하 %3d", e.c, e.n, e.sh)
	}
}
