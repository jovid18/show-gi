package server

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/metrics"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// matchAnalyzer 는 대인전의 평가치와 실력 추정치를 채운다(journal §83 · §95).
//
// 두 갈래로 일한다. 두는 동안 手마다 미리 재 두고(lookAhead), 판이 끝나면 그 결과로
// 평가치를 쓰고 실력을 커밋한다(analyze). 미리 못 잰 手는 끝날 때 그 자리에서 잰다.
//
// 미리 재는 쪽이 없으면 판이 끝나는 순간 그 판의 手数가 한꺼번에 몰린다 — 프로덕션에서
// 그 봉우리를 쟀다(journal §105).
//
// 착수 경로는 여전히 엔진을 모른다. internal/match 가 usi 를 import 하지 않는 규약은
// 그대로이고, 미리 재는 것은 기록기를 지나 이 패키지에서 일어나며 착수를 막지 않는다.
//
// 그래도 대인전이 대국 중에 엔진을 쓰기 시작한다. 같은 시간대의 엔진 대국과 풀을 다투는
// 것이 이 설계의 값이다(journal §105).
//
// 워커 수는 손잡이다. 엔진 풀을 지금 두고 있는 사람들과 공유하는데(01-core.md §4)
// 기본값이 풀 크기와 같다 — 풀이 사람이 기다리는 쪽에 먼저 빌려주므로 다 가져가도
// 착수가 안 밀린다(usi.priorityOf · cmd/api).
//
// 워커가 여럿이면 같은 판의 analyze 와 늦은 미리 재기가 겹친다. 평가치는 안 틀어진다 —
// DB 에 쓰는 것은 analyze 의 순차 루프뿐이고 미리 재는 쪽은 手数를 키로 한 맵에만
// 쓴다(journal §106). 겹칠 때 새는 자리는 remember 가 막는다.
//
// 진행 상태는 메모리다. 배포하면 하던 분석이 끊기고 그 판은 평가치 없이 남는다 —
// 방과 같은 성질이다(match.Hub). 실력 추정에 대인전이 간헐적으로만 기여하는 것은
// 그래서이고, 고장이 아니다.
type matchAnalyzer struct {
	store      *store.Store
	newAnalyst func() game.Analyst

	queue chan analysisJob
	// plies 는 아직 두는 중인 판의 手를 받는 줄이다. queue 와 갈라 둔 이유는 단위다 —
	// 저쪽은 판을 세고 이쪽은 手를 센다.
	plies chan plyJob

	// analysis 는 계측 창구다. 늘 non-nil 이다(metrics.Registry.Analysis).
	analysis *metrics.Analysis

	// judgeDeadline 은 한 手를 재는 시한이다. 0이면 analysisJudgeDeadline —
	// 대국이 game.Config.MoveDeadline 을 두는 것과 같은 규약이다.
	judgeDeadline time.Duration

	mu      sync.Mutex
	pending map[int64]struct{}
	// backlog 는 아직 안 꺼낸 일의 양이다. 판과 手 둘을 센다 — 판 수는 밀린 일의
	// 크기를 못 말한다(journal §91의 27·34·123手).
	//
	// 手 쪽은 미리 재는 줄의 길이에, 판이 끝날 때 아직 안 잰 手를 더한 값이다.
	// 그래서 이 값이 0 근처에 있는 것이 「미리 재는 쪽이 따라가고 있다」다.
	backlogGames int
	backlogPlies int
	// ahead 는 방마다 미리 재 둔 手다. 판이 끝나면 비운다.
	ahead map[string]*aheadOfMatch
}

// plyJob 은 아직 두는 중인 판의 手 하나다.
//
// 수순을 복사해서 든다. 부르는 쪽의 슬라이스는 다음 手에 계속 자라므로 그대로 들면
// 재는 시점에 무엇이 들어 있는지가 정해지지 않는다.
type plyJob struct {
	matchID string
	start   string
	moves   []string
	ply     int
}

// aheadOfMatch 는 한 방에서 미리 재 둔 것들이다.
type aheadOfMatch struct {
	plies map[int]judged
	// dead 는 이 판을 미리 재는 것을 그만뒀다는 표시다.
	//
	// 한 手가 실패하면 뒤의 手도 전부 같은 자리에서 실패한다(analyze 의 같은 판단) —
	// 그만두지 않으면 남은 手数만큼 탐색을 버린다.
	dead bool
}

