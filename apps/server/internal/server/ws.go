package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/jovid18/show-gi/apps/server/internal/book"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/quiz"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 대국은 WebSocket이다. 상대의 수도 개입도 **서버가 먼저 말을 걸므로** 요청/응답이 아니다.
// **세션은 연결에 매여 있다** — 끊기면 대국도 끝난다(README).

const (
	// writeTimeout 은 프레임 하나를 밀어 넣는 데 주는 시간이다.
	// 대국은 오래 열려 있으므로 연결 전체에는 시한을 두지 않는다.
	writeTimeout = 10 * time.Second

	// pingInterval 은 ALB의 900초 유휴 시한보다 충분히 짧아야 한다.
	pingInterval = 45 * time.Second
)

// clientMsg 는 브라우저가 보내는 것.
type clientMsg struct {
	Type string `json:"type"` // "move" | "resign" | "whatif"
	USI  string `json:"usi,omitempty"`

	// Ply·Moves 는 "whatif" 에서만 쓴다 — 「확정된 몇 手目에서 이 수순을 뒀다면」이다.
	//
	// **판(SFEN)을 받지 않는다.** 뿌리는 서버가 자기 기록에서 다시 둬서 만든다(whatif.go).
	Ply   int      `json:"ply,omitempty"`
	Moves []string `json:"moves,omitempty"`
}

// serverMsg 는 서버가 보내는 것.
type serverMsg struct {
	Type     string         `json:"type"` // "snapshot" | "error" | "whatif" | "whatif_error" | "summary"
	Snapshot *game.Snapshot `json:"snapshot,omitempty"`
	Reason   string         `json:"reason,omitempty"`  // 기계용 코드(영어)
	Message  string         `json:"message,omitempty"` // 화면용 문구(일본어)

	// WhatIf 는 가정 수순의 지금 자리다. **스냅샷과 갈라 둔다** — 이건 대국의 상태가
	// 아니라 「안 벌어진 일」이고, 하나로 합치면 화면이 두 판을 같은 것으로 그린다.
	WhatIf *whatifNode `json:"whatif,omitempty"`

	// Summary 는 대국이 끝난 뒤 **한 번** 오는 총평이다. 스냅샷과 갈라 둔 이유가 저것과
	// 같다 — 이건 국면의 상태가 아니라 판 전체에 대한 이야기이고, LLM을 기다리므로 결과
	// 문구보다 몇 초 늦게 도착한다.
	Summary *gameSummaryPayload `json:"summary,omitempty"`
}

// rejectMessages 는 착수가 거절된 이유 중 룰 엔진 밖의 것들이다.
//
// 룰 위반 문구는 shogi 패키지가 들고 있다. 여기 있는 것은 프로토콜 수준의 거절이라
// 그쪽에 둘 수 없다. 어느 쪽이든 화면에 나가므로 일본어다.
var rejectMessages = map[string]string{
	"not_your_turn": "相手の手番です。",
	"finished":      "対局はすでに終わっています。",
	"bad_move":      "指し手の形式が正しくありません。",
	"internal":      "サーバーで問題が発生しました。",
}

func rejection(err error) serverMsg {
	var ime *shogi.IllegalMoveError
	switch {
	case errors.As(err, &ime):
		return serverMsg{Type: "error", Reason: ime.Reason.String(), Message: ime.Message()}
	case errors.Is(err, game.ErrNotYourTurn):
		return reject("not_your_turn")
	case errors.Is(err, game.ErrFinished):
		return reject("finished")
	default:
		// USI 표기 파싱 실패 등. 클라이언트가 합법수만 보내면 도달하지 않는다.
		return reject("bad_move")
	}
}

func reject(reason string) serverMsg {
	return serverMsg{Type: "error", Reason: reason, Message: rejectMessages[reason]}
}

// gameHandler 는 연결 하나당 대국 하나를 연다.
type gameHandler struct {
	opts Options
	// auth 는 이 판이 누구의 것으로 남는지만 정한다. **대국을 막지 않는다** —
	// 로그인 없이 두는 판은 지금까지처럼 익명으로 남는다(06-status.md §18).
	auth *authHandler
}

