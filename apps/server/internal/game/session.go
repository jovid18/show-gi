// Package game 은 대국 세션의 상태머신이다.
//
// **상태는 goroutine 하나가 소유한다.** 입력은 채널로 모으고, 출력은 스냅샷을 뿌린다.
// 롤백(D3 개입)이 들어오는 순간 상태 변경 순서가 곧 제품 정합성이 되므로, mutex로
// 얼버무리면 "물러진 수가 기보에 남는" 종류의 버그가 재현 불가능한 형태로 나온다.
//
// 그래서 여기에는 잠금이 없다. 세션 goroutine 밖에서 상태를 읽는 길도 없다 —
// 스냅샷을 요청하는 것도 명령이다.
package game

import (
	"context"
	"errors"
	"log"
	"sync"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/tag"
)

// Analyst 는 착수 한 수를 판정하는 데 필요한 숫자를 구해 온다.
//
// 엔진을 직접 부르지 않고 인터페이스로 두는 이유는 **판정과 탐색을 갈라두기 위해서**다.
// 세션은 "이 수가 블런더인가"만 알면 되고, 그 답을 어떻게 구했는지는 모른다.
type Analyst interface {
	// Judge 는 startSFEN + moves 로 도달한 국면에서 **마지막 한 수**를 판정한다.
	// 판정에 필요한 탐색이 오래 걸릴 수 있으므로 세션 goroutine 밖에서 불린다.
	Judge(ctx context.Context, startSFEN string, moves []string, ply int) (Judgement, error)
}

// Opponent 는 상대(컴퓨터)의 수를 고른다.
//
// 구현이 둘이다 — NewEngineOpponent(엔진 최선수 그대로)와 NewAdaptiveOpponent(밴드 제어,
// 프로덕션이 쓰는 쪽). 상대의 강함이 바뀌는 자리가 여기뿐이라 세션 상태머신은 둘을 모른다.
type Opponent interface {
	Choose(ctx context.Context, startSFEN string, moves []string) (string, error)
}

// Config 는 세션 하나의 설정이다.
type Config struct {
	Opponent Opponent
	// Analyst 가 nil이면 개입하지 않는다. 대국은 그대로 된다.
	Analyst Analyst
	// Mate 가 nil이면 詰み 게이지가 꺼진 채로 대국한다.
	//
	// **Analyst 와 같은 solver 를 받지만 자리가 다르다.** 저쪽은 방금 둔 수를 판정하려고
	// 착수 **전** 국면을 묻고, 이쪽은 지금 사람 차례인 **현재** 국면을 묻는다. 같은 질문을
	// 한 수 늦게 하는 것이라 판정 결과를 게이지로 돌려쓸 수 없다.
	Mate MateSearcher
	// TesujiHint 가 nil이면 手筋 제안형 힌트가 꺼진 채로 대국한다. 囲い·전법 쪽 힌트는
	// 엔진을 안 쓰므로 그대로 뜬다(computeTagHints).
	//
	// **Opponent 와 같은 풀을 받지만 자리가 다르다** — `Mate` 가 `Analyst` 와 갈리는 것과
	// 같은 이유다. 상대는 **자기가 둘 수**를 고르려고 지금 국면을 묻고, 이쪽은 **사람이
	// 둘 수 있는 수 하나하나**를 둬 본 뒤의 국면을 묻는다. 묻는 국면이 아예 다르므로
	// 상대의 탐색 결과를 돌려쓸 수 없다.
	TesujiHint MultiSearcher
	// Recorder 가 nil이면 기록하지 않는다. 대국은 그대로 된다.
	Recorder Recorder
	// Explainer 가 nil이면 결정적 문구가 그대로 나간다 — `explain.Render` 와 같은 문장이다.
	//
	// **비어 있어도 카드가 완성된다**는 것이 이 자리의 규약이다. LLM은 문장의 품질을 올리는
	// 층이지 제품의 부품이 아니고, 그래서 키가 없어도 라우터가 죽어도 대국이 그대로 선다.
	Explainer explain.Explainer
	// ObservePlies 는 개입하지 않는 초반 구간이다. **기본값은 0 — 첫 수부터 판정한다.**
	//
	// 오프닝의 다양성은 수 번호가 아니라 임계치가 지킨다 — 전법 선택은 50~200cp(Δ 2~8%p)라
	// 어느 레벨도 안 걸리고, 銀 이상을 공짜로 주면 Δ 34%p라 입문에서도 걸린다(01-core.md §2).
	ObservePlies int
	// HumanColor 는 사람이 잡는 쪽. 기본은 先手(Black).
	HumanColor shogi.Color
	// StartSFEN 이 비면 평수 초기 국면.
	StartSFEN string
}

// ErrClosed 는 끝난 세션에 명령을 보냈을 때 나온다.
var ErrClosed = errors.New("game: session closed")

// ErrNotYourTurn 은 사람 차례가 아닐 때 착수를 보냈을 때 나온다.
var ErrNotYourTurn = errors.New("game: not your turn")

// ErrFinished 는 이미 끝난 대국에 착수를 보냈을 때 나온다.
var ErrFinished = errors.New("game: game already finished")

type cmdKind int

const (
	cmdMove cmdKind = iota
	cmdResign
	cmdSnapshot
	cmdSubscribe
	cmdUnsubscribe
)

type command struct {
	kind  cmdKind
	usi   string
	sub   chan Snapshot
	reply chan result
}

type result struct {
	snap Snapshot
	err  error
}

type engineResult struct {
	usi string
	err error
}

type mateResult struct {
	gen   int
	plies int // 詰み 手数. 0이면 못 찾았다
	err   error
}

