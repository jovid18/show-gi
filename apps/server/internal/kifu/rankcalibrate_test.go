package kifu

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/jovid18/show-gi/apps/server/internal/archive"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/handicap"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 段級 척도의 앵커를 재는 자리. 급수가 밖에서 정해진 기보를 프로덕션과 같은 판정 경로로
// 다시 두고, 급수마다 절대 낙폭이 어디에 떨어지는지를 표로 낸다.
//
// 상수는 이 테스트가 안 고친다. 재는 것과 정하는 것을 갈라 두는 것은 재채점 측정과 같은
// 규약이고(journal §39), 여기서 나온 값으로 경계를 박는 것은 사람의 결정이다(journal §94).
//
//	SHOWGI_MEASURE=1 SHOWGI_RANK_KIFU=~/kifu/manifest.txt \
//	SHOWGI_TEST_DATABASE_URL='postgres://showgi:showgi@localhost:5432/showgi' \
//	SHOWGI_USI_CMD=/opt/yaneuraou/run \
//	go test ./internal/kifu/ -run MeasureRankAnchors -v -timeout 6h
//
// DB가 필요한 이유는 캐시다. 검토 화면(/explore)이 지나간 국면은 positions 에 같은 깊이로
// 남아 있어서, 그 수순을 그대로 재생하면 판당 탐색 200번이 대부분 캐시로 답한다.
func TestMeasureRankAnchors(t *testing.T) {
	manifest := os.Getenv("SHOWGI_RANK_KIFU")
	dbURL := os.Getenv("SHOWGI_TEST_DATABASE_URL")
	// 엔진 경로는 두 이름을 다 받는다. 이 패키지의 임포트 테스트만 SHOWGI_TEST_ENGINE_PATH 를
	// 쓰고 나머지 측정은 SHOWGI_USI_CMD 라(README의 표) 셋째 이름을 만들지 않는다.
	enginePath := os.Getenv("SHOWGI_USI_CMD")
	if enginePath == "" {
		enginePath = os.Getenv("SHOWGI_TEST_ENGINE_PATH")
	}
	if manifest == "" || os.Getenv("SHOWGI_MEASURE") == "" {
		t.Skip("SHOWGI_RANK_KIFU + SHOWGI_MEASURE 가 있어야 돈다 — 판당 몇 분이다")
	}
	if dbURL == "" || enginePath == "" {
		t.Skip("SHOWGI_TEST_DATABASE_URL 과 엔진 경로(SHOWGI_USI_CMD)가 필요하다")
	}

	// 앵커를 몇 手부터 잴지. 초반은 定跡 구간이라 급수 신호가 없다 — 기본값 1은
	// 「전부 잰다」이고, 구간 표를 보고 이 값을 정한 뒤 다시 돌리는 것이 쓰는 법이다.
	// 앵커를 어느 手数 창에서 잴지. 초반은 定跡이라 급수 신호가 없고, 판마다 길이가
	// 달라서 끝까지 세면 자가 실력이 아니라 판 길이의 함수가 된다.
	// 기본값이 런타임과 같아야 한다. 앵커를 다른 창에서 재면 그 값을 skill 에 옮겨 적는
	// 순간 거짓이 된다(skill.AnchorFromPly).
	w := rankWindow{from: skill.AnchorFromPly, to: skill.AnchorToPly}
	if v := os.Getenv("SHOWGI_RANK_FROM_PLY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatalf("SHOWGI_RANK_FROM_PLY=%q 를 못 읽었다", v)
		}
		w.from = n
	}
	if v := os.Getenv("SHOWGI_RANK_TO_PLY"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < w.from {
			t.Fatalf("SHOWGI_RANK_TO_PLY=%q 를 못 읽었다", v)
		}
		w.to = n
	}

	decided := rankDecided
	if v := os.Getenv("SHOWGI_RANK_DECIDED"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0.5 || f > 1 {
			t.Fatalf("SHOWGI_RANK_DECIDED=%q 를 못 읽었다 (0.5~1)", v)
		}
		decided = f
	}

	entries, err := loadRankManifest(manifest)
	if err != nil {
		t.Fatalf("목록을 못 읽었다: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("목록이 비었다")
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

	// 詰み solver 는 없으면 없이 돈다. 그때 종반 판정이 빠지므로 절대 낙폭이 낮게 나오고,
	// 그것은 급수 사이의 차이를 좁히는 방향이다 — 표에 적어 둔다.
	var mate game.MateSearcher
	if cmd := os.Getenv("SHOWGI_MATE_CMD"); cmd != "" {
		matePool, err := usi.NewPool(1, cmd, map[string]string{"USI_Hash": "256", "Threads": "1"})
		if err != nil {
			t.Fatalf("詰み 엔진: %v", err)
		}
		defer matePool.Close()
		mate = matePool
	}

	// 프로덕션과 같은 레벨로 판정한다. 임계치가 갈리면 개입률이 그 값의 함수가 되고
	// (journal §92의 표) 그러면 이 표가 급수가 아니라 레벨을 재게 된다.
	analyst := game.NewEngineAnalyst(searcher, mate, intervene.Beginner)

	byLabel := map[string][]rankSide{}
	var paired []rankPair

	to := "끝"
	if w.to > 0 {
		to = strconv.Itoa(w.to) + "手"
	}
	fmt.Fprintf(os.Stderr, "\n앵커 창: %d手 ~ %s · 이미 갈린 국면 제외 경계 %.2f\n", w.from, to, decided)
	fmt.Fprintf(os.Stderr, "\n%-14s %-14s %5s %8s %8s %6s  %s\n",
		"라벨(先手)", "라벨(後手)", "手数", "先手낙폭", "後手낙폭", "개입", "기보")
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("─", 96))

	measured := 0
	for _, e := range entries {
		sides := measureGame(ctx, analyst, e.game, decided)
		sente, gote := sides[shogi.Black], sides[shogi.White]
		judged := len(sente.moves) + len(gote.moves)
		measured += sente.count(w) + gote.count(w)
		if judged < len(e.game.Moves) {
			// 판정이 빠진 手가 있다. 표본이 조용히 줄어드는 자리라 판마다 남긴다.
			t.Errorf("%s: %d手 중 %d手만 쟀다", e.source, len(e.game.Moves), judged)
		}
		sente.label, gote.label = e.senteLabel, e.goteLabel
		// 手가 0인 쪽은 안 센다. 낙폭 0으로 들어가면 그것이 「매 수 최선」이라 표를
		// 가장 센 쪽으로 끌고 간다.
		if sente.count(w) > 0 {
			byLabel[sente.label] = append(byLabel[sente.label], sente)
		}
		if gote.count(w) > 0 {
			byLabel[gote.label] = append(byLabel[gote.label], gote)
		}
		if sente.label != gote.label && sente.count(w) > 0 && gote.count(w) > 0 {
			paired = append(paired, rankPair{sente: sente, gote: gote})
		}

		fmt.Fprintf(os.Stderr, "%-14s %-14s %5d %8.4f %8.4f %6d  %s\n",
			sente.label, gote.label, judged,
			sente.mean(w), gote.mean(w), sente.blunders(w)+gote.blunders(w), shortSource(e.source))
	}

	// 한 手도 못 쟀으면 표가 머리만 찍히고 초록으로 끝난다. 엔진이 죽었거나 수순이
	// 안 재현된 것이고, 사람이 이 표에서 상수를 옮겨 적는 자리라 초록으로 두면 안 된다.
	if measured == 0 {
		t.Fatal("잰 手가 0이다 — 엔진이나 수순을 확인한다")
	}

	reportRankLabels(byLabel, w)
	reportRankPhases(byLabel)
	reportRankSeparation(byLabel, w)
	reportRankPaired(paired, w)
}

