package kifu

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
)

// 실 기보로 **태그가 맞게 붙는지**를 넓게 본다. 지금까지 囲い·전법 태그는 손으로 만든
// 국면에서만 재봤고(09-tags.md §4), 「실제 대국에서 얼마나·어디서 뜨는가」는 안 쟀다.
//
// **엔진을 안 쓴다.** 囲い·전법·戦型은 판과 수순만으로 정해지므로 이 측정이 초 단위로
// 끝나고, 그래서 고치고 다시 돌리는 것을 반복할 수 있다.
//
//	SHOWGI_KIFU_SCAN=1 go test ./internal/kifu/ -run ScanTags -v
//	SHOWGI_KIFU_SEED=2 SHOWGI_KIFU_SCAN=1 go test ./internal/kifu/ -run ScanTags -v
//
// **seed를 고정하고 찍는다.** 매번 다른 10판을 뽑으면 「고쳐서 나아진 것」과 「표본이
// 쉬워진 것」을 못 가른다 — 고치고 다시 도는 루프가 그 자리에서 성립하지 않는다.

const scanGames = 10

// scanCount 는 몇 판을 볼지다. 기본은 10판이고, 분포를 볼 때만 전부로 넓힌다 —
// 고치고 다시 도는 루프는 10판이라야 눈으로 훑을 수 있다.
func scanCount(t *testing.T) int {
	t.Helper()
	v := os.Getenv("SHOWGI_KIFU_GAMES")
	if v == "" {
		return scanGames
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		t.Fatalf("SHOWGI_KIFU_GAMES=%q 는 1 이상의 수가 아니다", v)
	}
	return n
}

// floodgateFiles 는 seed 로 정해진 표본이다. 파일 이름을 먼저 정렬하는 것이 요점이다 —
// 디렉터리 순서는 파일시스템이 정하므로 그것 위에서 섞으면 seed 가 있어도 재현되지 않는다.
func floodgateFiles(t *testing.T, seed uint64, n int) []string {
	t.Helper()

	dir := filepath.Join("testdata", "floodgate")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("floodgate 기보가 없다 (%s): %v", dir, err)
	}

	var all []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".csa" {
			all = append(all, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(all)
	if len(all) == 0 {
		t.Skipf("floodgate 디렉터리에 .csa 가 없다: %s", dir)
	}

	r := rand.New(rand.NewPCG(seed, 0x5eed))
	r.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })

	if n > len(all) {
		n = len(all)
	}
	t.Logf("seed=%d · 기보 %d개 중 %d개를 뽑았다", seed, len(all), n)
	return all[:n]
}

func scanSeed(t *testing.T) uint64 {
	t.Helper()
	v := os.Getenv("SHOWGI_KIFU_SEED")
	if v == "" {
		return 1
	}
	n, err := strconv.ParseUint(v, 10, 64)
	if err != nil {
		t.Fatalf("SHOWGI_KIFU_SEED=%q 는 수가 아니다", v)
	}
	return n
}

// firstAppearance 는 태그 코드마다 **처음 붙은 手数**다.
//
// 전이만 보는 것이 요점이다. 10판 × 120수 × 두 색을 전부 눈으로 볼 수 없고, 태그는 한 번
// 붙으면 대개 끝까지 남아서 매 수 찍으면 같은 줄이 수십 번 나온다.
type firstAppearance struct {
	code string
	ply  int
}

type gameScan struct {
	name    string
	plies   int
	byColor map[shogi.Color][]firstAppearance
	err     error
}

