package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
	if !quizQueued(t, st, gameID) {
		t.Errorf("game %d has no quiz queued", gameID)
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

	// 목표치로 돈다. 집는 질의가 판을 가리지 않아서 띄워 둔 api 컨테이너의 워커가 먼저
	// 가져갈 수 있는데, 그쪽도 같은 코드로 만들어 남긴다 — 재려는 것은 「누가 집었나」가
	// 아니라 「집으면 만들어져 남고 큐에서 걷히나」다(measureAhead 와 같은 규약).
	deadline := time.Now().Add(20 * time.Second)
	for {
		a.runOneQuiz(t.Context())
		// 생성기가 없는 분석기다. 문항은 비어 있고 행은 남는다 — 그러지 않으면 화면이
		// 「아직 만드는 중」에서 벗어나지 못한다(generateQuiz).
		if _, err := st.GameQuiz(t.Context(), gameID, quiz.Version); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no quiz was saved for the queued game")
		}
		time.Sleep(200 * time.Millisecond)
	}
	if quizQueued(t, st, gameID) {
		t.Errorf("game %d is still queued after its quiz was saved", gameID)
	}
}

// 집어 간 판은 리스가 낡아야 다시 잡힌다. 위와 같은 이유로 이 자리도 컨테이너와 다툰다. 판·手 큐와 같은 규약이고, 여기서 그 규약이
// 재시도를 판다 — 배포가 생성 도중에 끼면 그 판을 다음 워커가 도로 집는다.
func TestAStaleQuizClaimIsTakenBack(t *testing.T) {
	st := testStore(t)
	clearQueues(t, st)
	a, gameID := importedGameInTheQueue(t, st)
	if !a.queueQuiz(t.Context(), gameID) {
		t.Fatal("could not queue the quiz")
	}

	if got, err := st.ClaimQuizJob(t.Context(), time.Now().Add(-quizLease), quizAttempts); err != nil || got != gameID {
		t.Fatalf("claim = %d, %v; want game %d", got, err, gameID)
	}
	if _, err := st.ClaimQuizJob(t.Context(), time.Now().Add(-quizLease), quizAttempts); !errors.Is(err, store.ErrNoQuizJob) {
		t.Errorf("a fresh claim was taken again: %v", err)
	}
	if got, err := st.ClaimQuizJob(t.Context(), time.Now().Add(time.Minute), quizAttempts); err != nil || got != gameID {
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
	if !quizQueued(t, st, gameID) {
		t.Error("the game left the queue even though its quiz was never saved")
	}
}

// 되풀이는 횟수가 묶는다. 상한까지 실패한 판은 그때부터 집히지 않는다 — 한 번이 최대
// 5분이라 상한이 곧 그 판에 쓸 엔진 시간이다.
func TestAQuizStopsBeingClaimedAfterTooManyTries(t *testing.T) {
	st := testStore(t)
	clearQueues(t, st)
	a, gameID := importedGameInTheQueue(t, st)
	a.queueQuiz(t.Context(), gameID)

	stale := func() time.Time { return time.Now().Add(time.Minute) }
	for i := range quizAttempts {
		got, err := st.ClaimQuizJob(t.Context(), stale(), quizAttempts)
		if err != nil || got != gameID {
			t.Fatalf("claim %d = %d, %v; want game %d", i+1, got, err, gameID)
		}
		if err := st.FailQuizJob(t.Context(), gameID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.ClaimQuizJob(t.Context(), stale(), quizAttempts); !errors.Is(err, store.ErrNoQuizJob) {
		t.Errorf("claim after %d failures: %v; want the game to be left alone", quizAttempts, err)
	}
	// 행은 남는다. 여섯 시간 뒤 청소가 지운다.
	//
	// 그래도 「온다」는 아니다. 누구도 집지 않으므로, 참으로 답하면 화면이 오지 않을 것을
	// 기다린다(server/quiz.go 의 queued).
	if quizQueued(t, st, gameID) {
		t.Error("a game nobody will pick up still says its quiz is coming")
	}
	if n, err := st.QuizBacklog(t.Context(), time.Now().Add(time.Minute), quizAttempts); err != nil {
		t.Fatal(err)
	} else if n != 0 {
		t.Errorf("backlog = %d; a game nobody will pick up is not backlog", n)
	}
}

// 한 번도 집히지 않은 판이 먼저다.
//
// 집어서 잰다. 띄워 둔 api 컨테이너의 워커가 먼저 가져가면 갈린다 — 06-status §7 의
// 「DB 테스트 셋」과 같은 자리이고, 실제로 갈리는 것을 봤다. 컨테이너를 내리면 통과한다. 만들지 못해 남은 판이 30분마다 새 판을 제치면,
// 워커가 둘인 배포에서 만들 수 있는 판이 그만큼 늦어진다.
func TestANeverClaimedQuizGoesFirst(t *testing.T) {
	st := testStore(t)
	clearQueues(t, st)
	a, older := importedGameInTheQueue(t, st)
	a.queueQuiz(t.Context(), older)
	// 집혔다가 만들어지지 못한 판이다. 행이 그대로 남는다.
	if got, err := st.ClaimQuizJob(t.Context(), time.Now().Add(-quizLease), quizAttempts); err != nil || got != older {
		t.Fatalf("claim = %d, %v; want game %d", got, err, older)
	}

	_, newer := importedGameInTheQueue(t, st)
	a.queueQuiz(t.Context(), newer)

	// 리스가 낡아 둘 다 집힐 수 있다. 그때 먼저 오는 것은 한 번도 안 집힌 쪽이다.
	if got, err := st.ClaimQuizJob(t.Context(), time.Now().Add(time.Minute), quizAttempts); err != nil || got != newer {
		t.Errorf("claim = %d, %v; want the never-claimed game %d", got, err, newer)
	}
}

// 아직 재는 중인 판도 「온다」다. 가져온 판은 手를 다 재고 나서야 문항을 큐에 세우므로,
// 큐만 보면 가져오기 직후에 연 화면이 그 자리에서 기다리기를 그만둔다.
func TestAGameStillBeingAnalyzedCountsAsComing(t *testing.T) {
	st := testStore(t)
	clearQueues(t, st)
	a, gameID := importedGameInTheQueue(t, st)
	h := &quizHandler{review: &reviewHandler{store: st, analyzer: a}}
	r := httptest.NewRequest(http.MethodGet, "/api/games/1/quiz", nil)

	// 아직 재는 중이다. 문항은 아직 큐에 없다.
	if quizQueued(t, st, gameID) {
		t.Fatal("the quiz is queued before the game was measured")
	}
	if !h.queued(r, gameID) {
		t.Error("said the quiz is not coming while the game is still being analyzed")
	}

	if !a.runOneJob(t.Context()) {
		t.Fatal("the worker did not pick up the queued game")
	}
	if !h.queued(r, gameID) {
		t.Error("said the quiz is not coming right after it was queued")
	}
}

// 문항 큐는 따로 센다. 대수를 정하는 신호에 섞으면 문항 하나가 잡는 5분이 대를 붙이는
// 이유가 된다(journal §138).
//
// 개수를 못 박지 않는다. 두 게이지가 표를 전역으로 세므로 띄워 둔 api 컨테이너의 워커가
// 하나를 집어 가면 값이 달라진다 — 재려는 것은 문항이 판 몫에 섞이지 않는다이지 개수가 아니다.
func TestQueuedQuizzesAreCountedOnTheirOwn(t *testing.T) {
	st := testStore(t)
	clearQueues(t, st)
	reg := metrics.New("api", "test")
	a, gameID := importedGameInTheQueue(t, st)
	a.analysis = reg.Analysis()
	a.queueQuiz(t.Context(), gameID)

	a.sampleBacklog(t.Context())
	quizzes, games := reg.AnalysisBacklogQuizzes.Total(), reg.AnalysisBacklogGames.Total()
	if quizzes < 1 {
		t.Errorf("queued quizzes = %v, want at least the one just queued", quizzes)
	}
	if games > quizzes {
		t.Errorf("queued games = %v with %v quizzes; the quiz must not be counted there too", games, quizzes)
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

// importedGameInTheQueue 는 手가 전부 큐에 들어간 가져온 판 하나와 그 판을 잴 분석기를 준다.
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

// quizQueued 는 그 판의 문항이 아직 큐에 있는가다.
//
// 집어 보지 않는다. 집는 질의는 판을 가리지 않아서(query/analysis.sql) 띄워 둔 api
// 컨테이너의 워커가 먼저 가져가면 답이 달라진다 — 이 질의는 판 하나만 본다.
func quizQueued(t *testing.T, st *store.Store, gameID int64) bool {
	t.Helper()
	ok, err := st.IsQuizQueued(t.Context(), gameID, quizAttempts)
	if err != nil {
		t.Fatalf("is quiz queued: %v", err)
	}
	return ok
}

// 문항이 워커를 다 가져가지 못한다. 자리가 없으면 집지 않고 판과 手 쪽으로 넘어간다.
//
// 여기에 DB 가 필요 없다. 재는 것이 세는 자리 하나다.
func TestQuizzesDoNotTakeEveryWorker(t *testing.T) {
	a := &matchAnalyzer{quizSlots: make(chan struct{}, 1)}
	release, ok := a.takeQuizSlot()
	if !ok {
		t.Fatal("the first quiz could not take a slot")
	}
	if _, ok := a.takeQuizSlot(); ok {
		t.Error("a second quiz took a slot; one worker must stay on the other queues")
	}
	release()
	if _, ok := a.takeQuizSlot(); !ok {
		t.Error("the slot was not given back")
	}
}

// 집지 않는 티어에도 자리가 있다. 거기서 도는 것은 대체 경로뿐인데 그것도 5분짜리
// 탐색이라, 세지 않으면 끝나는 판마다 하나씩 뜬다.
func TestATierThatClaimsNothingStillCountsQuizzes(t *testing.T) {
	st := testStore(t)
	a := newMatchAnalyzer(t.Context(), AnalysisDeps{
		Store:      st,
		NewAnalyst: func() game.Analyst { return stubAnalyst{} },
		Workers:    0,
	})
	if a == nil {
		t.Fatal("no analyzer")
	}
	if got := cap(a.quizSlots); got != 1 {
		t.Errorf("quiz slots = %d, want 1 on a tier that claims nothing", got)
	}
}

// 자리를 세지 않는 분석기는 언제나 자리가 있다. 구조체 리터럴로 만드는 테스트가 그 모양이다.
func TestAnUncountedAnalyzerAlwaysHasASlot(t *testing.T) {
	a := &matchAnalyzer{}
	for range 3 {
		if _, ok := a.takeQuizSlot(); !ok {
			t.Fatal("an analyzer with no slot count refused a quiz")
		}
	}
}
