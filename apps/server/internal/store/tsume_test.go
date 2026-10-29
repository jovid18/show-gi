package store

import (
	"errors"
	"testing"
)

// 덮지 않는 규칙(ON CONFLICT DO NOTHING)과 이름을 지키는 규칙(UPDATE SET 에 name 이 없다)은
// SQL 에만 있다. mate_test.go 와 같은 이유로 실제 DB 로만 확인한다.

func tsumeUser(t *testing.T, s *Store) int64 {
	t.Helper()
	uid := "test/" + t.Name()
	if _, err := s.pool.Exec(t.Context(), `DELETE FROM users WHERE provider_uid = $1`, uid); err != nil {
		t.Fatalf("이전 실행 정리: %v", err)
	}
	id, err := s.UpsertUser(t.Context(), "google", uid, "テスト")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	return id
}

func TestTsumeResultIsWrittenOnce(t *testing.T) {
	s := open(t)
	k := "test/" + t.Name()
	if _, err := s.pool.Exec(t.Context(), `DELETE FROM tsume_results WHERE sfen_key = $1`, k); err != nil {
		t.Fatalf("이전 실행 정리: %v", err)
	}

	if _, ok, err := s.TsumeResult(t.Context(), k, 1); err != nil || ok {
		t.Fatalf("빈 캐시: ok=%v err=%v", ok, err)
	}
	first := TsumeResult{Status: "mate", Plies: 3, Shortest: true, Tree: []byte(`{"usi":"G*5b"}`), Nodes: 10}
	if err := s.SaveTsumeResult(t.Context(), k, 1, first); err != nil {
		t.Fatalf("SaveTsumeResult: %v", err)
	}
	if err := s.SaveTsumeResult(t.Context(), k, 1, TsumeResult{Status: "nomate", Nodes: 99}); err != nil {
		t.Fatalf("두 번째 SaveTsumeResult: %v", err)
	}

	got, ok, err := s.TsumeResult(t.Context(), k, 1)
	if err != nil || !ok {
		t.Fatalf("TsumeResult: ok=%v err=%v", ok, err)
	}
	if got.Status != "mate" || got.Plies != 3 || !got.Shortest || got.Nodes != 10 {
		t.Fatalf("첫 답이 덮였다: %+v", got)
	}
	if string(got.Tree) != `{"usi": "G*5b"}` && string(got.Tree) != `{"usi":"G*5b"}` {
		t.Fatalf("트리 %s", got.Tree)
	}

	// 풀이 판이 다르면 다른 행이다.
	if _, ok, _ := s.TsumeResult(t.Context(), k, 2); ok {
		t.Fatal("다른 판의 답을 읽었다")
	}
}

func TestTsumeSolveKeepsTheNameOnResolve(t *testing.T) {
	s := open(t)
	user := tsumeUser(t, s)
	k := "test/" + t.Name()

	if err := s.RecordTsumeSolve(t.Context(), user, k, "unknown", 0, false); err != nil {
		t.Fatalf("RecordTsumeSolve: %v", err)
	}
	rows, err := s.TsumeSolves(t.Context(), user)
	if err != nil || len(rows) != 1 {
		t.Fatalf("TsumeSolves: %v %+v", err, rows)
	}
	if err := s.RenameTsumeSolve(t.Context(), rows[0].ID, user, "第1問"); err != nil {
		t.Fatalf("RenameTsumeSolve: %v", err)
	}

	if err := s.RecordTsumeSolve(t.Context(), user, k, "mate", 7, true); err != nil {
		t.Fatalf("다시 RecordTsumeSolve: %v", err)
	}
	rows, err = s.TsumeSolves(t.Context(), user)
	if err != nil || len(rows) != 1 {
		t.Fatalf("같은 국면이 두 줄이 됐다: %v %+v", err, rows)
	}
	got := rows[0]
	if got.Name != "第1問" || got.Status != "mate" || got.Plies != 7 || !got.Shortest || got.SFENKey != k {
		t.Fatalf("got %+v", got)
	}
}

func TestTsumeSolveBelongsToItsOwner(t *testing.T) {
	s := open(t)
	owner := tsumeUser(t, s)
	other, err := s.UpsertUser(t.Context(), "google", "test/"+t.Name()+"/other", "ほか")
	if err != nil {
		t.Fatalf("UpsertUser: %v", err)
	}
	if err := s.RecordTsumeSolve(t.Context(), owner, "test/"+t.Name(), "nomate", 0, false); err != nil {
		t.Fatalf("RecordTsumeSolve: %v", err)
	}
	rows, err := s.TsumeSolves(t.Context(), owner)
	if err != nil || len(rows) != 1 {
		t.Fatalf("TsumeSolves: %v %+v", err, rows)
	}
	id := rows[0].ID

	if err := s.RenameTsumeSolve(t.Context(), id, other, "x"); !errors.Is(err, ErrNoTsumeSolve) {
		t.Fatalf("남의 이름 고치기: %v", err)
	}
	if err := s.DeleteTsumeSolve(t.Context(), id, other); !errors.Is(err, ErrNoTsumeSolve) {
		t.Fatalf("남의 기록 지우기: %v", err)
	}
	if err := s.DeleteTsumeSolve(t.Context(), id, owner); err != nil {
		t.Fatalf("DeleteTsumeSolve: %v", err)
	}
	if rows, _ := s.TsumeSolves(t.Context(), owner); len(rows) != 0 {
		t.Fatalf("지운 뒤 %+v", rows)
	}
}