// shortSource 는 표에 실을 기보 이름이다. 수순을 통째로 찍으면 한 줄이 수백 자가 되어
// 표가 안 읽힌다 — 어느 줄인지 알 만큼만 남긴다.
func shortSource(source string) string {
	const keep = 28
	if len(source) <= keep {
		return source
	}
	return source[:keep] + "…"
}

// rankEntry 는 목록의 한 줄이다. 라벨이 파일 안에 없는 이유는 그것이 기보의 사실이
// 아니라 출처의 사실이기 때문이다 — KIF 헤더의 이름 칸은 사이트마다 다르다.
type rankEntry struct {
	senteLabel string
	goteLabel  string
	source     string
	game       ParsedGame
}

// rankSide 는 한 판의 한쪽 몫이다. 두 축을 같이 들고 있는 이유는 段級이 절대 낙폭에서,
// 밴드가 EMA에서 나오기 때문이다(skill.Estimate) — 표에 둘 다 있어야 갈리는 것이 보인다.
//
// 手를 하나하나 들고 있는다. 초반은 定跡을 따라 두는 구간이라 그 사람의 급수가 아니라
// 책의 품질이고, 앵커를 그 위에서 재면 급수 차이가 0으로 희석된다 — 그래서 「몇 手부터」를
// 나중에 정할 수 있어야 한다(rankFromPly).
type rankSide struct {
	label string
	moves []rankMove
	ema   float64
}

