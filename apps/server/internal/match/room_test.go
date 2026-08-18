package match

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 이 파일이 지키는 것은 **방에 누가 들어올 수 있나** 하나다. 룰과 시계는 table_test.go.

func newTestHub(t *testing.T) *Hub {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return NewHub(ctx, HubConfig{TurnLimit: time.Minute})
}

var (
	alice = Player{UserID: 1, Name: "アリス"}
	bob   = Player{UserID: 2, Name: "ボブ"}
	carol = Player{UserID: 3, Name: "キャロル"}
)

// 방 id 는 **훑어볼 수 없어야 한다.** 연번이면 로그인한 아무나 남의 방을 열 수 있고,
// 그러면 정원 2명이라는 규칙이 「먼저 훑은 사람이 이긴다」가 된다.
func TestRoomIDIsUnguessable(t *testing.T) {
	h := newTestHub(t)

	seen := map[string]bool{}
	for range 500 {
		room, err := h.Create(alice, shogi.Black)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		// 128비트를 base64url 로 적으면 22자다. 짧아지면 엔트로피가 줄어든 것이다.
		if len(room.ID) != 22 {
			t.Fatalf("room id %q is %d chars, want 22", room.ID, len(room.ID))
		}
		if seen[room.ID] {
			t.Fatalf("room id %q came out twice", room.ID)
		}
		seen[room.ID] = true
	}
}

// 없는 방과 남의 방이 **같은 답**이어야 한다. 갈리면 그 차이만으로 「그 방은 있다」를
// 알 수 있고, 그게 곧 훑어보기의 시작이다.
func TestUnknownAndFullRoomsLookTheSame(t *testing.T) {
	h := newTestHub(t)

	room, err := h.Create(alice, shogi.Black)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// 밥이 손님 자리를 잡는다.
	if _, _, err := h.Enter(room.ID, bob); err != nil {
		t.Fatalf("bob cannot enter: %v", err)
	}

	// 캐롤은 자리가 없다.
	if _, err := h.Peek(room.ID, carol.UserID); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("peek by a third person: got %v, want ErrNoRoom", err)
	}
	if _, _, err := h.Enter(room.ID, carol); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("enter by a third person: got %v, want ErrNoRoom", err)
	}
	// 없는 방도 같은 답이다.
	if _, err := h.Peek("AAAAAAAAAAAAAAAAAAAAAA", carol.UserID); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("peek of an unknown room: got %v, want ErrNoRoom", err)
	}
}

// 자리는 **한 번 정해지면 안 바뀐다.** 다시 들어와도 같은 색이라야 끊겼다 붙는 사람이
// 남의 자리에 앉지 않는다.
func TestSeatsAreSticky(t *testing.T) {
	h := newTestHub(t)

	room, err := h.Create(alice, shogi.White) // 방을 만든 사람이 後手를 골랐다
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, c, err := h.Enter(room.ID, alice); err != nil || c != shogi.White {
		t.Fatalf("host: got (%v, %v), want (White, nil)", c, err)
	}
	if _, c, err := h.Enter(room.ID, bob); err != nil || c != shogi.Black {
		t.Fatalf("guest: got (%v, %v), want (Black, nil)", c, err)
	}
	// 둘 다 다시 들어와도 같은 색이다.
	if _, c, err := h.Enter(room.ID, bob); err != nil || c != shogi.Black {
		t.Fatalf("guest again: got (%v, %v), want (Black, nil)", c, err)
	}
	if _, c, err := h.Enter(room.ID, alice); err != nil || c != shogi.White {
		t.Fatalf("host again: got (%v, %v), want (White, nil)", c, err)
	}
}

// **혼자 두는 판이 생기면 안 된다.** 방을 만든 사람이 자기 링크를 열어도 손님 자리는
// 안 찬다 — 차면 그 방은 그 사람 혼자 두 색을 잡은 판이 된다.
func TestHostCannotTakeTheGuestSeat(t *testing.T) {
	h := newTestHub(t)

	room, err := h.Create(alice, shogi.Black)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := h.Enter(room.ID, alice); err != nil {
		t.Fatalf("host enter: %v", err)
	}

	// 손님 자리가 그대로 비어 있어야 한다 — 밥이 들어갈 수 있다.
	if _, c, err := h.Enter(room.ID, bob); err != nil || c != shogi.White {
		t.Fatalf("guest after the host opened the link: got (%v, %v), want (White, nil)", c, err)
	}
}

// 판은 **둘이 동시에 붙어 있을 때** 선다. 「들어온 적이 있다」로 세면, 링크를 보내고
// 탭을 닫은 방 주인이 손님이 여는 순간 시간패로 진다 — 그 자리에서 시계가 돌기 때문이다.
func TestTableStartsOnlyWhenBothAreConnected(t *testing.T) {
	h := newTestHub(t)

	room, err := h.Create(alice, shogi.Black)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := h.Enter(room.ID, bob); err != nil {
		t.Fatalf("guest enter: %v", err)
	}

	// 손님만 붙어 있다 — 아직 안 선다.
	detachBob := h.Connect(room, shogi.White)
	select {
	case <-room.Ready():
		t.Fatal("the table started with only one side connected")
	default:
	}

	// 방 주인이 붙으면 그 자리에서 선다.
	detachAlice := h.Connect(room, shogi.Black)
	select {
	case <-room.Ready():
	default:
		t.Fatal("the table did not start when both sides were connected")
	}
	if room.Table() == nil {
		t.Fatal("Ready closed but Table() is nil")
	}

	// 한쪽이 나가도 판은 그대로다 — 끝내는 것은 시계뿐이다.
	detachBob()
	if room.Table() == nil {
		t.Fatal("the table went away when one side disconnected")
	}
	detachAlice()
}

