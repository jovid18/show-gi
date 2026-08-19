package store

import "testing"

// 마이페이지가 세는 것이 되짚기 목록과 같은 모집단인지.
//
// 여기가 어긋나면 화면 둘이 같은 사람에 대해 다른 수를 말한다 — 「12판 뒀다」고 적힌
// 페이지에서 목록을 열면 9줄인 상태가 된다.
func TestPlayerTallyCountsTheSamePopulationAsTheList(t *testing.T) {
	s := open(t)
	uid := owner(t, s, "tally")

	// 결과별로 하나씩. 전부 한 수 이상 둔 판이어야 한다 — 질의가 EXISTS 로 거른다.
	for _, res := range []GameResult{ResultWin, ResultWin, ResultLoss, ResultDraw, ResultAbandoned} {
		id := ownedGame(t, s, &uid)
		if err := s.InsertMove(t.Context(), id, 1, "7g7f"); err != nil {
			t.Fatalf("InsertMove: %v", err)
		}
		if err := s.InsertIntervention(t.Context(), id, Intervention{
			Ply: 1, Kind: "blunder", Category: "hangs_piece", DeltaWin: 0.4, LevelBucket: "beginner",
		}); err != nil {
			t.Fatalf("InsertIntervention: %v", err)
		}
		if err := s.FinishGame(t.Context(), id, res); err != nil {
			t.Fatalf("FinishGame(%s): %v", res, err)
		}
	}

	// 한 수도 안 둔 판. 목록에서 빠지므로 여기서도 빠져야 한다.
	empty := ownedGame(t, s, &uid)
	if err := s.FinishGame(t.Context(), empty, ResultWin); err != nil {
		t.Fatalf("FinishGame(빈 판): %v", err)
	}

	got, err := s.PlayerTally(t.Context(), &uid)
	if err != nil {
		t.Fatalf("PlayerTally: %v", err)
	}

	if n := got.Results[ResultWin]; n != 2 {
		t.Errorf("win = %d, want 2 (한 수도 안 둔 판이 섞였다)", n)
	}
	if n := got.Results[ResultLoss]; n != 1 {
		t.Errorf("loss = %d, want 1", n)
	}
	if n := got.Results[ResultDraw]; n != 1 {
		t.Errorf("draw = %d, want 1", n)
	}
	// abandoned 는 되짚기 목록에 안 나가므로 전적에도 없어야 한다(journal §51).
	if n := got.Results[ResultAbandoned]; n != 0 {
		t.Errorf("abandoned = %d, want 0", n)
	}

	// 개입도 같은 판들만 센다 — abandoned 판의 것 하나가 빠져 4가 아니라 4다:
	// win 2 + loss 1 + draw 1.
	if n := got.Categories["hangs_piece"]; n != 4 {
		t.Errorf("hangs_piece = %d, want 4 (전적과 다른 모집단을 세고 있다)", n)
	}
	// 목록에서 센 판 수와 개입의 모집단이 같은지 곧바로 대조한다.
	list, err := s.ListGames(t.Context(), 100, &uid)
	if err != nil {
		t.Fatalf("ListGames: %v", err)
	}
	total := got.Results[ResultWin] + got.Results[ResultLoss] + got.Results[ResultDraw]
	if len(list) != total {
		t.Errorf("목록 %d줄 vs 전적 %d판 — 두 화면이 다른 수를 말한다", len(list), total)
	}
}

// 남의 판을 세지 않는다. 여기가 새면 마이페이지가 다른 사람의 전적을 그린다
// (02-architecture.md §7 위협 2).
func TestPlayerTallyIsPerOwner(t *testing.T) {
	s := open(t)
	mine := owner(t, s, "mine")
	theirs := owner(t, s, "theirs")

	id := ownedGame(t, s, &theirs)
	if err := s.InsertMove(t.Context(), id, 1, "7g7f"); err != nil {
		t.Fatalf("InsertMove: %v", err)
	}
	if err := s.FinishGame(t.Context(), id, ResultWin); err != nil {
		t.Fatalf("FinishGame: %v", err)
	}

	got, err := s.PlayerTally(t.Context(), &mine)
	if err != nil {
		t.Fatalf("PlayerTally: %v", err)
	}
	if n := got.Results[ResultWin]; n != 0 {
		t.Errorf("남의 판 %d개가 내 전적에 들어왔다", n)
	}
}
