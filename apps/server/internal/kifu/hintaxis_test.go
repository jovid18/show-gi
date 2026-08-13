package kifu

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
)

// 제안형 힌트가 **축마다 몇 번 말하는가**. 전법을 뺀 뒤에도 채널이 살아 있는지를 보는 자리다
// (회차 1 #0 · 06-status.md §71).
//
//	SHOWGI_KIFU_SCAN=1 SHOWGI_KIFU_GAMES=40 go test ./internal/kifu/ -run ScanHintAxes -v
//
// `game.computeTagHints` 와 **같은 물음**을 던진다 — 지금 국면의 합법수 중 아직 없는 이름을
// 만드는 것이 있는가. 다른 것은 상한·쿨다운을 안 보는 것뿐이라, 여기 숫자는 「말할 수 있는
// 자리」의 상한이다.
func hintAxesOne(path string, maxPly int) (map[tag.Kind]int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	g, err := ParseCSA(string(raw))
	if err != nil {
		return nil, err
	}
	pos, err := shogi.ParseSFEN(g.StartSFEN)
	if err != nil {
		return nil, err
	}

	out := map[tag.Kind]int{}
	moves := map[shogi.Color][]string{}

	for i, u := range g.Moves {
		if i >= maxPly {
			break
		}
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			return nil, err
		}
		mover := pos.Turn
		// 두기 **전**의 국면에서 묻는다 — 제안은 착수 전에 나간다.
		c := mover
		have := map[string]bool{}
		for _, t := range tag.Detect(tag.Input{
			Pos: pos, Color: c, PlayerMoves: moves[c], OpponentMoves: moves[c.Other()],
		}) {
			have[t.Code] = true
		}
		seen := map[tag.Kind]bool{}
		for _, cand := range pos.LegalMoves() {
			after := pos.Apply(cand)
			mine := append(append([]string(nil), moves[c]...), cand.USI())
			for _, t := range tag.Detect(tag.Input{
				Pos: after, Color: c, PlayerMoves: mine, OpponentMoves: moves[c.Other()],
			}) {
				if !have[t.Code] && !seen[t.Kind] {
					seen[t.Kind] = true
				}
			}
		}
		for k := range seen {
			out[k]++
		}

		pos = pos.Apply(m)
		moves[mover] = append(moves[mover], u)
	}
	return out, nil
}

func TestScanHintAxes(t *testing.T) {
	if os.Getenv("SHOWGI_KIFU_SCAN") == "" {
		t.Skip("SHOWGI_KIFU_SCAN 미설정")
	}
	files := floodgateFiles(t, scanSeed(t), scanCount(t))

	// 序盤만 본다. 이름이 선언되는 구간이 거기이고(tag.OpeningPlies), 전체를 돌면
	// 합법수 × 手数가 커져 측정이 분 단위가 된다.
	const maxPly = 40

	res := make([]map[tag.Kind]int, len(files))
	var wg sync.WaitGroup
	for i, f := range files {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m, err := hintAxesOne(f, maxPly)
			if err != nil {
				t.Logf("✗ %s — %v", filepath.Base(f), err)
				return
			}
			res[i] = m
		}()
	}
	wg.Wait()

	total := map[tag.Kind]int{}
	games := 0
	spoke := map[tag.Kind]int{}
	for _, m := range res {
		if m == nil {
			continue
		}
		games++
		for k, n := range m {
			total[k] += n
			if n > 0 {
				spoke[k]++
			}
		}
	}
	t.Logf("판 %d · 앞 %d手만 · 양쪽 합쳐 %d 手数", games, maxPly, games*maxPly)
	for _, k := range []tag.Kind{tag.KindCastle, tag.KindFormation, tag.KindOpening} {
		t.Logf("  %-10s 말할 수 있던 手数 %5d · 그런 판 %d/%d", k, total[k], spoke[k], games)
	}
}