// scanOne 은 기보 하나를 수마다 재생하며 **프로덕션과 같은 조건**으로 태그를 뽑는다.
//
// `session.go` 의 `styleTags()` 가 부르는 그 형태 그대로여야 한다 — 여기서 입력을 다르게
// 만들면 측정이 제품과 다른 것을 재게 되고, 그 어긋남은 아무 데서도 안 터진다.
func scanOne(path string) gameScan {
	out := gameScan{name: filepath.Base(path), byColor: map[shogi.Color][]firstAppearance{}}

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

	// 手番이 번갈아 간다는 것에 기대지 않고 **적용한 색으로** 가른다. 中断된 기보나
	// 평수가 아닌 시작 국면에서 짝수/홀수 가정이 조용히 뒤집힌다.
	moves := map[shogi.Color][]string{}
	seen := map[shogi.Color]map[string]bool{shogi.Black: {}, shogi.White: {}}

	for i, u := range g.Moves {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			out.err = fmt.Errorf("%d수 %q: %w", i+1, u, err)
			return out
		}
		mover := pos.Turn
		pos = pos.Apply(m)
		moves[mover] = append(moves[mover], u)
		out.plies = i + 1

		for _, c := range []shogi.Color{shogi.Black, shogi.White} {
			for _, tg := range tag.Detect(tag.Input{
				Pos: pos, Color: c,
				PlayerMoves: moves[c], OpponentMoves: moves[c.Other()],
			}) {
				if !seen[c][tg.Code] {
					seen[c][tg.Code] = true
					out.byColor[c] = append(out.byColor[c], firstAppearance{tg.Code, out.plies})
				}
			}
		}
	}
	return out
}

func TestScanTagsOverFloodgateGames(t *testing.T) {
	if os.Getenv("SHOWGI_KIFU_SCAN") == "" {
		t.Skip("SHOWGI_KIFU_SCAN 미설정")
	}

	files := floodgateFiles(t, scanSeed(t), scanCount(t))

	// 엔진을 안 쓰므로 그냥 전부 동시에 돌린다.
	results := make([]gameScan, len(files))
	var wg sync.WaitGroup
	for i, f := range files {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = scanOne(f)
		}()
	}
	wg.Wait()

	var (
		parsed, failed int
		noCastle       int
		totalTags      int
		perCode        = map[string]int{}
	)

	for _, r := range results {
		if r.err != nil {
			// **버린 판을 센다.** 조용히 줄어들면 표본 수가 거짓이 된다.
			failed++
			t.Logf("✗ %s — %v", r.name, r.err)
			continue
		}
		parsed++

		t.Logf("── %s (%d수)", r.name, r.plies)
		gotCastle := false
		for _, c := range []shogi.Color{shogi.Black, shogi.White} {
			side := "先手"
			if c == shogi.White {
				side = "後手"
			}
			if len(r.byColor[c]) == 0 {
				t.Logf("   %s  (이름 없음)", side)
				continue
			}
			for _, fa := range r.byColor[c] {
				t.Logf("   %s %3d수  %s", side, fa.ply, fa.code)
				perCode[fa.code]++
				totalTags++
				if tag.SourceOf(fa.code) != "" || isCastleCode(fa.code) {
					gotCastle = gotCastle || isCastleCode(fa.code)
				}
			}
		}
		if !gotCastle {
			noCastle++
		}
	}

	t.Logf("")
	t.Logf("판 %d개(파싱 실패 %d) · 붙은 이름 %d개 · 서로 다른 코드 %d개", parsed, failed, totalTags, len(perCode))

	// **precision 만 잰다.** floodgate 기보에 「이 국면은 美濃다」라는 라벨이 없어서,
	// 붙은 태그가 맞는지는 눈으로 볼 수 있어도 **붙어야 했는데 안 붙은 것은 안 보인다**.
	// 囲い가 21/42라 미구현 21종이 오탐과 구별되지 않는다.
	//
	// 그래서 대리 지표를 하나 둔다: 끝까지 **양쪽 다 囲い가 하나도 안 붙은 판**의 비율.
	// 프로 수준 엔진끼리의 대국이라 이 값이 크면 그만큼 못 보고 있다는 뜻이다.
	t.Logf("양쪽 다 囲い가 안 붙은 판: %d / %d", noCastle, parsed)

	type kv struct {
		code string
		n    int
	}
	var sorted []kv
	for c, n := range perCode {
		sorted = append(sorted, kv{c, n})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].n != sorted[j].n {
			return sorted[i].n > sorted[j].n
		}
		return sorted[i].code < sorted[j].code
	})
	for _, e := range sorted {
		t.Logf("  %-22s %d", e.code, e.n)
	}

	reportLatePlies(t, results)
}

