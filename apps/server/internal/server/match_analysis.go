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
// 미리 재는 쪽이 없으면 판이 끝나는 순간 手数가 한꺼번에 몰린다. 그 봉우리와, 대인전이
// 대국 중에 엔진을 쓰기 시작한다는 것의 값은 journal §105.
//
// 착수 경로는 여전히 엔진을 모른다. internal/match 가 usi 를 import 하지 않는 규약은
// 그대로이고, 미리 재는 것은 기록기를 지나 이 패키지에서 일어난다.
//
// 워커 수는 손잡이다. 기본값이 풀 크기와 같다 — 풀이 사람이 기다리는 쪽에 먼저
// 빌려주므로 다 가져가도 착수가 밀리지 않는다(usi.priorityOf · cmd/api).
//
// 워커가 여럿이면 같은 판의 analyze 와 늦은 미리 재기가 겹친다. 평가치는 틀어지지 않는다 —
// DB 에 쓰는 것은 analyze 의 순차 루프뿐이다(journal §106). 새는 자리는 remember 가 막는다.
//
// 미리 재는 큐는 표다(analysis_plies · 018). 그래서 배포가 끼어도 그 手는 없어지지 않고,
// 리스가 낡으면 다음 워커가 도로 집는다 — 프로세스 밖에 있는 것이 소비자를 여럿으로
// 늘릴 수 있게 하는 자리이기도 하다(journal §115).
//
// 판 단위 큐도 표다(analysis_jobs · 019). 자리는 옮겨 적지 않는다 — games 행 둘이 곧 두
// 자리라(012_match_games.sql) 여기 실으면 같은 사실이 두 벌이 된다(journal §118).
//
// 문항 큐는 셋째 표다(quiz_jobs · 023). 같은 워커가 집지만 예산이 달라서 갈랐고,
// 그 자리는 quiz_jobs.go 다(journal §138).
type matchAnalyzer struct {
	store      *store.Store
	newAnalyst func() game.Analyst

	// drain 은 표에 아직 적지 못한 手를 잠깐 담아 두는 자리다. 이름대로 배수구다 — 밀린 양은
	// 표에 적혀 있고(analysis_plies) 여기 있는 것은 INSERT 를 기다리는 것뿐이다.
	//
	// 착수 경로가 DB 를 기다리지 않게 하는 장치가 이것 하나다. 세우는 자리가 테이블
	// goroutine 이라(match.Recorder) 거기서 INSERT 를 치면 착수가 그만큼 늦는다.
	drain chan plyJob

	// analysis 는 계측 창구다. 늘 non-nil 이다(metrics.Registry.Analysis).
	analysis *metrics.Analysis

	// 문항 큐를 읽지 못했다는 말을 자리마다 한 번씩만 하게 한다.
	//
	// 이유가 거의 언제나 하나다. 배포가 마이그레이션보다 먼저 나가는 창에서 표가 없고
	// (023), 그 창이 몇 시간 갈 수 있다 — 집는 쪽은 手마다, 게이지는 5초마다 실패하므로
	// 매번 적으면 그 로그가 곧 요금이다. 시간 기록이 같은 자리에서 같은 판단을 한다
	// (archive.Searcher.timingLog).
	//
	// 하나로 묶지 않는다. 5초마다 도는 게이지가 언제나 먼저 태워서, 몇 시간 뒤 집는
	// 쪽에서 난 다른 실패가 영영 로그에 남지 않는다.
	quizClaimLog   sync.Once
	quizBacklogLog sync.Once

	// quizSlots 는 문항을 동시에 몇 개까지 만들 것인가다. 워커 수보다 하나 적다.
	//
	// 문항 하나가 워커를 최대 5분 잡으므로(quizTimeout), 워커가 둘인 배포에서 판 둘이
	// 가까이 끝나면 그 5분 동안 판도 手도 한 건 집히지 않는다. 그 사이에 가져온 판 하나가
	// 서면 밀린 手가 곧바로 100을 넘고, 5분을 채우면 알람이 사람을 부르고 대를 붙인다
	// (infra/alarms.tf) — 실제로 밀린 것이 아니라 워커가 다른 일을 하고 있는 것이다.
	//
	// 워커가 없는 티어에도 있다. 거기서 도는 것은 큐에 세우지 못한 판의 대체 경로뿐인데
	// (buildQuizNow) 그것도 5분짜리 탐색이라, 세지 않으면 끝나는 판마다 하나씩 떠서 풀을
	// 다 가져간다.
	//
	// 워커가 하나면 남길 자리가 없다. 문항이 그 하나를 5분 잡을 수 있고, 0으로 두면 문항이
	// 아예 만들어지지 않는다 — 둘 중 앞엣것을 고른 것이다.
	//
	// nil 은 구조체 리터럴로 만드는 테스트뿐이다.
	quizSlots chan struct{}

	// quiz 는 문항 큐를 집었을 때 쓴다(023). 세우는 쪽이 둘이고 그 둘이 엔진 대국과
	// 가져온 기보다. 대인전은 아직 문항을 만들지 않는다.
	//
	// level 은 가져온 기보에만 쓴다. 대인전에는 개입이 없다.
	quiz  *quiz.Builder
	level intervene.Level

	// judgeDeadline 은 한 手를 재는 시한이다. 0이면 analysisJudgeDeadline —
	// 대국이 game.Config.MoveDeadline 을 두는 것과 같은 규약이다.
	judgeDeadline time.Duration

	// 메모리에 상태가 없다. 밀린 양도 「분석 중」 표시도 표에 적혀 있고, 그래서 이 구조체를
	// 가진 프로세스가 몇이든 같은 것을 본다 — 소비자를 티어로 가르는 자리가 그것이다.
	//
	// store 가 nil 인 분석기는 테스트에만 있다. 프로덕션에서는 생성자가 store 없이
	// 분석기를 만들지 않는다(newMatchAnalyzer).
}

