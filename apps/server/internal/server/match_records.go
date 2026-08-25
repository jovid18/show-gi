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
// 들고 있는 이유는 판 번호 하나다. 대인전 한 판은 games 행 두 개로 남고
// (012_match_games.sql) 先手·後手마다 번호가 다른데, 그 번호를 아는 것은 기록기뿐이다 —
// 판이 끝난 뒤 「振り返り」 링크가 그 값으로 만들어진다.
//
// 방 자체는 match.Hub 가 소유한다. 여기는 그 옆에 붙는 곁장부이고, 그래서 만료도
// 따로 판다 — Hub 가 방을 걷어갈 때 이쪽에 알려주는 길을 내면 두 패키지가 서로를 알게 된다.
type matchRecords struct {
	store *store.Store
	// level 은 개입 임계치다. 대인전에서는 아무것도 안 정한다 — 개입이 없으므로
	// 기록기가 그 값을 쓸 행(interventions)이 생기지 않는다. 넘기는 것은 기록기를
	// 한 벌만 두기 위해서다(matchRecorder).
	level intervene.Level

	// analyzer 는 판이 끝난 뒤 평가치와 실력 추정치를 채우는 쪽이다. nil 일 수 있다 —
	// 엔진이 없는 배포에서는 대인전이 그대로 돌고 그 둘만 안 붙는다(matchAnalyzer).
	analyzer *matchAnalyzer

	mu     sync.Mutex
	byRoom map[string]*roomRecord
}

type roomRecord struct {
	at time.Time
	// matchID 는 미리 재 둔 것을 찾는 열쇠다(matchAnalyzer.ahead).
	matchID string
	rec     map[shogi.Color]*dbRecorder
	// id·ready 는 이미 받아 둔 판 번호와 그것이 정해졌다는 신호다.
	//
	// dbRecorder.done 은 값 하나짜리 채널이라 먼저 읽은 쪽이 가져가 버린다. 판이 끝나는
	// 순간에 새로고침하거나 같은 쪽으로 탭을 둘 열어 두는 것은 드문 일이 아닌데, 그때
	// 두 번째 연결은 5초를 기다린 끝에 링크를 못 그리고 로그에는 「기록이 안 끝났다」는
	// 거짓말이 남는다 — 그래서 받는 쪽을 하나로 모으고(collect) 여기 옮겨 둔다.
	//
	// ready 는 닫히는 채널이라 몇이 기다려도 다 깨어난다. 값을 실어 보내면 다시
	// 「먼저 읽은 쪽이 가져간다」로 돌아간다.
	id    map[shogi.Color]int64
	ready map[shogi.Color]chan struct{}

	// start·moves 는 미리 재는 데 넘길 수순이다. 두 자리가 같은 수를 다 적으므로
	// 手数로 거른다 — 두 번 세우면 같은 국면을 두 번 잰다.
	//
	// 기록에서 되읽지 않는다. 착수 경로에 질의를 하나 더 두는 것이고, 그 행은 기록기가
	// 비동기로 쓰므로 아직 없을 수도 있다.
	start string
	moves []string

	// plies 는 그 판에 둔 手数다. 두 자리가 같은 수를 다 적으므로 큰 쪽을 든다.
	//
	// 기록에서 되읽지 않고 여기서 센다 — 줄에 세울 때(analysisJob) 이 값이 필요하고,
	// 그 자리에서 질의를 하나 더 하면 판이 끝나는 경로가 그만큼 늘어난다.
	plies int

	// player·result 는 자리마다의 대국자와 결과다. 레이팅을 옮기는 데 쓴다(match_rating.go).
	//
	// 결과를 기록기 채널이 아니라 여기로 한 벌 더 받는다. 레이팅은 두 사람을 같이
	// 옮기므로 한쪽 관점의 행 하나로는 짝을 못 맞추고, 행에서 되읽으면 그 판의 수까지
	// 같이 끌고 온다(store.GameRecord).
	player map[shogi.Color]match.Player
	result map[shogi.Color]match.Result
}

// recordSweepAfter 는 곁장부의 항목을 지우기까지의 시간이다. 기록기 goroutine 은
// 이것과 무관하게 판이 끝나는 자리에서 접힌다(collect).
//
// 판이 시작한 시각부터 센다. 그래서 이 값은 한 판이 걸릴 수 있는 최대 시간보다
// 길어야 한다 — 짧으면 오래 두는 판이 끝나기도 전에 항목이 사라지고, 그때 「振り返り」
// 링크가 안 그려진다. 방의 만료(40분)로 잡았다가 그 함정을 봤다: 1手 60초라 100手만
// 둬도 그 값을 넘긴다.
const recordSweepAfter = 24 * time.Hour

