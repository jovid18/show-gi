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

	_, _, line := refutationLine(exchangeSFEN, tookThePawn, pv, RefutationPlies)

	want := []RefutationMove{
		{USI: "5a4a", Ja: "△4一玉", By: SideEngine},
		{USI: "1i2i", Ja: "▲2九玉", By: SideHuman},
		{USI: "4c5d", Ja: "△5四金", By: SideEngine},
	}
	assertLine(t, line, want)
}

// **시작한 교환은 끝까지 보여준다.** `△同金` 만 그리면 金이 銀을 그냥 딴 것으로 읽히는데
// 실제로는 되따고 또 되딴다. 반쪽이 틀린 것보다 두 수 긴 편이 낫다.
func TestRefutationLineShowsTheWholeExchange(t *testing.T) {
	pv := []string{"4c5d", "5i5d", "8a5d", "1i2i"}

	_, _, line := refutationLine(exchangeSFEN, tookThePawn, pv, RefutationPlies)

	want := []RefutationMove{
		{USI: "4c5d", Ja: "△同金", By: SideEngine},
		{USI: "5i5d", Ja: "▲同飛", By: SideHuman},
		{USI: "8a5d", Ja: "△同角", By: SideEngine},
	}
	assertLine(t, line, want)
}

// 첫 수는 언제나 상대의 수다. 판정하는 것이 사람의 수이기 때문이고, 사람이 어느 색을
// 잡았는지와 무관하다. 화면은 이 성질에 기대어 **첫 수만** 판에 긋는다.
func TestRefutationLineStartsWithTheOpponent(t *testing.T) {
	_, _, line := refutationLine(exchangeSFEN, tookThePawn, []string{"4c5d"}, RefutationPlies)
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

	_, _, line := refutationLine(shogi.StartSFEN, thrownBishop, pv, RefutationPlies)

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

	_, _, line := refutationLine(exchangeSFEN, tookThePawn, pv, 2)

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
			_, _, line := refutationLine(exchangeSFEN, tookThePawn, pv, RefutationPlies)
			if len(line) != 1 {
				t.Fatalf("수순 길이 %d, 기대 1: %+v", len(line), line)
			}
		})
	}
}

func TestRefutationLineIsEmptyWithoutAPV(t *testing.T) {
	if _, _, line := refutationLine(exchangeSFEN, tookThePawn, nil, RefutationPlies); line != nil {
		t.Errorf("PV가 없는데 수순이 나왔다: %+v", line)
	}
}

const doubleCheckKifu = `▲7六歩 △5二玉 ▲6六歩 △4二銀 ▲2六歩 △8四歩 ▲2五歩 △3四歩
▲7七角 △3二金 ▲7八銀 △3三銀 ▲6八飛 △6四歩 ▲4八玉 △5四歩
▲3八玉 △8五歩 ▲2八玉 △9四歩 ▲3八銀 △8四飛 ▲5八金左 △1四歩
▲1六歩 △5一金 ▲9六歩 △7四歩 ▲6七銀 △4四歩 ▲5六銀 △7三桂
▲8八角 △7二銀 ▲7七桂 △1三角 ▲6五歩 △8六歩 ▲同歩 △同飛
▲6四歩 △8七飛成 ▲8五歩 △9五歩 ▲6三歩成 △同銀 ▲9五歩 △4二金寄
▲8四歩 △3一角 ▲8三歩成 △6二金 ▲7三と △同金 ▲6四歩 △同銀
▲6五歩 △5三銀 ▲9四歩 △3二金 ▲9三歩成 △4二角 ▲9二と △6二銀
▲9一と △9八歩 ▲同香 △4一玉 ▲9七角 △同角成 ▲同香 △7七龍
▲8二角 △4二金 ▲6四歩 △2六桂 ▲2七銀 △7二金 ▲9三角成 △9七龍
▲8四馬 △3二玉 ▲6六桂 △5三香 ▲2六銀 △7三銀 ▲8五馬 △7七龍
▲6三歩成 △6七歩 ▲同飛 △7九龍 ▲4八金寄 △6三金 ▲6四歩 △同金
▲7四桂 △8九龍 ▲8六歩 △7八角 ▲6八飛 △8四歩 ▲9四馬 △6五歩
▲同銀 △5五金 ▲5六歩 △6五金 ▲同飛 △6四歩 ▲7五飛 △5五歩
▲同歩 △1五歩 ▲同歩 △5六歩 ▲6二桂成 △6七角成 ▲7三飛成 △4九龍
▲同金 △同馬 ▲6四龍 △3九金 ▲5四歩 △3八金打 ▲2七玉 △2二玉
▲5一銀 △3二金 ▲5三歩成 △5七歩成`