// judged 는 미리 재 둔 手 하나의 결과다.
//
// game.Judgement 를 그대로 안 든다. 그 안의 explain.Facts 가 태그 슬라이스를 드는데
// 대인전에는 개입이 없어서 아무도 안 읽고, 판이 끝날 때까지 살려 두면 방마다 手数만큼
// 쌓인다.
type judged struct {
	beforeCp int
	afterCp  int
	move     skill.Move
}

// errCannotReplay 는 엔진은 답했는데 판정이 국면을 못 되만든 자리다(Judgement.HasEvals).
//
// 오류로 바꿔 드는 이유는 부르는 쪽이 둘을 같이 다루기 때문이다 — 어느 쪽이든 그 手부터
// 뒤가 전부 같은 자리에서 실패한다.
var errCannotReplay = errors.New("cannot replay the position")

// analysisJob 은 줄에 서는 한 판이다.
//
// matchID 로 미리 재 둔 것을 찾는다(aheadOfMatch).
//
// plies 는 아직 안 잰 手数다. 총 手数가 아닌 이유는 밀린 양의 뜻이다 — 미리 재 둔 手는
// 이 줄에서 엔진을 안 부르므로, 그것까지 세면 밀린 양이 실제 일보다 커진다.
type analysisJob struct {
	matchID string
	seats   []analysisSeat
	plies   int
}

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

// analysisQueue 는 끝난 판을 몇 개까지 쌓아 둘 것인가다. 넘치면 버린다 — 여기서 막으면
// 판이 끝나는 자리가 같이 막힌다.
//
// 미리 재는 쪽이 따라가고 있으면 이 줄의 일은 평가치를 쓰고 실력을 커밋하는 것뿐이라
// 엔진을 안 부른다. 따라가지 못한 만큼만 여기서 잰다.
const analysisQueue = 64

// prefetchQueue 는 미리 재려고 세워 둘 手의 최대 개수다.
//
// 手로 세는 것이 판 단위와 갈리는 자리다. 판 단위는 한 칸이 그 판의 手数를 통째로 들어서
// 최악이 상한의 手数배가 된다(journal §105).
//
// 2048은 실측 배수구에서 서른 몇 분이다(journal §105). 크게 두면 「밀렸다」를 알아차리기
// 전에 메모리가 먼저 자란다.
//
// 넘치면 버린다. 버려도 잃는 것이 없다 — 그 手는 판이 끝날 때 그 자리에서 잰다.
const prefetchQueue = 2048

// analysisJudgeDeadline 은 한 手를 재는 데 줄 최대 시간이다. 대국의 판정과 같은 값을
// 쓴다(game.DefaultMoveDeadline).
//
// 없으면 한 手가 워커를 영영 붙잡는다. 판정이 매 手 詰み solver 를 부르는데
// (game.engineAnalyst.Judge) 그것이 `go mate infinite` 이라 스스로 안 끝나고, 취소로만
// 풀린다(usi.Engine.SearchMate). 대국 쪽은 세션이 시한을 걸어서 그 자리가 없다.
//
// 붙잡히는 것이 워커 하나로 안 끝난다. solver 풀이 둘뿐이고 詰み 게이지·종반 판정·퀴즈와
// 공유라, 한 국면이 그 절반을 프로세스가 죽을 때까지 들고 있는다.
const analysisJudgeDeadline = game.DefaultMoveDeadline

