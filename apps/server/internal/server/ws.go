package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
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
	Type     string         `json:"type"` // "snapshot" | "error" | "whatif" | "whatif_error"
	Snapshot *game.Snapshot `json:"snapshot,omitempty"`
	Reason   string         `json:"reason,omitempty"`  // 기계용 코드(영어)
	Message  string         `json:"message,omitempty"` // 화면용 문구(일본어)

	// WhatIf 는 가정 수순의 지금 자리다. **스냅샷과 갈라 둔다** — 이건 대국의 상태가
	// 아니라 「안 벌어진 일」이고, 하나로 합치면 화면이 두 판을 같은 것으로 그린다.
	WhatIf *whatifNode `json:"whatif,omitempty"`
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

	// Origin 기본 검사를 그대로 쓴다. 개발에서는 Vite가 /ws/game 을 프록시하므로 같은 오리진이다.
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept 가 이미 응답을 썼다
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	cfg := game.Config{
		Opponent:     h.opts.NewOpponent(),
		HumanColor:   h.opts.HumanColor,
		StartSFEN:    h.opts.StartSFEN,
		ObservePlies: h.opts.ObservePlies,
		Explainer:    h.opts.Explainer,
		Mate:         h.opts.Mate,
		// 手筋 제안형 힌트도 가정 수순과 **같은 풀**이다. 묻는 것이 같은 종류라서다 —
		// 둘 다 「이 수를 둬 보면 어떻게 되나」이고, 그래서 Options 에 필드를 따로 두지
		// 않는다. nil이면 手筋 힌트만 꺼지고 囲い·전법 힌트는 그대로 뜬다.
		TesujiHint: h.opts.Search,
	}
	if h.opts.NewAnalyst != nil {
		cfg.Analyst = h.opts.NewAnalyst()
	}
	// DB가 없으면 기록하지 않고 대국은 그대로 된다 — 엔진·캐시와 같은 판단이다.
	if h.opts.Store != nil {
		cfg.Recorder = newDBRecorder(ctx, h.opts.Store, h.opts.Level, userID)
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

	// 스냅샷을 쓰기 쪽으로 넘긴다.
	go func() {
		for {
			select {
			case snap, ok := <-snaps:
				if !ok {
					return
				}
				played.set(snap.Moves)
				emit(ctx, out, serverMsg{Type: "snapshot", Snapshot: &snap})
			case <-ctx.Done():
				return
			}
		}
	}()

	h.readLoop(ctx, conn, sess, out, &played)
}

func (h *gameHandler) readLoop(
	ctx context.Context,
	conn *websocket.Conn,
	sess *game.Session,
	out chan serverMsg,
	played *confirmed,
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
			h.whatif(ctx, out, played, slot, msg)

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

	root := whatifRoot{StartSFEN: h.opts.StartSFEN, Moves: played.get(), Human: h.opts.HumanColor}
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
