package kifu

import (
	"context"
	"os"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/archive"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

func TestImportGame(t *testing.T) {
	dbURL := os.Getenv("SHOWGI_TEST_DATABASE_URL")
	enginePath := os.Getenv("SHOWGI_TEST_ENGINE_PATH")
	if dbURL == "" || enginePath == "" {
		t.Skip("SHOWGI_TEST_DATABASE_URL and SHOWGI_TEST_ENGINE_PATH required")
	}

	ctx := context.Background()
	st, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	pool, err := usi.NewPool(1, enginePath, map[string]string{
		"USI_Hash": "64",
		"Threads":  "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	searcher := archive.Wrap(pool, st)
	analyst := game.NewEngineAnalyst(searcher, nil, intervene.Beginner)
	imp := NewImporter(st, searcher, analyst, 8, 3)

	g := ParsedGame{
		StartSFEN: "lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1",
		Moves: []string{
			"7g7f", "3c3d", "2g2f", "8c8d",
			"2f2e", "8d8e", "6i7h", "4a3b",
			"2e2d", "2c2d",
		},
		Result: ResultSenteWin,
		Sente:  "Test Sente",
		Gote:   "Test Gote",
	}

	res, err := imp.ImportGame(ctx, g)
	if err != nil {
		t.Fatal(err)
	}

	if res.MoveCount != 10 {
		t.Errorf("MoveCount = %d, want 10", res.MoveCount)
	}
	if res.GameID == 0 {
		t.Error("GameID is 0")
	}
	t.Logf("imported game %d: %d moves, %d blunders", res.GameID, res.MoveCount, res.Blunders)
}