// newMatchAnalyzer 는 워커를 띄운다. store 나 analyst 가 없으면 nil 을 준다 — 엔진
// 없는 배포에서 대인전이 그대로 도는 규약을 여기서도 지킨다. nil 인 채로 불려도 되도록
// 아래 메서드가 전부 nil 수신자를 받는다.
// workers 는 동시에 재는 goroutine 수다. 1 미만이면 1로 올린다.
func newMatchAnalyzer(
	ctx context.Context, st *store.Store, newAnalyst func() game.Analyst, reg *metrics.Registry,
	workers int,
) *matchAnalyzer {
	if st == nil || newAnalyst == nil {
		return nil
	}
	workers = max(workers, 1)
	a := &matchAnalyzer{
		store:      st,
		newAnalyst: newAnalyst,
		queue:      make(chan analysisJob, analysisQueue),
		plies:      make(chan plyJob, prefetchQueue),
		analysis:   reg.Analysis(),
		pending:    map[int64]struct{}{},
		ahead:      map[string]*aheadOfMatch{},
	}
	for range workers {
		go a.run(ctx)
	}
	go a.abandonOnStop(ctx)
	return a
}

// abandonOnStop 은 프로세스가 멈출 때 줄에 남은 판을 버린 것으로 센다.
//
// 세는 자리가 워커와 따로 있어야 한다. 워커는 한 판을 몇 분씩 재고 있어서, 그 판이
// 풀린 뒤에 세면 main 이 이미 마지막 EMF 줄을 낸 뒤다(startEmitter).
//
// 안 세면 AnalysisGamesDropped 가 0을 가리키면서 판이 평가치 없이 남는다. 큐가 꽉 차서
// 버리는 자리는 이미 세는데(enqueue) 종료만 안 셌고, 되짚기는 두 경우를 구별하지
// 못하므로(analyzing 이 false) 그 지표가 유일한 눈이다 — 실측은 journal §105.
//
// 줄을 비운 뒤에 들어온 판은 못 센다. 그 창이 종료 자체라서, 닫으려면 판이 끝나는
// 자리를 막아야 한다 — enqueue 가 막지 않기로 한 것과 같은 판단이다.
func (a *matchAnalyzer) abandonOnStop(ctx context.Context) {
	<-ctx.Done()
	for {
		select {
		case job := <-a.queue:
			a.took(job)
			a.forget(gameIDsOf(job.seats))
			a.discard(job.matchID)
			a.analysis.ObserveGame(metrics.AnalysisDropped, 0)
		default:
			return
		}
	}
}

func (a *matchAnalyzer) run(ctx context.Context) {
	// 빌리는 쪽의 이름을 여기서 한 번 붙인다. 아래 판정이 전부 이 컨텍스트를 지나므로
	// 풀 대기가 borrower=analysis 로 갈린다(usi.WithBorrower).
	ctx = usi.WithBorrower(ctx, usi.BorrowerAnalysis)
	// 미리 재는 쪽은 판정기를 한 벌만 쓴다. 手마다 새로 뜨면 방마다 하나씩 사는 것과
	// 같아지는데, 판정기는 상태가 없고 풀을 빌려 쓸 뿐이다.
	ahead := a.newAnalyst()
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-a.queue:
			a.took(job)
			started := time.Now()
			result := a.analyze(ctx, job.matchID, job.seats)
			a.analysis.ObserveGame(result, time.Since(started))
			a.forget(gameIDsOf(job.seats))
			a.discard(job.matchID)
		case p := <-a.plies:
			a.tookPly()
			a.lookAhead(ctx, ahead, p)
		}
	}
}

// lookAhead 는 手 하나를 미리 재서 들고 있는다.
//
// 실패는 조용히 끝낸다 — 판이 끝날 때 같은 手를 다시 재고, 판정을 남기는 자리는 거기다.
func (a *matchAnalyzer) lookAhead(ctx context.Context, analyst game.Analyst, p plyJob) {
	if a.givenUp(p.matchID) {
		return
	}
	got, err := a.judgeOne(ctx, analyst, p.start, p.moves, p.ply)
	if err != nil {
		a.giveUp(p.matchID)
		return
	}
	a.remember(p.matchID, p.ply, got)
}

// remember 는 미리 잰 것을 그 방의 자리에 넣는다. 자리가 없으면 버린다.
//
// 여기서 자리를 새로 만들지 않는 것이 규약이다. 워커가 둘 이상이면 같은 판의 analyze 와
// 늦은 미리 재기가 동시에 도는데, discard 뒤에 도착한 쪽이 자리를 다시 만들면 그 항목을
// 아무도 안 지운다 — 판마다 하나씩 샌다. 자리는 prefetch 가 만든다: 그쪽은 판이 살아
// 있는 동안에만 불리므로 analyze 보다 반드시 먼저다.
func (a *matchAnalyzer) remember(matchID string, ply int, got judged) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if m := a.ahead[matchID]; m != nil && !m.dead {
		m.plies[ply] = got
	}
}

