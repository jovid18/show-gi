package match

import (
	"context"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// Room 은 초대 링크 하나다. 정원이 둘이고, 둘이 정해지면 바뀌지 않는다.
//
// 들어오는 규칙은 셋이고 셋이 다 필요하다(journal §83): 로그인한 사람만
// (server/match.go 가 막는다), 유추할 수 없는 id(NewRoomID), 정원 2명.
type Room struct {
	ID string

	// hostColor 는 방을 만든 사람이 고른 先手·後手다. 손님은 나머지를 잡는다.
	hostColor shogi.Color
	host      Player
	createdAt time.Time

	// 아래는 Hub.mu 가 지킨다.
	guest *Player
	// connected 는 先手·後手마다 붙어 있는 연결 수다. 「시작해도 되나」이고, 시작된
	// 뒤의 「상대가 화면을 보고 있나」는 테이블이 따로 센다(table.state.online).
	connected map[shogi.Color]int
	table     *Table
	ready     chan struct{}
	// closed 는 방이 걷혔을 때 닫힌다. ready 와 짝이고 둘 중 하나만 닫힌다.
	closed chan struct{}
	// finishedAt 은 판이 끝난 시각이다. 0이면 아직 끝나지 않았다.
	finishedAt time.Time
}

// Ready 는 두 사람이 다 붙어 대국이 시작된 순간 닫힌다.
func (r *Room) Ready() <-chan struct{} { return r.ready }

// Closed 는 방이 걷혔을 때 닫힌다. 기다리던 연결이 이것으로 안다(journal §83).
func (r *Room) Closed() <-chan struct{} { return r.closed }

// HostName 은 방을 만든 사람의 이름이다. 손님이 들어가기 전에 보는 하나뿐인 정보다.
func (r *Room) HostName() string { return r.host.Name }

// IsHost 는 그 사람이 이 방을 만들었는가다. 잠금이 필요 없다. host 는 생성 뒤로
// 바뀌지 않는 하나뿐인 자리다(guest 는 Hub.mu 가 지킨다).
func (r *Room) IsHost(userID int64) bool { return userID == r.host.UserID }

// Table 은 시작된 대국이다. 아직 시작 전이면 nil — Ready 를 기다린 뒤에 부른다.
func (r *Room) Table() *Table { return r.table }

// Hub 는 방들을 갖고 있다. 프로세스 메모리다 — 방은 DB에 남지 않고, 배포하면 열려
// 있던 방이 사라진다(journal §83).
type Hub struct {
	mu    sync.Mutex
	rooms map[string]*Room

	// ctx 는 테이블의 수명이다. 연결 대신 서버가 준다 — 대인전 판은 한쪽이
	// 끊겨도 살아 있어야 하고(match.go 패키지 주석), 그래서 요청 ctx 에 매달 수 없다.
	ctx context.Context

	cfg HubConfig
}

// HubConfig 는 방을 만드는 데 필요한 것들이다.
type HubConfig struct {
	// NewRecorders 는 대국이 시작될 때 先手·後手마다 기록기를 하나씩 만든다.
	// nil 이면 남지 않는다. 매치 id 를 넘기는 근거는 journal §83.
	NewRecorders func(ctx context.Context, matchID string, black, white Player) map[shogi.Color]Recorder
	// TurnLimit 이 0이면 DefaultTurnLimit.
	TurnLimit time.Duration
	// now 는 테스트가 시계를 잡는 자리다.
	now func() time.Time
	// sweepEvery 가 0이면 sweepInterval. 테스트가 기다리지 않으려고 줄인다.
	sweepEvery time.Duration
}

// NewHub 는 방 저장소를 만든다. ctx 가 끝나면 열려 있던 판이 전부 접힌다(StatusAborted).
func NewHub(ctx context.Context, cfg HubConfig) *Hub {
	if cfg.now == nil {
		cfg.now = time.Now
	}
	if cfg.sweepEvery <= 0 {
		cfg.sweepEvery = sweepInterval
	}
	h := &Hub{rooms: map[string]*Room{}, ctx: ctx, cfg: cfg}
	go h.sweepLoop(ctx, cfg.sweepEvery)
	return h
}

// sweepInterval 은 만료를 훑는 주기다. 정확할 필요가 없다. 이 값만큼 늦게 걷힐 뿐이고
// 걷히는 조건(OpenTTL·FinishedTTL)은 분 단위다.
const sweepInterval = time.Minute

// sweepLoop 은 누구도 Hub 를 건드리지 않아도 만료를 훑는다.
//
// 손이 닿을 때만 훑으면 혼자 기다리는 방이 걷히지 않는다. 방을 만들고 링크를 보낸
// 사람은 Ready·Closed 에 머물러 있을 뿐 Hub 를 부르지 않는다(journal §83).
func (h *Hub) sweepLoop(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.mu.Lock()
			h.sweepLocked(h.cfg.now())
			h.mu.Unlock()
		}
	}
}

