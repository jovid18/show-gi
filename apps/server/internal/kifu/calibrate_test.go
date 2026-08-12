package kifu

import (
	"context"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/store"
)

func TestCalibrateK(t *testing.T) {
	dbURL := os.Getenv("SHOWGI_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SHOWGI_TEST_DATABASE_URL required")
	}

	ctx := context.Background()
	st, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	type sample struct {
		cp       int
		senteWon bool
	}

	rows, err := st.Pool().Query(ctx, `
		SELECT gm.eval_cp, g.result
		FROM game_moves gm
		JOIN games g ON g.id = gm.game_id
		WHERE g.user_id IS NULL
		  AND g.result IN ('win', 'loss')
		  AND gm.eval_cp IS NOT NULL
	`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var samples []sample
	for rows.Next() {
		var cp int32
		var result string
		if err := rows.Scan(&cp, &result); err != nil {
			t.Fatal(err)
		}
		samples = append(samples, sample{cp: int(cp), senteWon: result == "win"})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	if len(samples) == 0 {
		t.Skip("no pro game data in DB (import games first)")
	}
	t.Logf("samples: %d", len(samples))

	bestK := 0
	bestLoss := math.MaxFloat64

	fmt.Fprintf(os.Stderr, "\n%6s %12s %12s\n", "K", "log-loss", "accuracy")
	fmt.Fprintf(os.Stderr, "%6s %12s %12s\n", "------", "------------", "------------")

	for k := 200; k <= 1200; k += 25 {
		totalLoss := 0.0
		correct := 0

		for _, s := range samples {
			predicted := 1.0 / (1.0 + math.Exp(-float64(s.cp)/float64(k)))
			actual := 0.0
			if s.senteWon {
				actual = 1.0
			}

			p := clamp(predicted, 1e-10, 1-1e-10)
			totalLoss += -actual*math.Log(p) - (1-actual)*math.Log(1-p)

			if (predicted > 0.5) == s.senteWon {
				correct++
			}
		}

		avgLoss := totalLoss / float64(len(samples))
		accuracy := float64(correct) / float64(len(samples))

		fmt.Fprintf(os.Stderr, "%6d %12.6f %12.4f\n", k, avgLoss, accuracy)

		if avgLoss < bestLoss {
			bestLoss = avgLoss
			bestK = k
		}
	}

	fmt.Fprintf(os.Stderr, "\nbest K = %d (log-loss = %.6f)\n\n", bestK, bestLoss)
	t.Logf("best K = %d (log-loss = %.6f)", bestK, bestLoss)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
