package server

import (
	"context"
	"encoding/json"
	"errors"
	"hash/fnv"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/auth"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/match"
	"github.com/jovid18/show-gi/apps/server/internal/rating"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 이 파일이 지키는 것은 큐에 선 두 사람이 같은 방에서 만나는가다. 고르는 규칙 자체는
// internal/queue 가 DB 없이 지킨다.
//
// 진짜 DB가 필요하다. 큐가 표이고(016_match_queue.sql) 여기서 재는 것 대부분이
// 「두 요청이 같은 큐를 본다」라서, 표를 흉내내면 잴 것이 남지 않는다.
//
//	SHOWGI_TEST_DATABASE_URL=postgres://showgi:showgi@localhost:5432/showgi go test ./internal/server/
//
// 격리는 레이팅으로 한다(queueBase). 표를 비우지 않는 이유는 CI 가 패키지들을 같은 DB 에
// 동시에 걸기 때문이다 — 비우면 그 순간 큐에 서 있던 남의 테스트가 깨진다.
//
// 그래서 「큐에 몇 명인가」를 정확한 수로 단정하지 않는다. 그 값은 표 전체를 세는 제품
// 질의라(store.QueueWaiting) 남의 테스트가 섞인다. 내 행이 몇 개인가는 rows 가 따로 센다.
type queueFixture struct {
	h     http.Handler
	hub   *match.Hub
	store *store.Store
	// users 는 로그인 쿠키가 붙은 사람들이다. 실행마다 새로 만든다.
	users []queueUser
	// base 는 이 테스트의 레이팅 자리다(queueBase). 사람마다 옮기려면 여기에 더한다.
	base float64
}

type queueUser struct {
	id     int64
	name   string
	cookie *http.Cookie
}

// queueBase 는 이 테스트의 사람들이 서는 레이팅 자리다.
//
// 테스트마다 만 점씩 떨어뜨린다. 밴드는 아무리 넓어도 BaseMax 에 RD 둘을 더한 값이라
// 900을 못 넘으므로(queue.Band), 그 간격이면 남의 테스트 대기자와 절대 안 붙는다 —
// 제품의 장치로 격리하는 것이고, 표를 비우는 것보다 안전하다.
func queueBase(t *testing.T) float64 {
	t.Helper()
	h := fnv.New64a()
	if _, err := h.Write([]byte(t.Name())); err != nil {
		t.Fatalf("hash: %v", err)
	}
	return 1_000_000 + float64(h.Sum64()%100_000)*10_000
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

	f := queueFixture{h: Handler(opts), hub: m.hub, store: st, users: users, base: queueBase(t)}
	// 전원이 같은 자리에 선다. 벌려 놓을 테스트는 스스로 다시 부른다.
	f.rate(t, func(int) float64 { return f.base })
	return f
}

// rate 는 사람마다 레이팅을 심는다. 불확실성이 하한이라(rating.MinDeviation) 밴드가 가장
// 좁고, 그래서 이 테스트의 사람들끼리만 붙는다.
//
// 제품의 쓰기 문(SaveMatchRatings)을 안 쓴다. 저쪽은 한 문장이 두 사람을 같이 옮기므로
// 서로 다른 두 사람이 있어야 하는데, 여기는 혼자 서는 테스트에도 자리를 줘야 한다 —
// 안 주면 그 사람이 기본 1500에 남아 다른 「혼자 서는 테스트」와 붙는다.
func (f queueFixture) rate(t *testing.T, of func(i int) float64) {
	t.Helper()
	for i, u := range f.users {
		_, err := f.store.Pool().Exec(t.Context(), `
			INSERT INTO skill_profile (user_id, rating_est, rating_sd, rating_games, rating_updated_at)
			VALUES ($1, $2, $3, 1, now())
			ON CONFLICT (user_id) DO UPDATE
			SET rating_est = EXCLUDED.rating_est, rating_sd = EXCLUDED.rating_sd,
			    rating_games = 1, rating_updated_at = now()`,
			u.id, of(i), float64(rating.MinDeviation))
		if err != nil {
			t.Fatalf("레이팅 심기(%s): %v", u.name, err)
		}
	}
}

// rows 는 그 사람이 큐에 들고 있는 행 수다. 0이나 1이어야 한다 — 표의 PK 가 그것을
// 보장하지만, 「큐에 두 번 섰나」를 재는 자리에서 남의 테스트를 안 세려면 이쪽이 필요하다.
func (f queueFixture) rows(t *testing.T, u queueUser) int {
	t.Helper()
	var n int
	err := f.store.Pool().QueryRow(t.Context(),
		`SELECT count(*) FROM match_queue WHERE user_id = $1`, u.id).Scan(&n)
	if err != nil {
		t.Fatalf("행 세기(%s): %v", u.name, err)
	}
	return n
}

// poll 은 큐에 서거나 다시 물어본다. 화면이 하는 것과 같은 호출이다.
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

// pollUntilMatched 는 짝이 잡힐 때까지 다시 물어본다.
//
// 한 번에 잡혀야 한다고 재면 안 된다. 잠금이 SKIP LOCKED 라, 같은 DB 에서 도는 남의
// 짝짓기가 내 행을 잠근 회차에는 정당하게 「기다리는 중」이 나온다 — 화면도 2초 뒤에
// 다시 묻는다(useQueue).
//
// 반대 방향(「안 잡혀야 한다」)에는 재시도가 없다. 잠금은 짝을 없앨 뿐 만들지 못하므로
// 그쪽은 한 번으로 충분하다.
func (f queueFixture) pollUntilMatched(t *testing.T, u queueUser) queuePayload {
	t.Helper()
	var got queuePayload
	for range 50 {
		got = f.poll(t, u)
		if got.Status == queueStatusMatched {
			return got
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s: 짝이 안 잡혔다: %+v", u.name, got)
	return got
}

// 로그인하지 않으면 큐에 못 선다. 익명은 서로 구별할 수단이 없어서 짝짓기가 성립하지 않는다.
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
// 정했고(journal §92), 그때 화면이 말할 것이 「큐에 몇 명인가」다.
func TestOneWaiterIsNotPaired(t *testing.T) {
	f := queueServer(t, 1)
	me := f.users[0]

	got := f.poll(t, me)
	if got.Status != queueStatusWaiting {
		t.Fatalf("status = %q, want %q", got.Status, queueStatusWaiting)
	}
	if got.RoomID != "" {
		t.Errorf("방이 %q 인데 짝이 없다", got.RoomID)
	}
	if f.rows(t, me) != 1 {
		t.Errorf("내 행이 %d개, want 1 — 큐에 서지 못했다", f.rows(t, me))
	}
	// 자기 자신은 세어진다. 정확한 수를 안 보는 이유는 이 파일 머리에 있다.
	if got.Waiting < 1 {
		t.Errorf("큐에 %d명 — 자기 자신이 안 세어졌다", got.Waiting)
	}
}

// 다시 물어보는 것이 멱등이다. 큐가 늘지 않고 선 시각도 안 밀린다 — 밀리면 밴드가
// 매 재시도마다 처음으로 돌아가서 영영 안 넓어진다.
func TestPollingDoesNotResetTheWait(t *testing.T) {
	f := queueServer(t, 1)
	me := f.users[0]

	first := f.poll(t, me)
	time.Sleep(20 * time.Millisecond)
	second := f.poll(t, me)

	if n := f.rows(t, me); n != 1 {
		t.Errorf("내 행이 %d개, want 1 — 같은 사람이 두 줄이 됐다", n)
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

	paired := f.pollUntilMatched(t, second)
	if paired.RoomID == "" {
		t.Fatalf("나중에 선 사람: %+v, 방이 비었다", paired)
	}

	// 먼저 선 쪽은 쪽지를 읽기만 한다 — 이쪽은 잠금과 무관하므로 한 번에 와야 한다.
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
	// 위 호출로 다시 큐에 섰다. 뒷정리는 users 삭제가 한다(CASCADE).

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
	paired := f.pollUntilMatched(t, f.users[1])

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

// 밴드 밖의 두 사람은 안 붙는다. 불확실성이 하한이라 밴드가 Base0 + 100 이고, 격차
// 1400은 어느 쪽 밴드로도 안 덮인다(queue.Band).
func TestFarApartWaitersAreNotPaired(t *testing.T) {
	f := queueServer(t, 2)
	strong, weak := f.users[0], f.users[1]

	// 이 테스트의 자리 안에서 벌린다. 밖으로 나가면 남의 테스트 대기자와 만난다.
	f.rate(t, func(i int) float64 {
		if i == 0 {
			return f.base + 700
		}
		return f.base - 700
	})

	if got := f.poll(t, strong); got.Status != queueStatusWaiting {
		t.Fatalf("센 사람: %+v, want waiting", got)
	}
	got := f.poll(t, weak)
	if got.Status != queueStatusWaiting {
		t.Fatalf("약한 사람: %+v, want waiting — 밴드 밖인데 붙었다", got)
	}
	// 둘 다 큐에 남아 있다.
	for _, u := range f.users {
		if n := f.rows(t, u); n != 1 {
			t.Errorf("%s: 행이 %d개, want 1", u.name, n)
		}
	}
}

// 큐에서 빠지면 짝이 안 된다. 탭을 닫는 자리에서 부르는 경로다.
func TestLeavingTheQueueRemovesTheWaiter(t *testing.T) {
	f := queueServer(t, 2)
	left, other := f.users[0], f.users[1]

	f.poll(t, left)
	if rec := do(f.h, http.MethodDelete, "/api/queue", left.cookie); rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/queue: status = %d", rec.Code)
	}
	if n := f.rows(t, left); n != 0 {
		t.Fatalf("나간 사람의 행이 %d개, want 0", n)
	}

	if got := f.poll(t, other); got.Status != queueStatusWaiting {
		t.Fatalf("나간 사람과 짝이 됐다: %+v", got)
	}
}

// 짝짓기가 두 사람만 짓는다. 셋이 서면 하나는 남는다 — 남는 사람이 없으면 어딘가에서
// 한 사람이 두 방에 앉아 있는 것이다.
func TestThreeWaitersLeaveOneBehind(t *testing.T) {
	f := queueServer(t, 3)
	first, second, third := f.users[0], f.users[1], f.users[2]

	if got := f.poll(t, first); got.Status != queueStatusWaiting {
		t.Fatalf("첫째: %+v, want waiting", got)
	}
	f.pollUntilMatched(t, second) // 둘째가 첫째를 집는다

	// 셋째에게는 남은 후보가 없다. 첫째의 행은 쪽지가 붙어 후보에서 빠졌고, 둘째의 행은
	// 짝을 지으면서 사라졌다.
	if got := f.poll(t, third); got.Status != queueStatusWaiting {
		t.Fatalf("셋째: %+v, want waiting — 이미 짝이 있는 사람과 붙었다", got)
	}
	if n := f.rows(t, third); n != 1 {
		t.Errorf("셋째의 행이 %d개, want 1 — 큐에 남아 있어야 한다", n)
	}
}
