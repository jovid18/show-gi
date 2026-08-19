package kifu

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
)

// 실 기보로 태그가 맞게 붙는지를 넓게 본다. 지금까지 囲い·전법 태그는 손으로 만든
// 국면에서만 재봤고(09-tags.md §4), 「실제 대국에서 얼마나·어디서 뜨는가」는 안 쟀다.
//
// 엔진을 안 쓴다. 囲い·전법·戦型은 판과 수순만으로 정해지므로 이 측정이 초 단위로
// 끝나고, 그래서 고치고 다시 돌리는 것을 반복할 수 있다.
//
//	SHOWGI_KIFU_SCAN=1 go test ./internal/kifu/ -run ScanTags -v
//	SHOWGI_KIFU_SEED=2 SHOWGI_KIFU_SCAN=1 go test ./internal/kifu/ -run ScanTags -v
//
// seed를 고정하고 찍는다. 매번 다른 10판을 뽑으면 「고쳐서 나아진 것」과 「표본이
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

// floodgateFiles 는 seed 로 정해진 표본이다. 파일 이름을 먼저 정렬해야 한다 —
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

// firstAppearance 는 태그 코드마다 처음 붙은 手数다.
//
// 전이만 본다. 10판 × 120수 × 두 색을 전부 눈으로 볼 수 없고, 태그는 한 번
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

// scanOne 은 기보 하나를 수마다 재생하며 프로덕션과 같은 조건으로 태그를 뽑는다.
//
// session.go 의 styleTags() 가 부르는 그 형태 그대로여야 한다 — 여기서 입력을 다르게
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

	// 手番이 번갈아 간다는 것에 기대지 않고 적용한 색으로 가른다. 中断된 기보나
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
			// 버린 판을 센다. 조용히 줄어들면 표본 수가 거짓이 된다.
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

	// precision 만 잰다. floodgate 기보에 「이 국면은 美濃다」라는 라벨이 없어서,
	// 붙은 태그가 맞는지는 눈으로 볼 수 있어도 붙어야 했는데 안 붙은 것은 안 보인다.
	// 囲い가 21/42라 미구현 21종이 오탐과 구별되지 않는다.
	//
	// 그래서 대리 지표를 하나 둔다: 끝까지 양쪽 다 囲い가 하나도 안 붙은 판의 비율.
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

// reportLatePlies 는 축마다 이름이 처음 붙는 手数의 분포다.
//
// 戦法(飛의 筋)과 戦型(角換わり 등)은 序盤 분류인데 술어에 시간 경계가 없다. 그래서
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

	// ibisha 를 갈라 본다. 저것만 성질이 다르다 — 「振らなか다」는 상태이고 囲い이
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