func newMatchRecords(st *store.Store, level intervene.Level) *matchRecords {
	return &matchRecords{store: st, level: level, byRoom: map[string]*roomRecord{}}
}

// new 는 先手·後手마다 기록기를 하나씩 만든다. match.HubConfig.NewRecorders 가 이 함수다.
//
// ctx 는 서버의 것이다(Hub 가 준다). 연결에 매달면 한쪽이 탭을 닫는 순간 그 사람의
// 기록이 abandoned 로 닫히는데, 대인전은 그때도 판이 계속 돈다.
//
// 그래서 판마다 ctx 를 한 겹 더 판다. 서버 ctx 를 그대로 주면 기록기 goroutine 이
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
		at:      time.Now(),
		matchID: matchID,
		rec:     map[shogi.Color]*dbRecorder{},
		id:      map[shogi.Color]int64{},
		ready:   map[shogi.Color]chan struct{}{},
		player:  map[shogi.Color]match.Player{},
		result:  map[shogi.Color]match.Result{},
	}
	out := map[shogi.Color]match.Recorder{}
	for c, p := range map[shogi.Color]match.Player{shogi.Black: black, shogi.White: white} {
		// 주소를 사람마다 새로 뜬다. 반복 변수의 주소를 그대로 넘기면 두 기록기가
		// 같은 값을 가리키고, 그러면 한 판이 한 사람의 행 두 개로 남는다.
		userID := p.UserID
		// 계측을 안 넘긴다. 대인전은 FinishedWith 로 결과를 적으므로 Finished 를 지나지
		// 않고, game_finished_total 은 그 자리에서만 오른다.
		entry.rec[c] = newDBRecorder(ctx, m.store, nil, m.level, recordTarget{userID: &userID, matchID: matchID})
		entry.ready[c] = make(chan struct{})
		entry.player[c] = p
		out[c] = matchRecorder{
			db:      entry.rec[c],
			note:    m.noting(entry, c),
			counted: m.counting(entry),
			opened:  m.opening(entry),
		}
	}

	m.mu.Lock()
	m.sweepLocked(time.Now())
	m.byRoom[matchID] = entry
	m.mu.Unlock()

	go m.collect(ctx, cancel, entry)
	return out
}

// collect 는 두 기록기의 번호를 받아 두고, 둘 다 끝나면 기록기를 접는다.
//
// 번호는 여기 한 곳에서 받는다 — 연결마다 done 을 직접 읽으면 값이 하나뿐이라
// 먼저 읽은 쪽이 가져간다(roomRecord.id).
//
// 先手·後手마다 goroutine 을 따로 둔다. 한 자리에서 차례로 기다리면 한쪽이 늦는 것이
// 다른 쪽의 신호를 막는다 — 그러면 멀쩡히 기록된 사람이 「振り返り」 링크를 못 받고,
// 기다리는 5초도 둘이 나눠 쓰게 된다.
func (m *matchRecords) collect(ctx context.Context, cancel context.CancelFunc, entry *roomRecord) {
	// 마지막에 접는다. 번호를 받았다는 것은 evFinished 까지 다 썼다는 뜻이라
	// (dbRecorder.done) 여기서 끊어도 잃을 이벤트가 없다.
	defer cancel()

	var wg sync.WaitGroup
	for c, rec := range entry.rec {
		wg.Add(1)
		go func(c shogi.Color, rec *dbRecorder) {
			defer wg.Done()
			// 번호를 못 받아도 신호는 연다. 기다리는 쪽이 매달려 있으면 안 된다 —
			// 그때는 entry.id 가 비어 있어서 gameIDOf 가 false 를 준다.
			defer close(entry.ready[c])

			select {
			case id := <-rec.done:
				// 0은 「행이 없다」다. 그때는 안 적는다 — 없는 판으로 링크를 그릴 수 없다.
				if id == 0 {
					return
				}
				m.mu.Lock()
				entry.id[c] = id
				m.mu.Unlock()
				// 번호가 나가기 전에 표시한다. 아래 ready 가 닫히는 순간 화면이
				// 되짚기를 열 수 있고, 그때 이미 「분석 중」이라야 한다.
				//
				// 판 하나에 한 행이라 두 자리가 같은 것을 세운다 — 멱등이다.
				m.analyzer.hold(ctx, entry.matchID)
			case <-ctx.Done():
				// 서버가 내려간다.
			}
		}(c, rec)
	}
	wg.Wait()

	// 번호가 둘 다 있어야 줄을 세운다. 한쪽만 있으면 그 판은 반쪽이라, 채운 평가치가
	// 한 사람에게만 보인다.
	//
	// 사람과 색을 같이 실어 보낸다. 분석기가 실력 추정도 쌓는데(matchAnalyzer) 그러려면
	// 「이 手를 누가 뒀나」를 알아야 하고, 그 짝을 아는 것은 여기 곁장부뿐이다.
	//
	// 색 순서를 고정한다. 맵을 그냥 훑으면 첫 자리가 실행마다 달라지는데, 기보를 그
	// 자리의 행 하나에서만 읽으므로(analyze) 「이 판을 잴 수 있나」의 답이 같이 흔들린다.
	m.mu.Lock()
	seats := make([]analysisSeat, 0, len(entry.id))
	for _, c := range []shogi.Color{shogi.Black, shogi.White} {
		id, ok := entry.id[c]
		if !ok {
			continue
		}
		seats = append(seats, analysisSeat{gameID: id, userID: entry.player[c].UserID, color: c})
	}
	m.mu.Unlock()
	if len(seats) != len(entry.ready) {
		// 반쪽이라 분석하지 않는다. 표시는 걷는다 — 안 걷으면 그 판이 영영
		// 「분석 중」으로 남는다.
		m.analyzer.dropJob(ctx, entry.matchID)
		m.analyzer.discard(ctx, entry.matchID)
		return
	}
	m.mu.Lock()
	plies := entry.plies
	m.mu.Unlock()
	m.analyzer.enqueue(ctx, entry.matchID, plies)

	// 레이팅은 두 행이 다 있을 때만 옮긴다. 반쪽인 판은 한 사람에게만 남은 판이라,
	// 그것으로 두 사람의 값을 움직이면 기록과 레이팅이 갈린다.
	m.updateRatings(ctx, entry)
}

