package usi

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

const testSFEN = "lnsgkgsnl/1r5b1/ppppppppp/9/9/9/PPPPPPPPP/1B5R1/LNSGKGSNL b - 1"

func newFake(t *testing.T) *Engine {
	t.Helper()
	e, err := New("sh", nil, "testdata/fakeengine.sh")
	if err != nil {
		t.Fatalf("가짜 엔진 기동 실패: %v", err)
	}
	t.Cleanup(e.Close)
	return e
}

func TestHandshakeAndName(t *testing.T) {
	e := newFake(t)
	if got := e.Name(); got != "FakeEngine 1.0" {
		t.Fatalf("Name = %q", got)
	}
	for _, opt := range []string{"Skill Level", "USI_Variant", "MultiPV"} {
		if !e.HasOption(opt) {
			t.Errorf("옵션 %q 파싱 실패", opt)
		}
	}
}

func TestSearch(t *testing.T) {
	e := newFake(t)
	res, err := e.SearchDepth(t.Context(), testSFEN, nil, 6)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Best != "7g7f" {
		t.Fatalf("Best = %q", res.Best)
	}
	if res.ScoreCp != 42 || res.IsMate {
		t.Fatalf("Score = %+v (cp 42 기대)", res)
	}
	if res.Depth != 2 {
		t.Fatalf("Depth = %d (2 기대)", res.Depth)
	}
}

func TestSearchWithMoves(t *testing.T) {
	e := newFake(t)
	res, err := e.SearchDepth(t.Context(), testSFEN, []string{"7g7f", "3c3d"}, 6)
	if err != nil || res.Best == "" {
		t.Fatalf("Search(moves): %v %+v", err, res)
	}
}

func TestRestartAfterDeath(t *testing.T) {
	e := newFake(t)
	e.mu.Lock()
	_ = e.send("die")
	e.mu.Unlock()
	// 다음 Search는 실패 → 자동 재기동 → 성공해야 함
	res, err := e.SearchDepth(t.Context(), testSFEN, nil, 6)
	if err != nil {
		t.Fatalf("재기동 후 Search 실패: %v", err)
	}
	if res.Best != "7g7f" {
		t.Fatalf("재기동 후 Best = %q", res.Best)
	}
}

// 재기동 뒤에도 옵션이 남아야 한다. 안 그러면 살아난 엔진만 다른 설정으로 돈다.
func TestSetOptionSurvivesRestart(t *testing.T) {
	e := newFake(t)
	if err := e.SetMultiPV(4); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	_ = e.send("die")
	e.mu.Unlock()
	if _, err := e.SearchDepth(t.Context(), testSFEN, nil, 6); err != nil {
		t.Fatalf("재기동 실패: %v", err)
	}
	e.mu.Lock()
	saved := e.saved["MultiPV"]
	e.mu.Unlock()
	if saved != "4" {
		t.Fatalf("MultiPV 저장값 = %q", saved)
	}
}

func TestParseScoreMate(t *testing.T) {
	var res SearchResult
	parseScore("info depth 5 score mate 3 nodes 1000", &res)
	if !res.IsMate || res.MateIn != 3 || res.ScoreCp != MateCp-30 {
		t.Fatalf("mate 파싱: %+v", res)
	}
	parseScore("info depth 5 score mate -2 nodes 1000", &res)
	if !res.IsMate || res.MateIn != -2 || res.ScoreCp != -MateCp+20 {
		t.Fatalf("mate(-) 파싱: %+v", res)
	}
}

// fail-high/low 속보(lowerbound/upperbound)의 짧은 pv가 이미 받은 exact 수순을 덮어쓰면 안 된다.
func TestParseScoreBoundKeepsExactPv(t *testing.T) {
	var res SearchResult
	parseScore("info depth 18 multipv 1 score cp 900 pv 7g7f 3c3d 6g6f 5a5b", &res)
	parseScore("info depth 19 multipv 1 score cp 946 lowerbound nodes 5000 pv 7g7f", &res)
	if len(res.Lines) != 1 || len(res.Lines[0].PV) != 4 {
		t.Fatalf("bound 라인이 exact pv를 덮어씀: %+v", res.Lines)
	}
	if res.ScoreCp != 900 || len(res.PV) != 4 {
		t.Fatalf("top-level 결과가 bound로 오염됨: cp=%d pv=%v", res.ScoreCp, res.PV)
	}

	// 그 순위에 아직 아무것도 없으면 속보라도 채워 둔다
	var res2 SearchResult
	parseScore("info depth 10 multipv 2 score cp 300 upperbound pv 2g2f", &res2)
	if len(res2.Lines) != 2 || res2.Lines[1].Move != "2g2f" {
		t.Fatalf("빈 순위에 bound 라인이 채워지지 않음: %+v", res2.Lines)
	}
	// 이후 exact 라인이 오면 갱신
	parseScore("info depth 11 multipv 2 score cp 280 pv 2g2f 8c8d 2f2e", &res2)
	if len(res2.Lines[1].PV) != 3 || res2.Lines[1].ScoreCp != 280 {
		t.Fatalf("exact 라인이 bound를 갱신하지 않음: %+v", res2.Lines[1])
	}
}

