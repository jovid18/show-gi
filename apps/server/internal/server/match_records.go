package server

import (
	"context"
	"sync"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/match"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// matchRecords 는 방마다 만든 기록기를 들고 있다.
//
// **들고 있는 이유는 판 번호 하나다.** 대인전 한 판은 `games` 행 두 개로 남고
// (012_match_games.sql) 색마다 번호가 다른데, 그 번호를 아는 것은 기록기뿐이다 —
// 판이 끝난 뒤 「振り返り」 링크가 그 값으로 선다.
//
// 방 자체는 `match.Hub` 가 소유한다. 여기는 그 옆에 붙는 곁장부이고, 그래서 **만료도
// 따로 판다** — Hub 가 방을 걷어갈 때 이쪽에 알려주는 길을 내면 두 패키지가 서로를 알게 된다.
type matchRecords struct {
	store *store.Store
	// level 은 개입 임계치다. **대인전에서는 아무것도 안 정한다** — 개입이 없으므로
	// 기록기가 그 값을 쓸 행(interventions)이 생기지 않는다. 넘기는 것은 기록기를
	// 한 벌만 두기 위해서다(matchRecorder).
	level intervene.Level

	mu     sync.Mutex
	byRoom map[string]*roomRecord
}

type roomRecord struct {
	at  time.Time
	rec map[shogi.Color]*dbRecorder
}

// recordSweepAfter 는 곁장부의 항목을 지우기까지의 시간이다. **방의 만료보다 넉넉해야
// 한다** — 먼저 지우면 판이 끝난 자리에서 번호를 못 찾는다.
const recordSweepAfter = match.OpenTTL + match.FinishedTTL

func newMatchRecords(st *store.Store, level intervene.Level) *matchRecords {
	return &matchRecords{store: st, level: level, byRoom: map[string]*roomRecord{}}
}

// new 는 색마다 기록기를 하나씩 만든다. `match.HubConfig.NewRecorders` 가 이 함수다.
//
// **ctx 는 서버의 것이다**(Hub 가 준다). 연결에 매달면 한쪽이 탭을 닫는 순간 그 사람의
// 기록이 abandoned 로 닫히는데, 대인전은 그때도 판이 계속 돈다.
func (m *matchRecords) new(
	ctx context.Context, matchID string, black, white match.Player,
) map[shogi.Color]match.Recorder {
	if m == nil || m.store == nil {
		return nil // 기록이 없는 배포. 대국은 그대로 된다(Options.Store 와 같은 판단)
	}

	made := map[shogi.Color]*dbRecorder{}
	out := map[shogi.Color]match.Recorder{}
	for c, p := range map[shogi.Color]match.Player{shogi.Black: black, shogi.White: white} {
		// **주소를 사람마다 새로 뜬다.** 반복 변수의 주소를 그대로 넘기면 두 기록기가
		// 같은 값을 가리키고, 그러면 한 판이 한 사람의 행 두 개로 남는다.
		userID := p.UserID
		db := newDBRecorder(ctx, m.store, m.level, recordTarget{userID: &userID, matchID: matchID})
		made[c] = db
		out[c] = matchRecorder{db: db}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked(time.Now())
	m.byRoom[matchID] = &roomRecord{at: time.Now(), rec: made}
	return out
}

// doneOf 는 그 색의 기록이 다 쓰였을 때 번호를 실어 보내는 채널이다. 없으면 nil.
func (m *matchRecords) doneOf(matchID string, c shogi.Color) <-chan int64 {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.byRoom[matchID]
	if !ok {
		return nil
	}
	rec, ok := entry.rec[c]
	if !ok {
		return nil
	}
	return rec.done
}

func (m *matchRecords) sweepLocked(now time.Time) {
	for id, entry := range m.byRoom {
		if now.Sub(entry.at) > recordSweepAfter {
			delete(m.byRoom, id)
		}
	}
}
