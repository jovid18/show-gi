// Package metrics 는 서버가 자기 상태를 숫자로 낸다.
//
// 표면이 둘이고 대상이 다르다. /metrics 는 Prometheus 텍스트로 라벨을 다 들고(태스크
// 안에서만 닿는다), CloudWatch EMF 는 차원을 뺀 집계만 stdout 으로 낸다 — EMF 는 차원
// 조합 하나가 곧 과금 대상 지표 하나라서 route 를 차원으로 올리면 지표 수가 경로 수만큼
// 늘어난다(journal §90).
//
// 의존성이 없다. client_golang 을 넣지 않은 것은 이 레포의 직접 의존성이 셋뿐이고
// 여기서 필요한 것이 카운터·게이지·히스토그램 셋뿐이어서다.
package metrics

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
)

// DefaultBuckets 는 초 단위 지연에 쓰는 버킷 경계다. 5ms 부터 30초까지.
//
// 위쪽이 긴 것은 엔진 탐색이 깊이 12에서 초 단위이기 때문이다. HTTP 요청도 같은 경계를
// 쓴다 — /api/explore 처럼 탐색을 기다리는 경로가 있어서 위쪽이 필요하다.
var DefaultBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

// maxSamples 는 예약통에 담아 두는 관측 수다. EMF 의 숫자 배열 상한이 100이라 그 값이다.
const maxSamples = 100

// kind 는 지표의 종류다. 텍스트 표면의 TYPE 줄에 그대로 나간다.
type kind string

const (
	kindCounter   kind = "counter"
	kindGauge     kind = "gauge"
	kindHistogram kind = "histogram"
)

// family 는 이름과 라벨 이름을 공유하는 계열의 묶음이다. 라벨 값 조합 하나가 series 하나다.
type family struct {
	name    string
	help    string
	kind    kind
	labels  []string
	buckets []float64

	mu     sync.Mutex
	series map[string]*series
	// order 는 출력 순서를 고정한다. map 순회 순서로 내보내면 /metrics 의 diff 를
	// 눈으로 대조할 수 없고 테스트도 정렬에 매달린다.
	order []*series
}

// series 는 라벨 값 조합 하나의 값이다.
type series struct {
	labelValues []string

	// value 는 counter·gauge 의 값이다.
	value float64

	// counts·sum·count 는 histogram 의 누적이다. counts 는 buckets 와 같은 길이이고
	// 경계 이하의 관측 수를 담는다(마지막의 +Inf 는 count 가 대신한다).
	counts []uint64
	sum    float64
	count  uint64

	// samples 는 EMF 가 백분위를 내려면 필요한 원값이다. 상한을 넘으면 교체한다 —
	// 버킷만으로는 배열을 못 만들고(개수가 100을 넘는다) 배열만으로는 정확한 개수를
	// 못 낸다. 그래서 둘을 같이 든다.
	samples []float64
	// sampled 는 예약통을 비운 뒤로 들어온 관측 수다. count 와 갈라 두는 것이
	// 필수다 — 교체 확률의 분모가 누적이면 회차가 지날수록 확률이 0으로 내려가고,
	// 배열이 「그 회차 앞머리 100건」으로 굳는다.
	sampled uint64
}

// register 는 계열을 등록한다. 같은 이름을 두 번 등록하면 panic 한다 —
// 이름이 겹치면 텍스트 표면에서 두 TYPE 줄이 나가고, 그건 스크레이퍼가 조용히 버린다.
func (r *Registry) register(f *family) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.families {
		if existing.name == f.name {
			panic("metrics: duplicate metric name " + f.name)
		}
	}
	r.families = append(r.families, f)
}

// get 은 라벨 값 조합에 해당하는 series 를 찾고 없으면 만든다. 호출 측이 mu 를 잡고 있어야 한다.
func (f *family) get(labelValues []string) *series {
	if len(labelValues) != len(f.labels) {
		// 호출 자리가 전부 리터럴이라 이건 코딩 오류다. 조용히 다른 계열에 더하면
		// 지표가 틀린 채로 도는데, 지표는 틀렸다는 것 자체가 안 드러난다.
		panic(fmt.Sprintf("metrics: %s wants %d label values, got %d", f.name, len(f.labels), len(labelValues)))
	}
	key := strings.Join(labelValues, "\x00")
	if s, ok := f.series[key]; ok {
		return s
	}
	s := &series{labelValues: append([]string(nil), labelValues...)}
	if f.kind == kindHistogram {
		s.counts = make([]uint64, len(f.buckets))
	}
	f.series[key] = s
	f.order = append(f.order, s)
	return s
}