// gameSetup 은 이 연결 하나의 대국 설정이다. 시작 화면이 URL 쿼리로 고른 값이고, 비면
// Options 의 기본값이 그대로 산다.
//
// **`start` 메시지로 받지 않는다.** 그러면 세션이 첫 명령까지 기다려야 하고, 「연결 하나 =
// 대국 하나」(gameHandler)와 세션 수명이 그 자리에서 갈린다 — 쿼리는 업그레이드 전에
// 읽히므로 그 규약을 안 건드린다.
type gameSetup struct {
	human   shogi.Color
	opening book.Opening
	hasBook bool
	// startSFEN 은 이 판의 0手目다. 이어하는 판은 그 행에 적힌 것이고, 새 판은 Options 의 것이다.
	//
	// **가정 수순의 뿌리도 이 값이어야 한다**(whatifRoot). Options 의 것을 그대로 쓰면
	// 이어하는 판에서 뿌리와 수순이 서로 다른 국면의 것이 된다.
	startSFEN string
	// resumeID 가 0이 아니면 이어하는 판이다. 기록 쪽이 새 행을 안 만든다(recordTarget).
	resumeID int64
	// startMoves 는 그 판에서 이미 둬진 수순이다(game.Config.StartMoves).
	startMoves []string
}

// newSetup 은 쿼리에서 새 판의 설정을 읽는다. **못 읽는 값은 조용히 기본값이다** — 목록을
// 서버가 주므로(GET /api/openings) 여기 이상한 값이 오는 것은 클라이언트가 틀린 경우이고,
// 그때 대국을 거절하는 것보다 평수로 시작하는 것이 낫다. 고른 것이 실제로 걸렸는지는
// 스냅샷의 `opponentOpening` 으로 화면에서 보인다.
func newSetup(r *http.Request, opts Options) gameSetup {
	s := gameSetup{human: opts.HumanColor, startSFEN: opts.StartSFEN}
	switch r.URL.Query().Get("color") {
	case "b":
		s.human = shogi.Black
	case "w":
		s.human = shogi.White
	}
	if o, ok := book.Find(r.URL.Query().Get("opening")); ok {
		s.opening, s.hasBook = o, true
	}
	return s
}

// errNoResume 는 이어할 수 없다는 것 하나다. **왜인지는 안 갈라 준다** — 없는 판·남의
// 판·이미 다른 탭이 점유한 판이 같은 답을 받아야 남의 판 번호를 훑어볼 수 없다(§46).
var errNoResume = errors.New("ws: cannot resume")

// resumeSetup 은 이어할 판을 **점유하고** 그 설정을 읽는다.
//
// **업그레이드 전에 부른다.** 여기서 거절하면 아직 평범한 HTTP 요청이라 404로 끝나는데,
// 업그레이드 뒤에는 그 답을 프레임으로 말해야 하고 화면이 그것을 「대국 중 오류」와
// 구별해야 한다.
//
// 점유가 곧 되열기라(store.ClaimGameForResume) 이 함수가 성공한 뒤로는 그 행이 「두는 중」
// 이다. 되돌리는 것은 기록 쪽 하나뿐이다 — ctx 가 끝나면 다시 abandoned 로 닫는다.
func (h *gameHandler) resumeSetup(ctx context.Context, raw string, userID *int64) (gameSetup, error) {
	// **로그인한 사람만이다.** 익명 판은 서로 구별할 수단이 없어서(002_anonymous_games.sql)
	// 「누구의 중단된 판인가」에 답할 수가 없다.
	if userID == nil || h.opts.Store == nil {
		return gameSetup{}, errNoResume
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return gameSetup{}, errNoResume
	}

	claimed, err := h.opts.Store.ClaimGameForResume(ctx, id, *userID)
	if err != nil {
		if !errors.Is(err, store.ErrNoGame) {
			log.Printf("ws: claim game %d: %v", id, err)
		}
		return gameSetup{}, errNoResume
	}

	setup := gameSetup{
		human:     shogi.Black,
		startSFEN: claimed.StartSFEN,
		resumeID:  claimed.ID,
	}
	if claimed.MyColor == "w" {
		setup.human = shogi.White
	}
	// 진형도 그 판의 것으로 돌아간다. 북은 상태를 안 들고 매번 (startSFEN, moves) 에서
	// 다시 구하므로(game.bookOpponent), 이름 하나면 끊긴 자리 그대로 이어진다.
	if o, ok := book.Find(claimed.OpeningID); ok {
		setup.opening, setup.hasBook = o, true
	}

	rec, err := h.opts.Store.GameRecordAnyOwner(ctx, claimed.ID)
	if err == nil {
		setup.startMoves, err = resumeMoves(rec)
	}
	if err != nil {
		// **점유를 되돌린다.** 여기서 그냥 나가면 그 판은 result 가 NULL인 채로 남아
		// 되짚기에도 이어하기에도 안 걸린다 — 기록 쪽의 ctx 취소 경로는 세션이 서야
		// 도는 것이고, 이 자리는 아직 그 앞이다.
		log.Printf("ws: resume game %d: %v", claimed.ID, err)
		if ferr := h.opts.Store.FinishGame(ctx, claimed.ID, store.ResultAbandoned); ferr != nil {
			log.Printf("ws: resume game %d: cannot release the claim: %v", claimed.ID, ferr)
		}
		return gameSetup{}, errNoResume
	}
	return setup, nil
}

