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

**되짚기(`GET /api/games`)는 엔진이 아니라 DB에 매여 있다** — 엔진이 죽어 대국을 못 해도 지난 판은 볼 수 있어야 한다. **가정 수순만 그 조건이 다르다**(아래 마지막 줄): 「그래서 상대가 어떻게 하나」가 내용이라 엔진 없이는 성립하지 않고, 없으면 **그 한 경로만** 503이 된다.

| 라우트                             |                                                                                                                                                                                                 |
| ---------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /healthz`                     | 엔진·DB 상태를 값으로 말한다. **없어도 200**                                                                                                                                                    |
| `GET /ws/game`                     | 연결 하나가 대국 하나. 엔진이 없으면 503. `?color=b`·`w` 와 `opening=<id>` 로 고른다 ([§48](../../docs/06-status.md)). `?resume=<id>` 면 중단된 판을 이어 둔다 ([§51](../../docs/06-status.md)) |
| `GET /api/openings`                | 고를 수 있는 상대의 진형. **DB도 엔진도 로그인도 필요 없다** — 상수 목록이다                                                                                                                    |
| `GET /api/games`                   | 최근 대국 목록. **결과가 나온 판만** — 두는 중도 중단도 안 온다 ([§51](../../docs/06-status.md))                                                                                                |
| `GET /api/games/{id}`              | 한 판 전체 — 手数마다의 국면·棋譜 표기와 물러진 수 ([§33](../../docs/06-status.md)). 목록과 **같은 조건**이라 끝나지 않은 판은 404                                                              |
| `GET /api/games/{id}/summary`      | 그 판의 총평. **기보와 따로 간다** — 이쪽만 LLM을 기다린다 ([§52](../../docs/06-status.md)). 조건은 위 줄과 같다                                                                                |
| `GET /api/games/{id}/quiz`         | 그 판에서 뽑은 문항. **정답이 안 실려 온다** — 채점이 서버에 있다 ([§53](../../docs/06-status.md)). `ready:false` 는 **아직 만드는 중**이고 「문항 없음」과 다르다                              |
| `POST /api/games/{id}/quiz/mate`   | 詰み 문항 채점. **내가 낸 수만 보낸다** — 玉方의 응수는 저장된 트리에서 서버가 꺼내 둔다. **엔진을 안 쓴다**                                                                                    |
| `POST /api/games/{id}/quiz/best`   | 「최선수는?」 채점. 첫 수만 받는다. **엔진을 안 쓴다**                                                                                                                                          |
| `POST /api/games/{id}/whatif`      | 가정 수순 한 걸음. **DB와 엔진 둘 다 필요하다** — 없으면 503 ([§37](../../docs/06-status.md))                                                                                                   |
| `GET /api/resumable`               | 이어할 수 있는 중단된 판 하나. **로그인 안 했으면 늘 `null`** — 기록이 없는 배포에서도 200이다                                                                                                  |
| `POST /api/resumable/{id}/decline` | 「いいえ」. 그 판은 중단된 채로 끝나고 다시 안 물어본다                                                                                                                                         |
| `GET /api/me`                      | 지금 로그인한 사람. **로그인이 꺼진 배포에도 있다** — 아래                                                                                                                                      |
| `GET /api/me/profile`              | 마이페이지 — 段級·전적·崩れやすいところ ([§63](../../docs/06-status.md)). **로그인 안 했으면 401** — 익명 판은 서로 구별할 수단이 없어 「이 사람의 전적」에 답할 수가 없다                      |
| `GET /api/auth/google/start`       | Google로 보낸다. 로그인이 꺼져 있으면 **경로 자체가 없다**(404)                                                                                                                                 |
| `GET /api/auth/google/callback`    | Google이 돌려보내는 자리. 성공·실패 어느 쪽이든 `/` 로 되돌린다                                                                                                                                 |
| `POST /api/auth/logout`            | 쿠키를 지운다. 서버에는 지울 것이 없다                                                                                                                                                          |

**로그인은 대국의 전제가 아니다.** `GOOGLE_CLIENT_ID`·`GOOGLE_CLIENT_SECRET`·`SESSION_SECRET` 셋과 DB가 다 있어야 켜지고, 하나라도 없으면 표면이 닫힌 채 지금까지처럼 익명으로 둔다 — 바뀌는 것은 그 판이 `games.user_id` 로 누구에게 붙느냐 하나다. **`/api/me` 만은 꺼져 있어도 200을 준다**: 화면이 「로그인이 없는 배포」와 「서버 고장」을 갈라 그려야 하는데, 404면 그 둘이 같아진다.

> **세션이 표가 아니다.** 쿠키 자체를 `SESSION_SECRET` 으로 HMAC 서명한다(`internal/auth`). 그래서 마이그레이션도, 로그인마다의 쓰기도 없는 대신 **발급한 세션을 서버가 끊을 수 없다** — 키를 바꾸는 것이 곧 전원 로그아웃이다.

**대국 중에도 같은 것을 묻는다.** `/ws/game` 에 `{"type":"whatif","ply":N,"moves":[…]}` 를 보내면 `whatif` 메시지로 같은 자리가 온다 — 판정 코드는 한 벌이고 **뿌리를 어디서 얻느냐**만 갈린다(끝난 판은 DB 기록, 두는 중인 판은 세션이 방금 보낸 스냅샷). 기록은 비동기로 쌓이므로 개입 직후에 DB로 물으면 마지막 수가 아직 없을 수 있다.

> **`whatif` 는 판(SFEN)을 받지 않는다.** 받는 것은 「기보의 몇 手目에서」와 「거기서 어떤 수를 뒀나」뿐이고, 뿌리 국면은 서버가 기록에서 다시 둬서 만든다. SFEN을 받으면 그 표면이 곧 **아무 국면이나 깊이 12로 재 주는 공개 엔진**이 되고, 그 풀은 대국 세 판이 쓰는 것과 같은 풀이다.

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

# ④ 엔진 + DB — 기록을 국면으로 되돌려 엔진에 다시 묻는 측정(06-status.md §40)
#
# **`--network show-gi-net` 이 ③과 다른 점이다.** db가 컨테이너라 호스트의
# localhost 로는 안 보인다 — 컨테이너명으로 붙는다
docker run --rm --platform linux/arm64 --cpus 4 --network show-gi-net \
  -v "$PWD:/src:ro" show-gi-enginetest sh -c '
  cp -r /src /work && cd /work &&
  SHOWGI_USI_CMD=/opt/yaneuraou/run SHOWGI_MATE_CMD=/opt/yaneuraou/run-mate \
  SHOWGI_MEASURE=1 \
  SHOWGI_TEST_DATABASE_URL="postgres://showgi:showgi@show-gi-db:5432/showgi" \
  go test ./internal/game/ -run MeasureBlunder -v -timeout 60m'
```