// 囲い가 안 붙는 판이 왜 그런가 — 안 지은 것인지 못 본 것인지 가른다.
//
// 341판 중 226판(66%)에서 양쪽 다 囲い 이름이 없었다. 두 가지가 겹쳐 있을 수 있는데
// 대응이 정반대다: 강한 엔진이 고전 囲い를 안 짓는 것이면 고칠 것이 없고, 짓는데
// 우리가 못 보는 것이면 21/42를 넓혀야 한다.
//
// 玉의 자리가 그 둘을 가른다. 囲い는 玉을 구석으로 옮기는 일이므로, 이름이 없는데
// 玉이 2八·8八·9九 쪽에 있으면 우리가 못 본 것이고, 5九·4八 같은 가운데나 初期配置
// 그대로면 안 지은 것이다.
func TestScanKingsInGamesWithoutACastle(t *testing.T) {
	if os.Getenv("SHOWGI_KIFU_SCAN") == "" {
		t.Skip("SHOWGI_KIFU_SCAN 미설정")
	}

	files := floodgateFiles(t, scanSeed(t), scanCount(t))

	var (
		mu       sync.Mutex
		noName   = map[struct{ file, rank int }]int{}
		withName = map[struct{ file, rank int }]int{}
		igyoku   int // 居玉 — 玉이 初期配置 그대로
		sides    int
	)

	var wg sync.WaitGroup
	for _, f := range files {
		wg.Add(1)
		go func() {
			defer wg.Done()

			raw, err := os.ReadFile(f)
			if err != nil {
				return
			}
			g, err := ParseCSA(string(raw))
			if err != nil {
				return
			}
			pos, err := shogi.ParseSFEN(g.StartSFEN)
			if err != nil {
				return
			}

			// 玉이 자리를 잡은 뒤에 본다. 囲い는 보통 40수 안에 완성되고, 그보다 뒤는
			// 崩れている 중일 수 있다. 짧은 판은 마지막 국면을 쓴다.
			settle := min(40, len(g.Moves))
			for i := range settle {
				m, err := shogi.ParseUSIMove(g.Moves[i])
				if err != nil {
					return
				}
				pos = pos.Apply(m)
			}

			named := scanOne(f).byColor

			mu.Lock()
			defer mu.Unlock()
			for _, c := range []shogi.Color{shogi.Black, shogi.White} {
				sq, ok := kingSquare(pos, c)
				if !ok {
					continue
				}
				sides++

				at := struct{ file, rank int }{shogi.FileOf(sq), shogi.RankOf(sq)}
				if at == startKing(c) {
					igyoku++
				}

				hasCastle := false
				for _, fa := range named[c] {
					if isCastleCode(fa.code) {
						hasCastle = true
					}
				}
				if hasCastle {
					withName[at]++
				} else {
					noName[at]++
				}
			}
		}()
	}
	wg.Wait()

	t.Logf("40수 시점의 玉 자리 — 쪽 %d개 · 居玉 %d개(%.0f%%)", sides, igyoku, 100*float64(igyoku)/float64(sides))
	t.Logf("")
	t.Logf("  %-8s %8s %8s", "玉의 칸", "이름없음", "이름있음")
	dump(t, noName, withName)
}

func dump(t *testing.T, noName, withName map[struct{ file, rank int }]int) {
	t.Helper()

	type row struct {
		spot struct{ file, rank int }
		a, b int
	}
	seen := map[struct{ file, rank int }]bool{}
	var rows []row
	for s, n := range noName {
		seen[s] = true
		rows = append(rows, row{s, n, withName[s]})
	}
	for s, n := range withName {
		if !seen[s] {
			rows = append(rows, row{s, 0, n})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].a+rows[i].b > rows[j].a+rows[j].b })

	for i, r := range rows {
		if i >= 14 {
			break
		}
		t.Logf("  %d%-7s %8d %8d", r.spot.file, rankKanji(r.spot.rank), r.a, r.b)
	}
}

func rankKanji(rank int) string {
	return string([]rune("一二三四五六七八九")[rank-1])
}

func kingSquare(pos shogi.Position, c shogi.Color) (int, bool) {
	for sq := range pos.Board {
		p := pos.Board[sq]
		if !p.Empty() && p.Color() == c && p.Type() == shogi.King {
			return sq, true
		}
	}
	return 0, false
}

func startKing(c shogi.Color) struct{ file, rank int } {
	if c == shogi.Black {
		return struct{ file, rank int }{5, 9}
	}
	return struct{ file, rank int }{5, 1}
}