// labelMap 은 pick 함수에 넘기는 라벨 한 벌이다.
func (f *family) labelMap(s *series) map[string]string {
	m := make(map[string]string, len(f.labels))
	for i, name := range f.labels {
		m[name] = s.labelValues[i]
	}
	return m
}

// sumFunc 은 pick 이 고른 계열의 값을 더한다. pick 이 nil 이면 전부 더한다.
func (f *family) sumFunc(pick func(map[string]string) bool, of func(*series) float64) float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := 0.0
	for _, s := range f.order {
		if pick != nil && !pick(f.labelMap(s)) {
			continue
		}
		total += of(s)
	}
	return total
}

// Registry 는 이 프로세스가 내는 지표를 다 들고 있다. 무엇을 재는지는 New 에 한 자리로 있다.
type Registry struct {
	// service·environment 는 EMF 의 차원이자 엔티티 정보다. 텍스트 표면에는 안 나간다.
	service     string
	environment string

	mu       sync.Mutex
	families []*family

	// 아래가 이 앱이 재는 것 전부다. 새 지표를 늘릴 때 EMF 쪽도 같이 보게 하려고
	// 한 구조체에 모아 둔다(emf.go 의 Document).
	HTTPRequests *Counter
	HTTPDuration *Histogram
	HTTPPanics   *Counter

	PoolWait  *Histogram
	PoolInUse *Gauge
	PoolSize  *Gauge

	Searches       *Counter
	SearchDuration *Histogram

	MateSearches       *Counter
	MateSearchDuration *Histogram

	WSSessions       *Gauge
	WSSessionsOpened *Counter

	MatchPairings    *Counter
	MatchPairingWait *Histogram
	MatchPairingGap  *Histogram

	AnalysisBacklogGames *Gauge
	AnalysisBacklogPlies *Gauge
	AnalysisGames        *Counter
	AnalysisDuration     *Histogram
	GamesFinished        *Counter
}

// AnalysisBuckets 는 판 하나를 다 재는 데 걸리는 시간의 버킷이다. 30초부터 한 시간까지.
//
// DefaultBuckets 를 못 쓴다. 저쪽 상한이 30초인데 여기는 한 판이 手마다 판정 한 번이라
// (match_analysis.go 의 analyze) 100手면 분 단위가 정상이다.
var AnalysisBuckets = []float64{30, 60, 120, 300, 600, 1800, 3600}