// tesujiHintResult 는 게이트를 통과한 手筋 후보다. opts 는 계단이 짚을 수를 갖고 있고,
// 화면에 나가는 것은 이름뿐이다(tesujiHintTags).
type tesujiHintResult struct {
	gen     int
	opts    []TesujiOption
	dropped int // 상한에 걸려 못 물어본 후보 수
	err     error
}

type judgeResult struct {
	gen       int
	judgement Judgement
	move      Move
	// explanation 은 카드에 나갈 문장이다. 개입이 걸렸을 때만 채워진다.
	explanation explain.Result
	err         error
}

// Session 은 대국 하나다. 모든 메서드는 안전하게 동시 호출할 수 있다 —
// 실제로 하는 일은 세션 goroutine에 명령을 보내고 답을 기다리는 것뿐이다.
type Session struct {
	cmds      chan command
	done      chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
}

// 내부 상태. 세션 goroutine만 만진다.
type state struct {
	cfg     Config
	pos     shogi.Position
	start   string
	moves   []Move
	usis    []string
	prevTo  int
	repeats map[string]int
	status  Status
	winner  Side

	thinking bool
	// searchGen 은 국면이 바뀔 때마다 오른다. 탐색을 띄울 때의 값을 pendingGen 에
	// 적어두고, 결과가 돌아왔을 때 둘이 다르면 버린다 — 그 사이에 국면이 움직였다는 뜻이다.
	// D3에서 롤백이 들어오면 이게 실제로 값을 한다.
	searchGen  int
	pendingGen int

	// 판정도 세션 goroutine 밖에서 돈다(탐색과 같은 이유). 늦게 온 결과는 세대로 버린다.
	judging      bool
	judgeGen     int
	intervention *Intervention

	// stuck 은 **같은 국면에서 연속으로 물러진 횟수**다. 통과하는 수를 두면 0으로 돌아가고,
	// 되무르기는 국면을 그대로 되돌리므로 「연속」이 곧 「같은 국면」이 된다.
	stuck int
	// hint 는 그 횟수에 열린 안내다. intervention 과 수명이 같지만 뜻이 다르다 —
	// intervention 은 **방금 무엇을 했나**이고 이쪽은 **지금 무엇을 할까**다.
	hint *Hint

	// 詰み 게이지도 세션 goroutine 밖에서 돈다(탐색·판정과 같은 이유).
	//
	// **mateHeat 와 mateGen 을 함께 본다.** 국면이 움직이면(searchGen) 그 세기는 그 자리에서
	// 무효이고, 스냅샷이 둘을 대조해 판단한다 — 지우는 코드를 착수·롤백·종료에 흩어 두면
	// 그중 하나를 빠뜨렸을 때 낡은 불꽃이 새 국면에 남는다.
	gauging  bool
	gaugeGen int
	mateHeat int
	mateGen  int

	// tesuji 는 **엔진 게이트를 통과한** 手筋 이름들이고, tesujiGen 은 그것을 구한 국면이다.
	//
	// 게이지와 같은 규약이다 — 국면이 움직이면 그 자리에서 무효이므로 스냅샷이 둘을
	// 대조한다. 여기는 이유가 하나 더 있다: 이름을 통과시킨 것은 **그 국면의 평가치**라,
	// 다음 국면까지 들고 가면 엔진에게 묻지 않은 형태에 이름을 붙이는 것이 된다.
	tesuji    []tag.Tag
	tesujiGen int

	// 제안형 힌트. 빈도 상한과 쿨다운을 여기서 잡는다(01-core.md §7.1).
	tagHints       []tag.Tag
	tagHintGen     int
	tagHintCount   int
	tagHintLastPly int

	// 手筋 쪽 제안형 힌트. **예산(카운터)만 따로 센다** — 한 예산이면 囲い가 먼저 다 써서
	// 手筋이 못 뜬다(06-status.md §42). **이름이 아니라 후보를 들고 있는다** — 계단 ②③이
	// 짚을 수가 여기 있고, 이름은 언제든 후보에서 편다(tesujiHintTags).
	tesujiOpts        []TesujiOption
	tesujiHintGen     int
	tesujiHinting     bool
	tesujiHintCount   int
	tesujiHintLastPly int

	// 물러질 수 있으므로 착수 직전 국면을 들고 있는다. Position 이 값 타입이라 복사면 끝이다.
	prevPos    shogi.Position
	prevPrevTo int

	subs map[chan Snapshot]struct{}
}

// New 는 세션을 시작한다. ctx가 끝나면 세션도 끝난다.
func New(ctx context.Context, cfg Config) (*Session, error) {
	sfen := cfg.StartSFEN
	if sfen == "" {
		sfen = shogi.StartSFEN
	}
	pos, err := shogi.ParseSFEN(sfen)
	if err != nil {
		return nil, err
	}
	if cfg.Opponent == nil {
		return nil, errors.New("game: Opponent is required")
	}

	s := &Session{
		cmds:   make(chan command),
		done:   make(chan struct{}),
		closed: make(chan struct{}),
	}
	st := &state{
		cfg:     cfg,
		pos:     pos,
		start:   sfen,
		prevTo:  -1,
		repeats: map[string]int{},
		status:  StatusPlaying,
		subs:    map[chan Snapshot]struct{}{},
	}
	st.repeats[pos.RepetitionKey()]++

	go s.run(ctx, st)
	return s, nil
}