// prefetch 는 방금 둔 手를 미리 재는 줄에 세운다. 착수 경로에서 불리므로 즉시 돌아온다.
func (a *matchAnalyzer) prefetch(matchID, start string, moves []string, ply int) {
	if a == nil || start == "" || len(moves) == 0 {
		return
	}
	select {
	case a.plies <- plyJob{matchID: matchID, start: start, moves: moves, ply: ply}:
		a.mu.Lock()
		a.backlogPlies++
		// 자리를 여기서 만든다. remember 가 안 만드는 이유는 그쪽 주석에 있다.
		if a.ahead[matchID] == nil {
			a.ahead[matchID] = &aheadOfMatch{plies: map[int]judged{}}
		}
		games, plies := a.backlogGames, a.backlogPlies
		a.mu.Unlock()
		a.analysis.SetBacklog(games, plies)
	default:
		// 줄이 찼다. 이 手는 판이 끝날 때 잰다.
	}
}

// tookPly 는 미리 재는 줄에서 하나를 꺼낸 만큼 밀린 양을 내린다.
func (a *matchAnalyzer) tookPly() {
	a.mu.Lock()
	a.backlogPlies--
	games, plies := a.backlogGames, a.backlogPlies
	a.mu.Unlock()
	a.analysis.SetBacklog(games, plies)
}

func (a *matchAnalyzer) givenUp(matchID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.ahead[matchID]
	return m != nil && m.dead
}

func (a *matchAnalyzer) giveUp(matchID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if m := a.ahead[matchID]; m != nil {
		m.dead = true
	}
}

// recall 은 미리 재 둔 手를 준다.
func (a *matchAnalyzer) recall(matchID string, ply int) (judged, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.ahead[matchID]
	if m == nil {
		return judged{}, false
	}
	got, ok := m.plies[ply]
	return got, ok
}

// aheadCount 는 그 방에서 미리 재 둔 手数다. 판이 끝날 때 남은 일의 크기를 그것으로 센다.
func (a *matchAnalyzer) aheadCount(matchID string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	m := a.ahead[matchID]
	if m == nil {
		return 0
	}
	return len(m.plies)
}

// discard 는 그 방의 것을 버린다. 판이 끝난 뒤와, 반쪽이라 분석하지 않는 자리에서 부른다.
func (a *matchAnalyzer) discard(matchID string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.ahead, matchID)
}

// took 은 줄에서 꺼낸 만큼 밀린 양을 내린다.
func (a *matchAnalyzer) took(job analysisJob) {
	a.mu.Lock()
	a.backlogGames--
	a.backlogPlies -= job.plies
	games, plies := a.backlogGames, a.backlogPlies
	a.mu.Unlock()
	a.analysis.SetBacklog(games, plies)
}

// hold 는 그 판을 미리 「분석 중」으로 세운다.
//
// 줄에 세우기 전에 표시해야 한다. 두 행의 번호는 따로 정해지는데(matchRecords.collect)
// 화면은 자기 번호 하나만 알면 되짚기를 열 수 있다 — 다른 쪽 번호를 기다리는 사이에 열면
// 「분석 중」이 아직 false 라, 그래프가 「남지 않았다」에 굳고 폴링도 안 시작한다.
func (a *matchAnalyzer) hold(id int64) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending[id] = struct{}{}
}

