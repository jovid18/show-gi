// Package server 는 HTTP 표면과 프로세스 수명을 담당한다.
//
// 핸들러가 대국 상태를 직접 읽지 않는다. 상태는 세션 goroutine이 소유하고,
// 핸들러는 채널로 물어본다 — 지름길을 내는 순간 잠금이 필요해진다.
//
// 화면에 나가는 cp는 전부 플레이어 관점이다. DB(先手 관점)·엔진과 캐시(수번 관점)에서
// 오는 값은 이 패키지 경계에서 뒤집는다 — 안 뒤집으면 색이 다른 두 판을 나란히 못 놓고,
// 한 줄을 넘겨 보는 동안 부호가 뒤집힌다(journal §33).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/auth"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/match"
	"github.com/jovid18/show-gi/apps/server/internal/metrics"
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

	// Mate·Search 는 값이라 대국 사이에 공유된다(NewOpponent·NewAnalyst 와 다른 점이다) —
	// 둘 다 풀·캐시가 본체이고 대국별 상태가 없다. nil이면 그 기능만 꺼진다.

	// Mate 는 詰み solver 다. nil이면 詰み 게이지가 꺼진다.
	Mate game.MateSearcher

	// StartSFEN·HumanColor·ObservePlies 는 대국을 어디서 시작할지 정한다. 비어 있으면
	// 평수 초기 국면·先手·기본 관측 구간이다. 지금은 테스트만 채운다 — 되짚기는 이 값이
	// 아니라 기록의 games.start_sfen 을 쓴다(review.go·whatif.go).
	StartSFEN    string
	HumanColor   shogi.Color
	ObservePlies int

	// Store 는 국면 캐시이자 대국 기록이다. nil이어도 대국은 된다 — 캐시가 없으면
	// 매번 계산하고 기록이 없으면 남지 않을 뿐이다. /healthz 의 db 로 드러낸다.
	Store *store.Store

	// Level 은 개입 임계치를 정하는 실력 구간이다. 기록에도 같이 남는다 —
	// 어느 임계치에서 걸린 개입인지를 모르면 나중에 상수를 흔들어 볼 수 없다.
	Level intervene.Level

	// Role 은 이 태스크가 어느 티어인가다. 손잡이는 SERVER_ROLE 이고 읽는 것은 cmd/api 다.
	// RoleAnalysis 면 /healthz 와 /metrics 만 남고 나머지가 503이 된다.
	//
	// 비어 있으면 RoleBoth 다. 티어를 안 가른 배포와 테스트가 지금까지와 같다.
	Role string

	// Search 는 가정 수순·手筋 힌트가 쓰는 엔진이다(whatif.go). nil이면 그 표면만 꺼지고
	// 되짚기는 그대로 돈다.
	Search Searcher

	// Quiz 는 되짚기 퀴즈의 생성기다. nil이면 문항이 안 만들어지고, 그때 되짚기의
	// 퀴즈 자리는 조용히 비어 있다 — 읽는 표면은 이 값과 무관하게 늘 있다(quiz.go).
	//
	// 총평과 달리 여기서만 만들어진다. 되짚기에서 만들면 그 탐색이 진행 중인 다른
	// 대국의 착수를 기다리게 한다(journal §53).
	Quiz *quiz.Builder

	// Google·SessionSecret 이 다 있어야 로그인이 켜진다(Store 도 필요하다 — auth.go).
	// 하나라도 비면 표면이 통째로 닫히고 익명 대국으로 남는다.
	Google        *auth.Google
	SessionSecret string

	// PublicOrigin 은 브라우저가 이 서버를 부르는 주소다. 비면 요청에서 되짚는다(auth.go).
	PublicOrigin string

	// Metrics 는 요청·엔진·세션의 숫자가 쌓이는 곳이다. nil 이면 계측만 꺼지고
	// 요청 로그는 그대로 남는다 — 테스트가 그 상태로 돈다.
	Metrics *metrics.Registry

	// Match 는 대인전에 필요한 한 벌이다. nil이면 그 표면이 통째로 닫힌다 —
	// 엔진도 DB도 안 쓰는 기능이라(internal/match) 그 둘과 따로 켜고 끈다.
	//
	// 밖에서 받는 이유는 수명이다. 방에서 시작된 대국은 연결이 아니라 서버가 사는 동안
	// 살아 있어야 하고(match 패키지 주석), Handler 에는 그런 ctx 가 없다.
	Match *Match
}

// Match 는 대인전의 방들과 그 곁장부다. 둘이 늘 같이 다녀서 한 타입이다 — 방이
// 대국을 시작하고(Hub), 그 대국이 남긴 번호를 곁장부가 안다(matchRecords).
type Match struct {
	hub     *match.Hub
	records *matchRecords
}

