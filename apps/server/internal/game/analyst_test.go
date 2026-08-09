package game

import (
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// 反駁手順의 재료는 전부 룰 엔진에서 나온다. **엔진 없이 도는 테스트**여야 한다 —
// 이 레포에서 제일 흔한 함정이 「환경변수가 없으면 조용히 skip 되고 초록으로 보인다」이고,
// 화면에 그대로 나가는 표기가 거기 걸리면 안 된다(06-status.md §15).

// 5四에서 駒를 주고받는 국면. 사람이 銀으로 歩를 따면 金이 되딸 수 있다.
const exchangeSFEN = "1b2k4/9/5g3/4p4/4S4/9/9/9/4R3K b - 1"

// 銀으로 5四의 歩를 딴다. 이 한 수가 판정 대상이다.
var tookThePawn = []string{"5e5d"}

// 벌하는 수가 몇 수 뒤에 오는 국면. 그 앞의 조용한 수는 준비 수순이라 남는다 —
// 카테고리가 이유를 못 대는 자리가 바로 이 모양이다(06-status.md §17).
func TestRefutationLineRunsUntilTheDamageLands(t *testing.T) {
	pv := []string{"5a4a", "1i2i", "4c5d"}

	line := refutationLine(exchangeSFEN, tookThePawn, pv, RefutationPlies)

	want := []Move{
		{USI: "5a4a", Ja: "△4一玉", By: SideEngine},
		{USI: "1i2i", Ja: "▲2九玉", By: SideHuman},
		{USI: "4c5d", Ja: "△5四金", By: SideEngine},
	}
	if len(line) != len(want) {
		t.Fatalf("수순 길이 %d, 기대 %d: %+v", len(line), len(want), line)
	}
	for i, m := range line {
		if m != want[i] {
			t.Errorf("%d번째 수 %+v, 기대 %+v", i, m, want[i])
		}
	}
}

// 첫 수는 언제나 상대의 수다. 판정하는 것이 사람의 수이기 때문이고, 사람이 어느 색을
// 잡았는지와 무관하다. 화면은 이 성질에 기대어 **첫 수만** 판에 긋는다.
func TestRefutationLineStartsWithTheOpponent(t *testing.T) {
	line := refutationLine(exchangeSFEN, tookThePawn, []string{"4c5d"}, RefutationPlies)
	if len(line) != 1 || line[0].By != SideEngine {
		t.Fatalf("첫 수가 상대의 수가 아니다: %+v", line)
	}
}

// **길이는 국면이 정한다.** 角을 던지면 되따는 한 수로 이유가 끝나고, 거기에 수를
// 더 붙이면 그건 정보가 아니라 잡음이다. 사용자 피드백이 이 자리에서 나왔다.
func TestRefutationLineStopsWhenTheFirstMovePunishes(t *testing.T) {
	// 角交換을 유도해 두고 그 角을 그냥 던진다. 벌하는 수는 되따는 한 수뿐이다.
	thrownBishop := []string{"7g7f", "3c3d", "8h2b+"}
	pv := []string{"3a2b", "B*5e", "2b3c"}

	line := refutationLine(shogi.StartSFEN, thrownBishop, pv, RefutationPlies)

	if len(line) != 1 {
		t.Fatalf("수순 길이 %d, 기대 1: %+v", len(line), line)
	}
	// 물러진 수의 도착 칸을 되따는 수라 「同」이어야 한다. 이게 안 붙으면 화면에서
	// 「무엇을 벌하는 수인지」가 사라진다.
	if line[0].Ja != "△同銀" {
		t.Errorf("벌하는 수의 표기가 %q, 기대 %q", line[0].Ja, "△同銀")
	}
}

// 상한 안에서 아무 일도 안 일어나면 벌하는 첫 수만 남는다. **모르는 것을 길이로 메우지 않는다.**
func TestRefutationLineOnlyLooksAsFarAsTheLimit(t *testing.T) {
	pv := []string{"5a4a", "1i2i", "4c5d"} // 따는 수가 상한 밖이다

	line := refutationLine(exchangeSFEN, tookThePawn, pv, 2)

	if len(line) != 1 {
		t.Fatalf("수순 길이 %d, 기대 1: %+v", len(line), line)
	}
}

// **엔진 출력을 믿지 않는다.** 못 두는 수가 섞여 오면 거기서 끊는다 — 건너뛰고 이어
// 붙이지 않는다. 뒤에 오는 수는 그 수를 둔 국면의 것이라, 이어 붙이면 없는 수순이 된다.
func TestRefutationLineCutsAtAnUnplayableMove(t *testing.T) {
	cases := map[string][]string{
		"읽을 수 없는 좌표": {"5a4a", "zz9z", "4c5d"},
		"둘 수 없는 수":   {"5a4a", "5a5b", "4c5d"}, // 玉이 이미 4一로 옮겨 5一이 비었다
	}

	for name, pv := range cases {
		t.Run(name, func(t *testing.T) {
			line := refutationLine(exchangeSFEN, tookThePawn, pv, RefutationPlies)
			if len(line) != 1 {
				t.Fatalf("수순 길이 %d, 기대 1: %+v", len(line), line)
			}
		})
	}
}

func TestRefutationLineIsEmptyWithoutAPV(t *testing.T) {
	if line := refutationLine(exchangeSFEN, tookThePawn, nil, RefutationPlies); line != nil {
		t.Errorf("PV가 없는데 수순이 나왔다: %+v", line)
	}
}

func TestTrimRefutation(t *testing.T) {
	cases := map[string]struct {
		settles []bool
		want    int
	}{
		"바로 벌한다":        {[]bool{true, false, false}, 1},
		"몇 수 앞에서 벌어진다":  {[]bool{false, false, true, false}, 3},
		"계속 따도 처음까지만":   {[]bool{true, true, true, true}, 1},
		"조용한 수뿐이면 첫 수만": {[]bool{false, false, false}, 1},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := trimRefutation(c.settles); got != c.want {
				t.Errorf("trimRefutation(%v) = %d, 기대 %d", c.settles, got, c.want)
			}
		})
	}
}
