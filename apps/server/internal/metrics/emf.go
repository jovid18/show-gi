package metrics

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// Namespace 는 CloudWatch 의 지표 이름 공간이다.
const Namespace = "show-gi"

// DefaultInterval 은 EMF 한 줄을 내는 주기다.
//
// CloudWatch 의 기본 해상도가 1분이라 그보다 자주 내도 그래프가 나아지지 않는다.
// 요청마다 한 줄을 내는 쪽이 백분위는 더 정확하지만, 로그량이 트래픽에 비례해 늘고
// 그 로그가 곧 요금이다 — 여기는 주기 집계라 트래픽과 무관하게 하루 1,440줄이다.
const DefaultInterval = time.Minute

// Emitter 는 회차마다 EMF 한 줄을 stdout 에 쓴다.
//
// 카운터는 회차 사이의 증분을 낸다. 누적값을 그대로 올리면 CloudWatch 의 Sum 이 매
// 회차마다 지금까지의 전부를 다시 더한다 — 그래프가 단조증가하는 계단이 되고 알람이
// 못 걸린다. 그래서 지난 회차의 값을 들고 있다.
type Emitter struct {
	reg *Registry
	w   io.Writer
	// prev 는 Run 의 goroutine 하나만 만진다. 잠금이 없으므로 EmitTo 를 밖에서
	// 동시에 부르지 않는다 — 지금 그 자리는 테스트뿐이다.
	prev map[string]float64
}

// NewEmitter 는 EMF 를 낼 준비를 한다. w 는 보통 os.Stdout 이다 —
// awslogs 드라이버가 stdout·stderr 를 다 같은 로그 그룹으로 보내고, CloudWatch 는
// 그중 _aws 를 가진 줄만 지표로 뽑는다.
func NewEmitter(reg *Registry, w io.Writer) *Emitter {
	return &Emitter{reg: reg, w: w, prev: map[string]float64{}}
}

// Run 은 every 주기로 EMF 를 내고 ctx 가 끝날 때까지 막힌다.
//
// 끝나면서 한 줄을 더 낸다. 안 내면 종료 직전 회차가 통째로 사라지고, 배포마다
// 그 구간이 비어 그래프에 규칙적인 구멍이 생긴다.
func (e *Emitter) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = DefaultInterval
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			e.emit(time.Now())
		case <-ctx.Done():
			e.emit(time.Now())
			return
		}
	}
}

// emit 은 한 줄을 쓰고 실패는 로그로 끝낸다. 지표를 못 냈다고 대국을 멈추지 않는다.
func (e *Emitter) emit(now time.Time) {
	if err := e.EmitTo(e.w, now); err != nil {
		slog.Warn("cannot write metrics", "err", err)
	}
}

// EmitTo 는 EMF 문서 한 줄을 w 에 쓴다.
func (e *Emitter) EmitTo(w io.Writer, now time.Time) error {
	mets := e.collect()

	defs := make([]map[string]any, 0, len(mets))
	doc := map[string]any{
		"Service":     e.reg.service,
		"Environment": e.reg.environment,
	}
	for _, m := range mets {
		defs = append(defs, map[string]any{"Name": m.name, "Unit": m.unit})
		doc[m.name] = m.value
	}
	doc["_aws"] = map[string]any{
		"Timestamp": now.UnixMilli(),
		"CloudWatchMetrics": []map[string]any{{
			"Namespace": Namespace,
			// 차원이 Service·Environment 둘이다. 차원 조합 하나가 곧 과금 대상 지표
			// 하나라서 route 나 pool 은 여기 올리지 않는다 — 지표 수가 그 값의 개수만큼
			// 곱해진다. Environment 는 개수가 배포 환경 수만큼이고(지금 하나), 그것이
			// 없으면 두 번째 환경이 프로덕션과 같은 계열에 값을 섞어 알람을 흔든다.
			"Dimensions": [][]string{{"Service", "Environment"}},
			"Metrics":    defs,
		}},
	}

	line, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = w.Write(append(line, '\n'))
	return err
}

// metric 은 EMF 한 줄에 실리는 지표 하나다. value 는 숫자이거나 숫자 배열이다.
type metric struct {
	name  string
	unit  string
	value any
}