// releaseResume 는 점유를 되돌린다. 이어하는 판이 아니면 아무 일도 안 한다.
//
// **기록 쪽이 서기 전에만 부른다.** 그 뒤로는 세션 ctx 가 끝날 때 recorder 가 같은 일을
// 하고(recorder.go 의 ctx 취소 경로), 둘 다 부르면 abandoned 를 두 번 쓴다.
func (h *gameHandler) releaseResume(ctx context.Context, setup gameSetup) {
	if setup.resumeID == 0 || h.opts.Store == nil {
		return
	}
	if err := h.opts.Store.FinishGame(ctx, setup.resumeID, store.ResultAbandoned); err != nil {
		log.Printf("ws: cannot release the claim on game %d: %v", setup.resumeID, err)
	}
}

// resumeMoves 는 기록의 기보를 수순 하나로 편다.
//
// **手数에 구멍이 있으면 거절한다.** 기록은 큐가 넘치면 이벤트를 버리므로(recorder.go)
// 한 수가 빠질 수 있는데, 그걸 무시하고 이어 두면 그 뒤가 통째로 밀린 **없던 판**이 된다 —
// 되짚기가 같은 자리에서 재현을 멈추는 것과 같은 판단이다(review.go 의 detailOf).
func resumeMoves(rec store.GameRecord) ([]string, error) {
	out := make([]string, 0, len(rec.Moves))
	for i, m := range rec.Moves {
		if m.Ply != i+1 {
			return nil, fmt.Errorf("game %d: the record jumps to ply %d at %d", rec.ID, m.Ply, i+1)
		}
		out = append(out, m.USI)
	}
	return out, nil
}

// confirmed 는 **세션이 방금 보낸** 확정 수들이다. 세션에 물어보는 길을 새로 파지 않는
// 이유는 그쪽이 곧 **핸들러가 상태를 직접 읽는 지름길**이 되기 때문이다 — 어차피 구독해서
// 받는 스냅샷을 한 벌 들고 있는 것이다(06-status.md §37).
type confirmed struct {
	mu    sync.Mutex
	moves []string
}

func (c *confirmed) set(moves []game.Move) {
	next := make([]string, 0, len(moves))
	for _, m := range moves {
		next = append(next, m.USI)
	}
	c.mu.Lock()
	c.moves = next
	c.mu.Unlock()
}

func (c *confirmed) get() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.moves)
}

