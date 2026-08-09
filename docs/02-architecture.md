# 아키텍처 — 기술 결정 · 데이터 모델 · 보안

## 1. 기술 결정 (확정)

| 항목     | 결정                                              | 근거                                                                 |
| -------- | ------------------------------------------------- | -------------------------------------------------------------------- |
| 서버     | **Go 단일 서비스**                                | §2                                                                   |
| 프론트   | React + TS + Vite                                 | 확정 사항. 판 렌더는 새로 쓴다 (§8)                                  |
| 3D       | three.js, **정사영 + 2컷 한정**                   | [프론트엔드](03-frontend.md)                                         |
| DB       | **PostgreSQL 단일. 그래프 DB 쓰지 않는다**        | §4                                                                   |
| 엔진     | **やねうら王 + 水匠5**. `ENGINE_CMD`로 교체 가능  | §3                                                                   |
| LLM      | OrcaRouter (`model="auto"`, `temperature=0`)      | 해커톤 요구사항. [LLM 계층](04-llm.md)                               |
| 인증     | **Google OAuth만**                                | LINE은 심사 서류·채널 개설에 시간이 든다. 여유가 나면 D6에 추가      |
| 배포     | AWS ECS Fargate + ALB, Terraform, Route53         | §6                                                                   |
| 모노레포 | pnpm 워크스페이스 + `apps/server`(Go 별도 go.mod) | `../more-more`와 동일 구조. oxfmt/oxlint, `.githooks`, CI까지 그대로 |

---

## 2. 서버 언어를 Go로 하는 이유

Node도 후보였다. Node의 유일한 이점은 LLM SDK인데, **그 코드는 100줄이 안 된다** — OrcaRouter는 OpenAI 호환 HTTP라 Go에서 그냥 POST면 끝난다.

반대로 Go를 버리면 잃는 것이 크다.

