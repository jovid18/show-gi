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
// (012_match_games.sql) 先手·後手마다 번호가 다른데, 그 번호를 아는 것은 기록기뿐이다 —
// 판이 끝난 뒤 「振り返り」 링크가 그 값으로 만들어진다.
//
// 방 자체는 `match.Hub` 가 소유한다. 여기는 그 옆에 붙는 곁장부이고, 그래서 **만료도
// 따로 판다** — Hub 가 방을 걷어갈 때 이쪽에 알려주는 길을 내면 두 패키지가 서로를 알게 된다.
type matchRecords struct {
	store *store.Store
	// level 은 개입 임계치다. **대인전에서는 아무것도 안 정한다** — 개입이 없으므로
	// 기록기가 그 값을 쓸 행(interventions)이 생기지 않는다. 넘기는 것은 기록기를
	// 한 벌만 두기 위해서다(matchRecorder).
	level intervene.Level

	// analyzer 는 판이 끝난 뒤 평가치를 채우는 쪽이다. **nil 일 수 있다** — 엔진이 없는
	// 배포에서는 대인전이 그대로 돌고 평가치만 안 붙는다(matchAnalyzer).
	analyzer *matchAnalyzer

	mu     sync.Mutex
	byRoom map[string]*roomRecord
}

type roomRecord struct {
	at  time.Time
	rec map[shogi.Color]*dbRecorder
	// id·ready 는 **이미 받아 둔** 판 번호와 그것이 정해졌다는 신호다.
	//
	// `dbRecorder.done` 은 값 하나짜리 채널이라 먼저 읽은 쪽이 가져가 버린다. 판이 끝나는
	// 순간에 새로고침하거나 같은 쪽으로 탭을 둘 열어 두는 것은 드문 일이 아닌데, 그때
	// 두 번째 연결은 5초를 기다린 끝에 링크를 못 그리고 로그에는 「기록이 안 끝났다」는
	// **거짓말**이 남는다 — 그래서 받는 쪽을 **하나로 모으고**(collect) 여기 옮겨 둔다.
	//
	// `ready` 는 **닫히는 채널**이라 몇이 기다려도 다 깨어난다. 값을 실어 보내면 다시
	// 「먼저 읽은 쪽이 가져간다」로 돌아간다.
	id    map[shogi.Color]int64
	ready map[shogi.Color]chan struct{}
}

// recordSweepAfter 는 곁장부의 **항목**을 지우기까지의 시간이다. 기록기 goroutine 은
// 이것과 무관하게 판이 끝나는 자리에서 접힌다(collect).
//
// **판이 시작한 시각부터 센다.** 그래서 이 값은 **한 판이 걸릴 수 있는 최대 시간보다
// 길어야 한다** — 짧으면 오래 두는 판이 끝나기도 전에 항목이 사라지고, 그때 「振り返り」
// 링크가 안 그려진다. 방의 만료(40분)로 잡았다가 그 함정을 봤다: 1手 60초라 100手만
// 둬도 그 값을 넘긴다.
const recordSweepAfter = 24 * time.Hour

func newMatchRecords(st *store.Store, level intervene.Level) *matchRecords {
	return &matchRecords{store: st, level: level, byRoom: map[string]*roomRecord{}}
}

// new 는 先手·後手마다 기록기를 하나씩 만든다. `match.HubConfig.NewRecorders` 가 이 함수다.
//
// **ctx 는 서버의 것이다**(Hub 가 준다). 연결에 매달면 한쪽이 탭을 닫는 순간 그 사람의
// 기록이 abandoned 로 닫히는데, 대인전은 그때도 판이 계속 돈다.
//
// **그래서 판마다 ctx 를 한 겹 더 판다.** 서버 ctx 를 그대로 주면 기록기 goroutine 이
// 판이 끝난 뒤에도 이벤트 채널에 영원히 서 있는다 — 엔진 대국은 세션 ctx 가 연결과 함께
// 끝나서 그 자리가 없는데, 여기는 없앨 사람이 없다. 끝난 판마다 goroutine 둘과 256칸짜리
// 채널 둘이 남고, 배포 전까지 계속 쌓인다.
func (m *matchRecords) new(
	parent context.Context, matchID string, black, white match.Player,
) map[shogi.Color]match.Recorder {
	if m == nil || m.store == nil {
		return nil // 기록이 없는 배포. 대국은 그대로 된다(Options.Store 와 같은 판단)
	}

	ctx, cancel := context.WithCancel(parent)
	entry := &roomRecord{
		at:    time.Now(),
		rec:   map[shogi.Color]*dbRecorder{},
		id:    map[shogi.Color]int64{},
		ready: map[shogi.Color]chan struct{}{},
	}
	out := map[shogi.Color]match.Recorder{}
	for c, p := range map[shogi.Color]match.Player{shogi.Black: black, shogi.White: white} {
		// **주소를 사람마다 새로 뜬다.** 반복 변수의 주소를 그대로 넘기면 두 기록기가
		// 같은 값을 가리키고, 그러면 한 판이 한 사람의 행 두 개로 남는다.
		userID := p.UserID
		entry.rec[c] = newDBRecorder(ctx, m.store, m.level, recordTarget{userID: &userID, matchID: matchID})
		entry.ready[c] = make(chan struct{})
		out[c] = matchRecorder{db: entry.rec[c]}
	}

	m.mu.Lock()
	m.sweepLocked(time.Now())
	m.byRoom[matchID] = entry
	m.mu.Unlock()

	go m.collect(ctx, cancel, entry)
	return out
}