// enqueue 는 끝난 한 판의 두 자리를 줄에 세운다. hold 로 이미 표시된 것을 받는다.
//
// plies 는 그 판의 手数다. 0이어도 줄에는 선다 — 못 센 것과 안 둔 것을 여기서 가르지
// 않고, 밀린 양만 그만큼 적게 잡힌다.
func (a *matchAnalyzer) enqueue(matchID string, seats []analysisSeat, plies int) {
	if a == nil || len(seats) == 0 {
		return
	}
	ids := gameIDsOf(seats)
	for _, id := range ids {
		a.hold(id)
	}

	// 미리 재 둔 만큼을 뺀다. 남은 것이 이 줄에서 실제로 엔진을 부르는 양이다.
	pending := max(plies-a.aheadCount(matchID), 0)

	select {
	case a.queue <- analysisJob{matchID: matchID, seats: seats, plies: pending}:
		a.mu.Lock()
		a.backlogGames++
		// 미리 재 둔 만큼을 뺀 값을 센다. job 이 드는 값과 같아야 한다 — took 이 그것을
		// 빼므로, 여기서 총 手数를 더하면 차액이 영구히 남는다.
		a.backlogPlies += pending
		games, backlog := a.backlogGames, a.backlogPlies
		a.mu.Unlock()
		a.analysis.SetBacklog(games, backlog)
	default:
		a.forget(ids)
		a.discard(matchID)
		a.analysis.ObserveGame(metrics.AnalysisDropped, 0)
		log.Printf("match: analysis queue is full, leaving games %v without evals", ids)
	}
}

// gameIDsOf 는 자리 목록에서 판 번호만 뽑는다. 표시(pending)와 평가치 쓰기가 번호만 보므로
// 그 둘이 자리를 알 필요가 없다.
func gameIDsOf(seats []analysisSeat) []int64 {
	ids := make([]int64, 0, len(seats))
	for _, s := range seats {
		ids = append(ids, s.gameID)
	}
	return ids
}

// analyzing 은 그 판이 아직 줄에 있거나 도는 중인가다. 되짚기가 이 값으로 「분석 중」과
// 「남지 않았다」를 가른다.
func (a *matchAnalyzer) analyzing(gameID int64) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.pending[gameID]
	return ok
}

func (a *matchAnalyzer) forget(ids []int64) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range ids {
		delete(a.pending, id)
	}
}

// kifuOf 는 다시 둘 기보 하나를 고른다. 두 행에 같은 수가 들어가므로 한 행이면 된다.
//
// 구멍 없는 행 중 가장 긴 것을 쓴다. 기록기는 큐가 차면 이벤트를 버리고 계속하고
// (dbRecorder.send) 두 행을 각자 쓰므로 한쪽에만 手가 빌 수 있다.
//
// 「구멍이 없다」로는 모자란다. 같은 유실이 끝에 나면 구멍이 아니라 절단이고, 그 행은
// 빈틈없이 이어지므로 멀쩡해 보인다 — 그대로 쓰면 그 판이 짧게 끝난 판으로 둔갑해
// 앞부분만 실력에 들어간다(analyze 의 ply < len(moves)). 긴 쪽을 고르는 것이 그 답이다.
//
// 구멍 난 행을 그냥 쓸 수는 없다. 부르는 쪽이 색인을 手数로 쓰는데 한 칸이 비면 그 뒤가
// 전부 밀리고, 수순이 불법이 되는 것보다 手番이 조용히 뒤집히는 쪽이 나쁘다.
//
// 같은 길이면 앞 자리가 이긴다. 자리는 색으로 정렬돼 있어서(collect) 그 답이 실행마다
// 달라지지 않는다.
//
// whole 은 고른 행이 판 끝까지 있나다. 구멍 때문에 버린 행이 더 긴 手数를 들고 있으면
// false 다 — 그때 고른 행은 뒤가 잘린 것이다.
//
// 잘린 행의 창과 짧게 끝난 판의 창은 판정이 똑같이 나온다. 그래도 가르는 것은 규칙이
// 하나이기 때문이다: 잰 창이 그 판의 전부면 넣고, 판이 더 길었던 것을 알면 뺀다.
// 엔진이 창 안에서 죽었을 때 버리는 것과 같은 규칙이고(analyze), 안 가르면 같은 표본이
// 끊긴 이유에 따라 들어갔다 나갔다 한다(journal §95).
//
// 두 행이 같은 자리에서 같이 잘리면 못 잡는다. 서로를 비교해 알아내는 값이라 그때는
// 둘이 일치하고, 그 판은 짧게 끝난 판으로 남는다 — 기록에 진짜 마지막 手数가 없다.
//
// 그래서 두 행이 다 길이를 말해야 한다. 못 읽은 행과 빈 행은 둘 다 아무 말도 안 하고,
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
			// 길이를 말해 준 행만 센다. 못 읽은 행과 빈 행은 둘 다 아무 말도 안 하는데,
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
	// 길이를 안 말한 행이 있으면 판 길이를 모른다. whole 이 두 행을 견준 값이라 한쪽이
	// 없으면 뜻이 없고, 그때 true 를 주면 긴 판이 짧게 끝난 판으로 들어간다.
	return rec, moves, told == len(seats) && len(moves) >= maxPly, len(moves) > 0
}