func (h *gameHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// **업그레이드 전에 쿠키를 읽는다.** 업그레이드가 끝나면 이 요청은 하이재킹되어
	// 헤더를 다시 볼 길이 없다.
	var userID *int64
	if s, ok := h.auth.viewer(r); ok {
		id := s.UserID
		userID = &id
	}

	// 쿼리도 업그레이드 전에 읽는다 — 위와 같은 이유다.
	setup := newSetup(r, h.opts)
	if raw := r.URL.Query().Get("resume"); raw != "" {
		resumed, err := h.resumeSetup(r.Context(), raw, userID)
		if err != nil {
			// 404다. 「있지만 못 이어한다」를 알려주지 않는다(errNoResume).
			writeJSON(w, http.StatusNotFound, map[string]any{
				"error": "not_found", "message": "その対局は見つかりません。",
			})
			return
		}
		setup = resumed
	}

	// Origin 기본 검사를 그대로 쓴다. 개발에서는 Vite가 /ws/game 을 프록시하므로 같은 오리진이다.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		// **점유를 되돌린다.** 세션이 안 서면 기록 쪽도 안 돌아 그 판이 되열린 채 남는다.
		h.releaseResume(r.Context(), setup)
		return // Accept 가 이미 응답을 썼다
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// 진형은 **감싸는 것으로만** 붙는다. 안쪽 상대의 후보 생성도 자살수 필터도 밴드 제어도
	// 그대로 돌고, 북이 끝나면 그 상대가 이어받는다(game.NewBookOpponent).
	opponent := h.opts.NewOpponent()
	var openingName string
	if setup.hasBook {
		opponent = game.NewBookOpponent(opponent, setup.opening, setup.human.Other())
		openingName = setup.opening.Name
	}

	cfg := game.Config{
		Opponent:        opponent,
		HumanColor:      setup.human,
		OpponentOpening: openingName,
		StartSFEN:       setup.startSFEN,
		StartMoves:      setup.startMoves,
		ObservePlies:    h.opts.ObservePlies,
		Explainer:       h.opts.Explainer,
		Mate:            h.opts.Mate,
		// 手筋 제안형 힌트도 가정 수순과 **같은 풀**이다. 묻는 것이 같은 종류라서다 —
		// 둘 다 「이 수를 둬 보면 어떻게 되나」이고, 그래서 Options 에 필드를 따로 두지
		// 않는다. nil이면 手筋 힌트만 꺼지고 囲い·전법 힌트는 그대로 뜬다.
		TesujiHint: h.opts.Search,
	}
	if h.opts.NewAnalyst != nil {
		cfg.Analyst = h.opts.NewAnalyst()
		// **판정이 있을 때만 실력 추정이 있다.** 추정기의 입력이 판정 결과뿐이라
		// (skill.Move) 판정이 없으면 영원히 아무것도 안 보는 goroutine이 된다.
		//
		// 로그인한 사람은 **지난 판의 값에서 이어 시작하고 매 판정마다 저장된다**(skill.go).
		// 익명 대국은 그대로 판마다 초기화된다 — 쌓을 자리가 없다(002_anonymous_games.sql).
		cfg.Rater = skill.NewWorkerFrom(ctx, h.priorSkill(ctx, userID), h.saveSkill(ctx, userID))
	}
	// DB가 없으면 기록하지 않고 대국은 그대로 된다 — 엔진·캐시와 같은 판단이다.
	var recorder *dbRecorder
	if h.opts.Store != nil {
		recorder = newDBRecorder(ctx, h.opts.Store, h.opts.Level, recordTarget{
			userID:    userID,
			openingID: setup.opening.ID,
			resumeID:  setup.resumeID,
		})
		cfg.Recorder = recorder
	}
	sess, err := game.New(ctx, cfg)
	if err != nil {
		log.Printf("ws: cannot start session: %v", err)
		_ = conn.Close(websocket.StatusInternalError, "session")
		return
	}
	defer sess.Close()

	snaps, unsubscribe, err := sess.Subscribe(ctx)
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "subscribe")
		return
	}
	defer unsubscribe()

	// 쓰기는 한 goroutine만 한다. 두 곳에서 같은 연결에 쓰면 프레임이 섞인다.
	out := make(chan serverMsg, 8)
	go writeLoop(ctx, cancel, conn, out)

	// 가정 수순이 볼 정본 수순. 스냅샷이 올 때마다 갱신된다.
	var played confirmed

	// 스냅샷을 쓰기 쪽으로 넘긴다. 판이 끝나면 그 뒤에 총평 하나가 더 간다.
	go func() {
		summarized := false
		for {
			select {
			case snap, ok := <-snaps:
				if !ok {
					return
				}
				played.set(snap.Moves)
				emit(ctx, out, serverMsg{Type: "snapshot", Snapshot: &snap})

				// **스냅샷을 먼저 보내고 그 뒤에 총평을 만든다.** 결과 문구는 그 자리에서
				// 떠야 하고, 총평은 LLM을 기다리므로 몇 초 뒤에 도착한다.
				//
				// 한 번만 만든다 — 끝난 뒤에도 스냅샷이 또 올 수 있다(投了 확인 등).
				if !summarized && snap.Status != game.StatusPlaying {
					summarized = true
					go h.sendSummary(ctx, out, recorder)
				}
			case <-ctx.Done():
				// **끝난 스냅샷이 이미 와 있는지 한 번 더 본다.**
				//
				// 사람이 投了하고 그 순간 탭을 닫으면 두 case 가 동시에 준비되고, Go는
				// 둘 중 하나를 **무작위로** 고른다. 여기가 이기면 총평도 퀴즈도 안 만들어지는데,
				// 총평은 되짚기가 다시 청할 수 있어도(review.go summary) **퀴즈에는 그런
				// 자리가 없다** — 그 판은 영영 문항을 못 갖는다.
				if !summarized {
					select {
					case snap, ok := <-snaps:
						if ok && snap.Status != game.StatusPlaying {
							summarized = true
							go h.sendSummary(ctx, out, recorder)
						}
					default:
					}
				}
				return
			}
		}
	}()

	h.readLoop(ctx, conn, sess, out, &played, setup)
}

