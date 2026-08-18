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

	"github.com/jovid18/show-gi/apps/server/internal/archive"
	"github.com/jovid18/show-gi/apps/server/internal/auth"
	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/intervene"
	"github.com/jovid18/show-gi/apps/server/internal/quiz"
	"github.com/jovid18/show-gi/apps/server/internal/server"
	"github.com/jovid18/show-gi/apps/server/internal/store"
	"github.com/jovid18/show-gi/apps/server/internal/usi"
)

// 엔진 풀 크기의 기본값.
//
// 빌리는 자리가 **다섯**이다 — ① 상대 수 ② 개입 판정 ③ 가정 수순 ④ 手筋 힌트
// ⑤ 되짚기 퀴즈의 「최선수는?」. **詰み 탐색은 여기 없다** — 다른 바이너리라 풀이 따로다
// (defaultMatePoolSize).
//
// **3은 그 다섯을 다 덮는 값이 아니다.** 퀴즈가 판이 끝날 때 12국면 × 956ms ≈ 12초를 쓰므로
// (§53) 두 판이 동시에 끝나면 진행 중인 대국의 착수가 그만큼 뒤로 밀린다. 그래도 안 올린 것은
// **그 지연이 대국을 멈추지 않기 때문**이다(mate 풀과 달리 여기는 원래도 여럿이 다툰다).
// 올릴 자리는 태스크 정의의 `ENGINE_POOL_SIZE` 다.
// Fargate에 4 vCPU를 준 이유가 이것이고, 느려지면 태스크 정의만 바꿔 올린다.
const defaultEnginePoolSize = 3

// defaultMatePoolSize 는 詰将棋 solver 의 기본 개수다. **2인 이유는 startMateEngines 에 있다** —
// 퀴즈 생성이 하나를 오래 잡으므로 대국 쪽에 한 자리를 남긴다.
const defaultMatePoolSize = 2

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

	opts.Google = startAuth()
	opts.SessionSecret = os.Getenv("SESSION_SECRET")
	opts.PublicOrigin = os.Getenv("PUBLIC_ORIGIN")

	// 대인전. **엔진 앞에 둔다** — 엔진이 없어도 사람끼리는 둘 수 있다.
	//
	// ctx 를 여기서 주는 것이 요점이다: 대국의 수명은 연결이 아니라 이 프로세스다.
	// 핸들러의 `r.Context()` 에 매달면 한쪽이 탭을 닫는 순간 시계까지 멈춰서, 남은
	// 사람의 대국이 끝나지도 이어지지도 못한다(server.NewMatch).
	opts.Match = server.NewMatch(ctx, opts.Store, opts.Level)

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

		// **모든 탐색이 데이터가 된다.** 엔진을 부르는 자리가 여섯인데(상대의 수 · 개입
		// 판정 · 대국 중 가정 수순 · 되짚는 판의 가정 수순 · 手筋 제안형 힌트 · 되짚기
		// 퀴즈의 「최선수는?」) 기록을 여섯 곳에 흩뿌리면 반드시 하나가 빠진다. 그래서 **풀을 한 겹 감싸고 다섯이 같은
		// 하나를 받는다** — 감싸는 자리가 여기 한 줄이라 빠뜨릴 자리가 없다(internal/archive).
		//
		// 手筋 힌트가 이 구조에서 특히 값을 한다. 후보마다 착수 후 국면을 재는데 그 국면은
		// **플레이어가 실제로 그 수를 두면 바로 다시 물어볼 국면**이라, 캐시 적중이
		// 저절로 따라온다(journal §37: 같은 국면 1.54s → 27ms).
		//
		// DB가 없으면 그대로 통과시킨다. 인터페이스에 nil 포인터를 넣지 않는 것은
		// 아래 mate solver 와 같은 이유다.
		var into archive.Store
		if opts.Store != nil {
			into = opts.Store
		}
		searcher := archive.Wrap(pool, into)
		// 떠 있는 기록이 끝나기를 기다린다. 안 기다리면 **마지막 수의 분석이 버려진다.**
		// **등록 순서가 곧 종료 순서다**(LIFO). 이 줄이 위 defer st.Close() 보다 뒤라서 기록이 다 흘러간 뒤 DB가 닫힌다.
		defer searcher.Wait()

		opts.NewOpponent = func() game.Opponent {
			return game.NewAdaptiveOpponent(searcher, engineDepth(), opponentBand())
		}
		opts.NewAnalyst = func() game.Analyst {
			return game.NewEngineAnalyst(searcher, mate, opts.Level)
		}
		// 종반 판정·詰み 게이지·퀴즈의 詰み 트리가 **셋 다 이 풀이다.** 앞의 둘은 시간상
		// 겹치지 않지만(판정은 사람의 수 직후, 게이지는 상대의 수 직후) 퀴즈는 판이
		// 끝나는 자리에서 수십 초를 잡는다 — 그래서 하나로는 모자라다(matePoolSize).
		opts.Mate = mate

		// 가정 수순도 같은 풀이다(internal/server/whatif.go). 대국과 자리를 다투지만,
		// 겹치면 풀이 순서대로 빌려주고 그만큼 기다린다 — 지연은 여기서 허용된 비용이다.
		opts.Search = searcher

		// 되짚기 퀴즈의 생성기. **엔진 둘을 다 쓴다** — 詰み 문항은 solver, 「최선수는?」은
		// 탐색부다. 탐색부 쪽을 감싼 것으로 넘기는 이유는 그 결과도 `positions` 에 쌓여야
		// 하기 때문이다(§37) — 퀴즈가 재는 국면은 되짚기에서 가정 수순이 곧 다시 물어볼 자리다.
		//
		// mate 가 nil이면 **「최선수는?」 문항만** 나온다 — 없어지는 쪽이 詰み 문항이다
		// (`Build` 가 `b.mate != nil` 로 가른다). searcher 가 없으면 이 자리 자체가 없다.
		opts.Quiz = quiz.NewBuilder(mate, searcher, engineDepth())
	}

	if err := server.Run(ctx, *addr, opts); err != nil {
		log.Fatal(err)
	}
}

