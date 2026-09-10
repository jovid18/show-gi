package server

import (
	"context"
	"log"
	"strconv"
	"strings"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 가져온 기보의 사후 분석. 줄도 워커도 대인전의 것을 그대로 탄다(match_analysis.go) —
// 재는 일이 똑같아서다: 판 하나에 手数만큼의 판정이고, 누구도 그 앞에서 기다리지 않는다.
//
// 갈리는 것 셋이다. 자리가 하나이고(대인전은 games 행 둘), 手를 한 번에 다 세우고
// (대인전은 두는 동안 하나씩), 판정 결과가 悪手 줄로 남는다(대인전은 개입이 없다).
//
// 근거는 journal §126.

// importKeyPrefix 는 분석 키의 갈래를 가른다.
//
// analysis_jobs.match_id 를 「방 id」 대신 분석 키로 읽는다. 컬럼 이름을 바꾸지 않은 것은
// 공유 DB에서 RENAME 이 남의 서버를 그 자리에서 깨뜨리기 때문이다(CLAUDE.md).
//
// 방 id 와 부딪히지 않는다 — 그쪽은 영숫자 8자라 콜론이 들어가지 않는다(internal/match).
const importKeyPrefix = "import:"

// importKey 는 가져온 판 하나의 분석 키다. 모양이 이 파일에만 있고, 표를 읽는 질의도
// 이 함수가 만든 값을 받는다(store.IsGameAnalyzing).
func importKey(gameID int64) string {
	return importKeyPrefix + strconv.FormatInt(gameID, 10)
}

// importedGameID 는 키에서 판 번호를 되짚는다. 가져온 판의 키가 아니면 ok=false 다.
func importedGameID(key string) (int64, bool) {
	rest, ok := strings.CutPrefix(key, importKeyPrefix)
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// enqueueImport 는 가져온 판의 手를 전부 큐에 세우고 그 판을 「분석 중」으로 만든다.
//
// 이 갈래는 手를 한 번에 다 세운다. 수순 전부를 이미 알기 때문이고, 그래서
// 워커가 몇이든 手들이 병렬로 재어진다 — 판이 집힐 때는 대개 다 재어져 있어서
// analyze 가 결과를 모으기만 한다.
//
// 手를 먼저 세우고 판을 나중에 세운다. 반대로 하면 아직 手가 하나도 없는 판이 집혀
// 그 자리에서 手数만큼을 혼자 재게 된다.
func (a *matchAnalyzer) enqueueImport(ctx context.Context, gameID int64, startSFEN string, moves []string) error {
	if a == nil || a.store == nil {
		return nil
	}
	key := importKey(gameID)

	plies := make([]store.AnalysisPly, 0, len(moves))
	for i := range moves {
		plies = append(plies, store.AnalysisPly{
			MatchID:   key,
			Ply:       i + 1,
			StartSFEN: startSFEN,
			// 수순을 복사한다. 아래 append 가 같은 배열을 다시 쓰면 세워 둔 행들이
			// 서로의 수순을 보게 된다.
			Moves: append([]string(nil), moves[:i+1]...),
		})
	}
	if err := a.store.BulkEnqueueAnalysisPlies(ctx, plies); err != nil {
		return err
	}
	return a.store.ReadyAnalysisJob(ctx, key, len(moves))
}

// importSeat 은 가져온 판의 자리 하나다. 읽지 못하면 빈 목록이라 부르는 쪽이 그 판을
// 큐에서 걷는다(runOneJob).
func (a *matchAnalyzer) importSeat(ctx context.Context, gameID int64) []analysisSeat {
	row, err := a.store.ImportSeat(ctx, gameID)
	if err != nil {
		log.Printf("kifu: could not read the seat of imported game %d: %v", gameID, err)
		return nil
	}
	return []analysisSeat{{gameID: row.GameID, userID: row.UserID, color: colorOf(row.Color)}}
}

// recordBlunder 는 그 手의 판정을 悪手 줄로 남긴다.
//
// 여기서 둔 판의 개입과 같은 표를 쓴다(interventions). 그래야 되짚기의 목록도 마이페이지의
// 「崩れやすいところ」도 한 줄 고치지 않고 가져온 판을 같이 센다 — 사람이 정한 것이
// 「전부 합친다」다(journal §126).
//
// retracted_usi 를 적지 않는다. 그 칸은 「개입이 막지 않았다면 뒀을 수」인데 가져온 판에서는
// 누구도 막지 않았고 그 수가 기보에 그대로 남아 있다 — 적으면 없던 일을 있었다고 말하는 것이다.
// 화면은 그 手数의 수를 기보에서 찾는다(web 의 ReviewDetail).
func (a *matchAnalyzer) recordBlunder(ctx context.Context, gameID int64, ply int, mover shogi.Color, got judged) {
	// 평가치는 두는 쪽 관점으로 뒤집는다. judged 에 담긴 것은 先手 관점이고
	// (game.Judgement.SenteAfter) interventions 의 두 칸은 두는 쪽 관점이다
	// (intervene.Verdict.After). 뒤집지 않으면 後手가 둔 悪手의 부호 전체가 반대가 된다.
	after := got.after
	if mover == shogi.White {
		after = after.Neg()
	}

	iv := store.Intervention{
		Ply:         ply,
		Kind:        string(intervene.KindBlunder),
		Category:    got.category,
		DeltaWin:    got.move.DeltaWin,
		LevelBucket: levelBucket(a.level),
		Best:        got.best,
		After:       after,
	}
	if err := a.store.InsertIntervention(ctx, gameID, iv); err != nil && ctx.Err() == nil {
		log.Printf("kifu: could not record the blunder at ply %d of game %d: %v", ply, gameID, err)
	}
}