// readRecord 는 한 행을 읽는다. 한 번 다시 해 본다 — 잠깐 어긋난 것과 정말 없는 것을
// 여기서 갈라야 한다.
//
// 못 읽은 행 하나가 두 사람의 실력을 다 버린다(whole). 그 판은 이미 깊이 12짜리 탐색을
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
// 뒤가 잘린 것은 여기서 못 잡는다 — 그것도 빈틈없이 이어진다(kifuOf).
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
// 중간에 끊긴 판은 done 이 아니다.
func (a *matchAnalyzer) analyze(ctx context.Context, matchID string, seats []analysisSeat) string {
	ids := gameIDsOf(seats)
	// 평가치는 두 행에 다 쓰지만(ids) 기보는 한 행에서 읽는다. 아래 로그가 rec.ID 를
	// 쓰는 것은 그래서다 — 폴백이 걸린 판에서 ids[0] 을 적으면 안 읽은 행을 가리킨다.
	rec, moves, whole, ok := a.kifuOf(ctx, seats)
	if !ok {
		return metrics.AnalysisFailed
	}

	// 1手目를 둔 색. 手数 홀짝으로 안 가른다 — 駒落ち는 上手가 먼저 두므로 그 규약이
	// 뒤집힌다(journal §88). 대인전은 지금 平手 확정이지만 그 사실이 여기 박히면
	// 手合이 붙는 날 실력이 엉뚱한 사람에게 쌓인다.
	//
	// 판정에도 같은 문자열을 넘긴다. 여기서만 平手를 메우면 빈 칸을 가진 행에서
	// 「先手가 1手目」로 정해 놓고 엔진에는 빈 국면을 보내게 된다.
	start := startSFENOf(rec.StartSFEN)
	if !whole {
		// 잰 창이 그 판의 전부라고 말할 수 없다. 고른 행의 뒤가 잘렸거나 견줄 행을
		// 못 읽었거나이고(kifuOf), 어느 쪽이든 평가치만 채우고 실력은 안 쌓는다.
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
	byColor := map[shogi.Color][]skill.Move{}
	stopped := false
	for ply := 1; ply <= len(moves); ply++ {
		// 미리 재 둔 것이 있으면 그것을 쓴다. 없으면 여기서 잰다 — 미리 재는 쪽이
		// 못 따라간 만큼만 이 자리에서 엔진을 부른다.
		got, ok := a.recall(matchID, ply)
		var err error
		if !ok {
			got, err = a.judgeOne(ctx, analyst, start, moves[:ply], ply)
		}
		// 끊기는 이유가 둘이고 성질이 같다. 엔진이 못 답했거나, 판정이 국면을 못
		// 되만들었거나(HasEvals) — 어느 쪽이든 뒤의 手도 전부 같은 자리에서 실패한다.
		// 매 회차가 같은 수순을 한 手 늘려 다시 두기 때문이다.
		//
		// 되만들지 못한 판정은 평가치도 실력도 못 준다. 부호를 못 정했다는 뜻이고,
		// 그러면 駒落ち의 기준점이 0으로 남아 낙폭까지 틀어진다(intervene.Input.BaselineCp).
		if err != nil {
			log.Printf("match: analysis of game %d stopped at ply %d: %v", rec.ID, ply, err)
			// 창을 다 지난 뒤에 끊겼으면 그 표본은 온전하다 — 뒤가 없는 것은 애초에
			// 안 세는 구간이다. 마지막 手에서 끊긴 것도 온전하다: 잃은 것이 그 한 手라
			// 한 手 짧게 끝난 판과 같은 표본이다.
			//
			// 그 밖이면 남는 것이 더 긴 판의 앞부분뿐이고 그 구간이 체계적으로 쉬워서
			// 낙폭이 낮게 나오므로, 통째로 버린다(journal §95).
			if ply <= skill.AnchorToPly && ply < len(moves) {
				return metrics.AnalysisFailed
			}
			stopped = true
			break
		}
		if firstKnown && whole {
			if m := got.move; m.InAnchorWindow() {
				c := moverAt(first, ply)
				byColor[c] = append(byColor[c], m)
			}
		}
		a.setEval(ctx, ids, ply, got.afterCp)
		if ply > 1 {
			a.setEval(ctx, ids, ply-1, got.beforeCp)
		}
	}
	a.updateSkill(ctx, seats, byColor)
	if stopped {
		return metrics.AnalysisFailed
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

// judgeOne 은 手 하나를 재서 들고 있을 만큼만 남긴다.
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
		beforeCp: j.SenteCpBefore,
		afterCp:  j.SenteCpAfter,
		move:     skillMoveOf(j, ply),
	}, nil
}

// deadlineOf 는 지금 걸 시한이다. 안 정해 뒀으면 기본값이다.
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
// 12짜리 탐색 수백 번) 열어 두면 그 사이의 쓰기가 통째로 덮인다.
//
// 그래서 읽은 값이 그대로일 때만 쓴다(SaveSkillEstimateIfSamples). 그냥 덮으면 그
// 사이에 끝난 엔진 대국이 통째로 사라진다 — 그쪽은 세션이 끝나서 다시 쓸 일이 없다.
//
// 진 회차는 다시 읽어 얹는다. 手는 이미 손에 있으므로 엔진을 다시 안 부른다.
//
// 마지막 쓰기는 취소를 뗀다. 판을 다 잰 뒤에 오는 자리라, 여기서 끊기면 재느라 쓴
// 탐색이 통째로 버려지고 그 판은 다시 재지지 않는다 — 기록기가 같은 이유로 쓰기에
// 세션 ctx 를 안 쓴다(dbRecorder.run).
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
			// 이 판이 추정에 안 들어간 것으로 끝난다. 다음 판이 갱신 전 값 위에서 돈다.
			log.Printf("match: save skill %d: %v", seat.userID, err)
		}
	}
}

