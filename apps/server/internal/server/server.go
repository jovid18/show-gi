// Package server 는 HTTP 표면과 프로세스 수명을 담당한다.
//
// 핸들러가 대국 상태를 직접 읽지 않는다. 상태는 세션 goroutine이 소유하고,
// 핸들러는 채널로 물어본다 — 지름길을 내는 순간 잠금이 필요해진다.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/jovid18/show-gi/apps/server/internal/game"
)

// shutdownGrace 는 종료 신호를 받고 진행 중인 요청을 기다려주는 시간이다.
// 엔진 탐색이 걸린 요청이 있어도 이 안에서는 끝난다.
const shutdownGrace = 10 * time.Second

// Options 는 서버가 밖에서 받아야 하는 것들이다.
type Options struct {
	// NewOpponent 는 대국마다 상대를 하나 만든다.
	//
	// nil이면 /ws 가 503을 준다. 엔진이 없다고 프로세스를 죽이지 않는 이유는,
	// 그러면 ECS가 재시작을 반복하며 /healthz 까지 같이 죽어 사이트 전체가 내려가기 때문이다.
	// 엔진 고장은 대국만 막고 나머지는 살려둔다.
	NewOpponent func() game.Opponent
}

// Handler 는 라우팅만 조립한다. 테스트가 서버를 띄우지 않고 이걸 그대로 쓴다.
func Handler(opts Options) http.Handler {
	mux := http.NewServeMux()

	// 로드밸런서와 배포 스크립트가 보는 곳. 인증 뒤에 두지 않는다.
	//
	// **엔진이 없어도 200이다.** 여기서 실패를 내면 ECS가 태스크를 죽이고 재시작을
	// 반복해 사이트 전체가 내려간다. 대신 `engine` 필드로 상태를 드러낸다 —
	// 없으면 "배포는 성공했는데 대국만 안 되는" 상태를 아무도 못 알아챈다.
	// 실제로 한 번 그렇게 됐다(엔진 교체 배포에서 태스크 정의의 낡은 ENGINE_CMD가
	// 이미지의 값을 덮어썼다). 배포 워크플로가 이 필드를 확인한다.
	engineReady := opts.NewOpponent != nil
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "engine": engineReady})
	})

	if opts.NewOpponent != nil {
		mux.Handle("GET /ws/game", &gameHandler{newOpponent: opts.NewOpponent})
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
