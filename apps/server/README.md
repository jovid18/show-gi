# apps/server

API 서버. 대국 진행, 개입 판정, USI 엔진 브리지를 맡는다. 설계는 [docs/02-architecture.md](../../docs/02-architecture.md).

## 실행

```sh
cd apps/server

go run ./cmd/api                # :8080
go run ./cmd/api -addr :9000

go test -race ./...             # 엔진 프로세스와 세션이 동시에 도므로 -race는 항상 켠다
go vet ./...
gofmt -l .                      # 출력이 있으면 포맷이 어긋난 파일이다
```

```sh
curl localhost:8080/healthz     # {"ok":true}
```

## 배치

|                   |                                        |
| ----------------- | -------------------------------------- |
| `cmd/api`         | 플래그·시그널 처리. 로직은 두지 않는다 |
| `internal/server` | HTTP 표면과 프로세스 수명              |

`go.mod`는 레포 루트가 아니라 여기 있다. `apps/web`이 Node 워크스페이스라 루트를 한쪽 언어에 내주지 않으려는 것이고, 대신 Go 명령은 전부 이 디렉터리에서 돌린다.

## 앞으로 들어올 것

[docs/02-architecture.md](../../docs/02-architecture.md)의 패키지 구성을 따른다.

|                      |                                                        |
| -------------------- | ------------------------------------------------------ |
| `internal/shogi`     | 룰 엔진 — SFEN, 합법수, 반칙 검증. `../shogi`에서 이식 |
| `internal/usi`       | 엔진 프로세스 풀. MultiPV·mate 파싱                    |
| `internal/game`      | 대국 세션 상태머신 (goroutine 1/세션)                  |
| `internal/intervene` | 개입 판정 — 임계치, 롤백, 블런더 카테고리              |
| `internal/coach`     | 적응형 상대 수 선택                                    |
| `internal/tag`       | 囲い·전법·手筋 패턴 감지                               |
| `internal/profile`   | 실력 추정, 약점 프로파일                               |
| `internal/llm`       | OrcaRouter 클라이언트 + 3단 캐시                       |
| `internal/store`     | postgres (pgx)                                         |

## 아직 없는 것

외부 의존성이 하나도 없다(stdlib만). 그래서 `go.sum`이 없고, CI의 `setup-go` 캐시도 꺼둔 상태다 — pgx나 websocket이 들어오는 PR에서 `.github/workflows/server.yml`의 주석대로 되돌린다.

WebSocket은 `http.Server`의 `ReadHeaderTimeout`에서 빼야 한다. 대국 연결은 오래 열려 있다.
