package store

import (
	"context"
	"errors"
	"testing"
)

// 두 번째 로그인이 두 번째 사람을 만들면 판이 계정마다 흩어진다. 그리고 그때는
// 이미 늦다 — 갈라진 기록을 나중에 합칠 방법이 없다.
func TestUpsertUserIsIdempotent(t *testing.T) {
	s := open(t)
	uid := "test/" + t.Name()
	if _, err := s.pool.Exec(t.Context(), `DELETE FROM users WHERE provider_uid = $1`, uid); err != nil {
		t.Fatalf("이전 실행 정리: %v", err)
	}

	first, err := s.UpsertUser(t.Context(), "google", uid, "さとし")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if first == 0 {
		t.Fatal("id = 0")
	}

	// 이름을 바꿔서 다시 로그인한다. 같은 사람이어야 하고 이름은 새것이어야 한다.
	again, err := s.UpsertUser(t.Context(), "google", uid, "サトシ")
	if err != nil {
		t.Fatalf("두 번째 UpsertUser: %v", err)
	}
	if again != first {
		t.Errorf("id = %d, want %d — 같은 provider_uid 가 두 사람이 됐다", again, first)
	}

	var name string
	if err := s.pool.QueryRow(t.Context(), `SELECT display_name FROM users WHERE id = $1`, first).Scan(&name); err != nil {
		t.Fatalf("이름 조회: %v", err)
	}
	if name != "サトシ" {
		t.Errorf("display_name = %q, want サトシ", name)
	}
}

// 로그인한 사람의 판은 그 사람 것으로 남아야 한다. games.user_id 는 nullable이라
// (002_anonymous_games.sql) 안 채워도 조용히 성공한다 — 그래서 실물로 확인한다.
func TestCreateGameKeepsUserID(t *testing.T) {
	s := open(t)
	uid := "test/" + t.Name()
	if _, err := s.pool.Exec(t.Context(), `DELETE FROM users WHERE provider_uid = $1`, uid); err != nil {
		t.Fatalf("이전 실행 정리: %v", err)
	}
	userID, err := s.UpsertUser(t.Context(), "google", uid, "さとし")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}

	gameID, err := s.CreateGame(t.Context(), &userID, "b", "startpos")
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	var got *int64
	if err := s.pool.QueryRow(t.Context(), `SELECT user_id FROM games WHERE id = $1`, gameID).Scan(&got); err != nil {
		t.Fatalf("user_id 조회: %v", err)
	}
	if got == nil || *got != userID {
		t.Errorf("games.user_id = %v, want %d", got, userID)
	}
}

// 여기가 이 PR이 닫은 구멍이다. 로그인이 붙는 순간 `/api/games` 가 **남의 기보와
// 「그 사람이 어디서 막혔나」**를 그대로 열게 된다(docs/02-architecture.md §7 위협 2).
func TestGameRecordIsScopedToOwner(t *testing.T) {
	s := open(t)
	mine, theirs := owner(t, s, "mine"), owner(t, s, "theirs")

	myGame, err := s.CreateGame(t.Context(), &mine, "b", "startpos-for-"+t.Name())
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	t.Cleanup(func() {
		if _, err := s.pool.Exec(context.Background(), `DELETE FROM games WHERE id = $1`, myGame); err != nil {
			t.Errorf("정리: %v", err)
		}
	})

	if _, err := s.GameRecord(t.Context(), myGame, &mine); err != nil {
		t.Errorf("주인이 자기 판을 못 읽는다: %v", err)
	}
	// **남이면 「없다」다.** 403이면 그 번호의 판이 있다는 것을 알려주는 셈이다.
	if _, err := s.GameRecord(t.Context(), myGame, &theirs); !errors.Is(err, ErrNoGame) {
		t.Errorf("남의 판을 읽었다: err = %v, want %v", err, ErrNoGame)
	}
	// 로그인 안 한 사람에게도 안 보인다. 익명은 익명 판만 본다.
	if _, err := s.GameRecord(t.Context(), myGame, nil); !errors.Is(err, ErrNoGame) {
		t.Errorf("익명이 로그인한 사람의 판을 읽었다: err = %v, want %v", err, ErrNoGame)
	}
	// 주인을 안 보는 쪽은 그대로 읽힌다 — 측정이 쓰는 자리다.
	if _, err := s.GameRecordAnyOwner(t.Context(), myGame); err != nil {
		t.Errorf("GameRecordAnyOwner: %v", err)
	}
}

// 목록도 같이 걸러야 한다. 상세만 막으면 「누가 몇 판 뒀나」가 그대로 남는다.
func TestListGamesIsScopedToOwner(t *testing.T) {
	s := open(t)
	mine, theirs := owner(t, s, "mine"), owner(t, s, "theirs")

	anon := newGame(t, s)
	myGame := ownedGame(t, s, &mine)
	theirGame := ownedGame(t, s, &theirs)
	for _, id := range []int64{anon, myGame, theirGame} {
		if err := s.InsertMove(t.Context(), id, 1, "7g7f"); err != nil {
			t.Fatalf("InsertMove: %v", err)
		}
	}

	for _, c := range []struct {
		name    string
		asked   *int64
		want    int64
		notWant []int64
	}{
		{"주인", &mine, myGame, []int64{theirGame, anon}},
		{"익명", nil, anon, []int64{myGame, theirGame}},
	} {
		t.Run(c.name, func(t *testing.T) {
			games, err := s.ListGames(t.Context(), 100, c.asked)
			if err != nil {
				t.Fatalf("ListGames: %v", err)
			}
			seen := map[int64]bool{}
			for _, g := range games {
				seen[g.ID] = true
			}
			if !seen[c.want] {
				t.Errorf("자기 판 %d 가 목록에 없다", c.want)
			}
			for _, id := range c.notWant {
				if seen[id] {
					t.Errorf("남의 판 %d 가 목록에 있다", id)
				}
			}
		})
	}
}

func owner(t *testing.T, s *Store, suffix string) int64 {
	t.Helper()
	uid := "test/" + t.Name() + "/" + suffix
	if _, err := s.pool.Exec(t.Context(), `DELETE FROM users WHERE provider_uid = $1`, uid); err != nil {
		t.Fatalf("이전 실행 정리: %v", err)
	}
	id, err := s.UpsertUser(t.Context(), "google", uid, suffix)
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	t.Cleanup(func() {
		// games 가 ON DELETE CASCADE 로 딸려 간다 — 판을 따로 지우지 않는다.
		if _, err := s.pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id); err != nil {
			t.Errorf("정리: %v", err)
		}
	})
	return id
}

func ownedGame(t *testing.T, s *Store, userID *int64) int64 {
	t.Helper()
	id, err := s.CreateGame(t.Context(), userID, "b", "startpos-for-"+t.Name())
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	return id
}