// plyJob 은 배수구에 잠깐 실리는 手 하나다. 표에 적히고 나면 사라진다.
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
	// category·bestCp 는 가져온 판의 悪手 줄에만 읽힌다(interventions). 대인전의 手도
	// 같은 판정을 지나 값이 차지만 그쪽은 이 칸을 보지 않는다 — 개입이 없는 갈래다.
	//
	// 스칼라와 짧은 문자열이라 이 구조체를 가볍게 둔 이유(explain.Facts 의 태그 슬라이스)에
	// 걸리지 않는다.
	category string
	best     eval.Score
}

// errCannotReplay 는 엔진은 답했는데 판정이 국면을 되만들지 못한 자리다(Judgement.HasEvals).
//
// 오류로 바꾸는 것은 부르는 쪽이 둘을 같이 다루기 때문이다 — 어느 쪽이든 그 手부터
// 뒤가 전부 같은 자리에서 실패한다.
var errCannotReplay = errors.New("cannot replay the position")

// analysisSeat 는 끝난 판의 한 자리다. 대인전 한 판이 games 행 둘로 남고
// (012_match_games.sql) 자리마다 번호도 사람도 다르다.
//
// 색을 싣는 이유는 실력 추정이 「이 手를 누가 뒀나」를 알아야 하기 때문이다. 번호
// 하나만 넘기면 한쪽 프로파일에 두 사람의 手가 다 쌓인다.
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

// drainBuffer 는 표에 아직 적지 못한 手를 몇 개까지 담아 둘 것인가다.
//
// 큐의 길이와 다르다. 밀린 양은 표에 적히므로 여기 쌓이는 것은 INSERT 한 번이 늦은 만큼
// 뿐이고, 그래서 「밀렸다」를 알아차리기 전에 메모리가 자라던 자리가 없어졌다(journal §105).
//
// 넘치면 버린다. 버려도 잃는 것이 없다 — 그 手는 판이 끝날 때 그 자리에서 잰다.
const drainBuffer = 256

// plyLease 는 집어 간 手를 되찾기까지 기다리는 시간이다.
//
// 한 手는 analysisJudgeDeadline 안에 끝나거나 끊긴다. 그보다 넉넉히 잡는 이유는 이 값이
// 넘길 때 잃는 것과 늦을 때 잃는 것이 다르기 때문이다 — 짧으면 아직 도는 手를 남이
// 다시 집어 같은 국면을 두 번 재고, 길면 워커가 사라진 手가 그만큼 늦게 재어진다.
const plyLease = 3 * time.Minute

// plyPollInterval 은 잴 手가 없을 때 다시 물어보기까지의 시간이다.
//
// 짧을 이유가 없다. 미리 재는 것을 기다리는 사람이 없고, 판이 끝나는 자리는 폴링 대신
// 큐가 깨운다(run).
const plyPollInterval = 2 * time.Second

// backlogSampleInterval 은 밀린 양을 재는 주기다. EMF 가 분당 한 줄이라(metrics.DefaultInterval)
// 그보다 촘촘하면 그 줄이 보는 값이 늘 최근 것이다.
const backlogSampleInterval = 5 * time.Second

// plyTTL 은 걷는 쪽이 돌지 않은 행을 얼마 뒤에 버릴 것인가다.
//
// 판이 비정상으로 끝나면 discard 가 돌지 않고, 그때 남는 행이 이 표의 하나뿐인 누수다.
// 한 판을 넉넉히 넘기는 값이어야 한다 — 두는 중인 판의 행을 걷으면 그 手를 판이
// 끝날 때 다시 잰다.
const plyTTL = 6 * time.Hour

