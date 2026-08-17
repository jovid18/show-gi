package game

import (
	"errors"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 이어하기는 **기록에서 판을 다시 두는 것**이다(journal §46 · §51).
//
// 여기서 보는 것 셋: 手数가 이어지는가 · 표기와 둔 쪽이 붙는가 · 수번이 맞는가.
// 셋 중 하나만 어긋나도 서버는 조용하고, 사람만 자기 持ち駒가 다른 것을 본다.
func TestStartMovesRebuildsThePosition(t *testing.T) {
	// ▲7六歩 △3四歩 ▲2六歩 까지 두다 끊긴 판. 다음은 後手 차례다.
	played := []string{"7g7f", "3c3d", "2g2f"}
	// **여기서 보는 것은 되만든 판이지 그 뒤의 수가 아니다.** 답하는 상대를 주면 그 수가
	// 스냅샷보다 먼저 도착할 수 있고, 이 테스트는 그것으로 오래 흔들렸다(§73).
	// 이어서 상대가 두는 쪽은 TestResumedGameLetsTheOpponentMoveFirst 가 본다.
	sess := newSession(t, Config{
		Opponent:   silentOpponent{},
		HumanColor: shogi.Black,
		StartMoves: played,
	})

	snap, err := sess.Snapshot(t.Context())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if snap.Ply != len(played) {
		t.Errorf("ply = %d, want %d", snap.Ply, len(played))
	}
	if len(snap.Moves) != len(played) {
		t.Fatalf("기보 %d수, want %d", len(snap.Moves), len(played))
	}
	for i, m := range snap.Moves {
		if m.USI != played[i] {
			t.Errorf("%d手目 = %q, want %q", i+1, m.USI, played[i])
		}
		if m.Ja == "" {
			t.Errorf("%d手目에 棋譜 표기가 없다", i+1)
		}
		// 사람이 先手이므로 홀수 手数가 사람이다.
		want := SideEngine
		if i%2 == 0 {
			want = SideHuman
		}
		if m.By != want {
			t.Errorf("%d手目 by = %q, want %q", i+1, m.By, want)
		}
	}
	// **끊긴 자리의 수번 그대로다.** 여기가 어긋나면 이어한 순간 상대가 두 번 둔다.
	if snap.Turn != "w" || snap.YourTurn {
		t.Errorf("turn = %q yourTurn = %v, want w/false", snap.Turn, snap.YourTurn)
	}
}

// 되만든 판에서 **그대로 이어 둔다.** 手数가 이어지지 않으면 기보가 그 자리에서 덮인다
// (game_moves 는 (game_id, ply) 가 PK다).
func TestResumedGameKeepsCountingPlies(t *testing.T) {
	sess := newSession(t, Config{
		Opponent:   &scriptedOpponent{moves: []string{"8c8d"}},
		HumanColor: shogi.Black,
		StartMoves: []string{"7g7f", "3c3d"},
	})

	snap, err := sess.Play(t.Context(), "2g2f")
	if err != nil {
		t.Fatalf("Play: %v", err)
	}
	if snap.Ply != 3 {
		t.Errorf("이어 둔 수의 ply = %d, want 3", snap.Ply)
	}
	if last := snap.Moves[len(snap.Moves)-1]; last.USI != "2g2f" || last.By != SideHuman {
		t.Errorf("마지막 수 = %+v, want 2g2f/human", last)
	}
}

// **한 수라도 안 맞으면 세션이 아예 안 선다.** 기록은 큐가 넘치면 이벤트를 버리므로
// (server/recorder.go) 이런 기보가 실제로 나올 수 있고, 눈감고 이어 두면 그 뒤가 통째로
// 밀린 **없던 판**이 「그때 두던 판」의 얼굴로 선다.
func TestStartMovesRejectsABrokenRecord(t *testing.T) {
	for _, tc := range []struct {
		name  string
		moves []string
	}{
		{"읽을 수 없는 수", []string{"7g7f", "zzzz"}},
		{"그 국면에서 둘 수 없는 수", []string{"7g7f", "7g7f"}},
		{"수번이 뒤집힌 수", []string{"7g7f", "2g2f"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(t.Context(), Config{
				Opponent:   &scriptedOpponent{},
				HumanColor: shogi.Black,
				StartMoves: tc.moves,
			})
			if !errors.Is(err, ErrCannotResume) {
				t.Errorf("err = %v, want %v", err, ErrCannotResume)
			}
		})
	}
}

// 되만든 뒤 **상대 차례면 그 자리에서 생각한다.** 사람이 끊고 나갔을 때 상대의 수를
// 기다리던 판이 흔하고, 이어했는데 아무도 안 두면 그 판은 멈춘 것으로 보인다.
func TestResumedGameLetsTheOpponentMoveFirst(t *testing.T) {
	sess := newSession(t, Config{
		Opponent:   &scriptedOpponent{moves: []string{"3c3d"}},
		HumanColor: shogi.Black,
		StartMoves: []string{"7g7f"}, // 끊긴 자리가 後手 차례다
	})

	snaps, unsubscribe, err := sess.Subscribe(t.Context())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer unsubscribe()

	snap := waitFor(t, snaps, func(s Snapshot) bool { return s.Ply == 2 }, "상대의 수")
	if last := snap.Moves[1]; last.USI != "3c3d" || last.By != SideEngine {
		t.Errorf("2手目 = %+v, want 3c3d/engine", last)
	}
}
