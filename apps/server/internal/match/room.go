package match

import (
	"context"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// Room 은 초대 링크 하나다. **정원이 둘이고, 둘이 정해지면 안 바뀐다.**
//
// 방을 여는 규칙이 이 타입의 전부다:
//
//  1. **로그인한 사람만** — 방을 만드는 것도 들어가는 것도(server/match.go 가 막는다).
//     익명은 서로 구별할 수단이 없어서 「이 방의 상대가 아까 그 사람인가」에 답할 수 없다.
//  2. **id 를 유추할 수 없다** — 128비트 난수다(newRoomID).
//  3. **두 자리가 차면 그 뒤로는 아무도 못 들어간다** — 링크가 유출돼도 관전조차 안 된다.
//
// 셋이 다 필요하다. 1이 없으면 링크를 받은 누구나 남의 자리에 앉고, 2가 없으면 링크가
// 필요 없어지고, 3이 없으면 링크를 받은 세 번째 사람이 판을 들여다본다.
type Room struct {
	ID string

	// hostColor 는 방을 만든 사람이 고른 先手·後手다. 손님은 나머지를 잡는다.
	hostColor shogi.Color
	host      Player
	createdAt time.Time

	// 아래는 Hub.mu 가 지킨다.
	guest *Player
	// connected 는 先手·後手마다 붙어 있는 연결 수다. **대국이 시작되기 전에만 쓴다** — 시작된 뒤로는
	// 테이블이 자기 것으로 센다(table.state.online). 뜻이 다르다: 이쪽은 「판을 시작해도
	// 되나」이고 저쪽은 「상대가 화면을 보고 있나」다.
	connected map[shogi.Color]int
	table     *Table
	ready     chan struct{}
	// closed 는 **방이 걷혔을 때** 닫힌다. `ready` 와 짝이고 둘 중 하나만 닫힌다 —
	// 이게 없으면 만료된 방에서 기다리던 사람이 영영 「상대를 기다립니다」에 서 있고,
	// 그동안 그 화면이 **이미 죽은 링크를 광고한다.**
	closed chan struct{}
	// finishedAt 은 판이 끝난 시각이다. 0이면 아직 안 끝났다.
	finishedAt time.Time
}

// Ready 는 두 사람이 다 붙어 대국이 시작된 순간 닫힌다.
func (r *Room) Ready() <-chan struct{} { return r.ready }

// Closed 는 **방이 걷혔을 때** 닫힌다. 기다리던 연결이 그것을 알아야 화면이
// 「아직 기다리는 중」에서 벗어난다.
func (r *Room) Closed() <-chan struct{} { return r.closed }

// HostName 은 방을 만든 사람의 이름이다. **손님이 들어가기 전에 보는 유일한 정보다.**
func (r *Room) HostName() string { return r.host.Name }

// IsHost 는 그 사람이 이 방을 만들었는가다. **잠금이 필요 없다** — host 는 생성 뒤로
// 안 바뀌는 유일한 자리다(guest 는 Hub.mu 가 지킨다).
func (r *Room) IsHost(userID int64) bool { return userID == r.host.UserID }

// Table 은 시작된 대국이다. 아직 시작 전이면 nil — `Ready` 를 기다린 뒤에 부른다.
func (r *Room) Table() *Table { return r.table }

// Hub 는 방들을 들고 있다. **프로세스 메모리다** — 방은 DB에 안 남는다.
//
// 배포하면 열려 있던 방이 사라진다. 그래도 되는 이유는 **판이 끝나는 자리에서 이미
// 기록에 남기 때문**이고(Recorder), 두는 중에 배포가 끼면 엔진 대국도 지금 그 자리에서
// 끊긴다 — 대인전만 더 잃는 것이 없다.
type Hub struct {
	mu    sync.Mutex
	rooms map[string]*Room

	// ctx 는 **테이블의 수명**이다. 연결이 아니라 서버가 준다 — 대인전 판은 한쪽이
	// 끊겨도 살아 있어야 하고(match.go 패키지 주석), 그래서 요청 ctx 에 매달 수 없다.
	ctx context.Context

	cfg HubConfig
}

// HubConfig 는 방을 세우는 데 필요한 것들이다.
type HubConfig struct {
	// NewRecorders 는 대국이 시작될 때 先手·後手마다 기록기를 하나씩 만든다. nil 이면 안 남는다.
	//
	// **매치 id 를 넘긴다** — 한 판이 `games` 행 두 개로 남으므로 그 둘을 나중에 다시
	// 묶을 열쇠가 필요하다(journal §83).
	NewRecorders func(ctx context.Context, matchID string, black, white Player) map[shogi.Color]Recorder
	// TurnLimit 이 0이면 DefaultTurnLimit.
	TurnLimit time.Duration
	// now 는 테스트가 시계를 잡는 자리다.
	now func() time.Time
}

// NewHub 는 방 저장소를 만든다. ctx 가 끝나면 열려 있던 판이 전부 접힌다(StatusAborted).
func NewHub(ctx context.Context, cfg HubConfig) *Hub {
	if cfg.now == nil {
		cfg.now = time.Now
	}
	return &Hub{rooms: map[string]*Room{}, ctx: ctx, cfg: cfg}
}

// Create 는 방 하나를 연다. **만든 사람이 host 이고 先手·後手를 고른다.**
func (h *Hub) Create(host Player, hostColor shogi.Color) (*Room, error) {
	id, err := newRoomID()
	if err != nil {
		return nil, err
	}
	now := h.cfg.now()
	room := &Room{
		ID:        id,
		hostColor: hostColor,
		host:      host,
		createdAt: now,
		connected: map[shogi.Color]int{},
		ready:     make(chan struct{}),
		closed:    make(chan struct{}),
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweepLocked(now)
	h.dropSurplusLocked(host.UserID)
	h.rooms[id] = room
	return room, nil
}

// openRoomsPerHost 는 한 사람이 **아직 안 시작한** 방을 몇 개까지 들고 있을 수 있나다.
//
// **상한이 없으면 로그인한 사람 하나가 방을 무한히 만든다.** 방은 프로세스 메모리에
// 있으므로(Hub) 그것이 곧 이 서버의 메모리이고, 만료가 30분이라 그동안 계속 쌓인다.
//
// 넘으면 **거절이 아니라 오래된 것을 버린다.** 거절은 「왜 안 되나」를 화면에 설명해야
// 하는데, 방을 여러 개 만든 사람이 실제로 원하는 것은 대개 마지막 링크 하나다.
//
// **[미확정]** 3은 감으로 잡은 값이다.
const openRoomsPerHost = 3

// dropSurplusLocked 는 그 사람의 안 시작한 방이 상한을 넘으면 오래된 것부터 버린다.
//
// **사람이 걸려 있는 방은 절대 안 버린다** — 시작한 판(`table != nil`)은 물론이고
// **손님이 앉기만 한 방**(`guest != nil`)도 그렇다. 걷어가면 그쪽에서 기다리던 사람이
// 영영 기다리고, 방 주인은 자기 방에 다시 못 들어간다.
func (h *Hub) dropSurplusLocked(hostID int64) {
	var open []*Room
	for _, room := range h.rooms {
		// **손님이 앉은 방도 안 버린다.** `table == nil` 만 보면, 손님이 들어와 방 주인을
		// 기다리는 방이 「안 시작한 방」으로 세어져 버려진다 — 그러면 그 손님은 영영
		// 기다리고 방 주인은 자기 방에 다시 못 들어간다(404).
		if room.host.UserID == hostID && room.table == nil && room.guest == nil {
			open = append(open, room)
		}
	}
	if len(open) < openRoomsPerHost {
		return
	}
	// 만든 시각 순으로 오래된 것부터. 새 방이 하나 들어올 자리를 비운다.
	slices.SortFunc(open, func(a, b *Room) int { return a.createdAt.Compare(b.createdAt) })
	for _, room := range open[:len(open)-openRoomsPerHost+1] {
		h.dropLocked(room)
	}
}

// dropLocked 는 방을 걷어가고 **기다리던 연결에 알린다.** 지우기만 하면 그쪽은 영영
// 기다린다 — 방이 없어진 것을 알 길이 그 채널뿐이다.
//
// `ready` 가 이미 닫혔으면(대국이 시작됐으면) `closed` 는 아무도 안 본다. 그래도 닫는 것은
// 「걷혔다」가 방의 사실이기 때문이고, 두 번 닫힐 일은 없다 — 삭제가 한 번뿐이다.
func (h *Hub) dropLocked(room *Room) {
	delete(h.rooms, room.ID)
	close(room.closed)
}

// Peek 는 **들어가기 전에** 그 방을 볼 수 있는가다. 자격이 없으면 ErrNoRoom 하나다.
//
// 자리를 잡지 않는다 — 손님이 「◯◯さんの対局」을 확인하는 화면이 이 답으로 서고,
// 실제로 앉는 것은 WebSocket 이 붙을 때다(Enter).
func (h *Hub) Peek(id string, userID int64) (*Room, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweepLocked(h.cfg.now())

	room, ok := h.rooms[id]
	if !ok {
		return nil, ErrNoRoom
	}
	if room.seatOfLocked(userID) != nil || room.guest == nil {
		return room, nil
	}
	// 자리가 둘 다 남의 것이다. **없는 방과 같은 답을 준다**(ErrNoRoom).
	return nil, ErrNoRoom
}

// Enter 는 자리에 앉는다. 손님 자리가 비어 있으면 **여기서 확정되고 그 뒤로 안 바뀐다.**
//
// 돌려주는 값은 그 사람이 잡는 쪽이다. 그것을 클라이언트가 보내지 않는 것이 요점 —
// 요청으로 받으면 두 사람이 같은 쪽을 주장할 수 있다.
func (h *Hub) Enter(id string, p Player) (*Room, shogi.Color, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweepLocked(h.cfg.now())

	room, ok := h.rooms[id]
	if !ok {
		return nil, shogi.Black, ErrNoRoom
	}
	if seat := room.seatOfLocked(p.UserID); seat != nil {
		return room, *seat, nil
	}
	if room.guest != nil {
		return nil, shogi.Black, ErrNoRoom // 자리가 없다. 없는 방과 같은 답
	}
	// **자기 방에 손님으로 못 앉는다** — seatOfLocked 가 이미 host 로 답했으므로 여기
	// 오는 것은 다른 사람뿐이다. 그래도 한 번 더 보는 것은 나중에 위 분기가 바뀌었을 때
	// 혼자 두는 판이 조용히 생기는 것을 막기 위해서다.
	if p.UserID == room.host.UserID {
		return room, room.hostColor, nil
	}
	guest := p
	room.guest = &guest
	return room, room.hostColor.Other(), nil
}

// Connect 는 그쪽(先手·後手)의 연결 하나를 단다. 둘이 다 붙어 있으면 **그 자리에서 대국이 시작된다.**
//
// 돌려주는 함수를 부르면 떨어진다. 대국이 시작된 뒤로는 이 카운트가 아무것도 안 정한다 —
// 화면에 나가는 접속 표시는 테이블이 따로 센다(table.state.online).
func (h *Hub) Connect(room *Room, c shogi.Color) func() {
	h.mu.Lock()
	room.connected[c]++
	h.startLocked(room)
	h.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			if room.connected[c] > 0 {
				room.connected[c]--
			}
			h.mu.Unlock()
		})
	}
}