// Create 는 방 하나를 연다. 만든 사람이 host 이고 先手·後手를 고른다.
func (h *Hub) Create(host Player, hostColor shogi.Color) *Room {
	id := NewRoomID()
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
	return room
}

// CreatePaired 는 대기열이 지은 짝의 방을 연다. 손님이 처음부터 정해져 있다.
//
// Create 와 갈리는 것이 셋이다(journal §92).
//
//  1. id 를 받는다. 짝짓기가 그 값을 먼저 표에 적으므로(store.PairInQueue) 방이 id 를
//     뽑으면 표와 메모리가 다른 값을 들게 된다.
//  2. 손님이 채워져 있다. 자리가 둘 다 찬 방이라 seatOfLocked 가 그 둘만 통과시킨다.
//  3. 상한을 걸지 않는다(dropSurplusLocked). 부르면 이 사람이 따로 열어 둔 초대 링크가
//     경고 없이 죽는다.
func (h *Hub) CreatePaired(id string, host Player, hostColor shogi.Color, guest Player) *Room {
	now := h.cfg.now()
	seated := guest
	room := &Room{
		ID:        id,
		hostColor: hostColor,
		host:      host,
		createdAt: now,
		guest:     &seated,
		connected: map[shogi.Color]int{},
		ready:     make(chan struct{}),
		closed:    make(chan struct{}),
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.sweepLocked(now)
	h.rooms[id] = room
	return room
}

// openRoomsPerHost 는 한 사람이 아직 시작하지 않은 방을 몇 개까지 가질 수 있나다.
// 넘으면 거절 대신 오래된 것을 버린다(journal §83).
//
// 1이라 「방을 만든다」가 사람마다 멱등이다. 방을 볼 화면도 걷을 API도 없어서
// (match.go) 상한을 넘게 두면 호스트가 모르는 링크가 OpenTTL 동안 살아 있다.
const openRoomsPerHost = 1

// dropSurplusLocked 는 그 사람의 시작하지 않은 방이 상한을 넘으면 오래된 것부터 버린다.
//
// 사람이 걸려 있는 방은 버리지 않는다. 시작한 판(table != nil)뿐 아니라 손님이 앉기만
// 한 방(guest != nil)도 그렇다.
func (h *Hub) dropSurplusLocked(hostID int64) {
	var open []*Room
	for _, room := range h.rooms {
		if room.host.UserID == hostID && room.table == nil && room.guest == nil {
			open = append(open, room)
		}
	}
	if len(open) < openRoomsPerHost {
		return
	}
	// 새 방이 하나 들어올 자리를 비운다.
	slices.SortFunc(open, func(a, b *Room) int { return a.createdAt.Compare(b.createdAt) })
	for _, room := range open[:len(open)-openRoomsPerHost+1] {
		h.dropLocked(room)
	}
}

// dropLocked 는 방을 걷어가고 기다리던 연결에 알린다(journal §83).
//
// 대국이 시작된 방에서는 closed 를 누구도 보지 않는다. 그래도 닫는다. 두 번 닫힐 일은
// 없다 — 삭제가 한 번뿐이다.
func (h *Hub) dropLocked(room *Room) {
	delete(h.rooms, room.ID)
	close(room.closed)
}

// Peek 는 들어가기 전에 그 방을 볼 수 있는가다. 자격이 없으면 ErrNoRoom 하나다.
//
// 자리를 잡지 않는다. 실제로 앉는 것은 WebSocket 이 붙을 때다(Enter).
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
	// 자리가 둘 다 남의 것이다. 없는 방과 같은 답을 준다(ErrNoRoom).
	return nil, ErrNoRoom
}

