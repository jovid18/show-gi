package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/kifu"
	"github.com/jovid18/show-gi/apps/server/internal/metrics"
	"github.com/jovid18/show-gi/apps/server/internal/quiz"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// 판을 다 재고 나면 「분석 중」이 곧바로 꺼진다. 문항은 그때부터 따로 만들어진다.
//
// 한 잡에 묶여 있던 동안은 그래프가 다 찬 뒤에도 최대 5분(quizTimeout) 동안 이 값이
// 참이었고, 되짚기가 그동안 5초마다 판 전체를 다시 받았다(journal §138).
//
//	SHOWGI_TEST_DATABASE_URL=postgres://showgi:showgi@localhost:5432/showgi go test ./internal/server/
func TestAMeasuredGameStopsSayingItIsBeingAnalyzed(t *testing.T) {
	st := testStore(t)
	clearQueues(t, st)
	a, gameID := importedGameInTheQueue(t, st)

	if !a.analyzing(t.Context(), gameID) {
		t.Fatal("a queued import does not show as being analyzed")
	}
	if !a.runOneJob(t.Context()) {
		t.Fatal("the worker did not pick up the queued game")
	}
	if a.analyzing(t.Context(), gameID) {
		t.Error("still says it is being analyzed after every ply was measured")
	}

	// 문항은 아직 없다. 만드는 것은 그 판을 다시 집는 워커다.
	if _, err := st.GameQuiz(t.Context(), gameID, quiz.Version); !errors.Is(err, store.ErrNoQuiz) {
		t.Errorf("GameQuiz = %v, want ErrNoQuiz while the quiz is still queued", err)
	}
	if got := claimedQuiz(t, st); got != gameID {
		t.Errorf("queued quiz = %d, want game %d", got, gameID)
	}
}

// 문항을 만드는 것은 큐를 집은 워커다. 만들고 나면 그 판이 큐에서 걷힌다.
func TestAQueuedQuizIsBuiltByAWorker(t *testing.T) {
	st := testStore(t)
	clearQueues(t, st)
	a, gameID := importedGameInTheQueue(t, st)
	if !a.runOneJob(t.Context()) {
		t.Fatal("the worker did not pick up the queued game")
	}

	if !a.runOneQuiz(t.Context()) {
		t.Fatal("the worker did not pick up the queued quiz")
	}
	// 생성기가 없는 분석기다. 문항은 비어 있고 행은 남는다 — 그러지 않으면 화면이
	// 「아직 만드는 중」에서 벗어나지 못한다(generateQuiz).
	if _, err := st.GameQuiz(t.Context(), gameID, quiz.Version); err != nil {
		t.Errorf("GameQuiz after the worker ran: %v", err)
	}
	if got := claimedQuiz(t, st); got != 0 {
		t.Errorf("game %d is still queued after its quiz was saved", got)
	}
}

// 집어 간 판은 리스가 낡아야 다시 잡힌다. 판·手 큐와 같은 규약이고, 여기서 그 규약이
// 재시도를 판다 — 배포가 생성 도중에 끼면 그 판을 다음 워커가 도로 집는다.
func TestAStaleQuizClaimIsTakenBack(t *testing.T) {
	st := testStore(t)
	clearQueues(t, st)
	a, gameID := importedGameInTheQueue(t, st)
	if !a.queueQuiz(t.Context(), gameID) {
		t.Fatal("could not queue the quiz")
	}

	if got, err := st.ClaimQuizJob(t.Context(), time.Now().Add(-quizLease)); err != nil || got != gameID {
		t.Fatalf("claim = %d, %v; want game %d", got, err, gameID)
	}
	if _, err := st.ClaimQuizJob(t.Context(), time.Now().Add(-quizLease)); !errors.Is(err, store.ErrNoQuizJob) {
		t.Errorf("a fresh claim was taken again: %v", err)
	}
	if got, err := st.ClaimQuizJob(t.Context(), time.Now().Add(time.Minute)); err != nil || got != gameID {
		t.Errorf("a stale claim was not taken back: %d, %v", got, err)
	}
}

// 만들지 못한 판은 큐에 남는다. 그 자리가 이 큐의 재시도다 — 배포가 생성 도중에 끼면
// 풀이 먼저 닫혀 모든 탐색이 즉시 실패하는데, 걷어 버리면 그 판은 영영 문항을 갖지 못한다.
func TestAQuizThatCouldNotBeBuiltStaysInTheQueue(t *testing.T) {
	st := testStore(t)
	clearQueues(t, st)
	a, gameID := importedGameInTheQueue(t, st)
	// 엔진이 죽은 배포다. 한 번도 답을 받지 못하므로 빈 결과를 적지 않는다(generateQuiz).
	a.quiz = quiz.NewBuilder(nil, &fakeSearcher{err: errors.New("fake: engine is down")}, 14)
	a.queueQuiz(t.Context(), gameID)

	if !a.runOneQuiz(t.Context()) {
		t.Fatal("the worker did not pick up the queued quiz")
	}
	if _, err := st.GameQuiz(t.Context(), gameID, quiz.Version); !errors.Is(err, store.ErrNoQuiz) {
		t.Errorf("GameQuiz = %v, want ErrNoQuiz — an empty row would freeze the screen on 「問題はありません」", err)
	}
	// 리스가 낡으면 다음 워커가 도로 집는다.
	if got, err := st.ClaimQuizJob(t.Context(), time.Now().Add(time.Minute)); err != nil || got != gameID {
		t.Errorf("claim after the lease went stale = %d, %v; want game %d", got, err, gameID)
	}
}

