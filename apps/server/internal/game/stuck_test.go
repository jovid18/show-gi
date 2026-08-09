package game

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 실제 플레이 테스트 기보. 77·81수째에서 「생각한 수가 전부 물러졌다」고 보고됐다.
//
// **보고는 전수 조사가 아니다** — 직접 둬본 몇 수가 전부 걸렸다는 것이다. 아래 조사가
// 합법수 전부를 돌려 그 보고를 설명한다.
const stuckKifu = `▲7六歩 △7二銀 ▲6八飛 △5二金右 ▲5八金左 △9四歩 ▲4八玉 △2四歩
▲3八玉 △2五歩 ▲2八玉 △3二金 ▲3八銀 △3四歩 ▲6六歩 △3三角
▲9六歩 △1四歩 ▲1六歩 △2二銀 ▲5六歩 △8四歩 ▲5七金 △4二金右
▲7八銀 △8五歩 ▲7七角 △2三銀 ▲4六歩 △7四歩 ▲4七金 △4一玉
▲6七銀 △5四歩 ▲6五歩 △7三銀 ▲6六銀 △3一玉 ▲5五歩 △同歩
▲同銀 △6二銀 ▲6四歩 △8六歩 ▲6三歩成 △5一銀 ▲8六歩 △6二歩
▲同と △同銀 ▲6三歩 △7三銀 ▲5四銀 △2二玉 ▲3三角成 △同桂
▲7一角 △8三飛 ▲6二歩成 △8六飛 ▲6三飛成 △5三歩 ▲同銀成 △8九飛成
▲4二成銀 △同金 ▲5二と △3二金 ▲5三角成 △6二歩 ▲5四龍 △3五桂
▲3六金 △6七角 ▲5八金打 △7六角成 ▲5九歩 △5四馬 ▲同馬 △9九龍`

// kifuToUSI 는 棋譜 표기를 USI로 되돌린다.
//
// 손으로 옮기지 않는다 — **합법수마다 우리 MoveJa 를 돌려 표기가 일치하는 것을 찾는다.**
// 표기를 두 벌로 만들면 어긋났을 때 어느 쪽이 맞는지 알 수 없다(§6 ④와 같은 이유).
func kifuToUSI(t *testing.T, kifu string) ([]string, shogi.Position) {
	t.Helper()

	pos, err := shogi.ParseSFEN(shogi.StartSFEN)
	if err != nil {
		t.Fatalf("초기 국면: %v", err)
	}
	prevTo := -1
	var usis []string

	for i, ja := range strings.Fields(kifu) {
		var found *shogi.Move
		for _, m := range pos.LegalMoves() {
			if pos.MoveJa(m, prevTo) == ja {
				got := m
				if found != nil {
					t.Fatalf("%d수 %q 가 두 수에 맞는다: %s, %s", i+1, ja, found.USI(), got.USI())
				}
				found = &got
			}
		}
		if found == nil {
			t.Fatalf("%d수 %q 에 맞는 합법수가 없다", i+1, ja)
		}
		usis = append(usis, found.USI())
		pos = pos.Apply(*found)
		prevTo = int(found.To)
	}
	return usis, pos
}

func TestKifuRoundTrips(t *testing.T) {
	usis, pos := kifuToUSI(t, stuckKifu)
	if len(usis) != 80 {
		t.Fatalf("80수여야 한다: %d", len(usis))
	}
	if pos.Turn != shogi.Black {
		t.Fatalf("81수째는 先手 차례여야 한다: %v", pos.Turn)
	}
	t.Logf("81수째 국면: %s", pos.SFEN())
	t.Logf("합법수 %d개", len(pos.LegalMoves()))
}

// TestRealEngineStuckPosition 은 **「무엇을 둬도 블런더」가 사실인지** 잰다.
//
// 플레이 테스트에서 나온 보고이고, 사실이라면 판정식이 「이 수가 얼마나 나쁜가」만 보고
// 「더 나은 선택지가 실제로 있었나」를 안 보기 때문이다.
//
//	SHOWGI_USI_CMD=/opt/yaneuraou/run go test ./internal/game/ -run RealEngineStuck -v
func TestRealEngineStuckPosition(t *testing.T) {
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

	allUSIs, _ := kifuToUSI(t, stuckKifu)

	// 플레이 테스트에서 헤맸다고 보고된 두 자리다.
	for _, ply := range []int{77, 81} {
		surveyPly(t, pool, allUSIs, ply)
	}
}

