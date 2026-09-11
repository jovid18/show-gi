package server

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/eval"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/match"
	"github.com/jovid18/show-gi/apps/server/internal/metrics"
	"github.com/jovid18/show-gi/apps/server/internal/quiz"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// matchAnalyzer 는 대인전의 평가치와 실력 추정치를 채운다(journal §83 · §95).
//
// 두 갈래로 일한다. 두는 동안 手마다 미리 재 두고(lookAhead), 판이 끝나면 그 결과로
// 평가치를 쓰고 실력을 커밋한다(analyze). 미리 재지 못한 手는 끝날 때 그 자리에서 잰다.
//
// 착수 경로는 엔진을 모른다. internal/match 가 usi 를 import 하지 않고, 미리 재는 것은
// 기록기를 지나 이 패키지에서 일어난다.
//
// 워커가 여럿이면 같은 판의 analyze 와 늦은 미리 재기가 겹친다. DB 에 쓰는 것은 analyze 의
// 순차 루프뿐이라 평가치는 틀어지지 않고(journal §106), 새는 자리는 remember 가 막는다.
//
// 큐 셋이 다 표다. 手(analysis_plies · journal §115) · 판(analysis_jobs · journal §118) ·
// 문항(quiz_jobs · journal §138)이고, 문항 쪽 자리는 quiz_jobs.go 다.
type matchAnalyzer struct {
	store      *store.Store
	newAnalyst func() game.Analyst

	// drain 은 표에 아직 적지 못한 手를 잠깐 담아 두는 자리다.
	//
	// 착수 경로가 DB 를 기다리지 않게 하는 장치가 이것 하나다. 세우는 자리가 테이블
	// goroutine 이라(match.Recorder) 거기서 INSERT 를 치면 착수가 그만큼 늦는다.
	drain chan plyJob

	// analysis 는 계측 창구다. 늘 non-nil 이다(metrics.Registry.Analysis).
	analysis *metrics.Analysis

	// 문항 큐를 읽지 못했다는 말을 자리마다 한 번씩만 하게 한다.
	//
	// 이유가 거의 언제나 하나다. 배포가 마이그레이션보다 먼저 나가는 창에서 표가 없고(023),
	// 집는 쪽은 手마다 게이지는 5초마다 실패하므로 매번 적으면 그 로그가 곧 요금이다
	// (archive.Searcher.timingLog 가 같은 판단이다).
	//
	// 하나로 묶지 않는다. 5초마다 도는 게이지가 언제나 먼저 태워서, 몇 시간 뒤 집는
	// 쪽에서 난 다른 실패가 영영 로그에 남지 않는다.
	quizClaimLog   sync.Once
	quizBacklogLog sync.Once

	// quizSlots 는 문항을 동시에 몇 개까지 만들 것인가다. 워커 수보다 하나 적다.
	//
	// 문항 하나가 워커를 최대 5분 잡으므로(quizTimeout), 세지 않으면 워커가 둘인 배포에서
	// 판 둘이 가까이 끝날 때 그 5분 동안 판도 手도 집히지 않는다. 그 사이 밀린 手가 100을
	// 넘으면 알람이 사람을 부르고 대를 붙인다(infra/alarms.tf).
	//
	// 워커가 없는 티어에도 있다. 거기서 도는 대체 경로도(buildQuizNow) 5분짜리 탐색이라,
	// 세지 않으면 끝나는 판마다 하나씩 떠서 풀을 다 가져간다.
	//
	// 워커가 하나여도 하나는 남긴다. 0으로 두면 문항이 아예 만들어지지 않는다.
	//
	// nil 은 구조체 리터럴로 만드는 테스트뿐이다.
	quizSlots chan struct{}

	// quiz 는 문항 큐를 집었을 때 쓴다(023). 세우는 쪽이 엔진 대국과 가져온 기보 둘이고,
	// 대인전은 아직 문항을 만들지 않는다.
	//
	// level 은 가져온 기보에만 쓴다. 대인전에는 개입이 없다.
	quiz  *quiz.Builder
	level intervene.Level

	// judgeDeadline 은 한 手를 재는 시한이다. 0이면 analysisJudgeDeadline 이고, 대국이
	// game.Config.MoveDeadline 을 두는 것과 같은 규약이다.
	judgeDeadline time.Duration

	// 메모리에 상태가 없다. 밀린 양도 「분석 중」 표시도 표에 적혀 있어서, 이 구조체를 가진
	// 프로세스가 몇이든 같은 것을 본다.
	//
	// store 가 nil 인 분석기는 테스트에만 있다(newMatchAnalyzer).
}

// plyJob 은 배수구에 잠깐 실리는 手 하나다.
//
// 수순을 복사해서 갖는다. 부르는 쪽의 슬라이스는 다음 手에 계속 자라므로 그대로 가리키면
// 적히는 시점에 무엇이 들어 있는지가 정해지지 않는다.
type plyJob struct {
	matchID string
	start   string
	moves   []string
	ply     int
}

// judged 는 미리 재 둔 手 하나의 결과다.
//
// game.Judgement 를 그대로 갖지 않는다. 그 안의 explain.Facts 가 태그 슬라이스를 갖는데
// 대인전에는 개입이 없어서 누구도 읽지 않고, 판이 끝날 때까지 살려 두면 방마다 手数만큼
// 쌓인다.
type judged struct {
	before eval.Score
	after  eval.Score
	move   skill.Move
	// category·best 는 가져온 판의 悪手 줄에만 읽힌다(interventions). 대인전의 手도 같은
	// 판정을 지나 값이 차지만, 개입이 없는 갈래라 그쪽은 이 칸을 보지 않는다.
	category string
	best     eval.Score
}

// errCannotReplay 는 엔진은 답했는데 판정이 국면을 되만들지 못한 자리다(Judgement.HasEvals).
// 오류로 바꾸는 것은 부르는 쪽 둘이 이 자리를 같게 다뤄야 해서다(judgeOne).
var errCannotReplay = errors.New("cannot replay the position")

