// Package queue 는 대기열의 짝짓기 규칙이다.
//
// DB도 방도 엔진도 모른다. 입력은 줄에 서 있는 사람들의 레이팅·불확실성·선 시각뿐이고,
// 그래서 아래 상수를 흔들어 보는 데 DB도 대국도 필요 없다 — intervene·rating 과 같은 성질이다.
//
// 밴드를 SQL 에 안 적은 이유가 이 패키지가 있는 이유다. 후보를 고르는 질의에 식을 넣으면
// 같은 식이 Go 와 SQL 에 두 벌 있게 되고, 한쪽을 고칠 때 다른 쪽이 조용히 낡는다.
// 질의는 「잠그고 오래된 순으로 준다」까지만 하고 고르는 것은 여기가 한다.
//
// 근거는 journal §92.
package queue

import "time"

// Waiter 는 줄에 서 있는 한 사람이다. 이름도 사람도 안 들고 있다 — 고르는 데 안 쓴다.
type Waiter struct {
	UserID int64
	// Rating·Deviation 은 줄에 설 때 읽은 값이다. 서 있는 동안 안 바뀐다 — 대기 중에
	// 그 사람의 판이 끝날 수가 없다.
	Rating, Deviation float64
	// JoinedAt 은 줄에 선 시각이다. 밴드가 이 값으로 넓어지고, 같은 밴드 안에서는
	// 이 값이 순서를 정한다.
	JoinedAt time.Time
}

// Base0 은 처음 섰을 때의 밴드 폭이다. 두 사람의 불확실성이 여기에 더해진다(Band).
//
// [미확정] 200은 실측이 아니다. 재려면 사람끼리 둔 판이 쌓여야 한다(journal §92).
const Base0 = 200

// Expand 는 기다린 1초마다 밴드가 넓어지는 폭이다.
//
// [미확정] 20은 초기값이다. 30초면 상한에 닿는다 — 「기다리다 지쳐 나가는 것」과
// 「아무나 붙는 것」 사이를 그 근처로 잡았다.
const Expand = 20

// BaseMax 는 밴드의 상한이다. 넘겨도 넓어지지 않는다.
//
// [미확정] 800은 초기값이다. 시드가 Default ± SeedSpread 안에 있으므로(rating.SeedFromLoss)
// 이 값이면 오래 기다린 사람은 사실상 누구와도 붙는다.
const BaseMax = 800

// StaleAfter 는 이 시간만큼 다시 안 물어보면 줄에서 빠지는 시간이다.
//
// 큐에 heartbeat 가 따로 없다. 기다리는 쪽이 스스로 재시도하고 그 호출이 그 일을 겸한다
// (journal §92) — 탭을 닫은 사람을 걷어내는 장치가 이것뿐이라, 화면의 재시도 주기보다
// 넉넉히 길어야 한다(지금 2초).
const StaleAfter = 12 * time.Second

// PickupTTL 은 짝이 잡힌 자리를 들고 기다리는 시간이다. 넘으면 그 행도 걷는다.
//
// 안 걷으면 방으로 못 간 자리가 영영 남고, 그 사람은 다시 줄에 설 때마다 이미 죽은
// 방으로 보내진다 — 방은 프로세스 메모리라 배포 한 번에 사라진다(match.Hub).
const PickupTTL = 2 * time.Minute

// Candidates 는 한 번에 훑어보는 후보 수다. 오래 기다린 쪽부터 이만큼만 잠근다.
//
// 전부 잠그지 않는 이유는 줄이 길어질 때다. 밴드 안의 짝은 대개 앞머리에 있고, 뒤까지
// 잠그면 그 사이 다른 인스턴스의 짝짓기가 앞머리에서 헛돈다(FOR UPDATE SKIP LOCKED).
const Candidates = 20

// MaxBand 는 어떤 밴드보다도 넓은 폭이다. maxDeviation 은 불확실성의 상한이다
// (rating.MaxDeviation — 이 패키지는 그 값을 모르므로 받는다).
//
// 잠글 행을 고르는 데 쓴다. 질의가 밴드를 모르는 채로 앞머리 20줄을 잠그면 붙을 수
// 없는 사람까지 잠기고, 그동안 다른 짝짓기가 그 행에서 헛돈다 — 이 폭으로 미리 자르면
// 잠기는 것이 「붙을 가능성이 있는 사람」으로 줄어든다.
//
// 시간 항이 없다. 밴드는 기다린 만큼 넓어지지만 상한이 BaseMax 라, 여기에 불확실성
// 둘을 더한 것이 그 최대다 — 이보다 좁게 자르면 붙을 수 있는 짝을 잠그기 전에 버린다.
func MaxBand(maxDeviation float64) float64 { return BaseMax + 2*maxDeviation }

// Band 는 그만큼 기다린 사람이 받아들이는 레이팅 차다. 두 사람의 불확실성이 더해진다 —
// 모르는 사람에게 좁은 밴드는 뜻이 없다(journal §92).
func Band(waited time.Duration, devA, devB float64) float64 {
	// 음수를 0으로 본다. 선 시각은 DB 의 now() 이고 지금은 프로세스의 시계라(둘이 다른
	// 시계다) 앞선 DB 하나가 밴드를 Base0 아래로 끌어내릴 수 있다.
	if waited < 0 {
		waited = 0
	}
	base := Base0 + Expand*waited.Seconds()
	if base > BaseMax {
		base = BaseMax
	}
	return base + devA + devB
}

// Pairable 은 두 사람을 붙여도 되는가다.
//
// 양쪽 밴드를 다 본다. 찾는 쪽만 보면 오래 기다린 외곽 유저가 방금 온 사람을 끌어당기고,
// 그 사람은 동의한 적 없는 짝에 앉는다(journal §92).
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
// 밴드 안에서 FIFO 다. 밴드가 품질을 보장하니 그 안의 후보는 정의상 받아들일 만하고,
// 그중에서는 오래 기다린 쪽이 먼저다 — 최근접으로 고르면 가까운 짝을 가로채서 남은
// 둘이 최악으로 붙는다(journal §92).
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