1. **USI 엔진은 stdin/stdout 하위 프로세스다.** 프로세스 감시·재기동·타임아웃·동시 탐색이 전부 동시성 코드고, 이 제품은 엔진을 3개 이상 동시에 굴린다([선행 계산](01-core.md#4-레이턴시--플레이어가-고민하는-동안-계산한다)).
2. **이식할 수 있는 검증된 코드가 Go에 있다.** `../shogi`에 테스트까지 있는 룰엔진·USI 브리지·낙폭 분석이 이미 존재한다. 7일 일정에서 이게 결정적이다.
3. 세션 상태를 goroutine 하나가 소유하는 구조가 롤백 정합성과 잘 맞는다 (§5).

---

## 3. 엔진 선택

**やねうら王(v9.40) + 水匠5.** 이미지 안에서 소스로 빌드한다(arm64, 약 50초).

fairy-stockfish로 시작했다가 갈아탔다. 이유는 강함이 아니라 **`go mate`가 없어서**다 — 그쪽은 `go mate`를 조용히 무시하고 일반 탐색으로 떨어져서, 詰み 게이지가 그럴듯한 쓰레기를 표시하게 된다. 원래 판단 기준이 _"핵심은 절대 강함이 아니라 MultiPV 후보 배열과 mate 탐색"_ 이었는데, **그 둘 중 하나가 없다는 것을 실측으로 확인**한 시점에 근거가 사라졌다.

|               |                                               |
| ------------- | --------------------------------------------- |
| 탐색부        | `YANEURAOU_ENGINE_NNUE` — MultiPV·평가        |
| **詰み 탐색** | **`YANEURAOU_MATE_ENGINE` — 별도 바이너리다** |
| 평가함수      | 水匠5 (`nn.bin`, 63MB). `FV_SCALE=24`         |

**`go mate`는 탐색부가 아니라 詰将棋 solver 에디션이 답한다.** 같은 소스를 다른 `YANEURAOU_EDITION`으로 두 번 빌드해야 하고, 탐색부에 `go mate`를 보내면 YaneuraOu에서도 `bestmove`가 돌아온다. 실측으로 확인했다 — solver 쪽은 규격대로 `checkmate G*5b`를 준다.

라이선스는 **GPLv3**(やねうら王·NNUE 모두). 水匠5는 やねうら王 릴리스에서 무료로 공개된 단체 파일이다 — 최신 水匠은 지원자 전용 배포라 쓸 수 없다. 엔진은 파이프 너머의 별도 프로세스라 **GPL이 우리 코드로 전파되지 않는다.** 다만 이미지에 바이너리를 담는 것은 배포이므로 어느 태그를 빌드했는지 Dockerfile에 남긴다.

`ENGINE_CMD`로 실행 경로를 바꿀 수 있는 구조는 그대로다.

---

## 4. 데이터 모델 — 그래프 DB를 쓰지 않는 이유

원래 요구는 이랬다.

> 노드 = 국면, 간선 = 한 수. 간선에 ① 그 수순에서 나오는 囲い/전법 태그(배열) ② **탐색 깊이별 평가치 배열**을 저장.

이건 그래프 DB가 아니라 **인접 리스트 두 테이블**이면 끝난다. 필요한 질의가 "이 국면의 자식 수들"(1-hop)과 "루트부터 N수 재생"(경로가 이미 배열로 있음)뿐이고, **가변 길이 경로 탐색이 없다.** Neo4j를 넣으면 Terraform·백업·인증·JVM 메모리가 통째로 따라오는데 7일 일정에서 되돌려받는 것이 없다.

```sql
-- 국면 = SFEN(手数 제외)을 키로. 전치(transposition)가 자연히 합쳐진다
positions (
  sfen_key       text primary key,
  side_to_move   char(1),
  ply_hint       int,
  candidates     jsonb,          -- MultiPV 상위 k: [{usi, cp, pv}]
  computed_depth int,            -- 더 얕게 계산한 결과로 덮어쓰지 않는다
  created_at     timestamptz default now()
);

-- 간선 = 한 수
edges (
  parent_key     text references positions,
  usi            text,
  child_key      text references positions,
  tags           text[],         -- ['mino','bougin','ryoudori'] — 이 수로 성립한 태그
  eval_by_depth  int[],          -- [d1, d2, ... d14] 선수(sente) 관점 cp
  primary key (parent_key, usi)
);
create index on edges using gin (tags);
```

### `eval_by_depth`는 공짜로 얻는다

USI 엔진은 iterative deepening 중 `info depth 1 score cp … / info depth 2 …`를 계속 뱉는다. **`go` 한 번의 info 라인을 깊이별로 주워담으면 그게 곧 이 배열이다.** 별도 탐색을 14번 돌릴 필요가 없다.

**이 배열은 표시용 데이터가 아니라 개입 판정의 입력이다.** 초보자는 깊게 읽지 않으므로, 얕은 평가와 깊은 평가의 차이가 곧 **"초보자에게 보이는 것과 실제의 격차"**다. 그 격차의 부호가 양쪽 개입을 그대로 정의한다.

| shallow (d2) | deep (d14) | 정체                        | 개입            |
| ------------ | ---------- | --------------------------- | --------------- |
| 좋아 보임    | 실은 나쁨  | **함정** — 얕은 이득에 낚임 | 제지형 (블런더) |
| 나빠 보임    | 실은 좋음  | **手筋** — 捨て駒·踏み込み  | 제안형 (힌트)   |

하나의 배열이 개입의 두 방향을 동시에 정의한다. 조건 판정도, 설명 문장도(「여기까지만 보면 이득입니다」), [리뷰 화면 스파크라인](03-frontend.md#3-리뷰-화면)도 전부 이 배열 하나에서 나온다. 자세한 조건은 [개입 엔진 §7.1](01-core.md#71-어떤-手筋을-알릴-것인가--여기가-제품의-감각이다).

> **단 얕은 값은 MultiPV info 라인에서 못 줍는다.** 捨て駒는 얕은 깊이에서 상위 k에 들지 못해 애초에 라인에 안 나온다 — 손해로 보이는 것이 그 수의 정의다. shallow는 그 수를 둔 국면을 따로 depth 2로 평가해서 얻는다.

> **단 엔진이 깊이별로 찍어주게 만들어야 한다.** YaneuraOu의 `PvInterval` 기본값은 300(ms)이고, 그 간격으로만 PV를 찍는다. 우리 탐색은 그보다 빨리 끝나므로 **마지막 깊이 하나만 남는다** — 이 배열이 통째로 사라진다. `PvInterval=0`으로 껐고, 이건 배포 설정이 아니라 파서가 동작하기 위한 조건이라 핸드셰이크에 넣었다. 엔진을 또 바꾸면 같은 종류의 옵션이 있는지 먼저 본다.

> **단 이식해 오는 파서는 이걸 버린다.** `../shogi`의 `usi.parseScore`는 `depth` 필드를 아예 읽지 않고, 같은 multipv 순위의 라인을 계속 **덮어쓴다** — 마지막(가장 깊은) iteration만 남는다. 깊이별 배열을 얻으려면 `SearchLine`에 `Depth`를 추가하고 순위별로 append하도록 고쳐야 한다. 공짜인 것은 **엔진의 출력**이지 저쪽 코드가 아니다.

### 나머지 테이블

```sql
users        (id, provider, provider_uid, display_name, created_at)
games        (id, user_id, my_color, started_at, result, opening_tag, root_key)
game_moves   (game_id, ply, usi, sfen_key, eval_cp, intervened bool, retracted_usi text)
             -- retracted_usi = 개입으로 물러진 "원래 두려던 수" = 순수 실력 신호
interventions(id, game_id, ply, kind, category, delta_win, level_bucket,
              retracted_usi, hinted_tag, taken bool, explain_tier, cost_yen)
             -- kind: 'blunder'(제지형, 착수 후 롤백) | 'tesuji'(제안형, 착수 전 알림)
             -- retracted_usi는 blunder만, hinted_tag/taken은 tesuji만 채운다
skill_profile(user_id, rating_est, rating_sd, weakness jsonb, updated_at)
explain_cache(key text primary key, body text, model text, hits int)
             -- key = hash(category, level_bucket, piece, 국면특징 버킷)
kb_chunks    (id, title, body, tags text[], source_url, source_license, verified_by,
              embedding vector(1536))  -- pgvector. 출처 없는 chunk는 프롬프트에 붙이지 않는다
```

---

## 5. 시스템 구성

```
                  브라우저 (React + three.js)
                        │  WebSocket(대국) + REST(그 외)
                        ▼
   ┌──────────────────────────────────────────────┐
   │  Go API (apps/server)                        │
   │                                              │
   │  game      대국 세션 상태머신 (goroutine 1/세션)│
   │  intervene 개입 판정 — 임계치·롤백·카테고리     │
   │  coach     적응형 상대 수 선택 (지도 대국)      │
   │  tag       囲い·전법·手筋 패턴 감지            │
   │  usi       엔진 프로세스 풀 (MultiPV, mate)    │
   │  profile   실력 추정 · 약점 프로파일           │
   │  llm       OrcaRouter 클라 + 3단 캐시          │
   │  store     pgx                                │
   └───────┬───────────────────────┬──────────────┘
           │ stdin/stdout          │
   ┌───────▼─────────┐     ┌───────▼────────┐
   │ USI 엔진 N개     │     │  PostgreSQL     │
   │ (やねうら王+水匠5)│     │  (+ pgvector)   │
   └─────────────────┘     └────────────────┘
```

**엔진은 풀로 띄운다.** 최소 3개 — ① 상대 수 결정 ② 플레이어 후보 선행 계산 ③ mate 탐색(詰み 게이지).

> ③은 **다른 바이너리**다(§3). 같은 풀의 다른 슬롯이 아니라 詰将棋 solver 에디션을 따로 빌드해 띄운다. 스레드는 엔진당 1로 고정한다 — 동시성은 풀에서 얻고, 멀티스레드는 고정 깊이에서도 결과가 흔들려 `positions` 캐시를 못 쓰게 만든다.

**세션당 goroutine 하나가 상태를 소유한다.** 입력은 채널 fan-in, 출력은 스냅샷 broadcast. 롤백이 있는 이상 **상태 변경 순서가 곧 제품 정합성**이라 mutex로 얼버무리지 않는다.

---

## 6. 인프라

- **ECS Fargate**(ARM64) 태스크 하나에 web(Caddy) + api 컨테이너. 앞에 ALB가 ACM 인증서로 TLS를 끝낸다
- **RDS postgres 17.** 앱 태스크의 보안그룹에서만 접근 가능하고, 7일 자동 백업이 붙는다
- 비밀은 SSM Parameter Store → 태스크 정의의 `secrets`로 주입. 디스크에 남지 않는다
- Terraform으로 전부 코드화. state는 S3 + DynamoDB 잠금
- 레포는 **퍼블릭**

EC2 + docker compose로 시작했다가 갈아탔다. 비용은 거의 같은데(주 $2 차이), **직접 쓴 배포 글루가 통째로 사라진다** — 배포 스크립트, 비밀을 셸로 내보내는 스크립트, compose 오버레이, 헬스체크 루프, 인증서 볼륨이 전부 ECS·ALB의 기본 기능으로 대체된다. 직접 쓴 것만 직접 유지보수해야 한다.

**관리할 서버가 없다.** SSH 포트도, 패치할 OS도, 접속 키도 없다. 디버깅이 필요하면 ECS Exec으로 컨테이너에 들어간다.

state는 S3 + DynamoDB 잠금에 둔다. 로컬 state는 날리면 복구가 안 되고, **퍼블릭 레포에서는 실수로 커밋될 위험**도 있다.

절차와 부트스트랩은 [deploy/README.md](../deploy/README.md).

---

## 7. 보안 — 도메인 고유의 실재 위협

"HTTPS 씁니다, API 키 숨겼습니다"로 때우지 않는다. 이 앱에는 진짜 위협이 둘 있다.

**위협 1: 잘못 쓰면 부정행위 도구다.**
온라인 쇼기 플랫폼은 전부 대국 중 소프트 참조를 금지한다(lishogi 명시).
→ 개입은 **AI 연습 대국 한정.** 외부 대국 화면 오버레이가 불가능한 구조. 개입이 켜진 대국은 레이팅 비활성.

**위협 2: 실력 프로필은 민감 정보다.**
"이 사람은 종반에 약하다"는 데이터가 대인전 상대에게 넘어가면 안 된다.
→ `skill_profile`은 본인만 조회. 공개 API에 노출하지 않는다.

---

## 8. `../shogi`에서 이식할 목록

실제로 코드를 열어보고 정한 판정이다. **통째로 가져올 것은 셋뿐이고, 나머지는 참고이거나 안 가져온다.**

### 통째로 가져온다

| 무엇                 | 경로                            | 근거                                                                                                                                                                                                      |
| -------------------- | ------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **룰 엔진**          | `server/internal/shogi/*`       | 합법수 생성·`ValidateMove`·반칙 전부(二歩, 打ち歩詰め, 行き所のない駒, 王手放置, 강제 승격) + `RepetitionKey`(千日手) + 말 개수 검증. **테스트 13개가 반칙 케이스를 직접 찍는다.** 처음부터 쓰면 이틀치다 |
| **USI 클라이언트**   | `server/internal/usi/client.go` | 값어치는 기능이 아니라 **방어 코드**다 — fail-high/low 속보 라인 무시, 잘린 PV가 완전한 PV를 덮어쓰지 않게 하는 처리, `USI_Variant` 강제, 프로세스 재기동+옵션 복원. 전부 한 번씩 물려본 흔적이다         |
| **기보 일본어 표기** | `server/internal/shogi/kifu.go` | `MoveJa`가 USI를 「▲7六歩」「同 銀成」「打」로 바꾼다. **출력이 원래 일본어라 UI에 그대로 나간다** — 저쪽이 한국어 앱이었는데도 이건 손댈 데가 없다                                                       |

### 고쳐서 쓴다

| 무엇                        | 무엇을 고치나                                                                                                                                                                    |
| --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `usi.Engine`의 동시성       | `Engine`이 mutex로 탐색을 직렬화한다. 프로세스 1개 = 동시 탐색 1개. **`Engine` N개를 감싸는 풀을 새로 쓴다** — `Engine` 자체는 그대로                                            |
| `usi.parseScore`            | `depth`를 안 읽고 순위별로 덮어쓴다 → `eval_by_depth`가 안 나온다 (§4)                                                                                                           |
| mate 탐색                   | `Search`/`SearchDepth`뿐이라 `go mate`가 없다. 詰み 게이지용으로 추가                                                                                                            |
| `analysis`의 부호·병렬 구조 | `sentePov`/`moverPov`(선수 관점 부호 고정)와 워커 병렬화는 가져온다. **판정 로직은 버린다** — 저쪽은 cp 낙폭 300/800 고정, 우리는 승률 낙폭 × 레벨별 임계치라 계산 자체가 다르다 |
| `src/data/*`                | 수순·좌표·태그만. 설명문은 한국어라 전부 버리고 일본어로 새로 쓴다 (§ 아래)                                                                                                      |

### 안 가져온다

| 무엇                                           | 왜                                                                                                                                                                  |
| ---------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `src/lib/moves.ts`, `src/components/Board.tsx` | 좌표계가 다르고(row/col + 자체 `BoardState`), Board는 **학습용 샌드박스**라 대국용이 아니다. 코드에도 "대국용과 다른 자체 타입"이라 적혀 있다. 합법수는 서버가 준다 |
| `server/internal/swars/`                       | 将棋ウォーズ 스크래핑. 이 제품에 필요 없고, **외부 대국 연동은 보안 §7 위협 1과 정면으로 어긋난다**                                                                 |
| `src/pages/Ch0~16`                             | 한국어 강의 콘텐츠. 이 제품은 강의가 아니다                                                                                                                         |
| `deploy/terraform/` (EC2·EIP 구성)             | 우리는 ECS Fargate라 자원 구성이 겹치지 않는다                                                                                                                      |
| **`src/data/*` 전부**                          | 囲い·전법·手筋 데이터. **한 줄도 쓰지 않는다** — 원전이 개인 블로그이고, 手筋 208문은 시판 서적 디지털화다. 퍼블릭 레포에서는 신뢰성 이전에 저작권 문제다           |

참고만 할 것: `src/components/Koma.tsx`(기물 한 글자 렌더), 판 그리드 CSS.

> `../shogi`의 `server/.env`, `deploy/.env.prod`, `terraform.tfstate`는 **절대 딸려오지 않게 한다.** 이 레포는 public이다.

**지식 데이터는 전부 새로 만든다.** 수순은 공개 정석 파일 + 자체 엔진 검증, 서술문은 공식 자료와 라이선스가 명확한 소스에서 일본어로 새로 쓴다. 신뢰 계층과 `kb_chunks` 스키마는 [LLM 계층 §4](04-llm.md#4-rag--코퍼스는-처음부터-새로-만든다).