// analysisSeat 는 끝난 판의 한 자리다. 대인전 한 판이 games 행 둘로 남고
// (012_match_games.sql) 자리마다 번호도 사람도 다르다.
//
// 색을 싣는 것은 실력 추정이 「이 手를 누가 뒀나」를 알아야 하기 때문이다. 번호 하나만
// 넘기면 한쪽 프로파일에 두 사람의 手가 다 쌓인다.
type analysisSeat struct {
	gameID int64
	userID int64
	color  shogi.Color
}

// jobLease 는 집어 간 판을 되찾기까지 기다리는 시간이다.
//
// 手 하나(plyLease)보다 훨씬 길다. 판 하나가 手数만큼의 판정이라 몇 분이 정상이고,
// 짧게 잡으면 아직 도는 판을 남이 다시 집어 같은 판을 두 번 잰다.
const jobLease = 30 * time.Minute

// drainBuffer 는 표에 아직 적지 못한 手를 몇 개까지 담아 둘 것인가다. 큐의 길이가 아니다.
// 밀린 양은 표에 적히므로 여기 쌓이는 것은 INSERT 한 번이 늦은 만큼뿐이다(journal §105).
//
// 넘치면 버린다. 버려도 잃는 것이 없다. 그 手는 판이 끝날 때 그 자리에서 잰다.
const drainBuffer = 256

// plyLease 는 집어 간 手를 되찾기까지 기다리는 시간이다.
//
// 한 手는 analysisJudgeDeadline 안에 끝나거나 끊긴다. 그보다 넉넉히 잡는다. 짧으면 아직
// 도는 手를 남이 다시 집어 같은 국면을 두 번 잰다.
const plyLease = 3 * time.Minute

// plyPollInterval 은 잴 手가 없을 때 다시 물어보기까지의 시간이다. 미리 재는 것을
// 기다리는 사람이 없고, 판이 끝나는 자리는 폴링 대신 큐가 깨운다(run).
const plyPollInterval = 2 * time.Second

// backlogSampleInterval 은 밀린 양을 재는 주기다. EMF 가 분당 한 줄이라
// (metrics.DefaultInterval) 그보다 촘촘하면 그 줄이 보는 값이 늘 최근 것이다.
const backlogSampleInterval = 5 * time.Second

// plyTTL 은 걷는 쪽이 돌지 않은 행을 얼마 뒤에 버릴 것인가다.
//
// 판이 비정상으로 끝나면 discard 가 돌지 않고, 그때 남는 행이 이 표의 하나뿐인 누수다.
// 한 판을 넉넉히 넘겨야 한다. 두는 중인 판의 행을 걷으면 그 手를 판이 끝날 때 다시 잰다.
const plyTTL = 6 * time.Hour

// sweepInterval 은 오래된 행을 걷는 주기다.
const sweepInterval = 30 * time.Minute

// analysisJudgeDeadline 은 한 手를 재는 데 줄 최대 시간이다. 대국의 판정과 같은 값을
// 쓴다(game.DefaultMoveDeadline).
//
// 없으면 한 手가 워커를 영영 붙잡는다. 판정이 매 手 詰み solver 를 부르는데
// (game.engineAnalyst.Judge) 그것이 go mate infinite 이라 취소로만 풀린다
// (usi.Engine.SearchMate). 대국 쪽은 세션이 시한을 걸어서 그 자리가 없다.
//
// 붙잡히는 것이 워커 하나로 끝나지 않는다. solver 풀이 둘뿐이라 한 국면이 그 절반을
// 차지한다(journal §95).
const analysisJudgeDeadline = game.DefaultMoveDeadline

// newMatchAnalyzer 는 워커를 띄운다. store 나 analyst 가 없으면 nil 을 준다. 엔진 없는
// 배포에서도 대인전이 도는 규약이고, 아래 메서드가 전부 nil 수신자를 받는다.
//
// workers 는 큐를 집는 goroutine 수다. 0이면 집는 쪽을 아예 띄우지 않는다. 상호작용
// 티어가 그 모양이고, 그 티어는 手를 세우기만 한다(SERVER_ROLE, cmd/api/main.go).
//
// 세우는 쪽과 게이지는 workers 와 무관하게 돈다. 분석 티어가 죽었을 때 지표가 「데이터
// 없음」 대신 부푸는 값을 내보내야 하기 때문이다.
//
// 청소는 집는 티어만 돈다. 걷는 기준이 나이 하나뿐이라(plyTTL) 집지 않는 티어가 걷으면
// 분석 티어가 죽어 있는 동안 쌓인 큐를 여섯 시간 뒤부터 지운다. 백로그가 0으로 내려가고
// 알람이 풀려서, 장애가 제일 클 때 그것이 보이지 않게 된다.
func newMatchAnalyzer(ctx context.Context, deps AnalysisDeps) *matchAnalyzer {
	if deps.Store == nil || deps.NewAnalyst == nil {
		return nil
	}
	workers := max(deps.Workers, 0)
	a := &matchAnalyzer{
		store:      deps.Store,
		newAnalyst: deps.NewAnalyst,
		drain:      make(chan plyJob, drainBuffer),
		analysis:   deps.Metrics.Analysis(),
		quiz:       deps.Quiz,
		level:      deps.Level,
	}
	a.quizSlots = make(chan struct{}, max(workers-1, 1))
	for range workers {
		go a.run(ctx)
	}
	go a.drainPlies(ctx)
	go a.watchBacklog(ctx)
	if workers > 0 {
		go a.sweepPlies(ctx)
	}
	return a
}

// drainPlies 는 배수구에서 꺼내 표에 적는다. 착수 경로와 DB 사이를 가르는 자리다.
//
// 하나만 돈다. 착수가 몰릴 때 커넥션을 몇 개까지 쓸지를 여기서 하나로 묶는다. 手끼리
// 순서가 없으므로 지킬 순서도 없다.
func (a *matchAnalyzer) drainPlies(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case p := <-a.drain:
			a.writePly(ctx, p)
		}
	}
}

