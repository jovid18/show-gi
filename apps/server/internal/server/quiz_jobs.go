package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/book"
	"github.com/jovid18/show-gi/apps/server/internal/handicap"
	"github.com/jovid18/show-gi/apps/server/internal/metrics"
	"github.com/jovid18/show-gi/apps/server/internal/quiz"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 되짚기 문항을 만드는 줄과 그 소비자. 판을 재는 줄(019)에서 뗀 자리다(journal §138).
//
// 세우는 자리가 둘이다. 엔진 대국이 끝나는 자리(ws.go 의 sendSummary)와 가져온 판을
// 다 잰 자리(match_analysis.go 의 analyze)다. 대인전은 아직 문항을 만들지 않는다.

// quizLease 는 집어 간 판을 되찾기까지 기다리는 시간이다. quizTimeout 보다 넉넉하다.
// 짧게 잡으면 아직 만드는 중인 판을 남이 다시 집어 같은 국면을 두 번 잰다.
const quizLease = 30 * time.Minute

// quizAttempts 는 한 판의 문항을 몇 번까지 만들어 볼 것인가다. 한 번이 최대 5분이라
// 상한이 곧 그 판에 쓸 엔진 시간이다(journal §138).
const quizAttempts = 3

// queueQuiz 는 그 판의 문항을 큐에 세운다. 세웠으면 참이다.
//
// 거짓이면 부르는 쪽이 그 자리에서 만든다. 배포가 마이그레이션보다 먼저 나가는 창에서는
// 표가 없어 이 문장이 실패하고, 그 창에서 끝난 판이 영영 문항을 갖지 못한다(journal §138).
func (a *matchAnalyzer) queueQuiz(ctx context.Context, gameID int64) bool {
	if a == nil || a.store == nil {
		return false
	}
	// 취소를 벗기고 시한만 준다(dropQuiz 와 같은 이유). 분석 워커가 부르는 자리에서는
	// 종료 중에 이 ctx 가 이미 죽어 있고, 그대로 쓰면 세우기가 실패해 그 판이 줄에도
	// 남지 않는다.
	//
	// 시한이 필요한 것은 대국이 끝나는 자리다(ws.go 의 sendSummary). 거기서는 이 INSERT
	// 뒤에 총평이 나가므로, 커넥션이 마르면 사람이 총평을 못 받는다.
	write, cancel := context.WithTimeout(context.WithoutCancel(ctx), quizSaveTimeout)
	defer cancel()
	if err := a.store.EnqueueQuizJob(write, gameID); err != nil {
		if ctx.Err() == nil {
			log.Printf("quiz: could not queue game %d: %v", gameID, err)
		}
		return false
	}
	return true
}

// buildQuizNow 는 큐를 지나지 않고 만든다. 큐에 세우지 못한 자리에서만, 떨어져 나온
// goroutine 으로 부른다.
//
// 자리를 기다린다. 워커가 아니라 기다려도 막는 것이 없고, 기다리지 않으면 표가 없는
// 배포에서 끝나는 판마다 5분짜리 탐색이 하나씩 떠서 풀을 다 가져간다.
func (a *matchAnalyzer) buildQuizNow(ctx context.Context, gameID int64) {
	if a == nil || a.store == nil {
		return
	}
	release, ok := a.awaitQuizSlot(ctx)
	if !ok {
		return
	}
	defer release()

	// 읽는 데 시한을 준다. 이 ctx 는 취소되지 않으므로(부르는 쪽이 WithoutCancel 이다)
	// 걸리면 자리를 잡은 채로 영영 서 있는다.
	read, cancel := context.WithTimeout(ctx, quizSaveTimeout)
	defer cancel()
	rec, err := a.store.GameRecordAnyOwner(read, gameID)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("quiz: could not read game %d to build its quiz: %v", gameID, err)
		}
		return
	}
	generateQuiz(ctx, a.store, a.quiz, rec)
}