**`-race` 를 빼지 않는다.** 엔진 프로세스와 세션 goroutine이 동시에 도는 구조라 데이터 경합이 가장 값비싼 버그다.

**CI에서만 지는 테스트가 있으면 코어를 줄여서 재현한다** — 부하를 거는 것보다 이쪽이 확실하다 ([06-status.md §73](../../docs/06-status.md)).

```sh
GOMAXPROCS=2 go test -race -count=300 -run '<그 테스트>' ./internal/game/
```

| 환경변수                            | 없으면             | 쓰는 곳                                                                                               |
| ----------------------------------- | ------------------ | ----------------------------------------------------------------------------------------------------- |
| `SHOWGI_TEST_DATABASE_URL`          | DB 테스트 skip     | `internal/store`, `internal/intervene` 의 재채점 측정, `internal/game` 의 블런더 재분류 측정          |
| `SHOWGI_USI_CMD`                    | 실엔진 테스트 skip | `TestRealEngine`, `TestWSAgainstRealEngine`                                                           |
| `SHOWGI_MATE_CMD`                   | 詰み 측정 skip     | `TestMeasureMateSearch`, `TestMeasureBlunderMate`, `TestMeasureBlunderTsumero`                        |
| `SHOWGI_USI_CMD` + `SHOWGI_MEASURE` | 밴드 측정 skip     | `TestMeasureSkill*` — 실력 추정이 밴드를 옮기는 폭을 잰다(06-status.md §47). DB는 안 쓴다             |
| `SHOWGI_MEASURE`                    | 측정 전부 skip     | `TestMeasure*` — 몇 분 걸린다                                                                         |
| `SHOWGI_MEASURE` 만                 | 부하 측정 skip     | `TestMeasureTagHintLoad` — 手筋 게이트가 한 판에 쓰는 비용(06-status.md §56). **엔진도 DB도 안 쓴다** |
| `SHOWGI_GENERATE_TIER1`             | 사전 생성 skip     | `TestGenerateTier1` — **돈이 든다**. 아래                                                             |
| `SHOWGI_KIFU_SCAN`                  | 기보 스캔 skip     | `internal/kifu` 의 `TestScan*` 다섯. 엔진도 DB도 안 쓴다                                              |
| `SHOWGI_KIFU_DUMP`                  | 덤프 skip          | `TestDumpFormationCases` — 사례마다 마크다운 한 장을 그 경로에 떨군다                                 |
| `SHOWGI_TEST_ENGINE_PATH`           | 기보 임포트 skip   | `internal/kifu` 의 `TestImportGame`. **여기만 `SHOWGI_USI_CMD` 를 안 쓴다**                           |
| `ORCA_API_KEY`                      | 실라우터 skip      | `TestRealRouter` — **돈이 든다.** 프롬프트를 고치면 여기가 첫 관문이다                                |