// writePly 는 手 하나를 표에 세운다.
//
// 실패해도 판이 깨지지 않는다. 표가 없는 배포에서도 여기로 오는데(018), 그때는 미리 재기만
// 멈추고 그 手는 판이 끝날 때 재어진다.
func (a *matchAnalyzer) writePly(ctx context.Context, p plyJob) {
	err := a.store.EnqueueAnalysisPly(ctx, store.AnalysisPly{
		MatchID:   p.matchID,
		Ply:       p.ply,
		StartSFEN: p.start,
		Moves:     p.moves,
	})
	if err != nil && ctx.Err() == nil {
		log.Printf("match: could not queue ply %d of %s: %v", p.ply, p.matchID, err)
	}
}

// watchBacklog 은 밀린 양을 주기로 재어 지표에 놓는다. 늘고 주는 자리마다 세지 않는다.
// 카운터를 손으로 맞추면 차액이 남고, 그때 큐가 빈 채로 「밀려 있다」가 나온다.
func (a *matchAnalyzer) watchBacklog(ctx context.Context) {
	t := time.NewTicker(backlogSampleInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.sampleBacklog(ctx)
		}
	}
}

// sampleBacklog 은 지금 밀린 양을 지표에 놓는다.
//
// 세지 못하면 아무것도 놓지 않는다. 0으로 놓으면 「따라잡았다」로 읽혀 스케일 판단이
// 거꾸로 간다(journal §124). 질의가 계속 실패하면 대가 둘로 남고, 잃는 것이 요금이다.
//
// 두 큐를 각각 센다. 手 몫은 미리 재는 큐의 재지 않은 행이고 판 몫은 끝난 판의 아직 재지
// 않은 手数 합이다. 겹치지 않는 것은 enqueue 가 그 판의 手를 끊기 때문이다(journal §116).
func (a *matchAnalyzer) sampleBacklog(ctx context.Context) {
	if a == nil || a.store == nil {
		return
	}
	waiting, err := a.store.CountAnalysisBacklog(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("match: could not count the analysis backlog: %v", err)
		}
		return
	}
	games, queued, err := a.store.AnalysisJobBacklog(ctx, time.Now().Add(-jobLease))
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("match: could not count the queued games: %v", err)
		}
		return
	}
	a.analysis.SetBacklog(games, waiting+queued)

	// 문항은 따로 놓는다. 위 둘이 대수를 정하는 신호인데, 문항 하나가 5분을 잡는 것은
	// 대를 붙일 이유가 아니다.
	quizzes, err := a.store.QuizBacklog(ctx, time.Now().Add(-quizLease), quizAttempts)
	if err != nil {
		if ctx.Err() == nil {
			a.quizBacklogLog.Do(func() {
				log.Printf("match: could not read the quiz queue (logged once): %v", err)
			})
		}
		return
	}
	a.analysis.SetQuizBacklog(quizzes)
}

// sweepPlies 는 오래된 행을 걷는다. 집는 티어에서만 돈다(newMatchAnalyzer).
func (a *matchAnalyzer) sweepPlies(ctx context.Context) {
	t := time.NewTicker(sweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cutoff := time.Now().Add(-plyTTL)
			if err := a.store.SweepAnalysisPlies(ctx, cutoff); err != nil && ctx.Err() == nil {
				log.Printf("match: could not sweep old plies: %v", err)
			}
			// 자리가 영영 차지 않는 반쪽 판이 판 큐의 누수다.
			if err := a.store.SweepAnalysisJobs(ctx, cutoff); err != nil && ctx.Err() == nil {
				log.Printf("match: could not sweep old jobs: %v", err)
			}
			switch n, err := a.store.SweepQuizJobs(ctx, cutoff, time.Now().Add(-quizLease)); {
			case err != nil && ctx.Err() == nil:
				log.Printf("match: could not sweep old quiz jobs: %v", err)
			case n > 0:
				// 걷힌 판은 문항 없이 남는다. 나이만 보므로 「상한까지 실패했다」와
				// 「내내 밀려서 한 번도 집히지 않았다」가 같은 값이다.
				a.analysis.LostQuizzes(n)
				log.Printf("match: swept %d quiz jobs older than %s — those games have no quiz", n, plyTTL)
			}
		}
	}
}

// 종료할 때 큐를 비우지 않는다. 판이 표에 있으므로 프로세스가 사라져도 그 판은 없어지지
// 않고, 리스가 낡으면 다음 워커가 도로 집는다(journal §105 · §118).

func (a *matchAnalyzer) run(ctx context.Context) {
	// 아래 판정이 전부 이 컨텍스트를 지나므로 풀 대기가 borrower=analysis 로 갈린다.
	ctx = usi.WithBorrower(ctx, usi.BorrowerAnalysis)
	// 판정기를 한 벌만 쓴다. 상태가 없고 풀을 빌려 쓸 뿐이다.
	ahead := a.newAnalyst()
	for {
		if ctx.Err() != nil {
			return
		}
		// 집는 순서가 기다리는 사람 순이다(journal §138). 판을 재는 것은 되짚기의 그래프가
		// (analyzing), 문항은 그 화면의 한 자리가 기다리고, 미리 재는 것은 누구도 기다리지
		// 않는다. 집을 때만 정해지고 뺏지는 않는다.
		if a.runOneJob(ctx) || a.runOneQuiz(ctx) || a.measureOnePly(ctx, ahead) {
			continue
		}
		// 집을 것이 없다. 다음 폴링까지 잔다.
		select {
		case <-ctx.Done():
			return
		case <-time.After(plyPollInterval):
		}
	}
}

