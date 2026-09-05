// Command api 는 show-gi의 HTTP/WebSocket 서버다.
//
// 여기에는 플래그와 프로세스 수명만 둔다. 로직은 internal 아래에 있다.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/archive"
	"github.com/jovid18/show-gi/apps/server/internal/auth"
	"github.com/jovid18/show-gi/apps/server/internal/boardread"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/kifunorm"
	"github.com/jovid18/show-gi/apps/server/internal/metrics"
	"github.com/jovid18/show-gi/apps/server/internal/quiz"
	"github.com/jovid18/show-gi/apps/server/internal/server"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 엔진 풀 크기의 기본값.
//
// 빌리는 자리가 여섯이다 — ① 상대 수 ② 개입 판정 ③ 가정 수순 ④ 검토 ⑤ 부르는 힌트
// ⑥ 되짚기 퀴즈의 「최선수는?」. 詰み 탐색은 여기 없다 — 다른 바이너리라 풀이 따로다
// (defaultMatePoolSize).
//
// 3은 그 여섯을 다 덮는 값이 아니다. 퀴즈가 판이 끝날 때 십여 초를 쓰므로(journal §53)
// 두 판이 동시에 끝나면 진행 중인 대국의 착수가 그만큼 뒤로 밀린다. 그래도 안 올린 것은
// 그 지연이 대국을 멈추지 않기 때문이다(mate 풀과 달리 여기는 원래도 여럿이 다툰다).
// 올릴 자리는 태스크 정의의 ENGINE_POOL_SIZE 다.
//
// 프로덕션은 2다. 코어가 2개뿐이고, 슬롯만 늘리면 대기가 탐색 시간으로 옮겨갈 뿐인 것을
// 4로 재 봤다(journal §110). 올리는 것은 코어를 늘릴 때 같이 한다.
const defaultEnginePoolSize = 3

// defaultMatePoolSize 는 詰将棋 solver 의 기본 개수다. 2인 이유는 startMateEngines 에 있다 —
// 퀴즈 생성이 하나를 오래 잡으므로 대국 쪽에 한 자리를 남긴다.
const defaultMatePoolSize = 2

