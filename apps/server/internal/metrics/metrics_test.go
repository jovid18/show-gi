package metrics

import (
	"strings"
	"testing"
	"time"
)

func TestCounterAndGaugeText(t *testing.T) {
	r := New("api", "test")
	r.HTTPRequests.Inc("GET /healthz", "200")
	r.HTTPRequests.Inc("GET /healthz", "200")
	r.HTTPRequests.Inc("POST /api/explore", "401")
	r.WSSessions.Add(1, KindGame)

	out := text(t, r)
	for _, want := range []string{
		`# TYPE http_requests_total counter`,
		`http_requests_total{route="GET /healthz",status="200"} 2`,
		`http_requests_total{route="POST /api/explore",status="401"} 1`,
		`ws_sessions_active{kind="game"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("텍스트 표면에 %q 가 없다:\n%s", want, out)
		}
	}
}

func TestCounterNeverGoesDown(t *testing.T) {
	r := New("api", "test")
	r.HTTPRequests.Add(3, "GET /healthz", "200")
	r.HTTPRequests.Add(-2, "GET /healthz", "200")
	if got := r.HTTPRequests.Total(); got != 3 {
		t.Fatalf("Total()=%v, 음수가 반영됐다", got)
	}
}

func TestGaugeGoesBothWays(t *testing.T) {
	r := New("api", "test")
	close1 := r.Session(KindGame)
	close2 := r.Session(KindGame)
	if got := r.WSSessions.Total(); got != 2 {
		t.Fatalf("두 판 열린 뒤 Total()=%v", got)
	}
	close1()
	close2()
	if got := r.WSSessions.Total(); got != 0 {
		t.Fatalf("둘 다 닫은 뒤 Total()=%v — 게이지가 샌다", got)
	}
	if got := r.WSSessionsOpened.Total(); got != 2 {
		t.Fatalf("누적 세션 수=%v", got)
	}
}

func TestNilRegistryIsSilent(t *testing.T) {
	// 지표가 꺼진 배포(테스트가 그렇다)에서 호출 자리가 nil 검사를 안 해도 되게 한다.
	var r *Registry
	r.ObserveHTTP("GET /healthz", "200", 0)
	r.Session(KindGame)()
}

func TestHistogramBuckets(t *testing.T) {
	r := New("api", "test")
	// 0.01·0.3·60초. 마지막은 마지막 버킷(30)도 넘는다.
	r.HTTPDuration.Observe(0.01, "GET /healthz")
	r.HTTPDuration.Observe(0.3, "GET /healthz")
	r.HTTPDuration.Observe(60, "GET /healthz")

	out := text(t, r)
	for _, want := range []string{
		`http_request_duration_seconds_bucket{route="GET /healthz",le="0.01"} 1`,
		`http_request_duration_seconds_bucket{route="GET /healthz",le="0.25"} 1`,
		`http_request_duration_seconds_bucket{route="GET /healthz",le="0.5"} 2`,
		`http_request_duration_seconds_bucket{route="GET /healthz",le="30"} 2`,
		`http_request_duration_seconds_bucket{route="GET /healthz",le="+Inf"} 3`,
		`http_request_duration_seconds_count{route="GET /healthz"} 3`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("텍스트 표면에 %q 가 없다:\n%s", want, out)
		}
	}
}

func TestSamplesStayBoundedAndDrain(t *testing.T) {
	r := New("api", "test")
	for range 500 {
		r.HTTPDuration.Observe(0.1, "GET /healthz")
	}
	got := r.HTTPDuration.DrainSamples(nil)
	if len(got) != maxSamples {
		t.Fatalf("표본 %d개 — EMF 상한(%d)을 넘거나 모자란다", len(got), maxSamples)
	}
	if again := r.HTTPDuration.DrainSamples(nil); len(again) != 0 {
		t.Fatalf("비운 뒤에 %d개가 남았다", len(again))
	}
	// 버킷은 비우지 않는다. 텍스트 표면은 누적이어야 한다.
	if n := r.HTTPDuration.Count(nil); n != 500 {
		t.Fatalf("관측 수=%d — DrainSamples 가 버킷까지 건드렸다", n)
	}
}

func TestDrainSamplesAcrossSeriesStaysUnderLimit(t *testing.T) {
	r := New("api", "test")
	for range maxSamples {
		r.HTTPDuration.Observe(0.1, "GET /a")
		r.HTTPDuration.Observe(0.2, "GET /b")
	}
	got := r.HTTPDuration.DrainSamples(nil)
	if len(got) > maxSamples {
		t.Fatalf("계열 둘을 합쳐 %d개 — 상한을 넘겼다", len(got))
	}
	// 솎을 때 한 라벨이 배열을 다 먹으면 안 된다.
	var a, b int
	for _, v := range got {
		if v == 0.1 {
			a++
		} else {
			b++
		}
	}
	if a == 0 || b == 0 {
		t.Fatalf("솎은 결과가 한쪽에 몰렸다: 0.1이 %d개, 0.2가 %d개", a, b)
	}
}

func TestSumFuncPicksSeries(t *testing.T) {
	r := New("api", "test")
	r.HTTPRequests.Inc("GET /a", "200")
	r.HTTPRequests.Inc("GET /a", "503")
	r.HTTPRequests.Inc("GET /b", "500")
	if got := r.HTTPRequests.SumFunc(serverError); got != 2 {
		t.Fatalf("5xx 합=%v", got)
	}
	if got := r.HTTPRequests.Total(); got != 3 {
		t.Fatalf("전체 합=%v", got)
	}
}

func TestLabelCountMismatchPanics(t *testing.T) {
	r := New("api", "test")
	defer func() {
		if recover() == nil {
			t.Fatal("라벨 수가 틀렸는데 panic 하지 않았다 — 조용히 다른 계열에 더한다")
		}
	}()
	r.HTTPRequests.Inc("GET /healthz")
}

func TestDuplicateNamePanics(t *testing.T) {
	r := New("api", "test")
	defer func() {
		if recover() == nil {
			t.Fatal("같은 이름을 두 번 등록했는데 panic 하지 않았다")
		}
	}()
	r.NewCounter("http_requests_total", "겹치는 이름")
}

func TestLabelValueEscaping(t *testing.T) {
	r := New("api", "test")
	c := r.NewCounter("weird_total", "따옴표와 역슬래시", "name")
	c.Inc(`a"b\c`)
	out := text(t, r)
	if !strings.Contains(out, `weird_total{name="a\"b\\c"} 1`) {
		t.Fatalf("라벨 값을 안 감쌌다:\n%s", out)
	}
}

func text(t *testing.T, r *Registry) string {
	t.Helper()
	var b strings.Builder
	if err := r.WriteText(&b); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	return b.String()
}

// 회차를 여러 번 비워도 표본이 그 회차 전체를 대표해야 한다.
//
// 교체 확률의 분모를 누적 관측 수로 두면 회차가 지날수록 확률이 0으로 내려가서,
// 배열이 「그 회차 앞머리 100건」으로 굳는다. 그러면 앞이 조용하고 뒤가 밀린 분에
// p95 가 0.01초로 나온다 — 하필 알람이 울려야 하는 분이다.
func TestSamplesRepresentEachInterval(t *testing.T) {
	const rounds, perRound = 20, 500

	r := New("api", "test")
	late := 0
	for range rounds {
		// 앞 절반은 빠르고 뒤 절반은 느리다. 표본에 느린 쪽이 절반쯤 있어야 한다.
		for i := range perRound {
			v := 0.01
			if i >= perRound/2 {
				v = 1.0
			}
			r.HTTPDuration.Observe(v, "GET /x")
		}
		for _, v := range r.HTTPDuration.DrainSamples(nil) {
			if v == 1.0 {
				late++
			}
		}
	}

	// 고르게 뽑으면 회차마다 50개 안팎이다. 굳으면 첫 회차 뒤로 0에 붙는다.
	if late < rounds*maxSamples/4 {
		t.Fatalf("느린 쪽 표본이 %d개 / %d회차 — 회차가 지나며 굳었다", late, rounds)
	}
}

// 레지스트리가 없어도 배선이 그대로 돈다. 「nil 이면 계측만 꺼진다」가 창구 둘에서도
// 성립해야 한다 — 안 그러면 지표를 끈 배포가 기동에서 죽는다.
func TestNilRegistryHooksAreSilent(t *testing.T) {
	var r *Registry
	p := r.Pool(PoolSearch)
	p.SetSize(3)
	p.ObserveWait(time.Second)
	p.ObserveInUse(1)
	r.Search().ObserveSearch(time.Second, true)
	r.ObservePanic("GET /x")
}

// 라벨이 없는 계열도 텍스트 표면에 나가야 한다. 대기열의 셋이 이 앱의 첫 라벨 없는
// 지표이고(match_pairings_total 외 둘), EMF 에는 안 올리므로 여기가 유일한 출구다.
func TestUnlabeledFamiliesReachTheTextSurface(t *testing.T) {
	r := New("api", "test")
	// 짝 하나. 대기 시간은 두 사람 몫이 들어간다.
	r.ObservePairing(3*time.Second, 5*time.Second, 120)

	var b strings.Builder
	if err := r.WriteText(&b); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	out := b.String()
	for _, want := range []string{
		"match_pairings_total 1",
		"match_pairing_wait_seconds_count 2",
		"match_pairing_rating_gap_count 1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("텍스트 표면에 %q 가 없다:\n%s", want, out)
		}
	}
}