func (s *Session) run(ctx context.Context, st *state) {
	defer close(s.closed)

	engineDone := make(chan engineResult, 1)
	judgeDone := make(chan judgeResult, 1)
	gaugeDone := make(chan mateResult, 1)
	tesujiDone := make(chan tesujiHintResult, 1)

	// 기록도 세션 goroutine 안에서 시작한다 — 상태를 만지는 순서와 같은 줄에 둔다.
	if st.cfg.Recorder != nil {
		st.cfg.Recorder.Started(st.start, st.cfg.HumanColor)
	}

	// 엔진이 선수면 시작하자마자 생각한다. 사람이 선수면 게이지가 대신 걸린다 —
	// 둘은 정확히 반대 조건이라 언제나 하나만 돈다.
	st.computeTagHints()
	st.maybeTesujiHint(ctx, tesujiDone)
	st.maybeThink(ctx, engineDone)
	st.maybeGauge(ctx, gaugeDone)

	for {
		select {
		case <-ctx.Done():
			st.closeSubs()
			return

		case <-s.done:
			st.closeSubs()
			return

		case c := <-s.cmds:
			st.handle(ctx, c, engineDone, judgeDone, gaugeDone)

		case r := <-engineDone:
			st.applyEngineMove(ctx, r, engineDone, gaugeDone, tesujiDone)

		case r := <-judgeDone:
			st.applyVerdict(ctx, r, engineDone, gaugeDone, tesujiDone)

		case r := <-gaugeDone:
			st.applyMateHeat(r)

		case r := <-tesujiDone:
			st.applyTesujiHint(r)
		}
	}
}

func (st *state) handle(ctx context.Context, c command, engineDone chan engineResult, judgeDone chan judgeResult, gaugeDone chan mateResult) {
	switch c.kind {
	case cmdSnapshot:
		c.reply <- result{snap: st.snapshot()}

	case cmdSubscribe:
		st.subs[c.sub] = struct{}{}
		notify(c.sub, st.snapshot())
		c.reply <- result{snap: st.snapshot()}

	case cmdUnsubscribe:
		if _, ok := st.subs[c.sub]; ok {
			delete(st.subs, c.sub)
			close(c.sub)
		}
		c.reply <- result{}

	case cmdResign:
		if st.status != StatusPlaying {
			c.reply <- result{snap: st.snapshot(), err: ErrFinished}
			return
		}
		st.finish(StatusResigned, SideEngine)
		c.reply <- result{snap: st.snapshot()}
		st.broadcast()

	case cmdMove:
		snap, err := st.playHuman(ctx, c.usi, engineDone, judgeDone, gaugeDone)
		c.reply <- result{snap: snap, err: err}
		if err == nil {
			st.broadcast()
		}
	}
}

func (st *state) playHuman(ctx context.Context, usi string, engineDone chan engineResult, judgeDone chan judgeResult, gaugeDone chan mateResult) (Snapshot, error) {
	if st.status != StatusPlaying {
		return st.snapshot(), ErrFinished
	}
	if st.judging {
		return st.snapshot(), ErrNotYourTurn // 판정 중에는 다음 수를 못 둔다
	}
	if st.pos.Turn != st.cfg.HumanColor {
		return st.snapshot(), ErrNotYourTurn
	}
	m, err := shogi.ParseUSIMove(usi)
	if err != nil {
		return st.snapshot(), err
	}
	if err := st.pos.ValidateMove(m); err != nil {
		return st.snapshot(), err
	}

	// 새 수를 두면 직전 개입은 지운다 — 화면에 남아 있으면 방금 둔 수를 가리키는 것처럼 보인다.
	// 힌트도 같이 내린다. 또 물러지면 한 단계 올라간 것이 새로 뜬다.
	st.intervention, st.hint = nil, nil

	// 물러질 수 있으니 착수 전 국면을 들고 있는다.
	st.prevPos, st.prevPrevTo = st.pos, st.prevTo

	st.apply(m, SideHuman)

	// 판정할 것이 있으면 엔진을 부르기 전에 먼저 묻는다. **롤백이 있는 이상
	// 상대 수를 먼저 두면 되돌릴 것이 두 개가 된다.**
	if st.startJudging(ctx, judgeDone) {
		return st.snapshot(), nil
	}
	// 판정하지 않으면 그 자리에서 확정이다.
	st.recordLastMove()
	st.maybeThink(ctx, engineDone)
	st.maybeGauge(ctx, gaugeDone)
	return st.snapshot(), nil
}

// startJudging 은 방금 둔 사람의 수를 판정시킨다. 판정에 들어갔으면 true.
//
// 탐색과 같은 이유로 세션 goroutine 밖에서 돈다 — 여기서 기다리면 판정하는 동안
// 투료도 스냅샷도 못 받는다.
func (st *state) startJudging(ctx context.Context, judgeDone chan judgeResult) bool {
	if st.cfg.Analyst == nil || st.status != StatusPlaying {
		return false
	}
	if len(st.usis) <= st.cfg.ObservePlies {
		return false
	}
	st.judging = true
	st.judgeGen = st.searchGen

	gen := st.judgeGen
	analyst := st.cfg.Analyst
	explainer := st.cfg.Explainer
	start := st.start
	moves := append([]string(nil), st.usis...)
	played := st.moves[len(st.moves)-1]
	ply := len(st.usis)

	go func() {
		j, err := analyst.Judge(ctx, start, moves, ply)

		// **문장도 여기서 만든다.** 세션 goroutine 밖이라는 조건이 판정과 똑같고, 무엇보다
		// 카드가 뜨기 **전**에 문장이 손에 들어와야 한다 — 나중에 올려 보내 갈아끼우면
		// 플레이어가 읽는 도중에 글자가 바뀐다. 대신 이 시간이 카드 지연에 더해지므로
		// explain 쪽이 시한을 걸고, 넘기면 결정적 문구로 간다(explain.Deadline).
		var e explain.Result
		if err == nil && j.Verdict.Kind == intervene.KindBlunder {
			e = describe(ctx, explainer, j.Facts)
		}

		select {
		case judgeDone <- judgeResult{gen: gen, judgement: j, move: played, explanation: e, err: err}:
		case <-ctx.Done():
		}
	}()
	return true
}

