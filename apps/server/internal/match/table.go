package match

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// Recorder 는 한 사람의 몫을 기록한다. **판 하나에 둘이 붙는다** — 대인전 한 판이
// `games` 행 두 개로 남고, 그래서 되짚기·마이페이지·전적 질의가 소유자 조건을 한 줄도
// 안 고치고 그대로 돈다(journal §83).
//
// `game.Recorder` 와 갈라 둔 이유는 **말할 것이 다르기 때문**이다. 저쪽은 평가치·개입·
// 무르기·힌트까지 아홉을 받는데 여기서 벌어지는 일은 셋뿐이고, 같은 인터페이스를 쓰면
// 이 패키지가 「안 부르는 메서드 여섯」을 들고 있게 된다.
//
// 세 메서드 다 **즉시 돌아와야 한다** — 테이블 goroutine 이 부른다.
type Recorder interface {
	// Started 는 대국이 시작될 때 한 번. myColor 는 **이 기록기가 맡은 사람**이 잡은 쪽이다.
	Started(startSFEN string, myColor shogi.Color)
	// Moved 는 확정된 수다. 대인전에는 물러지는 수가 없으므로 둔 수가 곧 확정이다.
	Moved(ply int, usi string)
	// Finished 는 결과다. **이 기록기가 맡은 사람 관점**이라 같은 판에서 둘이 반대로 온다.
	Finished(r Result)
}

// Result 는 한 사람 관점의 결과다. `store.GameResult` 와 같은 어휘를 쓴다 —
// 옮기는 자리를 하나 더 두면 그 자리가 조용히 어긋난다.
type Result string

const (
	ResultWin  Result = "win"
	ResultLoss Result = "loss"
	ResultDraw Result = "draw"
	// ResultAbandoned 는 **승부가 안 난 채로 끝난 판**이다. 두 자리에서 온다:
	// 서버가 내려갔을 때(StatusAborted)와, 한 수도 안 둔 채 시간이 다 됐을 때(StatusExpired).
	//
	// 수를 두고 나서의 시간패는 여기가 아니라 win/loss 다 — 승부가 났기 때문이다.
	ResultAbandoned Result = "abandoned"
)

// Config 는 테이블 하나의 설정이다.
type Config struct {
	// Black·White 는 두 대국자다. **先手·後手가 여기서 확정된다** — 방을 만든 사람이 고른 것이
	// 그대로 들어오고, 대국이 이어지는 동안 안 바뀐다.
	Black, White Player
	// Recorders 는 先手·後手마다 하나씩이다. nil 이면 그쪽을 기록하지 않는다 — 익명 대국이
	// 아니라 **DB 가 없는 배포**를 위한 자리다(엔진 대국과 같은 판단).
	Recorders map[shogi.Color]Recorder
	// TurnLimit 이 0이면 DefaultTurnLimit.
	TurnLimit time.Duration
	// StartSFEN 이 비면 평수 초기 국면.
	StartSFEN string
	// now 는 테스트가 시계를 잡는 자리다. nil 이면 time.Now.
	now func() time.Time
}

type cmdKind int

const (
	cmdPlay cmdKind = iota
	cmdResign
	cmdSnapshot
	cmdSubscribe
	cmdUnsubscribe
	cmdPresence
)

type command struct {
	kind  cmdKind
	color shogi.Color
	usi   string
	sub   chan viewSnapshot
	on    bool
	reply chan result
}

type result struct {
	snap Snapshot
	err  error
}

// viewSnapshot 은 **관점이 아직 안 붙은** 스냅샷이다. 구독자가 자기 쪽으로 편다.
//
// 先手·後手마다 한 벌씩 만들어 뿌리지 않는 이유는 **뿌리는 자리가 하나여야 하기 때문**이다 —
// 둘로 갈면 한쪽에만 보내고 끝나는 경로가 생긴다.
type viewSnapshot struct{ st *snapshotData }

// Table 은 대국 하나다. 모든 메서드는 안전하게 동시 호출할 수 있다 — 하는 일이
// goroutine 에 명령을 보내고 답을 기다리는 것뿐이라는 점이 `game.Session` 과 같다.
type Table struct {
	cmds chan command
	// finished 는 **승패가 정해진 순간** 닫힌다. done 과 갈라 둔 이유는 그 둘의 시각이
	// 다르기 때문이다 — 끝난 판도 한동안 답하므로(finishedGrace) done 은 그만큼 늦게
	// 닫히고, 「振り返り」 링크를 그 뒤에 보내면 사람은 이미 화면을 떠나 있다.
	finished  chan struct{}
	closeOnce sync.Once
	done      chan struct{}
}

