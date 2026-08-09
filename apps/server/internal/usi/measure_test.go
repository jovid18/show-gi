package usi

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 측정에 쓰는 대국. **将棋ウォーズ 8급 vs 6~7급 — 실제 사람의 대국이다.**
//
// 손으로 쓴 SFEN을 쓰지 않는다. 그렇게 했다가 歩가 19장인 판을 만들었고 룰 엔진이 잡았다.
// 프로 대국도 안 쓴다 — 잘 둔 판일수록 상위 수들이 좁게 몰려서, 우리 사용자가 실제로
// 도달하는 국면과 후보의 흩어짐이 다르다. **밴드 적중률은 국면에 크게 좌우된다.**
//
// KIF에서 옮긴 뒤 전 수를 룰 엔진으로 검증했다(TestMeasureKifuIsLegal).
const (
	// jovid(8급) vs goku777(6급). 55수, 後手 투료.
	kifuA = `7g7f 3c3d 6g6f 6c6d 2h6h 7a6b 5i4h 6b6c 7i7h 6c5d
7h6g 8b6b 5g5f 2b3c 8h7g 3a3b 4i3h 5a4b 3i2h 4b3a
9g9f 6a5b 4h3i 7c7d 6i5h 8a7c 6h6i 7c8e 7g9e 6b6c
8i7g 8e7g+ 9e7g 6d6e N*5e 5d5e 5f5e 6e6f 6g6f 5c5d
P*6d 6c6d 7g8f 5d5e 8f6d 5b4b 6d9a+ 3c2d R*8a N*5g
5h5g P*6e 9a5e 6e6f S*2b`

	// jovid(8급) vs shakanyorai(7급). 161수, 後手 투료. 종반까지 간다.
	kifuB = `7g7f 8b4b 2h6h 4c4d 5i4h 3c3d 4h3h 2b3c 6i5h 3a3b
4i4h 7a7b 3i2h 3b4c 6g6f 4a5b 7i7h 5a6b 7h6g 6b7a
8h7g 3d3e 9g9f 4c3d 1g1f 3d4e 9f9e 3e3f 3g3f 4e3f
P*3g 3f4e 5g5f 4e3d 6f6e 3d4c 6g6f 4c3d 6f7e 3d4c
7g5e 4c5d 5e7g 5d6e 6h6e 5b4c 6e2e 2c2d 2e6e 4c5d
6e6i 5d4e 5h5g P*3f 3g3f 4e3f P*3g 3f3e 1f1e 4d4e
7g3c+ 2a3c B*5e B*4d 5e4d 4b4d B*2b 3e3d 2b1a+ B*7h
6i6h 7h8i+ 9i9h 8i7i 6h6e N*5a S*5e 4d4b 4g4f 3d3e
1a3c 4b5b 4f4e 3e4e 5e6f 7i8i N*4d 4e4d 3c4d 8i9h
5f5e L*4c 4d3d 9h8g P*4d 8g7f 4d4c+ 5a4c 3d4c 7f6e
4c6e R*7h L*8i 7h7i+ 8i8c+ 7b8c 6e8c L*8b 8c4g 5c5d
P*7h 7i7h B*3d 5b6b 3d7h N*3e 4g5f P*4g 5g4g 3e4g+
4h4g G*5c 5e5d 5c5d 5f3d P*5b 3d2d 5d5c 2d1c P*8g
7h8g 8b8g+ R*8e P*8b P*8c 6b7b 8c8b+ 7b8b P*8c 7a6b
8c8b+ 6b5a 1c3a B*4a S*4b 5a6b 4b4a+ 5c6d 7e6d 6c6d
B*4d S*5c 4d3e 6b6c 8b8a 6a6b 4a5a 6c7d 8e8b+ 6b6c
L*7i`
)

// measurePosition 은 (시작 국면 + 수순)으로 국면을 가리킨다. 엔진에 넘기는 형태 그대로다.
type measurePosition struct {
	name  string
	moves []string
}

// 序盤·中盤·終盤이 고루 들어가게 뽑는다.
func measurePositions(t *testing.T) []measurePosition {
	t.Helper()
	a, b := strings.Fields(kifuA), strings.Fields(kifuB)

	pick := func(game string, moves []string, ply int) measurePosition {
		if ply > len(moves) {
			t.Fatalf("%s: %d수를 뽑으려는데 %d수뿐", game, ply, len(moves))
		}
		return measurePosition{
			name:  fmt.Sprintf("%s-%d手", game, ply),
			moves: moves[:ply],
		}
	}
	return []measurePosition{
		pick("A", a, 20), pick("A", a, 45),
		pick("B", b, 30), pick("B", b, 70), pick("B", b, 110), pick("B", b, 150),
	}
}

