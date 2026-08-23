package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/auth"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/match"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 이 파일이 지키는 것은 줄에 선 두 사람이 같은 방에서 만나는가다. 고르는 규칙 자체는
// internal/queue 가 DB 없이 지킨다.
//
// 진짜 DB가 필요하다. 큐가 표이고(016_match_queue.sql) 여기서 재는 것 대부분이
// 「두 요청이 같은 줄을 본다」라서, 표를 흉내내면 잴 것이 남지 않는다.
//
//	SHOWGI_TEST_DATABASE_URL=postgres://showgi:showgi@localhost:5432/showgi go test ./internal/server/
type queueFixture struct {
	h     http.Handler
	hub   *match.Hub
	store *store.Store
	// users 는 로그인 쿠키가 붙은 사람들이다. 실행마다 새로 만든다.
	users []queueUser
}

type queueUser struct {
	id     int64
	name   string
	cookie *http.Cookie
}

func queueServer(t *testing.T, people int) queueFixture {
	t.Helper()
	url := os.Getenv("SHOWGI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SHOWGI_TEST_DATABASE_URL 미설정 — DB 테스트 건너뜀")
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	m := NewMatch(ctx, st, intervene.Beginner)
	opts := Options{
		Google:        auth.NewGoogle("client-id", "client-secret"),
		SessionSecret: "session-secret",
		Store:         st,
		Match:         m,
	}
	codec := auth.NewCodec(opts.SessionSecret)

	// 실행마다 다른 사람이어야 한다. 남은 큐 행을 물려받으면 두 번째 실행에서 짝이
	// 엉뚱하게 잡힌다 — 큐는 「지금 서 있는 사람」이 전부인 표라 그 사고가 조용하다.
	stamp := t.Name() + "-" + time.Now().Format("150405.000000000")
	users := make([]queueUser, 0, people)
	for i := range people {
		uid := stamp + "-" + string(rune('a'+i))
		id, err := st.UpsertUser(t.Context(), "test", uid, uid)
		if err != nil {
			t.Fatalf("upsert %s: %v", uid, err)
		}
		t.Cleanup(func() {
			// users 를 지우면 큐 행이 딸려 간다(ON DELETE CASCADE).
			if _, err := st.Pool().Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id); err != nil {
				t.Errorf("정리: %v", err)
			}
		})
		value, err := codec.Encode(id, uid, time.Now())
		if err != nil {
			t.Fatalf("encode session: %v", err)
		}
		users = append(users, queueUser{id: id, name: uid, cookie: &http.Cookie{Name: sessionCookie, Value: value}})
	}
	return queueFixture{h: Handler(opts), hub: m.hub, store: st, users: users}
}

// poll 은 줄에 서거나 다시 물어본다. 화면이 하는 것과 같은 호출이다.
func (f queueFixture) poll(t *testing.T, u queueUser) queuePayload {
	t.Helper()
	rec := do(f.h, http.MethodPost, "/api/queue", u.cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/queue: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var got queuePayload
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return got
}

// 로그인하지 않으면 줄에 못 선다. 익명은 서로 구별할 수단이 없어서 짝짓기가 성립하지 않는다.
func TestJoiningTheQueueNeedsSignIn(t *testing.T) {
	f := queueServer(t, 1)

	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		rec := do(f.h, method, "/api/queue", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want %d", method, rec.Code, http.StatusUnauthorized)
		}
	}
}

// 혼자 서면 안 잡힌다. 동시 접속자가 없으면 안 잡히는 것을 그대로 받아들이기로
// 정했고(journal §92), 그때 화면이 말할 것이 「줄에 몇 명인가」다.
func TestOneWaiterIsNotPaired(t *testing.T) {
	f := queueServer(t, 1)

	got := f.poll(t, f.users[0])
	if got.Status != queueStatusWaiting {
		t.Fatalf("status = %q, want %q", got.Status, queueStatusWaiting)
	}
	if got.Waiting != 1 {
		t.Errorf("줄에 %d명, want 1", got.Waiting)
	}
	if got.RoomID != "" {
		t.Errorf("방이 %q 인데 짝이 없다", got.RoomID)
	}
}

// 다시 물어보는 것이 멱등이다. 줄이 늘지 않고 선 시각도 안 밀린다 — 밀리면 밴드가
// 매 재시도마다 처음으로 돌아가서 영영 안 넓어진다.
func TestPollingDoesNotResetTheWait(t *testing.T) {
	f := queueServer(t, 1)

	first := f.poll(t, f.users[0])
	time.Sleep(20 * time.Millisecond)
	second := f.poll(t, f.users[0])

	if second.Waiting != 1 {
		t.Errorf("줄에 %d명, want 1 — 같은 사람이 두 줄이 됐다", second.Waiting)
	}
	if second.WaitedMs < first.WaitedMs {
		t.Errorf("기다린 시간이 %dms → %dms 로 줄었다", first.WaitedMs, second.WaitedMs)
	}
}

