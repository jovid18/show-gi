// Package server 는 HTTP 표면과 프로세스 수명을 담당한다.
//
// 핸들러가 대국 상태를 직접 읽지 않는다. 상태는 세션 goroutine이 소유하고,
// 핸들러는 채널로 물어본다 — 지름길을 내는 순간 잠금이 필요해진다.
//
// 화면에 나가는 cp는 전부 **플레이어 관점**이다. DB(先手 관점)·엔진과 캐시(수번 관점)에서
// 오는 값은 이 패키지 경계에서 뒤집는다 — 안 뒤집으면 색이 다른 두 판을 나란히 못 놓고,
// 한 줄을 넘겨 보는 동안 부호가 뒤집힌다(06-status.md §33).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/auth"
	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/quiz"
	"github.com/jovid18/show-gi/apps/server/internal/shogi"
	"github.com/jovid18/show-gi/apps/server/internal/store"
)

// shutdownGrace 는 종료 신호를 받고 진행 중인 요청을 기다려주는 시간이다.
// 엔진 탐색이 걸린 요청이 있어도 이 안에서는 끝난다.
const shutdownGrace = 10 * time.Second

// Options 는 서버가 밖에서 받아야 하는 것들이다.
type Options struct {
	// NewOpponent 는 대국마다 상대를 하나 만든다. nil이면 /ws/game 이 503이다 —
	// 프로세스는 안 죽인다(아래 /healthz 참조).
	NewOpponent func() game.Opponent

	// NewAnalyst 가 nil이면 개입 없이 대국만 한다.
	NewAnalyst func() game.Analyst

	// Mate·Search·Explainer 는 **값이라 대국 사이에 공유된다**(NewOpponent·NewAnalyst 와
	// 다른 점이다) — 셋 다 풀·캐시가 본체이고 대국별 상태가 없다. nil이면 그 기능만 꺼진다.

	// Mate 는 詰み solver 다. nil이면 詰み 게이지가 꺼진다.
	Mate game.MateSearcher

	// StartSFEN·HumanColor·ObservePlies 는 대국을 어디서 시작할지 정한다. 비어 있으면
	// 평수 초기 국면·先手·기본 관측 구간이다. 지금은 테스트만 채운다 — 되짚기는 이 값이
	// 아니라 기록의 `games.start_sfen` 을 쓴다(review.go·whatif.go).
	StartSFEN    string
	HumanColor   shogi.Color
	ObservePlies int

	// Store 는 국면 캐시이자 **대국 기록**이다. nil이어도 대국은 된다 — 캐시가 없으면
	// 매번 계산하고 기록이 없으면 남지 않을 뿐이다. /healthz 의 `db` 로 드러낸다.
	Store *store.Store

	// Level 은 개입 임계치를 정하는 실력 구간이다. 기록에도 같이 남는다 —
	// 어느 임계치에서 걸린 개입인지를 모르면 나중에 상수를 흔들어 볼 수 없다.
	Level intervene.Level

	// Search 는 가정 수순·手筋 힌트가 쓰는 엔진이다(whatif.go). nil이면 그 표면만 꺼지고
	// 되짚기는 그대로 돈다.
	Search Searcher

	// Explainer 는 개입 문구를 만든다. nil이면 결정적 템플릿이 나간다.
	Explainer explain.Explainer

	// Summarizer 는 대국 후 총평을 만든다. nil이면 결정적 총평이 나간다 — **총평 자체는
	// 안 꺼진다.** 숫자와 사실은 기록에서 나오므로 LLM이 없어도 화면이 빈 자리로 남지 않는다.
	Summarizer explain.Summarizer

	// Quiz 는 되짚기 퀴즈의 **생성기**다. nil이면 문항이 안 만들어지고, 그때 되짚기의
	// 퀴즈 자리는 조용히 비어 있다 — 읽는 표면은 이 값과 무관하게 늘 있다(quiz.go).
	//
	// **총평과 달리 여기서만 만들어진다.** 되짚기에서 만들면 그 탐색이 진행 중인 다른
	// 대국의 착수를 기다리게 한다(06-status.md §53).
	Quiz *quiz.Builder

	// Google·SessionSecret 이 다 있어야 로그인이 켜진다(Store 도 필요하다 — auth.go).
	// 하나라도 비면 표면이 통째로 닫히고 익명 대국으로 남는다.
	Google        *auth.Google
	SessionSecret string

	// PublicOrigin 은 브라우저가 이 서버를 부르는 주소다. 비면 요청에서 되짚는다(auth.go).
	PublicOrigin string
}

