package intervene

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 기록된 대국을 **상수를 바꿔 가며 다시 채점한다.**
//
// 엔진을 안 부른다. `game_moves.eval_cp` 에 남은 원본 cp와 `interventions.delta_win` 만
// 읽으므로, 이 패키지가 엔진을 모른다는 성질이 측정에도 그대로 남는다(CLAUDE.md).
// **엔진을 부르기 시작하면 상수를 흔들어 보는 데 엔진이 필요해진다** — 그러면 이
// 패키지가 그렇게 생긴 이유가 사라진다.
//
// `SHOWGI_MEASURE` 를 안 본다. 다른 `TestMeasure*` 는 엔진을 돌려 몇 분이 걸리지만
// 이것은 질의 몇 개라 초 단위다.
//
//	SHOWGI_TEST_DATABASE_URL='postgres://showgi:showgi@localhost:5432/showgi' \
//	  go test ./internal/intervene/ -run MeasureCalibration -v
//
// **로컬 DB에는 짧은 테스트 대국밖에 없다.** 실제 값은 기록이 쌓인 DB를 가리켜야
// 나온다(docs/06-status.md §19·§39).
func TestMeasureCalibrationFromRecords(t *testing.T) {
	url := os.Getenv("SHOWGI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SHOWGI_TEST_DATABASE_URL 미설정 — 재채점 건너뜀")
	}
	s, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(s.Close)

	games, err := s.ListGames(t.Context(), 500)
	if err != nil {
		t.Fatalf("ListGames: %v", err)
	}

	var (
		all    []sample
		bands  []bandRow
		scored []scoredGame
		// stuckRuns 는 **평가치와 무관하게 전부 센다.** 계단이 몇 번 열렸나는
		// 개입의 手数만 있으면 되고 cp가 필요 없다 — 여기에 재채점의 조건을 씌우면
		// 평가치 이전에 둔 판이 통째로 빠진다. **실제로 그렇게 틀렸다**(§39).
		// 계단이 마지막 칸까지 열린 국면이 전부 그 옛 판에 있었다.
		stuckRuns  = map[int]int{}
		stuckGames int
	)
	for _, g := range games {
		rec, err := s.GameRecord(t.Context(), g.ID)
		if err != nil {
			t.Fatalf("GameRecord(%d): %v", g.ID, err)
		}

		if per := retriesPerPly(rec); len(per) > 0 {
			stuckGames++
			for _, n := range per {
				stuckRuns[n]++
			}
		}

		ss, band, ok := rescore(rec)
		if !ok {
			continue // 평가치가 안 남은 판. 개입률의 분모를 못 세운다
		}
		all = append(all, ss...)
		bands = append(bands, band)
		scored = append(scored, scoredGame{rec: rec, samples: ss})
	}

	if len(all) == 0 {
		t.Skip("평가치가 남은 판이 없다 — 재채점할 것이 없다 (docs/06-status.md §26)")
	}

	// ─── 표본 ────────────────────────────────────────────────────────
	var b strings.Builder
	fmt.Fprintf(&b, "\n표본 — 평가치가 기록된 판 %d개\n", len(scored))
	fmt.Fprintf(&b, "%6s %6s %9s %6s  %s\n", "game", "총수", "채점가능", "개입", "결과")
	accepted, fired := 0, 0
	for _, sg := range scored {
		a, f := count(sg.samples)
		accepted, fired = accepted+a, fired+f
		fmt.Fprintf(&b, "%6d %6d %9d %6d  %s\n",
			sg.rec.ID, sg.rec.MoveCount, a, f, sg.rec.Result)
	}
	fmt.Fprintf(&b, "\n사람의 착수 시도 %d = 통과 %d + 물러짐 %d\n", len(all), accepted, fired)
	t.Log(b.String())

	// ─── 검증 — 재구성이 맞는가 ──────────────────────────────────────
	//
	// **통과한 수는 그때 살아 있던 임계치 아래여야 한다.** 하나라도 위에 있으면 ply를
	// 잘못 이었거나 부호를 뒤집었다는 뜻이라, 아래 표 전부가 못 믿을 값이 된다.
	//
	// 예외가 하나 있다: 詰み을 쥔 채 詰み을 유지한 수는 낙폭과 무관하게 통과한다
	// (Judge 의 종반 분기). 그래서 위반이 나오면 그 수부터 본다.
	worst, over := 0.0, 0
	for _, s := range all {
		if s.fired {
			continue
		}
		worst = math.Max(worst, s.delta)
		if s.delta > Beginner.Threshold() {
			over++
			t.Logf("통과한 수가 임계치 위다: game%d %d수 delta=%.4f", s.game, s.ply, s.delta)
		}
	}
	if over > 0 {
		t.Errorf("통과한 수 %d개가 beginner 임계치(%.2f)를 넘는다 — 詰み 유지 수가 아니라면 "+
			"ply 연결이나 부호가 틀렸다", over, Beginner.Threshold())
	}
	t.Logf("검증 · 통과한 수의 최대 delta = %.4f (임계치 %.2f)", worst, Beginner.Threshold())

	// ─── ① 임계치 ───────────────────────────────────────────────────
	b.Reset()
	b.WriteString("\n① 임계치 — 개입률이 어떻게 움직이나 (K=600 고정)\n")
	b.WriteString("통과한 수의 delta 는 원본 cp 둘로 다시 구했고, 물러진 수의 delta 는\n")
	b.WriteString("기록된 값 그대로다 — 둘 다 K=600 위에서 정확하다.\n\n")
	fmt.Fprintf(&b, "%8s %6s %8s\n", "임계치", "개입", "개입률")
	for _, th := range []float64{0.10, 0.12, 0.15, 0.18, 0.20, 0.22, 0.25, 0.28, 0.30, 0.35, 0.40, 0.50} {
		n := 0
		for _, s := range all {
			if s.delta > th {
				n++
			}
		}
		tag := ""
		switch {
		case math.Abs(th-Beginner.Threshold()) < 1e-9:
			tag = "  ← beginner(지금)"
		case math.Abs(th-Novice.Threshold()) < 1e-9:
			tag = "  ← novice"
		case math.Abs(th-Intermediate.Threshold()) < 1e-9:
			tag = "  ← intermediate"
		}
		fmt.Fprintf(&b, "%8.2f %6d %7.1f%%%s\n", th, n, pct(n, len(all)), tag)
	}
	t.Log(b.String())

	// ─── ② 판마다 얼마나 갈리나 ──────────────────────────────────────
	//
	// **임계치를 흔드는 것보다 이쪽이 크다.** 같은 상수에서 한 판은 0%이고 한 판은
	// 30%대다 — 개입률은 상수가 아니라 그 판이 정한다.
	b.Reset()
	b.WriteString("\n② 판마다 얼마나 갈리나\n")
	fmt.Fprintf(&b, "%6s %8s   %s\n", "game", "사람수", "θ=0.25   θ=0.18   θ=0.12")
	for _, sg := range scored {
		if len(sg.samples) == 0 {
			continue
		}
		fmt.Fprintf(&b, "%6d %8d  ", sg.rec.ID, len(sg.samples))
		for _, th := range []float64{0.25, 0.18, 0.12} {
			n := 0
			for _, s := range sg.samples {
				if s.delta > th {
					n++
				}
			}
			fmt.Fprintf(&b, " %3d회%5.1f%%", n, pct(n, len(sg.samples)))
		}
		b.WriteString("\n")
	}
	t.Log(b.String())

	// ─── ③ K ────────────────────────────────────────────────────────
	//
	// **물러진 수는 여기 못 들어온다.** 기록에 남은 것은 그 수의 delta 뿐이고 원본 cp
	// 둘이 아니라, K를 바꾸면 다시 못 구한다(§39). 그래서 이 표는 **통과한 수만**
	// 세는 하한이다 — K를 바꿔 새로 걸리는 수가 몇인가.
	b.Reset()
	b.WriteString("\n③ K — 통과한 수 중 몇 개가 새로 걸리나 (임계치 0.25 고정)\n")
	b.WriteString("물러진 수는 원본 cp가 안 남아 못 넣는다. 아래는 하한이다.\n\n")
	fmt.Fprintf(&b, "%8s %8s %10s\n", "K", "새로 걸림", "통과 대비")
	pairs := 0
	for _, s := range all {
		if s.hasPair {
			pairs++
		}
	}
	for _, k := range []float64{200, 300, 400, 600, 800, 1200} {
		n := 0
		for _, s := range all {
			if !s.hasPair {
				continue
			}
			if winRateAt(s.bestCp, k)-winRateAt(s.afterCp, k) > Beginner.Threshold() {
				n++
			}
		}
		tag := ""
		if k == K {
			tag = "  ← 지금"
		}
		fmt.Fprintf(&b, "%8.0f %8d %9.1f%%%s\n", k, n, pct(n, pairs), tag)
	}
	t.Log(b.String())

	// ─── ④ 카테고리 ─────────────────────────────────────────────────
	b.Reset()
	b.WriteString("\n④ 카테고리 — 무엇이 실제로 걸렸나\n")
	cats := map[string]int{}
	for _, s := range all {
		if s.fired {
			cats[s.category]++
		}
	}
	for _, kv := range sortedByCount(cats) {
		fmt.Fprintf(&b, "  %-16s %3d %6.1f%%\n", kv.k, kv.v, pct(kv.v, fired))
	}
	fmt.Fprintf(&b, "\n  missed_mate %d건 — 0이면 JudgeMatePlies=%d 는 이 표본이 못 잰다.\n",
		cats[string(CategoryMissedMate)], JudgeMatePlies)
	fmt.Fprintf(&b, "  shallow_trap %d건 — 0이면 ShallowTrapCp=%d 도 마찬가지다.\n",
		cats[string(CategoryShallowTrap)], ShallowTrapCp)
	t.Log(b.String())

	// ─── ⑤ 갇힘 ─────────────────────────────────────────────────────
	//
	// 계단이 열리는 지점(game.HintPieceAfter · HintMoveAfter)의 근거다. **같은 국면에서
	// 연속으로 몇 번 물러졌나**를 세면 그 문이 실제로 몇 번 열렸는지가 그대로 나온다.
	b.Reset()
	b.WriteString("\n⑤ 갇힘 — 같은 국면에서 연속으로 몇 번 물러졌나\n")
	b.WriteString("**평가치가 없는 판까지 전부 센다.** 계단은 cp를 안 쓴다.\n\n")
	stuckTotal, maxRun := 0, 0
	for n, c := range stuckRuns {
		stuckTotal += c
		if n > maxRun {
			maxRun = n
		}
	}
	fmt.Fprintf(&b, "%12s %6s %9s\n", "물러진 횟수", "국면", "그 이상")
	for n := 1; n <= max(maxRun, 5); n++ {
		ge := 0
		for k, c := range stuckRuns {
			if k >= n {
				ge += c
			}
		}
		fmt.Fprintf(&b, "%12d %6d %6d %6.1f%%\n", n, stuckRuns[n], ge, pct(ge, stuckTotal))
	}
	fmt.Fprintf(&b, "\n  %d판에서 물러진 국면 %d개. 계단이 열리려면 그 횟수만큼 같은 국면에서 막혀야 한다.\n",
		stuckGames, stuckTotal)
	t.Log(b.String())

	// ─── ⑥ 밴드 ─────────────────────────────────────────────────────
	//
	// §26이 「밴드가 지켜졌는지 알 수 없다」고 적어둔 자리다. 이제 알 수 있다.
	b.Reset()
	b.WriteString("\n⑥ 밴드 — 상대가 넘겨준 국면이 [+100,+300] 안이었나 (사람 관점 cp)\n")
	fmt.Fprintf(&b, "%6s %5s %8s %10s %10s %10s\n", "game", "n", "중앙값", "밴드아래", "밴드안", "밴드위")
	var pool []int
	for _, bd := range bands {
		if len(bd.cps) == 0 {
			continue
		}
		pool = append(pool, bd.cps...)
		lo, in, hi := split(bd.cps)
		fmt.Fprintf(&b, "%6d %5d %8d %6d%4.0f%% %6d%4.0f%% %6d%4.0f%%\n",
			bd.game, len(bd.cps), median(bd.cps),
			lo, pct(lo, len(bd.cps)), in, pct(in, len(bd.cps)), hi, pct(hi, len(bd.cps)))
	}
	if len(pool) > 0 {
		_, in, _ := split(pool)
		fmt.Fprintf(&b, "\n  전체 %d수 중 밴드 안 %d (%.1f%%), 중앙값 %d\n",
			len(pool), in, pct(in, len(pool)), median(pool))
	}
	t.Log(b.String())
}

