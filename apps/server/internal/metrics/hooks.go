package metrics

import "time"

// 엔진 풀의 pool 라벨 값. 두 풀이 다른 바이너리이고 잡히는 이유도 달라서 갈라 둔다.
const (
	PoolSearch = "search"
	PoolMate   = "mate"
)

// 탐색의 result 라벨 값. 캐시가 답한 것과 엔진을 부른 것을 가른다.
const (
	resultCached   = "cached"
	resultComputed = "computed"
)

// WebSocket 세션의 kind 라벨 값. 대국 한 판이 세션 하나다.
const (
	KindGame  = "game"
	KindMatch = "match"
)

// ObserveHTTP 는 요청 하나를 남긴다. route 는 라우팅 패턴이어야 한다 —
// 실제 경로를 넣으면 라벨 값이 요청마다 달라져 계열이 무한히 늘어난다.
func (r *Registry) ObserveHTTP(route, status string, d time.Duration) {
	if r == nil {
		return
	}
	r.HTTPRequests.Inc(route, status)
	// 101 은 업그레이드다. 그 요청은 대국이 끝날 때까지 안 돌아오므로 지연 분포에
	// 넣으면 분 단위 값이 섞여 다른 경로의 백분위를 못 읽는다 — 대국의 길이는
	// ws_sessions_active 와 요청 로그가 든다.
	if status == "101" {
		return
	}
	r.HTTPDuration.Observe(d.Seconds(), route)
}

// ObservePanic 은 핸들러가 panic 한 것을 남긴다.
func (r *Registry) ObservePanic(route string) {
	if r == nil {
		return
	}
	r.HTTPPanics.Inc(route)
}

// Session 은 세션 하나가 열렸음을 남기고 닫을 때 부를 함수를 준다.
//
// 게이지를 두 자리에서 올리고 내리면 이른 return 하나가 새는 게이지를 만들고, 그건
// 며칠 뒤 「세션이 안 닫힌다」로 보인다. 여는 쪽에서 defer 로 닫게 하는 것이 그 방어다.
func (r *Registry) Session(kind string) func() {
	if r == nil {
		return func() {}
	}
	r.WSSessionsOpened.Inc(kind)
	r.WSSessions.Add(1, kind)
	return func() { r.WSSessions.Add(-1, kind) }
}

// ObservePairing 은 대기열이 지은 짝 하나를 남긴다. 대기 시간은 두 사람 몫을 다 넣는다 —
// 밴드가 양쪽을 보므로(queue.Pairable) 한쪽만 재면 절반만 남는다.
//
// gap 은 두 사람의 레이팅 차다. 음수를 넣지 않는 것은 부르는 쪽의 규약이다.
func (r *Registry) ObservePairing(a, b time.Duration, gap float64) {
	if r == nil {
		return
	}
	r.MatchPairings.Inc()
	r.MatchPairingWait.Observe(a.Seconds())
	r.MatchPairingWait.Observe(b.Seconds())
	r.MatchPairingGap.Observe(gap)
}

// Pool 은 엔진 풀 하나의 계측 창구다. usi 쪽 인터페이스를 이 타입이 만족한다.
type Pool struct {
	reg  *Registry
	name string
}

// Pool 은 그 이름의 풀에 계측 창구를 하나 준다. r 이 nil 이어도 창구는 준다 —
// 「레지스트리가 없으면 계측만 꺼진다」가 배선 두 자리에서도 성립해야 한다.
// nil 포인터를 인터페이스에 넣지 않으려고 창구 자체는 늘 non-nil 이다.
func (r *Registry) Pool(name string) *Pool { return &Pool{reg: r, name: name} }

// SetSize 는 풀에 있는 엔진 수를 놓는다. 점유 수만으로는 포화를 못 읽는다 —
// 「3 중 3」과 「8 중 3」이 같은 숫자로 보인다.
func (p *Pool) SetSize(n int) {
	if p.reg == nil {
		return
	}
	p.reg.PoolSize.Set(float64(n), p.name)
}

// ObserveWait 는 엔진을 빌리기까지 기다린 시간을 남긴다. 안 기다렸으면 0이다 —
// 0도 같이 넣어야 백분위가 「대개 안 기다린다」를 말할 수 있다.
func (p *Pool) ObserveWait(d time.Duration, borrower string) {
	if p.reg == nil {
		return
	}
	p.reg.PoolWait.Observe(d.Seconds(), p.name, borrower)
}

// ObserveInUse 는 빌려 나간 엔진 수를 delta 만큼 옮긴다.
func (p *Pool) ObserveInUse(delta int) {
	if p.reg == nil {
		return
	}
	p.reg.PoolInUse.Add(float64(delta), p.name)
}

// Search 는 탐색의 계측 창구다. archive 쪽 인터페이스를 이 타입이 만족한다.
type Search struct{ reg *Registry }

// Search 는 탐색 계측 창구를 준다. Pool 과 같은 이유로 r 이 nil 이어도 non-nil 이다.
func (r *Registry) Search() *Search { return &Search{reg: r} }

// ObserveSearch 는 탐색 하나를 남긴다. cached 면 엔진을 안 부른 것이다.
func (s *Search) ObserveSearch(d time.Duration, cached bool) {
	if s.reg == nil {
		return
	}
	result := resultComputed
	if cached {
		result = resultCached
	}
	s.reg.Searches.Inc(result)
	s.reg.SearchDuration.Observe(d.Seconds(), result)
}

// Analysis 는 사후 분석의 계측 창구다(internal/server 의 matchAnalyzer).
//
// 창구 자체가 nil 이어도 메서드가 돈다. Pool 과 갈리는 자리다 — 저쪽은 늘 생성자를
// 지나는데, 이쪽은 분석기를 구조체 리터럴로 세우는 자리가 있다.
type Analysis struct{ reg *Registry }

// Analysis 는 그 창구를 준다. Pool 과 같은 이유로 r 이 nil 이어도 non-nil 이다.
func (r *Registry) Analysis() *Analysis { return &Analysis{reg: r} }

// 분석 결과의 이름들. analysis_games_total 의 result 라벨이다.
const (
	AnalysisDone    = "done"
	AnalysisDropped = "dropped"
	AnalysisFailed  = "failed"
)

// SetBacklog 은 지금 줄에 남아 있는 양을 놓는다. 판과 手를 같이 받는다.
func (a *Analysis) SetBacklog(games, plies int) {
	if a == nil || a.reg == nil {
		return
	}
	a.reg.AnalysisBacklogGames.Set(float64(games))
	a.reg.AnalysisBacklogPlies.Set(float64(plies))
}

// ObserveGame 은 판 하나가 줄을 떠난 것을 남긴다. dropped 는 아예 안 재고 나간 것이라
// 시간을 안 넣는다.
func (a *Analysis) ObserveGame(result string, d time.Duration) {
	if a == nil || a.reg == nil {
		return
	}
	a.reg.AnalysisGames.Inc(result)
	if result != AnalysisDropped {
		a.reg.AnalysisDuration.Observe(d.Seconds())
	}
}
