package kifu

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/archive"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/handicap"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 판정의 두 탐색이 지평이 한 수 다른 것(journal §41)이 낙폭을 얼마나 옮기는지 잰다.
//
// 착수 전은 N−1手 국면 depth 12, 착수 후는 N手 국면 depth 12라 착수 후가 한 手 더 본다.
// 착수 후 탐색의 depth 11 줄을 쓰면 두 쪽의 끝이 같은 手数에서 멈춘다. 두 낙폭을 手마다 견준다.
//
// 최선수를 둔 手가 기준이다. 그 手의 낙폭은 정의상 0이어야 하고, 어긋난 만큼이 오차다.
//
//	SHOWGI_MEASURE=1 SHOWGI_RANK_KIFU=~/show-gi-kifu/rank-anchors.txt \
//	SHOWGI_USI_CMD=/opt/yaneuraou/run \
//	SHOWGI_TEST_DATABASE_URL='postgres://…' \
//	go test ./internal/kifu/ -run MeasureJudgeHorizon -v -timeout 6h
func TestMeasureJudgeHorizon(t *testing.T) {
	manifest := os.Getenv("SHOWGI_RANK_KIFU")
	dbURL := os.Getenv("SHOWGI_TEST_DATABASE_URL")
	enginePath := os.Getenv("SHOWGI_USI_CMD")
	if manifest == "" || os.Getenv("SHOWGI_MEASURE") == "" {
		t.Skip("SHOWGI_RANK_KIFU + SHOWGI_MEASURE 가 있어야 돈다")
	}
	if dbURL == "" || enginePath == "" {
		t.Skip("SHOWGI_TEST_DATABASE_URL 과 SHOWGI_USI_CMD 가 필요하다")
	}
	entries, err := loadRankManifest(manifest)
	if err != nil || len(entries) == 0 {
		t.Fatalf("목록을 못 읽었다: %v", err)
	}

	ctx := context.Background()
	st, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	// SHOWGI_HORIZON_LIVE 가 있으면 캐시를 거치지 않고 엔진 넷으로 새로 잰다. 캐시가 되살린
	// 깊이별 줄은 저장 모양 때문에 깊이가 어긋날 수 있다(journal §142).
	live := os.Getenv("SHOWGI_HORIZON_LIVE") != ""
	size := 1
	if live {
		size = 4
	}
	pool, err := usi.NewPool(size, enginePath, map[string]string{"USI_Hash": "256", "Threads": "1"})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var searcher game.Searcher = pool
	if !live {
		// 프로덕션과 같은 경로다.
		a := archive.Wrap(pool, st)
		defer a.Wait()
		searcher = a
	}

	const depth = game.JudgeDepth
	levels := []intervene.Level{intervene.Beginner, intervene.Novice, intervene.Intermediate}

	type sample struct {
		label, where string
		best         bool
		dA, dE       float64
		cpA, cpE     int // 착수 후 cp(둔 쪽 관점). 詰み이면 판정에서 빠진다
		cpOK         bool
	}
	var all []sample
	missing := 0

	// 국면마다 한 번만 잰다. N手 국면의 결과가 N手의 「착수 후」이자 N+1手의 「착수 전」이다.
	results := make([][]usi.SearchResult, len(entries))
	var wg sync.WaitGroup
	sem := make(chan struct{}, size)
	var failed atomic.Bool
	for gi, e := range entries {
		start := e.game.StartSFEN
		if start == "" {
			start = shogi.StartSFEN
		}
		results[gi] = make([]usi.SearchResult, len(e.game.Moves)+1)
		for i := range results[gi] {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				r, err := searcher.SearchDepth(ctx, start, e.game.Moves[:i], depth)
				if err != nil {
					t.Errorf("%s %d手: %v", e.source, i, err)
					failed.Store(true)
				}
				results[gi][i] = r
			}()
		}
	}
	wg.Wait()
	if failed.Load() {
		t.FailNow()
	}

	for gi, e := range entries {
		start := e.game.StartSFEN
		if start == "" {
			start = shogi.StartSFEN
		}
		pos, err := shogi.ParseSFEN(start)
		if err != nil {
			t.Fatalf("%s: %v", e.source, err)
		}
		for i, u := range e.game.Moves {
			ply := i + 1
			mover := pos.Turn
			label := e.senteLabel
			if mover == shogi.White {
				label = e.goteLabel
			}
			m, err := shogi.ParseUSIMove(u)
			if err != nil {
				t.Fatalf("%s %d手: %v", e.source, ply, err)
			}
			best, after := results[gi][i], results[gi][ply]
			pos = pos.Apply(m)

			shorter, ok := after.ScoreAtDepth(depth - 1)
			if !ok {
				missing++
				continue
			}
			base := handicap.BaselineCpFor(start, mover)
			a, b := after.Score.Neg(), shorter.Neg()
			s := sample{
				label: label,
				where: fmt.Sprintf("%s %d手", shortSource(e.source), ply),
				best:  best.Best == u,
				dA:    intervene.WinRateOf(best.Score, base) - intervene.WinRateOf(a, base),
				dE:    intervene.WinRateOf(best.Score, base) - intervene.WinRateOf(b, base),
			}
			ca, okA := a.Centipawns()
			cb, okB := b.Centipawns()
			s.cpA, s.cpE, s.cpOK = ca, cb, okA && okB
			all = append(all, s)
		}
	}

	stats := func(name string, xs []sample) {
		var sumA, sumE, absA, absE, sumCp float64
		var nCp, aBigger int
		for _, s := range xs {
			sumA += s.dA
			sumE += s.dE
			absA += math.Abs(s.dA)
			absE += math.Abs(s.dE)
			if s.dA > s.dE {
				aBigger++
			}
			if s.cpOK {
				sumCp += float64(s.cpE - s.cpA)
				nCp++
			}
		}
		n := float64(len(xs))
		fmt.Fprintf(os.Stderr, "%-14s n=%4d  평균 낙폭 A %+.4f · E %+.4f  |낙폭| A %.4f · E %.4f  A>E %4.1f%%  착수 후 cp(E−A) 평균 %+.1f\n",
			name, len(xs), sumA/n, sumE/n, absA/n, absE/n, 100*float64(aBigger)/n, sumCp/float64(max(nCp, 1)))
	}

	var bestOnes []sample
	for _, s := range all {
		if s.best {
			bestOnes = append(bestOnes, s)
		}
	}
	fmt.Fprintf(os.Stderr, "\nlive=%v  A = 지금(착수 후 depth %d) · E = 착수 후 depth %d 줄. depth %d 줄이 없어 뺀 手 %d\n",
		live, depth, depth-1, depth-1, missing)
	stats("전체", all)
	stats("최선수를 둔 手", bestOnes)

	fmt.Fprintf(os.Stderr, "\n%-14s %6s %6s %6s %6s\n", "레벨(임계치)", "A 개입", "E 개입", "A만", "E만")
	var flips []string
	for _, lv := range levels {
		th := lv.Threshold()
		var nA, nE, onlyA, onlyE int
		for _, s := range all {
			ia, ie := s.dA > th, s.dE > th
			if ia {
				nA++
			}
			if ie {
				nE++
			}
			switch {
			case ia && !ie:
				onlyA++
				if lv == intervene.Beginner {
					flips = append(flips, fmt.Sprintf("A만  %-8s %-24s A %.3f E %.3f", s.label, s.where, s.dA, s.dE))
				}
			case ie && !ia:
				onlyE++
				if lv == intervene.Beginner {
					flips = append(flips, fmt.Sprintf("E만  %-8s %-24s A %.3f E %.3f", s.label, s.where, s.dA, s.dE))
				}
			}
		}
		fmt.Fprintf(os.Stderr, "%-14.2f %6d %6d %6d %6d\n", th, nA, nE, onlyA, onlyE)
	}
	sort.Strings(flips)
	fmt.Fprintf(os.Stderr, "\nBeginner 임계치에서 갈린 手 %d건\n", len(flips))
	for _, f := range flips {
		fmt.Fprintln(os.Stderr, f)
	}

	// 옛 캐시가 되살리던 depth 2. 최선수의 깊이별 줄을 빈 깊이 없이 당겨 쓰면 두 번째 칸이
	// depth 2 로 읽혔다. 새로 잰 결과에서 그 칸과 실제 depth 2 를 견준다(journal §142).
	if live {
		var n, gappy, wrong2 int
		for gi := range results {
			for _, r := range results[gi][1:] {
				byDepth := r.EvalByDepth(r.Best)
				if len(byDepth) == 0 || byDepth[len(byDepth)-1].Depth != r.Depth {
					continue
				}
				n++
				if len(byDepth) < r.Depth {
					gappy++
				}
				want, ok := r.ScoreAtDepth(game.ShallowDepth)
				if len(byDepth) >= game.ShallowDepth && ok && byDepth[game.ShallowDepth-1].Score != want {
					wrong2++
				}
			}
		}
		fmt.Fprintf(os.Stderr, "\n최선수 깊이별 줄 %d국면 중 빈 깊이가 있는 것 %d (%.1f%%), 옛 캐시의 depth %d 가 실제와 다른 것 %d (%.1f%%)\n",
			n, gappy, 100*float64(gappy)/float64(n), game.ShallowDepth, wrong2, 100*float64(wrong2)/float64(n))
	}

	// 최선수 手의 낙폭 분포. 0에서 얼마나 떨어져 있나.
	for _, v := range []struct {
		name string
		get  func(sample) float64
	}{{"A", func(s sample) float64 { return s.dA }}, {"E", func(s sample) float64 { return s.dE }}} {
		xs := make([]float64, len(bestOnes))
		for i, s := range bestOnes {
			xs[i] = v.get(s)
		}
		sort.Float64s(xs)
		q := func(p float64) float64 { return xs[int(p*float64(len(xs)-1))] }
		fmt.Fprintf(os.Stderr, "최선수 手 낙폭 %s  p05 %+.3f p25 %+.3f p50 %+.3f p75 %+.3f p95 %+.3f\n",
			v.name, q(0.05), q(0.25), q(0.5), q(0.75), q(0.95))
	}
}