// rankMove 는 판정된 수 한 건이다.
type rankMove struct {
	ply     int
	drop    float64
	blunder bool
	// decided 는 그 수를 두기 전에 이미 승패가 갈려 있었나다. 표본에서 뺀다 —
	// 승률이 포화한 구간이라 나쁜 수도 낙폭이 0에 가깝고(01-core.md §2), 반대로
	// 무너지는 몇 수만 크게 잡혀 판 평균을 그 꼬리가 정한다.
	decided bool
}

// rankDecided 는 「이미 갈렸다」의 기본 경계다. 런타임과 같은 값이어야 한다 —
// 앵커가 이 경계 위에서 나왔다(game.DecidedWinRate).
//
// SHOWGI_RANK_DECIDED 로 옮긴다. 1이면 아무것도 안 빼므로, 이 경계가 결론을 만드는지를
// 같은 표본에서 확인할 수 있다 — 실제로 이 값 하나가 「갈린다/아니다」를 뒤집었다.
const rankDecided = game.DecidedWinRate

func decidedAt(cut float64, j game.Judgement) bool {
	if cut >= 1 {
		return false
	}
	w := intervene.WinRate(j.Verdict.BestCp)
	return w >= cut || w <= 1-cut
}

// rankWindow 는 앵커를 재는 手数 창이다. 부르는 쪽이 한 벌만 들고 다니게 묶어 둔다.
type rankWindow struct {
	from int
	to   int // 0이면 끝까지
}

// counts 는 이 수가 앵커에 들어가는가다.
func (m rankMove) counts(w rankWindow) bool {
	if m.decided || m.ply < w.from {
		return false
	}
	return w.to == 0 || m.ply <= w.to
}

// count 는 from手 이후의 판정 수다.
func (s rankSide) count(w rankWindow) int {
	n := 0
	for _, m := range s.moves {
		if m.counts(w) {
			n++
		}
	}
	return n
}

// mean 은 from手 이후의 절대 낙폭 평균이다. 그 구간에 手가 없으면 0이 아니라 NaN이다 —
// 0은 「매 수 최선」이라 표에서 가장 센 쪽으로 읽힌다.
func (s rankSide) mean(w rankWindow) float64 {
	sum, n := 0.0, 0
	for _, m := range s.moves {
		if m.counts(w) {
			sum += m.drop
			n++
		}
	}
	if n == 0 {
		return math.NaN()
	}
	return sum / float64(n)
}

// rankBigDrops 는 「큰 실수」의 절대 경계들이다. 개입률과 재는 것이 같은데 레벨을 안 본다 —
// 개입은 임계치가 분모라(intervene.Level.Threshold) 그 값이 갈리면 빈도가 따라 갈린다.
var rankBigDrops = [...]float64{0.10, 0.20}

// bigRate 는 from手 이후에 낙폭이 cut 이상이던 수의 비율이다.
func (s rankSide) bigRate(w rankWindow, cut float64) float64 {
	big, n := 0, 0
	for _, m := range s.moves {
		if !m.counts(w) {
			continue
		}
		n++
		if m.drop >= cut {
			big++
		}
	}
	if n == 0 {
		return math.NaN()
	}
	return float64(big) / float64(n)
}

