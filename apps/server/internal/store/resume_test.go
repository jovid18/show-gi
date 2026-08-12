package store

import (
	"errors"
	"testing"
)

// 결과가 나온 판만 화면으로 나간다(docs/06-status.md §51).
//
// **목록과 상세를 같이 본다.** 목록에서만 빼면 `/reviews/<id>` 주소로 그냥 열리고,
// 규칙이 두 벌이면 한쪽만 고쳐진 채 남는다(§46).
func TestOnlyFinishedGamesAreVisible(t *testing.T) {
	s := open(t)
	me := owner(t, s, "me")

	playing := ownedGame(t, s, &me)   // 두는 중 (result NULL)
	abandoned := ownedGame(t, s, &me) // 연결이 끊겼다
	declined := ownedGame(t, s, &me)  // 끊긴 뒤 「いいえ」라고 답했다
	won := ownedGame(t, s, &me)       // 결과가 나왔다
	for _, id := range []int64{playing, abandoned, declined, won} {
		if err := s.InsertMove(t.Context(), id, 1, "7g7f"); err != nil {
			t.Fatalf("InsertMove: %v", err)
		}
	}
	for id, r := range map[int64]GameResult{abandoned: ResultAbandoned, declined: ResultDeclined, won: ResultWin} {
		if err := s.FinishGame(t.Context(), id, r); err != nil {
			t.Fatalf("FinishGame: %v", err)
		}
	}

	games, err := s.ListGames(t.Context(), 100, &me)
	if err != nil {
		t.Fatalf("ListGames: %v", err)
	}
	seen := map[int64]bool{}
	for _, g := range games {
		seen[g.ID] = true
	}
	if !seen[won] {
		t.Errorf("결과가 나온 판 %d 가 목록에 없다", won)
	}
	for _, id := range []int64{playing, abandoned, declined} {
		if seen[id] {
			t.Errorf("끝나지 않은 판 %d 가 목록에 있다", id)
		}
	}

	// 상세도 같은 규칙이다. **404가 되어야 한다** — 403이면 「그 번호의 판이 있다」를
	// 알려주는 셈이라 중단된 판의 존재가 새어 나간다.
	if _, err := s.GameRecord(t.Context(), won, &me); err != nil {
		t.Errorf("결과가 나온 판을 못 읽는다: %v", err)
	}
	for _, id := range []int64{playing, abandoned, declined} {
		if _, err := s.GameRecord(t.Context(), id, &me); !errors.Is(err, ErrNoGame) {
			t.Errorf("끝나지 않은 판 %d 가 열렸다: err = %v, want %v", id, err, ErrNoGame)
		}
	}
	// 측정 쪽은 그대로 읽는다 — `abandoned` 판이 표본에서 사라지면 안 된다(§39).
	if _, err := s.GameRecordAnyOwner(t.Context(), abandoned); err != nil {
		t.Errorf("GameRecordAnyOwner: %v", err)
	}
}