// describe 는 사실을 문장으로 바꾼다. Explainer 가 없으면 결정적 문구를 쓴다.
//
// nil 처리를 한 곳에 모아 두는 이유는 **문장이 비는 경로를 만들지 않기 위해서**다.
// 부르는 쪽마다 nil을 보면 그중 하나가 언젠가 빈 문자열을 그대로 카드에 싣는다.
func describe(ctx context.Context, e explain.Explainer, f explain.Facts) explain.Result {
	if e == nil {
		return explain.Result{Body: explain.Render(f), Tier: explain.TierTemplate}
	}
	return e.Explain(ctx, f)
}

// applyVerdict 는 판정 결과를 반영한다. 걸렸으면 **되무른다.**
func (st *state) applyVerdict(ctx context.Context, r judgeResult, engineDone chan engineResult, gaugeDone chan mateResult, tesujiDone chan tesujiHintResult) {
	if !st.judging || r.gen != st.judgeGen {
		return // 그 사이 국면이 움직였다. 버린다
	}
	st.judging = false

	if r.err != nil {
		// 판정이 실패했다고 대국을 멈추지 않는다. 개입은 부가 기능이고 대국이 본체다.
		log.Printf("game: judging failed, letting the move stand: %v", r.err)
	}

	if r.err == nil && r.judgement.Verdict.Kind == intervene.KindBlunder {
		st.rollback(r)
		// 되물러 사람 차례로 돌아왔다. 힌트와 게이지도 그 국면의 것으로 다시 구한다 —
		// 롤백이 searchGen 을 올리므로 물러지기 전의 것은 이미 무효다.
		st.computeTagHints()
		st.maybeTesujiHint(ctx, tesujiDone)
		st.maybeGauge(ctx, gaugeDone)
		st.broadcast()
		return
	}

	// 판정을 통과했다. 여기가 사람의 수가 확정되는 자리다. 갇힘도 여기서 풀린다.
	st.stuck = 0

	// **手筋의 이름이 여기서 정해진다.** 판정이 들고 온 평가치가 「이득인가」에 답하고
	// (tesuji.go), 그 답은 이 국면에서만 유효하므로 세대를 함께 적는다.
	//
	// 앞 국면을 함께 넘기는 것은 **이 수가 만든 형태에만** 그 답을 주기 위해서다.
	// `prevPos` 는 롤백용으로 이미 들고 있던 값이고, 판정이 끝난 지금 그것이 정확히
	// 「이 수를 두기 전」이다.
	//
	// 물러진 쪽에서는 이 줄에 오지 않는다. 되물러진 수가 만든 형태에 이름을 붙이면
	// 두지 않은 것으로 된 수가 판의 이름을 정하는 일이 된다(movesBy 와 같은 이유).
	st.tesuji = namedTesuji(st.prevPos, st.pos, st.cfg.HumanColor, r.move.USI, r.judgement)
	st.tesujiGen = st.searchGen

	st.recordLastMove()
	st.recordEvals(r.judgement)
	st.maybeThink(ctx, engineDone)
	st.maybeGauge(ctx, gaugeDone)
	st.broadcast()
}

// rollback 은 직전 사람의 수를 물린다.
//
// **되돌리는 것은 국면·기보·표기·千日手 계수까지 전부다.** 하나라도 남으면 물러진 수가
// 있었다는 흔적이 남아 다음 판정이 그 위에서 돈다.
func (st *state) rollback(r judgeResult) {
	key := st.pos.RepetitionKey()
	if n := st.repeats[key]; n > 0 {
		st.repeats[key] = n - 1
	}

	st.pos, st.prevTo = st.prevPos, st.prevPrevTo
	st.moves = st.moves[:len(st.moves)-1]
	st.usis = st.usis[:len(st.usis)-1]
	st.searchGen++ // 물러진 국면에 대한 늦은 결과를 버리기 위해

	// **물러진 수는 여기서만 남는다.** 기보에는 안 들어가므로, 이 한 줄을 안 쓰면
	// 개입에 오염되지 않은 유일한 실력 신호가 그대로 사라진다(01-core.md §5).
	if st.cfg.Recorder != nil {
		st.cfg.Recorder.Retracted(len(st.usis)+1, r.move.USI, r.judgement.Verdict, r.explanation)
	}

	st.stuck++
	st.hint = buildHint(st.stuck, r.judgement.BestUSI)

	v := r.judgement.Verdict
	st.intervention = &Intervention{
		Kind:            string(v.Kind),
		Category:        string(v.Category),
		RetractedUSI:    r.move.USI,
		RetractedJa:     r.move.Ja,
		DeltaWin:        v.DeltaWin,
		LostMate:        v.LostMate,
		Message:         r.explanation.Body,
		RetractedSFEN:   r.judgement.RetractedSFEN,
		RetractedChecks: r.judgement.RetractedChecks,
		Refutation:      r.judgement.Refutation,
	}
}

