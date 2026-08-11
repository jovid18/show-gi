// Package intervene 은 개입할지 말지를 정한다.
//
// **엔진을 모른다.** 입력은 이미 구해진 평가치와 詰み 거리뿐이고, 여기서 하는 일은
// 그 숫자를 제품의 판단으로 바꾸는 것이다. 그래야 상수(K·임계치)를 흔들어 보는 일이
// 엔진을 띄우지 않고 된다 — D3에서 그 상수들을 실측으로 잡아야 한다.
//
// 판정의 근거는 docs/01-core.md §2.
package intervene

import "math"

// K 는 cp를 승률로 바꿀 때의 기울기다.
//
// 크면 완만해지고(같은 cp 차이가 승률을 덜 움직인다) 작으면 가팔라진다.
//
// **600에 둔다. 기록으로는 못 정한다는 것이 실측 결과다**(06-status.md §39).
// K가 답해야 하는 질문은 「이 cp에서 실제로 몇 번 이기는가」인데, 그 답을 맞춰 보려면
// 승패 딱지가 붙은 대국이 있어야 한다. 이 제품에는 없다 — 상대가 밴드로 일부러
// 약해진 쪽이라 결과가 승률의 증거가 아니고, 기록된 판은 대부분 도중에 끊긴다.
//
// 그리고 **맞춰 볼 정밀도가 모자란다.** 앞선 탐색이 남긴 치환표 때문에 같은 국면을
// 같은 깊이로 다시 재도 평가치가 흔들리고(§34 ②), 그 폭이 승률로 **0.040**이다.
// 임계치가 0.25이므로 결정 여백의 6분의 1쯤이 잡음이다 — **입력이 그만큼 흔들리는데
// K를 소수점으로 맞추는 것은 맞추는 시늉이다.**
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
//
// **셋 다 그대로 둔다.** 기록 5판 265시도를 다시 채점한 값이 아래다(06-status.md §39).
//
//	0.25(beginner) → 13.2%   0.18(novice) → 18.9%   0.12(intermediate) → 27.5%
//
// 값을 옮길 근거가 안 나왔다. **판마다의 차이가 임계치보다 크기 때문**이다 — 같은
// 0.25에서 한 판은 0%(54수)이고 한 판은 34.5%(55수)다. 잔소리로 느껴지느냐를 정하는
// 것은 상수가 아니라 그 판이고, 그래서 여기를 흔드는 것으로는 안 잡힌다.
//
// > 위 셋을 나란히 읽을 때 조심할 것: 재채점한 265시도는 **전부 beginner로 둔 판**이다.
// > 0.12 칸은 「5급이 27.5% 개입당한다」가 아니라 「초심자의 수를 5급 잣대로 재면
// > 27.5%」다. 레벨별 값은 그 레벨의 사람이 둔 판이 쌓여야 잰다.
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

// Kind 는 개입의 종류다. DB의 interventions.kind 와 같은 값을 쓴다.
type Kind string

const (
	KindNone    Kind = ""
	KindBlunder Kind = "blunder"
)

// Input 은 한 수를 판정하는 데 필요한 전부다.
//
// 수 번호가 없다. **"언제 두었나"는 판정의 입력이 아니다** — 오프닝을 봐주는 것은
// 수 번호가 아니라 무엇을 뒀는가로 갈려야 하고(01-core.md §2), 그건 부르는 쪽이 정한다.
type Input struct {
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

	// Features 는 카테고리를 정하는 국면 사실들이다. 비어 있으면(Known=false)
	// 판정은 그대로 돌고 카테고리만 other 가 된다 — 개입이 카테고리에 매이지 않는다.
	Features Features

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
	// Category 는 **왜** 나쁜가다. Kind 가 KindNone 이면 비어 있다.
	Category Category
}

// JudgeMatePlies 는 **판정에 쓰는** 詰み 거리의 상한이다.
//
// 탐색은 11까지 하지만(비용이 같다) 판정은 5까지만 한다. 5手詰까지는 실제 대국에
// 자주 나오고 詰将棋의 표준 단위라 **배울 값이 있는 실수**인 반면, 11手詰을 놓친 것은
// 8급에게 실수가 아니다 — 애초에 보이지 않는 수순이다.
//
// **[미확정]** 레벨별로 다르게 할지. **기록으로는 아직 못 잰다**(06-status.md §39) —
// 재채점한 265시도에서 `missed_mate` 가 0건이라 이 상한이 한 번도 안 걸렸고, 걸린 적이
// 없으면 올릴지 내릴지의 근거도 없다. 그리고 詰み 거리는 기보에 안 남으므로(남는 것은
// cp뿐이다) 여기만은 엔진을 다시 돌려야 재진다.
const JudgeMatePlies = 5

// Judge 는 한 수를 판정한다.
func Judge(in Input) Verdict {
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
				Category: classify(in, true),
			}
		}
		return Verdict{}
	}

	delta := WinRate(in.BestCp) - WinRate(in.AfterCp)
	if delta > in.Level.Threshold() {
		return Verdict{Kind: KindBlunder, DeltaWin: delta, Category: classify(in, false)}
	}
	return Verdict{}
}