// summaryWait 는 기록이 다 쓰이기를 기다리는 시간이다. 큐를 비우는 일이라 밀리초 단위이고,
// 넘으면 총평을 포기한다 — 반쪽 기록으로 만든 총평은 **틀린 총평**이고, 화면은 그것이
// 없어도 결과와 기보를 이미 말한다.
const summaryWait = 5 * time.Second

// sendSummary 는 대국이 끝난 뒤 총평 하나를 보낸다.
//
// **기록이 다 쓰이기를 기다린다**(dbRecorder.done). 기록은 비동기라 끝난 스냅샷을 보고
// 곧바로 DB를 읽으면 마지막 수와 그 수의 개입이 없는데, 하필 그 수가 총평이 가장 말하고
// 싶은 것이다.
//
// **세션을 안 건드린다.** 읽는 것은 DB뿐이고, 그래서 이 함수는 review.go 와 같은 성질이다 —
// 이미 끝난 판을 읽는 일이다.
func (h *gameHandler) sendSummary(ctx context.Context, out chan serverMsg, recorder *dbRecorder) {
	if recorder == nil || h.opts.Store == nil {
		return // 기록이 없으면 셀 것이 없다. 총평도 없다
	}

	// **여기까지는 연결이 끊겨도 간다.** 뒤에 퀴즈 생성이 걸려 있고 그쪽은 여기서 못 띄우면
	// **아무 데서도 못 띄운다** — 총평은 되짚기가 다시 청하지만(review.go summary) 퀴즈에는
	// 그런 자리가 없다. 판이 끝나자마자 탭을 닫는 것이 드문 일도 아니다.
	//
	// 기다리는 것은 큐를 비우는 일이고 읽는 것은 질의 하나라, 끊긴 연결에 매달리는 값이 싸다.
	base := context.WithoutCancel(ctx)

	var gameID int64
	select {
	case gameID = <-recorder.done:
	case <-time.After(summaryWait):
		log.Printf("ws: summary: the record did not finish within %s", summaryWait)
		return
	}

	// **주인을 안 보고 읽는다.** 방금 이 연결이 만든 판이라 소유 검사가 답할 것이 없고,
	// 익명 대국에는 주인이 아예 없다(002_anonymous_games.sql).
	rec, err := h.opts.Store.GameRecordAnyOwner(base, gameID)
	if err != nil {
		log.Printf("ws: summary: read game %d: %v", gameID, err)
		return
	}

	// **퀴즈를 먼저 띄운다.** 총평은 LLM을 기다리므로 여기서 몇 초 막히는데, 그 사이에
	// 문항 만들기가 시작돼 있는 편이 낫다 — 사람이 되짚기를 여는 것은 총평을 읽은 뒤다.
	go h.generateQuiz(base, rec)

	// **총평은 살아 있는 ctx로 만든다.** 끊긴 연결에 보낼 문장을 사느라 라우터를 부를
	// 이유가 없고, 그쪽은 되짚기가 다시 청할 수 있다.
	payload := summarize(ctx, h.opts.Summarizer, rec, h.opts.Level)
	emit(ctx, out, serverMsg{Type: "summary", Summary: &payload})
}

