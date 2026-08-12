package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/archive"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/kifu"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

func main() {
	dbURL := flag.String("db", "", "database URL")
	enginePath := flag.String("engine", "/opt/yaneuraou/run", "engine command")
	poolSize := flag.Int("pool", 1, "engine pool size")
	depth := flag.Int("depth", 12, "search depth")
	multiPV := flag.Int("k", 10, "multi-PV count")
	workers := flag.Int("workers", 1, "concurrent game imports")
	flag.Parse()

	if *dbURL == "" {
		*dbURL = os.Getenv("DATABASE_URL")
	}
	if *dbURL == "" {
		log.Fatal("--db or DATABASE_URL required")
	}

	files := flag.Args()
	if len(files) == 0 {
		log.Fatal("no CSA/KIF files specified")
	}

	var games []kifu.ParsedGame
	for _, file := range files {
		g, err := parseFile(file)
		if err != nil {
			log.Printf("skip %s: %v", file, err)
			continue
		}
		games = append(games, g)
	}
	if len(games) == 0 {
		log.Fatal("no games parsed")
	}
	fmt.Printf("parsed %d games, importing with depth=%d k=%d pool=%d workers=%d\n",
		len(games), *depth, *multiPV, *poolSize, *workers)

	ctx := context.Background()
	st, err := store.Open(ctx, *dbURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer st.Close()

	pool, err := usi.NewPool(*poolSize, *enginePath, map[string]string{
		"USI_Hash": "128",
		"Threads":  "1",
	})
	if err != nil {
		log.Fatalf("engine pool: %v", err)
	}
	defer pool.Close()

	searcher := archive.Wrap(pool, st)
	analyst := game.NewEngineAnalyst(searcher, nil, intervene.Beginner)
	imp := kifu.NewImporter(st, searcher, analyst, *depth, *multiPV)

	totalStart := time.Now()
	var done atomic.Int32

	sem := make(chan struct{}, *workers)
	var wg sync.WaitGroup

	for i, g := range games {
		wg.Add(1)
		go func(idx int, g kifu.ParsedGame) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			start := time.Now()
			res, err := imp.ImportGame(ctx, g)
			elapsed := time.Since(start)
			n := done.Add(1)
			if err != nil {
				log.Printf("[%d/%d] %s: error: %v", n, len(games), g.Source, err)
				return
			}
			fmt.Printf("[%d/%d] %s: game=%d moves=%d blunders=%d (%s)\n",
				n, len(games), g.Source, res.GameID, res.MoveCount, res.Blunders, elapsed.Round(time.Second))
		}(i, g)
	}

	wg.Wait()
	fmt.Printf("\ndone. %d games in %s\n", len(games), time.Since(totalStart).Round(time.Second))
}

func parseFile(file string) (kifu.ParsedGame, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return kifu.ParsedGame{}, err
	}
	ext := strings.ToLower(filepath.Ext(file))
	var g kifu.ParsedGame
	switch ext {
	case ".kif", ".kifu":
		g, err = kifu.ParseKIF(string(data))
	case ".csa":
		g, err = kifu.ParseCSA(string(data))
	default:
		return kifu.ParsedGame{}, fmt.Errorf("unknown extension %s", ext)
	}
	if err != nil {
		return kifu.ParsedGame{}, err
	}
	g.Source = filepath.Base(file)
	return g, nil
}