// 아무도 안 들어온 방은 만료된다. **링크가 곧 열쇠라 오래 사는 열쇠를 안 둔다.**
func TestOpenRoomExpires(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	now := time.Now()
	h := NewHub(ctx, HubConfig{now: func() time.Time { return now }})

	room, err := h.Create(alice, shogi.Black)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	now = now.Add(OpenTTL + time.Minute)
	if _, err := h.Peek(room.ID, alice.UserID); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("an expired room still answers: %v", err)
	}
	if h.Rooms() != 0 {
		t.Fatalf("the expired room is still held: %d", h.Rooms())
	}
	// **기다리던 연결이 그것을 알아야 한다.** 안 알리면 그 화면은 영영 「상대를
	// 기다립니다」에 서 있고, 그동안 **이미 죽은 링크를 광고한다.**
	select {
	case <-room.Closed():
	default:
		t.Fatal("the room expired without telling the sockets waiting on it")
	}
}

// 상한에 걸려 밀려난 방도 같다 — **걷히는 자리가 둘이라 둘 다 알려야 한다.**
func TestARoomDroppedForTheCapTellsItsWaiters(t *testing.T) {
	h := newTestHub(t)

	first, err := h.Create(alice, shogi.Black)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	for range openRoomsPerHost {
		if _, err := h.Create(alice, shogi.Black); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	select {
	case <-first.Closed():
	default:
		t.Fatal("the room was dropped for the cap without telling the sockets waiting on it")
	}
}

// **손님이 앉은 방은 상한에 안 걸린다.** 걷어가면 그 손님은 영영 기다리고, 방 주인은
// 자기 방에 다시 못 들어간다.
func TestARoomWithASeatedGuestSurvivesTheCap(t *testing.T) {
	h := newTestHub(t)

	seated, err := h.Create(alice, shogi.Black)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := h.Enter(seated.ID, bob); err != nil {
		t.Fatalf("guest enter: %v", err)
	}

	for range 6 {
		if _, err := h.Create(alice, shogi.Black); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	if _, err := h.Peek(seated.ID, alice.UserID); err != nil {
		t.Fatalf("the room with a seated guest was dropped: %v", err)
	}
}

// **한 사람이 방을 무한히 만들 수 없다.** 방은 프로세스 메모리에 있고 만료가 30분이라,
// 상한이 없으면 로그인한 사람 하나가 그동안 계속 쌓을 수 있다.
func TestOpenRoomsPerHostAreCapped(t *testing.T) {
	h := newTestHub(t)

	ids := make([]string, 0, 6)
	for range 6 {
		room, err := h.Create(alice, shogi.Black)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		ids = append(ids, room.ID)
	}

	if h.Rooms() != openRoomsPerHost {
		t.Fatalf("the hub holds %d rooms, want %d", h.Rooms(), openRoomsPerHost)
	}
	// **마지막 것들이 산다** — 방을 또 만든 사람이 원하는 것은 마지막 링크다.
	for _, id := range ids[len(ids)-openRoomsPerHost:] {
		if _, err := h.Peek(id, alice.UserID); err != nil {
			t.Fatalf("the newest rooms should survive, but %s is gone: %v", id, err)
		}
	}
	if _, err := h.Peek(ids[0], alice.UserID); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("the oldest room survived: %v", err)
	}
}

// **시작한 판은 상한에 안 걸린다.** 걷어가면 두는 중인 두 사람이 그 자리에서 판을 잃는다.
func TestAStartedGameIsNeverDroppedForTheCap(t *testing.T) {
	h := newTestHub(t)

	live, err := h.Create(alice, shogi.Black)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := h.Enter(live.ID, bob); err != nil {
		t.Fatalf("guest enter: %v", err)
	}
	h.Connect(live, shogi.White)
	h.Connect(live, shogi.Black)
	if live.Table() == nil {
		t.Fatal("the table did not start")
	}

	// 그 뒤로 방을 잔뜩 만들어도 두는 판은 그대로다.
	for range 6 {
		if _, err := h.Create(alice, shogi.Black); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	if _, err := h.Peek(live.ID, alice.UserID); err != nil {
		t.Fatalf("the live game was dropped for the cap: %v", err)
	}
}

// **걷힌 방에는 판을 안 세운다.**
//
// `Enter` 와 `Connect` 가 잠금을 따로 잡으므로 그 사이에 방이 걷힐 수 있다. 그때 판을
// 세우면 `ready` 와 `closed` 가 둘 다 닫히고, 두 handler 의 select 가 무작위로 갈려서
// 한 사람은 판에 앉고 다른 사람은 「期限が切れました」를 본다 — 그 판은 60초 뒤 시간패로
// 끝나고 아무도 못 본 대국의 행 둘이 남는다.
func TestADroppedRoomNeverStartsATable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	now := time.Now()
	h := NewHub(ctx, HubConfig{now: func() time.Time { return now }})

	room, err := h.Create(alice, shogi.Black)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, _, err := h.Enter(room.ID, bob); err != nil {
		t.Fatalf("guest enter: %v", err)
	}
	h.Connect(room, shogi.White)

	// 그 사이에 방이 걷힌다 — 여기서는 만료로 민다.
	now = now.Add(OpenTTL + time.Minute)
	if _, err := h.Peek("anything", alice.UserID); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("peek: %v", err)
	}
	select {
	case <-room.Closed():
	default:
		t.Fatal("the room was swept without telling its waiters")
	}

	// 남은 한쪽이 이제 붙는다. **판이 서면 안 된다.**
	h.Connect(room, shogi.Black)
	if room.Table() != nil {
		t.Fatal("a table was started in a room that had already been dropped")
	}
	select {
	case <-room.Ready():
		t.Fatal("ready and closed are both closed — the two handlers would split at random")
	default:
	}
}