// blunders 는 from手 이후의 개입 건수다.
func (s rankSide) blunders(w rankWindow) int {
	n := 0
	for _, m := range s.moves {
		if m.counts(w) && m.blunder {
			n++
		}
	}
	return n
}

type rankPair struct{ sente, gote rankSide }

// measureGame 은 한 판을 프로덕션 판정으로 다시 두고 양쪽 몫을 돌려준다.
//
// 개입을 안 건다. 물러진 수가 판에서 빠지면 그 수의 낙폭이 표본에서 사라지고, 그건
// 정확히 재려던 것이다.
func measureGame(ctx context.Context, analyst game.Analyst, g ParsedGame, decided float64) map[shogi.Color]rankSide {
	startSFEN := g.StartSFEN
	if startSFEN == "" {
		startSFEN = shogi.StartSFEN
	}
	tracks := map[shogi.Color]*skill.Track{shogi.Black: skill.NewTrack(), shogi.White: skill.NewTrack()}
	out := map[shogi.Color]rankSide{}

	for i := range g.Moves {
		ply := i + 1
		j, err := analyst.Judge(ctx, startSFEN, g.Moves[:ply], ply)
		if err != nil {
			// 한 수를 못 재면 그 수만 빠진다. 판을 버리지 않는 것은 手数가 표에 있어서
			// 표본이 조용히 줄어드는 자리가 없기 때문이다.
			continue
		}
		c := moverAt(startSFEN, ply)
		side := out[c]
		side.moves = append(side.moves, rankMove{
			ply:     ply,
			drop:    absDrop(j),
			blunder: j.Verdict.Kind == intervene.KindBlunder,
			decided: decidedAt(decided, j),
		})
		e := tracks[c].Observe(skill.Move{
			Blunder:   j.Verdict.Kind == intervene.KindBlunder,
			DeltaWin:  j.Verdict.DeltaWin,
			Threshold: j.Threshold,
		})
		side.ema = e.Loss
		out[c] = side
	}
	return out
}

// absDrop 은 한 수의 절대 낙폭이다. skill 이 안에서 하는 것과 같은 계산이고, 여기서
// 다시 쓰는 것은 그쪽이 판마다의 평균을 안 내놓기 때문이다(Estimate.AbsLoss 는 누계다).
func absDrop(j game.Judgement) float64 {
	d := j.Verdict.DeltaWin
	if d < 0 {
		d = 0 // 두 탐색의 뿌리가 한 수 달라 음수가 나오는 국면이 있다(journal §41)
	}
	if j.Verdict.Kind == intervene.KindBlunder && d < skill.MateAbsLoss {
		d = skill.MateAbsLoss // 詰み을 놓친 수는 승률이 거의 안 움직인다(skill.absMoveLoss)
	}
	return d
}

// moverAt 은 그 手数를 둔 쪽이다. Judgement 이 手番을 안 내놓아서 여기서 센다 —
// 駒落ち는 上手가 먼저 두므로 手数의 홀짝만으로는 갈린다(journal §88).
func moverAt(startSFEN string, ply int) shogi.Color {
	first := shogi.Black
	if f := strings.Fields(startSFEN); len(f) > 1 && f[1] == "w" {
		first = shogi.White
	}
	if ply%2 == 1 {
		return first
	}
	if first == shogi.Black {
		return shogi.White
	}
	return shogi.Black
}