// markFinished 는 승패가 정해졌다고 알린다. 두 번 불려도 안전해야 한다 —
// 부르는 자리가 셋이다(착수·投了·시간패).
func (t *Table) markFinished() { t.closeOnce.Do(func() { close(t.finished) }) }

// state 는 테이블 goroutine 만 만진다.
type state struct {
	cfg     Config
	pos     shogi.Position
	prevTo  int
	moves   []recordedMove
	repeats map[string]int
	status  Status
	// winner 는 이긴 **쪽**이다. 무승부·중단이면 안 채운다.
	winner   shogi.Color
	hasWin   bool
	limit    time.Duration
	turnFrom time.Time
	// online 은 先手·後手마다 붙어 있는 연결 수다. 0이면 그쪽이 나가 있다.
	//
	// **판을 안 멈춘다.** 멈추면 지고 있는 쪽이 탭을 닫아 판을 얼릴 수 있고, 그게 이
	// 패키지에 시계가 있는 이유와 정면으로 어긋난다(DefaultTurnLimit).
	online map[shogi.Color]int
	subs   map[chan viewSnapshot]struct{}
	now    func() time.Time
}

type recordedMove struct {
	usi string
	ja  string
	by  shogi.Color
}

// NewTable 은 대국을 시작하고 시계를 건다. ctx 가 끝나면 대국도 끝난다 — **연결이 아니라
// 서버의 수명이다**(방을 들고 있는 Hub 가 준다).
func NewTable(ctx context.Context, cfg Config) (*Table, error) {
	sfen := cfg.StartSFEN
	if sfen == "" {
		sfen = shogi.StartSFEN
	}
	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		return nil, err
	}
	if cfg.TurnLimit <= 0 {
		cfg.TurnLimit = DefaultTurnLimit
	}
	now := cfg.now
	if now == nil {
		now = time.Now
	}

	st := &state{
		cfg:      cfg,
		pos:      pos,
		prevTo:   -1,
		repeats:  map[string]int{},
		status:   StatusPlaying,
		limit:    cfg.TurnLimit,
		turnFrom: now(),
		online:   map[shogi.Color]int{},
		subs:     map[chan viewSnapshot]struct{}{},
		now:      now,
	}
	st.repeats[pos.RepetitionKey()]++

	// **기록 행은 대국이 시작되는 자리에서 만든다.** 첫 수가 아니라 여기인 이유는 행이 **첫
	// `Moved` 보다 먼저** 있어야 하기 때문이다 — 기록기가 행 없이 온 수를 그냥 버린다
	// (server/recorder.go 의 `gameID == 0`).
	//
	// 값은 한 수도 안 둔 판에도 행 둘이 남는다는 것이다. **어느 목록에도 안 뜬다** —
	// 세는 질의가 전부 `EXISTS (game_moves)` 로 거른다.
	for c, rec := range cfg.Recorders {
		if rec != nil {
			rec.Started(sfen, c)
		}
	}

	t := &Table{
		cmds:     make(chan command),
		finished: make(chan struct{}),
		done:     make(chan struct{}),
	}
	go t.run(ctx, st)
	return t, nil
}

// finishedGrace 는 **판이 끝난 뒤에도 테이블이 답하는 시간**이다.
//
// 끝나는 그 순간에 문을 닫으면, 하필 그때 끊겼다 다시 붙은 사람이 결과 대신 오류를
// 본다 — 投了를 받은 쪽이 새로고침하는 것은 드문 일이 아니다.
//
// **방보다 길어야 한다**(FinishedTTL). 같게 두면 그 둘이 닫히는 순서가 정해지지 않아,
// 「방은 아직 열려 있는데 테이블이 이미 닫힌」 창이 생긴다 — 그때 들어온 사람은 404도
// 결과도 아닌 오류를 받는다. 길게 두면 그 사람은 언제나 둘 중 하나를 받는다:
// 방이 살아 있으면 결과를, 방이 걷혔으면 404를.
const finishedGrace = FinishedTTL + time.Minute

