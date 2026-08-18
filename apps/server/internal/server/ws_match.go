package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/match"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 대인전도 WebSocket 이다 — 상대의 수를 서버가 먼저 말해 주므로 요청/응답이 아니다.
//
// **`/ws/game` 과 갈라 둔 이유는 세션의 수명이다.** 저쪽은 「연결 하나 = 대국 하나」라
// 끊기면 판이 끝나는데(journal §46), 여기는 상대가 남아 있어서 끝낼 수가 없다 — 끊긴
// 사람은 같은 링크로 다시 들어와 이어 둔다. 그동안 그 사람의 시계는 흐른다.
//
// 한 사람이 탭을 두 개 열어 같은 방에 붙는 것은 막지 않는다. 둘 다 같은 색이라
// 스냅샷이 같고, 착수는 어느 쪽에서 오든 그 색의 수다.

// matchClientMsg 는 브라우저가 보내는 것.
type matchClientMsg struct {
	Type string `json:"type"` // "move" | "resign"
	USI  string `json:"usi,omitempty"`
}

// matchServerMsg 는 서버가 보내는 것.
type matchServerMsg struct {
	Type string `json:"type"` // "waiting" | "snapshot" | "error" | "record"

	// Room 은 `waiting` 에만 온다 — 초대 링크를 그리는 데 필요한 것들이다.
	Room *roomPayload `json:"room,omitempty"`

	Snapshot *match.Snapshot `json:"snapshot,omitempty"`

	Reason  string `json:"reason,omitempty"`  // 기계용 코드(영어)
	Message string `json:"message,omitempty"` // 화면용 문구(일본어)

	// GameID 는 판이 끝난 뒤 **한 번** 온다. 「振り返り」로 건너가는 링크가 이 값으로 선다 —
	// 대국 화면은 그때까지 자기 판의 번호를 모른다(기록이 WS 밖에서 비동기로 쓰인다).
	//
	// **총평이 아니다.** 대인전에는 총평이 없다 — 세는 것이 개입인데 그것이 없다(journal §83).
	GameID int64 `json:"gameId,omitempty"`
}

// matchRejects 는 착수가 거절된 이유 중 룰 엔진 밖의 것들이다. 엔진 대국의 것과
// **갈라 둔다** — 저쪽에는 무르기와 힌트의 거절이 다섯 더 있고, 여기에만 있는 것이
// 방이 걷혔다는 것 하나다.
//
// **「아직 상대가 안 들어왔다」가 없다.** 그 상태에서는 착수가 도달할 자리가 없다 —
// 읽는 쪽이 `room.Ready()` 뒤에야 선다.
var matchRejects = map[string]string{
	"not_your_turn": "相手の手番です。",
	"finished":      "対局はすでに終わっています。",
	"bad_move":      "指し手の形式が正しくありません。",
	// **방이 걷혔다.** 아무도 안 들어온 채 30분이 지났거나, 방을 만든 사람이 그 뒤로
	// 방을 여럿 더 만들어 이 방이 밀려났다(match.openRoomsPerHost).
	"room_closed": "この対局部屋は期限が切れました。",
	// **확인과 입장 사이에 남이 앉았다.** 창이 밀리초 단위라 드물다.
	"room_full": "この対局部屋はもう二人そろっています。",
}

func matchRejection(err error) matchServerMsg {
	var ime *shogi.IllegalMoveError
	switch {
	case errors.As(err, &ime):
		return matchServerMsg{Type: "error", Reason: ime.Reason.String(), Message: ime.Message()}
	case errors.Is(err, match.ErrNotYourTurn):
		return matchReject("not_your_turn")
	case errors.Is(err, match.ErrFinished):
		return matchReject("finished")
	default:
		return matchReject("bad_move")
	}
}

func matchReject(reason string) matchServerMsg {
	return matchServerMsg{Type: "error", Reason: reason, Message: matchRejects[reason]}
}

// matchHandlerWS 는 연결 하나를 방의 한 자리에 붙인다.
type matchHandlerWS struct {
	hub  *match.Hub
	auth *authHandler
	// records 는 방마다 만든 기록기다. **판 번호를 화면에 돌려주려고만 들고 있다.**
	records *matchRecords
}

