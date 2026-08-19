package game

import (
	"os"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// playtestUpTo103 은 실제 대국(games.id=84)의 102수까지다. USI 그대로 DB에서 나왔고,
// 103수째에 물러진 7c8d(不成)는 기보에 남지 않으므로 여기에도 없다(docs/08-playtest.md §4).
var playtestUpTo103 = []string{
	"7g7f", "7c7d", "2g2f", "7a6b", "2f2e", "4a3b", "3i3h", "5a5b", "3h2g",
	"8a7c", "2g2f", "6a5a", "2e2d", "2c2d", "2f3e", "8c8d", "3e2d", "3c3d",
	"8h2b+", "3b2b", "P*2c", "2b3b", "5i6h", "5a4b", "7i7h", "8b8a", "B*5e",
	"B*4d", "5e4d", "4c4d", "B*5e", "8d8e", "5e4d", "8e8f", "8g8f", "2a3c",
	"4i5h", "P*2e", "3g3f", "7c6e", "6g6f", "5b4c", "4d5e", "5c5d", "5e4f",
	"8a8f", "P*8g", "8f8a", "6f6e", "P*4e", "4f3g", "B*8h", "6i7i", "8h9i+",
	"7h7g", "1a1b", "3f3e", "9a9b", "4g4f", "L*8c", "8g8f", "3d3e", "2d3e",
	"8c8f", "P*3d", "8a8e", "3d3c+", "3b3c", "P*8h", "P*8g", "2h2e", "4c5c",
	"2c2b+", "8g8h+", "2b3a", "8h7i", "S*4d", "5c5b", "2e2b+", "8f8g+", "4d3c+",
	"G*6i", "6h6g", "9i8i", "6g6f", "5b6a", "3c4b", "8g7g", "4b5b", "6a7b",
	"5b6b", "7b8c", "5g5f", "S*3h", "N*7e", "7d7e", "P*8d", "8c8d", "S*7c",
	"8d9d", "G*8d", "8e8d",
}

// TestRealEngineUnpromotedIsNotGreed 는 제품이 틀린 것을 가르쳤던 그 국면을 다시 판정한다.
//
// ▲同銀不成이 greedy_capture 로 나가 「잡으면 안 된다」로 읽히던 국면이다 — 최선수가
// 같은 이동의 成이었고 差는 成하느냐뿐이었다(08-playtest.md §8).
//
//	SHOWGI_USI_CMD=/opt/yaneuraou/run go test ./internal/game/ -run RealEngineUnpromoted -v
func TestRealEngineUnpromotedIsNotGreed(t *testing.T) {
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

	if len(playtestUpTo103) != 102 {
		t.Fatalf("102수여야 한다: %d", len(playtestUpTo103))
	}

	analyst := NewEngineAnalyst(pool, nil, intervene.Beginner)
	moves := append(append([]string{}, playtestUpTo103...), "7c8d")

	j, err := analyst.Judge(t.Context(), shogi.StartSFEN, moves, len(moves))
	if err != nil {
		t.Fatalf("판정: %v", err)
	}

	t.Logf("최선수=%s  카테고리=%s  Δ승률=%.3f", j.BestUSI, j.Verdict.Category, j.Verdict.DeltaWin)

	if j.Verdict.Kind != intervene.KindBlunder {
		t.Fatalf("개입이 걸려야 한다: %+v", j.Verdict)
	}
	// 이 국면의 최선수가 같은 이동의 成 쪽이라는 것이 이 테스트의 전제다.
	if j.BestUSI != "7c8d+" {
		t.Fatalf("최선수가 7c8d+ 여야 전제가 성립한다: %s", j.BestUSI)
	}
	if j.Verdict.Category != intervene.CategoryUnpromoted {
		t.Fatalf("%s — 成하지 않은 것이 이유여야 한다. 잡는 것은 정답이었다", j.Verdict.Category)
	}
}