// TestMeasureKifuIsLegal 은 위 수순이 실제로 둘 수 있는 수순인지 본다.
//
// 엔진이 없어도 도므로 CI에서 매번 돈다. 측정 국면이 조용히 망가지면 그 위에서 정해진
// 상수가 전부 근거를 잃으므로, 데이터 쪽을 코드와 같이 지킨다.
func TestMeasureKifuIsLegal(t *testing.T) {
	for name, kifu := range map[string]string{"A": kifuA, "B": kifuB} {
		pos := shogi.StartPosition()
		moves := strings.Fields(kifu)
		for i, u := range moves {
			m, err := shogi.ParseUSIMove(u)
			if err != nil {
				t.Fatalf("%s %d수째 %q: %v", name, i+1, u, err)
			}
			if err := pos.ValidateMove(m); err != nil {
				t.Fatalf("%s %d수째 %s: 둘 수 없는 수다: %v", name, i+1, u, err)
			}
			pos = pos.Apply(m)
		}
		if ex := pos.InventoryExcess(); len(ex) > 0 {
			t.Errorf("%s: 최종 국면에 말이 초과됨 %v", name, ex)
		}
		t.Logf("%s: %d수 전부 합법. 최종 %s", name, len(moves), pos.SFEN())
	}
}

// TestMeasureDepthMultiPV 는 depth × MultiPV 의 소요 시간과 밴드 적중률을 잰다.
//
// 두 숫자를 **같이** 봐야 한다. 시간만 재면 "빠른데 밴드에 후보가 없는" 설정을 고르게 된다
// (05-roadmap.md 미결). 적응형 상대의 k와 개입 판정의 depth가 여기서 정해진다.
//
// **시간 상한을 두지 않는다.** 여기서 재려는 값이 바로 그 시간이라, 잘라내면 "얼마나
// 느린가"가 뭉개진다. 줄여야 하면 상한이 아니라 재는 칸 수를 줄인다.
//
// 결과는 **평가함수에 종속**이다. 엔진이나 nn.bin 을 바꾸면 다시 재고, 01-core.md §6의
// 밴드 숫자도 같이 다시 본다.
//
//	SHOWGI_USI_CMD=/opt/yaneuraou/run SHOWGI_MEASURE=1 go test ./internal/usi/ -run MeasureDepth -timeout 3h
func TestMeasureDepthMultiPV(t *testing.T) {
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" || os.Getenv("SHOWGI_MEASURE") == "" {
		t.Skip("SHOWGI_USI_CMD + SHOWGI_MEASURE 가 있어야 돈다 — 오래 걸린다")
	}

	e, err := New(cmd, map[string]string{
		"USI_Hash": "128", "Threads": "1", "FV_SCALE": "24",
		"BookFile": "no_book", "USI_OwnBook": "false",
	})
	if err != nil {
		t.Fatalf("엔진 기동 실패: %v", err)
	}
	t.Cleanup(e.Close)

	positions := measurePositions(t)

	// t.Logf 는 테스트가 끝나야 나온다. 오래 도는 측정에서는 진행이 안 보이므로 직접 찍는다.
	fmt.Printf("engine=%s\n국면 %d개: ", e.Name(), len(positions))
	for _, p := range positions {
		fmt.Printf("%s ", p.name)
	}
	fmt.Printf("\n\n%-6s %-4s %10s %10s %9s %s\n", "depth", "k", "합계", "최장", "밴드적중", "국면별 최대낙폭")

	for _, depth := range []int{10, 12, 14} {
		for _, k := range []int{1, 5, 10, 20, 40} {
			if err := e.SetMultiPV(k); err != nil {
				t.Fatalf("SetMultiPV(%d): %v", k, err)
			}

			var total, longest time.Duration
			hits := 0
			drops := make([]string, 0, len(positions))

			for _, p := range positions {
				start := time.Now()
				res, err := e.SearchDepth(t.Context(), shogi.StartSFEN, p.moves, depth)
				elapsed := time.Since(start)
				if err != nil {
					t.Fatalf("%s d%d k%d: %v", p.name, depth, k, err)
				}
				total += elapsed
				if elapsed > longest {
					longest = elapsed
				}
				if len(res.Lines) == 0 {
					drops = append(drops, "-")
					continue
				}

				// 후보의 cp 는 두는 쪽(=컴퓨터) 관점이다. 최선보다 얼마나 손해인지가
				// 곧 플레이어에게 돌아가는 이득이므로, 낙폭이 밴드에 드는 후보를 찾는다.
				best := res.Lines[0].ScoreCp
				worst := 0
				found := false
				for _, l := range res.Lines {
					loss := best - l.ScoreCp
					if loss > worst {
						worst = loss
					}
					if loss >= 100 && loss <= 300 {
						found = true
					}
				}
				if found {
					hits++
				}
				drops = append(drops, fmt.Sprintf("%d", worst))
			}

			fmt.Printf("%-6d %-4d %10s %10s %5d/%d  %v\n",
				depth, k, total.Round(time.Millisecond), longest.Round(time.Millisecond),
				hits, len(positions), drops)
		}
	}
}
