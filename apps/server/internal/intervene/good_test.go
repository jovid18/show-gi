package intervene

import (
	"testing"

	"github.com/jovid18/show-gi/apps/server/internal/eval"
)

func TestIsGood(t *testing.T) {
	cases := []struct {
		name string
		in   GoodInput
		want bool
	}{
		{"차가 작다", GoodInput{Best: eval.Cp(100), Second: eval.Cp(50)}, false},
		{"차가 크다", GoodInput{Best: eval.Cp(100), Second: eval.Cp(-400)}, true},
		// 호각에서 GoodGapWin(0.10)은 cp로 약 240이다.
		{"경계 안쪽", GoodInput{Best: eval.Cp(0), Second: eval.Cp(-250)}, true},
		{"경계 바깥", GoodInput{Best: eval.Cp(0), Second: eval.Cp(-200)}, false},
		{"다른 수는 詰まされる", GoodInput{Best: eval.Cp(0), Second: eval.Mate(-5)}, true},
		{"이기는 詰み은 부르지 않는다", GoodInput{Best: eval.Mate(3), Second: eval.Cp(0)}, false},
		// 이미 크게 이긴 국면은 cp 차가 커도 승률이 움직이지 않는다.
		{"이미 갈린 국면", GoodInput{Best: eval.Cp(3000), Second: eval.Cp(2000)}, false},
		// 駒落ち는 기준점에서 잰다. 같은 cp 차도 기준점 근처라 승률 차가 크다.
		{"기준점에서 잰다", GoodInput{Best: eval.Cp(1100), Second: eval.Cp(700), BaselineCp: 1000}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsGood(c.in); got != c.want {
				t.Errorf("IsGood = %v (gap %.3f), want %v", got, GoodGap(c.in), c.want)
			}
		})
	}
}

func TestGoodGapIsNeverNegative(t *testing.T) {
	if g := GoodGap(GoodInput{Best: eval.Cp(-100), Second: eval.Cp(100)}); g != 0 {
		t.Errorf("GoodGap = %v, want 0", g)
	}
}