// runOneQuiz 는 문항을 만들 판 하나를 집어 만들고 큐에서 걷는다. 집을 것이 없으면 false 다.
//
// 남기지 못하면 걷지 않는다. 그 행은 리스가 낡은 뒤 다시 집힌다(journal §138).
func (a *matchAnalyzer) runOneQuiz(ctx context.Context) bool {
	if a.store == nil {
		return false
	}
	// 자리가 없으면 집지 않는다. 워커를 다 가져가면 판도 手도 그동안 서 있다(quizSlots).
	release, ok := a.takeQuizSlot()
	if !ok {
		return false
	}
	defer release()

	gameID, err := a.store.ClaimQuizJob(ctx, time.Now().Add(-quizLease), quizAttempts)
	if errors.Is(err, store.ErrNoQuizJob) {
		return false
	}
	if err != nil {
		if ctx.Err() == nil {
			a.quizClaimLog.Do(func() {
				log.Printf("quiz: could not claim a game (logged once): %v", err)
			})
		}
		return false
	}

	// 읽는 데 시한을 준다. 자리를 이미 잡았으므로(위) 여기서 걸리면 이 프로세스의 문항이
	// 하나도 만들어지지 않는다 — 워커가 둘인 배포에서 자리가 하나뿐이다.
	read, cancel := context.WithTimeout(ctx, quizSaveTimeout)
	rec, err := a.store.GameRecordAnyOwner(read, gameID)
	cancel()
	if errors.Is(err, store.ErrNoGame) {
		// 그런 판이 없다. 다시 집어도 같은 답이므로 걷는다. 판이 지워지면 FK 가 이 행을
		// 같이 걷으므로(023 의 ON DELETE CASCADE) 남는 것은 집은 뒤에 지워진 자리뿐이다.
		a.dropQuiz(ctx, gameID)
		a.analysis.ObserveQuiz(metrics.AnalysisDropped, 0)
		log.Printf("quiz: game %d is gone — dropping it from the queue", gameID)
		return true
	}
	if err != nil {
		// 읽지 못한 것과 없는 것을 가른다. DB 가 흔들렸거나 프로세스가 멈추는 중이면 그
		// 판을 그대로 두고 리스가 낡기를 기다린다.
		if ctx.Err() == nil {
			log.Printf("quiz: could not read game %d: %v", gameID, err)
		}
		return true
	}

	// 이미 있으면 다시 만들지 않는다. 세우기가 시한에 걸린 뒤 커밋된 자리에서 그 판이
	// 줄에도 서고 대체 경로로도 만들어질 수 있다(queueQuiz 의 시한).
	if _, err := a.store.GameQuiz(ctx, gameID, quiz.Version); err == nil {
		a.dropQuiz(ctx, gameID)
		a.analysis.ObserveQuiz(metrics.AnalysisAlready, 0)
		return true
	}

	started := time.Now()
	if !generateQuiz(ctx, a.store, a.quiz, rec) {
		// 큐에 남겨 두고 횟수만 올린다. 상한을 넘으면 그때부터 집히지 않는다.
		a.failQuiz(ctx, gameID)
		a.analysis.ObserveQuiz(metrics.AnalysisFailed, time.Since(started))
		return true
	}
	a.analysis.ObserveQuiz(metrics.AnalysisDone, time.Since(started))
	a.dropQuiz(ctx, gameID)
	return true
}

// takeQuizSlot 은 문항 하나를 만들 자리를 잡는다. 없으면 ok=false 다.
//
// 기다리지 않는다. 기다리면 그 워커가 자리를 기다리는 동안 판도 手도 집지 않는다.
func (a *matchAnalyzer) takeQuizSlot() (func(), bool) {
	if a.quizSlots == nil {
		return func() {}, true
	}
	select {
	case a.quizSlots <- struct{}{}:
		return func() { <-a.quizSlots }, true
	default:
		return nil, false
	}
}

// failQuiz 는 만들어 봤는데 남기지 못했다고 적는다. dropQuiz 와 같은 이유로 취소를 벗긴다.
func (a *matchAnalyzer) failQuiz(parent context.Context, gameID int64) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), quizSaveTimeout)
	defer cancel()
	if err := a.store.FailQuizJob(ctx, gameID); err != nil {
		log.Printf("quiz: could not count the failed attempt on game %d: %v", gameID, err)
	}
}