// reportRankLabels 는 급수마다의 앵커 후보를 낸다. SD가 급수 사이의 차이보다 크면
// 그것이 이 측정의 답이고, 그때 눈금을 더 촘촘히 하는 것이 아니라 굵게 해야 한다
// (journal §39의 판간 분산).
func reportRankLabels(byLabel map[string][]rankSide, w rankWindow) {
	fmt.Fprintf(os.Stderr, "\n%-10s %5s %6s %9s %9s %9s %9s %8s %8s %8s %8s %6s\n",
		"라벨", "표본", "手数", "평균낙폭", "중앙값", "SD", "표준오차", "개입률", "≥0.10", "≥0.20", "EMA", "이름")
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("─", 96))

	for _, label := range sortedRankLabels(byLabel, w) {
		sides := byLabel[label]
		mean, sd := meanSD(sides, w)
		moves, blunders := 0, 0
		for _, s := range sides {
			moves += s.count(w)
			blunders += s.blunders(w)
		}
		se := 0.0
		if len(sides) > 1 {
			se = sd / math.Sqrt(float64(len(sides)))
		}
		// EMA도 같이 낸다. 밴드가 그 값을 보므로(skill.Estimate.Loss) 급수마다 상대가
		// 얼마나 세게 붙는지를 이 칸으로 읽는다 — 段級은 그 옆의 이름이 말한다.
		ema := 0.0
		for _, s := range sides {
			ema += s.ema
		}
		ema /= float64(len(sides))
		fmt.Fprintf(os.Stderr, "%-10s %5d %6d %9.4f %9.4f %9.4f %9.4f %7.1f%% %7.1f%% %7.1f%% %8.4f %6s\n",
			label, len(sides), moves, mean, median(sides, w), sd, se,
			100*float64(blunders)/math.Max(1, float64(moves)),
			100*labelBigRate(sides, w, rankBigDrops[0]), 100*labelBigRate(sides, w, rankBigDrops[1]),
			ema, rankNameOf(mean))
	}
}

// rankNameOf 는 그 절대 낙폭에 지금 척도가 붙이는 이름이다. 앵커가 이 표에서 나왔으므로
// (skill.rankAnchors) 라벨과 이름이 어긋나면 상수가 낡은 것이다.
func rankNameOf(absLoss float64) string {
	r, ok := skill.RankOf(skill.Estimate{AbsLoss: absLoss, AbsSamples: skill.MinSamples})
	if !ok {
		return "—"
	}
	return r.NameJa
}

// reportRankSeparation 은 라벨 둘이 실제로 갈리는지를 낸다. 앵커를 박기 전에 답해야
// 하는 질문이 이것뿐이다 — 안 갈리면 경계를 어디에 놓아도 그 경계가 표본의 사실이 아니다.
func reportRankSeparation(byLabel map[string][]rankSide, w rankWindow) {
	labels := sortedRankLabels(byLabel, w)
	if len(labels) < 2 {
		return
	}
	fmt.Fprintf(os.Stderr, "\n%-24s %-10s %9s %9s %7s  %s\n", "라벨 쌍", "무엇으로", "차이", "합성SE", "차/SE", "갈리나")
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("─", 84))

	// 두 축으로 잰다. 평균은 몇 수의 큰 낙폭이 정하고, 빈도는 그 큰 수가 얼마나 자주
	// 오는가다 — 개입률과 같은 것을 보면서 레벨을 안 본다(rankBigDrops).
	axes := []struct {
		name string
		of   func(rankSide) float64
	}{
		{"평균낙폭", func(s rankSide) float64 { return s.mean(w) }},
		{"≥0.10 빈도", func(s rankSide) float64 { return s.bigRate(w, rankBigDrops[0]) }},
		{"≥0.20 빈도", func(s rankSide) float64 { return s.bigRate(w, rankBigDrops[1]) }},
	}

	for i := range labels {
		for k := i + 1; k < len(labels); k++ {
			for _, axis := range axes {
				ma, sa, na := statsOf(byLabel[labels[i]], axis.of)
				mb, sb, nb := statsOf(byLabel[labels[k]], axis.of)
				if na == 0 || nb == 0 {
					continue
				}
				se := math.Sqrt(sa*sa/float64(na) + sb*sb/float64(nb))
				gap := math.Abs(ma - mb)
				ratio := 0.0
				if se > 0 {
					ratio = gap / se
				}
				verdict := "아니다"
				if ratio >= 2 {
					verdict = "갈린다"
				}
				fmt.Fprintf(os.Stderr, "%-24s %-10s %9.4f %9.4f %7.2f  %s\n",
					labels[i]+" ↔ "+labels[k], axis.name, gap, se, ratio, verdict)
			}
		}
	}
}

