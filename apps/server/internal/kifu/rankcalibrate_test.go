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

	fmt.Fprintf(os.Stderr, "\n%-14s %-14s %5s %8s %8s %6s  %s\n",
		"라벨(先手)", "라벨(後手)", "手数", "先手낙폭", "後手낙폭", "개입", "기보")
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("─", 96))

	measured := 0
	for _, e := range entries {
		sides := measureGame(ctx, analyst, e.game)
		sente, gote := sides[shogi.Black], sides[shogi.White]
		measured += sente.moves + gote.moves
		if sente.moves+gote.moves < len(e.game.Moves) {
			// 판정이 빠진 手가 있다. 표본이 조용히 줄어드는 자리라 판마다 남긴다.
			t.Errorf("%s: %d手 중 %d手만 쟀다", e.source, len(e.game.Moves), sente.moves+gote.moves)
		}
		sente.label, gote.label = e.senteLabel, e.goteLabel
		// 手가 0인 쪽은 안 센다. 낙폭 0으로 들어가면 그것이 「매 수 최선」이라 표를
		// 가장 센 쪽으로 끌고 간다.
		if sente.moves > 0 {
			byLabel[sente.label] = append(byLabel[sente.label], sente)
		}
		if gote.moves > 0 {
			byLabel[gote.label] = append(byLabel[gote.label], gote)
		}
		if sente.label != gote.label && sente.moves > 0 && gote.moves > 0 {
			paired = append(paired, rankPair{sente: sente, gote: gote})
		}

		fmt.Fprintf(os.Stderr, "%-14s %-14s %5d %8.4f %8.4f %6d  %s\n",
			sente.label, gote.label, sente.moves+gote.moves,
			sente.mean(), gote.mean(), sente.blunders+gote.blunders, e.source)
	}

	// 한 手도 못 쟀으면 표가 머리만 찍히고 초록으로 끝난다. 엔진이 죽었거나 수순이
	// 안 재현된 것이고, 사람이 이 표에서 상수를 옮겨 적는 자리라 초록으로 두면 안 된다.
	if measured == 0 {
		t.Fatal("잰 手가 0이다 — 엔진이나 수순을 확인한다")
	}

	reportRankLabels(byLabel)
	reportRankSeparation(byLabel)
	reportRankPaired(paired)
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
type rankSide struct {
	label    string
	moves    int
	absSum   float64
	blunders int
	ema      float64
}

func (s rankSide) mean() float64 {
	if s.moves == 0 {
		return 0
	}
	return s.absSum / float64(s.moves)
}

type rankPair struct{ sente, gote rankSide }

// measureGame 은 한 판을 프로덕션 판정으로 다시 두고 양쪽 몫을 돌려준다.
//
// 개입을 안 건다. 물러진 수가 판에서 빠지면 그 수의 낙폭이 표본에서 사라지고, 그건
// 정확히 재려던 것이다.
func measureGame(ctx context.Context, analyst game.Analyst, g ParsedGame) map[shogi.Color]rankSide {
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
		side.moves++
		side.absSum += absDrop(j)
		if j.Verdict.Kind == intervene.KindBlunder {
			side.blunders++
		}
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
func reportRankLabels(byLabel map[string][]rankSide) {
	fmt.Fprintf(os.Stderr, "\n%-10s %5s %6s %9s %9s %9s %8s %8s %6s %6s\n",
		"라벨", "표본", "手数", "평균낙폭", "SD", "표준오차", "개입률", "EMA", "이름", "옛이름")
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("─", 96))

	for _, label := range sortedRankLabels(byLabel) {
		sides := byLabel[label]
		mean, sd := meanSD(sides)
		moves, blunders := 0, 0
		for _, s := range sides {
			moves += s.moves
			blunders += s.blunders
		}
		se := 0.0
		if len(sides) > 1 {
			se = sd / math.Sqrt(float64(len(sides)))
		}
		// 두 이름을 나란히 낸다. 표시가 EMA에서 평균으로 옮겨 갔으므로(journal §94) 그
		// 이동이 몇 계급인지는 실측으로만 알 수 있고, 그 답이 이 두 칸이다.
		ema := 0.0
		for _, s := range sides {
			ema += s.ema
		}
		ema /= float64(len(sides))
		fmt.Fprintf(os.Stderr, "%-10s %5d %6d %9.4f %9.4f %9.4f %7.1f%% %8.4f %6s %6s\n",
			label, len(sides), moves, mean, sd, se,
			100*float64(blunders)/math.Max(1, float64(moves)), ema,
			rankNameOf(mean), rankNameOf(ema*skill.RankLossScale))
	}
}