// defaultMatePlies 는 詰み 탐색의 手数 한계다. 11인 근거는 01-core.md §2 —
// 비용이 7·9·11에서 평평해서 11이 詰み을 하나 더 찾고도 값이 같다.
const defaultMatePlies = 11

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	// 로그를 먼저 세운다. 이 아래의 기동 로그부터 구조화된 한 줄로 나가야 하고,
	// 그중 「무엇이 꺼진 채로 떴나」가 장애를 가르는 첫 정보다.
	setupLogging()

	// SIGINT/SIGTERM이 오면 ctx가 취소되고, 그걸 받아 진행 중인 요청을 마저 끝낸다.
	// 대국 세션과 엔진 프로세스가 붙어 있으므로 여기서 정리 순서가 갈린다.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 실력 추정이 붙기 전까지는 제일 너그러운 쪽이 기본이다 — 학습 앱에서 과잉 개입은
	// 잔소리가 되고, 이 제품이 피하려는 바로 그것이다(intervene.Beginner 주석).
	//
	// 판정과 기록이 같은 값을 봐야 한다. 갈리면 「어느 임계치에서 걸린 개입인가」가
	// 기록에서 틀리고, 그 위에서 상수를 흔들어 보게 된다.
	opts := server.Options{Level: intervene.Beginner}

	// 티어. 큐를 집는가와 사람을 받는가를 여기서 한 번 읽는다 — 두 자리에서 각각
	// os.Getenv 하면 잘못 적은 값이 한쪽에서만 경고를 내고 다른 쪽은 조용히 갈린다.
	role := analysisRole()
	opts.Role = role

	// 지표. 서버·엔진 풀·탐색이 같은 레지스트리를 쓴다 — 무엇을 재는지가
	// metrics.New 한 자리에 다 있어야 새 지표를 늘릴 때 EMF 쪽을 같이 보게 된다.
	reg := metrics.New("api", os.Getenv("ENVIRONMENT"))
	opts.Metrics = reg
	stopMetrics := startEmitter(reg)

	if st := openStore(ctx); st != nil {
		defer st.Close()
		opts.Store = st
	}

	// 취해 온 기보의 정규화 창구. 키가 없으면 nil 이고, 그때 결정적 파서로 읽히는
	// 기보만 들어온다 — Google 로그인이 값 하나만 비어도 표면째 닫히는 것과 달리
	// 여기는 폴백 한 겹만 꺼진다(internal/kifunorm).
	opts.KifuNorm = kifunorm.New(os.Getenv("OPENAI_API_KEY"), os.Getenv("OPENAI_MODEL"))
	if opts.KifuNorm == nil {
		slog.Info("kifu: no OPENAI_API_KEY, unreadable formats will be refused")
	}

	// 판이 찍힌 그림에서 국면을 읽는 창구(internal/boardread). 키는 위와 같은 것을 쓰고
	// 모델만 갈라 둔다 — 글자를 옮기는 일과 81칸을 읽는 일에 같은 모델을 댈 이유가 없다.
	opts.BoardRead = boardread.New(os.Getenv("OPENAI_API_KEY"), os.Getenv("BOARDREAD_MODEL"))
	if opts.BoardRead == nil {
		slog.Info("position: no OPENAI_API_KEY, reading a position from an image is off")
	}

	// 판독을 재는 그림과 라벨을 모아 두는 폴더. 로컬에서만 켠다 — 프로덕션은 이 값을
	// 안 주고, 없으면 그림도 안 남고 라벨 뿌리도 안 선다(apps/server/README.md).
	opts.BoardImageDir = os.Getenv("SHOWGI_BOARD_IMAGE_DIR")
	if opts.BoardImageDir != "" {
		slog.Info("position: collecting board images", "dir", opts.BoardImageDir)
	}

	opts.Google = startAuth()
	opts.SessionSecret = os.Getenv("SESSION_SECRET")
	opts.PublicOrigin = os.Getenv("PUBLIC_ORIGIN")

	// 대인전. 엔진 앞에 둔다 — 엔진이 없어도 사람끼리는 둘 수 있다.
	//
	// ctx 는 여기서 준다. 대국의 수명은 연결이 아니라 이 프로세스다.
	// 핸들러의 r.Context() 에 매달면 한쪽이 탭을 닫는 순간 시계까지 멈춰서, 남은
	// 사람의 대국이 끝나지도 이어지지도 못한다(server.NewMatch).
	opts.Match = server.NewMatch(ctx, opts.Store, opts.Level)

	if pool := startEngines(); pool != nil {
		defer pool.Close()
		pool.Observe(reg.Pool(metrics.PoolSearch))

		// 詰み solver 는 다른 바이너리라 따로 띄운다(02-architecture.md §3).
		// 없어도 대국과 승률 낙폭 판정은 그대로 돌고, 종반 판정만 빠진다.
		matePool := startMateEngines()
		if matePool != nil {
			defer matePool.Close()
			matePool.Observe(reg.Pool(metrics.PoolMate))
		}

		// 모든 탐색이 데이터가 된다. 엔진을 부르는 자리가 여섯인데(archive 의 목록) 기록을
		// 그 여섯에 흩뿌리면 반드시 하나가 빠진다 — 그래서 풀을 한 겹 감싸고 여섯이 같은
		// 하나를 받는다(internal/archive).
		//
		// 가정 수순이 이 구조에서 특히 값을 한다. 둬 본 국면은 플레이어가 실제로 그 수를
		// 두면 바로 다시 물어볼 국면이라 캐시 적중이 저절로 따라온다(journal §37).
		//
		// DB가 없으면 그대로 통과시킨다. 인터페이스에 nil 포인터를 넣지 않는 것은
		// 아래 mate solver 와 같은 이유다.
		var into archive.Store
		if opts.Store != nil {
			into = opts.Store
		}
		searcher := archive.Wrap(pool, into)
		// 계측도 같은 자리에 붙는다. 엔진을 부르는 여섯 자리가 다 여기를 지나므로
		// 하나만 달면 되고, 캐시가 답한 것과 엔진을 부른 것이 여기서 갈린다.
		searcher.Observe(reg.Search())
		// 떠 있는 기록이 끝나기를 기다린다. 안 기다리면 마지막 수의 분석이 버려진다.
		// 등록 순서가 곧 종료 순서다(LIFO). 이 줄이 위 defer st.Close() 보다 뒤라서 기록이 다 흘러간 뒤 DB가 닫힌다.
		defer searcher.Wait()

		// 詰み 탐색도 같은 겹을 지난다. 탐색부와 표를 갈라 둔 이유는 017 의 DDL 에 있고,
		// 여기서 값을 하는 것은 빌리는 넷이 같은 질문을 겹쳐서 한다는 것이다 — 게이지와
		// 종반 판정이 같은 국면을 한 手 간격으로, 퀴즈가 판이 끝난 뒤 그 전부를 다시
		// 묻는다(journal §110).
		//
		// 인터페이스에 nil 포인터를 넣지 않는다. *usi.Pool 이 nil이어도 인터페이스 값
		// 자체는 non-nil이 되어 == nil 검사를 통과하고, 그 다음 줄에서 죽는다.
		var mate game.MateSearcher
		if matePool != nil {
			var mateInto archive.MateStore
			if opts.Store != nil {
				mateInto = opts.Store
			}
			wrapped := archive.WrapMate(matePool, mateInto, matePlies())
			wrapped.Observe(reg.MateSearch())
			defer wrapped.Wait()
			mate = wrapped
		}

		opts.NewOpponent = func() game.Opponent {
			return game.NewAdaptiveOpponent(searcher, engineDepth(), opponentBand())
		}
		opts.NewAnalyst = func() game.Analyst {
			return game.NewEngineAnalyst(searcher, mate, opts.Level)
		}

		// 이 풀을 빌리는 자리가 넷이다 — 종반 판정, 詰み 게이지, 퀴즈의 詰み 트리,
		// 대인전 사후 분석. 앞의 둘은 시간상 겹치지 않지만(판정은 사람의 수 직후,
		// 게이지는 상대의 수 직후) 퀴즈는 판이 끝나는 자리에서 수십 초를 잡는다 —
		// 그래서 하나로는 모자라다(matePoolSize).
		opts.Mate = mate

		// 가정 수순도 같은 풀이다(internal/server/whatif.go). 대국과 자리를 다투지만,
		// 겹치면 풀이 순서대로 빌려주고 그만큼 기다린다 — 지연은 여기서 허용된 비용이다.
		//
		// 검토(internal/server/explore.go)도 이 풀이다. 저쪽은 뿌리가 0手目라 아무
		// 국면이나 물을 수 있어서 「그만큼 기다린다」로 두면 대국의 착수가 그 뒤에 큐에 선다 —
		// 그래서 그 표면만 자기 슬롯을 하나 갖는다(exploreSlots, journal §85).
		opts.Search = searcher

		// 되짚기 퀴즈의 생성기. 엔진 둘을 다 쓴다 — 詰み 문항은 solver, 「최선수는?」은
		// 탐색부다. 탐색부 쪽을 감싼 것으로 넘기는 이유는 그 결과도 positions 에 쌓여야
		// 하기 때문이다(§37) — 퀴즈가 재는 국면은 되짚기에서 가정 수순이 곧 다시 물어볼 자리다.
		//
		// mate 가 nil이면 「최선수는?」 문항만 나온다 — 없어지는 쪽이 詰み 문항이다
		// (Build 가 b.mate != nil 로 가른다). searcher 가 없으면 이 자리 자체가 없다.
		opts.Quiz = quiz.NewBuilder(mate, searcher, engineDepth())

		// 사후 분석. 갈래가 둘이다 — 대인전은 되짚기의 평가치와 두 사람의 실력 추정치를
		// 채우고(journal §105), 취해 온 기보는 거기에 悪手 줄과 문항까지 만든다(§126).
		// 착수 경로는 그래도 엔진을 안 지난다 — 미리 재는 것이 논블로킹이라 착수를 막지 않는다.
		//
		// 퀴즈 생성기보다 뒤에 선다. 취해 온 판의 문항을 이 분석기가 만들기 때문이고,
		// Run 보다는 앞이라 곁장부 goroutine 과 경합하지 않는다.
		opts.Match.AnalyzeWith(ctx, server.AnalysisDeps{
			Store:      opts.Store,
			NewAnalyst: opts.NewAnalyst,
			Metrics:    opts.Metrics,
			Workers:    analysisWorkers(pool.Size(), role),
			Quiz:       opts.Quiz,
			Level:      opts.Level,
		})
	}

	err := server.Run(ctx, *addr, opts)
	stopMetrics()
	if err != nil {
		// log.Fatal 이 아니다. 그쪽은 slog 를 info 로 지나가므로 LOG_LEVEL 을 올린
		// 배포에서는 「왜 죽었나」가 로그에 아예 안 남는다 — 빈 로그로 재시작을 반복한다.
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

// setupLogging 은 로그를 구조화한다. 손잡이는 LOG_LEVEL·LOG_FORMAT 이다.
//
// slog.SetDefault 가 log 패키지의 출력까지 이 핸들러로 돌린다. 그래서 서버에 남아 있는
// log.Printf 도 같은 JSON 한 줄로 나가고, 그 백여 곳을 옮겨 적지 않아도 된다 —
// 옮겨 적어서 얻는 것은 필드뿐이고, 그것이 필요한 자리만 옮긴다.
//
// stderr 인 것은 EMF 와 갈라 두려는 것이다. 지표는 stdout 으로 나가고 둘 다 같은 로그
// 그룹에 들어가지만, 사람이 로컬에서 볼 때는 한쪽만 보는 편이 낫다.
func setupLogging() {
	level, badLevel := logLevel()
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler = slog.NewJSONHandler(os.Stderr, opts)
	if os.Getenv("LOG_FORMAT") == "text" {
		h = slog.NewTextHandler(os.Stderr, opts)
	}
	// 요청 ID 를 ctx 에서 꺼내 모든 줄에 붙인다. 핸들러가 직접 넘기지 않아도 붙는다.
	slog.SetDefault(slog.New(server.LogHandler(h)))

	// 로거가 선 뒤에 알린다. 이 파일의 다른 환경변수도 다 그렇게 하고, 조용히 기본값으로
	// 떨어지면 LOG_LEVEL=warning 같은 오타가 「설정 안 함」과 구별되지 않는다.
	if badLevel != "" {
		slog.Warn("bad LOG_LEVEL", "value", badLevel, "using", level)
	}
}

// logLevel 은 남길 로그의 급이다. 두 번째 값은 못 읽은 원문이고, 읽었으면 빈 문자열이다.
//
// 기본은 info 다 — debug 는 요청 한 줄에 헬스체크까지 들어와 로그의 대부분이 그것이 된다
// (server.levelFor). 위로 올리지도 않는다: 아직 slog 로 안 옮긴 log.Print* 가 전부
// info 라 warn 이면 그것들이 통째로 사라진다(apps/server/README.md 의 그 경고).
func logLevel() (slog.Level, string) {
	raw := os.Getenv("LOG_LEVEL")
	if raw == "" {
		return slog.LevelInfo, ""
	}
	var l slog.Level
	if err := l.UnmarshalText([]byte(raw)); err != nil {
		return slog.LevelInfo, raw
	}
	return l, ""
}

// startEmitter 는 지표를 CloudWatch 로 내보내기 시작한다.
//
// ENVIRONMENT 가 비면 아무것도 안 낸다. 로컬에서 EMF 줄이 stdout 에 섞이는 것을 막는
// 것이 절반이고, 나머지 절반은 요금이다 — EMF 는 지표를 자동으로 만들어서, 켠 줄도
// 모르는 채로 커스텀 지표가 쌓이는 쪽이 나쁘다. 그때도 /metrics 는 그대로 있다.
// 돌려주는 함수는 마지막 회차를 내고 돌아온다. main 이 끝나기 직전에 부른다 —
// defer 로는 안 되는데, 리스너 오류가 log.Fatal 로 끝나면 defer 가 안 돈다.
func startEmitter(reg *metrics.Registry) func() {
	if os.Getenv("ENVIRONMENT") == "" {
		slog.Info("metrics stay local", "reason", "ENVIRONMENT is not set", "surface", "/metrics")
		return func() {}
	}

	// 수명을 프로세스에 맞춘다. main 의 ctx 가 아닌 것은, 리스너가 오류로 죽는 경우
	// 그 ctx 가 취소되지 않아 마지막 회차를 기다리다 프로세스가 멈추기 때문이다.
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		metrics.NewEmitter(reg, os.Stdout).Run(ctx, metrics.DefaultInterval)
	}()
	// 끝나면서 한 줄을 더 낸다. 안 내면 종료 직전 회차가 사라지고, 배포마다 그 구간이
	// 비어 그래프에 규칙적인 구멍이 생긴다.
	return func() {
		stop()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
}

// analysisWorkers 는 사후 분석을 동시에 몇 갈래로 돌릴지다. 손잡이는 ANALYSIS_WORKERS 다.
//
// 기본이 풀 크기다. 분석은 대국과 같은 풀에서 엔진을 빌리는데(archive.Wrap 하나를 여섯이
// 나눠 쓴다), 그래도 다 가져가도 되는 이유는 풀이 우선순위로 빌려주기 때문이다 —
// 사람이 기다리는 요청이 분석보다 먼저 받는다(usi.priorityOf).
//
// 하나 적게 두는 쪽을 먼저 지었다가 걷었다. 예약이 아니라 상한이라, 라이브 대국이 둘이면
// 남긴 하나를 서로 기다린다 — 지연은 안 막고 처리량만 깎았다(journal §106).
//
// 풀이 커지면 이 값도 같이 커진다. 그것이 이 함수가 상수가 아닌 이유다: 워커가 하나면
// vCPU 를 올려도 사후 분석 층은 그대로였다(journal §106).
func analysisWorkers(poolSize int, role string) int {
	if role == server.RoleInteractive {
		// 집는 쪽을 안 띄운다. 手는 그대로 표에 세워지고 분석 티어가 집는다.
		slog.Info("match analysis ready", "role", role, "workers", 0, "pool", poolSize)
		return 0
	}
	n := max(poolSize, 1)
	if v := os.Getenv("ANALYSIS_WORKERS"); v != "" {
		got, err := strconv.Atoi(v)
		if err != nil || got < 1 {
			slog.Warn("bad ANALYSIS_WORKERS", "value", v, "using", n)
		} else {
			n = got
		}
	}
	slog.Info("match analysis ready", "role", role, "workers", n, "pool", poolSize)
	return n
}

// analysisRole 은 이 프로세스가 어느 티어인가다. 정하는 것이 둘이다 — 큐를 집는가와
// 사람을 받는가. 손잡이는 SERVER_ROLE 이다.
//
// ROLE 이 아닌 이유는 그 이름이 이미 남의 것이기 때문이다 — .github/workflows 가
// IAM 역할 ARN 을 그 이름으로 들고 있고, 밖에서 들어온 값이 하필 아래 셋 중 하나면
// 티어가 조용히 갈린다. 다른 손잡이들과 같은 방식으로 주어를 붙였다(ENGINE_·ANALYSIS_).
//
//	interactive  집지 않는다. 사람을 받고 手를 큐에 세우기만 한다
//	analysis     집는다. /healthz·/metrics 만 세운다(server.Options.Role)
//	both         집고 받는다. 태스크가 하나인 배포의 모양이고 기본이다
//
// analysis 가 나머지를 404 가 아니라 503 으로 답하는 이유는 server.Handler 에 있다.
//
// 상호작용 티어를 여러 대로 올리는 것은 이 손잡이가 아니다. 방이 메모리에 서므로
// (journal §98) 그쪽은 방을 프로세스 밖으로 내린 뒤다.
func analysisRole() string {
	switch v := os.Getenv("SERVER_ROLE"); v {
	case "":
		return server.RoleBoth
	case server.RoleBoth, server.RoleInteractive, server.RoleAnalysis:
		return v
	default:
		slog.Warn("bad SERVER_ROLE", "value", v, "using", server.RoleBoth)
		return server.RoleBoth
	}
}

// startAuth 는 Google 로그인을 켠다. 키가 없으면 nil 이고 익명 대국으로 남는다.
//
// 없다고 프로세스를 죽이지 않는다. 엔진·DB 와 같은 판단이고, 여기서는 특히 그렇다 —
// 로그인은 대국의 전제가 아니라 그 판이 누구 것으로 남느냐일 뿐이다. 어느 쪽으로 돌고
// 있는지는 기동 로그가 한 줄로 말한다.
func startAuth() *auth.Google {
	g := auth.NewGoogle(os.Getenv("GOOGLE_CLIENT_ID"), os.Getenv("GOOGLE_CLIENT_SECRET"))
	if g == nil {
		slog.Warn("google sign-in is off", "reason", "GOOGLE_CLIENT_ID/SECRET are not set")
		return nil
	}
	if os.Getenv("SESSION_SECRET") == "" {
		// 서명 키가 없으면 쿠키를 위조할 수 있다. 그건 로그인이 없는 것보다 나쁘다.
		slog.Warn("google sign-in is off", "reason", "SESSION_SECRET is not set")
		return nil
	}
	slog.Info("google sign-in ready")
	return g
}

// openStore 는 DB에 붙는다. 실패하면 nil을 돌려주고 서버는 그냥 뜬다.
//
// 엔진과 같은 판단이다 — DB가 없다고 프로세스를 죽이지 않는다. 국면 캐시가 없으면
// 매번 계산할 뿐이지 대국은 된다. 죽이면 ECS 재시작 루프로 사이트 전체가 내려간다.
func openStore(ctx context.Context) *store.Store {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		slog.Warn("database is off", "reason", "DATABASE_URL is not set")
		return nil
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		slog.Error("database is off", "err", err)
		return nil
	}
	slog.Info("database ready")
	return st
}