// reportLatePlies 는 **축마다 이름이 처음 붙는 手数의 분포**다.
//
// 戦法(飛의 筋)과 戦型(角換わり 등)은 **序盤 분류**인데 술어에 시간 경계가 없다. 그래서
// 종반에 飛가 떠돌다 5筋에 한 번 서면 中飛車가 되고, 角이 어쩌다 교환되어 있으면
// 角換わり가 된다. 같은 판에서 한쪽이 15수에 居飛車, 131수에 中飛車가 된 것이 그 증거다.
//
// 경계를 몇 수로 둘지는 눈대중이 아니라 이 표로 정한다.
func reportLatePlies(t *testing.T, results []gameScan) {
	t.Helper()

	byKind := map[tag.Kind][]int{}
	for _, r := range results {
		if r.err != nil {
			continue
		}
		for _, list := range r.byColor {
			for _, fa := range list {
				byKind[kindOf(fa.code)] = append(byKind[kindOf(fa.code)], fa.ply)
			}
		}
	}

	byCode := map[string][]int{}
	for _, r := range results {
		if r.err != nil {
			continue
		}
		for _, list := range r.byColor {
			for _, fa := range list {
				byCode[fa.code] = append(byCode[fa.code], fa.ply)
			}
		}
	}

	cuts := []int{20, 30, 40, 60}
	header := func(what string) {
		t.Logf("")
		t.Logf("첫 등장 手数 — %s", what)
		t.Logf("  %-22s %5s %5s %5s %5s  %s", "", "개수", "중앙", "90%", "최대", "20수↑ 30수↑ 40수↑ 60수↑")
	}
	row := func(name string, plies []int) {
		if len(plies) == 0 {
			return
		}
		sort.Ints(plies)
		line := ""
		for _, c := range cuts {
			n := 0
			for _, p := range plies {
				if p > c {
					n++
				}
			}
			line += fmt.Sprintf("%5.0f%% ", 100*float64(n)/float64(len(plies)))
		}
		p90 := plies[min(len(plies)*9/10, len(plies)-1)]
		t.Logf("  %-22s %5d %5d %5d %5d  %s", name, len(plies), plies[len(plies)/2], p90, plies[len(plies)-1], line)
	}

	header("축 전체")
	for _, k := range []tag.Kind{tag.KindCastle, tag.KindFormation, tag.KindOpening} {
		row(string(k), byKind[k])
	}

	// **`ibisha` 를 갈라 본다.** 저것만 성질이 다르다 — 「振らなか다」는 상태이고 囲い이
	// 서야 뜨므로 저절로 늦다. 나머지 여섯은 「振った 수」라 序盤의 사실이다. 섞으면
	// 중앙값이 끌려 올라가서 경계를 잘못 고르게 된다.
	header("戦法·戦型을 코드별로 — 경계는 여기서 고른다")
	for _, code := range []string{
		"naka_bisha", "shiken_bisha", "sanken_bisha", "mukai_bisha", "sode_bisha", "migi_shiken_bisha",
		"ibisha",
		"kaku_gawari", "kakukan_furibisha", "ai_furibisha",
	} {
		row(code, byCode[code])
	}
}

func kindOf(code string) tag.Kind {
	for _, t := range tag.All() {
		if t.Code == code {
			return t.Kind
		}
	}
	return tag.KindTesuji
}

// isCastleCode 는 그 코드가 囲い 축인지 본다. `tag.All()` 이 축을 들고 있다.
func isCastleCode(code string) bool {
	for _, t := range tag.All() {
		if t.Code == code {
			return t.Kind == tag.KindCastle
		}
	}
	return false
}
