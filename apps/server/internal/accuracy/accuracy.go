// Package accuracy 는 한 판의 精度를 센다. Lichess 의 accuracy 와 같은 식이다
// (lila modules/analyse AccuracyPercent). 판정과 같이 저장된 평가치만 읽고 엔진을 모른다.
//
// 승률만 이 사이트의 것을 쓴다(intervene.WinRateOf). 체스의 cp 척도와 상한 1000cp 는 쇼기에
// 맞지 않고, 개입 판정과 그래프가 이미 이 승률로 말한다. 기준은 journal §144.
package accuracy

import (
	"math"

	"github.com/jovid18/show-gi/apps/server/internal/eval"
	"github.com/jovid18/show-gi/apps/server/internal/handicap"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
)

// Move 는 한 手의 精度(0~100)다. before·after 는 둔 쪽 관점의 승률(0~100)이다.
//
// 승률이 오르거나 그대로면 100이다. 곡선의 상수와 1점 보정은 Lichess 값 그대로다.
func Move(before, after float64) float64 {
	if after >= before {
		return 100
	}
	raw := 103.1668100711649*math.Exp(-0.04354415386753951*(before-after)) - 3.166924740191411
	return min(max(raw+1, 0), 100)
}

// Game 은 한 판에서 color 쪽의 精度(0~100)다. 셀 手가 없으면 ok=false 다.
//
// scores[i] 는 i+1手 뒤 국면의 先手 관점 평가치이고 nil 이면 아직 재지 않은 것이다. 0手
// 국면은 저장되지 않아 手合割 기준점(승률 50)으로 둔다.
//
// 手마다의 精度를 두 평균의 평균으로 묶는다. 하나는 형세가 흔들리는 구간에 무게를 준
// 평균이고(창 안 승률의 표준편차), 하나는 조화 평균이라 큰 실수 하나가 덮이지 않는다.
// 앞뒤 어느 한쪽이라도 비어 있는 手는 뺀다.
func Game(startSFEN string, color shogi.Color, scores []*eval.Score) (float64, bool) {
	start, err := shogi.ParseSFEN(startSFEN)
	if err != nil {
		return 0, false
	}
	base := handicap.BaselineCpFor(startSFEN, shogi.Black)

	// 先手 관점 승률(0~100). 0手는 기준점이다.
	wins := make([]*float64, len(scores)+1)
	even := 50.0
	wins[0] = &even
	for i, s := range scores {
		if s != nil {
			w := 100 * intervene.WinRateOf(*s, base)
			wins[i+1] = &w
		}
	}

	return fromWins(start.Turn, color, wins)
}

// fromWins 는 Game 의 본체다. wins[0] 은 0手 국면이고 wins[i] 는 i手 뒤, 先手 관점 승률이다.
func fromWins(first, color shogi.Color, wins []*float64) (float64, bool) {
	plies := len(wins) - 1
	window := min(max(plies/10, 2), 8)
	weights := volatility(wins, window)

	var sumAW, sumW, sumInv float64
	n := 0
	for i := range plies {
		mover := first
		if i%2 == 1 {
			mover = mover.Other()
		}
		if mover != color {
			continue
		}
		prev, next, w := wins[i], wins[i+1], weights[i]
		if prev == nil || next == nil || w == nil {
			continue
		}
		// 先手 관점 승률이라 後手는 오르내림이 뒤집힌다.
		before, after := *prev, *next
		if mover == shogi.White {
			before, after = 100-*prev, 100-*next
		}
		acc := Move(before, after)
		sumAW += acc * *w
		sumW += *w
		sumInv += 1 / max(acc, 1)
		n++
	}
	if n == 0 || sumW == 0 {
		return 0, false
	}
	weighted := sumAW / sumW
	harmonic := float64(n) / sumInv
	return (weighted + harmonic) / 2, true
}

// volatility 는 手마다의 무게다. i手의 무게는 그 手 무렵 window 개 승률의 표준편차이고
// 0.5~12로 자른다. 창 안에 빈 칸이 있으면 nil 이다.
//
// 창은 처음 window−2 手가 맨 앞 창을 함께 쓰고, 그 뒤로 한 칸씩 민다. 手 수와 창 수가
// 같아진다.
func volatility(wins []*float64, window int) []*float64 {
	head := min(window, len(wins))
	var windows [][]*float64
	for range head - 2 {
		windows = append(windows, wins[:head])
	}
	if len(wins) <= window {
		windows = append(windows, wins)
	} else {
		for i := 0; i+window <= len(wins); i++ {
			windows = append(windows, wins[i:i+window])
		}
	}
	out := make([]*float64, len(windows))
	for i, w := range windows {
		if sd, ok := stddev(w); ok {
			v := min(max(sd, 0.5), 12)
			out[i] = &v
		}
	}
	return out
}

// stddev 는 모표준편차다. 빈 칸이 하나라도 있으면 ok=false 다.
func stddev(xs []*float64) (float64, bool) {
	if len(xs) == 0 {
		return 0, false
	}
	var sum float64
	for _, x := range xs {
		if x == nil {
			return 0, false
		}
		sum += *x
	}
	mean := sum / float64(len(xs))
	var sq float64
	for _, x := range xs {
		sq += (*x - mean) * (*x - mean)
	}
	return math.Sqrt(sq / float64(len(xs))), true
}
