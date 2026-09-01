package server

import (
	"sync"
	"time"
)

// 정규화 계층을 부르는 횟수의 벽. 근거는 journal §126.
//
// **하루 몫의 벽(maxImportsPerDay)이 이 자리를 안 막는다.** 그쪽은 판이 만들어지는
// 것을 세는데, 옮겨 적는 일은 판을 만들기 전에 일어난다 — 읽기만 반복하는 사람은
// 그 벽에 영영 안 닿으면서 토큰을 계속 쓴다.
//
// 표에 안 적는다. 지키는 것이 돈이고 기록이 아니라서다 — 배포에 값이 사라지면 그 사람의
// 몫이 한 번 초기화될 뿐이고, 인스턴스가 여럿이면 벽이 그만큼 느슨해진다. 그 둘을
// 고치려면 표가 필요한데, 표를 두면 이 벽이 「기록」이 되어 지우는 규약이 하나 더 생긴다.

// maxTranscribesPerHour 는 한 사람이 한 시간에 정규화를 부를 수 있는 횟수다.
//
// 하루 몫(10판)보다 넉넉하다. 읽기는 여러 번 해 보는 것이 정상이고(형식을 고쳐 가며
// 다시 붙여 넣는다) 막으려는 것은 그 정상적인 반복이 아니라 무한히 도는 쪽이다.
//
// [미확정] 실측이 아니다. 사람이 한 판을 붙여 넣는 데 몇 번 시도하는지를 회차가 답하면 옮긴다.
const maxTranscribesPerHour = 20

// transcribeBudget 은 사람마다 최근 호출 시각을 든다.
//
// 창을 미끄러뜨린다. 시각을 세는 쪽이 「정시에 초기화」보다 낫다 — 후자는 창이 바뀌는
// 순간에 두 배가 지나간다.
type transcribeBudget struct {
	mu   sync.Mutex
	hits map[int64][]time.Time
	// now 는 테스트가 갈아 끼우는 자리다. nil이면 time.Now.
	now func() time.Time
}

func newTranscribeBudget() *transcribeBudget {
	return &transcribeBudget{hits: map[int64][]time.Time{}}
}

// take 는 한 번 부를 자리를 얻는다. 벽에 닿았으면 false 다.
//
// 부르기 전에 센다. 부른 뒤에 세면 시한에 걸린 호출이 몫을 안 쓰고, 그 실패가 가장
// 비싼 호출이다(응답을 기다린 만큼 이미 토큰을 썼다).
func (b *transcribeBudget) take(userID int64) bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	cutoff := b.clock().Add(-time.Hour)
	// 창 밖으로 나간 사람은 표에서 지운다. 안 지우면 이 맵이 로그인한 사람 수만큼 자란다.
	for id, at := range b.hits {
		kept := keepAfter(at, cutoff)
		if len(kept) == 0 {
			delete(b.hits, id)
			continue
		}
		b.hits[id] = kept
	}

	if len(b.hits[userID]) >= maxTranscribesPerHour {
		return false
	}
	b.hits[userID] = append(b.hits[userID], b.clock())
	return true
}

func (b *transcribeBudget) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

// keepAfter 는 창 안에 남은 시각만 준다. 목록이 시각 순이라 첫 유효 자리에서 자른다.
func keepAfter(at []time.Time, cutoff time.Time) []time.Time {
	for i, t := range at {
		if t.After(cutoff) {
			return at[i:]
		}
	}
	return nil
}