// runOneJob 은 끝난 판 하나를 집어 재고 큐에서 걷는다. 집을 것이 없으면 false 다.
func (a *matchAnalyzer) runOneJob(ctx context.Context) bool {
	if a.store == nil {
		return false
	}
	job, err := a.store.ClaimAnalysisJob(ctx, time.Now().Add(-jobLease))
	if errors.Is(err, store.ErrNoAnalysisJob) {
		return false
	}
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("match: could not claim a game to analyze: %v", err)
		}
		return false
	}

	seats := a.seatsOf(ctx, job.MatchID)
	if len(seats) == 0 {
		// 자리를 읽지 못했다. 다시 집어도 같은 답이므로 큐에서 걷고, 그 판은 평가치 없이
		// 남는다.
		a.dropJob(ctx, job.MatchID)
		a.discard(ctx, job.MatchID)
		a.analysis.ObserveGame(metrics.AnalysisDropped, 0)
		log.Printf("match: no seats for %s, leaving it without evals", job.MatchID)
		return true
	}

	started := time.Now()
	result := a.analyze(ctx, job.MatchID, seats)
	a.analysis.ObserveGame(result, time.Since(started))
	a.dropJob(ctx, job.MatchID)
	a.discard(ctx, job.MatchID)
	return true
}

// seatsOf 는 그 판의 자리들을 읽는다. games 행 둘이 곧 두 자리다(012_match_games.sql).
//
// 자리가 하나뿐이면 반쪽 판이라 아무것도 주지 않는다. 채운 평가치가 한 사람에게만 보이는
// 판을 만들지 않는다(matchRecords.collect 와 같은 판단).
//
// 가져온 기보는 자리가 하나다. 키가 그 갈래를 말한다(kifu_analysis.go).
func (a *matchAnalyzer) seatsOf(ctx context.Context, key string) []analysisSeat {
	if gameID, ok := importedGameID(key); ok {
		return a.importSeat(ctx, gameID)
	}
	rows, err := a.store.MatchSeats(ctx, key)
	if err != nil {
		log.Printf("match: could not read the seats of %s: %v", key, err)
		return nil
	}
	if len(rows) != 2 {
		return nil
	}
	out := make([]analysisSeat, 0, len(rows))
	for _, r := range rows {
		out = append(out, analysisSeat{gameID: r.GameID, userID: r.UserID, color: colorOf(r.Color)})
	}
	return out
}

// colorOf 는 기록에 적힌 한 글자를 색으로 되돌린다(match.ColorCode 의 반대 방향).
func colorOf(code string) shogi.Color {
	if code == match.ColorCode(shogi.White) {
		return shogi.White
	}
	return shogi.Black
}

// measureOnePly 는 큐에서 手 하나를 집어 잰다. 집을 것이 없으면 false 다. 여럿을 한 번에
// 잠그면 그중 하나가 시한을 다 쓰는 동안 나머지가 그 워커에 묶인다.
func (a *matchAnalyzer) measureOnePly(ctx context.Context, analyst game.Analyst) bool {
	p, err := a.store.ClaimAnalysisPly(ctx, time.Now().Add(-plyLease))
	if errors.Is(err, store.ErrNoAnalysisPly) {
		return false
	}
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("match: could not claim a ply to measure: %v", err)
		}
		return false
	}
	a.lookAhead(ctx, analyst, p)
	return true
}

// lookAhead 는 手 하나를 미리 재서 표에 남긴다. 실패는 경고 없이 끝낸다. 판이 끝날 때
// 같은 手를 다시 재고, 판정을 남기는 자리는 거기다.
//
// 그만둔 판인지 여기서 보지 않는다. 집는 질의가 이미 그 행을 주지 않는다
// (query/analysis.sql).
func (a *matchAnalyzer) lookAhead(ctx context.Context, analyst game.Analyst, p store.AnalysisPly) {
	got, err := a.judgeOne(ctx, analyst, p.StartSFEN, p.Moves, p.Ply)
	if err != nil {
		// 프로세스가 멈추는 중이면 그만두지 않는다. 그만두면 배포 한 번이 그때 두고 있던
		// 판들의 미리 재기 전체를 끈다(journal §115).
		//
		// 手 하나의 시한은 여기 걸리지 않는다. judge 가 자기 ctx 를 따로 두르므로 이 ctx 는
		// 멀쩡하고, 그때는 그만두는 것이 맞다.
		if ctx.Err() == nil {
			a.stopAhead(ctx, p.MatchID)
		}
		return
	}
	a.remember(ctx, p.MatchID, got)
}

// remember 는 잰 것을 그 手의 행에 적는다. 행을 만드는 것은 writePly 뿐이다.
//
// 행이 없으면 적지 않는다. 걷힌 뒤에 도착한 미리 재기가 행을 다시 만들면 판마다 하나씩
// 샌다(journal §106).
func (a *matchAnalyzer) remember(ctx context.Context, matchID string, got judged) {
	err := a.store.FinishAnalysisPly(ctx, matchID, store.MeasuredPly{
		Ply:       got.move.Ply,
		Before:    got.before,
		After:     got.after,
		Blunder:   got.move.Blunder,
		DeltaWin:  got.move.DeltaWin,
		Threshold: got.move.Threshold,
		Decided:   got.move.Decided,
		Category:  got.category,
		Best:      got.best,
	})
	if err != nil && ctx.Err() == nil {
		log.Printf("match: could not store ply %d of %s: %v", got.move.Ply, matchID, err)
	}
}

// prefetch 는 방금 둔 手를 배수구에 넣는다. 착수 경로에서 불리므로 즉시 돌아온다.
func (a *matchAnalyzer) prefetch(matchID, start string, moves []string, ply int) {
	if a == nil || start == "" || len(moves) == 0 {
		return
	}
	select {
	case a.drain <- plyJob{matchID: matchID, start: start, moves: moves, ply: ply}:
	default:
		// 배수구가 찼다. 이 手는 판이 끝날 때 잰다.
	}
}

