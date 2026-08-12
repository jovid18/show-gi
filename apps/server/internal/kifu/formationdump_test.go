package kifu

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
)

// 전법 태그가 붙은 **실제 사례**를 사람이 읽을 수 있게 떨어뜨린다.
//
// 숫자로는 여기까지가 끝이다 — 「四間飛車가 48번 붙었고 중앙 8수」는 **맞는지**를 말하지
// 않는다. 라벨이 없으므로 결국 수순을 보고 판단해야 하고, 그러려면 수순이 日本語 표기로
// 나와 있어야 한다.
//
//	SHOWGI_KIFU_DUMP=/tmp/formations go test ./internal/kifu/ -run DumpFormations
const dumpOpeningPlies = 40

func TestDumpFormationCases(t *testing.T) {
	outDir := os.Getenv("SHOWGI_KIFU_DUMP")
	if outDir == "" {
		t.Skip("SHOWGI_KIFU_DUMP 미설정")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("출력 디렉터리: %v", err)
	}

	files := floodgateFiles(t, scanSeed(t), scanCount(t))

	type hit struct {
		file  string
		side  shogi.Color
		ply   int
		usi   string
		moves []string // 日本語 표기
	}
	byCode := map[string][]hit{}

	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		g, err := ParseCSA(string(raw))
		if err != nil {
			continue
		}
		pos, err := shogi.ParseSFEN(g.StartSFEN)
		if err != nil {
			continue
		}

		// 日本語 표기는 **착수 전 국면**에서 만들어야 한다(session.apply 와 같다).
		var ja []string
		prevTo := -1
		moves := map[shogi.Color][]string{}
		seen := map[shogi.Color]map[string]bool{shogi.Black: {}, shogi.White: {}}
		found := map[string][]hit{}

		for i, u := range g.Moves {
			m, err := shogi.ParseUSIMove(u)
			if err != nil {
				break
			}
			mover := pos.Turn
			if i < dumpOpeningPlies {
				ja = append(ja, pos.MoveJa(m, prevTo))
			}
			pos = pos.Apply(m)
			prevTo = int(m.To)
			moves[mover] = append(moves[mover], u)

			for _, c := range []shogi.Color{shogi.Black, shogi.White} {
				for _, tg := range tag.Detect(tag.Input{
					Pos: pos, Color: c,
					PlayerMoves: moves[c], OpponentMoves: moves[c.Other()],
				}) {
					if tg.Kind != tag.KindFormation && tg.Kind != tag.KindOpening {
						continue
					}
					if seen[c][tg.Code] {
						continue
					}
					seen[c][tg.Code] = true
					found[tg.Code] = append(found[tg.Code], hit{file: filepath.Base(f), side: c, ply: i + 1, usi: u})
				}
			}
		}

		// 수순은 재생이 끝나야 완성된다. 그래서 붙이는 것도 여기서 한 번에 한다.
		for code, hs := range found {
			for _, h := range hs {
				h.moves = ja
				byCode[code] = append(byCode[code], h)
			}
		}
	}

	// **엣지 하나에 파일 하나.** 서브에이전트 하나가 케이스 하나만 보게 하려는 것이라,
	// 태그별로 묶으면 안 된다 — 묶으면 「이 태그는 대체로 맞다」 같은 뭉뚱그린 답이 온다.
	codes := make([]string, 0, len(byCode))
	for c := range byCode {
		codes = append(codes, c)
	}
	sort.Strings(codes)

	n := 0
	for _, code := range codes {
		nameJa := code
		for _, tg := range tag.All() {
			if tg.Code == code {
				nameJa = tg.NameJa
			}
		}

		for _, h := range byCode[code] {
			n++
			side, other := "先手", "後手"
			if h.side == shogi.White {
				side, other = "後手", "先手"
			}

			var b strings.Builder
			fmt.Fprintf(&b, "# 판정 대상 — `%s` (%s)\n\n", code, nameJa)
			fmt.Fprintf(&b, "| | |\n|---|---|\n")
			fmt.Fprintf(&b, "| 기보 | `%s` |\n", h.file)
			fmt.Fprintf(&b, "| 어느 쪽에 붙었나 | **%s** (상대는 %s) |\n", side, other)
			fmt.Fprintf(&b, "| 이름이 붙은 수 | **%d手째 `%s`** |\n\n", h.ply, h.usi)
			fmt.Fprintf(&b, "## 그 판의 첫 %d手\n\n```\n", dumpOpeningPlies)
			for j, mv := range h.moves {
				fmt.Fprintf(&b, "%3d %s", j+1, mv)
				if (j+1)%6 == 0 {
					fmt.Fprintf(&b, "\n")
				} else {
					fmt.Fprintf(&b, "  ")
				}
			}
			fmt.Fprintf(&b, "\n```\n")

			path := filepath.Join(outDir, fmt.Sprintf("case-%02d-%s.md", n, code))
			if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
				t.Fatalf("쓰기: %v", err)
			}
		}
	}
	t.Logf("케이스 %d개 → %s", n, outDir)
}