// run 은 명령과 시계를 한 자리에서 받는다. **시계가 select 안에 있는 것이 요점이다** —
// 밖에서 상태를 만지는 timer 를 두면 「시간이 다 됐다」와 「방금 뒀다」가 경합한다.
func (t *Table) run(ctx context.Context, st *state) {
	defer close(t.done)
	defer st.closeSubs()

	timer := time.NewTimer(st.limit)
	defer timer.Stop()

	// grace 는 판이 끝난 뒤에만 걸린다. 그 전에는 nil 채널이라 select 에서 안 골린다 —
	// Go 에서 nil 채널의 수신은 영원히 안 준비되고, 그것이 「아직 그 갈래가 없다」를
	// 표현하는 가장 싼 방법이다.
	var graceC <-chan time.Time
	grace := time.NewTimer(finishedGrace)
	stopTimer(grace)
	defer grace.Stop()

	// 끝난 판은 **결과를 뿌린 뒤에도 계속 답한다.** 문을 닫는 것은 grace 하나다.
	settle := func() {
		stopTimer(timer)
		t.markFinished()
		if graceC == nil {
			grace.Reset(finishedGrace)
			graceC = grace.C
		}
	}

	for {
		select {
		case c := <-t.cmds:
			st.handle(c)
			if st.status == StatusPlaying {
				resetTimer(timer, st.leftForTurn())
			} else {
				settle()
			}

		case <-timer.C:
			if st.status != StatusPlaying {
				continue
			}
			// **남은 시간을 다시 본다.** 착수로 timer 를 다시 걸기 직전에 이 case 가
			// 준비될 수 있고(Go 가 둘 중 하나를 무작위로 고른다), 그때 시간패로 끝내면
			// 방금 제때 둔 사람이 진다.
			if st.leftForTurn() > 0 {
				resetTimer(timer, st.leftForTurn())
				continue
			}
			st.timeout()
			settle()

		case <-graceC:
			return

		case <-ctx.Done():
			// **서버가 내려간다.** 이미 끝난 판에는 아무것도 안 한다 — 승패를 두 번 적으면
			// 기록에서 결과가 뒤집힌다.
			if st.status == StatusPlaying {
				// 승패를 만들지 않는다. 두 사람 다 잘못한 것이 없다.
				st.abort()
			}
			t.markFinished()
			return
		}
	}
}

func resetTimer(t *time.Timer, d time.Duration) {
	stopTimer(t)
	if d < 0 {
		d = 0
	}
	t.Reset(d)
}

func stopTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
}

// leftForTurn 은 지금 수번에 남은 시간이다.
func (st *state) leftForTurn() time.Duration {
	if st.status != StatusPlaying {
		return 0
	}
	return st.limit - st.now().Sub(st.turnFrom)
}

func (st *state) handle(c command) {
	switch c.kind {
	case cmdPlay:
		snap, err := st.play(c.color, c.usi)
		c.reply <- result{snap: snap, err: err}

	case cmdResign:
		snap, err := st.resign(c.color)
		c.reply <- result{snap: snap, err: err}

	case cmdSnapshot:
		c.reply <- result{snap: st.snapshot().for_(c.color)}

	case cmdSubscribe:
		st.subs[c.sub] = struct{}{}
		// 붙자마자 지금 자리를 하나 준다. 없으면 다음 착수까지 빈 화면이다.
		notify(c.sub, viewSnapshot{st: st.snapshot()})
		c.reply <- result{}

	case cmdUnsubscribe:
		if _, ok := st.subs[c.sub]; ok {
			delete(st.subs, c.sub)
			close(c.sub)
		}
		c.reply <- result{}

	case cmdPresence:
		if c.on {
			st.online[c.color]++
		} else if st.online[c.color] > 0 {
			st.online[c.color]--
		}
		// 상대에게 「들어왔다/나갔다」가 보여야 한다. 판은 안 움직인다.
		st.broadcast()
		c.reply <- result{}
	}
}