// startEngines 는 USI 엔진 풀을 띄운다. 실패하면 nil을 돌려주고 서버는 그냥 뜬다.
//
// 엔진이 없다고 프로세스를 죽이지 않는다. 죽이면 ECS가 재시작을 반복하고
// /healthz 까지 같이 사라져 사이트 전체가 내려간다. 대국만 막고 나머지는 살린다.
func startEngines() *usi.Pool {
	cmd := os.Getenv("ENGINE_CMD")
	if cmd == "" {
		slog.Warn("games are disabled", "reason", "ENGINE_CMD is not set")
		return nil
	}

	size := defaultEnginePoolSize
	if v := os.Getenv("ENGINE_POOL_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			slog.Warn("bad ENGINE_POOL_SIZE", "value", v, "using", size)
		} else {
			size = n
		}
	}

	opts := engineOptions()
	pool, err := usi.NewPool(size, cmd, opts)
	if err != nil {
		slog.Error("games are disabled", "cmd", cmd, "size", size, "err", err)
		return nil
	}
	slog.Info("engine pool ready", "cmd", cmd, "size", size, "options", opts)
	return pool
}

// startMateEngines 는 詰将棋 solver 풀을 띄운다. 없으면 nil.
//
// 탐색 한계는 手数(DepthLimit) 로 준다. 시간이 아니라 수로 자르는 이유는 다른 탐색과
// 같다 — 같은 국면이 같은 답을 줘야 캐시할 수 있다. 11인 것은 실측 결과다(06-status.md).
func startMateEngines() *usi.Pool {
	cmd := os.Getenv("ENGINE_MATE_CMD")
	if cmd == "" {
		slog.Warn("endgame judgment and the mate gauge are disabled", "reason", "ENGINE_MATE_CMD is not set")
		return nil
	}
	// 소비자가 넷이다 — 종반 판정, 詰み 게이지, 되짚기 퀴즈의 詰み 트리, 대인전 사후
	// 분석. 앞 둘은 시간상 안 겹치지만(판정은 사람의 수 직후, 게이지는 상대의 수 직후)
	// 세 번째가 판이 끝나는 자리에서 수십 초 동안 풀을 잡는다 — 그래서 기본이 2다
	// (journal §53). 넷째는 게이트 없이 手마다 부른다(journal §110).
	//
	// 「대국 쪽에 늘 한 자리가 남는다」는 아니다. 두 판이 동시에 끝나면 둘이 두 자리를 다
	// 잡는다. 탐색 하나마다 빌리고 돌려주므로(Pool.Do) 굶는 것이 아니라 큐에 서는 것이고,
	// 대국 쪽은 그것을 지연으로 겪는다.
	pool, err := usi.NewPool(matePoolSize(), cmd, map[string]string{
		"USI_Hash": envOr("ENGINE_HASH_MB", "128"),
		"Threads":  envOr("ENGINE_THREADS", "1"),
		// 여기서 env 를 직접 읽지 않는다. 같은 값을 캐시도 읽으므로(archive.WrapMate)
		// 파서가 둘이면 한쪽만 고쳐졌을 때 캐시가 얕은 한계의 답을 깊은 것으로 읽는다.
		"DepthLimit": strconv.Itoa(matePlies()),
	})
	if err != nil {
		slog.Error("endgame judgment is disabled", "err", err)
		return nil
	}
	slog.Info("mate solver ready", "cmd", cmd, "size", pool.Size())
	return pool
}

