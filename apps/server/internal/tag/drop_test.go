package tag

import (
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

func drop(t *testing.T, usi string) shogi.Move {
	t.Helper()
	m, err := shogi.ParseUSIMove(usi)
	if err != nil {
		t.Fatalf("%s: %v", usi, err)
	}
	return m
}

func dropNames(t *testing.T, sfen, usi string, c shogi.Color) []string {
	t.Helper()
	return codes(DropTesuji(forkBoard(t, sfen), drop(t, usi), c))
}

// **叩きの歩** — 5三의 後手 金 머리인 5四에 歩를 打つ.
func TestPawnDroppedOnAGoldsHeadIsTatakiNoFu(t *testing.T) {
	got := dropNames(t, "4k4/9/4g4/4P4/9/9/9/9/4K4 b - 1", "P*5d", shogi.Black)
	if len(got) != 1 || got[0] != "tataki_no_fu" {
		t.Errorf("[tataki_no_fu] 를 기대했는데 %v", got)
	}
}

// **판만 봐서는 알 수 없다.** 같은 국면이라도 그 歩가 걸어온 것이면 叩き가 아니다 —
// 이 부류가 방금 둔 수를 받는 이유가 여기 있고, 그 시그니처가 없으면 이 구별이 사라진다.
func TestAPawnThatWalkedThereIsNotTataki(t *testing.T) {
	got := dropNames(t, "4k4/9/4g4/4P4/9/9/9/9/4K4 b - 1", "5e5d", shogi.Black)
	if len(got) != 0 {
		t.Errorf("打った 것이 아닌데 %v 가 떴다", got)
	}
}

// **머리의 駒를 가린다.** 歩의 머리는 合わせの歩이고 玉의 머리는 또 다른 이름이라,
// 넓히면 다른 手筋에 叩き라는 이름을 붙이게 된다.
func TestTatakiOnlyCountsGoldAndSilver(t *testing.T) {
	for _, tc := range []struct{ name, sfen string }{
		{"歩の頭", "4k4/9/4p4/4P4/9/9/9/9/4K4 b - 1"},
		{"玉の頭", "9/9/4k4/4P4/9/9/9/9/4K4 b - 1"},
		{"飛の頭", "4k4/9/4r4/4P4/9/9/9/9/4K4 b - 1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := dropNames(t, tc.sfen, "P*5d", shogi.Black); len(got) != 0 {
				t.Errorf("%v 가 떴다", got)
			}
		})
	}
	if got := dropNames(t, "4k4/9/4s4/4P4/9/9/9/9/4K4 b - 1", "P*5d", shogi.Black); len(got) != 1 {
		t.Errorf("銀の頭는 叩きの歩다: %v", got)
	}
}

// **垂れ歩** — 적진 한 칸 앞(先手 4段)에 打って 다음 수의 成りを狙う.
func TestPawnDroppedJustBeforeTheCampIsTareFu(t *testing.T) {
	got := dropNames(t, "4k4/9/9/4P4/9/9/9/9/4K4 b - 1", "P*5d", shogi.Black)
	if len(got) != 1 || got[0] != "tare_fu" {
		t.Errorf("[tare_fu] 를 기대했는데 %v", got)
	}
}

// 한 段 더 뒤는 그냥 歩打다. 「다음 수로 成る」가 노림이 되는 자리라야 垂れ歩다.
func TestAPawnDroppedFurtherBackIsJustAPawn(t *testing.T) {
	if got := dropNames(t, "4k4/9/9/9/4P4/9/9/9/4K4 b - 1", "P*5e", shogi.Black); len(got) != 0 {
		t.Errorf("5五의 歩打에 %v 가 떴다", got)
	}
}

// 後手도 같은 규칙에서 나온다 — 적진의 방향만 뒤집힌다.
func TestDropTesujiMirrorsForGote(t *testing.T) {
	// 5六에 後手 歩. 그 앞(5七)이 비어 있고 적진은 7段부터다
	if got := dropNames(t, "4k4/9/9/9/9/4p4/9/9/4K4 w - 1", "P*5f", shogi.White); len(got) != 1 || got[0] != "tare_fu" {
		t.Errorf("後手의 垂れ歩: %v", got)
	}
	// 5七의 先手 金 머리에 打つ
	if got := dropNames(t, "4k4/9/9/9/9/4p4/4G4/9/4K4 w - 1", "P*5f", shogi.White); len(got) != 1 || got[0] != "tataki_no_fu" {
		t.Errorf("後手의 叩きの歩: %v", got)
	}
}

// 상대의 打으로는 내 이름이 나오지 않는다. 색을 안 보면 상대의 手筋이 내 화면에 뜬다.
func TestDropTesujiNeedsMyOwnPawn(t *testing.T) {
	if got := dropNames(t, "4k4/9/9/4P4/9/9/9/9/4K4 b - 1", "P*5d", shogi.White); len(got) != 0 {
		t.Errorf("先手의 歩인데 後手 手筋로 %v 가 떴다", got)
	}
}
