package server

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// stubAnalyst 는 엔진 없이 평가치만 돌려준다. **판정은 안 준다** — 대인전에 개입이 없다는
// 것을 이 테스트가 같이 지킨다.
type stubAnalyst struct {
	fail bool
}

func (s stubAnalyst) Judge(_ context.Context, _ string, moves []string, ply int) (game.Judgement, error) {
	if s.fail {
		return game.Judgement{}, errors.New("engine down")
	}
	// 手数를 그대로 값으로 쓴다 — 어느 칸에 무엇이 들어갔는지 눈으로 셀 수 있다.
	return game.Judgement{HasEvals: true, SenteCpBefore: ply * 10, SenteCpAfter: ply*10 + 1}, nil
}

// **평가치는 두 행에 다 들어간다.** 대인전 한 판이 `games` 행 둘이라, 한쪽만 채우면
// 같은 판을 두 사람이 다르게 본다.
//
//	SHOWGI_TEST_DATABASE_URL=postgres://showgi:showgi@localhost:5432/showgi go test ./internal/server/
func TestAnalysisFillsBothRowsOfAMatch(t *testing.T) {
	st, ids := matchRowsForAnalysis(t, []string{"7g7f", "3c3d", "2g2f"})

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst { return stubAnalyst{} })
	a.analyze(t.Context(), ids)

	for _, id := range ids {
		rec, err := st.GameRecordAnyOwner(t.Context(), id)
		if err != nil {
			t.Fatalf("read game %d: %v", id, err)
		}
		for _, m := range rec.Moves {
			if m.EvalCp == nil {
				t.Fatalf("game %d ply %d has no eval", id, m.Ply)
			}
		}
		// 마지막 手数만 `After` 로 남는다. 앞의 칸은 다음 회차가 `Before` 로 덮는다
		// (kifu/import.go 와 같은 모양).
		last := rec.Moves[len(rec.Moves)-1]
		if *last.EvalCp != len(rec.Moves)*10+1 {
			t.Errorf("game %d last eval = %d, want %d", id, *last.EvalCp, len(rec.Moves)*10+1)
		}
		if first := rec.Moves[0]; *first.EvalCp != 20 {
			t.Errorf("game %d first eval = %d, want 20 (2手째가 Before 로 덮는다)", id, *first.EvalCp)
		}
	}
}

// **엔진이 답을 못 하면 거기서 멈춘다.** 판정 없이 남는 것이 잘못 채우는 것보다 낫고,
// 그때 화면은 「분석 중」이 아니라 「남지 않았다」로 돌아간다.
func TestAnalysisStopsWhenTheEngineFails(t *testing.T) {
	st, ids := matchRowsForAnalysis(t, []string{"7g7f", "3c3d"})

	a := newMatchAnalyzer(t.Context(), st, func() game.Analyst { return stubAnalyst{fail: true} })
	a.analyze(t.Context(), ids)

	rec, err := st.GameRecordAnyOwner(t.Context(), ids[0])
	if err != nil {
		t.Fatalf("read game: %v", err)
	}
	for _, m := range rec.Moves {
		if m.EvalCp != nil {
			t.Errorf("ply %d got an eval %d, want none", m.Ply, *m.EvalCp)
		}
	}
}

// **줄에 서는 순간부터 「분석 중」이다.** 워커가 그 판에 닿기 전이라도 화면이 기다릴 것을
// 알아야 한다.
func TestQueuedGamesReadAsAnalyzing(t *testing.T) {
	a := &matchAnalyzer{queue: make(chan []int64, 1), pending: map[int64]struct{}{}}

	if a.analyzing(7) {
		t.Error("아무것도 안 세운 판이 분석 중이다")
	}
	a.enqueue([]int64{7, 8})
	if !a.analyzing(7) || !a.analyzing(8) {
		t.Error("줄에 선 판이 분석 중이 아니다")
	}
	a.forget([]int64{7, 8})
	if a.analyzing(7) || a.analyzing(8) {
		t.Error("끝난 판이 아직 분석 중이다")
	}
}

// **분석기가 없어도 부르는 쪽이 안 죽는다.** 엔진 없는 배포에서 대인전이 그대로 도는
// 규약이라, 그 배포에서는 이 값이 nil 인 채로 같은 자리를 지난다.
func TestANilAnalyzerIsSafeToUse(t *testing.T) {
	var a *matchAnalyzer
	a.hold(1)
	a.enqueue([]int64{1, 2})
	a.forget([]int64{1, 2})
	if a.analyzing(1) {
		t.Error("없는 분석기가 분석 중이라고 답했다")
	}
	if newMatchAnalyzer(t.Context(), nil, func() game.Analyst { return stubAnalyst{} }) != nil {
		t.Error("store 가 없으면 분석기를 만들지 않는다")
	}
}

// matchRowsForAnalysis 는 대인전 한 판(행 둘)을 만들고 수를 넣어 둔다.
func matchRowsForAnalysis(t *testing.T, moves []string) (*store.Store, []int64) {
	t.Helper()
	url := os.Getenv("SHOWGI_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SHOWGI_TEST_DATABASE_URL 미설정 — DB 테스트 건너뜀")
	}
	st, err := store.Open(t.Context(), url)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)

	matchID := "analysis-" + time.Now().Format("150405.000000000")
	ids := make([]int64, 0, 2)
	for i, c := range []string{"b", "w"} {
		u, err := st.UpsertUser(t.Context(), "test", matchID+"-"+c, "テスト")
		if err != nil {
			t.Fatalf("upsert user %d: %v", i, err)
		}
		id, err := st.CreateMatchGame(t.Context(), u, c, "", matchID)
		if err != nil {
			t.Fatalf("create match game %s: %v", c, err)
		}
		for ply, usi := range moves {
			if err := st.InsertMove(t.Context(), id, ply+1, usi); err != nil {
				t.Fatalf("insert move %d: %v", ply+1, err)
			}
		}
		ids = append(ids, id)
	}
	return st, ids
}
