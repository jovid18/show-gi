package accuracy

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/eval"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// testdata/lichess_cases.json 은 lila 의 gameAccuracy 를 줄 단위로 옮긴 Python
// (testdata/lichess_ref.py)이 낸 값이다. 승률 입력이 같으면 결과도 같아야 한다.
func TestGameMatchesTheLichessReference(t *testing.T) {
	raw, err := os.ReadFile("testdata/lichess_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		FirstBlack   bool       `json:"firstBlack"`
		Wins         []*float64 `json:"wins"`
		Black, White *float64
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		first := shogi.White
		if c.FirstBlack {
			first = shogi.Black
		}
		for color, want := range map[shogi.Color]*float64{shogi.Black: c.Black, shogi.White: c.White} {
			got, ok := fromWins(first, color, c.Wins)
			switch {
			case want == nil && ok:
				t.Errorf("case %d %v: %.6f 가 나왔다, 셀 手가 없어야 한다", i, color, got)
			case want != nil && !ok:
				t.Errorf("case %d %v: 값이 없다, want %.6f", i, color, *want)
			case want != nil && math.Abs(got-*want) > 1e-9:
				t.Errorf("case %d %v: %.9f, want %.9f", i, color, got, *want)
			}
		}
	}
}

func TestMoveIsPerfectUnlessTheWinRateDrops(t *testing.T) {
	if got := Move(40, 55); got != 100 {
		t.Errorf("오른 手 = %v, want 100", got)
	}
	if got := Move(60, 60); got != 100 {
		t.Errorf("그대로인 手 = %v, want 100", got)
	}
	// 10 떨어지면 약 65점이다. 크게 떨어지면 1점 보정만 남는다.
	if got := Move(60, 50); math.Abs(got-64.58) > 0.01 {
		t.Errorf("10 떨어진 手 = %.3f, want ≈64.58", got)
	}
	if got := Move(90, 10); math.Abs(got-1) > 0.01 {
		t.Errorf("80 떨어진 手 = %.3f, want ≈1", got)
	}
}

// 평가치는 先手 관점으로 저장된다. 後手 자리에서 뒤집지 않으면 後手의 悪手가 好手로 센다.
func TestGameReadsTheGoteSideFromItsOwnPointOfView(t *testing.T) {
	cp := func(v int) *eval.Score { s := eval.Cp(v); return &s }
	// 後手가 2手目에 크게 망친다. 先手 관점 +1500 이 後手에게는 나쁜 수다.
	scores := []*eval.Score{cp(0), cp(1500), cp(1500), cp(1500)}
	gote, ok := Game(shogi.StartSFEN, shogi.White, scores)
	if !ok {
		t.Fatal("後手 精度가 없다")
	}
	sente, _ := Game(shogi.StartSFEN, shogi.Black, scores)
	if !(gote < 50 && sente > 99) {
		t.Errorf("先手 %.1f · 後手 %.1f, 後手만 낮아야 한다", sente, gote)
	}
}

// 詰み은 승률 100/0이다(intervene.WinRateOf). 이기는 詰み을 놓치지 않은 手는 100점이다.
func TestAMateCountsAsAFullWinRate(t *testing.T) {
	m := eval.Mate(3)
	scores := []*eval.Score{&m}
	if got, ok := Game(shogi.StartSFEN, shogi.Black, scores); !ok || got != 100 {
		t.Errorf("詰み으로 가는 手 = %.3f(ok=%v), want 100", got, ok)
	}
}

// 아직 재지 않은 手는 빼고, 전부 비었으면 값이 없다. 0으로 채우면 호각으로 읽힌다.
func TestGameSkipsUnmeasuredPlies(t *testing.T) {
	if _, ok := Game(shogi.StartSFEN, shogi.Black, []*eval.Score{nil, nil, nil}); ok {
		t.Error("잰 手가 없는데 값이 나왔다")
	}
}