// apply 는 검증이 끝난 수를 반영한다. 표기는 착수 전 국면에서 만들어야 한다.
func (st *state) apply(m shogi.Move, by Side) {
	ja := st.pos.MoveJa(m, st.prevTo)
	st.pos = st.pos.Apply(m)
	st.prevTo = int(m.To)
	st.moves = append(st.moves, Move{USI: m.USI(), Ja: ja, By: by})
	st.usis = append(st.usis, m.USI())
	st.searchGen++

	key := st.pos.RepetitionKey()
	st.repeats[key]++

	switch {
	case st.pos.NoLegalMoves():
		// 쇼기에서는 手詰まり도 패배다. 어느 쪽이든 수번 측이 진다.
		status := StatusStalemate
		if st.pos.InCheck(st.pos.Turn) {
			status = StatusCheckmate
		}
		st.finish(status, st.sideOf(st.pos.Turn.Other()))
	case st.repeats[key] >= 4:
		// 千日手. 連続王手の千日手(반칙패)는 아직 구분하지 않는다 — 수순 전체를 보고
		// 매 수 王手였는지 따져야 하고, 초·중반에서는 거의 나오지 않는다.
		st.finish(StatusRepetition, "")
	}
}

func (st *state) finish(status Status, winner Side) {
	st.status = status
	st.winner = winner
	st.thinking = false
	st.judging = false
	st.searchGen++
	if st.cfg.Recorder != nil {
		st.cfg.Recorder.Finished(status, winner)
	}
}

// recordLastMove 는 **확정된** 직전 수를 기록에 넘긴다.
//
// `apply` 안이 아니라 확정되는 자리마다 부른다 — 착수와 확정이 같은 순간이 아니기
// 때문이다. 사람의 수는 판정을 통과해야 확정되고, 물러지면 기보에서 사라진다.
func (st *state) recordLastMove() {
	if st.cfg.Recorder == nil || len(st.moves) == 0 {
		return
	}
	last := st.moves[len(st.moves)-1]
	st.cfg.Recorder.Moved(len(st.moves), last.USI, last.By)
}

// recordEvals 는 판정이 들고 온 평가치 둘을 **두 手数에 한 번에** 채운다(Recorder.Evaluated).
// 그래서 마지막 수의 평가치는 안 채워진다 — 그 뒤에 사람의 수가 없으면 판정도 없다.
func (st *state) recordEvals(j Judgement) {
	if st.cfg.Recorder == nil || !j.HasEvals {
		return
	}
	ply := len(st.moves)
	st.cfg.Recorder.Evaluated(ply, j.SenteCpAfter)
	if ply >= 2 {
		st.cfg.Recorder.Evaluated(ply-1, j.SenteCpBefore)
	}
}

// maybeThink 는 엔진 차례면 탐색을 띄운다.
//
// 탐색은 세션 goroutine 밖에서 돈다. 여기서 기다리면 생각하는 동안 스냅샷 요청도
// 투료도 못 받는다 — 상태를 소유한 goroutine은 절대 오래 막히면 안 된다.
func (st *state) maybeThink(ctx context.Context, engineDone chan engineResult) {
	if st.status != StatusPlaying || st.thinking || st.pos.Turn == st.cfg.HumanColor {
		return
	}
	st.thinking = true
	st.pendingGen = st.searchGen

	opp := st.cfg.Opponent
	start := st.start
	// 슬라이스를 그대로 넘기면 다음 착수의 append가 같은 배열을 건드릴 수 있다.
	moves := append([]string(nil), st.usis...)

	go func() {
		usi, err := opp.Choose(ctx, start, moves)
		select {
		case engineDone <- engineResult{usi: usi, err: err}:
		case <-ctx.Done():
		}
	}()
}

// maybeGauge 는 사람 차례면 詰み 게이지를 그 국면의 값으로 다시 구한다.
//
// **maybeThink 와 정확히 반대 조건**이라 언제나 둘 중 하나만 돈다. 그래서 solver 풀이
// 하나여도 게이지와 개입 판정이 서로 기다리지 않는다 — 판정은 사람의 수 직후에,
// 게이지는 상대의 수 직후에 걸린다.
//
// 탐색·판정처럼 세션 goroutine 밖에서 돈다. 게이지는 부가 표시라 늦게 와도 되고,
// 여기서 기다리면 그동안 투료도 스냅샷도 못 받는다.
func (st *state) maybeGauge(ctx context.Context, gaugeDone chan mateResult) {
	if st.cfg.Mate == nil || st.status != StatusPlaying {
		return
	}
	// **사람 차례에서만 묻는다.** solver 는 수번 측의 詰み을 답하므로 이 자리라야
	// 「내가 상대 玉을 몇 手로 詰ますか」가 나온다. 상대 차례에 물으면 정확히 반대,
	// 즉 그리지 않기로 한 쪽이 나온다(gauge.go).
	if st.pos.Turn != st.cfg.HumanColor {
		return
	}
	if st.gauging && st.gaugeGen == st.searchGen {
		return // 같은 국면을 두 번 묻지 않는다
	}
	st.gauging = true
	st.gaugeGen = st.searchGen

	gen := st.gaugeGen
	mate := st.cfg.Mate
	start := st.start
	// 슬라이스를 그대로 넘기면 다음 착수의 append 가 같은 배열을 건드릴 수 있다.
	moves := append([]string(nil), st.usis...)

	go func() {
		r, err := mate.SearchMate(ctx, start, moves)
		select {
		case gaugeDone <- mateResult{gen: gen, plies: len(r.Moves), err: err}:
		case <-ctx.Done():
		}
	}()
}

