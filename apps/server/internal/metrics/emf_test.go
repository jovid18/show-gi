package metrics

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// at 은 테스트가 쓰는 고정 시각이다. 실제 시각을 쓰면 기대값을 쓸 수 없다.
var at = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func TestEMFDocumentShape(t *testing.T) {
	r := New("api", "prod")
	r.HTTPRequests.Inc("GET /healthz", "200")
	r.HTTPDuration.Observe(0.02, "GET /healthz")

	doc := emit(t, NewEmitter(r, nil), at)

	// 스펙이 요구하는 것 — _aws 는 루트에 있고 Timestamp 는 밀리초다.
	meta, ok := doc["_aws"].(map[string]any)
	if !ok {
		t.Fatalf("_aws 가 없다: %v", doc)
	}
	if got := meta["Timestamp"]; got != float64(at.UnixMilli()) {
		t.Errorf("Timestamp=%v, want %v", got, at.UnixMilli())
	}
	directives, ok := meta["CloudWatchMetrics"].([]any)
	if !ok || len(directives) != 1 {
		t.Fatalf("CloudWatchMetrics=%v", meta["CloudWatchMetrics"])
	}
	d := directives[0].(map[string]any)
	if d["Namespace"] != Namespace {
		t.Errorf("Namespace=%v", d["Namespace"])
	}

	// 차원은 Service·Environment 둘이다. 더 늘어나면 지표 수가 그만큼 곱해지고
	// 그게 요금이다. Environment 가 빠지면 두 환경이 한 계열에 섞인다.
	dims := d["Dimensions"].([]any)
	if len(dims) != 1 {
		t.Fatalf("차원 집합이 하나가 아니다: %v", dims)
	}
	set := dims[0].([]any)
	if len(set) != 2 || set[0] != "Service" || set[1] != "Environment" {
		t.Fatalf("차원이 Service·Environment 가 아니다: %v", set)
	}
	if doc["Service"] != "api" || doc["Environment"] != "prod" {
		t.Errorf("Service=%v Environment=%v", doc["Service"], doc["Environment"])
	}

	// 선언한 지표는 전부 루트에 값이 있어야 한다. 없으면 그 줄은 통째로 버려진다.
	for _, m := range d["Metrics"].([]any) {
		name := m.(map[string]any)["Name"].(string)
		if _, ok := doc[name]; !ok {
			t.Errorf("%s 를 선언했는데 루트에 값이 없다", name)
		}
	}
}

func TestEMFCountersAreDeltas(t *testing.T) {
	r := New("api", "prod")
	e := NewEmitter(r, nil)

	r.HTTPRequests.Add(5, "GET /healthz", "200")
	if got := emit(t, e, at)["HttpRequests"]; got != 5.0 {
		t.Fatalf("첫 회차 HttpRequests=%v", got)
	}

	// 다음 회차에서 두 건이 더 왔다. 누적(7)이 아니라 증분(2)이어야 한다.
	r.HTTPRequests.Add(2, "GET /healthz", "200")
	if got := emit(t, e, at)["HttpRequests"]; got != 2.0 {
		t.Fatalf("두 번째 회차 HttpRequests=%v — 누적을 올리면 Sum 이 매번 전부를 더한다", got)
	}

	// 아무 일도 없던 회차는 0이다. 지표가 아예 빠지면 알람이 결측으로 읽는다.
	if got := emit(t, e, at)["HttpRequests"]; got != 0.0 {
		t.Fatalf("조용한 회차 HttpRequests=%v", got)
	}
}

func TestEMFOmitsEmptyArrays(t *testing.T) {
	r := New("api", "prod")
	doc := emit(t, NewEmitter(r, nil), at)
	for _, name := range []string{"HttpDurationSeconds", "EngineSearchSeconds", "EnginePoolWaitSeconds"} {
		if _, ok := doc[name]; ok {
			t.Errorf("%s 가 관측 없이 나갔다 — 빈 배열은 EMF 검증에 걸린다", name)
		}
	}
}

