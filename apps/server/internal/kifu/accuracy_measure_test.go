package kifu

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/accuracy"
	"github.com/jovid18/show-gi/apps/server/internal/archive"
	"github.com/jovid18/show-gi/apps/server/internal/eval"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 精度(internal/accuracy)가 段級 앵커 기보에서 급수별로 얼마인지 잰다(journal §144).
// 국면마다 판정과 같은 깊이로 한 번 재고, 그 값을 先手 관점으로 옮겨 판마다 센다.
//
//	SHOWGI_MEASURE=1 SHOWGI_RANK_KIFU=~/show-gi-kifu/rank-anchors.txt \
//	SHOWGI_USI_CMD=/opt/yaneuraou/run SHOWGI_TEST_DATABASE_URL='postgres://…' \
//	go test ./internal/kifu/ -run MeasureAccuracy -v -timeout 6h
func TestMeasureAccuracy(t *testing.T) {
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
	const engines = 4
	pool, err := usi.NewPool(engines, enginePath, map[string]string{"USI_Hash": "256", "Threads": "1"})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	searcher := archive.Wrap(pool, st)
	defer searcher.Wait()

	// scores[g][i] 는 g판 i+1手 뒤의 先手 관점 값이다. 저장되는 game_moves 와 같은 모양이다.
	scores := make([][]*eval.Score, len(entries))
	var wg sync.WaitGroup
	sem := make(chan struct{}, engines)
	for gi, e := range entries {
		start := anchorStart(e)
		scores[gi] = make([]*eval.Score, len(e.game.Moves))
		for i := range e.game.Moves {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				r, err := searcher.SearchDepth(ctx, start, e.game.Moves[:i+1], game.JudgeDepth)
				if err != nil {
					t.Errorf("%s %d手: %v", e.source, i+1, err)
					return
				}
				// 탐색은 수번 관점이다. i+1手 뒤의 수번은 1手目를 둔 쪽의 반대다.
				sc := r.Score
				pos, _ := shogi.ParseSFEN(start)
				if (pos.Turn == shogi.Black) == (i%2 == 0) {
					sc = sc.Neg()
				}
				scores[gi][i] = &sc
			}()
		}
	}
	wg.Wait()

	byLabel := map[string][]float64{}
	for gi, e := range entries {
		for _, side := range []struct {
			c     shogi.Color
			label string
		}{{shogi.Black, e.senteLabel}, {shogi.White, e.goteLabel}} {
			if acc, ok := accuracy.Game(anchorStart(e), side.c, scores[gi]); ok {
				byLabel[side.label] = append(byLabel[side.label], acc)
			}
		}
	}
	labels := make([]string, 0, len(byLabel))
	for l := range byLabel {
		labels = append(labels, l)
	}
	sort.Slice(labels, func(a, b int) bool {
		x, _ := rankOrdinal(labels[a])
		y, _ := rankOrdinal(labels[b])
		return x < y
	})
	fmt.Fprintf(os.Stderr, "\n%-8s %4s %7s %7s %7s  %s\n", "라벨", "자리", "평균", "SD", "범위", "판마다")
	for _, l := range labels {
		xs := byLabel[l]
		var sum float64
		for _, x := range xs {
			sum += x
		}
		mean := sum / float64(len(xs))
		var sq float64
		for _, x := range xs {
			sq += (x - mean) * (x - mean)
		}
		sort.Float64s(xs)
		fmt.Fprintf(os.Stderr, "%-8s %4d %7.1f %7.1f %3.0f–%-3.0f  %.1f\n", l, len(xs), mean, math.Sqrt(sq/float64(len(xs))), xs[0], xs[len(xs)-1], xs)
	}
}

func anchorStart(e rankEntry) string {
	if e.game.StartSFEN == "" {
		return shogi.StartSFEN
	}
	return e.game.StartSFEN
}