// startLocked 는 조건이 차면 대국을 시작한다. **한 번만 시작한다** — `room.table` 이 그 표식이다.
func (h *Hub) startLocked(room *Room) {
	// **걷힌 방에서는 대국을 시작하지 않는다.** `Enter` 와 `Connect` 가 잠금을 따로 잡으므로 그
	// 사이에 이 방이 걷힐 수 있고(만료·상한), 그때 대국을 시작하면 `ready` 와 `closed` 가
	// **둘 다** 닫힌다 — 두 handler 의 select 가 무작위로 갈려서 한 사람은 판에 앉고
	// 다른 사람은 「期限が切れました」를 보게 된다. 그 판은 60초 뒤 시간패로 끝나고,
	// 아무도 못 본 대국의 행 둘이 남는다.
	if h.rooms[room.ID] != room {
		return
	}
	if room.table != nil || room.guest == nil {
		return
	}
	// **둘이 동시에 붙어 있어야 시작한다.** 「들어온 적이 있다」로 세면, 링크를 보내고
	// 탭을 닫은 방 주인이 손님이 여는 순간 시간패로 진다 — 시계가 그 자리에서 돌기 때문이다.
	if room.connected[shogi.Black] == 0 || room.connected[shogi.White] == 0 {
		return
	}

	black, white := room.host, *room.guest
	if room.hostColor == shogi.White {
		black, white = *room.guest, room.host
	}

	var recorders map[shogi.Color]Recorder
	if h.cfg.NewRecorders != nil {
		recorders = h.cfg.NewRecorders(h.ctx, room.ID, black, white)
	}
	table, err := NewTable(h.ctx, Config{
		Black:     black,
		White:     white,
		Recorders: recorders,
		TurnLimit: h.cfg.TurnLimit,
		now:       h.cfg.now,
	})
	if err != nil {
		// 평수 초기 국면이 안 서는 경우다 — 실질적으로 없다. 방을 그대로 두면 두 화면이
		// 영영 기다리므로 남기고, 링크는 만료가 걷어간다.
		log.Printf("match: cannot start the table in room %s: %v", room.ID, err)
		return
	}
	room.table = table
	close(room.ready)

	// 끝나는 시각을 적어 둔다. 방을 걷어가는 것은 만료 쪽이고(sweepLocked), 여기서
	// 지우면 결과 화면을 보고 있는 두 사람의 연결이 그 자리에서 끊긴다.
	go func() {
		<-table.Finished()
		h.mu.Lock()
		room.finishedAt = h.cfg.now()
		h.mu.Unlock()
	}()
}