func (h *matchHandlerWS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// **업그레이드 전에 쿠키와 쿼리를 읽는다.** 업그레이드가 끝나면 이 요청은
	// 하이재킹되어 헤더를 다시 볼 길이 없다(ws.go 와 같은 규약).
	s, ok := h.auth.viewer(r)
	if !ok {
		notFound(w) // 401이 아닌 이유는 match.go 의 notFound
		return
	}

	roomID := r.URL.Query().Get("room")

	// **자격은 업그레이드 전에 본다.** 여기서 거절하면 아직 평범한 HTTP 요청이라 404로
	// 끝나는데, 업그레이드 뒤에는 그 답을 프레임으로 말해야 하고 화면이 그것을 「대국 중
	// 오류」와 구별해야 한다(ws.go 의 resumeSetup 과 같은 규약).
	//
	// **자리는 안 잡는다**(Peek). 잡는 것은 업그레이드가 성공한 뒤다 — 여기서 잡으면
	// 업그레이드가 실패했을 때(프록시가 헤더를 지웠다·창을 닫았다) **자리가 타 버리고**,
	// 그 방은 그때부터 아무도 못 들어가는데 방 주인 화면은 링크를 계속 광고한다.
	if _, err := h.hub.Peek(roomID, s.UserID); err != nil {
		notFound(w)
		return
	}

	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept 가 이미 응답을 썼다
	}
	defer conn.CloseNow()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	out := make(chan matchServerMsg, 8)
	go matchWriteLoop(ctx, cancel, conn, out)

	// **여기서 자리가 정해진다.** 위 Peek 와 이 사이에 남이 앉을 수 있다 — 그 창은
	// 밀리초 단위이고, 걸리면 프레임으로 말한다(이미 자격 검사를 지난 사람이라
	// 「자리가 찼다」를 알려줘도 새어 나갈 것이 없다).
	room, color, err := h.hub.Enter(roomID, match.Player{UserID: s.UserID, Name: s.Name})
	if err != nil {
		emitMatch(ctx, out, matchReject("room_full"))
		select {
		case <-time.After(roomClosedFlush):
		case <-ctx.Done():
		}
		return
	}

	// **자리에 앉은 것과 화면을 보고 있는 것은 다르다.** 앞은 위 Enter 가 영구히 정했고,
	// 이건 이 연결이 사는 동안만이다 — 둘이 다 붙어 있는 순간 판이 선다.
	detach := h.hub.Connect(room, color)
	defer detach()

	// 상대를 기다리는 동안 화면이 그릴 것을 먼저 보낸다. 이게 없으면 방을 만든 사람은
	// 빈 화면을 보고 초대 링크를 복사할 자리가 없다.
	seat, waiting := h.hub.SeatOf(room, s.UserID)
	emitMatch(ctx, out, matchServerMsg{Type: "waiting", Room: &roomPayload{
		ID:        room.ID,
		YourColor: colorCode(seat),
		HostName:  room.HostName(),
		Waiting:   waiting,
		IsHost:    room.IsHost(s.UserID),
	}})

	select {
	case <-room.Ready():
	case <-room.Closed():
		// **방이 걷혔다.** 이 갈래가 없으면 여기서 기다리던 사람은 영영 「상대를
		// 기다립니다」에 서 있고, 그 화면이 **이미 죽은 링크를 계속 광고한다.**
		emitMatch(ctx, out, matchReject("room_closed"))
		// **문구가 나갈 틈을 준다.** 곧바로 돌아가면 defer 가 연결을 닫아 그 프레임이
		// 사라지고, 화면에는 「끊겼다」만 남는다 — 이유를 말하려고 보낸 것이 그 문구다.
		select {
		case <-time.After(roomClosedFlush):
		case <-ctx.Done():
		}
		return
	case <-ctx.Done():
		return
	}

	table := room.Table()
	snaps, unsubscribe, err := table.Subscribe(ctx, color)
	if err != nil {
		_ = conn.Close(websocket.StatusInternalError, "subscribe")
		return
	}
	defer unsubscribe()

	go func() {
		for snap := range snaps {
			emitMatch(ctx, out, matchServerMsg{Type: "snapshot", Snapshot: &snap})
		}
	}()

	// 판 번호는 **결과가 정해진 뒤**에 온다. 구독 채널이 닫히는 것을 기다리면 안 된다 —
	// 그쪽은 끝난 판이 답을 멈출 때라 10분 뒤이고(match.finishedGrace), 그때 사람은
	// 이미 화면을 떠나 있다.
	go func() {
		select {
		case <-table.Finished():
			h.sendRecordID(ctx, out, room, color)
		case <-ctx.Done():
		}
	}()

	h.matchReadLoop(ctx, conn, table, color, out)
}

