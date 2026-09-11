package server

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"sync"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/kifunorm"
)

// 정규화 계층을 부르는 횟수의 상한. 근거는 journal §126.
//
// 하루 몫(maxImportsPerDay)이 이 자리를 막지 않는다. 그쪽은 판이 만들어지는 것을 세는데,
// 옮겨 적는 일은 판을 만들기 전에 일어난다. 읽기만 반복하는 사람은 그 상한에 영영 닿지
// 않으면서 토큰을 계속 쓴다.
//
// 표에 적지 않는다. 배포에 값이 사라지면 그 사람의 몫이 한 번 초기화되고 인스턴스가
// 여럿이면 상한이 느슨해지지만, 표를 두면 이 상한이 「기록」이 되어 지우는 규약이 하나
// 더 생긴다.

// maxTranscribesPerHour 는 한 사람이 한 시간에 정규화를 부를 수 있는 횟수다.
//
// 하루 몫(maxImportsPerDay)보다 넉넉하다. 읽기는 여러 번 해 보는 것이 정상이고, 막으려는
// 것은 무한히 도는 쪽뿐이다.
//
// [미확정] 실측으로 잡지 않았다. 사람이 한 판을 붙여 넣는 데 몇 번 시도하는지를 재 보면 옮긴다.
const maxTranscribesPerHour = 20

// hourlyBudget 은 사람마다 최근 호출 시각을 담는다.
//
// 창을 미끄러뜨린다. 「정시에 초기화」로 두면 창이 바뀌는 순간에 두 배가 지나간다.
//
// 상한이 둘이다. 기보를 옮겨 적는 것과 그림에서 판을 읽는 것이 각자 창을 갖는다
// (maxTranscribesPerHour · maxBoardReadsPerHour). 하나로 세면 값이 다른 두 호출에
// 같은 자를 대게 된다.
type hourlyBudget struct {
	mu   sync.Mutex
	hits map[int64][]time.Time
	// limit 은 한 시간에 허용하는 호출 수다.
	limit int
	// now 는 테스트가 갈아 끼우는 자리다. nil이면 time.Now.
	now func() time.Time
}

func newHourlyBudget(limit int) *hourlyBudget {
	return &hourlyBudget{hits: map[int64][]time.Time{}, limit: limit}
}

// take 는 한 번 부를 자리를 얻는다. 상한에 닿았으면 false 다.
//
// 부르기 전에 센다. 부른 뒤에 세면 시한에 걸린 호출이 몫을 쓰지 않고, 그 실패가 가장
// 비싼 호출이다(응답을 기다린 만큼 이미 토큰을 썼다).
func (b *hourlyBudget) take(userID int64) bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	cutoff := b.clock().Add(-time.Hour)
	// 창 밖으로 나간 사람은 표에서 지운다. 지우지 않으면 이 맵이 로그인한 사람 수만큼 자란다.
	for id, at := range b.hits {
		kept := keepAfter(at, cutoff)
		if len(kept) == 0 {
			delete(b.hits, id)
			continue
		}
		b.hits[id] = kept
	}

	if len(b.hits[userID]) >= b.limit {
		return false
	}
	b.hits[userID] = append(b.hits[userID], b.clock())
	return true
}

func (b *hourlyBudget) clock() time.Time {
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

// 옮겨 적은 결과를 짧게 담아 두는 자리. 근거는 journal §126.
//
// 사람이 미리보기에서 확인한 판이 가져오는 판과 같아야 한다. 원문을 두 번 받는 설계는
// 「파싱이 결정적이다」에 기대는데(kifu_import.go) 그 전제가 정규화 계층에는 없다. 같은
// 텍스트를 두 번 물어 다른 표기가 오면 확인한 것과 들어온 것이 갈린다.
//
// 결정적 파서로 읽히는 기보는 여기 오지 않는다. 그쪽은 두 번 읽어도 같은 답이다.

// transcribeTTL 은 옮겨 적은 결과를 담아 두는 시간이다.
//
// 미리보기를 보고 자리를 고르는 데 걸리는 시간이면 된다. 지나면 다시 옮겨 적고, 그때
// 몫도 다시 센다.
const transcribeTTL = 10 * time.Minute

// transcribeCacheMax 는 담아 두는 항목 수의 상한이다. 넘으면 오래된 것부터 버린다.
const transcribeCacheMax = 64

type transcribeCache struct {
	mu   sync.Mutex
	at   map[string]transcribeEntry
	now  func() time.Time
	seen int
}

type transcribeEntry struct {
	got kifunorm.Result
	at  time.Time
}

func newTranscribeCache() *transcribeCache {
	return &transcribeCache{at: map[string]transcribeEntry{}}
}

// transcribeKey 는 사람과 원문을 함께 묶는다.
//
// 사람을 넣는 것은 남의 항목을 보지 못하게 하려는 것이다. 해시가 같으면 원문도 같으므로
// 새어 나갈 사실이 없지만, 넣지 않으면 「이 텍스트를 누가 올렸나」를 물어볼 수 있는
// 자리가 된다.
func transcribeKey(userID int64, text string) string {
	sum := sha256.Sum256([]byte(text))
	return strconv.FormatInt(userID, 10) + ":" + hex.EncodeToString(sum[:])
}

// get 은 아직 살아 있는 항목을 준다.
func (c *transcribeCache) get(userID int64, text string) (kifunorm.Result, bool) {
	if c == nil {
		return kifunorm.Result{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.at[transcribeKey(userID, text)]
	if !ok || c.clock().Sub(e.at) > transcribeTTL {
		return kifunorm.Result{}, false
	}
	return e.got, true
}

// put 은 옮겨 적은 결과를 남긴다.
func (c *transcribeCache) put(userID int64, text string, got kifunorm.Result) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.clock()
	// 오래된 것을 먼저 걷는다. 그것만으로 상한 아래로 내려가지 않으면 오래된 것부터 버린다.
	for k, e := range c.at {
		if now.Sub(e.at) > transcribeTTL {
			delete(c.at, k)
		}
	}
	for len(c.at) >= transcribeCacheMax {
		oldest, at := "", time.Time{}
		for k, e := range c.at {
			if at.IsZero() || e.at.Before(at) {
				oldest, at = k, e.at
			}
		}
		delete(c.at, oldest)
	}
	c.at[transcribeKey(userID, text)] = transcribeEntry{got: got, at: now}
}

func (c *transcribeCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}