// sweepInterval 은 오래된 행을 걷는 주기다.
const sweepInterval = 30 * time.Minute

// analysisJudgeDeadline 은 한 手를 재는 데 줄 최대 시간이다. 대국의 판정과 같은 값을
// 쓴다(game.DefaultMoveDeadline).
//
// 없으면 한 手가 워커를 영영 붙잡는다. 판정이 매 手 詰み solver 를 부르는데
// (game.engineAnalyst.Judge) 그것이 go mate infinite 이라 스스로 끝나지 않고, 취소로만
// 풀린다(usi.Engine.SearchMate). 대국 쪽은 세션이 시한을 걸어서 그 자리가 없다.
//
// 붙잡히는 것이 워커 하나로 끝나지 않는다. solver 풀이 둘뿐이라 한 국면이 그 절반을
// 차지한다(journal §95).
const analysisJudgeDeadline = game.DefaultMoveDeadline

// newMatchAnalyzer 는 워커를 띄운다. store 나 analyst 가 없으면 nil 을 준다 — 엔진
// 없는 배포에서 대인전이 그대로 도는 규약을 여기서도 지킨다. nil 인 채로 불려도 되도록
// 아래 메서드가 전부 nil 수신자를 받는다.
//
// workers 는 큐를 집는 goroutine 수다. 0이면 집는 쪽을 아예 띄우지 않는다 — 상호작용
// 티어가 그 모양이고, 그 티어는 手를 세우기만 한다(SERVER_ROLE, cmd/api/main.go).
//
// 세우는 쪽과 게이지는 workers 와 무관하게 돈다. 게이지가 집지 않는 티어에서도 도는
// 것은 분석 티어가 죽었을 때 지표가 「데이터 없음」 대신 부푸는 값을 내보내야 하기
// 때문이다 — 그때가 밀린 양이 제일 큰 자리다.
//
// 청소는 집는 티어만 돈다. 걷는 기준이 나이 하나뿐이라(plyTTL) 집지 않는 티어가 걷으면
// 분석 티어가 죽어 있는 동안 쌓인 큐를 여섯 시간 뒤부터 지운다 — 백로그가 0으로
// 내려가고 알람이 풀리고 되짚기가 「남지 않았다」로 보인다. 장애가 제일 클 때 그것이
// 보이지 않게 되는 것이고, 티어를 가르기 전에는 걷는 쪽이 곧 집는 쪽이라 없던 자리다.
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
	// 문항이 워커를 다 가져가지 못하게 한다. 하나는 판과 手 쪽에 남는다.
	//
	// 집지 않는 티어에도 하나를 준다. 거기서도 대체 경로가 돌고(buildQuizNow) 그것이
	// 세어지지 않으면 끝나는 판마다 5분짜리 탐색이 하나씩 뜬다.
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
// 하나만 돈다. 착수가 몰릴 때 커넥션을 몇 개까지 쓸지를 여기서 하나로 묶는다 —
// 手끼리 순서가 없으므로 지킬 순서도 없다.
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

// watchBacklog 은 밀린 양을 주기로 재어 지표에 놓는다.
//
// 늘고 주는 자리마다 세지 않는다. 手가 표에 있으므로 세는 것이 질의 하나이고, 카운터를
// 손으로 맞추면 차액이 남을 수 있다 — 그때 지표는 「밀려 있다」인데 큐는 비어 있어서
// 밀린 것인지 세지 못한 것인지 가릴 수 없다.
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

// sampleBacklog 은 지금 밀린 양을 지표에 놓는다. 판도 手도 표에 적혀 있다.
//
// 세지 못하면 아무것도 놓지 않는다. 오래된 값이 남는 쪽이고, 0으로 놓으면 그때
// 「따라잡았다」로 읽혀 스케일 판단이 거꾸로 간다.
//
// 그 오래된 값이 이제 대수를 정한다(journal §124). 질의가 계속 실패하고 마지막 값이 1 이상이면
// 스케일 인이 걸리지 않아 대가 둘로 남는다 — 잃는 것이 요금이라 이쪽으로 기울여 둔다.
//
// 두 큐를 각각 센다. 手 몫은 미리 재는 큐의 재지 않은 행이고, 판 몫은 끝난 판이 아직 재지 않은
// 手数의 합이다 — 겹치지 않는 이유는 enqueue 가 그 판의 手를 끊기 때문이다(journal §116).
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

	// 문항은 따로 놓는다. 위 둘이 대수를 정하는 신호인데(journal §124) 문항 하나가
	// 5분을 잡는 것은 대를 붙일 이유가 아니다.
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
			// 여기서 걷힌 판은 문항 없이 남는다. 0이 아니면 그 자체로 사고이므로 적는다 —
			// 나이만 보므로 「계속 실패했다」와 「내내 밀려서 한 번도 집히지 않았다」가 같은 값이다.
			switch n, err := a.store.SweepQuizJobs(ctx, cutoff, time.Now().Add(-quizLease)); {
			case err != nil && ctx.Err() == nil:
				log.Printf("match: could not sweep old quiz jobs: %v", err)
			case n > 0:
				// 나이만 보므로 「상한까지 실패했다」와 「내내 밀려서 한 번도 집히지 않았다」가
				// 같은 값이다. 어느 쪽이든 그 판은 문항 없이 남는다.
				a.analysis.LostQuizzes(n)
				log.Printf("match: swept %d quiz jobs older than %s — those games have no quiz", n, plyTTL)
			}
		}
	}
}