// stopAhead 는 그 판을 미리 재는 것을 그만둔다. 이미 잰 값은 남는다.
//
// 부르는 자리가 둘이다. 한 手가 실패했거나(뒤의 手도 전부 같은 자리에서 실패한다), 판이
// 끝나 남은 手를 analyze 가 맡거나다.
func (a *matchAnalyzer) stopAhead(ctx context.Context, matchID string) {
	if a.store == nil {
		return
	}
	if err := a.store.StopAnalysisAhead(ctx, matchID); err != nil && ctx.Err() == nil {
		log.Printf("match: could not stop measuring %s: %v", matchID, err)
	}
}

// measuredOf 는 그 판에서 미리 재 둔 것을 手数로 찾을 수 있게 준다. 판 하나에 한 번
// 읽는다. 手마다 물으면 판이 끝나는 자리에서 手数만큼 왕복한다.
func (a *matchAnalyzer) measuredOf(ctx context.Context, matchID string) map[int]judged {
	rows, err := a.store.MeasuredAnalysisPlies(ctx, matchID)
	if err != nil {
		// 그 판은 전부 이 자리에서 다시 재어진다.
		log.Printf("match: could not read what was measured ahead for %s: %v", matchID, err)
		return nil
	}
	out := make(map[int]judged, len(rows))
	for _, r := range rows {
		out[r.Ply] = judged{
			before:   r.Before,
			after:    r.After,
			category: r.Category,
			best:     r.Best,
			move: skill.Move{
				Blunder:   r.Blunder,
				DeltaWin:  r.DeltaWin,
				Threshold: r.Threshold,
				Ply:       r.Ply,
				Decided:   r.Decided,
			},
		}
	}
	return out
}

// aheadCount 는 그 판에서 미리 재 둔 手数다. 판이 끝날 때 남은 일의 크기가 그것으로 정해진다.
func (a *matchAnalyzer) aheadCount(ctx context.Context, matchID string) int {
	if a.store == nil {
		return 0
	}
	n, err := a.store.CountMeasuredAnalysisPlies(ctx, matchID)
	if err != nil {
		// 세지 못하면 0으로 둔다. 작게 잡히면 포화를 알아보지 못한다.
		log.Printf("match: could not count what was measured ahead for %s: %v", matchID, err)
		return 0
	}
	return n
}

// discard 는 그 판의 행을 걷는다. 판이 끝난 뒤와, 반쪽이라 분석하지 않는 자리에서 부른다.
// 걷히지 않은 행은 sweepPlies 가 맡는다.
func (a *matchAnalyzer) discard(ctx context.Context, matchID string) {
	if a == nil || a.store == nil {
		return
	}
	if err := a.store.DiscardAnalysisMatch(ctx, matchID); err != nil && ctx.Err() == nil {
		log.Printf("match: could not discard the plies of %s: %v", matchID, err)
	}
}

// hold 는 그 판을 큐에 세우되 아직 집히지 않게 둔다. 그때부터 「분석 중」이다.
//
// 자리가 다 차기 전에 서야 한다. 두 행의 번호는 따로 정해지는데(matchRecords.collect)
// 화면은 자기 번호 하나로 되짚기를 열 수 있고, 그 사이에 열리면 「분석 중」이 아직 false 라
// 그래프가 「남지 않았다」에 굳고 폴링도 시작하지 않는다.
func (a *matchAnalyzer) hold(ctx context.Context, matchID string) {
	if a == nil || a.store == nil || matchID == "" {
		return
	}
	if err := a.store.HoldAnalysisJob(ctx, matchID); err != nil && ctx.Err() == nil {
		log.Printf("match: could not hold %s: %v", matchID, err)
	}
}

// enqueue 는 자리가 다 찬 판을 집히게 한다. hold 로 이미 서 있는 행을 채운다.
//
// plies 는 그 판의 手数다. 0이어도 큐에는 선다. 밀린 양만 그만큼 적게 잡힌다.
func (a *matchAnalyzer) enqueue(ctx context.Context, matchID string, plies int) {
	if a == nil || a.store == nil || matchID == "" {
		return
	}
	// 미리 재는 것을 여기서 끝낸다. 남은 手는 analyze 가 그 자리에서 재므로 표가 더 내줄
	// 것이 없고, 끊지 않으면 그 手가 두 큐에 같이 남아 두 번 세어진다(journal §116).
	a.stopAhead(ctx, matchID)

	// 미리 재 둔 만큼을 뺀다. 위에서 끊어도 이 값은 변하지 않는다. 세는 것이 이미 잰 행이다.
	pending := max(plies-a.aheadCount(ctx, matchID), 0)

	if err := a.store.ReadyAnalysisJob(ctx, matchID, pending); err != nil {
		// 큐에 세우지 못했다. 표시를 걷지 않으면 화면이 영영 「분석 중」이다.
		a.dropJob(ctx, matchID)
		a.discard(ctx, matchID)
		a.analysis.ObserveGame(metrics.AnalysisDropped, 0)
		log.Printf("match: could not queue %s, leaving it without evals: %v", matchID, err)
	}
}

// dropJob 은 그 판을 큐에서 걷는다. 다 재고 나서와, 반쪽이라 분석하지 않는 자리에서
// 부른다. 「분석 중」이 여기서 꺼진다.
func (a *matchAnalyzer) dropJob(ctx context.Context, matchID string) {
	if a == nil || a.store == nil || matchID == "" {
		return
	}
	if err := a.store.DropAnalysisJob(ctx, matchID); err != nil && ctx.Err() == nil {
		log.Printf("match: could not drop %s from the queue: %v", matchID, err)
	}
}