// sample 은 사람의 착수 시도 하나다. 물러진 것과 통과한 것을 같은 자리에 담는다 —
// **개입률의 분모가 그 둘의 합**이고, 한쪽만 세면 비율이 아니라 개수가 된다.
type sample struct {
	game     int64
	ply      int
	delta    float64 // K=600 위의 승률 낙폭
	fired    bool
	category string

	// bestCp·afterCp 는 **통과한 수에만** 있다. 물러진 수는 원본 cp가 안 남는다(§39).
	bestCp, afterCp int
	hasPair         bool
}

type scoredGame struct {
	rec     store.GameRecord
	samples []sample
}

// retriesPerPly 는 手数마다 몇 번 물러졌는지다. 그 수가 곧 그 국면에서 갇힘 계수가
// 올라간 높이이고, `game.HintPieceAfter`·`HintMoveAfter` 가 열렸는지를 정한다.
//
// **평가치를 안 본다.** 개입 행만 있으면 되므로 재채점이 안 되는 판에서도 나온다.
func retriesPerPly(rec store.GameRecord) map[int]int {
	if len(rec.Interventions) == 0 {
		return nil
	}
	per := make(map[int]int, len(rec.Interventions))
	for _, iv := range rec.Interventions {
		per[iv.Ply]++
	}
	return per
}