// NewMatch 는 대인전 한 벌을 만든다. Run 보다 먼저 부른다 — ctx 가 방에서 시작된 대국들의
// 수명이라 요청 ctx 로는 만들 수 없다.
//
// 기록기를 여기서 끼운다. internal/match 가 store 를 모르는 것은 internal/game 이
// 모르는 것과 같은 규약이고(server/recorder.go 가 그 다리다), 여기가 그 다리의 대인전 몫이다.
//
// st 가 nil이어도 방은 열린다 — 기록만 안 남는다.
func NewMatch(ctx context.Context, st *store.Store, level intervene.Level) *Match {
	records := newMatchRecords(st, level)
	return &Match{
		hub:     match.NewHub(ctx, match.HubConfig{NewRecorders: records.new}),
		records: records,
	}
}

// analyzerOrNil 은 되짚기가 「분석 중인가」를 물을 상대다. 대인전이 꺼진 배포에서는
// Options.Match 자체가 nil이라 수신자까지 nil 을 받는다.
func (m *Match) analyzerOrNil() *matchAnalyzer {
	if m == nil {
		return nil
	}
	return m.records.analyzer
}

// AnalyzeWith 는 판이 끝난 뒤 평가치와 실력 추정치를 채울 분석기를 단다(matchAnalyzer).
//
// 만드는 자리와 다는 자리가 갈려 있다. 대인전은 엔진보다 먼저 서고(cmd/api 의 「엔진
// 앞에 둔다」) 분석기는 엔진이 있어야 만들 수 있다 — 순서가 그 사실을 그대로 말한다.
//
// 기동 중에 한 번만 부른다. Run 뒤에 부르면 곁장부 goroutine 과 경합한다.
func (m *Match) AnalyzeWith(
	ctx context.Context, st *store.Store, newAnalyst func() game.Analyst, reg *metrics.Registry,
	workers int,
) {
	m.records.analyzer = newMatchAnalyzer(ctx, st, newAnalyst, reg, workers)
}

// 티어 이름. 값을 여기 두는 것은 라우팅이 이 이름으로 갈리기 때문이고, 손잡이(SERVER_ROLE)를
// 읽어 넘기는 것은 cmd/api 다.
const (
	RoleBoth        = "both"
	RoleInteractive = "interactive"
	RoleAnalysis    = "analysis"
)

// roleOf 는 /healthz 에 나갈 티어 이름이다. 비어 있으면 both 다.
//
// 이 값이 없으면 티어를 밖에서 구별할 방법이 없다. 대상 그룹이 분석 태스크를 물고
// 있어도 /healthz 는 200 이라 계속 붙어 있고, 배포 워크플로의 확인도 그 태스크가
// 답할 수 있다(.github/workflows/images.yml).
func roleOf(opts Options) string {
	if opts.Role == "" {
		return RoleBoth
	}
	return opts.Role
}