// analyzing 은 그 판이 아직 큐에 있거나 도는 중인가다. 되짚기가 이 값으로 「분석 중」과
// 「남지 않았다」를 가른다.
//
// 읽지 못하면 false 다. true 로 두면 화면이 오지 않을 값을 기다리며 폴링을 멈추지 않는다.
func (a *matchAnalyzer) analyzing(ctx context.Context, gameID int64) bool {
	if a == nil || a.store == nil {
		return false
	}
	ok, err := a.store.IsGameAnalyzing(ctx, gameID, importKey(gameID))
	if err != nil {
		log.Printf("match: could not tell whether game %d is being analyzed: %v", gameID, err)
		return false
	}
	return ok
}

// gameIDsOf 는 자리 목록에서 판 번호만 뽑는다. 평가치를 쓰는 쪽(setEval)이 번호만 본다.
func gameIDsOf(seats []analysisSeat) []int64 {
	ids := make([]int64, 0, len(seats))
	for _, s := range seats {
		ids = append(ids, s.gameID)
	}
	return ids
}

// kifuOf 는 두 행 중 쓸 한 행을 고르고, 그 수순이 판 끝까지인지(whole) 함께 준다.
// 주는 수순은 1手부터 빈틈없이 이어진다. 한 행이면 되는 것은 두 행에 같은 수가
// 들어가기 때문이다(journal §95).
//
// 두 행이 같은 자리에서 같이 잘리면 잡아내지 못한다. 기록기는 행마다 따로지만 둘이 같은
// DB 를 기다리므로, 그 사고가 한 행에만 오리라고 가정할 수 없다.
func (a *matchAnalyzer) kifuOf(
	ctx context.Context, seats []analysisSeat,
) (rec store.GameRecord, moves []string, whole, ok bool) {
	maxPly, told := 0, 0
	for _, seat := range seats {
		got, err := a.readRecord(ctx, seat.gameID)
		if err != nil {
			log.Printf("match: cannot read game %d to analyze: %v", seat.gameID, err)
			continue
		}
		if n := len(got.Moves); n > 0 {
			// 잘린 행도 contiguousMoves 를 지나므로, 이 값과 견주지 않으면 뒤가 잘린 판이
			// 짧게 끝난 판과 구별되지 않는다.
			told++
			maxPly = max(maxPly, got.Moves[n-1].Ply)
		}
		usi, contiguous := contiguousMoves(got)
		if !contiguous {
			// 한 칸이 비면 그 뒤가 전부 밀려 手番이 경고 없이 뒤집힌다.
			log.Printf("match: game %d has a gap in its moves", seat.gameID)
			continue
		}
		if len(usi) > len(moves) {
			rec, moves = got, usi
		}
	}
	// 한쪽이 빠지면 maxPly 가 남은 행의 것뿐이라 뒤 비교가 언제나 참이 되고, 뒤가 잘린
	// 판이 온전한 판으로 들어간다.
	whole = told == len(seats) && len(moves) >= maxPly
	ok = len(moves) > 0
	return rec, moves, whole, ok
}

// readRecord 는 한 행을 읽는다. 한 번 다시 해 본다. 잠깐 어긋난 것과 정말 없는 것을
// 여기서 갈라야 한다.
//
// 읽지 못한 행 하나가 두 사람의 실력을 다 버린다(whole). 그 판은 이미 탐색을 수백 번 쓴
// 뒤라, 풀이 한 번 딸꾹한 값으로 그걸 버리는 것이 아깝다.
func (a *matchAnalyzer) readRecord(ctx context.Context, gameID int64) (store.GameRecord, error) {
	got, err := a.store.GameRecordAnyOwner(ctx, gameID)
	if err == nil || errors.Is(err, store.ErrNoGame) || ctx.Err() != nil {
		return got, err
	}
	return a.store.GameRecordAnyOwner(ctx, gameID)
}

// contiguousMoves 는 手数가 1부터 빈틈없이 이어질 때만 수순을 준다.
//
// 뒤가 잘린 것은 여기서 잡아내지 못한다. 그것도 빈틈없이 이어진다(kifuOf).
func contiguousMoves(rec store.GameRecord) ([]string, bool) {
	moves := make([]string, 0, len(rec.Moves))
	for i, m := range rec.Moves {
		if m.Ply != i+1 {
			return nil, false
		}
		moves = append(moves, m.USI)
	}
	return moves, true
}

