// Command api 는 show-gi의 HTTP/WebSocket 서버다.
//
// 여기에는 플래그와 프로세스 수명만 둔다. 로직은 internal 아래에 있다.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/jovid18/show-gi/apps/server/internal/explain"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/server"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 엔진 풀 크기의 기본값.
//
// 최소 3개다 — ① 상대 수 ② 플레이어 후보 선행 계산 ③ mate 탐색(詰み 게이지).
// Fargate에 4 vCPU를 준 이유가 이것이고, 느려지면 태스크 정의만 바꿔 올린다.
const defaultEnginePoolSize = 3

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	// SIGINT/SIGTERM이 오면 ctx가 취소되고, 그걸 받아 진행 중인 요청을 마저 끝낸다.
	// 대국 세션과 엔진 프로세스가 붙어 있으므로 여기서 정리 순서가 갈린다.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 실력 추정이 붙기 전까지는 제일 너그러운 쪽이 기본이다 — 학습 앱에서 과잉 개입은
	// 잔소리가 되고, 이 제품이 피하려는 바로 그것이다(intervene.Beginner 주석).
	//
	// **판정과 기록이 같은 값을 봐야 한다.** 갈리면 「어느 임계치에서 걸린 개입인가」가
	// 기록에서 틀리고, 그 위에서 상수를 흔들어 보게 된다.
	opts := server.Options{Level: intervene.Beginner}

	if st := openStore(ctx); st != nil {
		defer st.Close()
		opts.Store = st
	}

	opts.Explainer = startExplainer(opts.Store)

	if pool := startEngines(); pool != nil {
		defer pool.Close()

		// 詰み solver 는 **다른 바이너리**라 따로 띄운다(02-architecture.md §3).
		// 없어도 대국과 승률 낙폭 판정은 그대로 돌고, 종반 판정만 빠진다.
		matePool := startMateEngines()
		if matePool != nil {
			defer matePool.Close()
		}

		// **인터페이스에 nil 포인터를 넣지 않는다.** `*usi.Pool` 이 nil이어도 인터페이스
		// 값 자체는 non-nil이 되어 `== nil` 검사를 통과하고, 그 다음 줄에서 죽는다.
		var mate game.MateSearcher
		if matePool != nil {
			mate = matePool
		}

		opts.NewOpponent = func() game.Opponent {
			return game.NewAdaptiveOpponent(pool, engineDepth(), opponentBand())
		}
		opts.NewAnalyst = func() game.Analyst {
			return game.NewEngineAnalyst(pool, mate, opts.Level)
		}
		// 종반 판정과 詰み 게이지가 같은 풀을 쓴다. 두 자리가 시간상 겹치지 않아
		// (판정은 사람의 수 직후, 게이지는 상대의 수 직후) 하나로 충분하다.
		opts.Mate = mate
	}

	if err := server.Run(ctx, *addr, opts); err != nil {
		log.Fatal(err)
	}
}

// startExplainer 는 개입 문구를 만드는 계층을 세운다.
//
// **키가 없으면 결정적 문구만 나가고 대국은 그대로 된다.** 엔진·DB와 같은 판단이다 —
// 없는 것으로 프로세스를 죽이지 않는다. 프로덕션에는 실키가 들어가 있고(06-status.md §3),
// 이 경로는 로컬과 라우터 장애 때의 바닥이다. 기동 로그가 어느 쪽인지 한 줄로 말한다.
//
// st 가 nil이면 캐시가 없다. 그러면 **같은 설명을 매번 다시 산다** — 로그에 한 줄 남긴다.
func startExplainer(st *store.Store) explain.Explainer {
	client := explain.NewClient(
		os.Getenv("ORCA_API_KEY"),
		os.Getenv("ORCA_BASE_URL"),
		os.Getenv("ORCA_MODEL_SMALL"),
		os.Getenv("ORCA_MODEL_LARGE"),
		envFloat("ORCA_USDJPY", explain.DefaultUSDJPY),
	)
	if client == nil {
		log.Print("ORCA_API_KEY is not set — interventions will use the built-in Japanese templates")
		return explain.TemplateOnly()
	}
	if st == nil {
		log.Print("explain: no database — every explanation will be generated again (no Tier 0)")
		return explain.NewLayered(nil, client)
	}
	log.Print("explain: OrcaRouter ready, cached explanations come from the database")
	return explain.NewLayered(st, client)
}

// envFloat 는 소수 환경변수를 읽는다. 틀린 값은 기본값으로 되돌리고 조용히 넘기지 않는다.
func envFloat(name string, fallback float64) float64 {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 {
		log.Printf("%s=%q is not a positive number, using %v", name, v, fallback)
		return fallback
	}
	return f
}

// openStore 는 DB에 붙는다. 실패하면 nil을 돌려주고 서버는 그냥 뜬다.
//
// 엔진과 같은 판단이다 — **DB가 없다고 프로세스를 죽이지 않는다.** 국면 캐시가 없으면
// 매번 계산할 뿐이지 대국은 된다. 죽이면 ECS 재시작 루프로 사이트 전체가 내려간다.
func openStore(ctx context.Context) *store.Store {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		log.Print("DATABASE_URL is not set — the position cache is disabled")
		return nil
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		log.Printf("cannot open the database — the position cache is disabled: %v", err)
		return nil
	}
	log.Print("database ready")
	return st
}

