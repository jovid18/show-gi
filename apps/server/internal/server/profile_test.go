package server

import (
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 한 번 걸린 것은 약점이 아니다. 그날 우연히 둔 수 하나가 「당신의 약점 1위」가 되면
// 안 된다(weaknessMin).
func TestOneOffCategoryIsNotAWeakness(t *testing.T) {
	total, got := weaknessesOf(map[string]int{"hangs_piece": 5, "lets_mate": 1})
	if total != 6 {
		t.Errorf("전체 = %d, want 6 (자르기 전의 합이어야 한다)", total)
	}
	if len(got) != 1 || got[0].Code != "hangs_piece" {
		t.Fatalf("목록 = %+v, 한 번짜리가 빠져야 한다", got)
	}
}

// Share 의 분모는 **자르기 전의** 전체다. 잘린 뒤의 합을 쓰면 비율이 1을 넘는다.
func TestShareUsesTheWholeCount(t *testing.T) {
	_, got := weaknessesOf(map[string]int{"a": 6, "b": 3, "c": 1})
	if len(got) == 0 {
		t.Fatal("목록이 비었다")
	}
	if want := 6.0 / 10.0; got[0].Share != want {
		t.Errorf("Share = %v, want %v", got[0].Share, want)
	}
	var sum float64
	for _, w := range got {
		sum += w.Share
	}
	if sum > 1 {
		t.Errorf("비율의 합 %v 가 1을 넘는다", sum)
	}
}

// 순서는 총평과 같은 규칙이다 — 많은 순, 같으면 코드 순. 무작위면 새로고침마다
// 「1위」가 바뀐다.
func TestWeaknessOrderIsStable(t *testing.T) {
	counts := map[string]int{"zeta": 4, "alpha": 4, "mid": 9, "low": 2}
	for range 20 {
		_, got := weaknessesOf(counts)
		if len(got) != weaknessTop {
			t.Fatalf("줄 수 = %d, want %d", len(got), weaknessTop)
		}
		if got[0].Code != "mid" || got[1].Code != "alpha" || got[2].Code != "zeta" {
			t.Fatalf("순서 = %s %s %s", got[0].Code, got[1].Code, got[2].Code)
		}
	}
}

// 개입이 없으면 목록도 전체도 0이다. 화면이 그때 「戻した手がありません」을 그린다.
func TestNoInterventionsGivesNothing(t *testing.T) {
	total, got := weaknessesOf(nil)
	if total != 0 || got != nil {
		t.Errorf("전체 = %d, 목록 = %+v", total, got)
	}
}

// 전적은 끝난 셋만 더한다. abandoned·declined 가 섞이면 목록에 안 보이는 판이
// 전적에는 들어간다(query/games.sql 의 조건과 같아야 한다).
func TestRecordCountsOnlyFinishedGames(t *testing.T) {
	got := winLossDraw(map[store.GameResult]int{
		store.ResultWin:       3,
		store.ResultLoss:      2,
		store.ResultDraw:      1,
		store.ResultAbandoned: 9,
	})
	if got.Games != 6 {
		t.Errorf("Games = %d, want 6", got.Games)
	}
	if got.Win != 3 || got.Loss != 2 || got.Draw != 1 {
		t.Errorf("%+v", got)
	}
}