// Handler 는 라우팅만 조립한다. 테스트가 서버를 띄우지 않고 이걸 그대로 쓴다.
func Handler(opts Options) http.Handler {
	mux := http.NewServeMux()

	// 로드밸런서와 배포 워크플로가 보는 곳. 인증 뒤에 두지 않는다.
	//
	// **엔진이 없어도 200이다.** 여기서 실패하면 ECS가 재시작을 반복해 사이트 전체가
	// 내려간다. 대신 `engine` 필드로 드러낸다 — 배포 워크플로가 그걸 확인한다(§11).
	engineReady := opts.NewOpponent != nil
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		// DB는 기동 때 붙었어도 나중에 끊길 수 있으므로 매번 확인한다.
		// 엔진은 프로세스라 살아 있으면 살아 있는 것이고, 죽으면 다음 탐색에서 재기동된다.
		dbReady := false
		if opts.Store != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			dbReady = opts.Store.Ping(ctx) == nil
			cancel()
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "engine": engineReady, "db": dbReady,
		})
	})

	// 로그인. **켜지지 않아도 /api/me 는 있다** — 화면이 「로그인이라는 것이 이 배포에
	// 있는가」를 물어보는 자리이고, 없으면 그 물음이 404가 되어 고장과 구별되지 않는다.
	ah := &authHandler{
		google:       opts.Google,
		codec:        auth.NewCodec(opts.SessionSecret),
		store:        opts.Store,
		publicOrigin: opts.PublicOrigin,
	}
	mux.HandleFunc("GET /api/me", ah.me)
	if ah.enabled() {
		mux.HandleFunc("GET /api/auth/google/start", ah.start)
		mux.HandleFunc("GET "+callbackPath, ah.callback)
		mux.HandleFunc("POST /api/auth/logout", ah.logout)
	}

	// 시작 화면이 고를 진형 목록. **DB도 엔진도 로그인도 필요 없다** — 상수 목록이라
	// (internal/book) 무엇이 꺼져 있어도 이 자리는 답한다.
	mux.HandleFunc("GET /api/openings", openings)

	// 끝난 판을 되짚는 표면(review.go). **DB에 매여 있고 엔진과 무관하다** — 가정 수순만
	// 엔진이 필요해 그 한 경로가 따로 503이 된다(README 라우트 표). 화면은 /healthz 의
	// `engine` 을 보고 미리 그 자리를 닫는다.
	storeUnavailable := func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "store_unavailable",
			"message": "対局の記録を利用できません。",
		})
	}
	if opts.Store != nil {
		rev := &reviewHandler{store: opts.Store, auth: ah, summarizer: opts.Summarizer, level: opts.Level}
		mux.HandleFunc("GET /api/games", rev.list)
		mux.HandleFunc("GET /api/games/{id}", rev.detail)
		// 총평은 기보와 **따로** 간다 — 이쪽만 LLM을 기다린다(review.go summary).
		mux.HandleFunc("GET /api/games/{id}/summary", rev.summary)

		// 퀴즈(quiz.go). **엔진과 무관하다** — 문항은 판이 끝나는 자리에서 이미 만들어져
		// 있고 채점은 저장된 트리를 읽는 일뿐이다. 되짚기와 같은 문으로 기록을 읽는다.
		qz := &quizHandler{review: rev}
		mux.HandleFunc("GET /api/games/{id}/quiz", qz.get)
		mux.HandleFunc("POST /api/games/{id}/quiz/mate", qz.mate)
		mux.HandleFunc("POST /api/games/{id}/quiz/best", qz.best)

		// 이어하기(resume.go). **엔진과 무관하다** — 여기는 묻고 답하는 자리뿐이고,
		// 실제로 이어 두는 것은 `/ws/game?resume=` 이라 그쪽이 엔진에 매여 있다.
		res := &resumeHandler{store: opts.Store, auth: ah}
		mux.HandleFunc("GET /api/resumable", res.find)
		mux.HandleFunc("POST /api/resumable/{id}/decline", res.decline)

		if opts.Search != nil {
			wi := &whatifHandler{store: opts.Store, search: opts.Search, auth: ah}
			mux.HandleFunc("POST /api/games/{id}/whatif", wi.play)
		} else {
			mux.HandleFunc("POST /api/games/{id}/whatif", func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusServiceUnavailable, map[string]any{
					"error":   "engine_unavailable",
					"message": "対局エンジンを利用できません。",
				})
			})
		}
	} else {
		mux.HandleFunc("GET /api/games", storeUnavailable)
		mux.HandleFunc("GET /api/games/{id}", storeUnavailable)
		mux.HandleFunc("GET /api/games/{id}/summary", storeUnavailable)
		mux.HandleFunc("GET /api/games/{id}/quiz", storeUnavailable)
		mux.HandleFunc("POST /api/games/{id}/quiz/mate", storeUnavailable)
		mux.HandleFunc("POST /api/games/{id}/quiz/best", storeUnavailable)
		mux.HandleFunc("POST /api/games/{id}/whatif", storeUnavailable)
		// **여기는 503이 아니라 「없다」다.** 기록이 없는 배포에는 이어할 판이 있을 수가
		// 없고, 첫 화면이 늘 부르는 자리라 실패로 답하면 물음 카드가 아니라 오류가 뜬다.
		mux.HandleFunc("GET /api/resumable", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"game": nil})
		})
		mux.HandleFunc("POST /api/resumable/{id}/decline", storeUnavailable)
	}

	if opts.NewOpponent != nil {
		mux.Handle("GET /ws/game", &gameHandler{opts: opts, auth: ah})
	} else {
		mux.HandleFunc("GET /ws/game", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error":   "engine_unavailable",
				"message": "対局エンジンを利用できません。",
			})
		})
	}

	return mux
}

// Run 은 서버를 띄우고 ctx가 취소될 때까지 막힌다.
func Run(ctx context.Context, addr string, opts Options) error {
	srv := &http.Server{
		Addr:    addr,
		Handler: Handler(opts),

		// 기본값이 없으면 느린 클라이언트 하나가 연결을 무한정 붙들 수 있다.
		//
		// WebSocket 대국에는 영향이 없다. 이건 헤더를 다 읽을 때까지의 시한이고,
		// 업그레이드 헤더는 즉시 오며 그 뒤로는 연결이 하이재킹되어 이 시한 밖이다.
		// 그래서 ReadTimeout(본문 전체)은 여전히 걸지 않는다 — 대국은 오래 열려 있다.
		ReadHeaderTimeout: 5 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", addr)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err

	case <-ctx.Done():
		log.Print("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// 응답 헤더를 이미 보냈으므로 상태코드를 바꿀 수 없다. 남기기만 한다.
		log.Printf("write json: %v", err)
	}
}