// startEngines 는 USI 엔진 풀을 띄운다. 실패하면 nil을 돌려주고 서버는 그냥 뜬다.
//
// **엔진이 없다고 프로세스를 죽이지 않는다.** 죽이면 ECS가 재시작을 반복하고
// /healthz 까지 같이 사라져 사이트 전체가 내려간다. 대국만 막고 나머지는 살린다.
func startEngines() *usi.Pool {
	cmd := os.Getenv("ENGINE_CMD")
	if cmd == "" {
		log.Print("ENGINE_CMD is not set — games are disabled")
		return nil
	}

	size := defaultEnginePoolSize
	if v := os.Getenv("ENGINE_POOL_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			log.Printf("ENGINE_POOL_SIZE=%q is not a positive integer, using %d", v, size)
		} else {
			size = n
		}
	}

	opts := engineOptions()
	pool, err := usi.NewPool(size, cmd, opts)
	if err != nil {
		log.Printf("cannot start engine pool (%s x%d) — games are disabled: %v", cmd, size, err)
		return nil
	}
	log.Printf("engine pool ready: %s x%d %v", cmd, size, opts)
	return pool
}

// startMateEngines 는 詰将棋 solver 풀을 띄운다. 없으면 nil.
//
// 탐색 한계는 **手数(DepthLimit)** 로 준다. 시간이 아니라 수로 자르는 이유는 다른 탐색과
// 같다 — 같은 국면이 같은 답을 줘야 캐시할 수 있다. 11인 것은 실측 결과다(06-status.md).
func startMateEngines() *usi.Pool {
	cmd := os.Getenv("ENGINE_MATE_CMD")
	if cmd == "" {
		log.Print("ENGINE_MATE_CMD is not set — endgame judgment and the mate gauge are disabled")
		return nil
	}
	// 매 수 **두 번**이지만(종반 판정과 詰み 게이지) 그 둘이 시간상 겹치지 않아
	// 상대 수 계산만큼 동시에 돌 필요가 없다. 판정은 사람의 수 직후에, 게이지는
	// 상대의 수 직후에 걸린다 — 사람 차례와 엔진 차례가 곧 두 자리의 조건이다.
	pool, err := usi.NewPool(1, cmd, map[string]string{
		"USI_Hash":   envOr("ENGINE_HASH_MB", "128"),
		"Threads":    envOr("ENGINE_THREADS", "1"),
		"DepthLimit": envOr("ENGINE_MATE_PLIES", "11"),
	})
	if err != nil {
		log.Printf("cannot start the mate solver — endgame judgment is disabled: %v", err)
		return nil
	}
	log.Printf("mate solver ready: %s", cmd)
	return pool
}

// engineOptions 는 엔진 전체에 거는 설정이다. 대국마다 달라지는 값은 여기 두지 않는다.
//
// 엔진이 모르는 옵션은 조용히 무시되므로(광고된 것만 보낸다) 엔진을 바꿔도 깨지지 않는다.
// 대신 **값이 틀린 채로 도는 것은 안 깨진다** — 엔진을 바꿀 때 같이 확인할 것.
func engineOptions() map[string]string {
	opts := map[string]string{}

	// 평가함수가 요구하는 cp 보정값. 水匠5는 24다.
	// 이게 틀리면 cp 척도가 통째로 달라지고 블런더 임계치가 그 위에서 잡힌다.
	if v := os.Getenv("ENGINE_FV_SCALE"); v != "" {
		opts["FV_SCALE"] = v
	}

	// 치환표 크기(MB). **엔진 하나가 통째로 잡는 메모리라 풀 크기만큼 곱해진다.**
	// YaneuraOu의 기본값은 1024라, 3개만 띄워도 3GB를 잡고 기동 때 그만큼 지운다.
	opts["USI_Hash"] = envOr("ENGINE_HASH_MB", "128")

	// 엔진당 스레드. 기본값이 4라 풀 3개면 12스레드가 4 vCPU에 몰린다.
	//
	// 더 중요한 이유는 **결정성**이다. 스레드가 여럿이면 고정 깊이에서도 탐색 순서와
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
// **상수를 실측으로 잡는 동안 흔들어 볼 손잡이라 환경변수로 뺐다.** 값이 정해지면
// 기본값이 되고 여기는 남는다 — 레이팅이 붙으면 플레이어마다 달라질 자리다.
func opponentBand() game.Band {
	lo, hi := envInt("OPPONENT_BAND_LO", game.DefaultBand.LoCp), envInt("OPPONENT_BAND_HI", game.DefaultBand.HiCp)
	if lo > hi {
		log.Printf("OPPONENT_BAND_LO(%d) > HI(%d), using default", lo, hi)
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
		log.Printf("%s=%q is not an integer, using %d", name, v, fallback)
		return fallback
	}
	return n
}

// engineDepth 는 상대 수를 고를 때의 탐색 깊이다.
//
// 시간이 아니라 깊이인 이유는 game.NewAdaptiveOpponent 주석에 있다. 지연이 문제가 되면
// **여기를 줄인다**(14→12). 시간 상한을 걸어 중간에 자르는 쪽이 아니다.
func engineDepth() int {
	v := os.Getenv("ENGINE_DEPTH")
	if v == "" {
		return game.DefaultDepth
	}
	d, err := strconv.Atoi(v)
	if err != nil || d < 1 {
		log.Printf("ENGINE_DEPTH=%q is not a positive integer, using %d", v, game.DefaultDepth)
		return game.DefaultDepth
	}
	return d
}