// 종료할 때 큐를 비우지 않는다. 판이 표에 있으므로 프로세스가 사라져도 그 판은
// 없어지지 않고, 리스가 낡으면 다음 워커가 도로 집는다 — 메모리 채널이던 동안 재배포 한 번이
// 46판을 잃었던 자리다(journal §105 · §118).

func (a *matchAnalyzer) run(ctx context.Context) {
	// 빌리는 쪽의 이름을 여기서 한 번 붙인다. 아래 판정이 전부 이 컨텍스트를 지나므로
	// 풀 대기가 borrower=analysis 로 갈린다(usi.WithBorrower).
	ctx = usi.WithBorrower(ctx, usi.BorrowerAnalysis)
	// 미리 재는 쪽은 판정기를 한 벌만 쓴다. 手마다 새로 뜨면 방마다 하나씩 사는 것과
	// 같아지는데, 판정기는 상태가 없고 풀을 빌려 쓸 뿐이다.
	ahead := a.newAnalyst()
	for {
		if ctx.Err() != nil {
			return
		}
		// 집는 순서가 기다리는 사람 순이다. 판을 재는 것은 되짚기의 그래프가 기다리고
		// (analyzing), 문항은 그 화면의 한 자리가 기다리며, 미리 재는 것은 누구도
		// 기다리지 않는다.
		//
		// 집을 때만 정해지고 뺏지는 않는다. 문항 하나가 워커를 최대 5분 잡으므로
		// (quizTimeout) 워커가 둘인 배포에서는 판 둘이 가까이 끝나면 그동안 판을 재는
		// 쪽이 한 건도 집히지 않는다 — 재지 않은 자리다(journal §138).
		if a.runOneJob(ctx) || a.runOneQuiz(ctx) || a.measureOnePly(ctx, ahead) {
			continue
		}
		// 둘 다 없다. 다음 폴링까지 잔다.
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
		// 자리를 읽지 못했다. 그 판은 평가치 없이 남는다 — 다시 집어도 같은 자리에서
		// 같은 답이므로 큐에서 걷는다.
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
// 색은 코드 한 글자로 저장돼 있다(match.ColorCode). 여기서 되돌리는 것이 그 규약의
// 반대 방향이고, 자리가 하나뿐이면 반쪽 판이라 아무것도 주지 않는다 — 채운 평가치가 한
// 사람에게만 보이는 판을 만들지 않는다(matchRecords.collect 와 같은 판단).
//
// 가져온 기보는 자리가 하나다. 키가 그 갈래를 말하므로(kifu_analysis.go) 표에 갈래를
// 적는 칸을 만들지 않았다.
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

// measureOnePly 는 큐에서 手 하나를 집어 잰다. 집을 것이 없으면 false 다.
//
// 하나씩 집는다. 여럿을 한 번에 잠그면 그중 하나가 시한을 다 쓰는 동안 나머지가 그
// 워커에 묶이고, 그 사이 다른 워커는 빈손으로 폴링한다.
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

// lookAhead 는 手 하나를 미리 재서 표에 남긴다.
//
// 실패는 경고 없이 끝낸다 — 판이 끝날 때 같은 手를 다시 재고, 판정을 남기는 자리는 거기다.
//
// 그만둔 판인지 여기서 보지 않는다. 집는 질의가 이미 그 행을 주지 않는다(query/analysis.sql).
func (a *matchAnalyzer) lookAhead(ctx context.Context, analyst game.Analyst, p store.AnalysisPly) {
	got, err := a.judgeOne(ctx, analyst, p.StartSFEN, p.Moves, p.Ply)
	if err != nil {
		// 프로세스가 멈추는 중이면 그만두지 않는다. 그 실패는 우리 사정에서 오고,
		// 그만두면 배포 한 번이 그때 두고 있던 판들의 미리 재기 전체를 끈다 — 그 판들은
		// 手数만큼을 끝날 때 몰아서 재게 된다(journal §115).
		//
		// 手 하나의 시한은 여기 걸리지 않는다. 그쪽은 judge 가 자기 ctx 를 따로 두르므로
		// 이 ctx 는 멀쩡하고, 그때는 그만두는 것이 맞다.
		if ctx.Err() == nil {
			a.stopAhead(ctx, p.MatchID)
		}
		return
	}
	a.remember(ctx, p.MatchID, got)
}

// remember 는 잰 것을 그 手의 행에 적는다.
//
// 행이 없으면 적지 않는다. 워커가 둘 이상이면 같은 판의 analyze 와 늦은 미리 재기가 겹치는데,
// 걷힌 뒤에 도착한 쪽이 행을 다시 만들면 그 항목을 누구도 지우지 않는다 — 판마다 하나씩
// 샌다(journal §106). 행을 만드는 것은 세우는 쪽뿐이다(writePly).
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
// 부르는 자리가 둘이고 이유가 다르다. 한 手가 실패했거나(뒤의 手도 전부 같은 자리에서
// 실패하므로 그만두지 않으면 남은 手数만큼 탐색을 버린다), 판이 끝나 남은 手를 analyze 가
// 맡거나다(enqueue).
func (a *matchAnalyzer) stopAhead(ctx context.Context, matchID string) {
	if a.store == nil {
		return
	}
	if err := a.store.StopAnalysisAhead(ctx, matchID); err != nil && ctx.Err() == nil {
		log.Printf("match: could not stop measuring %s: %v", matchID, err)
	}
}

// measuredOf 는 그 판에서 미리 재 둔 것을 手数로 찾을 수 있게 준다.
//
// 판 하나에 한 번 읽는다. 手마다 물으면 판이 끝나는 자리에서 手数만큼 왕복하고, 그것
// 자체가 밀리는 값이다.
func (a *matchAnalyzer) measuredOf(ctx context.Context, matchID string) map[int]judged {
	rows, err := a.store.MeasuredAnalysisPlies(ctx, matchID)
	if err != nil {
		// 미리 잰 것을 읽지 못했다. 그 판은 전부 이 자리에서 다시 재어진다.
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

// aheadCount 는 그 판에서 미리 재 둔 手数다. 판이 끝날 때 남은 일의 크기를 그것으로 센다.
func (a *matchAnalyzer) aheadCount(ctx context.Context, matchID string) int {
	if a.store == nil {
		return 0
	}
	n, err := a.store.CountMeasuredAnalysisPlies(ctx, matchID)
	if err != nil {
		// 세지 못하면 0으로 둔다. 밀린 양이 실제보다 크게 잡히는 쪽이고, 작게 잡히면
		// 포화를 알아보지 못한다.
		log.Printf("match: could not count what was measured ahead for %s: %v", matchID, err)
		return 0
	}
	return n
}

// discard 는 그 판의 행을 걷는다. 판이 끝난 뒤와, 반쪽이라 분석하지 않는 자리에서 부른다.
//
// 실패해도 새지 않는다. 걷히지 않은 행은 sweepPlies 가 맡는다.
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
// 화면은 자기 번호 하나만 알면 되짚기를 열 수 있다 — 다른 쪽 번호를 기다리는 사이에 열면
// 「분석 중」이 아직 false 라, 그래프가 「남지 않았다」에 굳고 폴링도 시작하지 않는다.
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
// plies 는 그 판의 手数다. 0이어도 큐에는 선다 — 세지 못한 것과 두지 않은 것을 여기서 가르지
// 않고, 밀린 양만 그만큼 적게 잡힌다.
func (a *matchAnalyzer) enqueue(ctx context.Context, matchID string, plies int) {
	if a == nil || a.store == nil || matchID == "" {
		return
	}
	// 미리 재는 것을 여기서 끝낸다. 남은 手는 analyze 가 그 자리에서 재므로 표가 더 내줄
	// 것이 없고, 끊지 않으면 그 手가 두 큐에 같이 남아 두 번 세어진다(journal §116).
	// 덤으로 analyze 가 재는 手를 다른 워커가 동시에 집는 낭비도 없어진다.
	a.stopAhead(ctx, matchID)

	// 미리 재 둔 만큼을 뺀다. 남은 것이 이 큐에서 실제로 엔진을 부르는 양이다.
	// 위에서 끊어도 이 값은 변하지 않는다 — 세는 것이 이미 잰 행이다(aheadCount).
	pending := max(plies-a.aheadCount(ctx, matchID), 0)

	if err := a.store.ReadyAnalysisJob(ctx, matchID, pending); err != nil {
		// 큐에 세우지 못했다. 그 판은 평가치 없이 남는다 — 표시를 걷지 않으면 화면이
		// 영영 「분석 중」이다.
		a.dropJob(ctx, matchID)
		a.discard(ctx, matchID)
		a.analysis.ObserveGame(metrics.AnalysisDropped, 0)
		log.Printf("match: could not queue %s, leaving it without evals: %v", matchID, err)
	}
}

// dropJob 은 그 판을 큐에서 걷는다. 다 재고 나서와, 반쪽이라 분석하지 않는 자리에서 부른다.
//
// 「분석 중」이 여기서 꺼진다. 걷지 않으면 그 판이 영영 그 표시로 남는다.
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
// 읽지 못하면 false 다. 「남지 않았다」로 보이는 쪽이고, true 로 두면 화면이 오지 않을
// 값을 기다리며 폴링을 멈추지 않는다.
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

// gameIDsOf 는 자리 목록에서 판 번호만 뽑는다. 평가치를 쓰는 쪽이 번호만 보므로
// 그 자리가 자리를 알 필요가 없다.
func gameIDsOf(seats []analysisSeat) []int64 {
	ids := make([]int64, 0, len(seats))
	for _, s := range seats {
		ids = append(ids, s.gameID)
	}
	return ids
}

// kifuOf 는 다시 둘 기보 하나를 고른다. 두 행에 같은 수가 들어가므로 한 행이면 된다.
//
// 구멍 없는 행 중 가장 긴 것을 쓴다. 기록기는 큐가 차면 이벤트를 버리고 계속하고
// (dbRecorder.send) 두 행을 각자 쓰므로 한쪽에만 手가 빌 수 있다.
//
// 「구멍이 없다」로는 모자란다. 같은 유실이 끝에 나면 구멍 대신 절단이 되고, 그 행은
// 빈틈없이 이어지므로 멀쩡해 보인다 — 그대로 쓰면 그 판이 짧게 끝난 판으로 둔갑해
// 앞부분만 실력에 들어간다(analyze 의 ply < len(moves)). 긴 쪽을 고르는 것이 그 답이다.
//
// 구멍 난 행을 그냥 쓸 수는 없다. 부르는 쪽이 색인을 手数로 쓰는데 한 칸이 비면 그 뒤가
// 전부 밀리고, 수순이 불법이 되는 것보다 手番이 경고 없이 뒤집히는 쪽이 나쁘다.
//
// 같은 길이면 앞 자리가 이긴다. 자리는 색으로 정렬돼 있어서(collect) 그 답이 실행마다
// 달라지지 않는다.
//
// whole 은 고른 행이 판 끝까지 있나다. 구멍 때문에 버린 행이 더 긴 手数를 갖고 있으면
// false 다 — 그때 고른 행은 뒤가 잘린 것이다.
//
// 잘린 행의 창과 짧게 끝난 판의 창은 판정이 똑같이 나온다. 그래도 가르는 것은 규칙이
// 하나이기 때문이다: 잰 창이 그 판의 전부면 넣고, 판이 더 길었던 것을 알면 뺀다.
// 엔진이 창 안에서 죽었을 때 버리는 것과 같은 규칙이고(analyze), 가르지 않으면 같은 표본이
// 끊긴 이유에 따라 들어갔다 나갔다 한다(journal §95).
//
// 두 행이 같은 자리에서 같이 잘리면 잡아내지 못한다. 서로를 비교해 알아내는 값이라 그때는
// 둘이 일치하고, 그 판은 짧게 끝난 판으로 남는다 — 기록에 진짜 마지막 手数가 없다.
//
// 그래서 두 행이 다 길이를 말해야 한다. 읽지 못한 행과 빈 행은 둘 다 아무 말도 하지 않고,
// 남은 한 행만으로는 그 행이 끝까지인지 잘렸는지를 가릴 수 없다.
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
			// 길이를 말해 준 행만 센다. 읽지 못한 행과 빈 행은 둘 다 아무 말도 하지 않는데,
			// 읽히기만 한 것을 세면 빈 행이 「봤다」로 들어간다.
			told++
			maxPly = max(maxPly, got.Moves[n-1].Ply)
		}
		usi, contiguous := contiguousMoves(got)
		if !contiguous {
			log.Printf("match: game %d has a gap in its moves", seat.gameID)
			continue
		}
		if len(usi) > len(moves) {
			rec, moves = got, usi
		}
	}
	// 길이를 말하지 않은 행이 있으면 판 길이를 모른다. whole 이 두 행을 견준 값이라 한쪽이
	// 없으면 뜻이 없고, 그때 true 를 주면 긴 판이 짧게 끝난 판으로 들어간다.
	return rec, moves, told == len(seats) && len(moves) >= maxPly, len(moves) > 0
}

// readRecord 는 한 행을 읽는다. 한 번 다시 해 본다 — 잠깐 어긋난 것과 정말 없는 것을
// 여기서 갈라야 한다.
//
// 읽지 못한 행 하나가 두 사람의 실력을 다 버린다(whole). 그 판은 이미 깊이 12짜리 탐색을
// 수백 번 쓴 뒤라, 풀이 한 번 딸꾹한 값으로 그걸 버리는 것이 아깝다.
func (a *matchAnalyzer) readRecord(ctx context.Context, gameID int64) (store.GameRecord, error) {
	got, err := a.store.GameRecordAnyOwner(ctx, gameID)
	if err == nil || errors.Is(err, store.ErrNoGame) || ctx.Err() != nil {
		return got, err
	}
	return a.store.GameRecordAnyOwner(ctx, gameID)
}

// contiguousMoves 는 手数가 1부터 빈틈없이 이어질 때만 수순을 준다.
//
// 뒤가 잘린 것은 여기서 잡아내지 못한다 — 그것도 빈틈없이 이어진다(kifuOf).
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

// analyze 는 한 판을 처음부터 다시 재서 eval_cp 와 두 사람의 실력 추정치를 채운다.
//
// 한 번 재서 두 행에 쓴다. eval_cp 는 先手 관점이고 뒤집는 것은 되짚기다(review.go).
//
// 개입은 대인전에 없다. 그래도 판정을 버리지 않는 것은 skill.Move 가 먹는 값이 전부
// 여기서 이미 나오기 때문이고, 그래서 추가 탐색이 0이다(journal §95).
//
// Before 로 직전 칸을 덮는 것은 일부러다(kifu/import.go 와 같은 모양). 같은 칸에 두
// 탐색이 쓰고, 그래야 되짚기가 읽는 값이 엔진 대국의 것과 같은 규약이 된다(journal §41).
// analyze 는 그 판을 다 재고 결과 이름을 준다. 이름은 metrics.Analysis 의 어휘다 —
// 중간에 끊긴 판에는 done 을 주지 않는다.
func (a *matchAnalyzer) analyze(ctx context.Context, key string, seats []analysisSeat) string {
	ids := gameIDsOf(seats)
	// 가져온 판은 판정 결과가 悪手 줄로 남는다. 대인전은 개입이 없어 그 자리가 비어 있다.
	_, imported := importedGameID(key)
	// 평가치는 두 행에 다 쓰지만(ids) 기보는 한 행에서 읽는다. 아래 로그가 rec.ID 를
	// 쓰는 것은 그래서다 — 폴백이 걸린 판에서 ids[0] 을 적으면 읽지 않은 행을 가리킨다.
	rec, moves, whole, ok := a.kifuOf(ctx, seats)
	if !ok {
		return metrics.AnalysisFailed
	}

	// 1手目를 둔 색. 手数 홀짝으로 가르지 않는다 — 駒落ち는 上手가 먼저 두므로 그 규약이
	// 뒤집힌다(journal §88). 대인전은 지금 平手 확정이지만 그 사실이 여기 박히면
	// 手合이 붙는 날 실력이 엉뚱한 사람에게 쌓인다.
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
		// 그 판은 실력 추정에서 빠진다. 평가치도 대개 같이 빠진다 — 판정 안의 replay 가
		// 같은 문자열에서 같이 실패하고, 그러면 HasEvals 가 false 다(game.Judge).
		log.Printf("match: game %d start sfen %q: %v", rec.ID, rec.StartSFEN, err)
	}

	analyst := a.newAnalyst()
	// 미리 재 둔 것을 한 번에 읽는다. 아래 루프가 手마다 이 맵을 먼저 본다.
	measured := a.measuredOf(ctx, key)
	byColor := map[shogi.Color][]skill.Move{}
	stopped := false
	for ply := 1; ply <= len(moves); ply++ {
		// 미리 재 둔 것이 있으면 그것을 쓴다. 없으면 여기서 잰다 — 미리 재는 쪽이
		// 따라가지 못한 만큼만 이 자리에서 엔진을 부른다.
		got, ok := measured[ply]
		var err error
		if !ok {
			got, err = a.judgeOne(ctx, analyst, start, moves[:ply], ply)
		}
		// 끊기는 이유가 둘이고 성질이 같다. 엔진이 답하지 못했거나, 판정이 국면을
		// 되만들지 못했거나(HasEvals) — 어느 쪽이든 뒤의 手도 전부 같은 자리에서 실패한다.
		// 매번 같은 수순을 한 手 늘려 다시 두기 때문이다.
		//
		// 되만들지 못한 판정은 평가치도 실력도 주지 못한다. 부호를 정하지 못했다는 뜻이고,
		// 그러면 駒落ち의 기준점이 0으로 남아 낙폭까지 틀어진다(intervene.Input.BaselineCp).
		if err != nil {
			log.Printf("match: analysis of game %d stopped at ply %d: %v", rec.ID, ply, err)
			// 창을 다 지난 뒤에 끊겼으면 그 표본은 온전하다 — 뒤가 없는 것은 애초에
			// 세지 않는 구간이다. 마지막 手에서 끊긴 것도 온전하다: 잃은 것이 그 한 手라
			// 한 手 짧게 끝난 판과 같은 표본이다.
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
	// 여기서 만들지 않고 큐에 세운다. 이 잡이 걷혀야 「분석 중」이 꺼지는데 문항 쪽
	// 예산이 5분이라(quizTimeout), 한 잡에 두면 그래프가 다 찬 뒤에도 그만큼
	// 폴링이 이어진다(journal §138).
	if imported && !a.queueQuiz(ctx, seats[0].gameID) {
		// 워커를 잡지 않는다. 여기서 만들면 최대 5분 동안 이 워커가 판도 手도 집지
		// 않는데, 자리를 세어 둔 것이 바로 그것을 막으려는 것이다(quizSlots).
		// 엔진 대국이 같은 자리에서 하는 것과 같은 모양이다(ws.go).
		go a.buildQuizNow(context.WithoutCancel(ctx), seats[0].gameID)
	}
	return metrics.AnalysisDone
}

// judge 는 한 手를 시한 안에서 잰다. 시한을 넘기면 그 자리에서 끊긴 것으로 친다 —
// 뒤의 手도 같은 국면을 지나야 하므로 다음도 넘길 공산이 크다(analysisJudgeDeadline).
func (a *matchAnalyzer) judge(
	ctx context.Context, analyst game.Analyst, start string, moves []string, ply int,
) (game.Judgement, error) {
	ctx, cancel := context.WithTimeout(ctx, a.deadlineOf())
	defer cancel()
	return analyst.Judge(ctx, start, moves, ply)
}

// judgeOne 은 手 하나를 재서 필요한 칸만 남긴다.
//
// HasEvals 가 false 면 오류로 바꾼다. 부르는 쪽 둘이 그 자리를 같게 다뤄야 해서다 —
// 미리 재는 쪽은 그 판을 그만두고, 판이 끝날 때는 거기서 멈춘다.
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
// 부르는 쪽이 센 값을 쓴다 — 여기서는 그게 Judge 에 넘긴 바로 그 값이다.
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
// 판을 다 잰 뒤에 한 번에 읽고 쓴다. 지난 값 위에 얹는 읽기-쓰기라, 재는 동안(깊이
// 12짜리 탐색 수백 번) 열어 두면 그 사이의 쓰기 전체가 덮인다.
//
// 그래서 읽은 값이 그대로일 때만 쓴다(SaveSkillEstimateIfSamples). 그냥 덮으면 그
// 사이에 끝난 엔진 대국 전체가 사라진다 — 그쪽은 세션이 끝나서 다시 쓸 일이 없다.
//
// 경합에 지면 다시 읽어 얹는다. 手는 이미 손에 있으므로 엔진을 다시 부르지 않는다.
//
// 마지막 쓰기는 취소를 뗀다. 판을 다 잰 뒤에 오는 자리라, 여기서 끊기면 재느라 쓴
// 탐색 전체가 버려지고 그 판은 다시 재지지 않는다 — 기록기가 같은 이유로 쓰기에
// 세션 ctx 를 쓰지 않는다(dbRecorder.run).
//
// 종료를 이겨내지는 못한다. 이 워커를 기다려 주는 자리가 없어서 풀이 먼저 닫히면
// 그대로 실패한다 — 떼는 것은 취소뿐이다.
func (a *matchAnalyzer) updateSkill(ctx context.Context, seats []analysisSeat, byColor map[shogi.Color][]skill.Move) {
	ctx = context.WithoutCancel(ctx)
	for _, seat := range seats {
		got := byColor[seat.color]
		if len(got) == 0 {
			// 창(21~60手) 안에 이 사람의 手가 없다. 46手에 끝난 판이 실제로 그랬다
			// (journal §94).
			continue
		}
		// 한쪽만 쌓일 수 있다. 프로파일은 사람마다 따로이고 둘을 견주는 자리가 없어서
		// 짝이 맞아야 할 이유가 없다 — 평가치를 두 행에 다 쓰는 것과 다른 성질이다.
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
// 상대가 대국 중인 세션이면 판정마다 쓰므로 몇 번을 해도 진다. 그때는 포기하는 것이
// 맞다 — 그 세션이 자기 트랙을 갖고 있어서, 여기가 이겨도 다음 판정이 도로 덮는다.
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
