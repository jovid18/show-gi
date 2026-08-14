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
	// done 은 **마지막 이벤트까지 쓴 뒤** 대국 id를 한 번 실어 보낸다. 버퍼가 1이라 받는
	// 쪽이 없어도 막히지 않는다.
	//
	// 총평이 이것을 기다린다(summary.go). 기록이 비동기라, 끝난 스냅샷을 보고 곧바로 DB를
	// 읽으면 **마지막 수와 그 수의 개입이 아직 없다** — 하필 총평이 가장 말하고 싶은 수다.
	// 뮤텍스로 id만 여는 길도 있었는데, 그러면 「id가 있다」와 「기록이 다 됐다」가 갈려서
	// 부르는 쪽이 둘을 따로 기다려야 한다.
	done chan int64
}

type recordKind int

const (
	evStarted recordKind = iota
	evMoved
	evEvaluated
	evRetracted
	evUndone
	evNamed
	evHinted
	evHintTaken
	evFinished
)

type recordEvent struct {
	kind      recordKind
	startSFEN string
	color     shogi.Color
	ply       int
	usi       string
	// stage 는 `evHinted` 의 단계(1|2)이고, taken 은 `evHintTaken` 의 답이다.
	stage int
	taken bool
	// code 는 `evNamed` 의 태그 코드이자 힌트 두 이벤트의 국면 키다. **`usi` 를 돌려쓰지 않는다** — 저 칸은 수이고
	// 이건 이름이라, 같은 칸에 넣으면 이벤트마다 뜻이 달라지는 칸이 하나 생긴다.
	code      string
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

// recordTarget 은 이 판을 어느 행에 남기나다. **연결이 열릴 때 한 번 정해지고 그 판
// 내내 안 바뀐다**: 두는 중에 다른 탭에서 로그아웃해도 이 판은 시작할 때의 주인으로 끝난다.
type recordTarget struct {
	// userID 는 nil일 수 있다 — 로그인 전 대국이다(002_anonymous_games.sql).
	userID *int64
	// openingID 는 사람이 고른 상대의 진형이다(internal/book). 새 판을 열 때만 쓴다.
	openingID string
	// resumeID 가 0이 아니면 **새 판을 열지 않고 그 행에 이어 적는다.** 되열기는 점유가
	// 이미 끝냈다(store.ClaimGameForResume).
	resumeID int64
}

// newDBRecorder 는 대국 하나를 기록할 Recorder 를 만든다. ctx 가 끝나면 정리한다.
func newDBRecorder(ctx context.Context, st *store.Store, level intervene.Level, target recordTarget) *dbRecorder {
	r := &dbRecorder{events: make(chan recordEvent, recordQueue), done: make(chan int64, 1)}
	go r.run(ctx, st, level, target)
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

// **Moved 와 같은 채널로 보낸다.** 무르기는 그 手数까지의 기보를 지우므로, 지우기가
// 아직 안 쓴 착수를 앞질러 가면 지워야 할 수가 그 뒤에 들어와 되살아난다.
func (r *dbRecorder) Undone(ply int, usi string) {
	r.send(recordEvent{kind: evUndone, ply: ply, usi: usi})
}

// Named 는 사람이 처음 짜낸 이름 하나다. **한 판에 코드마다 한 번**이라 세션이 이미
// 걸러서 보낸다(game.recordStyleTags) — 여기서 또 세지 않는다.
func (r *dbRecorder) Named(code string) {
	r.send(recordEvent{kind: evNamed, code: code})
}

// Hinted 는 사람이 불러서 받은 힌트 한 번이다. 개입과 갈라 두는 이유는 010_game_hints.sql.
func (r *dbRecorder) Hinted(ply int, key string, stage int, bestUSI string) {
	r.send(recordEvent{kind: evHinted, ply: ply, code: key, stage: stage, usi: bestUSI})
}

func (r *dbRecorder) HintTaken(key string, taken bool) {
	r.send(recordEvent{kind: evHintTaken, code: key, taken: taken})
}

func (r *dbRecorder) Finished(status game.Status, winner game.Side) {
	r.send(recordEvent{kind: evFinished, status: status, winner: winner})
}

// run 은 이벤트를 순서대로 쓴다.
//
// **쓰기는 세션 ctx 를 안 쓴다.** 연결이 끊기면 세션 ctx 가 먼저 취소되는데, 그 시점에
// 아직 안 쓴 이벤트가 남아 있으면 전부 실패한다 — 대국이 끝나는 순간이 바로 마지막
// 이벤트가 몰리는 순간이라 그게 제일 아깝다.
func (r *dbRecorder) run(ctx context.Context, st *store.Store, level intervene.Level, target recordTarget) {
	write := context.WithoutCancel(ctx)

	// **이어하는 판은 시작부터 id 를 안다.** 점유가 그 행을 이미 되열어 놨으므로
	// (store.ClaimGameForResume), 세션이 서지 못하고 끝나도 아래 ctx 취소 경로가 다시
	// abandoned 로 닫는다 — 되열린 채로 남으면 그 판은 되짚기에도(§51) 이어하기에도
	// 안 걸리는 유령이 된다.
	gameID := target.resumeID
	finished := false

	drain := func(ev recordEvent) {
		switch ev.kind {
		case evStarted:
			// **이어하는 판은 행을 새로 만들지 않는다.** 만들면 한 대국의 기보와 개입이
			// 두 행으로 갈리고, 「그 국면이 그 사람에게 얼마나 어려웠나」가 흩어진다(§46).
			// 시작 국면도 진형도 그 행에 있는 그대로 둔다 — 다시 적으면 원본을 덮는다.
			if target.resumeID != 0 {
				return
			}
			color := "b"
			if ev.color == shogi.White {
				color = "w"
			}
			id, err := st.CreateGame(write, target.userID, color, ev.startSFEN, target.openingID)
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
				BestCp:       ev.verdict.BestCp,
				AfterCp:      ev.verdict.AfterCp,
				LevelBucket:  levelBucket(level),
				RetractedUSI: ev.usi,
				ExplainTier:  explainTier(ev.explained),
				CostYen:      ev.explained.CostYen,
			}); err != nil {
				log.Printf("game record: intervention %d: %v", ev.ply, err)
			}

		case evUndone:
			if gameID == 0 {
				return
			}
			if err := st.RecordUndo(write, gameID, ev.ply, ev.usi); err != nil {
				log.Printf("game record: undo %d: %v", ev.ply, err)
			}

		case evNamed:
			if gameID == 0 {
				return
			}
			if err := st.AddStyleTag(write, gameID, ev.code); err != nil {
				log.Printf("game record: style tag %q: %v", ev.code, err)
			}

		case evHinted:
			if gameID == 0 {
				return
			}
			if err := st.RecordHint(write, gameID, ev.ply, ev.code, ev.stage, ev.usi); err != nil {
				log.Printf("game record: hint %d: %v", ev.ply, err)
			}

		case evHintTaken:
			if gameID == 0 {
				return
			}
			if err := st.MarkHintTaken(write, gameID, ev.code, ev.taken); err != nil {
				log.Printf("game record: hint taken: %v", err)
			}

		case evFinished:
			if gameID == 0 {
				return
			}
			if err := st.FinishGame(write, gameID, resultOf(ev.status, ev.winner)); err != nil {
				log.Printf("game record: finish: %v", err)
			}
			finished = true
			// 여기까지 왔으면 이 판의 기록은 전부 들어갔다 — 이벤트가 한 채널로 순서대로
			// 오므로(Evaluated 주석) 뒤에 남은 것이 없다.
			select {
			case r.done <- gameID:
			default:
			}
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