// applyMateHeat 는 구해진 게이지 세기를 반영한다.
func (st *state) applyMateHeat(r mateResult) {
	if !st.gauging || r.gen != st.gaugeGen {
		return // 그 사이 게이지를 다시 걸었다. 늦게 온 앞의 결과다
	}
	st.gauging = false

	if r.gen != st.searchGen {
		return // 국면이 움직였다. 스냅샷이 mateGen 으로 걸러내지만 적지도 않는다
	}
	if r.err != nil {
		// 게이지가 없다고 대국을 멈추지 않는다. 개입 판정과 같은 판단이다 —
		// 테두리가 어두운 채로 남고 대국은 그대로 간다.
		log.Printf("game: mate gauge failed, the border stays dark: %v", r.err)
		return
	}

	st.mateHeat, st.mateGen = mateHeat(r.plies), r.gen
	// **세기가 그대로여도 뿌린다.** 국면이 바뀌면 스냅샷이 게이지를 껐다가 여기서 다시
	// 켜는데, 「바뀌었을 때만」으로 두면 값이 같은 국면에서 꺼진 채로 남는다.
	st.broadcast()
}

func (st *state) applyEngineMove(ctx context.Context, r engineResult, engineDone chan engineResult, gaugeDone chan mateResult, tesujiDone chan tesujiHintResult) {
	if !st.thinking || st.pendingGen != st.searchGen {
		return // 롤백·투료로 국면이 바뀐 뒤 도착한 결과. 버린다
	}
	st.thinking = false

	if r.err != nil {
		log.Printf("game: engine search failed: %v", r.err)
		st.finish(StatusResigned, SideHuman)
		st.broadcast()
		return
	}

	switch r.usi {
	case "resign", "win", "none", "":
		st.finish(StatusResigned, SideHuman)
		st.broadcast()
		return
	}

	// 엔진 출력을 그대로 믿지 않는다. 국면을 잘못 보냈거나 엔진이 헷갈린 경우
	// 여기서 잡히고, 안 잡으면 기보가 조용히 깨진 채로 남는다.
	m, err := shogi.ParseUSIMove(r.usi)
	if err == nil {
		err = st.pos.ValidateMove(m)
	}
	if err != nil {
		log.Printf("game: engine returned an unplayable move %q: %v", r.usi, err)
		st.finish(StatusResigned, SideHuman)
		st.broadcast()
		return
	}

	st.apply(m, SideEngine)
	st.recordLastMove() // 상대 수는 판정하지 않으므로 두는 즉시 확정이다
	st.computeTagHints()
	st.maybeTesujiHint(ctx, tesujiDone)
	st.maybeThink(ctx, engineDone)
	st.maybeGauge(ctx, gaugeDone)
	st.broadcast()
}

func (st *state) sideOf(c shogi.Color) Side {
	if c == st.cfg.HumanColor {
		return SideHuman
	}
	return SideEngine
}

// movesBy 는 한쪽이 둔 수만 순서대로 낸다. 전법·戦型 판정의 입력이다.
//
// **물러진 수는 들어 있지 않다.** `st.moves` 는 롤백 때 잘리므로, 되물러진 수로 전법이
// 정해지는 일이 없다 — 두지 않은 것으로 된 수가 판의 이름을 정하면 개입이 기보를
// 바꾸는 것이 된다.
func (st *state) movesBy(side Side) []string {
	out := make([]string, 0, len(st.moves))
	for _, m := range st.moves {
		if m.By == side {
			out = append(out, m.USI)
		}
	}
	return out
}

// styleTags 는 화면에 나갈 이름 전부다 — 囲い·전법·戦型은 판에서 매번 다시 세고,
// 手筋은 **엔진 게이트를 통과한 것만** 실린다.
//
// 두 축이 다르게 오는 것이 요점이고, **비용 때문이 아니다.** 囲い는 판만 보면 알 수
// 있어 매번 세는 것이 언제나 맞지만, 手筋은 「이득인가」를 엔진 평가치가 정하고 그 값은
// 판에서 다시 읽을 수 없다 — 그래서 판정이 끝난 자리에서 한 번 구해 세대와 함께
// 들고 있는다(applyVerdict).
//
// **手筋이 먼저 온다.** 화면은 새로 붙은 이름 하나를 골라 잠깐 띄우는데(useTagAnnounce),
// 手筋은 국면이 움직이면 사라지는 이름이고 囲い는 남아 있어 다음 스냅샷에서도 뜬다.
// 뒤에 두면 한 스냅샷에 둘이 함께 붙었을 때 사라지는 쪽이 밀려서 영영 안 뜬다.
func (st *state) styleTags() []tag.Tag {
	var out []tag.Tag
	if st.tesujiGen == st.searchGen {
		out = append(out, st.tesuji...)
	}
	return append(out, tag.Detect(tag.Input{
		Pos:           st.pos,
		Color:         st.cfg.HumanColor,
		PlayerMoves:   st.movesBy(SideHuman),
		OpponentMoves: st.movesBy(SideEngine),
	})...)
}

const (
	TagHintMaxPerGame = 3
	TagHintCooldown   = 10
)