// noting 은 그 자리의 결과를 곁장부에 적는 콜백이다. 기록기가 판이 끝날 때 한 번 부른다.
//
// 즉시 돌아온다 — 테이블 goroutine 이 부르는 자리다(match.Recorder). 그리고 기록기의
// done 보다 반드시 먼저 적힌다: FinishedWith 가 이벤트를 큐에 넣은 뒤 여기가 불리고,
// done 은 그 이벤트를 DB 에 쓴 뒤에야 열린다.
func (m *matchRecords) noting(entry *roomRecord, c shogi.Color) func(match.Result) {
	return func(r match.Result) {
		m.mu.Lock()
		entry.result[c] = r
		m.mu.Unlock()
	}
}

// counting 은 手数를 곁장부에 적고 그 手를 미리 재는 줄에 세운다. 두 자리가 같은 수를
// 적으므로 앞선 자리만 세운다.
//
// 즉시 돌아온다 — 테이블 goroutine 이 부르는 자리다(match.Recorder). 세우는 것이
// 논블로킹이라(matchAnalyzer.prefetch) 줄이 차 있어도 착수가 안 늦는다.
func (m *matchRecords) counting(entry *roomRecord) func(int, string) {
	return func(ply int, usi string) {
		m.mu.Lock()
		entry.plies = max(entry.plies, ply)
		if ply != len(entry.moves)+1 {
			// 뒤따라온 자리다. 같은 手를 두 번 세우면 같은 국면을 두 번 잰다.
			m.mu.Unlock()
			return
		}
		entry.moves = append(entry.moves, usi)
		matchID, start := entry.matchID, entry.start
		// 잠금 안에서 복사한다. 슬라이스는 다음 手에 계속 자라므로 그대로 넘기면
		// 재는 시점에 무엇이 들어 있는지가 정해지지 않는다.
		moves := append([]string(nil), entry.moves...)
		m.mu.Unlock()
		m.analyzer.prefetch(matchID, start, moves, ply)
	}
}

// opening 은 시작 국면을 곁장부에 적는 창구다. 두 자리가 같은 값을 주므로 먼저 온 것을 든다.
func (m *matchRecords) opening(entry *roomRecord) func(string) {
	return func(startSFEN string) {
		m.mu.Lock()
		if entry.start == "" {
			entry.start = startSFEN
		}
		m.mu.Unlock()
	}
}

// gameIDOf 는 그쪽의 판 번호를 기다렸다 준다. 못 얻으면 두 번째 값이 false 다.
//
// 몇이 물어도 다 답한다 — 신호가 닫히는 채널이고 값은 곁장부에 남아 있다(roomRecord.id).
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
