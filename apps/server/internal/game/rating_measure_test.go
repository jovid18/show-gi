package game

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/skill"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 이 파일은 손잡이가 실제로 듣는지를 잰다. 밴드를 옮기는 코드가 도는 것은 단위
// 테스트가 보고, 여기서 보는 것은 그 옮김이 실제 후보 안에서 다른 수로 나오는가다.
//
// 후보 10개가 20~40cp 안에 몰리는 국면에서는 밴드를 아무리 옮겨도 도달할 수 없다는 것을
// §21 ①이 미리 적어 뒀고, 그러면 이 기능은 아무것도 안 하는 코드가 된다.
//
//	docker run --rm --platform linux/arm64 --cpus 4 -v "$PWD:/src:ro" show-gi-enginetest sh -c '
//	  cp -r /src /work && cd /work &&
//	  SHOWGI_USI_CMD=/opt/yaneuraou/run SHOWGI_MEASURE=1 \
//	  go test ./internal/game/ -run MeasureSkill -v -timeout 30m'

func measureEnginePool(t *testing.T, size int) *usi.Pool {
	t.Helper()
	if os.Getenv("SHOWGI_MEASURE") == "" {
		t.Skip("SHOWGI_MEASURE 미설정")
	}
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" {
		t.Skip("SHOWGI_USI_CMD 미설정")
	}
	pool, err := usi.NewPool(size, cmd, map[string]string{
		"USI_Hash": "128", "Threads": "1", "FV_SCALE": "24",
		"BookFile": "no_book", "USI_OwnBook": "false",
	})
	if err != nil {
		t.Fatalf("엔진 풀: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// 추정치 세 자리. 가운데가 기준선이고 양쪽 끝이 손잡이의 한계다.
var measureLevels = []struct {
	name string
	sk   skill.Estimate
}{
	{"헤맴(loss=1)", skill.Estimate{Loss: 1, Samples: skill.MinSamples}},
	{"기준선(loss=.5)", skill.Unknown},
	{"잘 둠(loss=0)", skill.Estimate{Loss: 0, Samples: skill.MinSamples}},
}

// TestMeasureSkillBandReach 는 같은 후보에서 다른 수가 나오는 국면이 몇 %인가를 잰다.
//
// 국면마다 탐색은 한 번뿐이다 — 결과를 stubMulti 에 담아 고르는 코드만 세 번 돌린다.
// 강함이 갈리는 자리는 선택이지 탐색이 아니므로 이것이 같은 측정이고, 엔진 시간이 3분의 1이다.
func TestMeasureSkillBandReach(t *testing.T) {
	pool := measureEnginePool(t, 2)

	lines := weakGameOpponentTurns(t, pool, 24)
	t.Logf("상대 차례 %d 국면을 잰다 (depth %d · k=%d)", len(lines), DefaultDepth, CandidateK)

	moved, spread := 0, 0
	for i, moves := range lines {
		res, err := pool.SearchMultiPV(t.Context(), shogi.StartSFEN, moves, DefaultDepth, CandidateK)
		if err != nil {
			t.Fatalf("%d번째 국면 탐색: %v", i, err)
		}

		var picks []string
		seen := map[string]bool{}
		for _, lv := range measureLevels {
			o := NewAdaptiveOpponent(&stubMulti{res: res}, DefaultDepth, DefaultBand)
			u, err := o.Choose(t.Context(), shogi.StartSFEN, moves, lv.sk)
			if err != nil {
				t.Fatalf("%d번째 국면 선택: %v", i, err)
			}
			picks = append(picks, fmt.Sprintf("%s=%s(%+dcp)", lv.name, u, playerCpOf(res, u)))
			seen[u] = true
		}

		// 후보가 얼마나 퍼져 있나. 좁으면 밴드를 옮겨도 갈 곳이 없다.
		lo, hi := candidateSpan(res)
		if hi-lo >= 200 {
			spread++
		}
		if len(seen) > 1 {
			moved++
		}
		t.Logf("%2d수: 후보폭 %+d~%+dcp  %s", len(moves), lo, hi, strings.Join(picks, "  "))
	}

	t.Logf("고른 수가 갈린 국면: %d/%d", moved, len(lines))
	t.Logf("후보폭 200cp 이상: %d/%d", spread, len(lines))

	if moved == 0 {
		t.Errorf("어느 국면에서도 강함이 안 갈렸다 — 밴드를 옮기는 코드가 아무 일도 안 한다")
	}
}

// TestMeasureSkillKeepsWeakPlayerAlive 는 §16 표에 한 줄을 더한다 —
// 헤매는 사람에게 실제로 형세가 남는가.
func TestMeasureSkillKeepsWeakPlayerAlive(t *testing.T) {
	pool := measureEnginePool(t, 2)

	const plies = 20
	got := map[string]int{}
	for _, lv := range measureLevels {
		opp := NewAdaptiveOpponent(pool, 8, DefaultBand)
		cp := playWeaklyWith(t, pool, opp, plies, lv.sk)
		got[lv.name] = cp
		t.Logf("%-16s 플레이어 관점 %+dcp", lv.name, cp)
	}

	if got["헤맴(loss=1)"] <= got["잘 둠(loss=0)"] {
		t.Errorf("헤매는 쪽이 더 너그럽지 않다: %+d vs %+d",
			got["헤맴(loss=1)"], got["잘 둠(loss=0)"])
	}
}

// weakGameOpponentTurns 는 약한 기보를 두면서 상대 차례마다의 수순을 모은다.
// 측정이 볼 국면들이고, 상대는 기준선으로 둔다.
func weakGameOpponentTurns(t *testing.T, pool *usi.Pool, plies int) [][]string {
	t.Helper()

	opp := NewAdaptiveOpponent(pool, 8, DefaultBand)
	pos, _ := shogi.ParseSFEN(shogi.StartSFEN)

	var moves []string
	var turns [][]string
	for i := range plies {
		legal := pos.LegalMoves()
		if len(legal) == 0 {
			break
		}
		var m shogi.Move
		if i%2 == 0 {
			m = legal[0] // 사람 — 늘 첫 합법수. 초심자보다도 못 두는 하한이다(§16)
		} else {
			turns = append(turns, append([]string(nil), moves...))
			u, err := opp.Choose(t.Context(), shogi.StartSFEN, moves, skill.Unknown)
			if err != nil {
				t.Fatalf("%d수째 상대: %v", i, err)
			}
			if m, err = shogi.ParseUSIMove(u); err != nil {
				t.Fatalf("%d수째 상대가 이상한 수를 돌려줬다 %q: %v", i, u, err)
			}
		}
		pos = pos.Apply(m)
		moves = append(moves, m.USI())
	}
	return turns
}

// playerCpOf 는 그 수의 플레이어 관점 cp다. 엔진은 수번(=상대) 관점으로 답한다.
func playerCpOf(res usi.SearchResult, move string) int {
	for _, l := range res.Lines {
		if l.Move == move {
			return -l.ScoreCp
		}
	}
	return 0
}

// candidateSpan 은 후보들이 플레이어 관점으로 어디부터 어디까지 퍼져 있나다.
func candidateSpan(res usi.SearchResult) (lo, hi int) {
	first := true
	for _, l := range res.Lines {
		if l.Move == "" {
			continue
		}
		cp := -l.ScoreCp
		if first {
			lo, hi, first = cp, cp, false
			continue
		}
		lo, hi = min(lo, cp), max(hi, cp)
	}
	return lo, hi
}
