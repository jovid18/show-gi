// Package queue 는 대기열의 짝짓기 규칙이다.
//
// DB도 방도 엔진도 모른다. 입력은 대기열에 서 있는 사람들의 레이팅·불확실성·선 시각뿐이라
// 아래 상수를 흔들어 보는 데 DB도 대국도 필요 없다. intervene·rating 과 같은 성질이다.
//
// 밴드 식이 여기에만 있다. 후보를 고르는 질의에도 넣으면 같은 식이 Go 와 SQL 에 두 벌
// 있게 되고, 한쪽을 고칠 때 다른 쪽이 경고 없이 어긋난다(journal §98).
package queue

import "time"

// Waiter 는 대기열에 서 있는 한 사람이다.
type Waiter struct {
	UserID int64
	// 대기열에 설 때 읽은 값이다. 서 있는 동안 바뀌지 않는다. 대기 중에 그 사람의 판이
	// 끝날 수가 없다.
	Rating, Deviation float64
	// JoinedAt 은 대기열에 선 시각이다. 밴드가 이 값으로 넓어지고, 같은 밴드 안에서는
	// 이 값이 순서를 정한다.
	JoinedAt time.Time
}

// Base0 은 처음 섰을 때의 밴드 폭이다. 두 사람의 불확실성이 여기에 더해진다(Band).
//
// [미확정] 이 값을 비롯한 여섯이 전부 초기값이다. 재려면 사람끼리 대기열에 서 본 판이
// 쌓여야 한다(journal §98).
const Base0 = 200

// Expand 는 기다린 1초마다 밴드가 넓어지는 폭이다. [미확정]
const Expand = 20

// BaseMax 는 밴드의 상한이다. 넘겨도 넓어지지 않는다. [미확정]
const BaseMax = 800

// StaleAfter 는 이 시간만큼 다시 물어보지 않으면 대기열에서 빠지는 시간이다. [미확정]
//
// 대기열에 heartbeat 가 따로 없다. 기다리는 쪽의 재시도가 그 일을 겸하고, 탭을 닫은
// 사람을 걷어내는 장치가 이것뿐이라 화면의 재시도 주기보다 넉넉히 길어야 한다(지금 2초).
const StaleAfter = 12 * time.Second

// PickupTTL 은 짝이 잡힌 자리를 잡아 두고 기다리는 시간이다. 넘으면 그 행도 걷는다. [미확정]
//
// 걷지 않으면 그 사람이 다시 대기열에 설 때마다 이미 죽은 방으로 보내진다. 방은 프로세스
// 메모리라 배포 한 번에 사라진다(match.Hub).
const PickupTTL = 2 * time.Minute

// Candidates 는 한 번에 훑어보는 후보 수다. 오래 기다린 쪽부터 이만큼만 잠근다. [미확정]
const Candidates = 20

// MaxBand 는 어떤 밴드보다도 넓은 폭이다. maxDeviation 은 불확실성의 상한이고
// (rating.MaxDeviation), 이 패키지가 그 값을 모르므로 받는다.
//
// 잠글 행을 미리 자르는 데 쓴다. 시간 항이 없어서 어떤 밴드보다도 넓고, 그래서 붙을 수
// 있는 짝이 여기서 빠지지 않는다(journal §98).
func MaxBand(maxDeviation float64) float64 { return BaseMax + 2*maxDeviation }

// Band 는 그만큼 기다린 사람이 받아들이는 레이팅 차다. 두 사람의 불확실성이 더해진다.
// 모르는 사람에게 좁은 밴드는 뜻이 없다(journal §92).
func Band(waited time.Duration, devA, devB float64) float64 {
	// 음수를 0으로 본다. 선 시각은 DB 의 now() 이고 지금은 프로세스의 시계다. 앞선 DB
	// 하나가 밴드를 Base0 아래로 끌어내릴 수 있다.
	if waited < 0 {
		waited = 0
	}
	base := Base0 + Expand*waited.Seconds()
	if base > BaseMax {
		base = BaseMax
	}
	return base + devA + devB
}

// Pairable 은 두 사람을 붙여도 되는가다. 양쪽 밴드를 다 본다.
//
// 찾는 쪽만 보면 오래 기다린 외곽 유저가 방금 온 사람을 끌어당기고, 그 사람은 동의한
// 적 없는 짝에 앉는다(journal §92).
func Pairable(a, b Waiter, now time.Time) bool {
	gap := a.Rating - b.Rating
	if gap < 0 {
		gap = -gap
	}
	return gap <= min(
		Band(now.Sub(a.JoinedAt), a.Deviation, b.Deviation),
		Band(now.Sub(b.JoinedAt), a.Deviation, b.Deviation),
	)
}

// Pick 은 후보 중에서 짝 하나를 고른다. 없으면 두 번째 값이 false 다.
//
// 밴드 안에서 FIFO 다. 밴드가 품질을 보장하니 그 안에서는 오래 기다린 쪽이 먼저다
// (journal §92).
//
// candidates 는 오래 기다린 순이어야 한다. 질의가 그 순서로 준다.
func Pick(me Waiter, candidates []Waiter, now time.Time) (Waiter, bool) {
	for _, c := range candidates {
		if c.UserID == me.UserID {
			continue
		}
		if Pairable(me, c, now) {
			return c, true
		}
	}
	return Waiter{}, false
}