// quizTimeout 은 문항을 만드는 데 주는 시한이다.
//
// **회차를 자르는 자리는 여기 하나다.** 詰み 탐색 예산이 2400 × 107ms ≈ 4.3분이라
// (quiz.MateSearchBudget) 그 아래로는 예산이 먼저 걸리지 않고, gap 쪽 12국면 × 956ms ≈ 12초를
// 더해도 이 값이 마지막이다 — 여유는 40초 남짓으로 **넉넉하지 않다**.
//
// 넘으면 만들던 것을 버린다. 반쪽 트리는 채점에 쓸 수 없다.
const quizTimeout = 5 * time.Minute

// quizSaveTimeout 은 만든 것을 남기는 데 주는 시한이다. DB 쓰기 한 번이라 짧다.
const quizSaveTimeout = 10 * time.Second

// generateQuiz 는 끝난 판에서 문항을 만들어 저장한다.
//
// **연결이 끊겨도 계속한다**(`context.WithoutCancel`). 만드는 데 수십 초가 걸리는데 사람은
// 판이 끝나면 곧 화면을 떠나고, 요청 ctx에 매어 두면 그 순간 문항이 사라진다 — 되짚기에서
// 만들지 않기로 했으므로(06-status.md §53) 여기서 못 만들면 **아무 데서도 못 만든다.**
//
// 종료가 걸리지는 않는다. `Pool.Close` 가 막히는 것은 **진행 중인 탐색 하나**뿐이고, 그
// 하나는 詰み 쪽이 `DepthLimit=11` 로 100ms대이고 탐색 쪽이 1초대다.
//
// **엔진 풀을 오래 잡는다.** mate 풀이 하나면 그동안 진행 중인 다른 대국의 詰み 게이지와
// 종반 판정이 막힌다 — 그래서 풀 크기를 손잡이로 뺐다(cmd/api/main.go startMateEngines).
func (h *gameHandler) generateQuiz(parent context.Context, rec store.GameRecord) {
	if h.opts.Store == nil {
		return
	}

	// **생성기가 없어도 행은 남긴다.** 안 남기면 화면이 「아직 만드는 중」에서 영영 안
	// 벗어난다 — 엔진 없는 배포에서 그 문장은 오지 않을 것을 기다리라는 거짓말이다.
	var q quiz.Quiz
	if h.opts.Quiz != nil {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), quizTimeout)
		built, complete := h.opts.Quiz.Build(ctx, quizInput(rec))
		cut := ctx.Err() != nil
		cancel()

		// **못 본 채로 비었을 때만 안 적는다.**
		//
		// 「끝까지 못 봤다」가 참이어도 **나온 것은 사실이다** — 다 지어진 詰み 트리는 gap
		// 후보 하나를 못 쟀다고 틀려지지 않고, 잰 gap 문항은 트리가 못 섰다고 틀려지지 않는다.
		// 둘을 한 깃발로 묶어 통째로 버리면, 한쪽의 사소한 실패가 멀쩡한 다른 쪽을 지운다.
		//
		// 버리는 것은 **빈 결과**뿐이다. 그때만 「이 판에 문항이 없다」와 「못 봤다」가 같은
		// 그림이 되고, 빈 행을 남기면 화면이 앞쪽으로 단정한다 — 생성이 판이 끝날 때
		// 한 번뿐이라 그 거짓이 영구히 남는다. 안 적으면 화면은 「아직 안 왔다」에 머문다.
		//
		// 시한만 보면 모자란다. **배포가 생성 도중에 끼면** 풀이 먼저 닫혀
		// (`main` 의 defer 순서가 엔진 → DB다) 모든 탐색이 즉시 실패하는데, 그때 ctx는
		// 멀쩡하고 결과만 비어 있다 — 그래서 생성기가 「끝까지 봤는가」를 따로 말한다.
		if (cut || !complete) && built.Empty() {
			log.Printf("ws: quiz: game %d: an incomplete run found nothing (timed out: %v) — leaving no row rather than claiming there was nothing", rec.ID, cut)
			return
		}
		q = built
	}

	// **문항이 없어도 저장한다.** 안 하면 「아직 만드는 중」과 「문항이 없는 판」이 화면에서
	// 같은 그림이 되는데, 만드는 데 수십 초가 걸려서 그 사이에 「問題はありません」을
	// 그리면 그것이 거짓이 된다(quiz.go 의 `ready`).
	payload, err := json.Marshal(q)
	if err != nil {
		log.Printf("ws: quiz: game %d: encode: %v", rec.ID, err)
		return
	}

	// **쓰는 데 시한을 따로 준다.** 만드는 쪽이 시한에 걸렸으면 그 ctx는 이미 죽어 있고,
	// 그대로 쓰면 **만들어 놓고 못 남기는** 자리가 되어 화면이 영영 기다린다.
	save, cancel := context.WithTimeout(context.WithoutCancel(parent), quizSaveTimeout)
	defer cancel()
	if err := h.opts.Store.SaveGameQuiz(save, rec.ID, quiz.Version, payload); err != nil {
		log.Printf("ws: quiz: game %d: save: %v", rec.ID, err)
		return
	}

	mate := 0
	if q.Mate != nil {
		mate = q.Mate.Plies
	}
	log.Printf("ws: quiz: game %d: %d-ply mate item, %d best items", rec.ID, mate, len(q.Best))
}