// awaitQuizSlot 은 자리가 날 때까지 기다린다. 못 잡으면 ok=false 이고, 그 판은 문항 없이
// 남는다(걷힌 판과 같은 칸에 센다).
//
// 시한을 여기서 건다. 부르는 쪽 둘이 다 취소를 벗긴 ctx 를 주므로 기다리는 쪽은 아무것도
// 끊지 못하고, 그러면 기다리는 goroutine 이 계속 는다.
func (a *matchAnalyzer) awaitQuizSlot(parent context.Context) (func(), bool) {
	if a.quizSlots == nil {
		return func() {}, true
	}
	ctx, cancel := context.WithTimeout(parent, quizTimeout)
	defer cancel()
	select {
	case a.quizSlots <- struct{}{}:
		return func() { <-a.quizSlots }, true
	case <-ctx.Done():
		a.analysis.StarvedQuiz()
		log.Print("quiz: waited for a slot too long — that game has no quiz")
		return nil, false
	}
}

// dropQuiz 는 그 판을 문항 큐에서 걷는다.
//
// 취소를 벗긴다(generateQuiz 와 같은 이유). 배포 중에 이 DELETE 만 실패하면 그 행이
// 리스가 낡은 뒤 다시 집혀 이미 있는 문항을 다시 만든다.
func (a *matchAnalyzer) dropQuiz(parent context.Context, gameID int64) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), quizSaveTimeout)
	defer cancel()
	if err := a.store.DropQuizJob(ctx, gameID); err != nil {
		log.Printf("quiz: could not drop game %d from the queue: %v", gameID, err)
	}
}

// quizTimeout 은 문항을 만드는 데 주는 시한이다. 넘으면 만들던 것을 버린다. 반쪽 트리는
// 채점에 쓸 수 없다.
//
// 자르는 자리는 여기 하나다. 詰み 탐색 예산(quiz.MateSearchBudget)과 gap 쪽을 더해도 이
// 값이 마지막이 되도록 잡았다(journal §53).
const quizTimeout = 5 * time.Minute

// quizSaveTimeout 은 만든 것을 남기는 데 주는 시한이다. DB 쓰기 한 번이라 짧다.
const quizSaveTimeout = 10 * time.Second

