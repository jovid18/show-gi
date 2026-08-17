package game

import (
	"os"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// **k가 다르면 1위가 몇 번이나 갈리는가.** 개입 문장이 판정의 k=1 PV를 말하고 카드가 k=3의
// 1위를 짚던 자리의 크기다(journal §58 · playtests/2026-08-13-human-1.md §6 #8).
//
//	SHOWGI_MEASURE=1 SHOWGI_USI_CMD=/opt/yaneuraou/run go test ./internal/game/ -run MeasureCardBest -v
//
// 재는 국면은 **사람이 둔 첫 판에서 사람이 두고 난 뒤**의 자리들이다 — 개입 카드가 서는 것과
// 같은 모양(상대 차례)이다. 물러진 수 자체는 기보에 없으므로(롤백됐다) 그 국면을 그대로
// 되만들 수는 없고, 여기서 재는 것은 「그 깊이에서 k가 1위를 바꾸는 빈도」다.
func TestMeasureCardBestDivergence(t *testing.T) {
	if os.Getenv("SHOWGI_MEASURE") == "" {
		t.Skip("SHOWGI_MEASURE 미설정")
	}
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" {
		t.Skip("SHOWGI_USI_CMD 미설정")
	}
	pool, err := usi.NewPool(1, cmd, map[string]string{
		"USI_Hash": "128", "Threads": "1", "FV_SCALE": "24",
		"BookFile": "no_book", "USI_OwnBook": "false",
	})
	if err != nil {
		t.Fatalf("엔진 풀: %v", err)
	}
	defer pool.Close()

	moves := humanOneKifu(t)

	// step 은 몇 手마다 재는가다. 짝수라야 **사람(先手)이 둔 뒤**의 국면만 걸린다.
	const step = 8

	var (
		checked, differed int
		k1Total, k3Total  time.Duration
	)
	for i := 0; i < len(moves); i += step {
		line := moves[:i+1]

		start := time.Now()
		k1, err := pool.SearchMultiPV(t.Context(), shogi.StartSFEN, line, JudgeDepth, 1)
		if err != nil {
			// **여기서 죽지 않는다.** 이 판의 종반은 엔진이 실제로 죽는 자리이고(§56이
			// 5분을 못 설명한 그 구간이다), 재던 것을 통째로 버리면 그 사실만 남고 숫자가
			// 사라진다. 잰 데까지 요약하고 멈춘다.
			t.Logf("%d手 k=1 에서 멈췄다: %v", i+1, err)
			break
		}
		k1Elapsed := time.Since(start)

		// **뒤에 오는 쪽이 치환표를 물려받는다.** 프로덕션도 같은 순서(판정 → cardPV)라
		// 그대로 두지만, 그래서 아래 k=3 소요는 찬 상태의 값보다 짧다.
		start = time.Now()
		k3, err := pool.SearchMultiPV(t.Context(), shogi.StartSFEN, line, JudgeDepth, OtherBranches)
		if err != nil {
			t.Logf("%d手 k=3 에서 멈췄다: %v", i+1, err)
			break
		}
		k3Elapsed := time.Since(start)

		k1Total, k3Total = k1Total+k1Elapsed, k3Total+k3Elapsed

		one, three := k1.Ranked(), k3.Ranked()
		if len(one) == 0 || len(three) == 0 {
			t.Logf("%d手: 후보가 없다 — 세지 않는다", i+1)
			continue
		}
		checked++
		if one[0].Move != three[0].Move {
			differed++
			t.Logf("%3d手  k=1 %-6s (%+5d)  ≠  k=3 %-6s (%+5d)",
				i+1, one[0].Move, one[0].ScoreCp, three[0].Move, three[0].ScoreCp)
		}
	}

	t.Logf("국면 %d개 중 1위가 갈린 것 %d개 (%.0f%%)", checked, differed, 100*float64(differed)/float64(checked))
	t.Logf("평균 소요  k=1 %v · k=3 %v (같은 국면을 이어서 재므로 k=3은 치환표가 더워져 있다)",
		k1Total/time.Duration(checked), k3Total/time.Duration(checked))
}