// seatOfLocked 는 그 사람이 이 방에서 잡은 쪽이다. 자리가 없으면 nil.
func (r *Room) seatOfLocked(userID int64) *shogi.Color {
	if userID == r.host.UserID {
		c := r.hostColor
		return &c
	}
	if r.guest != nil && userID == r.guest.UserID {
		c := r.hostColor.Other()
		return &c
	}
	return nil
}

// sweepLocked 는 만료된 방을 걷어간다. **따로 도는 goroutine 을 안 둔다** — 방이 많아야
// 수십이고, 손이 닿을 때 훑는 것으로 충분하다.
func (h *Hub) sweepLocked(now time.Time) {
	for _, room := range h.rooms {
		switch {
		case !room.finishedAt.IsZero():
			if now.Sub(room.finishedAt) > FinishedTTL {
				h.dropLocked(room)
			}
		case room.table == nil && now.Sub(room.createdAt) > OpenTTL:
			// 대국이 시작되지 않은 방이다. **링크가 곧 열쇠라 오래 두지 않는다**(roomIDBytes).
			h.dropLocked(room)
		}
	}
}

// Rooms 는 지금 들고 있는 방 수다. 테스트와 운영 확인용이다.
func (h *Hub) Rooms() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.rooms)
}

// SeatOf 는 그 사람이 이 방에서 잡을 쪽과, 아직 상대를 기다리는가다.
//
// **자격 검사를 안 한다** — `Peek` 를 통과한 사람에게만 뜻이 있다. 아직 안 앉은 사람에게는
// 「앉는다면 어느 쪽인가」를 답한다: 방을 만든 사람이 먼저 골랐으므로 손님 몫은 하나다.
func (h *Hub) SeatOf(room *Room, userID int64) (seat shogi.Color, waiting bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	waiting = room.guest == nil
	if s := room.seatOfLocked(userID); s != nil {
		return *s, waiting
	}
	return room.hostColor.Other(), waiting
}
