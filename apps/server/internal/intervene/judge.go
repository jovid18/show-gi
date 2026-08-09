// Package intervene 은 개입할지 말지를 정한다.
//
// **엔진을 모른다.** 입력은 이미 구해진 평가치와 詰み 거리뿐이고, 여기서 하는 일은
// 그 숫자를 제품의 판단으로 바꾸는 것이다. 그래야 상수(K·임계치)를 흔들어 보는 일이
// 엔진을 띄우지 않고 된다 — D3에서 그 상수들을 실측으로 잡아야 한다.
//
// 판정의 근거는 docs/01-core.md §2.
package intervene

import "math"

// K 는 cp를 승률로 바꿀 때의 기울기다. **[미확정: 실측으로 보정]**
//
// 크면 완만해지고(같은 cp 차이가 승률을 덜 움직인다) 작으면 가팔라진다.
// 水匠5의 cp 척도 위에서 다시 잡아야 한다.
const K = 600.0

// WinRate 는 수번 측 관점 cp를 승률로 바꾼다.
//
// cp를 그대로 쓰지 않는 이유는 우세 구간에서 의미가 압축되기 때문이다 —
// -800이 국면마다 다른 뜻이 된다.
func WinRate(cp int) float64 {
	return 1 / (1 + math.Exp(-float64(cp)/K))
}

// Level 은 개입 임계치를 정하는 실력 구간이다.
type Level int

const (
	// Beginner 는 프로필이 없을 때의 기본값이다.
	//
	// 제일 너그러운 쪽을 기본으로 둔다. 학습 앱에서 과잉 개입은 **잔소리**가 되고,
	// 이 제품이 피하려는 바로 그것이다(§1). 실력 추정이 붙으면 좁혀진다.
	Beginner     Level = iota // ~15급
	Novice                    // 10~14급
	Intermediate              // 5~9급
)

// Threshold 는 레벨별 승률 낙폭 임계치(0~1)다.
func (l Level) Threshold() float64 {
	switch l {
	case Novice:
		return 0.18
	case Intermediate:
		return 0.12
	default:
		return 0.25
	}
}

// ObservePlies 는 개입하지 않는 초반 구간이다.
//
// 요구사항이자 **실력 관측 구간**이기도 하다 — 개입이 없는 수가 순수한 실력 신호다.
const ObservePlies = 20

// Kind 는 개입의 종류다. DB의 interventions.kind 와 같은 값을 쓴다.
type Kind string

const (
	KindNone    Kind = ""
	KindBlunder Kind = "blunder"
)

// Input 은 한 수를 판정하는 데 필요한 전부다.
type Input struct {
	// Ply 는 이 수가 몇 번째인가(1부터). 관측 구간 판정에 쓴다.
	Ply int

	// BestCp 는 착수 **전** 국면의 최선수 평가치. 두는 쪽 관점.
	BestCp int
	// AfterCp 는 착수 **후** 국면의 평가치를 **둔 쪽 관점으로 뒤집은 것**.
	//
	// 엔진은 늘 수번 측 관점으로 답하므로, 착수 후에는 상대 관점이 된다.
	// 부르는 쪽에서 부호를 뒤집어 넘긴다 — 여기서 뒤집으면 "누구 관점인가"가
	// 두 군데에 흩어진다.
	AfterCp int

	// MateBefore 는 착수 전에 내가 가지고 있던 詰み 거리(手数). 없으면 0.
	MateBefore int
	// MateAfter 는 착수 후에도 남아 있는 詰み 거리. 없으면 0.
	MateAfter int

	Level Level
}

// Verdict 는 판정 결과다.
type Verdict struct {
	Kind Kind
	// DeltaWin 은 승률 낙폭(0~1). 종반 판정으로 걸렸을 때는 0에 가까울 수 있다 —
	// 승률이 포화하는 구간이라 그 값이 작다는 것이 바로 詰み 거리를 쓰는 이유다.
	DeltaWin float64
	// LostMate 는 종반 판정으로 걸렸는가. 설명 문구가 갈린다.
	LostMate bool
}

// JudgeMatePlies 는 **판정에 쓰는** 詰み 거리의 상한이다.
//
// 탐색은 11까지 하지만(비용이 같다) 판정은 5까지만 한다. 5手詰까지는 실제 대국에
// 자주 나오고 詰将棋의 표준 단위라 **배울 값이 있는 실수**인 반면, 11手詰을 놓친 것은
// 8급에게 실수가 아니다 — 애초에 보이지 않는 수순이다.
//
// **[미확정]** 레벨별로 다르게 할지.
const JudgeMatePlies = 5

// Judge 는 한 수를 판정한다.
func Judge(in Input) Verdict {
	if in.Ply <= ObservePlies {
		return Verdict{}
	}

	// 종반 — 승률이 포화해 낙폭이 판정력을 잃는 구간이다.
	//
	// 이 규칙이 필요한 쪽은 **이기고 있는 쪽뿐**이다. 지는 쪽(詰まされる 수를 둔 경우)은
	// 승률이 멀쩡히 움직여 아래 낙폭 판정이 이미 잡는다.
	if in.MateBefore > 0 && in.MateBefore <= JudgeMatePlies {
		lost := in.MateAfter == 0 || in.MateAfter > in.MateBefore+2
		if lost {
			return Verdict{
				Kind:     KindBlunder,
				DeltaWin: WinRate(in.BestCp) - WinRate(in.AfterCp),
				LostMate: true,
			}
		}
		return Verdict{}
	}

	delta := WinRate(in.BestCp) - WinRate(in.AfterCp)
	if delta > in.Level.Threshold() {
		return Verdict{Kind: KindBlunder, DeltaWin: delta}
	}
	return Verdict{}
}