// movetime 만료로 중단된 마지막 iteration은 bound 표기 없이도 pv가 1~2수만 찍힌다 —
// 직전 iteration의 완결된 수순을 유지해야 한다.
func TestParseScoreTruncatedFinalIterationKeepsFullPv(t *testing.T) {
	var res SearchResult
	parseScore("info depth 13 multipv 3 score cp 151 pv 2g2f 3c3d 4i5h 5a4b 2f2e", &res)
	parseScore("info depth 14 multipv 3 score cp 146 pv 4g4f 5a4b", &res)
	if got := res.Lines[2].PV; len(got) != 5 {
		t.Fatalf("중단된 짧은 라인이 완전한 수순을 덮어씀: %v", got)
	}
	// 3수 이상의 정상 라인은 그대로 갱신된다
	parseScore("info depth 14 multipv 3 score cp 146 pv 4g4f 5a4b 2g2f", &res)
	if got := res.Lines[2].PV; len(got) != 3 || got[0] != "4g4f" {
		t.Fatalf("정상 라인이 갱신되지 않음: %v", got)
	}
}

// 원본 파서는 같은 순위를 계속 덮어써서 마지막 깊이만 남겼다.
// 깊이별로 남지 않으면 "얕게는 좋아 보이는데 깊게는 나쁜 수"를 못 찾는다.
func TestEvalByDepth(t *testing.T) {
	e := newFake(t)
	res, err := e.SearchDepth(t.Context(), testSFEN, nil, 6)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	best := res.EvalByDepth("7g7f")
	if len(best) != 2 || best[0].Depth != 1 || best[0].Cp != 31 || best[1].Depth != 2 || best[1].Cp != 42 {
		t.Fatalf("7g7f 깊이별 = %+v (d1:31, d2:42 기대)", best)
	}

	// 함정 수: 얕게는 +12, 깊게는 -5
	trap := res.EvalByDepth("2g2f")
	if len(trap) != 2 || trap[0].Cp != 12 || trap[1].Cp != -5 {
		t.Fatalf("2g2f 깊이별 = %+v (d1:12, d2:-5 기대)", trap)
	}

	// 최종 순위별 결과는 가장 깊은 값이어야 한다
	if len(res.Lines) != 2 || res.Lines[0].ScoreCp != 42 || res.Lines[1].ScoreCp != -5 {
		t.Fatalf("Lines = %+v", res.Lines)
	}
	if got := res.EvalByDepth("9i9h"); got != nil {
		t.Fatalf("없는 수에 값이 나옴: %+v", got)
	}
}

// 속보 라인의 점수는 확정값이 아니다. 깊이별 기록에 들어가면 개입 판정이
// 엔진이 "아직 모른다"고 말한 값을 근거로 삼게 된다.
func TestHistorySkipsBoundLines(t *testing.T) {
	var res SearchResult
	parseScore("info depth 8 multipv 1 score cp 100 pv 7g7f 3c3d 2g2f", &res)
	parseScore("info depth 9 multipv 1 score cp 900 lowerbound pv 7g7f 3c3d 2g2f", &res)
	if len(res.History) != 1 || res.History[0].Depth != 8 {
		t.Fatalf("속보 라인이 기록됨: %+v", res.History)
	}

	// 같은 (깊이, 순위)가 다시 오면 나중 것이 이긴다
	parseScore("info depth 8 multipv 1 score cp 120 pv 7g7f 3c3d 6g6f", &res)
	if len(res.History) != 1 || res.History[0].ScoreCp != 120 {
		t.Fatalf("같은 깊이·순위가 중복 기록됨: %+v", res.History)
	}
}

