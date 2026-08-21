package server

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// matchAnalyzer 는 끝난 대인전을 다시 재서 평가치와 실력 추정치를 채운다(journal §83 · §95).
//
// 두는 동안에는 엔진이 한 번도 안 돈다 — 그것이 이 갈래의 규약이고(internal/match 는
// usi 를 import 하지 않는다), 여기는 그 뒤에 기록에만 손을 댄다.
//
// 워커가 하나다. 엔진 풀은 지금 두고 있는 사람들과 공유다(01-core.md §4).
//
// 진행 상태는 메모리다. 배포하면 하던 분석이 끊기고 그 판은 평가치 없이 남는다 —
// 방과 같은 성질이다(match.Hub). 실력 추정에 대인전이 간헐적으로만 기여하는 것은
// 그래서이고, 고장이 아니다.
type matchAnalyzer struct {
	store      *store.Store
	newAnalyst func() game.Analyst

	queue chan []analysisSeat

	// judgeDeadline 은 한 手를 재는 시한이다. 0이면 analysisJudgeDeadline —
	// 대국이 game.Config.MoveDeadline 을 두는 것과 같은 규약이다.
	judgeDeadline time.Duration

	mu      sync.Mutex
	pending map[int64]struct{}
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

// analysisQueue 는 몇 판까지 쌓아 둘 것인가다. 넘치면 버린다 — 여기서 막으면 판이
// 끝나는 자리가 같이 막힌다.
const analysisQueue = 64

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
func newMatchAnalyzer(ctx context.Context, st *store.Store, newAnalyst func() game.Analyst) *matchAnalyzer {
	if st == nil || newAnalyst == nil {
		return nil
	}
	a := &matchAnalyzer{
		store:      st,
		newAnalyst: newAnalyst,
		queue:      make(chan []analysisSeat, analysisQueue),
		pending:    map[int64]struct{}{},
	}
	go a.run(ctx)
	return a
}

func (a *matchAnalyzer) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case seats := <-a.queue:
			a.analyze(ctx, seats)
			a.forget(gameIDsOf(seats))
		}
	}
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
func (a *matchAnalyzer) enqueue(seats []analysisSeat) {
	if a == nil || len(seats) == 0 {
		return
	}
	ids := gameIDsOf(seats)
	for _, id := range ids {
		a.hold(id)
	}

	select {
	case a.queue <- seats:
	default:
		a.forget(ids)
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
func (a *matchAnalyzer) analyze(ctx context.Context, seats []analysisSeat) {
	ids := gameIDsOf(seats)
	// 평가치는 두 행에 다 쓰지만(ids) 기보는 한 행에서 읽는다. 아래 로그가 rec.ID 를
	// 쓰는 것은 그래서다 — 폴백이 걸린 판에서 ids[0] 을 적으면 안 읽은 행을 가리킨다.
	rec, moves, whole, ok := a.kifuOf(ctx, seats)
	if !ok {
		return
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
	for ply := 1; ply <= len(moves); ply++ {
		j, err := a.judge(ctx, analyst, start, moves[:ply], ply)
		// 끊기는 이유가 둘이고 성질이 같다. 엔진이 못 답했거나, 판정이 국면을 못
		// 되만들었거나(HasEvals) — 어느 쪽이든 뒤의 手도 전부 같은 자리에서 실패한다.
		// 매 회차가 같은 수순을 한 手 늘려 다시 두기 때문이다.
		//
		// 되만들지 못한 판정은 평가치도 실력도 못 준다. 부호를 못 정했다는 뜻이고,
		// 그러면 駒落ち의 기준점이 0으로 남아 낙폭까지 틀어진다(intervene.Input.BaselineCp).
		if err != nil || !j.HasEvals {
			why := "cannot replay the position"
			if err != nil {
				why = err.Error()
			}
			log.Printf("match: analysis of game %d stopped at ply %d: %s", rec.ID, ply, why)
			// 창을 다 지난 뒤에 끊겼으면 그 표본은 온전하다 — 뒤가 없는 것은 애초에
			// 안 세는 구간이다. 마지막 手에서 끊긴 것도 온전하다: 잃은 것이 그 한 手라
			// 한 手 짧게 끝난 판과 같은 표본이다.
			//
			// 그 밖이면 남는 것이 더 긴 판의 앞부분뿐이고 그 구간이 체계적으로 쉬워서
			// 낙폭이 낮게 나오므로, 통째로 버린다(journal §95).
			if ply <= skill.AnchorToPly && ply < len(moves) {
				return
			}
			break
		}
		if firstKnown && whole {
			if m := skillMoveOf(j, ply); m.InAnchorWindow() {
				c := moverAt(first, ply)
				byColor[c] = append(byColor[c], m)
			}
		}
		a.setEval(ctx, ids, ply, j.SenteCpAfter)
		if ply > 1 {
			a.setEval(ctx, ids, ply-1, j.SenteCpBefore)
		}
	}
	a.updateSkill(ctx, seats, byColor)
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