// Enter 는 자리에 앉는다. 손님 자리가 비어 있으면 여기서 확정되고 그 뒤로 바뀌지 않는다.
//
// 돌려주는 값은 그 사람이 잡는 쪽이다. 요청으로 받으면 두 사람이 같은 쪽을 주장할 수 있다.
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
	// 자기 방에 손님으로 앉을 수 없다. 위 seatOfLocked 가 이미 host 로 답했으므로 여기
	// 오는 것은 다른 사람뿐이고, 한 번 더 보는 것은 그 분기가 바뀌는 날의 방어다.
	if p.UserID == room.host.UserID {
		return room, room.hostColor, nil
	}
	guest := p
	room.guest = &guest
	return room, room.hostColor.Other(), nil
}

// Connect 는 그쪽(先手·後手)의 연결 하나를 단다. 둘이 다 붙어 있으면 그 자리에서
// 대국이 시작된다. 떼는 것은 돌려주는 함수다.
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

// startLocked 는 조건이 차면 대국을 시작한다. 한 번만 시작한다 — room.table 이 그 표식이다.
func (h *Hub) startLocked(room *Room) {
	// 걷힌 방에서는 대국을 시작하지 않는다. Enter 와 Connect 가 잠금을 따로 잡으므로
	// 그 사이에 방이 걷힐 수 있고, 그때 시작하면 ready 와 closed 가 둘 다 닫힌다.
	// 무엇이 깨지는지는 TestADroppedRoomNeverStartsATable.
	if h.rooms[room.ID] != room {
		return
	}
	if room.table != nil || room.guest == nil {
		return
	}
	// 둘이 동시에 붙어 있어야 시작한다(journal §83).
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
		// 平手 초기 국면을 만들 수 없는 경우다. 방은 남기고 만료가 걷어간다.
		log.Printf("match: cannot start the table in room %s: %v", room.ID, err)
		return
	}
	room.table = table
	close(room.ready)

	// 끝나는 시각만 적는다. 방을 걷어가는 것은 만료 쪽이고(sweepLocked), 여기서 지우면
	// 결과 화면을 보고 있는 두 사람의 연결이 그 자리에서 끊긴다.
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

// sweepLocked 는 만료된 방을 걷어간다. Hub 를 건드리는 모든 자리와 sweepLoop 이 부른다.
func (h *Hub) sweepLocked(now time.Time) {
	for _, room := range h.rooms {
		switch {
		case !room.finishedAt.IsZero():
			if now.Sub(room.finishedAt) > FinishedTTL {
				h.dropLocked(room)
			}
		case room.table == nil && now.Sub(room.createdAt) > OpenTTL:
			// 링크가 곧 열쇠라 시작되지 않은 방을 오래 두지 않는다(roomIDLen).
			h.dropLocked(room)
		}
	}
}

// Rooms 는 지금 갖고 있는 방 수다. 테스트와 운영 확인용이다.
func (h *Hub) Rooms() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.rooms)
}

// SeatOf 는 그 사람이 이 방에서 잡을 쪽과, 아직 상대를 기다리는가다.
//
// 자격 검사를 하지 않는다. Peek 를 통과한 사람에게만 뜻이 있고, 아직 앉지 않은
// 사람에게는 「앉는다면 어느 쪽인가」를 답한다.
func (h *Hub) SeatOf(room *Room, userID int64) (seat shogi.Color, waiting bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	waiting = room.guest == nil
	if s := room.seatOfLocked(userID); s != nil {
		return *s, waiting
	}
	return room.hostColor.Other(), waiting
}