// 못 본 囲い가 어느 형태인가 — 위키 목록에서 고르지 말고 판에서 읽는다.
//
// 위 테스트가 「玉은 囲い 자리에 있는데 이름이 없는」 쪽 76개를 찾아냈고, 그 자리가
// 2八·8二(美濃 계열)와 7八·2二(矢倉 계열)에 몰려 있었다. 어느 변형을 넣어야 하는지는
// 그 국면에서 玉 주변에 실제로 무엇이 서 있는가로 정해야 한다 — 목록에서 고르면
// 실전에 안 나오는 이름부터 넣게 된다(09-tags.md 가 「한계 가치가 낮다」고 적은 자리다).
//
// 좌표는 전부 先手 기준으로 뒤집어 센다. 그래야 양쪽 표본이 한 줄에 모인다.
func TestScanWhatStandsAroundAnUnnamedKing(t *testing.T) {
	if os.Getenv("SHOWGI_KIFU_SCAN") == "" {
		t.Skip("SHOWGI_KIFU_SCAN 미설정")
	}

	files := floodgateFiles(t, scanSeed(t), scanCount(t))

	// 玉이 서 있는 囲い 자리(先手 기준). 위 측정에서 미검출이 몰린 곳이다.
	interesting := map[[2]int]string{{2, 8}: "美濃側 2八", {7, 8}: "矢倉側 7八"}

	// 玉 주변에서 囲い을 이루는 칸들(先手 2八 기준의 상대 위치).
	around := [][2]int{{1, 0}, {2, 0}, {3, 0}, {1, 1}, {0, 1}, {-1, 0}, {1, -1}, {0, -1}}

	var (
		mu      sync.Mutex
		byShape = map[string]int{}
		total   int
	)

	var wg sync.WaitGroup
	for _, f := range files {
		wg.Add(1)
		go func() {
			defer wg.Done()

			raw, err := os.ReadFile(f)
			if err != nil {
				return
			}
			g, err := ParseCSA(string(raw))
			if err != nil {
				return
			}
			pos, err := shogi.ParseSFEN(g.StartSFEN)
			if err != nil {
				return
			}
			for i := range min(40, len(g.Moves)) {
				m, err := shogi.ParseUSIMove(g.Moves[i])
				if err != nil {
					return
				}
				pos = pos.Apply(m)
			}

			named := scanOne(f).byColor

			mu.Lock()
			defer mu.Unlock()
			for _, c := range []shogi.Color{shogi.Black, shogi.White} {
				sq, ok := kingSquare(pos, c)
				if !ok {
					continue
				}
				file, rank := senteView(shogi.FileOf(sq), shogi.RankOf(sq), c)
				spot, watched := interesting[[2]int{file, rank}]
				if !watched {
					continue
				}
				for _, fa := range named[c] {
					if isCastleCode(fa.code) {
						return // 이름이 붙은 쪽은 볼 것이 없다
					}
				}

				total++
				byShape[spot+"  "+neighbourhood(pos, c, file, rank, around)]++
			}
		}()
	}
	wg.Wait()

	type kv struct {
		shape string
		n     int
	}
	var rows []kv
	for s, n := range byShape {
		rows = append(rows, kv{s, n})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].n != rows[j].n {
			return rows[i].n > rows[j].n
		}
		return rows[i].shape < rows[j].shape
	})

	t.Logf("이름 없는 玉 주변 — 쪽 %d개 · 서로 다른 배치 %d개", total, len(rows))
	for i, r := range rows {
		if i >= 15 {
			break
		}
		t.Logf("  %3d  %s", r.n, r.shape)
	}
}