func TestEMFArraysStayUnderSpecLimit(t *testing.T) {
	r := New("api", "prod")
	for range 1000 {
		r.HTTPDuration.Observe(0.05, "GET /healthz")
	}
	vs, ok := emit(t, NewEmitter(r, nil), at)["HttpDurationSeconds"].([]any)
	if !ok {
		t.Fatal("HttpDurationSeconds 가 배열이 아니다")
	}
	if len(vs) > 100 {
		t.Fatalf("배열이 %d개 — 스펙 상한은 100이다", len(vs))
	}
	// 개수는 배열이 아니라 카운터로 낸다. 표본을 잘라도 개수는 정확해야 한다.
	if n := r.HTTPDuration.Count(nil); n != 1000 {
		t.Fatalf("관측 수=%d", n)
	}
}

func TestEMFSearchSecondsExcludesCache(t *testing.T) {
	r := New("api", "prod")
	s := r.Search()
	s.ObserveSearch(3*time.Second, false)
	s.ObserveSearch(time.Millisecond, true)

	doc := emit(t, NewEmitter(r, nil), at)
	vs := doc["EngineSearchSeconds"].([]any)
	if len(vs) != 1 || vs[0] != 3.0 {
		t.Fatalf("EngineSearchSeconds=%v — 캐시가 답한 것이 섞였다", vs)
	}
	if doc["EngineSearches"] != 2.0 || doc["EngineSearchesCached"] != 1.0 {
		t.Fatalf("탐색 수=%v 캐시=%v", doc["EngineSearches"], doc["EngineSearchesCached"])
	}
}

func TestEMFPoolWaitIsSearchPoolOnly(t *testing.T) {
	r := New("api", "prod")
	r.Pool(PoolSearch).ObserveWait(2 * time.Second)
	r.Pool(PoolMate).ObserveWait(9 * time.Second)
	r.Pool(PoolSearch).ObserveInUse(2)
	r.Pool(PoolMate).ObserveInUse(1)

	doc := emit(t, NewEmitter(r, nil), at)
	vs := doc["EnginePoolWaitSeconds"].([]any)
	if len(vs) != 1 || vs[0] != 2.0 {
		t.Fatalf("EnginePoolWaitSeconds=%v — 詰み 풀이 섞였다", vs)
	}
	if doc["EnginePoolInUse"] != 2.0 {
		t.Fatalf("EnginePoolInUse=%v", doc["EnginePoolInUse"])
	}
}

func TestEMFIsOneLine(t *testing.T) {
	r := New("api", "prod")
	var b strings.Builder
	if err := NewEmitter(r, nil).EmitTo(&b, at); err != nil {
		t.Fatalf("EmitTo: %v", err)
	}
	out := b.String()
	if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Fatalf("EMF 는 줄 하나여야 한다 — CloudWatch 가 로그 이벤트 하나를 통째로 파싱한다:\n%q", out)
	}
}

func emit(t *testing.T, e *Emitter, now time.Time) map[string]any {
	t.Helper()
	var b strings.Builder
	if err := e.EmitTo(&b, now); err != nil {
		t.Fatalf("EmitTo: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(b.String()), &doc); err != nil {
		t.Fatalf("EMF 가 JSON 이 아니다: %v\n%s", err, b.String())
	}
	return doc
}

// 안 고른 계열의 예약통도 비워야 한다. 남겨 두면 그 계열은 100개가 찬 뒤로 교체
// 확률이 0에 붙어, 나중에 그것을 내기 시작하는 날 첫 회차가 기동 무렵 값을 낸다.
func TestDrainEmptiesUnpickedSeriesToo(t *testing.T) {
	r := New("api", "prod")
	mate := r.Pool(PoolMate)
	for range maxSamples + 50 {
		mate.ObserveWait(9 * time.Second)
	}
	// 회차 하나가 지난다. 낸 것은 탐색 풀뿐이다(collect 가 그것만 고른다).
	emit(t, NewEmitter(r, nil), at)

	// 그 뒤에 詰み 풀의 값이 바뀌면, 다음에 그것을 낼 때 새 값이 보여야 한다.
	mate.ObserveWait(time.Millisecond)
	got := r.PoolWait.DrainSamples(func(l map[string]string) bool { return l["pool"] == PoolMate })
	if len(got) != 1 || got[0] != 0.001 {
		t.Fatalf("詰み 풀 표본=%v — 안 비워져서 옛 값이 남았다", got)
	}
}
