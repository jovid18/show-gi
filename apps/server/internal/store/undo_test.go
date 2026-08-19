package store

import "testing"

// 무르기는 기보를 자르고 무른 수를 따로 남긴다. 둘 다 SQL에만 있는 규칙이라
// (query/games.sql) 가짜로는 검증할 수 없다.
func TestRecordUndoCutsTheKifuAndKeepsTheMove(t *testing.T) {
	s := open(t)
	id, err := s.CreateGame(t.Context(), nil, "b", "startpos-for-"+t.Name(), "")
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}

	// 사람 1手 · 상대 2手까지 두고, 사람 수에 평가치가 붙은 상태.
	for ply, usi := range map[int]string{1: "7g7f", 2: "3c3d"} {
		if err := s.InsertMove(t.Context(), id, ply, usi); err != nil {
			t.Fatalf("InsertMove %d: %v", ply, err)
		}
	}
	if err := s.SetMoveEval(t.Context(), id, 1, 123); err != nil {
		t.Fatalf("SetMoveEval: %v", err)
	}

	if err := s.RecordUndo(t.Context(), id, 1, "7g7f"); err != nil {
		t.Fatalf("RecordUndo: %v", err)
	}

	rec, err := s.GameRecordAnyOwner(t.Context(), id)
	if err != nil {
		t.Fatalf("GameRecordAnyOwner: %v", err)
	}

	// 상대의 응수까지 사라진다. 사람 수만 지우면 기보가 상대 수로 시작한다.
	if len(rec.Moves) != 0 {
		t.Fatalf("기보가 안 잘렸다: %+v", rec.Moves)
	}
	if len(rec.Undos) != 1 {
		t.Fatalf("무르기 기록 = %d개 (1 기대)", len(rec.Undos))
	}
	u := rec.Undos[0]
	if u.Ply != 1 || u.USI != "7g7f" {
		t.Fatalf("무른 수 = %+v", u)
	}
	// 평가치는 지우기 전에 옮겨 담는다. 순서가 뒤집히면 여기가 nil이 된다.
	if u.EvalCp == nil || *u.EvalCp != 123 {
		t.Fatalf("무른 수의 평가치 = %v (123 기대)", u.EvalCp)
	}

	// 개입 횟수에 안 섞인다 — 목록의 그 숫자는 「AI가 몇 번 막았나」다.
	if rec.InterventionCount != 0 {
		t.Fatalf("무르기가 개입 횟수에 섞였다: %d", rec.InterventionCount)
	}

	// 이어하는 판이 예산을 이어받는 근거.
	n, err := s.CountUndos(t.Context(), id)
	if err != nil {
		t.Fatalf("CountUndos: %v", err)
	}
	if n != 1 {
		t.Fatalf("무른 횟수 = %d (1 기대)", n)
	}
}

// 같은 手数를 여러 번 무를 수 있다 — 무르고 다시 두고 또 무르면 그렇게 된다.
// 유니크 제약이 있으면 두 번째가 실패하고, 실패는 로그로만 남아 조용하다.
func TestRecordUndoAcceptsTheSamePlyTwice(t *testing.T) {
	s := open(t)
	id, err := s.CreateGame(t.Context(), nil, "b", "startpos-for-"+t.Name(), "")
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}

	for _, usi := range []string{"7g7f", "2g2f"} {
		if err := s.InsertMove(t.Context(), id, 1, usi); err != nil {
			t.Fatalf("InsertMove: %v", err)
		}
		if err := s.RecordUndo(t.Context(), id, 1, usi); err != nil {
			t.Fatalf("RecordUndo %s: %v", usi, err)
		}
	}

	rec, err := s.GameRecordAnyOwner(t.Context(), id)
	if err != nil {
		t.Fatalf("GameRecordAnyOwner: %v", err)
	}
	if len(rec.Undos) != 2 {
		t.Fatalf("무르기 기록 = %d개 (2 기대)", len(rec.Undos))
	}
	// 무른 순서가 남아야 한다 — id 로 이어 정렬하는 근거다.
	if rec.Undos[0].USI != "7g7f" || rec.Undos[1].USI != "2g2f" {
		t.Fatalf("무른 순서가 안 지켜졌다: %+v", rec.Undos)
	}
}