// bandRow 는 한 판에서 **상대가 두고 넘겨준** 국면들의 사람 관점 cp다.
type bandRow struct {
	game int64
	cps  []int
}

// mateCp 는 mate 점수가 cp로 환산되어 들어온 값의 하한이다(usi.MateCp).
//
// 밴드 통계에서 뺀다 — 詰み이 보이는 국면은 밴드가 조절할 수 있는 구간이 아니고,
// 30000 짜리 한 값이 중앙값 빼고 전부를 망가뜨린다. 여기에 usi 를 import 하지 않는
// 것은 이 패키지가 엔진 쪽을 모르게 두기 위해서다.
const mateCp = 30000

// rescore 는 한 판의 기록에서 사람의 착수 시도를 전부 되살린다.
//
// **엔진을 다시 안 돌린다.** `eval_cp` 에 남은 것이 판정이 그때 손에 들고 있던 값
// 그대로이기 때문이고(§26), 그것이 이 재채점이 성립하는 유일한 이유다.
//
//	사람의 N수     BestCp  = eval_cp[N-1]   (착수 전 국면 = 직전 상대 수 뒤)
//	               AfterCp = eval_cp[N]
//
// 둘 다 先手 관점으로 저장되므로 사람이 後手면 부호를 뒤집는다.
//
// ok 가 false면 평가치가 안 남은 판이다.
func rescore(rec store.GameRecord) (samples []sample, band bandRow, ok bool) {
	ev := make(map[int]int, len(rec.Moves))
	for _, m := range rec.Moves {
		if m.EvalCp != nil {
			ev[m.Ply] = *m.EvalCp
		}
	}
	if len(ev) == 0 {
		return nil, bandRow{}, false
	}

	sign, humanOdd := perspective(rec)
	band.game = rec.ID

	for _, m := range rec.Moves {
		isHuman := m.Ply%2 == 1 == humanOdd
		cp, has := ev[m.Ply]
		if !isHuman {
			if has && abs(cp*sign) < mateCp {
				band.cps = append(band.cps, sign*cp)
			}
			continue
		}
		before, hasBefore := ev[m.Ply-1]
		if !has || !hasBefore || m.Ply < 2 {
			continue
		}
		best, after := sign*before, sign*cp
		samples = append(samples, sample{
			game: rec.ID, ply: m.Ply,
			delta:   WinRate(best) - WinRate(after),
			bestCp:  best,
			afterCp: after,
			hasPair: true,
		})
	}

	// 물러진 수. **원본 cp가 없다** — 남은 것은 그때 K=600으로 구한 delta 뿐이다.
	for _, iv := range rec.Interventions {
		samples = append(samples, sample{
			game: rec.ID, ply: iv.Ply,
			delta:    iv.DeltaWin,
			fired:    true,
			category: iv.Category,
		})
	}
	return samples, band, true
}