func (st *state) play(by shogi.Color, usi string) (Snapshot, error) {
	if st.status != StatusPlaying {
		return st.snapshot().for_(by), ErrFinished
	}
	if st.pos.Turn != by {
		return st.snapshot().for_(by), ErrNotYourTurn
	}
	m, err := shogi.ParseUSIMove(usi)
	if err != nil {
		return st.snapshot().for_(by), err
	}
	if err := st.pos.ValidateMove(m); err != nil {
		return st.snapshot().for_(by), err
	}

	ja := st.pos.MoveJa(m, st.prevTo)
	st.pos = st.pos.Apply(m)
	st.prevTo = int(m.To)
	st.moves = append(st.moves, recordedMove{usi: m.USI(), ja: ja, by: by})
	st.repeats[st.pos.RepetitionKey()]++
	// **시계는 착수와 같은 자리에서 다시 시작한다.** 갈라 두면 그 사이가 어느 쪽 시간도
	// 아닌 구간이 되고, 느린 DB 쓰기 하나가 상대의 시간을 먹는다.
	st.turnFrom = st.now()

	if rec := st.cfg.Recorders[by]; rec != nil {
		rec.Moved(len(st.moves), m.USI())
	}
	// **상대 쪽 기록기도 같은 수를 받는다.** 행이 둘이고 기보는 한 벌이라, 한쪽에만
	// 넣으면 그 판이 두 사람에게 다른 판으로 남는다.
	if rec := st.cfg.Recorders[by.Other()]; rec != nil {
		rec.Moved(len(st.moves), m.USI())
	}

	switch {
	case st.pos.NoLegalMoves():
		// 쇼기에서는 手詰まり도 패배다 — 어느 쪽이든 수번 측이 진다(game.state.apply 와 같다).
		status := StatusStalemate
		if st.pos.InCheck(st.pos.Turn) {
			status = StatusCheckmate
		}
		st.finish(status, st.pos.Turn.Other(), true)
	case st.repeats[st.pos.RepetitionKey()] >= 4:
		st.finish(StatusRepetition, shogi.Black, false)
	}

	st.broadcast()
	return st.snapshot().for_(by), nil
}

func (st *state) resign(by shogi.Color) (Snapshot, error) {
	if st.status != StatusPlaying {
		return st.snapshot().for_(by), ErrFinished
	}
	st.finish(StatusResigned, by.Other(), true)
	st.broadcast()
	return st.snapshot().for_(by), nil
}

// timeout 은 수번 쪽의 시간이 다 됐을 때다. **대개 승패가 난다** — 중단과 갈라 두는 자리다.
//
// **한 수도 안 뒀으면 예외다.** 방 주인이 링크를 보내고 탭을 열어 둔 채 자리를 뜨는 것이
// 흔한데, 그때 승패를 적으면 **0手짜리 판**이 두 사람의 전적에 win/loss 로 남고 이긴 쪽의
// 「振り返り」 링크가 빈 판을 연다. 아무도 안 뒀으면 판이 없었던 것이다.
func (st *state) timeout() {
	if len(st.moves) == 0 {
		st.finish(StatusExpired, shogi.Black, false)
		st.broadcast()
		return
	}
	st.finish(StatusTimeout, st.pos.Turn.Other(), true)
	st.broadcast()
}

// abort 는 승패 없이 판을 접는다. 서버가 내려갈 때뿐이다.
func (st *state) abort() {
	st.finish(StatusAborted, shogi.Black, false)
	st.broadcast()
}

func (st *state) finish(status Status, winner shogi.Color, hasWin bool) {
	st.status, st.winner, st.hasWin = status, winner, hasWin
	for c, rec := range st.cfg.Recorders {
		if rec == nil {
			continue
		}
		rec.Finished(st.resultFor(c))
	}
}

// resultFor 는 그쪽의 관점 결과다.
func (st *state) resultFor(c shogi.Color) Result {
	switch {
	case st.status == StatusRepetition:
		return ResultDraw
	case !st.hasWin:
		// 승부가 안 났다 — 서버가 내려갔거나 한 수도 안 둔 채 시간이 다 됐다.
		// **되짚기가 결과 있는 판만 열기 때문에**(§51) 어느 쪽이든 목록에 안 뜬다.
		return ResultAbandoned
	case st.winner == c:
		return ResultWin
	default:
		return ResultLoss
	}
}

func (st *state) broadcast() {
	snap := st.snapshot()
	for ch := range st.subs {
		notify(ch, viewSnapshot{st: snap})
	}
}

func (st *state) closeSubs() {
	for ch := range st.subs {
		delete(st.subs, ch)
		close(ch)
	}
}

// notify 는 막히지 않게 보낸다. 느린 구독자가 테이블을 붙들면 **상대의 시계가 그만큼
// 흐른다** — 엔진 대국에서 같은 일이 「상대가 늦게 둔다」로 끝나는 것과 값이 다르다.
func notify(ch chan viewSnapshot, snap viewSnapshot) {
	select {
	case ch <- snap:
	default:
		// 앞의 것을 버리고 최신을 넣는다. 스냅샷은 언제나 통째라 중간을 놓쳐도 맞다.
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- snap:
		default:
		}
	}
}