> **`SHOWGI_MEASURE` 는 혼자서는 아무것도 안 연다.** `TestMeasure*` 는 전부 `*_CMD` 와 **둘 다** 있어야 돈다. 한쪽만 주면 실엔진 테스트는 돌고 측정만 조용히 건너뛴다 — 초록이 「쟀다」는 뜻이 아닌 자리가 여기 한 겹 더 있다.

> **재채점 측정만 `SHOWGI_MEASURE` 를 안 본다.** `TestMeasureCalibrationFromRecords` 는 엔진을 안 돌리고 DB만 읽어 초 단위로 끝난다. 대신 **기록이 쌓인 DB를 가리켜야 값이 나온다** — 로컬 DB에는 짧은 테스트 대국밖에 없다 ([docs/06-status.md §39](../../docs/06-status.md)).

**`SHOWGI_GENERATE_TIER1` 만 성격이 다르다** — 검증이 아니라 **만드는 일**이다. Tier 1 문구 21개를 실제 라우터로 만들어 `internal/store/migrations/004_explain_cache_tier1.sql` 에 떨어뜨린다([06-status.md §38](../../docs/06-status.md)). **프롬프트를 고쳤을 때만** 다시 돌린다 — 그때 `promptVersion` 이 올라가 옛 행이 통째로 죽고, 게이트 없는 `TestTier1MigrationMatchesFacts` 가 그것을 잡는다.

```sh
set -a && . ../../.env && set +a   # ORCA_API_KEY. 없으면 만들 것이 없으므로 실패한다
SHOWGI_GENERATE_TIER1=1 go test ./internal/explain/ -run GenerateTier1 -v
```

> **엔진이나 평가함수를 바꾸면 ③이 첫 관문이다.** 실제로 `PvInterval` 문제를 거기서 잡았다 — 안 돌렸으면 D3에서 "개입이 왜 안 걸리지"로 나타났을 것이다 ([docs/06-status.md](../../docs/06-status.md) §10).

## 실 기보 — floodgate

태그가 실제 대국에서 맞게 붙는지를 넓게 보는 데 쓴다([docs/06-status.md](../../docs/06-status.md) §44). **레포에 커밋하지 않는다** — 남의 대국 기록이고 하루치가 5MB 넘는다. `.gitignore` 에 경로가 있다.

```sh
cd apps/server/internal/kifu/testdata/floodgate
DAY=2020/03/14   # 하루에 300판쯤 있다. /shogi/x/ 에 2010년부터 있다
curl -sS "https://wdoor.c.u-tokyo.ac.jp/shogi/x/$DAY/" |
  grep -oE 'href="[^"]+\.csa"' | sed 's/href="//;s/"//' |
  while IFS= read -r f; do
    out="${f#wdoor+floodgate-300-10F+}"
    [ -f "$out" ] || curl -sS --max-time 30 -o "$out" "https://wdoor.c.u-tokyo.ac.jp/shogi/x/$DAY/$f"
  done
```

```sh
# 10판씩 본다. seed 가 표본을 정하고, 바꾸면 새 10판이 나온다.
# **`-run Scan` 이다** — `-run ScanTags` 는 여덟 중 하나만 돌린다
SHOWGI_KIFU_SCAN=1 go test ./internal/kifu/ -run Scan -v
SHOWGI_KIFU_SEED=7 SHOWGI_KIFU_SCAN=1 go test ./internal/kifu/ -run Scan -v

# 상수를 잡을 때만 전부로 넓힌다
SHOWGI_KIFU_SCAN=1 SHOWGI_KIFU_GAMES=341 go test ./internal/kifu/ -run Scan -v
```

