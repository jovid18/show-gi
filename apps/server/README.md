# apps/server

API 서버. 대국 진행, 개입 판정, USI 엔진 브리지를 맡는다. 설계는 [docs/02-architecture.md](../../docs/02-architecture.md).

## 실행

```sh
cd apps/server

go run ./cmd/api                # :8080
go vet ./... && gofmt -l .      # gofmt 는 출력이 있으면 어긋난 파일이다
```

`ENGINE_CMD` 가 없으면 대국이 막히고 `/healthz` 가 `engine:false` 를 준다 — 서버는 그래도 뜬다. `DATABASE_URL` 도 마찬가지다(`db:false`). **둘 다 없어도 죽지 않는 것이 의도다** — 죽이면 ECS가 재시작을 반복해 사이트 전체가 내려간다.

```sh
curl localhost:8080/healthz     # {"ok":true,"engine":true,"db":true}
```

표면은 셋이다. **리뷰(`/api/games`)는 엔진이 아니라 DB에 매여 있다** — 엔진이 죽어 대국을 못 해도 지난 판은 볼 수 있어야 한다.

| 라우트                |                                                                                     |
| --------------------- | ----------------------------------------------------------------------------------- |
| `GET /healthz`        | 엔진·DB 상태를 값으로 말한다. **없어도 200**                                        |
| `GET /ws/game`        | 연결 하나가 대국 하나. 엔진이 없으면 503                                            |
| `GET /api/games`      | 최근 대국 목록. 한 수도 안 둔 판은 안 온다                                          |
| `GET /api/games/{id}` | 한 판 전체 — 手数마다의 국면·棋譜 표기와 물러진 수 ([§33](../../docs/06-status.md)) |

`ORCA_API_KEY` 도 같다. 없으면 개입 문구가 **결정적 일본어 템플릿**으로 나가고 나머지는 그대로 돈다 — 그 문구도 사실(利き 매수·잡히는 駒)을 담으므로 화면이 비지 않는다. 기동 로그가 어느 쪽으로 돌고 있는지 한 줄로 말한다. 값은 [.env.example](../../.env.example) 참조.

## 테스트

세 층이고, **아래로 갈수록 CI에서 안 돈다.**

```sh
# ① 항상 — 엔진도 DB도 없이 돈다. DB 테스트는 조용히 skip 된다
go test -race ./...

# ② DB — 규칙이 SQL의 WHERE 절에만 있어 가짜로는 검증이 안 된다
docker compose up -d db
for f in internal/store/migrations/*.sql; do   # 번호 순서대로 전부
  docker exec -i show-gi-db psql -U showgi -d showgi -v ON_ERROR_STOP=1 < "$f"
done
SHOWGI_TEST_DATABASE_URL='postgres://showgi:showgi@localhost:5432/showgi' go test -race ./...

# ③ 실엔진 — 이미지가 필요하다. enginetest.Dockerfile 참조
docker build --platform linux/arm64 -t show-gi-api .
docker build --platform linux/arm64 -t show-gi-enginetest -f enginetest.Dockerfile .
docker run --rm --platform linux/arm64 --cpus 4 -v "$PWD:/src:ro" show-gi-enginetest sh -c '
  cp -r /src /work && cd /work &&
  SHOWGI_USI_CMD=/opt/yaneuraou/run go test ./... -run RealEngine -v'
```

**`-race` 를 빼지 않는다.** 엔진 프로세스와 세션 goroutine이 동시에 도는 구조라 데이터 경합이 가장 값비싼 버그다.

| 환경변수                   | 없으면             | 쓰는 곳                                               |
| -------------------------- | ------------------ | ----------------------------------------------------- |
| `SHOWGI_TEST_DATABASE_URL` | DB 테스트 skip     | `internal/store`, `internal/intervene` 의 재채점 측정 |
| `SHOWGI_USI_CMD`           | 실엔진 테스트 skip | `TestRealEngine`, `TestWSAgainstRealEngine`           |
| `SHOWGI_MATE_CMD`          | 詰み 측정 skip     | `TestMeasureMateSearch`                               |
| `SHOWGI_MEASURE`           | 측정 전부 skip     | `TestMeasure*` — 몇 분 걸린다                         |
| `SHOWGI_GENERATE_TIER1`    | 사전 생성 skip     | `TestGenerateTier1` — **돈이 든다**. 아래             |

