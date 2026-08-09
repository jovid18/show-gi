package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 대국은 WebSocket으로 한다. 상대의 수는 사람이 뭘 보내서가 아니라 엔진이 다 생각했을 때
// 도착하고, D3의 개입도 마찬가지로 서버가 먼저 말을 건다. 요청/응답으로는 표현되지 않는다.
//
// **세션은 연결에 매여 있다.** 끊기면 대국도 끝난다 — 아직 대국을 DB에 쓰지 않기 때문이다.
// 그래서 배포하면 진행 중인 대국이 끊긴다(06-status.md).

const (
	// writeTimeout 은 프레임 하나를 밀어 넣는 데 주는 시간이다.
	// 대국은 오래 열려 있으므로 연결 전체에는 시한을 두지 않는다.
	writeTimeout = 10 * time.Second

	// pingInterval 은 ALB의 900초 유휴 시한보다 충분히 짧아야 한다.
	pingInterval = 45 * time.Second
)

// clientMsg 는 브라우저가 보내는 것.
type clientMsg struct {
	Type string `json:"type"` // "move" | "resign"
	USI  string `json:"usi,omitempty"`
}

// serverMsg 는 서버가 보내는 것.
type serverMsg struct {
	Type     string         `json:"type"` // "snapshot" | "error"
	Snapshot *game.Snapshot `json:"snapshot,omitempty"`
	Reason   string         `json:"reason,omitempty"`  // 기계용 코드(영어)
	Message  string         `json:"message,omitempty"` // 화면용 문구(일본어)
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
}

func (h *gameHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Origin 기본 검사를 그대로 쓴다. 개발에서는 Vite가 /ws를 프록시하므로 같은 오리진이다.
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
	}
	if h.opts.NewAnalyst != nil {
		cfg.Analyst = h.opts.NewAnalyst()
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

	// 스냅샷을 쓰기 쪽으로 넘긴다.
	go func() {
		for {
			select {
			case snap, ok := <-snaps:
				if !ok {
					return
				}
				emit(ctx, out, serverMsg{Type: "snapshot", Snapshot: &snap})
			case <-ctx.Done():
				return
			}
		}
	}()

	h.readLoop(ctx, conn, sess, out)
}

func (h *gameHandler) readLoop(ctx context.Context, conn *websocket.Conn, sess *game.Session, out chan serverMsg) {
	for {
		var msg clientMsg
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return // 끊겼거나 ctx 종료. 어느 쪽이든 대국을 접는다
		}

		switch msg.Type {
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
			// 사람이 오래 생각하면 그동안 프레임이 하나도 안 오간다. ALB는 900초에
			// 유휴 연결을 끊으므로, 아무 말 없이 대국이 사라지는 것을 이걸로 막는다.
			// 죽은 상대를 알아채는 수단이기도 하다.
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
