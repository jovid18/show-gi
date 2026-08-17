package game

import (
	"os"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// TestRealEngineAdaptiveKeepsTheGamePlayable 는 **이 상대가 존재하는 이유**를 잰다.
//
// D3에서 초기 국면부터 아무 수나 두게 했더니 20수 만에 절망적인 형세가 되어 개입이
// 한 번도 안 걸렸다(journal §13). 승률이 포화하기 때문이고, 고칠 버그가 아니라
// **판정이 의미를 갖는 구간이 형세가 팽팽할 때**라는 사실이다.
//
// 즉 적응형 상대는 있으면 좋은 기능이 아니라 **개입이 걸리는 구간을 유지하는 장치**다.
// 그래서 같은 약한 기보를 두 상대에게 두게 하고 형세를 견준다.
//
//	SHOWGI_USI_CMD=/opt/yaneuraou/run go test ./internal/game/ -run RealEngineAdaptive -v
func TestRealEngineAdaptiveKeepsTheGamePlayable(t *testing.T) {
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" {
		t.Skip("SHOWGI_USI_CMD 미설정")
	}
	pool, err := usi.NewPool(2, cmd, map[string]string{
		"USI_Hash": "128", "Threads": "1", "FV_SCALE": "24",
		"BookFile": "no_book", "USI_OwnBook": "false",
	})
	if err != nil {
		t.Fatalf("엔진 풀: %v", err)
	}
	defer pool.Close()

	const plies = 20
	adaptive := playWeakly(t, pool, NewAdaptiveOpponent(pool, 8, DefaultBand), plies)
	best := playWeakly(t, pool, NewEngineOpponent(pool, 8), plies)

	t.Logf("적응형 상대  : 플레이어 관점 %+dcp", adaptive)
	t.Logf("최선수 상대  : 플레이어 관점 %+dcp", best)

	if adaptive <= best {
		t.Errorf("적응형 상대가 최선수 상대보다 너그럽지 않다: %+d vs %+d", adaptive, best)
	}

	// 승률이 포화하면 개입이 눈이 먼다. K=600에서 −1200cp면 승률 약 12%이고,
	// 거기서 더 나빠져도 낙폭이 임계치를 못 넘는다(01-core.md §2).
	if adaptive < -1200 {
		t.Errorf("20수 만에 개입이 안 걸리는 구간까지 밀렸다: %+dcp", adaptive)
	}
}

// playWeakly 는 **일부러 약하게** 둔다 — 늘 합법수 목록의 첫 수다.
//
// 초심자를 흉내내려는 것이 아니라 **초심자보다도 못 두는 쪽**을 잡아 하한을 보는 것이다.
// 여기서 판이 유지되면 실제 플레이어에게는 더 유지된다.
func playWeakly(t *testing.T, pool *usi.Pool, opp Opponent, plies int) int {
	t.Helper()
	return playWeaklyWith(t, pool, opp, plies, skill.Unknown)
}

// playWeaklyWith 는 상대에게 넘길 추정치를 밖에서 정한다 — 그 값이 강함을 어디까지
// 움직이는지를 재는 자리가 rating_measure_test.go 다.
func playWeaklyWith(t *testing.T, pool *usi.Pool, opp Opponent, plies int, sk skill.Estimate) int {
	t.Helper()

	pos, _ := shogi.ParseSFEN(shogi.StartSFEN)
	var moves []string

	for i := 0; i < plies; i++ {
		legal := pos.LegalMoves()
		if len(legal) == 0 {
			break
		}
		var m shogi.Move
		if i%2 == 0 {
			m = legal[0] // 사람 차례 — 아무 수나
		} else {
			u, err := opp.Choose(t.Context(), shogi.StartSFEN, moves, sk)
			if err != nil {
				t.Fatalf("%d수째 상대: %v", i, err)
			}
			if m, err = shogi.ParseUSIMove(u); err != nil {
				t.Fatalf("%d수째 상대가 이상한 수를 돌려줬다 %q: %v", i, u, err)
			}
			if err := pos.ValidateMove(m); err != nil {
				t.Fatalf("%d수째 상대가 불법수를 돌려줬다 %q: %v", i, u, err)
			}
		}
		pos = pos.Apply(m)
		moves = append(moves, m.USI())
	}

	res, err := pool.SearchDepth(t.Context(), shogi.StartSFEN, moves, 10)
	if err != nil {
		t.Fatalf("형세 측정: %v", err)
	}
	// 수번이 사람(先手)이면 그대로, 상대면 뒤집는다
	if pos.Turn == shogi.Black {
		return res.ScoreCp
	}
	return -res.ScoreCp
}