// analyze 는 한 판을 처음부터 다시 재서 eval_cp 와 두 사람의 실력 추정치를 채우고, 결과
// 이름을 준다. 이름은 metrics.Analysis 의 어휘이고, 중간에 끊긴 판에는 done 을 주지 않는다.
//
// 한 번 재서 두 행에 쓴다. eval_cp 는 先手 관점이고 뒤집는 것은 되짚기다(review.go).
//
// 개입은 대인전에 없다. 그래도 판정을 버리지 않는 것은 skill.Move 가 먹는 값이 전부
// 여기서 이미 나오기 때문이고, 그래서 추가 탐색이 0이다(journal §95).
//
// Before 로 직전 칸을 덮는 것은 일부러다(kifu/import.go 와 같은 모양). 같은 칸에 두
// 탐색이 쓰고, 그래야 되짚기가 읽는 값이 엔진 대국의 것과 같은 규약이 된다(journal §41).
func (a *matchAnalyzer) analyze(ctx context.Context, key string, seats []analysisSeat) string {
	ids := gameIDsOf(seats)
	// 가져온 판은 판정 결과가 悪手 줄로 남는다. 대인전은 개입이 없어 그 자리가 비어 있다.
	_, imported := importedGameID(key)
	// 평가치는 두 행에 다 쓰지만(ids) 기보는 한 행에서 읽는다. 아래 로그가 rec.ID 를 쓴다.
	// 폴백이 걸린 판에서 ids[0] 을 적으면 읽지 않은 행을 가리킨다.
	rec, moves, whole, ok := a.kifuOf(ctx, seats)
	if !ok {
		return metrics.AnalysisFailed
	}

	// 1手目를 둔 색. 手数 홀짝으로 가르지 않는다. 駒落ち는 上手가 먼저 두므로 그 규약이
	// 뒤집힌다(journal §88).
	//
	// 판정에도 같은 문자열을 넘긴다. 여기서만 平手를 메우면 빈 칸을 가진 행에서
	// 「先手가 1手目」로 정해 놓고 엔진에는 빈 국면을 보내게 된다.
	start := startSFENOf(rec.StartSFEN)
	if !whole {
		// 잰 창이 그 판의 전부라고 말할 수 없다. 고른 행의 뒤가 잘렸거나 견줄 행을
		// 읽지 못했거나이고(kifuOf), 어느 쪽이든 평가치만 채우고 실력은 쌓지 않는다.
		log.Printf("match: game %d is not known to be whole, skill will not be updated", rec.ID)
	}
	first, firstKnown := shogi.Black, false
	if pos, err := shogi.ParseSFEN(start); err == nil {
		first, firstKnown = pos.Turn, true
	} else {
		// 그 판은 실력 추정에서 빠진다. 평가치도 대개 같이 빠진다. 판정 안의 replay 가
		// 같은 문자열에서 같이 실패해 HasEvals 가 false 다(game.Judge).
		log.Printf("match: game %d start sfen %q: %v", rec.ID, rec.StartSFEN, err)
	}

	analyst := a.newAnalyst()
	measured := a.measuredOf(ctx, key)
	byColor := map[shogi.Color][]skill.Move{}
	stopped := false
	for ply := 1; ply <= len(moves); ply++ {
		got, ok := measured[ply]
		var err error
		if !ok {
			got, err = a.judgeOne(ctx, analyst, start, moves[:ply], ply)
		}
		// 끊기는 이유가 둘이고 성질이 같다. 엔진이 답하지 못했거나 판정이 국면을 되만들지
		// 못했거나(HasEvals), 어느 쪽이든 뒤의 手도 전부 같은 자리에서 실패한다. 매번 같은
		// 수순을 한 手 늘려 다시 두기 때문이다.
		//
		// 되만들지 못한 판정은 부호를 정하지 못했다는 뜻이라, 駒落ち의 기준점이 0으로 남고
		// 낙폭까지 틀어진다(intervene.Input.BaselineCp).
		if err != nil {
			log.Printf("match: analysis of game %d stopped at ply %d: %v", rec.ID, ply, err)
			// 창을 다 지난 뒤에 끊겼으면 그 표본은 온전하다. 마지막 手에서 끊긴 것도
			// 온전하다. 잃은 것이 그 한 手라 한 手 짧게 끝난 판과 같은 표본이다.
			//
			// 그 밖이면 남는 것이 더 긴 판의 앞부분뿐이고 그 구간이 체계적으로 쉬워서
			// 낙폭이 낮게 나오므로, 전부 버린다(journal §95).
			if ply <= skill.AnchorToPly && ply < len(moves) {
				return metrics.AnalysisFailed
			}
			stopped = true
			break
		}
		if firstKnown {
			c := moverAt(first, ply)
			if whole {
				if m := got.move; m.InAnchorWindow() {
					byColor[c] = append(byColor[c], m)
				}
			}
			// 가져온 판에서만, 그 사람이 둔 悪手를 줄로 남긴다. 상대의 悪手까지 남기면
			// 마이페이지의 「崩れやすいところ」가 두 사람 몫을 한 사람 것으로 센다.
			if imported && got.move.Blunder && c == seats[0].color {
				a.recordBlunder(ctx, seats[0].gameID, ply, c, got)
			}
		}
		a.setEval(ctx, ids, ply, got.after)
		if ply > 1 {
			a.setEval(ctx, ids, ply-1, got.before)
		}
	}
	a.updateSkill(ctx, seats, byColor)
	if stopped {
		return metrics.AnalysisFailed
	}
	// 문항은 다 잰 판에서만 세운다. 중간에 끊긴 판은 뒤쪽 평가치가 비어 있어서, 문항이
	// 「아직 재지 않은 자리」를 가리키게 된다.
	//
	// 여기서 만들지 않고 큐에 세운다. 이 잡이 걷혀야 「분석 중」이 꺼지는데 문항 쪽 예산이
	// 5분이라(quizTimeout), 한 잡에 두면 그래프가 다 찬 뒤에도 그만큼 폴링이 이어진다
	// (journal §138).
	if imported && !a.queueQuiz(ctx, seats[0].gameID) {
		// 워커를 잡지 않는다. 여기서 만들면 최대 5분 동안 이 워커가 판도 手도 집지 않는다
		// (quizSlots). 엔진 대국이 같은 자리에서 하는 것과 같은 모양이다(ws.go).
		go a.buildQuizNow(context.WithoutCancel(ctx), seats[0].gameID)
	}
	return metrics.AnalysisDone
}

// judge 는 한 手를 시한 안에서 잰다. 시한을 넘기면 그 자리에서 끊긴 것으로 친다. 뒤의 手도
// 같은 국면을 지나야 하므로 다음도 넘길 공산이 크다(analysisJudgeDeadline).
func (a *matchAnalyzer) judge(
	ctx context.Context, analyst game.Analyst, start string, moves []string, ply int,
) (game.Judgement, error) {
	ctx, cancel := context.WithTimeout(ctx, a.deadlineOf())
	defer cancel()
	return analyst.Judge(ctx, start, moves, ply)
}

// judgeOne 은 手 하나를 재서 필요한 칸만 남긴다.
//
// HasEvals 가 false 면 오류로 바꾼다. 부르는 쪽 둘이 그 자리를 같게 다뤄야 해서다. 미리
// 재는 쪽은 그 판을 그만두고, 판이 끝날 때는 거기서 멈춘다.
func (a *matchAnalyzer) judgeOne(
	ctx context.Context, analyst game.Analyst, start string, moves []string, ply int,
) (judged, error) {
	j, err := a.judge(ctx, analyst, start, moves, ply)
	if err != nil {
		return judged{}, err
	}
	if !j.HasEvals {
		return judged{}, errCannotReplay
	}
	return judged{
		before:   j.SenteBefore,
		after:    j.SenteAfter,
		move:     skillMoveOf(j, ply),
		category: string(j.Verdict.Category),
		best:     j.Verdict.Best,
	}, nil
}