// matePoolSize 는 詰将棋 solver 를 몇 개 띄우나다. 손잡이는 ENGINE_MATE_POOL_SIZE 다.
//
// 탐색부의 ENGINE_POOL_SIZE 와 갈라 둔다. 두 풀이 다른 바이너리이고 잡히는 이유도
// 다르다 — 저쪽은 상대의 수 계산이고 이쪽은 게이지·종반 판정·퀴즈 생성이다. 한 값으로
// 묶으면 어느 쪽 때문에 올렸는지 다음에 아무도 모른다.
func matePoolSize() int {
	size := defaultMatePoolSize
	if v := os.Getenv("ENGINE_MATE_POOL_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			slog.Warn("bad ENGINE_MATE_POOL_SIZE", "value", v, "using", size)
		} else {
			size = n
		}
	}
	return size
}

// matePlies 는 solver 의 手数 한계다. 손잡이는 ENGINE_MATE_PLIES 다.
//
// 읽는 자리가 둘이라 여기 하나로 모은다 — 풀에 주는 DepthLimit 과 캐시가 견주는 값이다.
// 캐시 쪽이 이 값으로 「쌓인 답을 쓸 수 있나」를 판단하므로, 두 값이 갈리면 한계 9의
// 「詰み이 없다」를 한계 11의 답으로 쓴다(archive.Mate.lookup).
func matePlies() int {
	plies := defaultMatePlies
	if v := os.Getenv("ENGINE_MATE_PLIES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			slog.Warn("bad ENGINE_MATE_PLIES", "value", v, "using", plies)
		} else {
			plies = n
		}
	}
	return plies
}