// perspective 는 저장된 先手 관점 cp를 사람 관점으로 옮기는 부호와, 사람이 홀수 手数를
// 두는가를 준다.
//
// 시작 국면의 수번을 본다 — 평수면 先手지만 `start_sfen` 이 다른 국면일 수 있다.
func perspective(rec store.GameRecord) (sign int, humanOdd bool) {
	startBlack := true
	if f := strings.Fields(rec.StartSFEN); len(f) >= 2 {
		startBlack = f[1] != "w"
	}
	humanBlack := rec.MyColor != "w"
	sign = 1
	if !humanBlack {
		sign = -1
	}
	// 첫 手数를 두는 것은 시작 국면의 수번이다.
	return sign, humanBlack == startBlack
}

func winRateAt(cp int, k float64) float64 {
	return 1 / (1 + math.Exp(-float64(cp)/k))
}

func count(ss []sample) (accepted, fired int) {
	for _, s := range ss {
		if s.fired {
			fired++
		} else {
			accepted++
		}
	}
	return
}

func split(cps []int) (lo, in, hi int) {
	for _, cp := range cps {
		switch {
		case cp < 100:
			lo++
		case cp <= 300:
			in++
		default:
			hi++
		}
	}
	return
}

func median(cps []int) int {
	c := append([]int(nil), cps...)
	sort.Ints(c)
	return c[len(c)/2]
}

func pct(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total) * 100
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

type kv struct {
	k string
	v int
}

func sortedByCount(m map[string]int) []kv {
	out := make([]kv, 0, len(m))
	for k, v := range m {
		out = append(out, kv{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].v != out[j].v {
			return out[i].v > out[j].v
		}
		return out[i].k < out[j].k
	})
	return out
}
