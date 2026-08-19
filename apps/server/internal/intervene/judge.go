// Package intervene 은 개입할지 말지를 정한다.
//
// **엔진을 모른다.** 입력은 이미 구해진 평가치와 詰み 거리뿐이고, 판도 SFEN도 usi 타입도
// 시그니처에 없다. 그래야 상수를 흔들어 보는 데 엔진을 안 띄운다 — 이 성질을 깨는 import
// 하나가 이 패키지가 따로 있는 이유를 없앤다.
//
// **튜너블 넷(K · Level.Threshold · JudgeMatePlies · ShallowTrapCp)은 전부 초기값이다.**
// 265시도를 다시 채점하고도 하나를 못 옮겼다 — 표본이 전부 에이전트가 둔 판이고, 같은
// 국면을 다시 재도 승률이 0.040 흔들려 그보다 곱게 자를 수가 없다(journal §39).
// 그래서 아래 상수 주석은 「무엇을 정하나」까지고, 「왜 아직 못 정하나」는 §NN 쪽에 있다.
//
// 판정의 근거는 docs/01-core.md §2.
package intervene

import "math"

// K 는 cp를 승률로 바꿀 때의 기울기다. 크면 완만해지고 작으면 가팔라진다.
//
// **600은 실측값이 아니라 초기값이다** — 맞춰 볼 승패 딱지도, 맞춰 볼 정밀도도
// 없다(journal §39 ⑥·⑦).
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
// **셋 다 초기값 그대로다.** 판마다의 차이가 임계치보다 크고, 표본이 전부 beginner·전부
// 에이전트라 레벨별 값은 아직 못 잰다(journal §39 ①).
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

	// BaselineCp 는 이 판의 「형세 0」이다. **BestCp·AfterCp 와 같은 관점**(두는 쪽)이고,
	// 平手는 0이다. 駒落ち는 그 手合의 초기 평가치가 들어온다(internal/handicap).
	//
	// **없으면 駒落ち에서 개입이 사라진다.** 승률은 양쪽 끝에서 포화하므로(01-core.md §2)
	// 二枚落ち의 +1490에서는 낙폭 0.25를 넘기는 데 1058cp가 필요하다 — 銀 헌납(약 1000cp,
	// 01-core.md §2)도 안 걸린다는 뜻이다. 기준점을 빼면 「아직 아무것도 안 흘렸다」가 다시
	// 승률 0.5에 오고, 그 자리의 발화선이 660cp가 된다 — 平手가 원래 서 있던 감도다.
	//
	// **平手는 이 칸이 0이라 지금까지와 한 비트도 다르지 않다**(journal §84).
	//
	// **Verdict 의 BestCp·AfterCp 는 이 값을 안 빼고 원본으로 남는다** — K를 바꿔 다시
	// 채점하는 자리가 그 두 칸이고(005_intervention_cp.sql), 기준점은 `games.start_sfen`
	// 에서 언제든 다시 구할 수 있다. 빼서 저장하면 원본이 어디에도 없어진다.
	BaselineCp int

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
	// Kind 는 개입하는가다. **아래 숫자들이 채워졌는지와 별개다** — 통과한 수도 낙폭을
	// 갖고 돌아온다(Judge).
	Kind Kind
	// DeltaWin 은 승률 낙폭(0~1). 종반 판정으로 걸렸을 때는 0에 가까울 수 있다 —
	// 승률이 포화하는 구간이라 그 값이 작다는 것이 바로 詰み 거리를 쓰는 이유다.
	//
	// **통과한 수에도 있다.** 실력 추정이 매 수의 이 값으로 도므로(internal/skill), 여기가
	// 개입한 수에서만 채워지면 신호가 개입에 오염된 표본만 남는다.
	DeltaWin float64
	// BestCp·AfterCp 는 낙폭을 만든 **두 원본**이다. 둘 다 두는 쪽 관점(Input 과 같다).
	//
	// **낙폭만 남기면 K 를 바꿔 다시 채점할 수 없다** — 미지수가 둘인데 식이 하나다
	// (migrations/005_intervention_cp.sql · journal §39 ⑥ · §41).
	BestCp  int
	AfterCp int
	// LostMate 는 종반 판정으로 걸렸는가. 설명 문구가 갈린다.
	LostMate bool
	// Category 는 **왜** 나쁜가다. Kind 가 KindNone 이면 비어 있다.
	Category Category
}

// JudgeMatePlies 는 **판정에 쓰는** 詰み 거리의 상한이다. 탐색은 11까지 하지만(비용이 같다)
// 판정은 5까지다 — 5手詰이 「배울 값이 있는 실수」인 경계라서다(01-core.md §2).
//
// **[미확정]** 레벨별로 가를지. 기록으로는 못 잰다(journal §39 ⑤ · §40 ⑤).
const JudgeMatePlies = 5

// Judge 는 한 수를 판정한다.
func Judge(in Input) Verdict {
	// **기준점에서 재기 시작한다.** 두 항에 같은 값을 빼므로 平手(0)에서는 지금까지와
	// 한 비트도 다르지 않고, 駒落ち에서만 판정이 포화 구간을 벗어난다(Input.BaselineCp).
	delta := WinRate(in.BestCp-in.BaselineCp) - WinRate(in.AfterCp-in.BaselineCp)

	// **통과한 수도 낙폭을 담아 돌려준다.** 임계치를 안 넘었다는 것이 손해가 없다는 뜻이
	// 아니고, 실력 추정은 걸린 수가 아니라 **매 수의 낙폭**으로 돈다(internal/skill).
	// Kind 하나만 보면 되던 자리는 그대로다 — 통과는 KindNone 이다.
	pass := Verdict{DeltaWin: delta, BestCp: in.BestCp, AfterCp: in.AfterCp}

	// 종반 — 승률이 포화해 낙폭이 판정력을 잃는 구간이다. **이기고 있는 쪽에만 필요하다** —
	// 지는 쪽은 승률이 멀쩡히 움직여 아래 낙폭 판정이 이미 잡는다(01-core.md §2).
	if in.MateBefore > 0 && in.MateBefore <= JudgeMatePlies {
		lost := in.MateAfter == 0 || in.MateAfter > in.MateBefore+2
		if lost {
			return Verdict{
				Kind:     KindBlunder,
				DeltaWin: delta,
				BestCp:   in.BestCp,
				AfterCp:  in.AfterCp,
				LostMate: true,
				Category: classify(in, true),
			}
		}
		return pass
	}

	if delta > in.Level.Threshold() {
		return Verdict{
			Kind:     KindBlunder,
			DeltaWin: delta,
			BestCp:   in.BestCp,
			AfterCp:  in.AfterCp,
			Category: classify(in, false),
		}
	}
	return pass
}