// rankNameOf 는 그 절대 낙폭에 지금 척도가 붙이는 이름이다. EMA를 넣으면 옛 척도의
// 이름이 나온다 — 두 축이 같은 자를 쓰고 분모만 달랐다(skill.RankLossScale).
func rankNameOf(absLoss float64) string {
	r, ok := skill.RankOf(skill.Estimate{AbsLoss: absLoss, AbsSamples: skill.MinSamples})
	if !ok {
		return "—"
	}
	return r.NameJa
}

// reportRankSeparation 은 라벨 둘이 실제로 갈리는지를 낸다. 앵커를 박기 전에 답해야
// 하는 질문이 이것뿐이다 — 안 갈리면 경계를 어디에 놓아도 그 경계가 표본의 사실이 아니다.
func reportRankSeparation(byLabel map[string][]rankSide) {
	labels := sortedRankLabels(byLabel)
	if len(labels) < 2 {
		return
	}
	fmt.Fprintf(os.Stderr, "\n%-24s %9s %9s %7s  %s\n", "라벨 쌍", "차이", "합성SE", "차/SE", "갈리나")
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("─", 72))

	for i := range labels {
		for k := i + 1; k < len(labels); k++ {
			a, b := byLabel[labels[i]], byLabel[labels[k]]
			ma, sa := meanSD(a)
			mb, sb := meanSD(b)
			se := math.Sqrt(sa*sa/math.Max(1, float64(len(a))) + sb*sb/math.Max(1, float64(len(b))))
			gap := math.Abs(ma - mb)
			ratio := 0.0
			if se > 0 {
				ratio = gap / se
			}
			verdict := "아니다"
			if ratio >= 2 {
				verdict = "갈린다"
			}
			fmt.Fprintf(os.Stderr, "%-24s %9.4f %9.4f %7.2f  %s\n",
				labels[i]+" ↔ "+labels[k], gap, se, ratio, verdict)
		}
	}
}

// reportRankPaired 는 교차 대국의 대응 비교다. 판마다의 차이가 짝 안에서 상쇄되므로
// (§39의 분산이 판의 성질이라면) 같은 판 수로 더 예민하다.
//
// 대신 교란이 하나 남는다. 약한 쪽이 실수하면 상대의 국면이 쉬워져서, 차이가 급수 때문인지
// 국면 때문인지를 이 표로는 못 가른다 — 「갈리지 않는다」를 값싸게 확인하는 데 쓴다.
func reportRankPaired(paired []rankPair) {
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
		if weak.mean() > strong.mean() {
			agree++
		}
		fmt.Fprintf(os.Stderr, "  %-10s %.4f  vs  %-10s %.4f\n",
			weak.label, weak.mean(), strong.label, strong.mean())
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
func sortedRankLabels(byLabel map[string][]rankSide) []string {
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
		mi, _ := meanSD(byLabel[labels[i]])
		mk, _ := meanSD(byLabel[labels[k]])
		return mi > mk
	})
	return labels
}

// meanSD 는 한 라벨의 평균과 표준편차다. 판마다의 평균을 표본 하나로 세는 것이 §39가
// 잰 분산과 같은 단위다 — 手를 통째로 모아 세면 긴 판이 표를 끌고 간다.
func meanSD(sides []rankSide) (mean, sd float64) {
	if len(sides) == 0 {
		return 0, 0
	}
	for _, s := range sides {
		mean += s.mean()
	}
	mean /= float64(len(sides))
	if len(sides) < 2 {
		return mean, 0
	}
	for _, s := range sides {
		d := s.mean() - mean
		sd += d * d
	}
	return mean, math.Sqrt(sd / float64(len(sides)-1))
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