// 둘이 서면 짝이 잡힌다. 두 번째로 부른 쪽이 그 자리에서 방을 받고, 먼저 선 쪽은
// 다시 물어볼 때 같은 방을 반대 색으로 받는다.
func TestTwoWaitersMeetInOneRoom(t *testing.T) {
	f := queueServer(t, 2)
	first, second := f.users[0], f.users[1]

	if got := f.poll(t, first); got.Status != queueStatusWaiting {
		t.Fatalf("먼저 선 사람: status = %q, want %q", got.Status, queueStatusWaiting)
	}

	paired := f.poll(t, second)
	if paired.Status != queueStatusMatched || paired.RoomID == "" {
		t.Fatalf("나중에 선 사람: %+v, want matched", paired)
	}

	told := f.poll(t, first)
	if told.Status != queueStatusMatched {
		t.Fatalf("먼저 선 사람: status = %q, want %q", told.Status, queueStatusMatched)
	}
	if told.RoomID != paired.RoomID {
		t.Fatalf("방이 %q / %q — 두 사람이 다른 방으로 갔다", told.RoomID, paired.RoomID)
	}
	if told.YourColor == paired.YourColor {
		t.Fatalf("둘 다 %q 다 — 한 판에 같은 쪽이 둘일 수 없다", told.YourColor)
	}

	// 자리가 한 번만 나간다. 두 번 나가면 화면이 두 번 방으로 가고, 그 방은 정원이 둘이다.
	if again := f.poll(t, first); again.Status != queueStatusWaiting {
		t.Errorf("같은 자리를 두 번 받았다: %+v", again)
	}
	// 위 호출로 다시 줄에 섰다. 뒷정리는 users 삭제가 한다(CASCADE).

	// 그 방은 두 사람의 것이다. 제3자는 없는 방과 같은 답을 받는다(match.Hub.CreatePaired).
	if _, err := f.hub.Peek(paired.RoomID, first.id); err != nil {
		t.Errorf("먼저 선 사람이 방을 못 본다: %v", err)
	}
	if _, err := f.hub.Peek(paired.RoomID, second.id); err != nil {
		t.Errorf("나중에 선 사람이 방을 못 본다: %v", err)
	}
	if _, err := f.hub.Peek(paired.RoomID, first.id+second.id+1); !errors.Is(err, match.ErrNoRoom) {
		t.Errorf("제3자가 방을 봤다: %v", err)
	}
}

// 확인 화면이 안 뜬다. 손님이 처음부터 앉아 있어서 방이 waiting 이 아니고, 화면은
// 그때 「참가하시겠습니까」를 안 그린다(journal §92).
func TestAPairedRoomSkipsTheJoinScreen(t *testing.T) {
	f := queueServer(t, 2)

	f.poll(t, f.users[0])
	paired := f.poll(t, f.users[1])
	if paired.Status != queueStatusMatched {
		t.Fatalf("짝이 안 잡혔다: %+v", paired)
	}

	for _, u := range f.users {
		rec := do(f.h, http.MethodGet, "/api/rooms/"+paired.RoomID, u.cookie)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: GET room status = %d", u.name, rec.Code)
		}
		var room roomPayload
		if err := json.Unmarshal(rec.Body.Bytes(), &room); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if room.Waiting {
			t.Errorf("%s: waiting 이 참이다 — 자리가 둘 다 찬 방이다", u.name)
		}
	}
}

// 밴드 밖의 두 사람은 안 붙는다. 레이팅이 굳은(불확실성이 작은) 두 사람을 멀리 떨어뜨려
// 두면 밴드가 그 격차를 못 덮는다(queue.Band).
func TestFarApartWaitersAreNotPaired(t *testing.T) {
	f := queueServer(t, 2)
	strong, weak := f.users[0], f.users[1]

	// 판을 둔 사람들이라 불확실성이 하한 근처다. 밴드가 Base0 + 두 사람의 RD 이므로
	// 격차 1400은 어느 쪽 밴드로도 안 덮인다.
	err := f.store.SaveMatchRatings(t.Context(),
		strong.id, store.MatchRating{Value: 2400, Deviation: 50},
		weak.id, store.MatchRating{Value: 1000, Deviation: 50})
	if err != nil {
		t.Fatalf("SaveMatchRatings: %v", err)
	}

	if got := f.poll(t, strong); got.Status != queueStatusWaiting {
		t.Fatalf("센 사람: %+v, want waiting", got)
	}
	got := f.poll(t, weak)
	if got.Status != queueStatusWaiting {
		t.Fatalf("약한 사람: %+v, want waiting — 밴드 밖인데 붙었다", got)
	}
	if got.Waiting != 2 {
		t.Errorf("줄에 %d명, want 2", got.Waiting)
	}
}

// 줄에서 빠지면 짝이 안 된다. 탭을 닫는 자리에서 부르는 경로다.
func TestLeavingTheQueueRemovesTheWaiter(t *testing.T) {
	f := queueServer(t, 2)
	left, other := f.users[0], f.users[1]

	f.poll(t, left)
	if rec := do(f.h, http.MethodDelete, "/api/queue", left.cookie); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/queue: status = %d", rec.Code)
	}

	got := f.poll(t, other)
	if got.Status != queueStatusWaiting {
		t.Fatalf("나간 사람과 짝이 됐다: %+v", got)
	}
	if got.Waiting != 1 {
		t.Errorf("줄에 %d명, want 1", got.Waiting)
	}
}

// 짝짓기가 두 사람만 짓는다. 셋이 동시에 서면 하나는 남는다 — 남는 사람이 없으면
// 어딘가에서 한 사람이 두 방에 앉아 있는 것이다.
func TestThreeWaitersLeaveOneBehind(t *testing.T) {
	f := queueServer(t, 3)

	statuses := make([]queuePayload, 0, 3)
	for _, u := range f.users {
		statuses = append(statuses, f.poll(t, u))
	}

	matched := 0
	for _, s := range statuses {
		if s.Status == queueStatusMatched {
			matched++
		}
	}
	if matched != 1 {
		t.Fatalf("이 자리에서 짝이 %d 번 잡혔다, want 1", matched)
	}
	// 남은 한 사람은 아직 줄에 있다. 짝이 된 쪽의 쪽지는 아직 안 찾아갔다.
	if last := statuses[2]; last.Status == queueStatusMatched && statuses[1].Status == queueStatusMatched {
		t.Error("셋 다 짝이 됐다")
	}
}