// 手筋의 형태가 실 기보에서 얼마나·어디서 서는가.
//
// 지금까지 手筋은 한 판(playtestUpTo103)에서만 쟀다([journal §34](../../../../docs/journal/21-40.md)).
// 형태 6개 · 이름 2개가 그 판의 전부였고, 그 숫자로 빈도를 말할 수는 없다.
//
// 엔진을 안 쓰고 룰 층만 본다. game.NamedTesuji 에 두 cp를 같게 넣으면 낙폭 0이라
// 게이트가 언제나 통과하고, 남는 것이 정확히 freshTesuji — 프로덕션이 쓰는 그 함수다.
// 측정이 자기 규칙을 새로 쓰지 않게 하는 방법이고, §34 ⑦이 잡은 「측정과 제품이 다른
// 것을 세고 있었다」를 피하는 자리다.
//
// 그래서 이 표는 게이트 앞의 수다. 엔진이 얼마를 끄는지는 §42의 실 기보 측정이 답한다
// (형태 6개 중 4개를 껐다).
func TestScanTesujiShapesOverFloodgateGames(t *testing.T) {
	if os.Getenv("SHOWGI_KIFU_SCAN") == "" {
		t.Skip("SHOWGI_KIFU_SCAN 미설정")
	}

	files := floodgateFiles(t, scanSeed(t), scanCount(t))

	var (
		mu       sync.Mutex
		perCode  = map[string]int{}
		plies    = map[string][]int{}
		gamesHit = map[string]int{}
		games    int
		shapes   int
	)

	var wg sync.WaitGroup
	for _, f := range files {
		wg.Add(1)
		go func() {
			defer wg.Done()

			raw, err := os.ReadFile(f)
			if err != nil {
				return
			}
			g, err := ParseCSA(string(raw))
			if err != nil {
				return
			}
			pos, err := shogi.ParseSFEN(g.StartSFEN)
			if err != nil {
				return
			}

			local := map[string]int{}
			localPly := map[string][]int{}
			n := 0

			for i, u := range g.Moves {
				m, err := shogi.ParseUSIMove(u)
				if err != nil {
					return
				}
				before := pos
				mover := pos.Turn
				pos = pos.Apply(m)

				// 두 cp가 같으면 낙폭 0 — 게이트를 중립화한 룰 층이다.
				for _, tg := range game.NamedTesuji(before, pos, mover, u, 0, 0) {
					local[tg.Code]++
					localPly[tg.Code] = append(localPly[tg.Code], i+1)
					n++
				}
			}

			mu.Lock()
			defer mu.Unlock()
			games++
			shapes += n
			for c, k := range local {
				perCode[c] += k
				gamesHit[c]++
				plies[c] = append(plies[c], localPly[c]...)
			}
		}()
	}
	wg.Wait()

	t.Logf("판 %d개 · 手筋의 형태가 새로 선 수 %d개 (판당 %.1f)", games, shapes, float64(shapes)/float64(games))
	t.Logf("")
	t.Logf("  %-20s %6s %8s %6s %6s", "코드", "횟수", "나온 판", "중앙手数", "최대")

	type kv struct {
		code string
		n    int
	}
	var rows []kv
	for c, n := range perCode {
		rows = append(rows, kv{c, n})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].n != rows[j].n {
			return rows[i].n > rows[j].n
		}
		return rows[i].code < rows[j].code
	})
	for _, r := range rows {
		p := plies[r.code]
		sort.Ints(p)
		t.Logf("  %-20s %6d %6d/%d %6d %6d", r.code, r.n, gamesHit[r.code], games, p[len(p)/2], p[len(p)-1])
	}
}

