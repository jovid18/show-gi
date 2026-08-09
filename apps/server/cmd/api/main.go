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

	"github.com/jovid18/show-gi/apps/server/internal/game"
	"github.com/jovid18/show-gi/apps/server/internal/server"
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

	opts := server.Options{}

	if pool := startEngines(); pool != nil {
		defer pool.Close()
		opts.NewOpponent = func() game.Opponent {
			return game.NewEngineOpponent(pool, engineDepth())
		}
	}

	if err := server.Run(ctx, *addr, opts); err != nil {
		log.Fatal(err)
	}
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

// engineDepth 는 상대 수를 고를 때의 탐색 깊이다.
//
// 시간이 아니라 깊이인 이유는 game.NewEngineOpponent 주석에 있다. 지연이 문제가 되면
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
