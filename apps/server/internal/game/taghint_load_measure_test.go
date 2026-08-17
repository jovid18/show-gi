package game

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 手筋 힌트 게이트가 **한 판에 얼마를 쓰는가**. 엔진도 DB도 안 쓰고 룰 엔진만 돌린다.
//
//	SHOWGI_MEASURE=1 go test ./internal/game/ -run MeasureTagHintLoad -v
//
// 재는 판은 사람이 둔 첫 판이다(`games.id=397` · 298手 · 先手 · docs/playtests/2026-08-13-human-1.md).
// 그 판이 종반에 멈췄고, 여기서 나오는 두 숫자가 그 이유의 절반이다 — **몇 번 여는가**와
// **한 번이 얼마인가**. 나머지 절반은 엔진 쪽 시한이고 그건 deadline_test.go 다.

// humanOneKifu 는 그 판의 기보다. 로컬 DB가 초기화되면 위 문서와 이 파일만 남는다.
func humanOneKifu(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("testdata/human-1.usi")
	if err != nil {
		t.Fatalf("기보를 못 읽었다: %v", err)
	}
	return strings.Fields(string(raw))
}

func TestMeasureTagHintLoad(t *testing.T) {
	if os.Getenv("SHOWGI_MEASURE") == "" {
		t.Skip("SHOWGI_MEASURE 미설정")
	}
	moves := humanOneKifu(t)
	const human = shogi.Black

	pos, err := shogi.ParseSFEN(shogi.StartSFEN)
	if err != nil {
		t.Fatalf("초기 국면: %v", err)
	}

	var (
		turns              int           // 사람 차례 手数
		openEvery          int           // 쿨다운 없이 게이트가 열린 手数 (옛 자)
		searchesEvery      int           // 그때 걸리는 엔진 탐색 수
		openCooled         int           // 쿨다운을 「물어본 자리」에서 잰 것 (지금)
		searchesCooled     int           //
		total, worst       time.Duration // 룰 필터 비용
		worstPly, worstLeg int
		lastAsk            int
		cands              int // 후보 총수
		mostCands          int
	)

	for i, u := range moves {
		if pos.Turn == human {
			ply := i
			turns++

			start := time.Now()
			opts := tesujiOptions(pos, human)
			spent := time.Since(start)
			total += spent
			if spent > worst {
				worst, worstPly, worstLeg = spent, ply, len(pos.LegalMoves())
			}

			if len(opts) > 0 {
				openEvery++
				// **탐색은 후보 수와 무관하게 한 번이다**(gateTesujiOptions). 옛 자는
				// `1 + min(후보, 상한)` 이었고 그 숫자가 §56의 586·131이다 — §74에서 바뀌었다.
				searchesEvery++
				cands += len(opts)
				mostCands = max(mostCands, len(opts))
				if lastAsk == 0 || ply-lastAsk >= TagHintCooldown {
					openCooled++
					searchesCooled++
					lastAsk = ply
				}
			}
		}
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			t.Fatalf("%d手目 %q: %v", i+1, u, err)
		}
		pos = pos.Apply(m)
	}

	t.Logf("사람 차례 %d회 · 게이트가 열린 手数 %d → %d · 엔진 탐색 %d회 → %d회",
		turns, openEvery, openCooled, searchesEvery, searchesCooled)
	t.Logf("룰 필터 한 번: 평균 %v · 최장 %v (%d手目, 합법수 %d)",
		total/time.Duration(turns), worst, worstPly, worstLeg)
	t.Logf("열린 手数당 후보: 평균 %.1f개 · 최다 %d개 — **상한이 없다**. 전부 한 탐색으로 답한다",
		float64(cands)/float64(openEvery), mostCands)
}

// humanThreeKifu 는 회차 3의 기보다(`games.id=863` · 208手 · 사람이 **後手**).
// 회차 1과 갈라 두는 이유는 journal §76 — 그 판이 §74 뒤의 첫 사람 판이다.
func humanThreeKifu(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile("testdata/human-3.usi")
	if err != nil {
		t.Fatalf("기보를 못 읽었다: %v", err)
	}
	return strings.Fields(string(raw))
}