// skillWriteTimeout 은 한 사람의 얹기가 붙잡아 둘 수 있는 최대 시간이다. 취소를
// 떼어냈으므로(updateSkill) 상한이 여기 하나뿐이다.
//
// 사람마다 따로 센다. 한 예산을 둘이 나눠 쓰면 DB가 굼뜬 날 앞사람이 다 쓰고 뒷사람이
// 조용히 빠진다.
const skillWriteTimeout = 10 * time.Second

// skillCASTries 는 얹기를 몇 번까지 다시 해 볼 것인가다.
//
// 상대가 대국 중인 세션이면 판정마다 쓰므로 몇 번을 해도 진다. 그때는 포기하는 것이
// 맞다 — 그 세션이 자기 트랙을 들고 있어서, 여기가 이겨도 다음 판정이 도로 덮는다.
const skillCASTries = 3

// saveMoves 는 그 사람의 프로파일에 手들을 얹는다. 읽기-쓰기가 어긋나면 다시 읽는다.
func (a *matchAnalyzer) saveMoves(ctx context.Context, userID int64, moves []skill.Move) error {
	// 넣을 手가 없으면 아무것도 안 쓴다. 빈 채로 지나가면 아래가 제로값을 저장해서
	// 「매 수 최선」이 되고, 그건 척도의 가장 센 이름이다(skill.RankOf).
	if len(moves) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, skillWriteTimeout)
	defer cancel()

	for range skillCASTries {
		// 「없음」을 따로 안 가른다. 표본 0인 추정치를 넘기면 추정기가 기준선에서
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

func (a *matchAnalyzer) setEval(ctx context.Context, ids []int64, ply, senteCp int) {
	for _, id := range ids {
		if err := a.store.SetMoveEval(ctx, id, ply, senteCp); err != nil {
			log.Printf("match: set eval of game %d ply %d: %v", id, ply, err)
		}
	}
}