// computeTagHints 는 이 국면에서 플레이어의 합법수 중 **새 이름을 만드는 것**을 찾는다.
//
// 엔진을 안 부른다 — 전법·戦型은 판과 수순만으로 정해지므로 합법수마다 시뮬레이션하면
// 끝이다. 手筋은 평가치가 있어야 해서 비동기로 따로 구한다(maybeTesujiHint).
//
// **囲い는 제안에서만 뺀다**(styleTags 쪽에는 남는다). 짓다 만 형태에 이름이 없어서,
// 「이 수를 두면 이름이 생긴다」가 구현한 종류 수에 달린 임의의 한 수가 된다 — §44.
func (st *state) computeTagHints() {
	st.tagHintGen = st.searchGen

	if st.status != StatusPlaying || st.pos.Turn != st.cfg.HumanColor {
		st.tagHints = nil
		return
	}
	if st.tagHintCount >= TagHintMaxPerGame {
		st.tagHints = nil
		return
	}
	ply := len(st.moves)
	if st.tagHintLastPly > 0 && ply > st.tagHintLastPly && ply-st.tagHintLastPly < TagHintCooldown {
		st.tagHints = nil
		return
	}

	playerMoves := st.movesBy(SideHuman)
	oppMoves := st.movesBy(SideEngine)

	have := map[string]bool{}
	for _, t := range tag.Detect(tag.Input{
		Pos: st.pos, Color: st.cfg.HumanColor,
		PlayerMoves: playerMoves, OpponentMoves: oppMoves,
	}) {
		have[t.Code] = true
	}

	seen := map[string]bool{}
	var hints []tag.Tag
	for _, m := range st.pos.LegalMoves() {
		after := st.pos.Apply(m)
		afterMoves := append(append([]string(nil), playerMoves...), m.USI())
		for _, t := range tag.Detect(tag.Input{
			Pos: after, Color: st.cfg.HumanColor,
			PlayerMoves: afterMoves, OpponentMoves: oppMoves,
		}) {
			if t.Kind == tag.KindCastle {
				continue // 짓는 도중의 한 수를 권할 근거가 없다(§44)
			}
			if !have[t.Code] && !seen[t.Code] {
				seen[t.Code] = true
				hints = append(hints, t)
			}
		}
	}
	st.tagHints = hints
	if len(hints) > 0 && st.tagHintLastPly != ply {
		st.tagHintCount++
		st.tagHintLastPly = ply
	}
}

// maybeTesujiHint 는 사람 차례면 **「지금 두면 새 手筋 이름이 생기는 수」를 찾아 그것이
// 엔진에게도 이득인지 묻고, 통과한 것만 남긴다.** 빈도 상한과 쿨다운도 여기서 본다.
//
// `computeTagHints` 와 갈리는 이유가 하나다: 囲い·전법은 판과 수순만으로 정해지지만
// 手筋은 「그래서 得인가」를 엔진이 답해야 이름이 붙는다(tesuji.go). 그래서 게이지와 같은
// 모양이 된다 — goroutine 으로 던지고 세대로 걸러 받는다(maybeGauge).
//
// **엔진을 걸기 전에 룰로 거른다.** 새 이름이 생기는 수는 국면당 대부분 0이라
// (06-status.md §34 · §42) 이 한 줄이 사람 차례마다 도는 탐색을 거의 다 없앤다.
func (st *state) maybeTesujiHint(ctx context.Context, done chan tesujiHintResult) {
	if st.cfg.TesujiHint == nil || st.status != StatusPlaying {
		return
	}
	// 사람 차례에서만 묻는다. 상대 차례의 手筋은 알릴 것이 아니라 당할 것이다.
	if st.pos.Turn != st.cfg.HumanColor {
		return
	}
	if st.tesujiHinting && st.tesujiHintGen == st.searchGen {
		return // 같은 국면을 두 번 묻지 않는다
	}
	if st.tesujiHintCount >= TagHintMaxPerGame {
		return
	}
	ply := len(st.moves)
	if st.tesujiHintLastPly > 0 && ply > st.tesujiHintLastPly && ply-st.tesujiHintLastPly < TagHintCooldown {
		return
	}

	opts := tesujiOptions(st.pos, st.cfg.HumanColor)
	if len(opts) == 0 {
		st.tesujiOpts, st.tesujiHintGen = nil, st.searchGen
		return
	}

	st.tesujiHinting = true
	st.tesujiHintGen = st.searchGen

	gen := st.tesujiHintGen
	search := st.cfg.TesujiHint
	start := st.start
	color := st.cfg.HumanColor
	// 슬라이스를 그대로 넘기면 다음 착수의 append 가 같은 배열을 건드릴 수 있다.
	moves := append([]string(nil), st.usis...)

	go func() {
		kept, dropped, err := gateTesujiOptions(ctx, search, JudgeDepth, start, moves, opts, color)
		select {
		case done <- tesujiHintResult{gen: gen, opts: kept, dropped: dropped, err: err}:
		case <-ctx.Done():
		}
	}()
}

// applyTesujiHint 는 게이트를 통과한 후보를 반영한다.
func (st *state) applyTesujiHint(r tesujiHintResult) {
	if !st.tesujiHinting || r.gen != st.tesujiHintGen {
		return // 그 사이 다시 걸었다. 늦게 온 앞의 결과다
	}
	st.tesujiHinting = false

	if r.gen != st.searchGen {
		return // 국면이 움직였다. 낡은 평가치로 이름을 붙이는 것이 게이트를 없애는 것과 같다
	}
	if r.err != nil {
		// 힌트가 없다고 대국을 멈추지 않는다. 게이지·개입 판정과 같은 판단이다.
		log.Printf("game: tesuji hint search failed, the hint stays quiet: %v", r.err)
		return
	}
	if r.dropped > 0 {
		// 잘린 것을 안 세면 「手筋이 없었다」와 「못 봤다」가 같은 화면이 된다.
		log.Printf("game: tesuji hint skipped %d candidate(s) over the cap", r.dropped)
	}

	st.tesujiOpts = r.opts
	if len(r.opts) > 0 {
		if ply := len(st.moves); st.tesujiHintLastPly != ply {
			st.tesujiHintCount++
			st.tesujiHintLastPly = ply
		}
		st.pointHintAtTesuji()
	}
	st.broadcast()
}