// New 는 이 앱의 지표를 다 만든 레지스트리다.
//
// service·environment 는 EMF 에만 쓰인다. 비워 두면 각각 api·local 로 둔다 —
// 로컬에서 낸 지표가 프로덕션 지표와 같은 자리에 쌓이면 그래프가 조용히 오염된다.
func New(service, environment string) *Registry {
	if service == "" {
		service = "api"
	}
	if environment == "" {
		environment = "local"
	}
	r := &Registry{service: service, environment: environment}

	r.HTTPRequests = r.NewCounter("http_requests_total",
		"HTTP 요청 수", "route", "status")
	r.HTTPDuration = r.NewHistogram("http_request_duration_seconds",
		"HTTP 요청 처리 시간(초)", DefaultBuckets, "route")
	// panic 은 상태 코드로 다 안 잡힌다 — 업그레이드된 연결에서 나면 그 요청의 상태가
	// 이미 101이라 5xx 로 셀 수가 없다. 그래서 따로 센다.
	r.HTTPPanics = r.NewCounter("http_panics_total",
		"핸들러가 panic 한 횟수", "route")

	// 풀 두 개가 같은 계열에 라벨로 갈린다. 탐색부와 詰み solver 는 잡히는 이유가
	// 달라서(cmd/api 의 matePoolSize) 어느 쪽이 밀렸는지가 구별되어야 한다.
	// borrower 는 누가 빌렸나다(usi.WithBorrower). 이 라벨이 「사후 분석이 대국을
	// 굶히는가」를 직접 답한다 — 분석이 도는 동안 borrower=game 의 대기가 튀는지로
	// 갈린다. 분석기를 떼어 낼지의 판단이 그 숫자 하나에 걸려 있다.
	r.PoolWait = r.NewHistogram("engine_pool_wait_seconds",
		"엔진을 빌리기까지 기다린 시간(초)", DefaultBuckets, "pool", "borrower")
	r.PoolInUse = r.NewGauge("engine_pool_in_use",
		"지금 빌려 나가 있는 엔진 수", "pool")
	r.PoolSize = r.NewGauge("engine_pool_size",
		"풀에 있는 엔진 수", "pool")

	// result 는 cached·computed 다. 이 비율이 국면 캐시가 실제로 일하는지를 말한다.
	r.Searches = r.NewCounter("engine_searches_total",
		"탐색 요청 수", "result")
	// 이 값은 풀 대기를 포함한다 — 재는 자리가 풀 바깥이라(archive) 부르는 쪽이 실제로
	// 기다린 시간이다. 엔진 자체가 걸린 시간은 여기서 engine_pool_wait_seconds 를 뺀 것이다.
	r.SearchDuration = r.NewHistogram("engine_search_duration_seconds",
		"탐색 하나가 답을 받기까지 걸린 시간(초). 풀 대기를 포함한다", DefaultBuckets, "result")

	// 詰み 탐색을 탐색부와 갈라 센다. 섞으면 위의 두 지표가 뜻을 잃는다 — 詰み 쪽은
	// 한계까지 다 뒤진 nomate 가 가장 비싸서 분포의 모양이 아예 다르고, 그 두 지표가
	// 부하 회차의 신호다(journal §106).
	//
	// result 는 cached·computed·unproven 이다. unproven 은 checkmate timeout —
	// solver 를 부르고도 답을 못 얻어 캐시에 안 쌓인 것이라, 이 값이 크면 캐시가
	// 영원히 안 채워지는 구간이 있다는 뜻이다.
	r.MateSearches = r.NewCounter("engine_mate_searches_total",
		"詰み 탐색 요청 수", "result")
	r.MateSearchDuration = r.NewHistogram("engine_mate_search_duration_seconds",
		"詰み 탐색 하나가 답을 받기까지 걸린 시간(초). 풀 대기를 포함한다", DefaultBuckets, "result")

	// kind 는 game·match 다. 연결이 아니라 대국 세션을 센다.
	r.WSSessions = r.NewGauge("ws_sessions_active",
		"열려 있는 WebSocket 대국 세션 수", "kind")
	r.WSSessionsOpened = r.NewCounter("ws_sessions_opened_total",
		"열린 WebSocket 대국 세션 수", "kind")

	// 대기열의 셋은 EMF 에 안 올린다. 저쪽은 열 개로 묶여 있어서(emf.go 의 collect) 여기서
	// 넷을 더하면 그 선을 넘고, 그 결정은 밴드 상수를 실제로 재는 회차에 같이 온다
	// (journal §92의 남은 것). 그때까지는 /metrics 와 컨테이너 안에서 읽는다.
	//
	// 대기 중인 사람을 게이지로 안 센다. 대기열은 표에 있고(match_queue) 프로세스가 그것을
	// 소유하지 않아서, 인스턴스마다 올렸다 내리면 탭을 닫은 사람이 영영 안 내려간다 —
	// 「지금 몇 명이 기다리나」는 표를 세는 쪽이 답한다(store.QueueWaiting).
	r.MatchPairings = r.NewCounter("match_pairings_total",
		"대기열이 지은 짝 수")
	r.MatchPairingWait = r.NewHistogram("match_pairing_wait_seconds",
		"짝이 잡히기까지 대기열에서 기다린 시간(초). 짝마다 두 사람 몫이 들어간다", DefaultBuckets)
	// 경계가 밴드의 어휘다. 상한이 BaseMax(800)를 넘어가는 것은 불확실성이 얹히기
	// 때문이다(queue.Band) — 서로를 모르는 두 사람은 그보다 먼 격차로도 붙는다.
	r.MatchPairingGap = r.NewHistogram("match_pairing_rating_gap",
		"짝이 된 두 사람의 레이팅 차", []float64{25, 50, 100, 200, 400, 800, 1600})

	// 사후 분석의 넷. 지금까지 이 층은 로그 문자열로만 보였다(match_analysis.go).
	//
	// 밀린 것을 판과 手 둘로 센다. 판 수만으로는 밀린 일의 크기를 못 말한다 — 회차 4의
	// 세 판이 27·34·123手였다(journal §91). 手 쪽이 나중에 스케일 기준이 될 값이다.
	r.AnalysisBacklogGames = r.NewGauge("analysis_backlog_games",
		"분석을 기다리는 판 수")
	r.AnalysisBacklogPlies = r.NewGauge("analysis_backlog_plies",
		"분석을 기다리는 手의 합")
	// result 는 done·dropped·failed 다. dropped 는 큐가 넘쳐 버린 판이고
	// (analysisQueue) failed 는 판정이 중간에 끊긴 판이다.
	r.AnalysisGames = r.NewCounter("analysis_games_total",
		"분석이 끝난 판 수", "result")
	r.AnalysisDuration = r.NewHistogram("analysis_game_duration_seconds",
		"판 하나를 처음부터 끝까지 재는 데 걸린 시간(초)", AnalysisBuckets)

	// status 는 game.Status 의 값 그대로다. aborted 를 세는 것이 이 지표의 이유다 —
	// 상대의 수를 시한 안에 못 얻어 접은 판이고, games.result 에서는 사람이 창을 닫은
	// 판과 같은 값이 되어 구별되지 않는다(recorder.go 의 resultOf).
	r.GamesFinished = r.NewCounter("game_finished_total",
		"끝난 대국 수", "status")

	return r
}

