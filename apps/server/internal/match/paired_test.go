package match

import (
	"errors"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 이 파일이 지키는 것은 대기열이 지은 방 하나다(journal §92 · §98). 링크로 만든 방과
// 갈리는 자리만 본다 — 나머지는 room_test.go 가 이미 지킨다.

// 큐 방은 상대가 이미 정해져 있다. 링크 방식과 정반대라, 방 id 를 알아도 제3자가
// 앉을 수 없어야 한다.
func TestPairedRoomAdmitsOnlyTheTwo(t *testing.T) {
	h := newTestHub(t)

	id := NewRoomID()
	room := h.CreatePaired(id, alice, shogi.Black, bob)
	if room.ID != id {
		t.Fatalf("방 id 가 %q, want %q — 짝짓기가 표에 적은 값이 그대로여야 한다", room.ID, id)
	}

	// 두 사람은 각자 정해진 쪽에 앉는다.
	if _, c, err := h.Enter(id, alice); err != nil || c != shogi.Black {
		t.Fatalf("host: got (%v, %v), want (Black, nil)", c, err)
	}
	if _, c, err := h.Enter(id, bob); err != nil || c != shogi.White {
		t.Fatalf("guest: got (%v, %v), want (White, nil)", c, err)
	}

	// 제3자는 없는 방과 같은 답을 받는다.
	if _, err := h.Peek(id, carol.UserID); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("peek by a third person: got %v, want ErrNoRoom", err)
	}
	if _, _, err := h.Enter(id, carol); !errors.Is(err, ErrNoRoom) {
		t.Fatalf("enter by a third person: got %v, want ErrNoRoom", err)
	}
}

// 확인 화면이 안 뜨는 근거가 이 값이다. 손님이 이미 앉아 있으면 방은 waiting 이 아니고,
// 화면은 그때 「참가하시겠습니까」를 안 그린다(screens/match/MatchScreen.tsx).
func TestPairedRoomIsNotWaiting(t *testing.T) {
	h := newTestHub(t)

	room := h.CreatePaired(NewRoomID(), alice, shogi.White, bob)
	for _, c := range []struct {
		name string
		p    Player
		want shogi.Color
	}{{"host", alice, shogi.White}, {"guest", bob, shogi.Black}} {
		seat, waiting := h.SeatOf(room, c.p.UserID)
		if waiting {
			t.Errorf("%s: waiting 이 참이다 — 손님이 이미 앉아 있는 방이다", c.name)
		}
		if seat != c.want {
			t.Errorf("%s: 자리가 %v, want %v", c.name, seat, c.want)
		}
	}
}

// 큐 방은 상한을 안 건드린다. 줄에 선 사람이 따로 열어 둔 초대 링크가 조용히 죽으면
// 안 되고, 반대로 초대 링크를 여는 것이 큐 방을 걷어가서도 안 된다.
func TestPairedRoomAndTheInviteLinkCoexist(t *testing.T) {
	h := newTestHub(t)

	invite := h.Create(alice, shogi.Black)
	paired := h.CreatePaired(NewRoomID(), alice, shogi.Black, bob)

	if _, err := h.Peek(invite.ID, alice.UserID); err != nil {
		t.Errorf("큐 방을 세우면서 초대 링크가 죽었다: %v", err)
	}
	if _, err := h.Peek(paired.ID, alice.UserID); err != nil {
		t.Errorf("큐 방을 못 읽는다: %v", err)
	}

	// 반대 방향. 새 초대 링크가 큐 방을 걷어가지 않는다 — 손님이 앉은 방은 필터에서
	// 이미 빠진다(dropSurplusLocked).
	h.Create(alice, shogi.Black)
	if _, err := h.Peek(paired.ID, alice.UserID); err != nil {
		t.Errorf("새 초대 링크가 큐 방을 걷어갔다: %v", err)
	}
}

// 큐 방도 둘이 다 붙어야 시작한다. 링크 방식과 같은 규약이다 — 한 사람만 와 있는
// 판에서 시계가 돌면 상대가 안 온 것이 시간패가 된다.
func TestPairedRoomStartsOnlyWhenBothConnect(t *testing.T) {
	h := newTestHub(t)

	room := h.CreatePaired(NewRoomID(), alice, shogi.Black, bob)
	detach := h.Connect(room, shogi.Black)
	defer detach()

	select {
	case <-room.Ready():
		t.Fatal("한 사람만 붙었는데 대국이 시작됐다")
	default:
	}

	off := h.Connect(room, shogi.White)
	defer off()
	select {
	case <-room.Ready():
	default:
		t.Fatal("둘이 다 붙었는데 대국이 안 시작됐다")
	}
	if room.Table() == nil {
		t.Fatal("테이블이 없다")
	}
}