// surveyPly 는 그 국면의 **합법수 전부**를 판정에 돌린다.
func surveyPly(t *testing.T, pool *usi.Pool, allUSIs []string, ply int) {
	t.Helper()

	usis := allUSIs[:ply-1]
	pos, err := positionAfter(shogi.StartSFEN, usis)
	if err != nil {
		t.Fatalf("국면 복원: %v", err)
	}

	// 착수 **전** 최선수는 모든 후보에 공통이라 한 번만 구한다.
	before, err := pool.SearchDepth(t.Context(), shogi.StartSFEN, usis, JudgeDepth)
	if err != nil {
		t.Fatalf("착수 전 탐색: %v", err)
	}

	legal := pos.LegalMoves()
	counts := map[intervene.Level]int{}
	cats := map[intervene.Category]int{}

	// **초심자가 고려할 만한 수**를 얕은 평가로 근사한다. depth 2에서 좋아 보이는 수다.
	// 「모든 수가 블런더는 아니었지만 내가 생각한 수는 블런더였다」가 이 줄에 걸린다.
	plausible, plausibleBlunder, haveShallow := 0, 0, 0
	var samples []string
	// 그중 몇 개가 반전 폭 임계치별로 shallow_trap 이 되는가
	reversalHits := map[int]int{}

	// 반박 수순이 실제로 몇 수가 되는가. **길이를 상수로 박지 않기로 한 근거**이고,
	// 여기서 전부 1수로 쪼그라들면 이 기능이 겨냥한 자리(§17)를 못 덮는다는 뜻이다.
	lineLen := map[int]int{}
	var lineSamples []string

	for _, m := range legal {
		next := append(append([]string{}, usis...), m.USI())
		after, err := pool.SearchDepth(t.Context(), shogi.StartSFEN, next, JudgeDepth)
		if err != nil {
			t.Fatalf("%s: %v", m.USI(), err)
		}
		in := intervene.Input{
			BestCp:   before.ScoreCp,
			AfterCp:  -after.ScoreCp,
			Features: MoveFeatures(pos, m),
		}
		if cp, ok := after.ScoreAtDepth(ShallowDepth); ok {
			in.Features.ShallowCp, in.Features.HasShallow = -cp, true
		}

		blunder := false
		for _, lv := range []intervene.Level{intervene.Beginner, intervene.Novice, intervene.Intermediate} {
			in.Level = lv
			if v := intervene.Judge(in); v.Kind == intervene.KindBlunder {
				counts[lv]++
				if lv == intervene.Beginner {
					cats[v.Category]++
					blunder = true
				}
			}
		}

		if blunder {
			_, line := refutationLine(shogi.StartSFEN, next, after.PV, RefutationPlies)
			lineLen[len(line)]++
			if len(line) > 1 && len(lineSamples) < 6 {
				var ja []string
				for _, mv := range line {
					ja = append(ja, mv.Ja)
				}
				lineSamples = append(lineSamples, fmt.Sprintf("%s → %s", m.USI(), strings.Join(ja, " ")))
			}
		}

		if in.Features.HasShallow {
			haveShallow++
			if len(samples) < 6 {
				samples = append(samples, fmt.Sprintf("%s shallow=%+d deep=%+d", m.USI(), in.Features.ShallowCp, in.AfterCp))
			}
		}
		if in.Features.HasShallow && in.Features.ShallowCp > 0 {
			plausible++
			if blunder {
				plausibleBlunder++
				for _, th := range []int{100, 200, 300, 500} {
					if in.AfterCp < 0 && in.Features.ShallowCp-in.AfterCp >= th {
						reversalHits[th]++
					}
				}
			}
		}
	}

	n := len(legal)
	fmt.Printf("\n=== %d수째 · 합법수 %d개 · 최선 %s (%+dcp) ===\n", ply, n, before.Best, before.ScoreCp)
	for _, lv := range []intervene.Level{intervene.Beginner, intervene.Novice, intervene.Intermediate} {
		fmt.Printf("  임계치 %.2f  블런더 %3d/%d (%.0f%%)\n",
			lv.Threshold(), counts[lv], n, 100*float64(counts[lv])/float64(n))
	}
	fmt.Printf("  카테고리(입문): %v\n", cats)
	fmt.Printf("  얕게 봐서 괜찮아 보이는 수 %d개 중 블런더 %d개 (%.0f%%)\n",
		plausible, plausibleBlunder, 100*float64(plausibleBlunder)/float64(max(plausible, 1)))
	fmt.Printf("  그중 shallow_trap 이 될 것: 100cp %d · 200cp %d · 300cp(현재) %d · 500cp %d\n",
		reversalHits[100], reversalHits[200], reversalHits[300], reversalHits[500])
	fmt.Printf("  **얕은 평가(depth %d)가 잡힌 수: %d/%d**\n", ShallowDepth, haveShallow, n)
	for _, s := range samples {
		fmt.Printf("    %s\n", s)
	}
	fmt.Printf("  반박 수순 길이(입문 블런더 %d개): ", counts[intervene.Beginner])
	for i := 1; i <= RefutationPlies; i++ {
		if lineLen[i] > 0 {
			fmt.Printf("%d수 %d개  ", i, lineLen[i])
		}
	}
	fmt.Println()
	for _, s := range lineSamples {
		fmt.Printf("    %s\n", s)
	}
}