> **재채점 측정만 `SHOWGI_MEASURE` 를 안 본다.** `TestMeasureCalibrationFromRecords` 는 엔진을 안 돌리고 DB만 읽어 초 단위로 끝난다. 대신 **기록이 쌓인 DB를 가리켜야 값이 나온다** — 로컬 DB에는 짧은 테스트 대국밖에 없다 ([docs/06-status.md §39](../../docs/06-status.md)).

**`SHOWGI_GENERATE_TIER1` 만 성격이 다르다** — 검증이 아니라 **만드는 일**이다. Tier 1 문구 21개를 실제 라우터로 만들어 `internal/store/migrations/004_explain_cache_tier1.sql` 에 떨어뜨린다([06-status.md §38](../../docs/06-status.md)). **프롬프트를 고쳤을 때만** 다시 돌린다 — 그때 `promptVersion` 이 올라가 옛 행이 통째로 죽고, 게이트 없는 `TestTier1MigrationMatchesFacts` 가 그것을 잡는다.

```sh
set -a && . ../../.env && set +a   # ORCA_API_KEY. 없으면 만들 것이 없으므로 실패한다
SHOWGI_GENERATE_TIER1=1 go test ./internal/explain/ -run GenerateTier1 -v
```

> **엔진이나 평가함수를 바꾸면 ③이 첫 관문이다.** 실제로 `PvInterval` 문제를 거기서 잡았다 — 안 돌렸으면 D3에서 "개입이 왜 안 걸리지"로 나타났을 것이다 ([docs/06-status.md](../../docs/06-status.md) §10).

## 스키마를 바꿀 때

`internal/store/migrations/*.sql` 이 정본이고 **sqlc가 거기서 코드를 만든다.** 질의를 추가하거나 스키마를 고치면 생성물을 다시 만들어야 한다.

```sh
go tool sqlc generate           # internal/store/db/ 를 다시 만든다
```

sqlc 는 `go.mod` 의 `tool` 로 고정돼 있어 따로 설치할 것이 없다.

## 배치

|                      |                                                                           |
| -------------------- | ------------------------------------------------------------------------- |
| `cmd/api`            | 플래그·시그널·배선. 로직은 두지 않는다                                    |
| `internal/server`    | HTTP 표면, WebSocket 대국 프로토콜, 프로세스 수명                         |
| `internal/game`      | 대국 세션 상태머신 — **goroutine 1개가 상태를 소유**한다                  |
| `internal/intervene` | 개입 판정. **엔진을 모른다** — 입력이 평가치와 詰み 거리뿐이다            |
| `internal/explain`   | 설명 문구. **판단하지 않는다** — 정해진 사실을 문장으로만 바꾼다          |
| `internal/shogi`     | 룰 엔진 — SFEN, 합법수, 반칙 검증, 棋譜 표기                              |
| `internal/usi`       | 엔진 프로세스 풀. MultiPV·깊이별 평가치·詰み 탐색                         |
| `internal/store`     | postgres (pgx + sqlc). `db/` 는 생성물이라 손대지 않는다                  |
| `internal/tag`       | 囲い·전법·戦型·手筋의 이름. **엔진도 DB도 모른다** — 국면과 수순만 받는다 |

아직 없는 것: `internal/profile`(실력 추정), `internal/kb`(RAG 코퍼스).

`go.mod`는 레포 루트가 아니라 여기 있다. `apps/web`이 Node 워크스페이스라 루트를 한쪽 언어에 내주지 않으려는 것이고, 대신 Go 명령은 전부 이 디렉터리에서 돌린다.

## 알아두면 좋은 것

- **탐색은 깊이로만 건다.** 시간(`go movetime`)을 쓰지 않아서 `usi` 패키지에 그 API가 아예 없다. 이유는 [CLAUDE.md](../../CLAUDE.md)에 있다
- **엔진 실행 경로(`ENGINE_CMD`)를 태스크 정의에 두지 않는다.** 이미지 내부 구조라 두 곳에 적으면 조용히 어긋난다 — 실제로 한 번 물렸다(docs/06-status.md §11)
- 대국 세션은 **서버 메모리에 있고 연결에 매여 있다.** 배포하면 진행 중인 대국이 끊긴다