// NewCounter 는 늘어나기만 하는 지표를 만든다.
func (r *Registry) NewCounter(name, help string, labels ...string) *Counter {
	f := &family{name: name, help: help, kind: kindCounter, labels: labels, series: map[string]*series{}}
	r.register(f)
	return &Counter{f: f}
}

// NewGauge 는 오르내리는 지표를 만든다.
func (r *Registry) NewGauge(name, help string, labels ...string) *Gauge {
	f := &family{name: name, help: help, kind: kindGauge, labels: labels, series: map[string]*series{}}
	r.register(f)
	return &Gauge{f: f}
}

// NewHistogram 은 분포를 재는 지표를 만든다. buckets 가 비면 DefaultBuckets 다.
func (r *Registry) NewHistogram(name, help string, buckets []float64, labels ...string) *Histogram {
	if len(buckets) == 0 {
		buckets = DefaultBuckets
	}
	f := &family{
		name: name, help: help, kind: kindHistogram, labels: labels,
		buckets: buckets, series: map[string]*series{},
	}
	r.register(f)
	return &Histogram{f: f}
}

// Counter 는 늘어나기만 하는 값이다.
type Counter struct{ f *family }

// Inc 는 1을 더한다.
func (c *Counter) Inc(labelValues ...string) { c.Add(1, labelValues...) }

// Add 는 v 를 더한다. 음수는 무시한다 — 카운터가 줄면 스크레이퍼가 재시작으로 읽는다.
func (c *Counter) Add(v float64, labelValues ...string) {
	if v < 0 {
		return
	}
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	c.f.get(labelValues).value += v
}

// Total 은 라벨과 무관하게 다 더한 값이다.
func (c *Counter) Total() float64 { return c.SumFunc(nil) }

// SumFunc 은 pick 이 고른 계열만 더한다.
func (c *Counter) SumFunc(pick func(labels map[string]string) bool) float64 {
	return c.f.sumFunc(pick, func(s *series) float64 { return s.value })
}

// Gauge 는 오르내리는 값이다.
type Gauge struct{ f *family }

// Set 은 값을 그대로 놓는다.
func (g *Gauge) Set(v float64, labelValues ...string) {
	g.f.mu.Lock()
	defer g.f.mu.Unlock()
	g.f.get(labelValues).value = v
}

// Add 는 값을 더한다. 음수를 넣어 뺀다.
func (g *Gauge) Add(v float64, labelValues ...string) {
	g.f.mu.Lock()
	defer g.f.mu.Unlock()
	g.f.get(labelValues).value += v
}

// Total 은 라벨과 무관하게 다 더한 값이다.
func (g *Gauge) Total() float64 { return g.SumFunc(nil) }

// SumFunc 은 pick 이 고른 계열만 더한다.
func (g *Gauge) SumFunc(pick func(labels map[string]string) bool) float64 {
	return g.f.sumFunc(pick, func(s *series) float64 { return s.value })
}

// Histogram 은 분포를 재는 값이다.
type Histogram struct{ f *family }