// deadlineOf 는 지금 걸 시한이다. 정해 두지 않았으면 기본값이다.
func (a *matchAnalyzer) deadlineOf() time.Duration {
	if a.judgeDeadline > 0 {
		return a.judgeDeadline
	}
	return analysisJudgeDeadline
}

// skillMoveOf 는 판정 하나에서 추정에 쓰는 값만 뽑는다. 手数는 판정이 실어 준 것 대신
// 부르는 쪽이 센 값을 쓴다. 여기서는 그게 Judge 에 넘긴 바로 그 값이다.
func skillMoveOf(j game.Judgement, ply int) skill.Move {
	return skill.Move{
		Blunder:   j.Verdict.Kind == intervene.KindBlunder,
		DeltaWin:  j.Verdict.DeltaWin,
		Threshold: j.Threshold,
		Ply:       ply,
		Decided:   j.Decided(),
	}
}

// moverAt 는 그 手数를 둔 색이다. first 는 1手目를 둔 쪽이다.
func moverAt(first shogi.Color, ply int) shogi.Color {
	if ply%2 == 1 {
		return first
	}
	return first.Other()
}

// updateSkill 은 한 판에서 주운 手를 두 사람의 프로파일에 쌓는다.
//
// 판을 다 잰 뒤에 한 번에 읽고 쓴다. 지난 값 위에 얹는 읽기-쓰기라, 재는 동안(탐색 수백
// 번) 열어 두면 그 사이의 쓰기 전체가 덮인다.
//
// 그래서 읽은 값이 그대로일 때만 쓴다(SaveSkillEstimateIfSamples). 경합에 지면 다시 읽어
// 얹는다. 手는 이미 손에 있으므로 엔진을 다시 부르지 않는다.
//
// 마지막 쓰기는 취소를 뗀다. 판을 다 잰 뒤에 오는 자리라, 여기서 끊기면 재느라 쓴 탐색
// 전체가 버려지고 그 판은 다시 재지지 않는다(dbRecorder.run 이 같은 이유로 세션 ctx 를
// 쓰지 않는다). 종료는 이겨내지 못한다. 풀이 먼저 닫히면 그대로 실패한다.
func (a *matchAnalyzer) updateSkill(ctx context.Context, seats []analysisSeat, byColor map[shogi.Color][]skill.Move) {
	ctx = context.WithoutCancel(ctx)
	for _, seat := range seats {
		got := byColor[seat.color]
		if len(got) == 0 {
			// 창(21~60手) 안에 이 사람의 手가 없다. 46手에 끝난 판이 실제로 그랬다
			// (journal §94).
			continue
		}
		// 한쪽만 쌓일 수 있다. 프로파일은 사람마다 따로이고 둘을 견주는 자리가 없다.
		if err := a.saveMoves(ctx, seat.userID, got); err != nil {
			// 이 판이 추정에 들어가지 않은 것으로 끝난다. 다음 판이 갱신 전 값 위에서 돈다.
			log.Printf("match: save skill %d: %v", seat.userID, err)
		}
	}
}

// skillWriteTimeout 은 한 사람의 얹기가 붙잡아 둘 수 있는 최대 시간이다. 취소를
// 떼어냈으므로(updateSkill) 상한이 여기 하나뿐이다.
//
// 사람마다 따로 센다. 한 예산을 둘이 나눠 쓰면 DB가 굼뜬 날 앞사람이 다 쓰고 뒷사람이
// 경고 없이 빠진다.
const skillWriteTimeout = 10 * time.Second

// skillCASTries 는 얹기를 몇 번까지 다시 해 볼 것인가다.
//
// 상대가 대국 중인 세션이면 판정마다 쓰므로 몇 번을 해도 진다. 그때는 포기한다. 그 세션이
// 자기 트랙을 갖고 있어서, 여기가 이겨도 다음 판정이 도로 덮는다.
const skillCASTries = 3

// saveMoves 는 그 사람의 프로파일에 手들을 얹는다. 읽기-쓰기가 어긋나면 다시 읽는다.
func (a *matchAnalyzer) saveMoves(ctx context.Context, userID int64, moves []skill.Move) error {
	// 넣을 手가 없으면 아무것도 쓰지 않는다. 빈 채로 지나가면 아래가 제로값을 저장해서
	// 「매 수 최선」이 되고, 그건 척도의 가장 센 이름이다(skill.RankOf).
	if len(moves) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, skillWriteTimeout)
	defer cancel()

	for range skillCASTries {
		// 「없음」을 따로 가르지 않는다. 표본 0인 추정치를 넘기면 추정기가 기준선에서
		// 시작한다(skill.NewTrackFrom).
		prior, _, err := a.store.SkillProfile(ctx, userID)
		if err != nil {
			return err
		}
		t := skill.NewTrackFrom(skillEstimateOf(prior))
		var e skill.Estimate
		for _, m := range moves {
			e = t.Observe(m)
		}
		saved, err := a.store.SaveSkillEstimateIfSamples(ctx, userID, storeSkillEstimate(e), prior.Samples)
		if err != nil {
			return err
		}
		if saved {
			return nil
		}
	}
	log.Printf("match: skill of %d changed under the analysis, dropping this game", userID)
	return nil
}

func (a *matchAnalyzer) setEval(ctx context.Context, ids []int64, ply int, sente eval.Score) {
	for _, id := range ids {
		if err := a.store.SetMoveEval(ctx, id, ply, sente); err != nil {
			log.Printf("match: set eval of game %d ply %d: %v", id, ply, err)
		}
	}
}