// 両取り를 건 駒가 成っているか로 가른다.
//
// forkNames 는 龍·馬를 든다 — 「飛의 縦横·角의 斜め를 그대로 갖는다」가 이유였다. 그런데
// 종반에 적진에 들어간 龍은 거의 언제나 두 개를 동시에 노린다. 그러면 十字飛車라는
// 이름이 「飛로 두 방향을 찌른 手筋」이 아니라 「龍이 龍답게 서 있다」가 된다.
//
// 프로덕션 경로가 아니다. Fork 를 판 위에서 직접 훑어 駒 종류까지 본다 — 여기서
// 필요한 것이 「어느 駒였나」인데 NamedTesuji 는 이름만 돌려주기 때문이다.
func TestScanForksByPromotion(t *testing.T) {
	if os.Getenv("SHOWGI_KIFU_SCAN") == "" {
		t.Skip("SHOWGI_KIFU_SCAN 미설정")
	}

	files := floodgateFiles(t, scanSeed(t), scanCount(t))

	var (
		mu    sync.Mutex
		count = map[string]int{}
		plies = map[string][]int{}
	)

	promoted := map[shogi.PieceType]bool{shogi.PromRook: true, shogi.PromBishop: true}

	var wg sync.WaitGroup
	for _, f := range files {
		wg.Add(1)
		go func() {
			defer wg.Done()

			raw, err := os.ReadFile(f)
			if err != nil {
				return
			}
			g, err := ParseCSA(string(raw))
			if err != nil {
				return
			}
			pos, err := shogi.ParseSFEN(g.StartSFEN)
			if err != nil {
				return
			}

			local := map[string][]int{}
			for i, u := range g.Moves {
				m, err := shogi.ParseUSIMove(u)
				if err != nil {
					return
				}
				before := pos
				mover := pos.Turn
				pos = pos.Apply(m)

				// 그 수가 새로 만든 형태만 — 이미 서 있던 것을 매 수 다시 세면
				// 종반의 한 형태가 수십 번으로 부풀어 비교가 무의미해진다.
				had := map[string]bool{}
				for _, tg := range tag.FindTesuji(before, mover) {
					had[tg.Code] = true
				}
				for sq := range pos.Board {
					p := pos.Board[sq]
					if p.Empty() || p.Color() != mover {
						continue
					}
					tg, ok := tag.Fork(pos, sq, mover)
					if !ok || had[tg.Code] {
						continue
					}
					key := tg.Code
					if promoted[p.Type()] {
						key += " (成)"
					}
					local[key] = append(local[key], i+1)
				}
			}

			mu.Lock()
			defer mu.Unlock()
			for k, ps := range local {
				count[k] += len(ps)
				plies[k] = append(plies[k], ps...)
			}
		}()
	}
	wg.Wait()

	type kv struct {
		key string
		n   int
	}
	var rows []kv
	for k, n := range count {
		rows = append(rows, kv{k, n})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].n != rows[j].n {
			return rows[i].n > rows[j].n
		}
		return rows[i].key < rows[j].key
	})

	t.Logf("  %-26s %6s %8s", "両取り를 건 駒", "횟수", "중앙手数")
	for _, r := range rows {
		p := plies[r.key]
		sort.Ints(p)
		t.Logf("  %-26s %6d %8d", r.key, r.n, p[len(p)/2])
	}
}

// senteView 는 後手의 칸을 先手 기준으로 뒤집는다.
func senteView(file, rank int, c shogi.Color) (int, int) {
	if c == shogi.Black {
		return file, rank
	}
	return 10 - file, 10 - rank
}

// neighbourhood 는 玉 주변 칸에 선 자기 駒를 「칸:駒」로 늘어놓는다. 빈 칸과 상대 駒는
// 적지 않는다 — 囲い은 자기 駒의 배치이고, 나머지를 적으면 같은 형태가 수십 갈래로 흩어진다.
func neighbourhood(pos shogi.Position, c shogi.Color, kf, kr int, around [][2]int) string {
	var parts []string
	for _, d := range around {
		file, rank := kf+d[0], kr+d[1]
		if file < 1 || file > 9 || rank < 1 || rank > 9 {
			continue
		}
		bf, br := senteView(file, rank, c) // 뒤집은 것을 되돌려 실제 칸을 찾는다
		p := pos.Board[shogi.SquareOf(bf, br)]
		if p.Empty() || p.Color() != c {
			continue
		}
		parts = append(parts, fmt.Sprintf("%d%s%s", file, rankKanji(rank), pieceKanji(p.Type())))
	}
	if len(parts) == 0 {
		return "(주변에 자기 駒 없음)"
	}
	return strings.Join(parts, " ")
}

func pieceKanji(pt shogi.PieceType) string {
	switch pt {
	case shogi.Pawn:
		return "歩"
	case shogi.Lance:
		return "香"
	case shogi.Knight:
		return "桂"
	case shogi.Silver:
		return "銀"
	case shogi.Gold:
		return "金"
	case shogi.Bishop:
		return "角"
	case shogi.Rook:
		return "飛"
	case shogi.King:
		return "玉"
	}
	return "成"
}

func kindOf(code string) tag.Kind {
	for _, t := range tag.All() {
		if t.Code == code {
			return t.Kind
		}
	}
	return tag.KindTesuji
}

// isCastleCode 는 그 코드가 囲い 축인지 본다. tag.All() 이 축을 들고 있다.
func isCastleCode(code string) bool {
	for _, t := range tag.All() {
		if t.Code == code {
			return t.Kind == tag.KindCastle
		}
	}
	return false
}