// statsOf 는 측면마다 뽑은 값의 평균·표준편차·개수다. 잰 手가 없는 측면은 빠진다.
func statsOf(sides []rankSide, of func(rankSide) float64) (mean, sd float64, n int) {
	var vals []float64
	for _, s := range sides {
		if v := of(s); !math.IsNaN(v) {
			vals = append(vals, v)
		}
	}
	if len(vals) == 0 {
		return math.NaN(), 0, 0
	}
	for _, v := range vals {
		mean += v
	}
	mean /= float64(len(vals))
	if len(vals) < 2 {
		return mean, 0, len(vals)
	}
	for _, v := range vals {
		sd += (v - mean) * (v - mean)
	}
	return mean, math.Sqrt(sd / float64(len(vals)-1)), len(vals)
}

// reportRankPaired 는 교차 대국의 대응 비교다. 판마다의 차이가 짝 안에서 상쇄되므로
// (§39의 분산이 판의 성질이라면) 같은 판 수로 더 예민하다.
//
// 대신 교란이 하나 남는다. 약한 쪽이 실수하면 상대의 국면이 쉬워져서, 차이가 급수 때문인지
// 국면 때문인지를 이 표로는 못 가른다 — 「갈리지 않는다」를 값싸게 확인하는 데 쓴다.
func reportRankPaired(paired []rankPair, w rankWindow) {
	if len(paired) == 0 {
		return
	}
	agree, n := 0, 0
	fmt.Fprintf(os.Stderr, "\n교차 대국 — 같은 판 안에서 약한 쪽이 더 크게 잃었나\n")
	for _, p := range paired {
		weak, strong, ok := weakerFirst(p)
		if !ok {
			continue // 라벨을 척도 위에 못 놓았다(rankOrdinal)
		}
		n++
		if weak.mean(w) > strong.mean(w) {
			agree++
		}
		fmt.Fprintf(os.Stderr, "  %-10s %.4f  vs  %-10s %.4f\n",
			weak.label, weak.mean(w), strong.label, strong.mean(w))
	}
	if n > 0 {
		fmt.Fprintf(os.Stderr, "  방향이 맞은 판: %d / %d\n", agree, n)
	}
}

// weakerFirst 는 짝의 약한 쪽을 앞으로 놓는다. 라벨이 급수 표기가 아니면 못 놓는다.
func weakerFirst(p rankPair) (weak, strong rankSide, ok bool) {
	a, aok := rankOrdinal(p.sente.label)
	b, bok := rankOrdinal(p.gote.label)
	if !aok || !bok || a == b {
		return rankSide{}, rankSide{}, false
	}
	if a < b {
		return p.sente, p.gote, true
	}
	return p.gote, p.sente, true
}

var rankLabelRe = regexp.MustCompile(`^(\d+)(kyu|dan)$`)

// rankOrdinal 은 라벨을 척도 위의 자리로 옮긴다. 級은 숫자가 클수록 약해서 부호가 뒤집힌다.
func rankOrdinal(label string) (int, bool) {
	m := rankLabelRe.FindStringSubmatch(strings.ToLower(label))
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	if m[2] == "kyu" {
		return -n, true
	}
	return n, true
}

// sortedRankLabels 는 표의 줄 순서다. 급수 표기면 척도 순, 아니면 실측 낙폭 순이다 —
// 이름을 못 읽었다고 표가 뒤섞이면 갈리는지를 눈으로 못 본다.
func sortedRankLabels(byLabel map[string][]rankSide, w rankWindow) []string {
	labels := make([]string, 0, len(byLabel))
	for l := range byLabel {
		labels = append(labels, l)
	}
	sort.Slice(labels, func(i, k int) bool {
		a, aok := rankOrdinal(labels[i])
		b, bok := rankOrdinal(labels[k])
		if aok && bok {
			return a < b
		}
		mi, _ := meanSD(byLabel[labels[i]], w)
		mk, _ := meanSD(byLabel[labels[k]], w)
		return mi > mk
	})
	return labels
}

