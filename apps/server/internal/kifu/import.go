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

	// 진형은 안 적는다. 들여오는 기보는 사람이 고른 것이 아니라 이미 둬진 판이다.
	gameID, err := imp.store.CreateGame(ctx, nil, "b", startSFEN, "")
	if err != nil {
		return ImportResult{}, fmt.Errorf("create game: %w", err)
	}

	res := ImportResult{GameID: gameID, MoveCount: len(g.Moves)}

	for i, move := range g.Moves {
		ply := i + 1

		// 결과를 안 쓴다 — 목적이 archive 가 남기는 positions·edges 행이고, 그게 임포트의 산출물이다(그래서 실패해도 임포트는 성립한다).
		// Wait() 로 그 기록을 DB에 내려앉힌 뒤라야 아래 Judge 가 같은 국면을 캐시로 맞힌다 — 없으면 같은 국면을 두 번 판다.
		if _, err := imp.searcher.SearchMultiPV(ctx, startSFEN, g.Moves[:i], imp.depth, imp.multiPV); err != nil {
			log.Printf("kifu: multiPV before ply %d: %v", ply, err)
		}
		imp.searcher.Wait()

		if err := imp.store.InsertMove(ctx, gameID, ply, move); err != nil {
			return res, fmt.Errorf("insert move %d: %w", ply, err)
		}

		j, err := imp.analyst.Judge(ctx, startSFEN, g.Moves[:ply], ply)
		if err != nil {
			// 수는 이미 들어갔고 평가치·개입만 빠진다 — 표본이 조용히 줄어드는 자리다(ImportResult 에 세는 칸이 없어 로그에만 남는다).
			log.Printf("kifu: judge ply %d: %v", ply, err)
			continue
		}

		if j.HasEvals {
			if err := imp.store.SetMoveEval(ctx, gameID, ply, j.SenteCpAfter); err != nil {
				log.Printf("kifu: set eval ply %d: %v", ply, err)
			}
			// 직전 회차가 After 로 적은 칸을 Before 로 덮는다 — game/session.go 의 기록과 **일부러** 같은 모양이라
			// calibrate_test.go 가 읽는 eval_cp 가 제품과 같은 값이 된다. 같은 칸에 두 탐색이 쓴다(06-status.md §41).
			if ply > 1 {
				if err := imp.store.SetMoveEval(ctx, gameID, ply-1, j.SenteCpBefore); err != nil {
					log.Printf("kifu: set eval ply %d: %v", ply-1, err)
				}
			}
		}

		v := j.Verdict
		if v.Kind != intervene.KindNone {
			// TODO: 아래 LevelBucket 은 "pro" 인데 판정은 intervene.Beginner 로 돈다(cmd/importkifu/main.go).
			// cmd/api 의 「판정과 기록이 같은 값을 본다」와 어긋나고, server 의 levelBucket() 은 "pro" 를 만들지 않는다.
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