func (t *Table) send(ctx context.Context, c command) (Snapshot, error) {
	c.reply = make(chan result, 1)
	select {
	case t.cmds <- c:
	case <-t.done:
		return Snapshot{}, ErrClosed
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
	select {
	case r := <-c.reply:
		return r.snap, r.err
	case <-t.done:
		return Snapshot{}, ErrClosed
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
}

// Play 는 한 수 둔다. by 는 **서버가 쿠키에서 정한 쪽**이다 — 클라이언트가 보내지 않는다.
func (t *Table) Play(ctx context.Context, by shogi.Color, usi string) (Snapshot, error) {
	return t.send(ctx, command{kind: cmdPlay, color: by, usi: usi})
}

// Resign 은 投了다.
func (t *Table) Resign(ctx context.Context, by shogi.Color) (Snapshot, error) {
	return t.send(ctx, command{kind: cmdResign, color: by})
}

// Snapshot 은 그쪽이 보는 지금 자리다.
func (t *Table) Snapshot(ctx context.Context, by shogi.Color) (Snapshot, error) {
	return t.send(ctx, command{kind: cmdSnapshot, color: by})
}

// Subscribe 는 그쪽의 화면에 붙는다. 돌려주는 함수를 부르면 떨어진다.
//
// **접속 표시가 여기 붙어 있다.** 구독이 곧 「그 사람이 화면을 보고 있다」이고, 갈라 두면
// 끊긴 연결이 붙어 있는 것으로 남는 경로가 생긴다.
func (t *Table) Subscribe(ctx context.Context, by shogi.Color) (<-chan Snapshot, func(), error) {
	raw := make(chan viewSnapshot, 1)
	if _, err := t.send(ctx, command{kind: cmdSubscribe, sub: raw}); err != nil {
		return nil, nil, err
	}
	if _, err := t.send(ctx, command{kind: cmdPresence, color: by, on: true}); err != nil {
		// **구독을 되돌린다.** 여기서 그냥 나가면 `raw` 가 구독 목록에 남은 채 아무도
		// 안 읽고, 돌려줄 정리 함수도 없다 — 테이블이 사는 내내(긴 판이면 몇 시간)
		// 착수마다 그 채널에 헛되이 보내게 된다.
		off, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		if _, uerr := t.send(off, command{kind: cmdUnsubscribe, sub: raw}); uerr != nil && !errorsIsClosed(uerr) {
			log.Printf("match: cannot undo a half-made subscription: %v", uerr)
		}
		return nil, nil, err
	}

	// 관점을 붙이는 자리다. 테이블은 先手·後手를 모르는 스냅샷 하나만 뿌리고(viewSnapshot),
	// 「너」와 「상대」로 펴는 것은 구독자마다 한다.
	out := make(chan Snapshot, 1)
	go func() {
		defer close(out)
		for v := range raw {
			select {
			case out <- v.st.for_(by):
			case <-ctx.Done():
				return
			}
		}
	}()

	var once sync.Once
	return out, func() {
		once.Do(func() {
			// **정리에 ctx 를 안 쓴다.** 떨어지는 이유가 대개 그 ctx 의 종료라, 그것을
			// 그대로 넘기면 구독이 남고 그 사람은 영영 접속 중으로 보인다.
			off, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			if _, err := t.send(off, command{kind: cmdPresence, color: by, on: false}); err != nil &&
				!errorsIsClosed(err) {
				log.Printf("match: presence off: %v", err)
			}
			if _, err := t.send(off, command{kind: cmdUnsubscribe, sub: raw}); err != nil &&
				!errorsIsClosed(err) {
				log.Printf("match: unsubscribe: %v", err)
			}
		})
	}, nil
}

// Finished 는 **승패가 정해지면** 닫힌다. 그 뒤로도 테이블은 한동안 답한다(finishedGrace).
func (t *Table) Finished() <-chan struct{} { return t.finished }

// Done 은 테이블이 **완전히 닫히면** 닫힌다. 그 뒤로는 모든 명령이 ErrClosed 다.
func (t *Table) Done() <-chan struct{} { return t.done }

// errorsIsClosed 는 「테이블이 이미 닫혔다」를 조용히 넘기는 자리다 — 판이 끝나는 순간과
// 연결이 떨어지는 순간이 겹치는 것은 흔한 일이고, 그때 로그를 남기면 정상 종료마다 줄이 쌓인다.
func errorsIsClosed(err error) bool { return err == ErrClosed }