// quizInput 은 기록을 문항 생성기의 입력으로 옮긴다. **옮기는 자리가 여기다** —
// internal/quiz 가 `store` 를 모르게 두면 문항 기준이 기록의 모양에 안 매인다.
func quizInput(rec store.GameRecord) quiz.Input {
	in := quiz.Input{
		StartSFEN:    startSFENOf(rec.StartSFEN),
		Human:        shogi.Black,
		Won:          rec.Result == store.ResultWin,
		OpeningPlies: openingPlies(rec),
	}
	if rec.MyColor == "w" {
		in.Human = shogi.White
	}
	// **구멍에서 끊는다.** 기보에 빠진 手数가 있으면 그 뒤는 手数와 배열의 자리가 어긋나고,
	// 그대로 두면 문항이 **한 번도 벌어지지 않은 국면**을 가리킨다(review.go detailOf).
	for i, m := range rec.Moves {
		if m.Ply != i+1 {
			break
		}
		in.Moves = append(in.Moves, m.USI)
		in.EvalCp = append(in.EvalCp, m.EvalCp)
	}
	return in
}

// openingPlies 는 컴퓨터가 고른 진형의 수순이 덮는 手数다. 「おまかせ」면 0이다.
//
// `book.Opening.Moves` 는 **한쪽의 수**만 주므로 手数로는 두 배다. 한 手 남짓 넘치거나
// 모자라는 것은 상관없다 — 이 값은 「여기까지는 아직 정석이다」의 바닥이다.
func openingPlies(rec store.GameRecord) int {
	if rec.OpeningID == "" {
		return 0
	}
	o, ok := book.Find(rec.OpeningID)
	if !ok {
		return 0
	}
	engine := shogi.White
	if rec.MyColor == "w" {
		engine = shogi.Black
	}
	return 2 * len(o.Moves(engine))
}

