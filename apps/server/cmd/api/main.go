// Command api 는 show-gi의 HTTP/WebSocket 서버다.
//
// 여기에는 플래그와 프로세스 수명만 둔다. 로직은 internal 아래에 있다.
package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"

	"github.com/jovid18/show-gi/apps/server/internal/server"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	// SIGINT/SIGTERM이 오면 ctx가 취소되고, 그걸 받아 진행 중인 요청을 마저 끝낸다.
	// 대국 세션과 엔진 프로세스가 붙으면 여기서 정리 순서가 갈리므로 처음부터 깔아둔다.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := server.Run(ctx, *addr); err != nil {
		log.Fatal(err)
	}
}
