package kifu

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/archive"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 好手 판정이 사람 기보에서 얼마나 자주 걸리는지 잰다(journal §141). 段級 앵커와 같은
// 목록 파일을 읽고, 프로덕션과 같은 판정 경로로 手마다 다시 묻는다.
//
// 상수는 고치지 않는다. 임계치별 빈도와 걸린 수의 목록을 찍고, 사람이 그 목록을 보고
// intervene.GoodGapWin 을 정한다.
//
//	SHOWGI_MEASURE=1 SHOWGI_RANK_KIFU=~/show-gi-kifu/rank-anchors.txt \
//	SHOWGI_USI_CMD=/opt/yaneuraou/run SHOWGI_MATE_CMD=/opt/yaneuraou/run-mate \
//	SHOWGI_TEST_DATABASE_URL='postgres://…' \
//	go test ./internal/kifu/ -run MeasureGoodMove -v -timeout 6h
func TestMeasureGoodMove(t *testing.T) {
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
	pool, err := usi.NewPool(1, enginePath, map[string]string{"USI_Hash": "256", "Threads": "1"})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	searcher := archive.Wrap(pool, st)
	defer searcher.Wait()

	var mate game.MateSearcher
	if cmd := os.Getenv("SHOWGI_MATE_CMD"); cmd != "" {
		matePool, err := usi.NewPool(1, cmd, map[string]string{"USI_Hash": "256", "Threads": "1"})
		if err != nil {
			t.Fatalf("詰み 엔진: %v", err)
		}
		defer matePool.Close()
		mate = matePool
	}
	analyst := game.NewEngineAnalyst(searcher, mate, intervene.Beginner)

	cuts := []float64{0.05, 0.10, 0.15, 0.20, 0.25, 0.30}
	type tally struct {
		judged, blunder, best, asked int
		obvious                      map[game.Obvious]int
		over                         []int
	}
	byLabel := map[string]*tally{}
	var found []string

	for _, e := range entries {
		start := e.game.StartSFEN
		if start == "" {
			start = shogi.StartSFEN
		}
		pos, err := shogi.ParseSFEN(start)
		if err != nil {
			t.Fatalf("%s: %v", e.source, err)
		}
		prevTo := -1
		for i, u := range e.game.Moves {
			ply := i + 1
			m, err := shogi.ParseUSIMove(u)
			if err != nil {
				t.Fatalf("%s %d手: %v", e.source, ply, err)
			}
			label := e.senteLabel
			if pos.Turn == shogi.White {
				label = e.goteLabel
			}
			ja := pos.MoveJa(m, prevTo)

			tl := byLabel[label]
			if tl == nil {
				tl = &tally{obvious: map[game.Obvious]int{}, over: make([]int, len(cuts))}
				byLabel[label] = tl
			}
			j, err := analyst.Judge(ctx, start, e.game.Moves[:ply], ply)
			if err == nil {
				game.CheckGood(ctx, analyst, &j)
				tl.judged++
				switch {
				case j.Verdict.Kind != intervene.KindNone:
					tl.blunder++
				case j.BestUSI == u:
					tl.best++
					if j.Obvious != game.ObviousNone {
						tl.obvious[j.Obvious]++
					}
				}
				if j.GoodAsked {
					tl.asked++
					for c, cut := range cuts {
						if j.GoodGap >= cut {
							tl.over[c]++
						}
					}
					if j.GoodGap >= cuts[0] {
						found = append(found, fmt.Sprintf("%-8s %4d手 %-10s gap %.3f  %s",
							label, ply, ja, j.GoodGap, shortSource(e.source)))
					}
				}
			} else {
				t.Errorf("%s %d手: %v", e.source, ply, err)
			}
			pos = pos.Apply(m)
			prevTo = int(m.To)
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

	head := fmt.Sprintf("\n%-8s %5s %5s %5s %6s %6s %6s %6s %5s", "라벨", "手", "개입", "最善", "王手受", "同", "タダ取", "得取", "물음")
	for _, c := range cuts {
		head += fmt.Sprintf(" ≥%.2f", c)
	}
	fmt.Fprintln(os.Stderr, head)
	fmt.Fprintln(os.Stderr, strings.Repeat("─", len(head)+10))
	for _, l := range labels {
		tl := byLabel[l]
		row := fmt.Sprintf("%-8s %5d %5d %5d %6d %6d %6d %6d %5d", l, tl.judged, tl.blunder, tl.best,
			tl.obvious[game.ObviousEvasion], tl.obvious[game.ObviousRecapture], tl.obvious[game.ObviousFreeCapture],
			tl.obvious[game.ObviousGainingCapture], tl.asked)
		for c := range cuts {
			row += fmt.Sprintf(" %5d", tl.over[c])
		}
		fmt.Fprintln(os.Stderr, row)
	}
	fmt.Fprintf(os.Stderr, "\n차선수와의 승률 차가 %.2f 이상인 最善手 %d건\n", cuts[0], len(found))
	for _, f := range found {
		fmt.Fprintln(os.Stderr, f)
	}
}