// engineOptions 는 엔진 전체에 거는 설정이다. 대국마다 달라지는 값은 여기 두지 않는다.
//
// 엔진이 모르는 옵션은 조용히 무시되므로(광고된 것만 보낸다) 엔진을 바꿔도 깨지지 않는다.
// 대신 값이 틀린 채로 도는 것은 안 깨진다 — 엔진을 바꿀 때 같이 확인할 것.
func engineOptions() map[string]string {
	opts := map[string]string{}

	// 평가함수가 요구하는 cp 보정값. 水匠5는 24다.
	// 이게 틀리면 cp 척도가 통째로 달라지고 블런더 임계치가 그 위에서 잡힌다.
	if v := os.Getenv("ENGINE_FV_SCALE"); v != "" {
		opts["FV_SCALE"] = v
	}

	// 치환표 크기(MB). 엔진 하나가 통째로 잡는 메모리라 풀 크기만큼 곱해진다.
	// YaneuraOu의 기본값은 1024라, 3개만 띄워도 3GB를 잡고 기동 때 그만큼 지운다.
	opts["USI_Hash"] = envOr("ENGINE_HASH_MB", "128")

	// 엔진당 스레드. 기본값이 4라 풀 3개면 12스레드가 4 vCPU에 몰린다.
	//
	// 더 중요한 이유는 결정성이다. 스레드가 여럿이면 고정 깊이에서도 탐색 순서와
	// 치환표 경합 때문에 같은 국면이 같은 답을 주지 않는다. 그러면 positions 캐시가
	// "같은 국면 = 같은 결과"를 전제로 못 한다. 동시 탐색은 스레드가 아니라 풀로 얻는다.
	opts["Threads"] = envOr("ENGINE_THREADS", "1")

	// 정석 북은 우리가 따로 만든다(D4). 그냥 두면 없는 파일을 찾다 매 기동 에러를 남긴다.
	// USI_OwnBook=false 만으로는 파일을 계속 읽으러 간다 — 이름을 no_book 으로 바꿔야 멈춘다.
	opts["BookFile"] = "no_book"
	opts["USI_OwnBook"] = "false"

	return opts
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// opponentBand 는 상대가 겨냥할 형세 구간이다. 플레이어 관점 cp.
//
// 상수를 실측으로 잡는 동안 흔들어 볼 손잡이라 환경변수로 뺐다. 값이 정해지면
// 기본값이 되고 여기는 남는다 — 레이팅이 붙으면 플레이어마다 달라질 자리다.
func opponentBand() game.Band {
	lo, hi := envInt("OPPONENT_BAND_LO", game.DefaultBand.LoCp), envInt("OPPONENT_BAND_HI", game.DefaultBand.HiCp)
	if lo > hi {
		slog.Warn("OPPONENT_BAND_LO is above HI", "lo", lo, "hi", hi, "using", game.DefaultBand)
		return game.DefaultBand
	}
	return game.Band{LoCp: lo, HiCp: hi}
}

func envInt(name string, fallback int) int {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("value is not an integer", "name", name, "value", v, "using", fallback)
		return fallback
	}
	return n
}

// engineDepth 는 상대 수를 고를 때의 탐색 깊이다.
//
// 시간이 아니라 깊이인 이유는 game.NewAdaptiveOpponent 주석에 있다. 지연이 문제가 되면
// 여기를 줄인다(기본값이 14이므로 12가 그 손잡이다). 시간 상한을 걸어 중간에 자르는
// 쪽이 아니다.
//
// **이 값을 걸면 여섯 자리가 갈린다.** 상대 수와 퀴즈만 여기를 읽고 나머지 넷은
// 상수라, 캐시가 서로 못 쓰는 두 무리가 된다(internal/archive).
func engineDepth() int {
	v := os.Getenv("ENGINE_DEPTH")
	if v == "" {
		return game.DefaultDepth
	}
	d, err := strconv.Atoi(v)
	if err != nil || d < 1 {
		slog.Warn("bad ENGINE_DEPTH", "value", v, "using", game.DefaultDepth)
		return game.DefaultDepth
	}
	return d
}