// meanSD 는 한 라벨의 평균과 표준편차다. 판마다의 평균을 표본 하나로 세는 것이 §39가
// 잰 분산과 같은 단위다 — 手를 통째로 모아 세면 긴 판이 표를 끌고 간다.
func meanSD(sides []rankSide, w rankWindow) (mean, sd float64) {
	var vals []float64
	for _, s := range sides {
		if v := s.mean(w); !math.IsNaN(v) {
			vals = append(vals, v)
		}
	}
	if len(vals) == 0 {
		return math.NaN(), 0
	}
	for _, v := range vals {
		mean += v
	}
	mean /= float64(len(vals))
	if len(vals) < 2 {
		return mean, 0
	}
	for _, v := range vals {
		d := v - mean
		sd += d * d
	}
	return mean, math.Sqrt(sd / float64(len(vals)-1))
}

// labelBigRate 는 라벨 전체에서 큰 실수의 비율이다. 측면마다 재서 평균한다 — 手를 통째로
// 모으면 긴 판이 표를 끌고 간다(meanSD 와 같은 규약).
func labelBigRate(sides []rankSide, w rankWindow, cut float64) float64 {
	sum, n := 0.0, 0
	for _, s := range sides {
		if v := s.bigRate(w, cut); !math.IsNaN(v) {
			sum += v
			n++
		}
	}
	if n == 0 {
		return math.NaN()
	}
	return sum / float64(n)
}

// sideMeans 는 라벨의 판별 평균들이다. 잰 手가 없는 쪽은 빠진다.
func sideMeans(sides []rankSide, w rankWindow) []float64 {
	var out []float64
	for _, s := range sides {
		if v := s.mean(w); !math.IsNaN(v) {
			out = append(out, v)
		}
	}
	sort.Float64s(out)
	return out
}

// median 은 판별 평균의 중앙값이다. 한 판이 종반에 무너져 평균을 끌고 가는 것을 이 값이 말한다.
func median(sides []rankSide, w rankWindow) float64 {
	v := sideMeans(sides, w)
	if len(v) == 0 {
		return math.NaN()
	}
	if len(v)%2 == 1 {
		return v[len(v)/2]
	}
	return (v[len(v)/2-1] + v[len(v)/2]) / 2
}

// trimmedMean 은 양 끝 하나씩을 버린 평균이다. 표본이 넷보다 적으면 버릴 것이 없어 평균과 같다.
func trimmedMean(sides []rankSide, w rankWindow) float64 {
	v := sideMeans(sides, w)
	if len(v) == 0 {
		return math.NaN()
	}
	if len(v) >= 4 {
		v = v[1 : len(v)-1]
	}
	sum := 0.0
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v))
}

// rankSegments 는 구간 표의 경계다. 定跡이 어디까지인지를 이 표로 눈으로 본다 —
// 초반의 낙폭이 뒤 구간보다 뚜렷하게 낮으면 그 구간은 사람이 아니라 책이 둔 것이다.
var rankSegments = [...]int{1, 21, 41, 61}

// reportRankPhases 는 급수마다 手数 구간별 평균을 낸다. 앵커를 몇 手부터 잴지가 이 표로
// 정해진다(SHOWGI_RANK_FROM_PLY).
func reportRankPhases(byLabel map[string][]rankSide) {
	fmt.Fprintf(os.Stderr, "\n구간별 평균 낙폭 — 초반이 낮으면 定跡 구간이다\n")
	fmt.Fprintf(os.Stderr, "%-10s %10s %10s %10s %10s\n", "라벨", "1-20手", "21-40手", "41-60手", "61手~")
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("─", 56))

	for _, label := range sortedRankLabels(byLabel, rankWindow{from: 1}) {
		fmt.Fprintf(os.Stderr, "%-10s", label)
		for i, lo := range rankSegments {
			hi := math.MaxInt32
			if i+1 < len(rankSegments) {
				hi = rankSegments[i+1] - 1
			}
			sum, n := 0.0, 0
			for _, s := range byLabel[label] {
				for _, m := range s.moves {
					if m.ply >= lo && m.ply <= hi && !m.decided {
						sum += m.drop
						n++
					}
				}
			}
			if n == 0 {
				fmt.Fprintf(os.Stderr, " %10s", "—")
				continue
			}
			fmt.Fprintf(os.Stderr, " %10.4f", sum/float64(n))
		}
		fmt.Fprintln(os.Stderr)
	}
}

