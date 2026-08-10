package game

import (
	"os"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

var playtestUpTo69 = []string{
	"7g7f", "7c7d", "2g2f", "7a6b", "2f2e", "4a3b", "3i3h", "5a5b", "3h2g",
	"8a7c", "2g2f", "6a5a", "2e2d", "2c2d", "2f3e", "8c8d", "3e2d", "3c3d",
	"8h2b+", "3b2b", "P*2c", "2b3b", "5i6h", "5a4b", "7i7h", "8b8a", "B*5e",
	"B*4d", "5e4d", "4c4d", "B*5e", "8d8e", "5e4d", "8e8f", "8g8f", "2a3c",
	"4i5h", "P*2e", "3g3f", "7c6e", "6g6f", "5b4c", "4d5e", "5c5d", "5e4f",
	"8a8f", "P*8g", "8f8a", "6f6e", "P*4e", "4f3g", "B*8h", "6i7i", "8h9i+",
	"7h7g", "1a1b", "3f3e", "9a9b", "4g4f", "L*8c", "8g8f", "3d3e", "2d3e",
	"8c8f", "P*3d", "8a8e", "3d3c+", "3b3c",
}

// 69수째 `▲2五飛`의 반박 수순이 왜 한 수로 끊겼는지 본다(docs/08-playtest.md §7).
func TestRealEngineRefutationDiag(t *testing.T) {
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" {
		t.Skip("SHOWGI_USI_CMD 미설정")
	}
	pool, err := usi.NewPool(1, cmd, map[string]string{
		"USI_Hash": "128", "Threads": "1", "FV_SCALE": "24",
		"BookFile": "no_book", "USI_OwnBook": "false",
	})
	if err != nil {
		t.Fatalf("풀: %v", err)
	}
	defer pool.Close()

	if len(playtestUpTo69) != 68 {
		t.Fatalf("68수여야 한다: %d", len(playtestUpTo69))
	}
	moves := append(append([]string{}, playtestUpTo69...), "2h2e")

	after, err := pool.SearchDepth(t.Context(), shogi.StartSFEN, moves, JudgeDepth)
	if err != nil {
		t.Fatalf("착수 후 탐색: %v", err)
	}
	t.Logf("PV(%d수): %s", len(after.PV), strings.Join(after.PV, " "))

	// 각 수의 사실을 그대로 찍는다 — settles·captureSq·gaveCheck 가 trim 의 입력이다.
	pos, err := positionAfter(shogi.StartSFEN, moves)
	if err != nil {
		t.Fatalf("국면: %v", err)
	}
	prevTo := -1
	if len(moves) > 0 {
		if m, err := shogi.ParseUSIMove(moves[len(moves)-1]); err == nil {
			prevTo = int(m.To)
		}
	}
	for i, u := range after.PV {
		m, err := shogi.ParseUSIMove(u)
		if err != nil {
			t.Logf("%2d %-6s ← 못 읽는다: %v", i, u, err)
			break
		}
		if err := pos.ValidateMove(m); err != nil {
			t.Logf("%2d %-6s ← 합법이 아니다: %v", i, u, err)
			break
		}
		capSq := -1
		if !m.IsDrop() && !pos.Board[m.To].Empty() {
			capSq = int(m.To)
		}
		ja := pos.MoveJa(m, prevTo)
		pos = pos.Apply(m)
		prevTo = int(m.To)
		checks := checkLines(pos)
		t.Logf("%2d %-6s %-10s capSq=%3d gaveCheck=%v settles=%v", i, u, ja, capSq, len(checks) > 0, capSq >= 0 || len(checks) > 0)
	}

	// △8九香成 뒤에 **되잡는 수가 실제로 나쁜가.** 사람은 「金 하나가 지킨다」고 셌고
	// 엔진은 되잡지 않았다 — 그 차이를 숫자로 본다.
	afterLance := append(append([]string{}, moves...), after.PV[0])
	lp, err := positionAfter(shogi.StartSFEN, afterLance)
	if err != nil {
		t.Fatalf("香成 뒤 국면: %v", err)
	}
	cand, err := pool.SearchMultiPV(t.Context(), shogi.StartSFEN, afterLance, JudgeDepth, CandidateK)
	if err != nil {
		t.Fatalf("후보: %v", err)
	}
	target := int8(-1)
	if m, err := shogi.ParseUSIMove(after.PV[0]); err == nil {
		target = m.To
	}
	t.Logf("--- △%s 뒤 내 후보 (수번=%v) ---", after.PV[0], lp.Turn)
	for i, ln := range cand.Lines {
		if ln.Move == "" {
			continue
		}
		mark := ""
		if m, err := shogi.ParseUSIMove(ln.Move); err == nil && !m.IsDrop() && m.To == target {
			mark = "  ← 되잡는 수"
		}
		t.Logf("%2d위 %-6s %+5d%s", i+1, ln.Move, ln.ScoreCp, mark)
	}

	// **되잡는 수를 직접 둬 본다.** 사람이 기대한 수순이 실재하는가.
	for _, m := range lp.LegalMoves() {
		if m.IsDrop() || m.To != target {
			continue
		}
		reply := append(append([]string{}, afterLance...), m.USI())
		r, err := pool.SearchDepth(t.Context(), shogi.StartSFEN, reply, JudgeDepth)
		if err != nil {
			t.Fatalf("되잡기 탐색: %v", err)
		}
		// 엔진은 수번 측(=상대) 관점으로 답한다. 내 관점으로 뒤집는다.
		t.Logf("되잡는 수 %-6s → 내 관점 %+5d, 상대의 다음 수 %s (PV %s)",
			m.USI(), -r.ScoreCp, r.Best, strings.Join(r.PV[:min(4, len(r.PV))], " "))
	}

	_, _, line := refutationLine(shogi.StartSFEN, moves, after.PV, RefutationPlies)
	var ja []string
	for _, mv := range line {
		ja = append(ja, mv.Ja)
	}
	t.Logf("실제로 화면에 가는 것: %d수 — %s", len(line), strings.Join(ja, " "))
}