// Handler 는 라우팅만 조립한다. 테스트가 서버를 띄우지 않고 이걸 그대로 쓴다.
func Handler(opts Options) http.Handler {
	mux := http.NewServeMux()

	// 로드밸런서와 배포 워크플로가 보는 곳. 인증 뒤에 두지 않는다.
	//
	// 엔진이 없어도 200이다. 여기서 실패하면 ECS가 재시작을 반복해 사이트 전체가
	// 내려간다. 대신 engine 필드로 드러낸다 — 배포 워크플로가 그걸 확인한다(§11).
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
			"ok": true, "engine": engineReady, "db": dbReady, "role": roleOf(opts),
		})
	})

	// 지표. 밖에서 안 닿는다 — Caddy 가 /ws·/api·/healthz 만 프록시하므로 이 경로는
	// 태스크 안에서만 열린다(apps/web/Caddyfile). 프로덕션에서 실제로 보는 것은
	// stdout 으로 나가는 EMF 쪽이고(internal/metrics), 여기는 로컬과 컨테이너 안에서
	// 같은 숫자를 라벨까지 붙여 읽는 자리다.
	if opts.Metrics != nil {
		mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
			if err := opts.Metrics.WriteText(w); err != nil {
				slog.WarnContext(r.Context(), "cannot write /metrics", "err", err)
			}
		})
	}

	// 분석 티어는 여기서 끝난다. 사람이 쓰는 표면을 아예 안 세운다.
	//
	// 막는 이유가 둘이다. 방이 짝지은 프로세스의 메모리에 서므로(journal §98) 이 티어가
	// 짝을 지으면 그 방을 아무도 못 열고 로그에 아무것도 안 남는다. 대국·검토·가정
	// 수순은 깨지지 않지만 이 박스의 엔진을 분석보다 높은 우선순위로 가져간다
	// (usi.priorityOf) — 그러면 티어를 가른 값이 없어진다.
	//
	// 404가 아니라 503이다. 없애면 「배포가 낡았다」와 구별되지 않는다. 확인하는 자리
	// 둘은 위에 이미 섰다 — 그것까지 막으면 ECS 가 이 태스크를 계속 죽인다.
	if opts.Role == RoleAnalysis {
		var said sync.Once
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			// 원인을 한 줄로 남긴다. 이 503은 코드가 깨진 것이 아니라 대상 그룹 설정이
			// 틀렸다는 뜻인데, 요청 로그와 show-gi-5xx 알람은 그 둘을 구별하지 못한다.
			//
			// 한 번만 남긴다. 어느 경로였는지는 observe 가 요청마다 이미 적고, 여기까지
			// 매번 적으면 스캐너 하나가 로그를 채운다 — 그 로그가 곧 요금이다.
			said.Do(func() {
				slog.WarnContext(r.Context(), "a request reached the analysis tier",
					"hint", "the target group should not send people here")
			})
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error":   "not_served_here",
				"message": "このサーバーでは対局を受け付けていません。",
			})
		})
		return observe(opts.Metrics, mux)
	}

	// 로그인. 켜지지 않아도 /api/me 는 있다 — 화면이 「로그인이라는 것이 이 배포에
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

	// 시작 화면이 고를 진형 목록. DB도 엔진도 로그인도 필요 없다 — 상수 목록이라
	// (internal/book) 무엇이 꺼져 있어도 이 자리는 답한다.
	mux.HandleFunc("GET /api/openings", openings)

	// 手合割 목록. 위와 같은 이유로 아무것에도 안 매여 있다(internal/handicap).
	mux.HandleFunc("GET /api/handicaps", handicaps)

	// 끝난 판을 되짚는 표면(review.go). DB에 매여 있고 엔진과 무관하다 — 가정 수순만
	// 엔진이 필요해 그 한 경로가 따로 503이 된다(README 라우트 표). 화면은 /healthz 의
	// engine 을 보고 미리 그 자리를 닫는다.
	storeUnavailable := func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":   "store_unavailable",
			"message": "対局の記録を利用できません。",
		})
	}
	if opts.Store != nil {
		// 마이페이지. 판 하나가 아니라 사람 하나를 읽는다(profile.go).
		mux.HandleFunc("GET /api/me/profile", (&profileHandler{store: opts.Store, auth: ah}).get)

		rev := &reviewHandler{store: opts.Store, auth: ah, level: opts.Level, analyzer: opts.Match.analyzerOrNil()}
		mux.HandleFunc("GET /api/games", rev.list)
		mux.HandleFunc("GET /api/games/{id}", rev.detail)
		// 총평은 기보와 따로 간다 — 화면이 판을 먼저 그린다(review.go summary).
		mux.HandleFunc("GET /api/games/{id}/summary", rev.summary)

		// 퀴즈(quiz.go). 엔진과 무관하다 — 문항은 판이 끝나는 자리에서 이미 만들어져
		// 있고 채점은 저장된 트리를 읽는 일뿐이다. 되짚기와 같은 문으로 기록을 읽는다.
		qz := &quizHandler{review: rev}
		mux.HandleFunc("GET /api/games/{id}/quiz", qz.get)
		mux.HandleFunc("POST /api/games/{id}/quiz/mate", qz.mate)
		mux.HandleFunc("POST /api/games/{id}/quiz/best", qz.best)

		// 이어하기(resume.go). 엔진과 무관하다 — 여기는 묻고 답하는 자리뿐이고,
		// 실제로 이어 두는 것은 /ws/game?resume= 이라 그쪽이 엔진에 매여 있다.
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
		mux.HandleFunc("GET /api/me/profile", storeUnavailable)
		mux.HandleFunc("GET /api/games", storeUnavailable)
		mux.HandleFunc("GET /api/games/{id}", storeUnavailable)
		mux.HandleFunc("GET /api/games/{id}/summary", storeUnavailable)
		mux.HandleFunc("GET /api/games/{id}/quiz", storeUnavailable)
		mux.HandleFunc("POST /api/games/{id}/quiz/mate", storeUnavailable)
		mux.HandleFunc("POST /api/games/{id}/quiz/best", storeUnavailable)
		mux.HandleFunc("POST /api/games/{id}/whatif", storeUnavailable)
		// 여기는 503이 아니라 「없다」다. 기록이 없는 배포에는 이어할 판이 있을 수가
		// 없고, 첫 화면이 늘 부르는 자리라 실패로 답하면 물음 카드가 아니라 오류가 뜬다.
		mux.HandleFunc("GET /api/resumable", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"game": nil})
		})
		mux.HandleFunc("POST /api/resumable/{id}/decline", storeUnavailable)
	}

	// 검토(explore.go). DB 블록 밖이다 — 뿌리가 手合割 표라 기록이 없어도 경로가 서고,
	// positions 는 있으면 캐시로 쓴다(없으면 답은 같고 매번 다시 잰다).
	//
	// 로그인이 필요 없다(journal §100). 엔진 하나만 있으면 서는 표면이라 DB 없는 배포에서도
	// 그대로 돌고, 익명이 가져갈 수 있는 것은 슬롯 하나로 묶여 있다(exploreSlots).
	// 저장한 국면만 로그인 뒤에 남는다 — 그쪽은 사람마다 다른 기록이라 자격이 필요하다.
	if opts.Search != nil {
		mux.HandleFunc("POST /api/explore", newExploreHandler(opts.Store, opts.Search).play)
	} else {
		mux.HandleFunc("POST /api/explore", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error":   "engine_unavailable",
				"message": "対局エンジンを利用できません。",
			})
		})
	}

	// 검토에서 저장한 국면(explore_snapshots.go). 엔진을 안 탄다 — 기록에 넣고 꺼내는
	// 일뿐이고, 불러오기는 화면이 주소를 고쳐 위의 /api/explore 로 다시 묻는 것이다.
	//
	// 위 블록과 달리 DB에 매여 있다. 검토는 기록이 없어도 서지만 저장은 그럴 수가 없고,
	// 그때 401(로그인)로 답하면 로그인한 뒤에도 안 되는 자리를 가리킨다 — 그래서 503이다.
	if opts.Store != nil {
		sn := &exploreSnapshotHandler{store: opts.Store, auth: ah}
		mux.HandleFunc("GET /api/explore/snapshots", sn.list)
		mux.HandleFunc("POST /api/explore/snapshots", sn.save)
		mux.HandleFunc("PATCH /api/explore/snapshots/{id}", sn.rename)
		mux.HandleFunc("DELETE /api/explore/snapshots/{id}", sn.remove)
	} else {
		snapshotsUnavailable := func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error":   "store_unavailable",
				"message": "局面の保存を利用できません。",
			})
		}
		mux.HandleFunc("GET /api/explore/snapshots", snapshotsUnavailable)
		mux.HandleFunc("POST /api/explore/snapshots", snapshotsUnavailable)
		mux.HandleFunc("PATCH /api/explore/snapshots/{id}", snapshotsUnavailable)
		mux.HandleFunc("DELETE /api/explore/snapshots/{id}", snapshotsUnavailable)
	}

	// 대인전(match.go · ws_match.go). 엔진도 DB도 안 탄다 — 룰 엔진과 시계뿐이라
	// 다른 무엇이 꺼져 있어도 이 셋은 답한다. 기록만 DB 유무에 걸린다.
	//
	// 셋 다 로그인이 필요하다. 익명은 서로 구별할 수단이 없어서 정원 2명이라는 규칙이
	// 성립하지 않는다(internal/match 의 Room).
	if opts.Match != nil {
		mh := &matchHandler{hub: opts.Match.hub, auth: ah}
		mux.HandleFunc("POST /api/rooms", mh.create)
		mux.HandleFunc("GET /api/rooms/{id}", mh.get)
		mux.Handle("GET /ws/match", &matchHandlerWS{
			hub: opts.Match.hub, auth: ah, records: opts.Match.records, metrics: opts.Metrics,
		})

		// 대기열(queue.go). 위 셋과 달리 DB에 매여 있다 — 줄이 표에 있고, 그래야 모든
		// 인스턴스가 같은 줄을 본다(journal §92). 그래서 기록이 없는 배포에서는 방은
		// 열리는데 줄은 안 선다.
		if opts.Store != nil {
			qh := &queueHandler{hub: opts.Match.hub, store: opts.Store, auth: ah, metrics: opts.Metrics}
			mux.HandleFunc("POST /api/queue", qh.join)
			mux.HandleFunc("DELETE /api/queue", qh.leave)
		} else {
			queueOff := func(w http.ResponseWriter, _ *http.Request) { queueUnavailable(w) }
			mux.HandleFunc("POST /api/queue", queueOff)
			mux.HandleFunc("DELETE /api/queue", queueOff)
		}
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

	// 감싸는 것이 mux 밖이어야 한다. route 라벨로 쓰는 r.Pattern 을 ServeMux 가 채우므로
	// 안쪽에 두면 그 값을 읽을 수 없다(observe.go).
	return observe(opts.Metrics, mux)
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