// 両王手는 **먹어서 풀 수 없다.** 플레이 테스트에서 「同銀으로 먹으면 되는 것 아닌가」가
// 나온 국면이고, 그때 화면이 그 이유를 말하지 못했다(06-status.md §20).
//
// ▲1七銀 뒤 △2八金은 金이 3八에서 나가면서 **4九馬의 대각선을 연다** — 金과 馬가 동시에
// 王手라 玉을 움직일 수밖에 없다. 붉은 화살표 두 줄이 곧 그 사실이다.
//
// **엔진 없이 돈다.** 王手를 거는 말을 찾는 것은 룰 엔진의 일이고, 화면에 나가는 단언이라
// 환경변수가 없으면 조용히 skip 되는 자리에 두지 않는다.
func TestCheckLinesFindsBothCheckersOfADoubleCheck(t *testing.T) {
	usis, _ := kifuToUSI(t, doubleCheckKifu)

	pos, err := positionAfter(shogi.StartSFEN, append(usis, "2f1g", "3h2h"))
	if err != nil {
		t.Fatalf("국면 복원: %v", err)
	}

	got := checkLines(pos)
	want := []Attack{{From: "2h", To: "2g"}, {From: "4i", To: "2g"}}
	if len(got) != len(want) {
		t.Fatalf("王手 줄 %d개, 기대 %d개: %+v", len(got), len(want), got)
	}
	for i, a := range got {
		if a != want[i] {
			t.Errorf("%d번째 줄 %+v, 기대 %+v", i, a, want[i])
		}
	}

	// 두 줄이라는 것과 「먹어서 못 푼다」가 같은 사실이어야 한다. 玉을 움직이는 수뿐이다.
	for _, m := range pos.LegalMoves() {
		if pos.Board[m.From].Type() != shogi.King {
			t.Errorf("両王手인데 玉 말고 두는 수가 있다: %s", m.USI())
		}
	}
}

func TestTrimRefutation(t *testing.T) {
	quiet := refutationStep{captureSq: -1}
	check := refutationStep{settles: true, captureSq: -1, gaveCheck: true}
	takes := func(sq int) refutationStep { return refutationStep{settles: true, captureSq: sq} }

	cases := map[string]struct {
		steps []refutationStep
		want  int
	}{
		"바로 벌하고 끝난다":     {[]refutationStep{takes(30), quiet, quiet}, 1},
		"몇 수 앞에서 벌어진다":   {[]refutationStep{quiet, quiet, takes(30), quiet}, 3},
		"같은 칸의 교환은 끝까지":  {[]refutationStep{quiet, takes(30), takes(30), takes(30), quiet}, 4},
		"다른 칸이면 별개 교환이다": {[]refutationStep{takes(30), takes(41), takes(41)}, 1},
		// **王手는 혼자 서지 못한다.** 응수가 강제라, 답을 빼면 「먹으면 되지 않나」가 된다.
		"王手는 응수까지":      {[]refutationStep{check, quiet, quiet}, 2},
		"連続王手는 이어지는 동안": {[]refutationStep{check, takes(30), check, quiet, quiet}, 4},
		"조용한 수뿐이면 첫 수만": {[]refutationStep{quiet, quiet, quiet}, 1},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := trimRefutation(c.steps); got != c.want {
				t.Errorf("trimRefutation(%v) = %d, 기대 %d", c.steps, got, c.want)
			}
		})
	}
}

// assertLine 은 수와 표기를 견준다. **국면은 값으로 안 박는다** — SFEN 문자열을 테스트에
// 적어두면 룰 엔진이 아니라 그 문자열을 지키게 된다. 있는지와 매 수 달라지는지만 본다.
func assertLine(t *testing.T, got, want []RefutationMove) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("수순 길이 %d, 기대 %d: %+v", len(got), len(want), got)
	}
	seen := map[string]bool{}
	for i, m := range got {
		if m.USI != want[i].USI || m.Ja != want[i].Ja || m.By != want[i].By {
			t.Errorf("%d번째 수 %+v, 기대 %+v", i, m, want[i])
		}
		// 화면이 이 값으로 판을 그린다. 비어 있으면 넘기기가 통째로 안 된다.
		if m.SFEN == "" {
			t.Errorf("%d번째 수(%s)에 국면이 없다", i, m.USI)
		}
		if seen[m.SFEN] {
			t.Errorf("%d번째 수(%s)에서 국면이 안 나아갔다", i, m.USI)
		}
		seen[m.SFEN] = true
	}
}