// 이어할 후보는 **중단된 판 하나**다. 두는 중인 판도, 이미 답한 판도, 남의 판도 아니다.
func TestResumableGameIsTheLatestAbandonedOne(t *testing.T) {
	s := open(t)
	me, other := owner(t, s, "me"), owner(t, s, "other")

	// 한 수도 안 둔 판은 후보가 아니다 — 이어하는 것이 새 판을 여는 것과 같다.
	empty := ownedGame(t, s, &me)
	if err := s.FinishGame(t.Context(), empty, ResultAbandoned); err != nil {
		t.Fatalf("FinishGame: %v", err)
	}

	older := abandonedGame(t, s, &me)
	theirs := abandonedGame(t, s, &other)
	newest := abandonedGame(t, s, &me)

	got, err := s.ResumableGame(t.Context(), me)
	if err != nil {
		t.Fatalf("ResumableGame: %v", err)
	}
	if got.ID != newest {
		t.Errorf("후보 = %d, want %d (older=%d empty=%d theirs=%d)", got.ID, newest, older, empty, theirs)
	}
	if got.MoveCount != 1 {
		t.Errorf("moveCount = %d, want 1", got.MoveCount)
	}

	// 「いいえ」라고 답하면 그 자리에서 후보가 아니다. **다시 물어보지 않는 것이 이
	// 상태를 갈라 둔 유일한 이유다.**
	if err := s.DeclineResume(t.Context(), newest, me); err != nil {
		t.Fatalf("DeclineResume: %v", err)
	}
	got, err = s.ResumableGame(t.Context(), me)
	if err != nil {
		t.Fatalf("ResumableGame: %v", err)
	}
	if got.ID != older {
		t.Errorf("답한 뒤 후보 = %d, want %d", got.ID, older)
	}

	// 남의 판은 답할 수도 없다. 404가 되는 자리다.
	if err := s.DeclineResume(t.Context(), theirs, me); !errors.Is(err, ErrNoGame) {
		t.Errorf("남의 판을 닫았다: err = %v, want %v", err, ErrNoGame)
	}
}

// **점유는 한 번만 성공한다.** 탭 두 개가 같은 판을 이어하려 들면 뒤엣것이 여기서
// 걸려야 한다 — 안 그러면 세션 goroutine 둘이 한 대국 행에 기록을 겹쳐 쓴다.
func TestClaimGameForResumeIsExclusive(t *testing.T) {
	s := open(t)
	me, other := owner(t, s, "me"), owner(t, s, "other")
	id := abandonedGame(t, s, &me)

	claimed, err := s.ClaimGameForResume(t.Context(), id, me)
	if err != nil {
		t.Fatalf("ClaimGameForResume: %v", err)
	}
	if claimed.ID != id || claimed.MyColor != "b" {
		t.Errorf("claimed = %+v, want id=%d myColor=b", claimed, id)
	}
	if claimed.OpeningID != "shikenbisha" {
		t.Errorf("openingID = %q, want shikenbisha — 진형이 그 판의 것으로 돌아와야 한다", claimed.OpeningID)
	}

	// 두 번째는 0행이다. 되열린 판은 더 이상 `abandoned` 가 아니다.
	if _, err := s.ClaimGameForResume(t.Context(), id, me); !errors.Is(err, ErrNoGame) {
		t.Errorf("두 번째 점유가 성공했다: err = %v, want %v", err, ErrNoGame)
	}
	// 남도 못 잡는다. 애초에 후보로도 안 나온다.
	if _, err := s.ClaimGameForResume(t.Context(), id, other); !errors.Is(err, ErrNoGame) {
		t.Errorf("남이 점유했다: err = %v, want %v", err, ErrNoGame)
	}

	// 점유가 곧 되열기다 — 그 판은 이제 「두는 중」이라 되짚기에도 안 나온다(§51).
	if _, err := s.GameRecord(t.Context(), id, &me); !errors.Is(err, ErrNoGame) {
		t.Errorf("되열린 판이 되짚기에 나온다: err = %v", err)
	}
}

// abandonedGame 은 한 수 두고 끊긴 판 하나를 만든다. 진형은 이어하기가 되살리는지를
// 보려고 채워 둔다.
func abandonedGame(t *testing.T, s *Store, userID *int64) int64 {
	t.Helper()
	id, err := s.CreateGame(t.Context(), userID, "b", "startpos-for-"+t.Name(), "shikenbisha")
	if err != nil {
		t.Fatalf("CreateGame: %v", err)
	}
	if err := s.InsertMove(t.Context(), id, 1, "7g7f"); err != nil {
		t.Fatalf("InsertMove: %v", err)
	}
	if err := s.FinishGame(t.Context(), id, ResultAbandoned); err != nil {
		t.Fatalf("FinishGame: %v", err)
	}
	return id
}