// Observe 는 관측 하나를 넣는다.
func (h *Histogram) Observe(v float64, labelValues ...string) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	s := h.f.get(labelValues)
	s.sum += v
	s.count++
	for i, b := range h.f.buckets {
		if v <= b {
			s.counts[i]++
		}
	}
	s.observe(v)
}

// observe 는 예약통에 값을 담는다. 호출 측이 mu 를 잡고 있어야 한다.
//
// 상한까지는 그대로 담고 그 뒤로는 확률 maxSamples/sampled 로 자리를 바꾼다(알고리즘 R).
// 앞의 100개만 남기면 회차 앞머리의 요청만 백분위에 반영된다.
//
// 분모가 sampled 인 것이 요점이다. 누적(count)을 쓰면 회차가 지날수록 확률이 0으로
// 내려가 그 굳는 상태가 되고, 하필 바쁜 분에 틀린다.
func (s *series) observe(v float64) {
	s.sampled++
	if len(s.samples) < maxSamples {
		s.samples = append(s.samples, v)
		return
	}
	if i := rand.Uint64N(s.sampled); i < maxSamples {
		s.samples[i] = v
	}
}

// Count 는 pick 이 고른 계열의 관측 수다.
func (h *Histogram) Count(pick func(labels map[string]string) bool) uint64 {
	return uint64(h.f.sumFunc(pick, func(s *series) float64 { return float64(s.count) }))
}

// DrainSamples 는 예약통을 비우고 pick 이 고른 계열의 값만 준다.
//
// 비우는 것은 EMF 가 회차마다 그 회차의 분포를 내야 하기 때문이다. 버킷은 안 건드린다 —
// 텍스트 표면은 누적이어야 한다.
//
// 안 고른 계열도 비운다. 남겨 두면 그 계열은 아무도 안 비워서 100개가 찬 뒤로 교체
// 확률이 0에 붙고, 나중에 그것을 내기 시작하는 날 첫 회차가 기동 무렵의 값을 낸다 —
// pool=mate 와 result=cached 가 지금 그 자리다.
func (h *Histogram) DrainSamples(pick func(labels map[string]string) bool) []float64 {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	var out []float64
	for _, s := range h.f.order {
		if pick == nil || pick(h.f.labelMap(s)) {
			out = append(out, s.samples...)
		}
		s.samples = s.samples[:0]
		s.sampled = 0
	}
	if len(out) > maxSamples {
		// 계열이 여럿이면 합친 것이 상한을 넘을 수 있다. 앞에서 자르면 라벨 하나가
		// 배열을 다 먹으므로 고르게 솎는다.
		out = thin(out, maxSamples)
	}
	return out
}

// LabeledSamples 는 계열 하나의 라벨과 표본이다.
type LabeledSamples struct {
	Labels  map[string]string
	Samples []float64
}

// DrainSamplesAll 은 예약통을 한 번에 비우고 계열마다 갈라 준다.
//
// DrainSamples 를 두 번 부를 수 없어서 있다 — 그쪽은 pick 과 무관하게 예약통을 통째로
// 비우므로 두 번째 호출이 늘 빈 배열이다. 같은 지표를 여러 벌로 낼 때 이쪽을 쓴다.
//
// 라벨을 그대로 준다. 축 하나로 갈라 주면 두 축이 필요해지는 날 이 함수를 다시 고쳐야
// 하는데, 풀 대기가 이미 pool·borrower 둘이다.
//
// 솎지 않고 준다. 부르는 쪽이 무엇끼리 합칠지 정한 뒤에 솎아야 한다.
func (h *Histogram) DrainSamplesAll() []LabeledSamples {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	out := make([]LabeledSamples, 0, len(h.f.order))
	for _, s := range h.f.order {
		if len(s.samples) > 0 {
			out = append(out, LabeledSamples{
				Labels:  h.f.labelMap(s),
				Samples: append([]float64(nil), s.samples...),
			})
		}
		s.samples = s.samples[:0]
		s.sampled = 0
	}
	return out
}

// thin 은 값을 고르게 솎아 n 개로 줄인다.
func thin(vs []float64, n int) []float64 {
	if len(vs) <= n {
		return vs
	}
	out := make([]float64, 0, n)
	step := float64(len(vs)) / float64(n)
	for i := range n {
		out = append(out, vs[int(float64(i)*step)])
	}
	return out
}
