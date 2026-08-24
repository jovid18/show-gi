package store

import (
	"errors"
	"testing"
)

// 여기서 확인하는 규칙(얕은 한계의 답이 깊은 것을 못 덮는다)은 SQL의 WHERE 절에만
// 있다. Go 쪽에 옮겨 적지 않았으므로 가짜로는 검증할 수 없다 — store_test.go 의 open 참조.

// mateKey 는 테스트마다 다른 키를 주고 시작할 때 지운다. positions 쪽 key 와 같은 이유다.
func mateKey(t *testing.T, s *Store) string {
	t.Helper()
	k := "test/" + t.Name()
	if _, err := s.pool.Exec(t.Context(), `DELETE FROM mate_positions WHERE sfen_key = $1`, k); err != nil {
		t.Fatalf("이전 실행 정리: %v", err)
	}
	return k
}

func TestMateRoundTrip(t *testing.T) {
	s := open(t)
	k := mateKey(t, s)

	if _, err := s.GetMate(t.Context(), k); !errors.Is(err, ErrNoMate) {
		t.Fatalf("빈 캐시에서 ErrNoMate 기대, got %v", err)
	}

	want := Mate{SFENKey: k, DepthLimit: 11, Moves: []string{"1a1b", "2a2b", "3a3b"}}
	stored, err := s.PutMate(t.Context(), want)
	if err != nil {
		t.Fatalf("PutMate: %v", err)
	}
	if !stored {
		t.Fatal("첫 쓰기가 저장되지 않았다")
	}

	got, err := s.GetMate(t.Context(), k)
	if err != nil {
		t.Fatalf("GetMate: %v", err)
	}
	if got.DepthLimit != want.DepthLimit || len(got.Moves) != len(want.Moves) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for i, m := range want.Moves {
		if got.Moves[i] != m {
			t.Fatalf("수순 %d번째가 %q, %q 기대", i, got.Moves[i], m)
		}
	}
}

// 「詰み이 없다」는 빈 배열로 남는다. nil 을 보내도 NOT NULL 칸이 거절하지 않아야 한다.
func TestMateStoresProvenNoMate(t *testing.T) {
	s := open(t)
	k := mateKey(t, s)

	if _, err := s.PutMate(t.Context(), Mate{SFENKey: k, DepthLimit: 11}); err != nil {
		t.Fatalf("PutMate: %v", err)
	}
	got, err := s.GetMate(t.Context(), k)
	if err != nil {
		t.Fatalf("GetMate: %v", err)
	}
	if len(got.Moves) != 0 {
		t.Fatalf("수순이 %v 다. 빈 것이어야 한다", got.Moves)
	}
	if got.DepthLimit != 11 {
		t.Fatalf("한계가 %d 다", got.DepthLimit)
	}
}

// 얕은 한계가 깊은 한계를 덮으면 있는 詰み이 사라진다. 그것을 WHERE 절이 막는다.
func TestMateShallowerLimitDoesNotOverwrite(t *testing.T) {
	s := open(t)
	k := mateKey(t, s)

	deep := Mate{SFENKey: k, DepthLimit: 11, Moves: []string{"1a1b", "2a2b", "3a3b"}}
	if _, err := s.PutMate(t.Context(), deep); err != nil {
		t.Fatalf("깊은 쓰기: %v", err)
	}

	// 한계 9의 「詰み이 없다」. 덮으면 위의 3手詰이 사라진다.
	stored, err := s.PutMate(t.Context(), Mate{SFENKey: k, DepthLimit: 9})
	if err != nil {
		t.Fatalf("얕은 쓰기: %v", err)
	}
	if stored {
		t.Fatal("얕은 한계의 답이 저장됐다고 답했다")
	}

	got, err := s.GetMate(t.Context(), k)
	if err != nil {
		t.Fatalf("GetMate: %v", err)
	}
	if got.DepthLimit != 11 || len(got.Moves) != 3 {
		t.Fatalf("깊은 답이 덮였다: %+v", got)
	}
}

// 같은 한계로 다시 쓰면 갱신하지 않는다. solver 가 같은 한계에서 결정적이라 답이 같다.
func TestMateSameLimitIsNotRewritten(t *testing.T) {
	s := open(t)
	k := mateKey(t, s)

	if _, err := s.PutMate(t.Context(), Mate{SFENKey: k, DepthLimit: 11}); err != nil {
		t.Fatalf("첫 쓰기: %v", err)
	}
	stored, err := s.PutMate(t.Context(), Mate{SFENKey: k, DepthLimit: 11})
	if err != nil {
		t.Fatalf("두 번째 쓰기: %v", err)
	}
	if stored {
		t.Fatal("같은 한계인데 갱신했다고 답했다")
	}
}

// 깊은 한계는 얕은 것을 덮는다. ENGINE_MATE_PLIES 를 올린 배포가 이 방향이다.
func TestMateDeeperLimitOverwrites(t *testing.T) {
	s := open(t)
	k := mateKey(t, s)

	if _, err := s.PutMate(t.Context(), Mate{SFENKey: k, DepthLimit: 9}); err != nil {
		t.Fatalf("얕은 쓰기: %v", err)
	}
	stored, err := s.PutMate(t.Context(), Mate{
		SFENKey: k, DepthLimit: 13, Moves: []string{"1a1b", "2a2b", "3a3b", "4a4b", "5a5b"},
	})
	if err != nil {
		t.Fatalf("깊은 쓰기: %v", err)
	}
	if !stored {
		t.Fatal("깊은 한계의 답이 안 저장됐다")
	}

	got, err := s.GetMate(t.Context(), k)
	if err != nil {
		t.Fatalf("GetMate: %v", err)
	}
	if got.DepthLimit != 13 || len(got.Moves) != 5 {
		t.Fatalf("덮이지 않았다: %+v", got)
	}
}

func TestCountMatePositions(t *testing.T) {
	s := open(t)
	if _, err := s.CountMatePositions(t.Context()); err != nil {
		t.Fatalf("CountMatePositions: %v", err)
	}
}