// startAuth 는 Google 로그인을 켠다. 키가 없으면 nil이고 익명 대국으로 남는다.
//
// **없다고 프로세스를 죽이지 않는다.** 엔진·DB·LLM 키와 같은 판단이고, 여기서는
// 특히 그렇다 — 로그인은 대국의 전제가 아니라 그 판이 누구 것으로 남느냐일 뿐이다.
// 기동 로그가 어느 쪽으로 돌고 있는지 한 줄로 말한다.
func startAuth() *auth.Google {
	g := auth.NewGoogle(os.Getenv("GOOGLE_CLIENT_ID"), os.Getenv("GOOGLE_CLIENT_SECRET"))
	if g == nil {
		log.Print("GOOGLE_CLIENT_ID/SECRET are not set — games stay anonymous")
		return nil
	}
	if os.Getenv("SESSION_SECRET") == "" {
		// 서명 키가 없으면 쿠키를 위조할 수 있다. 그건 로그인이 없는 것보다 나쁘다.
		log.Print("SESSION_SECRET is not set — sign-in stays off")
		return nil
	}
	log.Print("google sign-in ready")
	return g
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
	// 대국 중에는 매 수 **두 번**이지만(종반 판정과 詰み 게이지) 그 둘이 시간상 겹치지
	// 않는다 — 판정은 사람의 수 직후, 게이지는 상대의 수 직후다. 거기까지면 하나로 족했다.
	//
	// **세 번째 소비자가 그 전제를 깼다.** 되짚기 퀴즈의 詰み 트리가 판이 끝나는 자리에서
	// 수십 초 동안 이 풀을 잡고(journal §53), 그동안 **다른 대국의** 게이지와 종반
	// 판정이 막힌다. 그래서 기본을 2로 올렸다.
	//
	// **「대국 쪽에 늘 한 자리가 남는다」는 아니다.** 생성은 끝나는 판마다 하나씩 뜨고 수를
	// 막는 자리가 없어서, 두 판이 동시에 끝나면 둘이 두 자리를 다 잡는다. 다만 탐색 하나마다
	// 빌리고 돌려주므로(`Pool.Do`) 굶는 것이 아니라 **줄을 서는 것**이고, 대국 쪽은 지연으로
	// 겪는다 — 하나였을 때는 수십 초를 통째로 기다렸다.
	pool, err := usi.NewPool(matePoolSize(), cmd, map[string]string{
		"USI_Hash":   envOr("ENGINE_HASH_MB", "128"),
		"Threads":    envOr("ENGINE_THREADS", "1"),
		"DepthLimit": envOr("ENGINE_MATE_PLIES", "11"),
	})
	if err != nil {
		log.Printf("cannot start the mate solver — endgame judgment is disabled: %v", err)
		return nil
	}
	log.Printf("mate solver ready: %s x%d", cmd, pool.Size())
	return pool
}

// matePoolSize 는 詰将棋 solver 를 몇 개 띄우나다. 손잡이는 `ENGINE_MATE_POOL_SIZE` 다.
//
// **탐색부의 `ENGINE_POOL_SIZE` 와 갈라 둔다.** 두 풀이 다른 바이너리이고 잡히는 이유도
// 다르다 — 저쪽은 상대의 수 계산이고 이쪽은 게이지·종반 판정·퀴즈 생성이다. 한 값으로
// 묶으면 어느 쪽 때문에 올렸는지 다음에 아무도 모른다.
func matePoolSize() int {
	size := defaultMatePoolSize
	if v := os.Getenv("ENGINE_MATE_POOL_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			log.Printf("ENGINE_MATE_POOL_SIZE=%q is not a positive integer, using %d", v, size)
		} else {
			size = n
		}
	}
	return size
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