// collect 는 이번 회차에 낼 지표를 고른다.
//
// 여기 있는 것만 CloudWatch 에 올라간다. 텍스트 표면(/metrics)이 라벨을 다 들고 있는
// 것과 갈리는 자리이고, 갈라 둔 이유는 요금이다.
//
// 지금 열두 개 + 분포 다섯이다. 열 개로 묶어 두던 선을 詰み 층의 셋이 넘었다(journal §111) —
// 차원이 Service·Environment 둘뿐이라 이름 하나가 과금 지표 하나이고, 개당 월 $0.30 이다.
// 늘릴 때마다 이 숫자를 고친다.
func (e *Emitter) collect() []metric {
	r := e.reg
	out := []metric{
		{"HttpRequests", "Count", e.delta("HttpRequests", r.HTTPRequests.Total())},
		{"HttpServerErrors", "Count", e.delta("HttpServerErrors", r.HTTPRequests.SumFunc(serverError))},
		{"HttpPanics", "Count", e.delta("HttpPanics", r.HTTPPanics.Total())},
		{"EngineSearches", "Count", e.delta("EngineSearches", r.Searches.Total())},
		{"EngineSearchesCached", "Count", e.delta("EngineSearchesCached", r.Searches.SumFunc(cached))},
		// 詰み 층은 이 둘로만 보인다. 풀 지표는 여전히 searchPool 필터를 지나서
		// (아래 EnginePoolInUse) 詰み 풀이 줄을 섰는지는 프로덕션 데이터로 알 수 없다.
		//
		// 두 값이 필요하다. 캐시가 일하는지는 비율이라, 부른 총수 없이 히트 수만 올리면
		// 조용해진 것과 안 쓰이는 것이 같은 그림이 된다.
		{"MateSearches", "Count", e.delta("MateSearches", r.MateSearches.Total())},
		{"MateSearchesCached", "Count",
			e.delta("MateSearchesCached", r.MateSearches.SumFunc(cached))},
		{"EnginePoolInUse", "Count", r.PoolInUse.SumFunc(searchPool)},
		{"WsSessionsActive", "Count", r.WSSessions.Total()},
		// 밀린 手가 이 층의 부하 지표다. 판 수는 같이 안 올린다 — 두 값이 늘 같은
		// 방향으로 움직이고, 갈라 보고 싶으면 /metrics 에 둘 다 있다.
		{"AnalysisBacklogPlies", "Count", r.AnalysisBacklogPlies.Total()},
		// 버려진 판은 평가치도 실력도 없이 남는다. 0이 아니면 그 자체로 사고다.
		{"AnalysisGamesDropped", "Count",
			e.delta("AnalysisGamesDropped", r.AnalysisGames.SumFunc(dropped))},
		// 상대의 수를 시한 안에 못 얻어 접은 판. games.result 로는 셀 수 없어서
		// (사람이 창을 닫은 판과 같은 값이 된다) 부하 회차의 깨짐 신호가 이것이다.
		{"GamesAborted", "Count",
			e.delta("GamesAborted", r.GamesFinished.SumFunc(aborted))},
	}

	// 배열은 비어 있으면 아예 안 낸다. 빈 배열을 올리면 그 회차가 0 관측으로 읽히는
	// 것이 아니라 EMF 검증에 걸려 줄이 통째로 버려진다.
	if s := r.HTTPDuration.DrainSamples(nil); len(s) > 0 {
		out = append(out, metric{"HttpDurationSeconds", "Seconds", s})
	}
	// 엔진을 실제로 부른 것만 낸다. 캐시가 답한 것을 섞으면 분포가 0 근처로 몰려
	// 「엔진을 부르면 얼마나 걸리나」를 못 읽는다. 이 값에는 풀 대기가 들어 있고,
	// 가르려면 EnginePoolWaitSeconds 를 뺀다.
	if s := r.SearchDuration.DrainSamples(computed); len(s) > 0 {
		out = append(out, metric{"EngineSearchSeconds", "Seconds", s})
	}
	// 한 번 비우고 셋으로 낸다. 두 번 부르면 두 번째가 빈 배열이다(DrainSamplesAll).
	//
	// 갈라 내는 것 둘이다. borrower=game 은 「대국이 실제로 굶었나」다 — 합친 값에는
	// 사후 분석과 검토가 섞여 있어서 그쪽으로만 읽힌다. pool=mate 는 詰み 풀이 줄을
	// 섰나이고, 크기가 2라 대기가 0보다 큰 것 자체가 포화이므로 borrower 로 더 안
	// 가른다(journal §111).
	var searchAll, searchGame, mateAll []float64
	for _, s := range r.PoolWait.DrainSamplesAll() {
		switch s.Labels["pool"] {
		case PoolSearch:
			searchAll = append(searchAll, s.Samples...)
			if s.Labels["borrower"] == usi.BorrowerGame {
				searchGame = append(searchGame, s.Samples...)
			}
		case PoolMate:
			mateAll = append(mateAll, s.Samples...)
		}
	}
	for _, w := range []struct {
		name string
		vs   []float64
	}{
		{"EnginePoolWaitSeconds", searchAll},
		{"EnginePoolWaitGameSeconds", searchGame},
		{"MatePoolWaitSeconds", mateAll},
	} {
		if len(w.vs) > 0 {
			out = append(out, metric{w.name, "Seconds", thin(w.vs, maxSamples)})
		}
	}
	return out
}

// delta 는 누적 카운터에서 지난 회차 이후의 증분을 낸다.
func (e *Emitter) delta(name string, total float64) float64 {
	d := total - e.prev[name]
	e.prev[name] = total
	if d < 0 {
		// 카운터가 줄 수는 없다. 여기 오면 우리 버그이고, 음수를 올리면 그래프가
		// 아래로 튀어 원인을 찾는 데 시간을 쓴다.
		return 0
	}
	return d
}

func serverError(labels map[string]string) bool {
	return strings.HasPrefix(labels["status"], "5")
}

func cached(labels map[string]string) bool { return labels["result"] == resultCached }

func computed(labels map[string]string) bool { return labels["result"] == resultComputed }

func searchPool(labels map[string]string) bool { return labels["pool"] == PoolSearch }

func dropped(labels map[string]string) bool { return labels["result"] == AnalysisDropped }

func aborted(labels map[string]string) bool { return labels["status"] == "aborted" }