func (h *gameHandler) readLoop(
	ctx context.Context,
	conn *websocket.Conn,
	sess *game.Session,
	out chan serverMsg,
	played *confirmed,
	setup gameSetup,
) {
	// 가정 수순은 한 번에 하나만 돈다. 탐색 둘이 엔진 풀을 잡는 자리라, 연타가 곧
	// **대국 상대의 탐색이 기다리는 시간**이 된다.
	slot := make(chan struct{}, 1)
	slot <- struct{}{}

	for {
		var msg clientMsg
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return // 끊겼거나 ctx 종료. 어느 쪽이든 대국을 접는다
		}

		switch msg.Type {
		case "whatif":
			h.whatif(ctx, out, played, slot, msg, setup)

		case "move":
			if _, err := sess.Play(ctx, msg.USI); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, game.ErrClosed) {
					return
				}
				emit(ctx, out, rejection(err))
			}
			// 성공하면 구독 채널로 스냅샷이 온다. 여기서 또 보내지 않는다.

		case "resign":
			if _, err := sess.Resign(ctx); err != nil && !errors.Is(err, game.ErrFinished) {
				return
			}

		default:
			emit(ctx, out, reject("bad_move"))
		}
	}
}

// whatif 는 「そのとき、こう指していたら」를 대국 화면에서 답한다. 리뷰와 **같은 장치**이고
// (whatif.go) 갈리는 것은 뿌리뿐 — 여기는 방금 받은 스냅샷이다(DB는 개입 직후 한 수가
// 비어 있을 수 있다, §37). **세션은 하나도 안 건드린다.**
func (h *gameHandler) whatif(
	ctx context.Context,
	out chan serverMsg,
	played *confirmed,
	slot chan struct{},
	msg clientMsg,
	setup gameSetup,
) {
	if h.opts.Search == nil {
		emit(ctx, out, whatifError("engine_unavailable"))
		return
	}
	if msg.Ply < 0 || len(msg.Moves) > whatifMaxLine {
		emit(ctx, out, whatifError("bad_line"))
		return
	}

	select {
	case <-slot:
	default:
		// 앞의 것이 아직 돈다. **막고 기다리지 않는다** — readLoop이 멈추면 그동안
		// 投了도 못 한다.
		emit(ctx, out, whatifError("busy"))
		return
	}

	// **뿌리는 이 판의 0手目다**(gameSetup.startSFEN). 이어하는 판은 그것이 Options 의
	// 기본값과 다를 수 있고, 어긋나면 수순이 없는 국면에 얹힌다.
	root := whatifRoot{StartSFEN: setup.startSFEN, Moves: played.get(), Human: setup.human}
	req := whatifRequest{Ply: msg.Ply, Moves: msg.Moves}

	// **탐색을 readLoop 안에서 하지 않는다.** 400ms 짜리 두 번이라, 그동안 클라이언트가
	// 보내는 것이 전부 큐에 쌓인다.
	go func() {
		defer func() { slot <- struct{}{} }()

		searchCtx, cancel := context.WithTimeout(ctx, whatifTimeout)
		defer cancel()

		node, err := whatifNodeOf(searchCtx, root, req, h.opts.Search, cacheOf(h.opts.Store))
		if err != nil {
			log.Printf("ws: whatif ply %d: %v", req.Ply, err)
			emit(ctx, out, whatifError(whatifReason(err)))
			return
		}
		emit(ctx, out, serverMsg{Type: "whatif", WhatIf: &node})
	}()
}

func whatifError(reason string) serverMsg {
	return serverMsg{Type: "whatif_error", Reason: reason, Message: whatifMessages[reason]}
}

func writeLoop(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, out <-chan serverMsg) {
	defer cancel()

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()

	for {
		select {
		case msg := <-out:
			wctx, done := context.WithTimeout(ctx, writeTimeout)
			err := wsjson.Write(wctx, conn, msg)
			done()
			if err != nil {
				return
			}

		case <-ping.C:
			// 사람이 오래 생각하면 프레임이 하나도 안 오간다. 죽은 상대를 알아채는 수단이기도 하다.
			pctx, done := context.WithTimeout(ctx, writeTimeout)
			err := conn.Ping(pctx)
			done()
			if err != nil {
				return
			}

		case <-ctx.Done():
			return
		}
	}
}

// emit 은 막히지 않게 보낸다. 느린 클라이언트가 세션을 붙들면 안 된다.
func emit(ctx context.Context, out chan<- serverMsg, msg serverMsg) {
	select {
	case out <- msg:
	case <-ctx.Done():
	}
}
