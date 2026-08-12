package kifu

import (
	"context"
	"fmt"
	"log"

	"github.com/jovid18/show-gi/apps/server/internal/archive"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

type ImportResult struct {
	GameID    int64
	MoveCount int
	Blunders  int
}

type Importer struct {
	store    *store.Store
	searcher *archive.Searcher
	analyst  game.Analyst
	depth    int
	multiPV  int
}

func NewImporter(st *store.Store, searcher *archive.Searcher, analyst game.Analyst, depth, multiPV int) *Importer {
	return &Importer{
		store:    st,
		searcher: searcher,
		analyst:  analyst,
		depth:    depth,
		multiPV:  multiPV,
	}
}

func (imp *Importer) ImportGame(ctx context.Context, g ParsedGame) (ImportResult, error) {
	startSFEN := g.StartSFEN
	if startSFEN == "" {
		startSFEN = "lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1"
	}

	gameID, err := imp.store.CreateGame(ctx, nil, "b", startSFEN)
	if err != nil {
		return ImportResult{}, fmt.Errorf("create game: %w", err)
	}

	res := ImportResult{GameID: gameID, MoveCount: len(g.Moves)}

	for i, move := range g.Moves {
		ply := i + 1

		if _, err := imp.searcher.SearchMultiPV(ctx, startSFEN, g.Moves[:i], imp.depth, imp.multiPV); err != nil {
			log.Printf("kifu: multiPV before ply %d: %v", ply, err)
		}
		imp.searcher.Wait()

		if err := imp.store.InsertMove(ctx, gameID, ply, move); err != nil {
			return res, fmt.Errorf("insert move %d: %w", ply, err)
		}

		j, err := imp.analyst.Judge(ctx, startSFEN, g.Moves[:ply], ply)
		if err != nil {
			log.Printf("kifu: judge ply %d: %v", ply, err)
			continue
		}

		if j.HasEvals {
			if err := imp.store.SetMoveEval(ctx, gameID, ply, j.SenteCpAfter); err != nil {
				log.Printf("kifu: set eval ply %d: %v", ply, err)
			}
			if ply > 1 {
				if err := imp.store.SetMoveEval(ctx, gameID, ply-1, j.SenteCpBefore); err != nil {
					log.Printf("kifu: set eval ply %d: %v", ply-1, err)
				}
			}
		}

		v := j.Verdict
		if v.Kind != intervene.KindNone {
			iv := store.Intervention{
				Ply:          ply,
				Kind:         string(v.Kind),
				Category:     string(v.Category),
				DeltaWin:     v.DeltaWin,
				LevelBucket:  "pro",
				RetractedUSI: move,
				BestCp:       v.BestCp,
				AfterCp:      v.AfterCp,
			}
			if err := imp.store.InsertIntervention(ctx, gameID, iv); err != nil {
				log.Printf("kifu: intervention ply %d: %v", ply, err)
			}
			res.Blunders++
		}
	}

	if _, err := imp.searcher.SearchMultiPV(ctx, startSFEN, g.Moves, imp.depth, imp.multiPV); err != nil {
		log.Printf("kifu: multiPV after last move: %v", err)
	}
	imp.searcher.Wait()

	storeResult := storeGameResult(g.Result)
	if err := imp.store.FinishGame(ctx, gameID, storeResult); err != nil {
		return res, fmt.Errorf("finish game: %w", err)
	}

	return res, nil
}

func storeGameResult(r GameResult) store.GameResult {
	switch r {
	case ResultSenteWin:
		return store.ResultWin
	case ResultGoteWin:
		return store.ResultLoss
	case ResultDraw:
		return store.ResultDraw
	default:
		return store.ResultAbandoned
	}
}