// loadRankManifest 는 라벨이 붙은 기보 목록을 읽는다.
//
// 목록은 레포 밖에 둔다. 제3자 기보를 testdata 에 커밋하지 않고 파생 상수만 넣는 것이
// 여기의 규약이다 — 레포가 퍼블릭이다(CLAUDE.md).
//
// 한 줄이 「라벨 기보」다. 라벨이 sente_vs_gote 꼴이면 양쪽이 갈리고, 기보는 셋 중 하나다:
// 검토 화면의 주소(/explore?m=…) · 쉼표로 이은 USI · .kif/.csa 경로.
func loadRankManifest(path string) ([]rankEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []rankEntry
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		at := strings.IndexFunc(line, unicode.IsSpace)
		if at < 0 {
			return nil, fmt.Errorf("%d번째 줄에 기보가 없다: %q", i+1, line)
		}
		label := line[:at]
		source := strings.TrimSpace(line[at:])
		if source == "" {
			return nil, fmt.Errorf("%d번째 줄에 기보가 없다: %q", i+1, line)
		}
		g, err := parseRankSource(source)
		if err != nil {
			return nil, fmt.Errorf("%d번째 줄: %w", i+1, err)
		}
		e := rankEntry{source: source, game: g}
		if s, gt, ok := strings.Cut(label, "_vs_"); ok {
			e.senteLabel, e.goteLabel = s, gt
		} else {
			e.senteLabel, e.goteLabel = label, label
		}
		out = append(out, e)
	}
	return out, nil
}

// parseRankSource 는 목록의 기보 한 칸을 판으로 바꾼다.
func parseRankSource(source string) (ParsedGame, error) {
	switch {
	case strings.Contains(source, "/explore"):
		return exploreGame(source)
	case strings.HasSuffix(strings.ToLower(source), ".csa"):
		raw, err := os.ReadFile(source)
		if err != nil {
			return ParsedGame{}, err
		}
		return ParseCSA(string(raw))
	case strings.HasSuffix(strings.ToLower(source), ".kif") || strings.HasSuffix(strings.ToLower(source), ".kifu"):
		raw, err := os.ReadFile(source)
		if err != nil {
			return ParsedGame{}, err
		}
		return ParseKIF(string(raw))
	case filepath.IsAbs(source) || strings.HasPrefix(source, "~"):
		return ParsedGame{}, fmt.Errorf("확장자로 형식을 못 정한다: %s", source)
	default:
		return csvGame(source)
	}
}

// exploreGame 은 검토 화면의 주소에서 판을 만든다. 그 화면은 양쪽 수를 다 받고 서버가
// 한 수도 대신 두지 않으므로(server/explore.go) 주소 하나가 기보 하나다.
func exploreGame(rawURL string) (ParsedGame, error) {
	// 브라우저 주소창에서 복사하면 쉼표가 %2C 로 올 수 있다. 손으로 자르면 그 줄이
	// 수 하나로 뭉치고, 그 판은 「재현이 안 된 판」이 아니라 「이상한 판」이 된다.
	u, err := url.Parse(rawURL)
	if err != nil {
		return ParsedGame{}, fmt.Errorf("주소를 못 읽었다: %w", err)
	}
	q := u.Query()
	g := ParsedGame{Source: "explore", Moves: splitMoves(q.Get("m"))}
	if id := q.Get("h"); id != "" {
		h, found := handicap.Find(id)
		if !found {
			return ParsedGame{}, fmt.Errorf("모르는 手合割: %s", id)
		}
		g.StartSFEN = h.SFEN
	}
	if len(g.Moves) == 0 {
		return ParsedGame{}, fmt.Errorf("주소에 수순이 없다: %s", rawURL)
	}
	return g, nil
}

func csvGame(csv string) (ParsedGame, error) {
	moves := splitMoves(csv)
	if len(moves) == 0 {
		return ParsedGame{}, fmt.Errorf("수순을 못 읽었다: %q", csv)
	}
	return ParsedGame{Moves: moves, Source: "csv"}, nil
}

func splitMoves(s string) []string {
	var out []string
	for _, m := range strings.Split(s, ",") {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}