// pointHintAtTesuji 는 **이미 열린 계단**이 최선수 대신 手筋을 짚게 바꾼다.
//
// 계단을 새로 만들지 않는다. 둘 다 「네가 무엇을 두면 되는가」이고 발동 조건도 같아서,
// 따로 두면 같은 파랑이 두 뜻이 된다 — 06-status.md §41.
//
// 바꿔도 되는 근거는 게이트다. 후보는 전부 `TesujiLossCp` 안이고 그 대신 **이름이 있다**
// (01-core.md §7.1). 여럿이면 첫 번째 — `LegalMoves` 순서라 결정적이고 전부 같은 게이트를
// 지났으므로 여기서 새로 고를 근거가 없다.
func (st *state) pointHintAtTesuji() {
	if st.hint == nil || len(st.tesujiOpts) == 0 {
		return
	}
	if h := buildHint(st.stuck, st.tesujiOpts[0].USI); h != nil {
		st.hint = h
	}
}

func (st *state) snapshot() Snapshot {
	turn := "b"
	if st.pos.Turn == shogi.White {
		turn = "w"
	}
	yours := st.status == StatusPlaying && !st.judging && st.pos.Turn == st.cfg.HumanColor

	snap := Snapshot{
		SFEN:         st.pos.SFEN(),
		Ply:          len(st.moves),
		Turn:         turn,
		YourTurn:     yours,
		InCheck:      st.pos.InCheck(st.pos.Turn),
		Thinking:     st.thinking,
		Moves:        append([]Move(nil), st.moves...),
		Status:       st.status,
		Winner:       st.winner,
		Judging:      st.judging,
		Intervention: st.intervention,
		Hint:         st.hint,
		StyleTags:    st.styleTags(),
	}
	// 국면이 움직였으면 게이지는 그 자리에서 무효다(state.mateGen).
	if st.mateGen == st.searchGen {
		snap.MateHeat = st.mateHeat
	}
	// 囲い·전법과 手筋이 **같은 칸으로 나간다.** 표시가 하나여야 하기 때문이고(§41),
	// 세대를 따로 보는 것은 手筋이 엔진을 기다리느라 늦게 도착하기 때문이다 — 囲い 쪽은
	// 이미 떠 있고 手筋이 몇 초 뒤에 합류한다.
	//
	// **새 슬라이스에 담는다.** `st.tagHints` 에 그대로 덧붙이면 이미 뿌린 스냅샷과 배열을
	// 공유하게 되고, 구독자가 그것을 읽는 동안 세션이 다음 append 를 쓴다.
	var hints []tag.Tag
	if st.tagHintGen == st.searchGen {
		hints = append(hints, st.tagHints...)
	}
	if st.tesujiHintGen == st.searchGen {
		hints = append(hints, tesujiHintTags(st.tesujiOpts)...)
	}
	snap.TagHints = hints
	if yours {
		for _, m := range st.pos.LegalMoves() {
			snap.LegalMoves = append(snap.LegalMoves, m.USI())
		}
	}
	return snap
}

func (st *state) broadcast() {
	snap := st.snapshot()
	for ch := range st.subs {
		notify(ch, snap)
	}
}

func (st *state) closeSubs() {
	for ch := range st.subs {
		close(ch)
	}
	st.subs = nil
}

// notify 는 막히지 않고 최신 스냅샷을 넣는다.
//
// 느린 클라이언트 하나가 세션 goroutine을 멈추게 두면 다른 사람의 대국까지 선다.
// 스냅샷은 항상 전체 상태라 중간 것을 버려도 손실이 없다 — 최신만 의미가 있다.
func notify(ch chan Snapshot, snap Snapshot) {
	for range 2 {
		select {
		case ch <- snap:
			return
		default:
		}
		select {
		case <-ch: // 낡은 것을 버리고 다시 시도
		default:
		}
	}
}

func (s *Session) send(ctx context.Context, c command) (Snapshot, error) {
	c.reply = make(chan result, 1)
	select {
	case s.cmds <- c:
	case <-s.closed:
		return Snapshot{}, ErrClosed
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
	select {
	case r := <-c.reply:
		return r.snap, r.err
	case <-s.closed:
		return Snapshot{}, ErrClosed
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	}
}

// Play 는 사람의 수를 둔다. 불법수면 *shogi.IllegalMoveError 가 돌아온다.
func (s *Session) Play(ctx context.Context, usi string) (Snapshot, error) {
	return s.send(ctx, command{kind: cmdMove, usi: usi})
}

// Resign 은 사람이 投了한다.
func (s *Session) Resign(ctx context.Context) (Snapshot, error) {
	return s.send(ctx, command{kind: cmdResign})
}

// Snapshot 은 지금 상태를 돌려준다.
func (s *Session) Snapshot(ctx context.Context) (Snapshot, error) {
	return s.send(ctx, command{kind: cmdSnapshot})
}

// Subscribe 는 상태가 바뀔 때마다 스냅샷을 받는 채널을 준다.
// 반환된 취소 함수를 반드시 부른다. 세션이 끝나면 채널이 닫힌다.
func (s *Session) Subscribe(ctx context.Context) (<-chan Snapshot, func(), error) {
	ch := make(chan Snapshot, 1)
	if _, err := s.send(ctx, command{kind: cmdSubscribe, sub: ch}); err != nil {
		return nil, nil, err
	}
	cancel := func() {
		_, _ = s.send(context.WithoutCancel(ctx), command{kind: cmdUnsubscribe, sub: ch})
	}
	return ch, cancel, nil
}

// Close 는 세션을 끝내고 goroutine이 정리될 때까지 기다린다. 두 번 불러도 안전하다.
func (s *Session) Close() {
	s.closeOnce.Do(func() { close(s.done) })
	<-s.closed
}