// 한 번도 집히지 않은 판이 먼저다. 만들지 못해 남은 판이 30분마다 새 판을 제치면,
// 워커가 둘인 배포에서 만들 수 있는 판이 그만큼 늦어진다.
func TestANeverClaimedQuizGoesFirst(t *testing.T) {
	st := testStore(t)
	clearQueues(t, st)
	a, older := importedGameInTheQueue(t, st)
	a.queueQuiz(t.Context(), older)
	// 집혔다가 만들어지지 못한 판이다. 행이 그대로 남는다.
	if got, err := st.ClaimQuizJob(t.Context(), time.Now().Add(-quizLease)); err != nil || got != older {
		t.Fatalf("claim = %d, %v; want game %d", got, err, older)
	}

	_, newer := importedGameInTheQueue(t, st)
	a.queueQuiz(t.Context(), newer)

	// 리스가 낡아 둘 다 집힐 수 있다. 그때 먼저 오는 것은 한 번도 안 집힌 쪽이다.
	if got, err := st.ClaimQuizJob(t.Context(), time.Now().Add(time.Minute)); err != nil || got != newer {
		t.Errorf("claim = %d, %v; want the never-claimed game %d", got, err, newer)
	}
}

// 문항 큐는 따로 센다. 대수를 정하는 신호에 섞으면 문항 하나가 잡는 5분이 대를 붙이는
// 이유가 된다(journal §138).
func TestQueuedQuizzesAreCountedOnTheirOwn(t *testing.T) {
	st := testStore(t)
	clearQueues(t, st)
	reg := metrics.New("api", "test")
	a, gameID := importedGameInTheQueue(t, st)
	a.analysis = reg.Analysis()
	a.queueQuiz(t.Context(), gameID)

	a.sampleBacklog(t.Context())
	if got := reg.AnalysisBacklogQuizzes.Total(); got != 1 {
		t.Errorf("queued quizzes = %v, want 1", got)
	}
	if got := reg.AnalysisBacklogGames.Total(); got != 1 {
		t.Errorf("queued games = %v, want 1 — the quiz must not be counted there", got)
	}
}

// 세울 자리가 없으면 거짓을 준다. 부르는 쪽이 그것으로 「그 자리에서 만든다」로 갈린다 —
// 세우지도 만들지도 않으면 그 판이 영영 문항을 갖지 못한다.
func TestQueueingAQuizWithoutAnAnalyzerSaysSo(t *testing.T) {
	var a *matchAnalyzer
	if a.queueQuiz(t.Context(), 1) {
		t.Error("a nil analyzer said it queued the quiz")
	}
}

// importedGameInTheQueue 는 手가 전부 줄에 선 가져온 판 하나와 그 판을 잴 분석기를 준다.
//
// 판정기는 낙폭 0을 준다. 여기서 재려는 것이 문항이 언제 만들어지는가라, 판정의 값은
// 아무것이나 된다.
func importedGameInTheQueue(t *testing.T, st *store.Store) (*matchAnalyzer, int64) {
	t.Helper()
	userID, err := st.UpsertUser(t.Context(), "test", "quizjob-"+time.Now().Format("150405.000000000"), "テスト")
	if err != nil {
		t.Fatal(err)
	}
	g, notation, err := kifu.Read(shuffleGameUSI(8))
	if err != nil {
		t.Fatal(err)
	}
	h := &kifuHandler{store: st}
	gameID, err := h.save(t.Context(), userID, "b", string(notation), g, store.ResultWin)
	if err != nil {
		t.Fatal(err)
	}

	a := analyzerFor(st, func() game.Analyst { return stubAnalyst{} })
	if err := a.enqueueImport(t.Context(), gameID, g.StartSFEN, g.Moves); err != nil {
		t.Fatal(err)
	}
	// 남겨 두면 띄워 둔 api 컨테이너의 워커가 집어 간다(06-status §7 의 「DB 테스트 셋」).
	t.Cleanup(func() {
		a.dropJob(context.Background(), importKey(gameID))
		a.discard(context.Background(), importKey(gameID))
		a.dropQuiz(context.Background(), gameID)
	})
	return a, gameID
}

// claimedQuiz 는 지금 줄에 선 판 하나를 집어 그 번호를 준다. 없으면 0이다.
func claimedQuiz(t *testing.T, st *store.Store) int64 {
	t.Helper()
	id, err := st.ClaimQuizJob(t.Context(), time.Now().Add(-quizLease))
	if errors.Is(err, store.ErrNoQuizJob) {
		return 0
	}
	if err != nil {
		t.Fatalf("claim a quiz job: %v", err)
	}
	return id
}