> **seed 를 안 고정하면 이 루프가 성립하지 않는다.** 매번 다른 10판을 뽑으면 「고쳐서 나아진 것」과 「표본이 쉬워진 것」을 못 가른다.

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
| `internal/skill`     | 실력 추정. **엔진도 DB도 판도 모른다** — 입력이 낙폭과 「걸렸나」뿐이다   |
| `internal/shogi`     | 룰 엔진 — SFEN, 합법수, 반칙 검증, 棋譜 표기                              |
| `internal/usi`       | 엔진 프로세스 풀. MultiPV·깊이별 평가치·詰み 탐색                         |
| `internal/archive`   | **모든 탐색을 데이터로 만든다** — `positions`·`edges` (§37)               |
| `internal/store`     | postgres (pgx + sqlc). `db/` 는 생성물이라 손대지 않는다                  |
| `internal/tag`       | 囲い·전법·戦型·手筋의 이름. **엔진도 DB도 모른다** — 국면과 수순만 받는다 |
| `internal/auth`      | Google OAuth와 **서명 쿠키**. 세션이 표가 아니다 — 마이그레이션이 없다    |
| `internal/book`      | 상대의 진형 4종 수순. **후보 생성이 아니라 선택만** 한다                  |
| `internal/quiz`      | 되짚기 퀴즈의 생성과 채점. **채점은 저장된 트리라 엔진 0회**              |
| `internal/kifu`      | KIF·CSA 파서와 실 기보 임포트. **서버는 안 쓴다** — `cmd/importkifu` 만   |
| `cmd/importkifu`     | 실 기보를 같은 판정 경로로 다시 둬 DB에 넣는다. 플래그·배선뿐             |

> **`internal/kb` 는 없고 앞으로도 안 만든다.** RAG는 `store` 의 태그 질의(`query/kb.sql`)와 `explain.WithKnowledge` 콜백 둘로 끝났다([docs/06-status.md §43](../../docs/06-status.md)) — 검색이 벡터가 아니라 `kb_chunks.tags` 의 GIN 인덱스라 담을 로직이 없다. 여기 「아직 없는 것」으로 적혀 있던 자리다.

`go.mod`는 레포 루트가 아니라 여기 있다. `apps/web`이 Node 워크스페이스라 루트를 한쪽 언어에 내주지 않으려는 것이고, 대신 Go 명령은 전부 이 디렉터리에서 돌린다.

## 코드를 고치기 전에 — 헤매는 자리는 정해져 있다

문서를 안 보고 코드만으로 이 서버를 읽혀 봤고, 그때 틀리게 믿은 것들이 아래다. 넷 다 **사실이 적힌 파일과 사람이 먼저 여는 파일이 다르다**는 하나의 모양이다.

### ① 프로덕션 상대는 `adaptive.go` 하나뿐이다

`opponent.go` 의 `NewEngineOpponent` 은 **테스트만 쓴다.** 이름과 주석 길이 때문에 그쪽을 먼저 열게 되지만, `cmd/api` 가 배선하는 것은 `NewAdaptiveOpponent` 뿐이다.

**상대를 약하게 하려면 밴드를 올린다**(`adaptive.go` 의 `DefaultBand`, 또는 `OPPONENT_BAND_LO/HI`). 깊이는 **지연** 손잡이이지 강함 손잡이가 아니고, `intervene.Level.Threshold` 는 **개입 빈도**이지 상대 실력이 아니다.

**`LoCp` 가 실제 손잡이다.** 그것이 「한 수에 최소 얼마를 양보하나」의 바닥이 되고, `HiCp` 는 그 바닥을 **절대 좌표로 읽을지 지금 형세에 얹을지**를 가르는 경계다(docs/06-status.md §55).

### ② 세션 goroutine을 떠나는 곳은 넷뿐이다

상태는 세션 goroutine 하나가 소유하고 잠금이 없다. 느린 일만 밖으로 나가며, 전부 `searchGen` 을 들고 나가 **돌아왔을 때 세대가 다르면 버려진다.**

| `internal/game/session.go` | 밖에서 하는 일            |
| -------------------------- | ------------------------- |
| `startJudging`             | 판정 + **개입 문장 생성** |
| `maybeThink`               | 상대 수 탐색              |
| `maybeGauge`               | 詰み 게이지               |
| `maybeTesujiHint`          | 手筋 제안 후보            |

