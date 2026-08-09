package game

import (
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 反駁手順의 재료는 전부 룰 엔진에서 나온다. **엔진 없이 도는 테스트**여야 한다 —
// 이 레포에서 제일 흔한 함정이 「환경변수가 없으면 조용히 skip 되고 초록으로 보인다」이고,
// 화면에 그대로 나가는 표기가 거기 걸리면 안 된다(06-status.md §15).

// 角交換을 유도해 두고 그 角을 그냥 던진 국면. 벌하는 수가 되따는 수라 「同」이 나온다.
var thrownBishop = []string{"7g7f", "3c3d", "8h2b+"}

func TestRefutationLineReadsAsAKifu(t *testing.T) {
	pv := []string{"3a2b", "B*5e", "2b3c"}

	line := refutationLine(shogi.StartSFEN, thrownBishop, pv, RefutationPlies)

	if len(line) != len(pv) {
		t.Fatalf("수순 길이 %d, 기대 %d: %+v", len(line), len(pv), line)
	}

	// 첫 수는 언제나 상대의 수다. 판정하는 것이 사람의 수이기 때문이고,
	// 사람이 어느 색을 잡았는지와 무관하다.
	want := []Side{SideEngine, SideHuman, SideEngine}
	for i, m := range line {
		if m.By != want[i] {
			t.Errorf("%d번째 수의 대국자 %q, 기대 %q", i, m.By, want[i])
		}
		if m.USI != pv[i] {
			t.Errorf("%d번째 수 %q, 기대 %q", i, m.USI, pv[i])
		}
		if m.Ja == "" {
			t.Errorf("%d번째 수(%s)에 棋譜 표기가 없다", i, m.USI)
		}
	}

	// 물러진 수의 도착 칸을 되따는 수라 「同」이어야 한다. 이게 안 붙으면 화면에서
	// 「무엇을 벌하는 수인지」가 사라진다.
	if line[0].Ja != "△同銀" {
		t.Errorf("벌하는 수의 표기가 %q, 기대 %q", line[0].Ja, "△同銀")
	}
}

func TestRefutationLineStopsAtTheLimit(t *testing.T) {
	pv := []string{"3a2b", "B*5e", "2b3c", "5e7c+", "3c4d"}

	line := refutationLine(shogi.StartSFEN, thrownBishop, pv, 2)

	if len(line) != 2 {
		t.Fatalf("수순 길이 %d, 기대 2: %+v", len(line), line)
	}
}

// **엔진 출력을 믿지 않는다.** 못 두는 수가 섞여 오면 거기서 끊고 그때까지만 그린다 —
// 화면에 나가는 단언이라 틀린 것을 그리느니 짧게 그린다.
func TestRefutationLineCutsAtAnUnplayableMove(t *testing.T) {
	cases := map[string][]string{
		"읽을 수 없는 좌표": {"3a2b", "zz9z"},
		"둘 수 없는 수":   {"3a2b", "1a1b"}, // 黑 차례에 白의 香를 움직이는 수
	}

	for name, pv := range cases {
		t.Run(name, func(t *testing.T) {
			line := refutationLine(shogi.StartSFEN, thrownBishop, pv, RefutationPlies)
			if len(line) != 1 {
				t.Fatalf("수순 길이 %d, 기대 1: %+v", len(line), line)
			}
		})
	}
}

func TestRefutationLineIsEmptyWithoutAPV(t *testing.T) {
	if line := refutationLine(shogi.StartSFEN, thrownBishop, nil, RefutationPlies); line != nil {
		t.Errorf("PV가 없는데 수순이 나왔다: %+v", line)
	}
}