// collect 는 두 기록기의 번호를 받아 두고, **둘 다 끝나면 기록기를 접는다.**
//
// 번호를 여기 한 곳에서 받는 것이 요점이다 — 연결마다 `done` 을 직접 읽으면 값이 하나뿐이라
// 먼저 읽은 쪽이 가져간다(roomRecord.id).
//
// **先手·後手마다 goroutine 을 따로 둔다.** 한 자리에서 차례로 기다리면 한쪽이 늦는 것이
// 다른 쪽의 신호를 막는다 — 그러면 멀쩡히 기록된 사람이 「振り返り」 링크를 못 받고,
// 기다리는 5초도 둘이 나눠 쓰게 된다.
func (m *matchRecords) collect(ctx context.Context, cancel context.CancelFunc, entry *roomRecord) {
	// **마지막에 접는다.** 번호를 받았다는 것은 `evFinished` 까지 다 썼다는 뜻이라
	// (dbRecorder.done) 여기서 끊어도 잃을 이벤트가 없다.
	defer cancel()

	var wg sync.WaitGroup
	for c, rec := range entry.rec {
		wg.Add(1)
		go func(c shogi.Color, rec *dbRecorder) {
			defer wg.Done()
			// **번호를 못 받아도 신호는 연다.** 기다리는 쪽이 매달려 있으면 안 된다 —
			// 그때는 `entry.id` 가 비어 있어서 gameIDOf 가 false 를 준다.
			defer close(entry.ready[c])

			select {
			case id := <-rec.done:
				// **0은 「행이 없다」다.** 그때는 안 적는다 — 없는 판으로 링크를 그릴 수 없다.
				if id == 0 {
					return
				}
				m.mu.Lock()
				entry.id[c] = id
				m.mu.Unlock()
				// **번호가 나가기 전에 표시한다.** 아래 `ready` 가 닫히는 순간 화면이
				// 되짚기를 열 수 있고, 그때 이미 「분석 중」이라야 한다.
				m.analyzer.hold(id)
			case <-ctx.Done():
				// 서버가 내려간다.
			}
		}(c, rec)
	}
	wg.Wait()

	// **번호가 둘 다 있어야 줄을 세운다.** 한쪽만 있으면 그 판은 반쪽이라, 채운 평가치가
	// 한 사람에게만 보인다.
	m.mu.Lock()
	ids := make([]int64, 0, len(entry.id))
	for _, id := range entry.id {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	if len(ids) != len(entry.ready) {
		// 반쪽이라 분석하지 않는다. **표시는 걷는다** — 안 걷으면 그 판이 영영
		// 「분석 중」으로 남는다.
		m.analyzer.forget(ids)
		return
	}
	m.analyzer.enqueue(ids)
}

// gameIDOf 는 그쪽의 판 번호를 기다렸다 준다. 못 얻으면 두 번째 값이 false 다.
//
// **몇이 물어도 다 답한다** — 신호가 닫히는 채널이고 값은 곁장부에 남아 있다(roomRecord.id).
func (m *matchRecords) gameIDOf(
	ctx context.Context, matchID string, c shogi.Color, wait time.Duration,
) (int64, bool) {
	if m == nil {
		return 0, false
	}
	m.mu.Lock()
	entry, ok := m.byRoom[matchID]
	m.mu.Unlock()
	if !ok {
		return 0, false
	}
	ready, ok := entry.ready[c]
	if !ok {
		return 0, false
	}

	select {
	case <-ready:
	case <-time.After(wait):
		return 0, false
	case <-ctx.Done():
		return 0, false
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := entry.id[c]
	return id, ok
}

func (m *matchRecords) sweepLocked(now time.Time) {
	for id, entry := range m.byRoom {
		if now.Sub(entry.at) > recordSweepAfter {
			delete(m.byRoom, id)
		}
	}
}
