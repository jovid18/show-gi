// Package skill 은 한 판 안에서 플레이어의 실력을 추정한다.
//
// 엔진도 DB도 판도 모른다. 입력은 이미 결정적으로 구해진 숫자뿐이다 — 승률 낙폭과
// 「개입이 걸렸나」. intervene 이 같은 성질을 갖는 것과 같은 이유이고(judge.go), 그래서
// 아래 상수를 흔들어 보는 데 엔진도 대국도 필요 없다.
//
// 추정을 대국 결과로 하지 않는다. 한 판이 끝난 뒤 매기면 이번 판에서 흥미를 잃은
// 사람에게는 늦다(journal §21 ①). 그래서 신호는 착수 한 수마다 온다 — 다만 반영은
// 언제나 한 수 뒤다(game.Opponent). 표본 셋을 채운 뒤이므로 첫 조절은 네 번째 판정의
// 응수부터다.
package skill

// Move 는 판정이 끝난 사람의 수 한 건에서 추정에 쓰는 전부다.
//
// 수도 국면도 없다. 무엇을 뒀는지가 아니라 얼마나 손해였는지만 본다.
type Move struct {
	// Blunder 는 개입이 걸렸는가(= 물러졌는가).
	//
	// DeltaWin 과 따로 받는 이유는 종반 판정으로 걸린 수가 있기 때문이다. 詰み을
	// 놓친 수는 승률이 포화한 구간에서 나와 낙폭이 0에 가깝고(intervene.JudgeMatePlies),
	// 낙폭만 보면 그 수가 잘 둔 수로 들어온다.
	Blunder bool
	// DeltaWin 은 승률 낙폭(0~1). 통과한 수도 값을 갖는다 — 임계치를 넘지 않았을 뿐이다.
	DeltaWin float64
	// Threshold 는 그 판정에 쓰인 임계치다(intervene.Level.Threshold). 낙폭을 이걸로
	// 나눠 0~1로 만든다.
	//
	// 여기서 레벨을 보고 값을 고르지 않는 것은 이 패키지가 intervene 을 모르기 위해서다 —
	// 부르는 쪽이 이미 그 값으로 판정했으므로 같은 값을 그대로 넘긴다.
	Threshold float64
}

// Estimate 는 지금까지 본 것으로 만든 추정치다.
type Estimate struct {
	// Loss 는 최근 착수의 정규화된 낙폭(0~1)이다. 0이면 매 수 최선, 1이면 매 수 블런더.
	//
	// 실력이 아니라 손해로 적는다. 「실력 0.7」은 무엇에 대한 0.7인지가 없지만
	// 낙폭은 임계치에 대한 비율이라 뜻이 하나다. 사람에게 보이는 段級은 여기서 파생한다
	// (rank.go) — 저장되는 것은 언제나 이 값이다.
	Loss float64
	// Samples 는 본 수의 개수다.
	Samples int
}

// PriorLoss 는 아무것도 보기 전의 값이다. 기본 밴드가 여기서 나온다 —
// 모르는 채로 상대를 세게도 약하게도 만들지 않는다.
const PriorLoss = 0.5

// Unknown 은 아직 아무 수도 안 본 추정치다. 추정기가 꺼져 있을 때도 이 값이다 —
// 부르는 쪽이 「없음」과 「모름」을 갈라 볼 이유가 없고, 둘 다 기준선 밴드로 간다.
var Unknown = Estimate{Loss: PriorLoss}

// RiseRate·FallRate 는 낙폭이 오를 때와 내릴 때의 반영 비율이다. 둘은 비대칭이다.
//
// 나빠지는 쪽이 빠르다 — 도움은 헤매는 그 자리에서 와야 한다. 좋아지는 쪽이 느린 것은
// 한 수 잘 둔 것이 실력의 증거가 못 되기 때문이다. 조용한 국면에서는 아무 수나 둬도
// 낙폭이 0이고(journal §21 ①의 「후보가 20~40cp에 몰리는 국면」), 거기서 빠르게
// 올리면 방금 통과한 사람이 곧바로 더 센 상대를 만난다.
//
// [미확정] 두 값 다 초기값이다. 근거는 journal §47.
const (
	RiseRate = 0.5
	FallRate = 0.1
)

// MinSamples 는 밴드를 옮기기 전에 볼 수의 개수다.
//
// 첫 수 하나로 상대가 바뀌면 사람이 알아차리기 전에 강함이 흔들린다. [미확정]
const MinSamples = 3

// Track 은 롤링 추정기다. 동시 호출 안전하지 않다 — Worker 의 goroutine 하나가 소유한다.
type Track struct {
	loss    float64
	samples int
}

// NewTrack 은 아무것도 모르는 상태에서 시작한다.
func NewTrack() *Track {
	return &Track{loss: Unknown.Loss}
}

// NewTrackFrom 은 지난 판의 값에서 이어 시작한다. 표본이 이미 차 있으면 첫 판정부터
// 밴드가 움직인다(Estimate.Ready).
//
// 값을 잘라서 받는다 — 저장해 둔 값이 밖에서 온 것이라(DB) 1을 넘는 낙폭 하나가 밴드를
// 임의로 밀 수 있다. adaptive.skillShift 가 같은 이유로 한 번 더 자른다.
func NewTrackFrom(e Estimate) *Track {
	if e.Samples <= 0 {
		return NewTrack()
	}
	return &Track{loss: clamp01(e.Loss), samples: e.Samples}
}

// Observe 는 한 수를 반영하고 새 추정치를 돌려준다.
func (t *Track) Observe(m Move) Estimate {
	l := moveLoss(m)
	rate := FallRate
	if l > t.loss {
		rate = RiseRate
	}
	t.loss += rate * (l - t.loss)
	t.samples++
	return t.Estimate()
}

// Estimate 는 지금 값이다.
func (t *Track) Estimate() Estimate {
	return Estimate{Loss: t.loss, Samples: t.samples}
}

// Ready 는 밴드를 옮겨도 되는가다. 아니면 부르는 쪽이 기본 밴드를 쓴다.
func (e Estimate) Ready() bool { return e.Samples >= MinSamples }

// moveLoss 는 한 수의 손해를 0~1로 만든다.
//
// 임계치로 나누므로 낙폭으로 걸린 블런더는 저절로 1이 된다. Blunder 를 따로 보는 것은
// 종반 판정으로 걸린 수 때문이다(Move.Blunder).
func moveLoss(m Move) float64 {
	if m.Blunder {
		return 1
	}
	if m.Threshold <= 0 {
		return 0 // 임계치를 모르면 정규화할 수 없다. 통과한 수이므로 손해 없음으로 둔다
	}
	return clamp01(m.DeltaWin / m.Threshold)
}

// clamp01 은 0~1로 자른다. 낙폭이 음수로 나올 수 있다 — 판정의 두 탐색이 뿌리가 한 수
// 달라서 최선수보다 좋아 보이는 값이 나오는 국면이 있다(journal §41).
func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