나머지는 전부 goroutine 안이다 — `computeTagHints` 도 여기 있다(엔진을 안 부르므로).

> **개입 문장은 `applyVerdict` 가 아니라 판정 goroutine 안에서 만들어진다.** 카드가 뜨기 **전**이고, 그래서 `explain.Deadline` 이 카드 지연에 그대로 더해진다.

### ③ nil이면 죽지 않고 그 기능만 꺼진다

`Options` 의 의존은 전부 nil을 받는다. **프로세스를 죽이지 않는 것이 의도다** — 죽이면 ECS가 재시작을 반복해 사이트 전체가 내려간다.

| nil         | 꺼지는 것                                    |
| ----------- | -------------------------------------------- |
| NewOpponent | `/ws/game` 이 503. 되짚기는 그대로           |
| NewAnalyst  | 개입 없이 대국만                             |
| Mate        | 詰み 게이지                                  |
| Search      | 가정 수순 · 手筋 힌트                        |
| Store       | 기록과 캐시 (대국은 된다)                    |
| Explainer   | LLM 문구 → **결정적 일본어 템플릿**으로 대체 |

### ④ 카테고리를 하나 더하면 다섯 곳이다

**어느 하나를 빠뜨려도 컴파일도 테스트도 안 깨지고, 화면이 조용히 미분류로 떨어진다.**

`intervene/category.go`(상수 + `classify` 의 **순서 있는** switch) → `explain/render.go` → `explain/label.go` → `explain/prompt.go` → `explain/facts.go` 의 `used()` → 테스트의 `allCategories`.

> **`promptVersion` 은 올리지 않는다.** 그건 **문장이 달라질 때만**이다. 카테고리를 더했다고 올리면 `004_explain_cache_tier1.sql` 의 21행이 통째로 죽어, 무료였던 설명이 다시 돈을 쓴다([§38](../../docs/06-status.md)).

### WebSocket 메시지

한 연결이 대국 하나. **서버가 먼저 말을 건다**(상대의 수·개입) — 요청/응답이 아니다.

| 받는 것                 | 보내는 것                                                                                                                       |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `move` `{usi}`          | `snapshot` — **언제나 전체 상태**(부분 갱신 없음)                                                                               |
| `undo`                  | `snapshot`. 待った — 판당 `game.UndoMaxPerGame`(3)회, 자기 수와 상대의 응수까지 2手를 되감는다 ([§72](../../docs/06-status.md)) |
| `resign`                | `error` `{reason, message}`                                                                                                     |
| `whatif` `{ply, moves}` | `whatif` / `whatif_error`                                                                                                       |

그 외 타입은 `bad_move` 로 거절된다. `reason` 은 기계용 영어 코드이고 `message` 가 화면에 나가는 일본어다.

> **「무를 수 있나」를 화면이 다시 짓지 않는다.** 스냅샷이 `undoLeft` 와 `canUndo` 를 둘 다 싣는다 — `yourTurn && undoLeft > 0` 으로는 「되돌릴 자기 수가 아직 없는 첫 手」가 안 걸러진다.

## 알아두면 좋은 것

- **탐색은 깊이로만 건다.** 시간(`go movetime`)을 쓰지 않아서 `usi` 패키지에 그 API가 아예 없다. 이유는 [CLAUDE.md](../../CLAUDE.md)에 있다
- **엔진 실행 경로(`ENGINE_CMD`)를 태스크 정의에 두지 않는다.** 이미지 내부 구조라 두 곳에 적으면 조용히 어긋난다 — 실제로 한 번 물렸다(docs/06-status.md §11)
- **엔진 풀이 둘이고 크기 손잡이도 둘이다.** 탐색부는 `ENGINE_POOL_SIZE`(기본 3), 詰将棋 solver 는 `ENGINE_MATE_POOL_SIZE`(기본 2). 다른 바이너리이고 잡히는 이유도 달라서 갈라 뒀다 — solver 쪽은 종반 판정·詰み 게이지에 **되짚기 퀴즈 생성**이 얹혀 있고, 그것이 판이 끝나는 자리에서 수십 초를 잡는다(docs/06-status.md §53)
- 대국 세션은 **서버 메모리에 있고 연결에 매여 있다.** 배포하면 진행 중인 대국이 끊긴다