// generateQuiz 는 끝난 판에서 문항을 만들어 저장한다. 남겼으면 참이다. 거짓은 「이번에는
// 남기지 못했다」이지 「문항이 없다」가 아니다.
//
// 부르는 자리가 셋이다. 큐를 집은 워커(runOneQuiz)와, 큐에 세우지 못한 두 자리
// (buildQuizNow · ws.go 의 sendSummary)다.
//
// ctx 의 취소를 벗긴다(context.WithoutCancel). 여기서 만들지 못하면 아무 데서도 만들 수
// 없다(journal §53).
//
// 엔진 풀을 오래 잡는다. mate 풀이 하나면 그동안 다른 대국의 詰み 게이지와 종반 판정이
// 막히고, 그래서 풀 크기가 손잡이다(cmd/api/main.go startMateEngines).
func generateQuiz(parent context.Context, st *store.Store, builder *quiz.Builder, rec store.GameRecord) bool {
	if st == nil {
		return false
	}

	// 생성기가 없어도 행은 남긴다. 남기지 않으면 화면이 「아직 만드는 중」에서 영영
	// 벗어나지 못한다 — 엔진 없는 배포에서 그 문장은 오지 않을 것을 기다리라는 거짓말이다.
	var q quiz.Quiz
	if builder != nil {
		ctx := usi.WithBorrower(context.WithoutCancel(parent), usi.BorrowerQuiz)
		ctx, cancel := context.WithTimeout(ctx, quizTimeout)
		built, measured := builder.Build(ctx, quizInput(rec))
		cut := ctx.Err() != nil
		cancel()

		// 보지 못한 채로 비었을 때만 적지 않는다. 나온 것이 있으면 남긴다. 다 지어진 詰み
		// 트리는 gap 후보 하나를 재지 못했다고 틀려지지 않는다.
		//
		// 시한만으로는 모자란다. 배포가 생성 도중에 끼면 풀이 먼저 닫혀(main 의 defer
		// 순서가 엔진 → DB다) 모든 탐색이 즉시 실패하는데, 그때 ctx 는 멀쩡하고 결과만
		// 비어 있다. 그래서 생성기가 「한 번이라도 답을 받았는가」를 따로 말한다.
		if (cut || !measured) && built.Empty() {
			log.Printf("quiz: game %d: nothing was measured (timed out: %v) — leaving no row rather than claiming there was nothing", rec.ID, cut)
			return false
		}
		q = built
	}

	// 문항이 없어도 저장한다. 그러지 않으면 「아직 만드는 중」과 「문항이 없는 판」이
	// 화면에서 같은 그림이 된다(quiz.go 의 Ready).
	payload, err := json.Marshal(q)
	if err != nil {
		log.Printf("quiz: game %d: encode: %v", rec.ID, err)
		return false
	}

	// 쓰는 데 시한을 따로 준다. 만드는 쪽이 시한에 걸렸으면 그 ctx 는 이미 죽어 있고,
	// 그대로 쓰면 만들어 놓고 남기지 못하는 자리가 되어 화면이 영영 기다린다.
	save, cancel := context.WithTimeout(context.WithoutCancel(parent), quizSaveTimeout)
	defer cancel()
	if err := st.SaveGameQuiz(save, rec.ID, quiz.Version, payload); err != nil {
		log.Printf("quiz: game %d: save: %v", rec.ID, err)
		return false
	}

	mate := 0
	if q.Mate != nil {
		mate = q.Mate.Plies
	}
	log.Printf("quiz: game %d: %d-ply mate item, %d best items", rec.ID, mate, len(q.Best))
	return true
}

// quizInput 은 기록을 문항 생성기의 입력으로 옮긴다. 여기서 옮겨야 internal/quiz 가
// store 를 모르고, 그래야 문항 기준이 기록의 모양에 매이지 않는다.
func quizInput(rec store.GameRecord) quiz.Input {
	in := quiz.Input{
		StartSFEN: startSFENOf(rec.StartSFEN),
		// 낙폭을 승률로 재므로 기준점이 필요하다 — 넘기지 않으면 駒落ち 판의 문항이
		// 手数 순으로 뽑힌다(quiz.Input.BaselineCp).
		BaselineCp:   handicap.BaselineCp(rec.StartSFEN),
		Human:        shogi.Black,
		Won:          rec.Result == store.ResultWin,
		OpeningPlies: openingPlies(rec),
	}
	if rec.MyColor == "w" {
		in.Human = shogi.White
	}
	// 구멍에서 끊는다. 기보에 빠진 手数가 있으면 그 뒤는 手数와 배열의 자리가 어긋나고,
	// 그대로 두면 문항이 한 번도 벌어지지 않은 국면을 가리킨다(review.go detailOf).
	for i, m := range rec.Moves {
		if m.Ply != i+1 {
			break
		}
		in.Moves = append(in.Moves, m.USI)
		in.Evals = append(in.Evals, m.Score)
	}
	return in
}

// openingPlies 는 컴퓨터가 고른 진형의 수순이 덮는 手数다. 「おまかせ」면 0이다.
//
// book.Opening.Moves 는 한쪽의 수만 주므로 手数로는 두 배다. 색을 보지 않는 것은 後手 몫이
// 같은 수순을 180° 돌린 것이라 개수가 같아서다. 이 값은 「여기까지는 아직 정석이다」의
// 바닥이라 한 手 남짓 어긋나도 상관없다.
func openingPlies(rec store.GameRecord) int {
	if rec.OpeningID == "" {
		return 0
	}
	o, ok := book.Find(rec.OpeningID)
	if !ok {
		return 0
	}
	return 2 * len(o.Moves(shogi.Black))
}