// 취소는 즉시 돌아와야 하고, 엔진은 그 뒤에도 멀쩡해야 한다.
// bestmove를 삼키지 못한 채 다음 탐색을 시작하면 이전 탐색의 답을 이번 결과로 읽는다.
func TestSearchCancelSwallowsBestmove(t *testing.T) {
	e := newFake(t)
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := e.search(ctx, testSFEN, nil, "go infinite", 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("취소 에러 기대, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > stopGrace {
		t.Fatalf("취소가 %v 걸림 — stop이 안 먹었다", elapsed)
	}

	// 엔진이 그대로 쓸 수 있어야 한다. 이전 탐색의 bestmove가 남아 있으면 여기서 드러난다.
	res, err := e.SearchDepth(t.Context(), testSFEN, nil, 6)
	if err != nil {
		t.Fatalf("취소 후 Search 실패: %v", err)
	}
	if res.Best != "7g7f" || res.ScoreCp != 42 {
		t.Fatalf("취소 후 결과가 오염됨: %+v", res)
	}
}

// TestRealEngine 은 진짜 USI 엔진에 붙여 파서를 확인한다.
//
// 가짜 엔진은 우리가 적은 것만 돌려주므로, 실제 출력을 읽는다는 증거가 되지 못한다.
// 엔진마다 info 라인의 필드 순서와 잡토큰이 다르고, 거기서 깨지면 조용히 깨진다.
//
// SHOWGI_USI_CMD 가 없으면 건너뛴다 — CI 러너에는 엔진이 없다.
// **엔진을 갈아끼울 때(YaneuraOu) 이 테스트가 첫 관문이다:**
//
//	SHOWGI_USI_CMD=fairy-stockfish go test ./internal/usi/ -run RealEngine -v
func TestRealEngine(t *testing.T) {
	cmd := os.Getenv("SHOWGI_USI_CMD")
	if cmd == "" {
		t.Skip("SHOWGI_USI_CMD 미설정 — 실엔진 검증 건너뜀")
	}

	e, err := New(cmd, nil)
	if err != nil {
		t.Fatalf("엔진 기동 실패 (%s): %v", cmd, err)
	}
	t.Cleanup(e.Close)
	t.Logf("engine: %s", e.Name())

	if err := e.SetMultiPV(3); err != nil {
		t.Fatalf("SetMultiPV: %v", err)
	}
	res, err := e.SearchDepth(t.Context(), testSFEN, nil, 10)
	if err != nil {
		t.Fatalf("SearchDepth: %v", err)
	}

	if !usiMoveRe.MatchString(res.Best) {
		t.Fatalf("Best가 USI 수 형식이 아님: %q", res.Best)
	}
	if res.Depth < 10 {
		t.Fatalf("Depth = %d (요청 10)", res.Depth)
	}
	if len(res.Lines) != 3 {
		t.Fatalf("MultiPV 3인데 Lines = %d개", len(res.Lines))
	}
	for i, l := range res.Lines {
		if l.Move == "" || len(l.PV) == 0 {
			t.Fatalf("Lines[%d]가 비었음: %+v", i, l)
		}
	}

	// 깊이별 기록이 실제로 여러 깊이에 걸쳐 쌓였는가 — 이게 이 PR의 핵심이다
	byDepth := res.EvalByDepth(res.Lines[0].Move)
	if len(byDepth) < 3 {
		t.Fatalf("깊이별 기록이 %d개뿐 — iterative deepening을 못 줍고 있다: %+v", len(byDepth), byDepth)
	}
	for i := 1; i < len(byDepth); i++ {
		if byDepth[i].Depth <= byDepth[i-1].Depth {
			t.Fatalf("깊이가 오름차순이 아님: %+v", byDepth)
		}
	}
	t.Logf("best=%s cp=%d depth=%d, %s의 깊이별=%+v",
		res.Best, res.ScoreCp, res.Depth, res.Lines[0].Move, byDepth)
}

// stop을 무시하는 엔진은 버리고 재기동한다. 취소는 그래도 즉시 돌아온다.
func TestSearchCancelRestartsDeafEngine(t *testing.T) {
	e := newFake(t)
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	if _, err := e.search(ctx, testSFEN, nil, "go deaf", 0); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("취소 에러 기대, got %v", err)
	}
	// 재기동됐으므로 다음 탐색이 정상이어야 한다
	res, err := e.SearchDepth(t.Context(), testSFEN, nil, 6)
	if err != nil {
		t.Fatalf("재기동 후 Search 실패: %v", err)
	}
	if res.Best != "7g7f" {
		t.Fatalf("재기동 후 Best = %q", res.Best)
	}
}
