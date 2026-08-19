// Command importkifu 는 실 기보(KIF·CSA)를 제품과 같은 경로(엔진 → archive → store)로 다시 둬 DB에 남기는 오프라인 배치다.
// 산출물은 태그 스캔과 K 실측(internal/kifu/calibrate_test.go)이 먹는 표본이라, 서버(cmd/api)는 이 프로그램을 부르지 않는다.
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
	// 워커를 늘려도 아래 archive.Searcher 는 하나다 — 한 워커의 Wait() 가 다른 워커의 기록까지 기다리고, WaitGroup 이
	// 「카운터가 0일 때의 Add 는 Wait 보다 먼저」를 요구해 패닉 여지도 생긴다. 올릴 거면 워커마다 Searcher 를 따로 만든다.
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
	// 詰み solver 를 안 붙인다(nil) — 종반 판정만 빠지고 승률 낙폭 판정은 그대로 돈다.
	// 임계치는 Beginner 인데 import.go 가 기록에 남기는 LevelBucket 은 "pro" 라 갈려 있다 — 그쪽 TODO.
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