// matchReadLoop 는 그 사람이 보내는 것을 받는다. **색은 여기서 안 받는다** — 자리에서
// 이미 정해졌고(Hub.Enter), 요청으로 받으면 두 사람이 같은 색을 주장할 수 있다.
func (h *matchHandlerWS) matchReadLoop(
	ctx context.Context,
	conn *websocket.Conn,
	table *match.Table,
	color shogi.Color,
	out chan matchServerMsg,
) {
	for {
		var msg matchClientMsg
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return // 끊겼거나 ctx 종료. **판은 안 접는다** — 상대가 남아 있다
		}

		switch msg.Type {
		case "move":
			if _, err := table.Play(ctx, color, msg.USI); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, match.ErrClosed) {
					return
				}
				emitMatch(ctx, out, matchRejection(err))
			}
			// 성공하면 구독 채널로 스냅샷이 온다 — 두 사람 다에게 간다.

		case "resign":
			if _, err := table.Resign(ctx, color); err != nil && !errors.Is(err, match.ErrFinished) {
				return
			}

		default:
			emitMatch(ctx, out, matchReject("bad_move"))
		}
	}
}

// roomClosedFlush 는 「방이 걷혔다」를 내보내고 기다리는 시간이다. 버퍼 하나를 비우는
// 일이라 짧다 — 길게 잡으면 만료된 방마다 핸들러가 그만큼 살아 있는다.
const roomClosedFlush = time.Second

// matchRecordWait 는 기록이 다 쓰이기를 기다리는 시간이다. 큐를 비우는 일이라 짧다 —
// 넘으면 번호를 포기하고, 그때 화면은 「振り返り」 링크만 안 그린다(결과는 이미 떴다).
const matchRecordWait = 5 * time.Second

// sendRecordID 는 이 사람의 판 번호를 보낸다. **한 판이 행 두 개라 색마다 번호가 다르다.**
//
// **몇 번을 물어도 답한다.** 곁장부가 한 번 받은 번호를 들고 있어서(matchRecords.gameIDOf),
// 판이 끝나는 순간에 새로고침한 사람도 「振り返り」 링크를 받는다.
func (h *matchHandlerWS) sendRecordID(ctx context.Context, out chan matchServerMsg, room *match.Room, color shogi.Color) {
	id, ok := h.records.gameIDOf(ctx, room.ID, color, matchRecordWait)
	if !ok {
		// 기록이 없는 배포이거나, 시한 안에 안 끝났다. 결과와 기보는 이미 화면에 있고
		// 「振り返り」 링크만 안 그려진다.
		return
	}
	emitMatch(ctx, out, matchServerMsg{Type: "record", GameID: id})
}

func matchWriteLoop(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, out <-chan matchServerMsg) {
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

// emitMatch 는 막히지 않게 보낸다. 느린 클라이언트가 테이블을 붙들면 **상대의 시계가
// 그만큼 흐른다**(match.notify 와 같은 이유).
func emitMatch(ctx context.Context, out chan<- matchServerMsg, msg matchServerMsg) {
	select {
	case out <- msg:
	case <-ctx.Done():
	}
}

// matchRecorder 는 `dbRecorder` 를 대인전 쪽 인터페이스에 맞춘다.
//
// **기록기를 한 벌만 두려고 있는 자리다.** 큐가 넘칠 때 버리는 규약, 연결이 끊겨도
// 마저 쓰는 규약, 안 끝난 판을 abandoned 로 닫는 규약이 전부 저쪽에 있고 미묘하다 —
// 두 벌이면 한쪽만 고쳐진다.
type matchRecorder struct{ db *dbRecorder }

func (m matchRecorder) Started(startSFEN string, myColor shogi.Color) {
	m.db.Started(startSFEN, myColor)
}

// Moved 는 확정된 수다. **`by` 를 `SideHuman` 으로 넘긴다** — 기록기가 그 칸을 안 쓰고
// (query/games.sql 의 InsertMove 에 없다) 대인전에는 「engine」이 없다.
func (m matchRecorder) Moved(ply int, usi string) {
	m.db.Moved(ply, usi, game.SideHuman)
}

func (m matchRecorder) Finished(r match.Result) {
	m.db.FinishedWith(store.GameResult(r))
}
