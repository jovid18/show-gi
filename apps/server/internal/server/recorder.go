package server

import (
	"context"
	"log"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// dbRecorder 는 대국을 DB에 남긴다. `game` 과 `store` 가 만나는 유일한 자리다.
//
// **세션 goroutine을 막지 않는다.** 이벤트를 버퍼 채널에 던지고 자기 goroutine이 쓴다.
// 상태를 goroutine 하나가 소유한다는 것이 이 프로젝트의 정합성이라, DB가 느리다고
// 그 줄을 세우지 않는다.
type dbRecorder struct {
	events chan recordEvent
}

type recordKind int

const (
	evStarted recordKind = iota
	evMoved
	evEvaluated
	evRetracted
	evFinished
)

type recordEvent struct {
	kind      recordKind
	startSFEN string
	color     shogi.Color
	ply       int
	usi       string
	by        game.Side
	cp        int
	verdict   intervene.Verdict
	explained explain.Result
	status    game.Status
	winner    game.Side
}

// recordQueue 는 이벤트 버퍼 크기다.
//
// 한 판이 200수 남짓이고 쓰기 한 번이 밀리초 단위라, 이 크기가 차는 것은 **DB가
// 멈췄을 때뿐**이다. 그때는 넘치는 것을 버린다 — 기다리면 대국이 멈춘다.
const recordQueue = 256

// newDBRecorder 는 대국 하나를 기록할 Recorder 를 만든다. ctx 가 끝나면 정리한다.
func newDBRecorder(ctx context.Context, st *store.Store, level intervene.Level) game.Recorder {
	r := &dbRecorder{events: make(chan recordEvent, recordQueue)}
	go r.run(ctx, st, level)
	return r
}

func (r *dbRecorder) send(ev recordEvent) {
	select {
	case r.events <- ev:
	default:
		// **버리고 계속한다.** 기록은 부가 기능이고 대국이 본체다.
		// 조용히 버리지는 않는다 — 구멍이 생긴 것을 나중에 알아야 한다.
		log.Printf("game record: queue full, dropping event kind=%d", ev.kind)
	}
}

func (r *dbRecorder) Started(startSFEN string, humanColor shogi.Color) {
	r.send(recordEvent{kind: evStarted, startSFEN: startSFEN, color: humanColor})
}

func (r *dbRecorder) Moved(ply int, usi string, by game.Side) {
	r.send(recordEvent{kind: evMoved, ply: ply, usi: usi, by: by})
}

// **Moved 와 같은 채널로 보낸다.** 평가치는 그 수가 들어간 뒤에 와야 하고, 한 채널이면
// 순서가 저절로 지켜진다. 큐를 갈라 두면 평가치가 먼저 도착해 조용히 버려질 수 있다.
func (r *dbRecorder) Evaluated(ply int, senteCp int) {
	r.send(recordEvent{kind: evEvaluated, ply: ply, cp: senteCp})
}

func (r *dbRecorder) Retracted(ply int, usi string, v intervene.Verdict, e explain.Result) {
	r.send(recordEvent{kind: evRetracted, ply: ply, usi: usi, verdict: v, explained: e})
}

func (r *dbRecorder) Finished(status game.Status, winner game.Side) {
	r.send(recordEvent{kind: evFinished, status: status, winner: winner})
}

// run 은 이벤트를 순서대로 쓴다.
//
// **쓰기는 세션 ctx 를 안 쓴다.** 연결이 끊기면 세션 ctx 가 먼저 취소되는데, 그 시점에
// 아직 안 쓴 이벤트가 남아 있으면 전부 실패한다 — 대국이 끝나는 순간이 바로 마지막
// 이벤트가 몰리는 순간이라 그게 제일 아깝다.
func (r *dbRecorder) run(ctx context.Context, st *store.Store, level intervene.Level) {
	write := context.WithoutCancel(ctx)

	var gameID int64
	finished := false

	drain := func(ev recordEvent) {
		switch ev.kind {
		case evStarted:
			color := "b"
			if ev.color == shogi.White {
				color = "w"
			}
			id, err := st.CreateGame(write, nil, color, ev.startSFEN)
			if err != nil {
				log.Printf("game record: create game: %v", err)
				return
			}
			gameID = id

		case evMoved:
			if gameID == 0 {
				return
			}
			if err := st.InsertMove(write, gameID, ev.ply, ev.usi); err != nil {
				log.Printf("game record: move %d: %v", ev.ply, err)
			}

		case evEvaluated:
			if gameID == 0 {
				return
			}
			if err := st.SetMoveEval(write, gameID, ev.ply, ev.cp); err != nil {
				log.Printf("game record: eval %d: %v", ev.ply, err)
			}

		case evRetracted:
			if gameID == 0 {
				return
			}
			if err := st.InsertIntervention(write, gameID, store.Intervention{
				Ply:          ev.ply,
				Kind:         string(ev.verdict.Kind),
				Category:     string(ev.verdict.Category),
				DeltaWin:     ev.verdict.DeltaWin,
				LevelBucket:  levelBucket(level),
				RetractedUSI: ev.usi,
				ExplainTier:  explainTier(ev.explained),
				CostYen:      ev.explained.CostYen,
			}); err != nil {
				log.Printf("game record: intervention %d: %v", ev.ply, err)
			}

		case evFinished:
			if gameID == 0 {
				return
			}
			if err := st.FinishGame(write, gameID, resultOf(ev.status, ev.winner)); err != nil {
				log.Printf("game record: finish: %v", err)
			}
			finished = true
		}
	}

	for {
		select {
		case ev := <-r.events:
			drain(ev)

		case <-ctx.Done():
			// 큐에 남은 것을 마저 쓴다. 대국이 끝나는 순간이 이벤트가 제일 몰리는 때다.
			for {
				select {
				case ev := <-r.events:
					drain(ev)
					continue
				default:
				}
				break
			}
			// 끝나지 않고 연결이 끊긴 판은 그렇게 남긴다 — 빈 result 로 두면
			// 「아직 두는 중인 판」과 구별이 안 된다.
			if gameID != 0 && !finished {
				if err := st.FinishGame(write, gameID, store.ResultAbandoned); err != nil {
					log.Printf("game record: abandon: %v", err)
				}
			}
			return
		}
	}
}

// resultOf 는 대국 결과를 games.result 의 어휘로 옮긴다. **사람 기준**이다.
func resultOf(status game.Status, winner game.Side) store.GameResult {
	switch {
	case status == game.StatusRepetition:
		return store.ResultDraw
	case winner == game.SideHuman:
		return store.ResultWin
	case winner == game.SideEngine:
		return store.ResultLoss
	default:
		return store.ResultAbandoned
	}
}

// explainTier 는 계층을 DB의 어휘로 옮긴다.
//
// **LLM을 안 거쳤으면 nil(=NULL)이다.** `explain.TierTemplate` 을 그대로 -1로 적으면
// 「0=캐시 히트」와 같은 칸에서 음수가 섞이고, 무엇보다 그 둘의 뜻이 정반대다 —
// 히트는 부를 것을 아껴서 0엔이고 NULL은 애초에 부르지 않은 것이다.
func explainTier(e explain.Result) *int {
	if e.Tier < 0 {
		return nil
	}
	t := e.Tier
	return &t
}

func levelBucket(l intervene.Level) string {
	switch l {
	case intervene.Novice:
		return "novice"
	case intervene.Intermediate:
		return "intermediate"
	default:
		return "beginner"
	}
}
