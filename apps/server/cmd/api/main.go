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
	"time"

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
			return game.NewEngineOpponent(pool, engineMoveTime())
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

	pool, err := usi.NewPool(size, cmd)
	if err != nil {
		log.Printf("cannot start engine pool (%s x%d) — games are disabled: %v", cmd, size, err)
		return nil
	}
	log.Printf("engine pool ready: %s x%d", cmd, size)
	return pool
}

// engineMoveTime 은 상대가 한 수를 생각하는 시간이다.
//
// D2에서는 고정값이다. D4의 적응형 상대가 들어오면 레벨이 이걸 정하게 된다.
func engineMoveTime() time.Duration {
	const fallback = 700 * time.Millisecond
	v := os.Getenv("ENGINE_MOVETIME_MS")
	if v == "" {
		return fallback
	}
	ms, err := strconv.Atoi(v)
	if err != nil || ms <= 0 {
		log.Printf("ENGINE_MOVETIME_MS=%q is not a positive integer, using %v", v, fallback)
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}
