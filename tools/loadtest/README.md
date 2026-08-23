# 부하 회차 도구

**동시에 몇 판까지 되나**를 재는 자리다([06-status.md](../../docs/06-status.md) §5의 열린 물음). 근거와 결정은 [journal §103](../../docs/journal/101-120.md).

k6 를 쓴다. **규칙 엔진이 필요 없다** — 서버가 스냅샷마다 `legalMoves` 를 통째로 주므로 그중 하나를 뽑으면 판이 진행된다.

```
main.js      회차의 입구. MODE 로 시나리오를 고른다
engine.js    엔진 대국 (/ws/game, 쿠키는 선택)
match.js     대인전 (/api/queue → /ws/match, 로그인 필요)
lib/         손잡이·지표·쿠키·수 고르기
seed.sql     provider='loadtest' 사용자와 레이팅
cleanup.sql  회차가 끝나면 돌린다 (한 줄)
```

## 준비

```sh
brew install k6

# 사용자를 만든다. 번호가 마지막에 찍힌다 — 그 목록을 LT_UIDS 로 넘긴다
docker exec -i show-gi-db psql -U showgi -d showgi -v ON_ERROR_STOP=1 -v n=8 < tools/loadtest/seed.sql
```

**쿠키는 도구가 굽는다.** 세션은 서버에 짝이 없는 서명 쿠키라(`internal/auth`) `SESSION_SECRET` 만 있으면 Google 왕복 없이 같은 값을 만들 수 있다. 프로덕션 회차에서는 SSM 에서 그때 읽어 환경변수로만 넘긴다 — 디스크에 안 남긴다.

**엔진 대국에도 쿠키를 단다.** `/ws/game` 은 익명으로도 열리지만, 그러면 `games.user_id` 가 NULL 이라 **회차가 만든 판과 실제 익명 사용자의 판을 구별할 수 없고** `cleanup.sql` 이 그 판에 닿지 않는다. `SESSION_SECRET` 과 `LT_UIDS` 가 둘 다 있으면 붙고, 없으면 익명으로 돌면서 `setup` 이 경고한다.

```sh
export SESSION_SECRET=$(aws ssm get-parameter --name /show-gi/prod/SESSION_SECRET \
  --with-decryption --region ap-northeast-1 --profile show-gi \
  --query Parameter.Value --output text)
```

## 돌리는 법

```sh
# 엔진 대국만. 쿠키를 달면 그 판들을 나중에 지울 수 있다
BASE=http://localhost:8080 MODE=engine VUS_ENGINE=3 DURATION=3m \
  LT_UIDS=5457,5458,5459 SESSION_SECRET="$SESSION_SECRET" k6 run tools/loadtest/main.js

# 대인전만
MODE=match VUS_MATCH=4 LT_UIDS=5457,5458,5459,5460 k6 run tools/loadtest/main.js

# 둘을 동시에 — 서로 굶히는지를 재는 회차다
MODE=both VUS_ENGINE=3 VUS_MATCH=4 LT_UIDS=… k6 run tools/loadtest/main.js
```

| 환경변수                               | 기본값                  | 무엇                                                  |
| -------------------------------------- | ----------------------- | ----------------------------------------------------- |
| `BASE`                                 | `http://localhost:8080` | WS 주소는 여기서 만든다                               |
| `MODE`                                 | `engine`                | `engine` · `match` · `both`                           |
| `VUS_ENGINE` · `VUS_MATCH`             | 3 · 2                   | **VU 하나가 판 하나**다                               |
| `DURATION`                             | `3m`                    | 회차 길이                                             |
| `MAX_PLIES`                            | 100                     | 手数 상한. 넘으면 投了한다. 60은 終盤에 못 갔다       |
| `SEED`                                 | 1                       | 같은 씨앗이 같은 수순을 준다                          |
| `LT_UIDS`                              | —                       | 회차가 쓸 사용자 번호. **VU 합보다 많아야 한다**      |
| `HANDICAP`                             | —                       | `/api/handicaps` 의 id                                |
| `GAME_TIMEOUT_MS` · `STALL_TIMEOUT_MS` | 900000 · 90000          | 멈춤을 잡는 것은 STALL. GAME 은 보루라 안 터져야 한다 |
| `QUEUE_TRIES` · `QUEUE_INTERVAL`       | 30 · 2                  | 대기열을 몇 번, 몇 초마다 물어보나                    |

**VU 하나가 판 하나다.** 이 앱의 부하 단위가 초당 도착률이 아니라 동시 판수라서 `constant-vus` 로 둔다 — 판 하나가 연결 하나를 몇 분 잡는다.

**VU 하나는 사람 하나보다 크다.** 도구는 자기 차례가 된 그 순간 두므로 생각 시간이 0이다. 사람이 한 수에 T초를 쓰면 엔진 수요가 `2.15/(2.15+T)` 로 줄고(2.15초는 동시 1판에서 잰 착수 왕복 평균, [journal §104](../../docs/journal/101-120.md)), **T=3초면 VU 8 이 사람 20명쯤**이다. 그래서 VU 수는 **보수적 상한**으로 읽는다 — VU N 에서 버티면 사람 N 명에서는 확실히 버틴다. 거꾸로는 아니다: 사람 20명은 순간적으로 동시 8판을 넘는 일이 생긴다.

**「깨질 때까지」 올린다.** 회차를 시작하기 전에 깨짐 정의를 적고, 그 신호가 나올 때까지 계단을 만든다 — 5xx·panic 1건 이상 · WS 연결 실패 · `cut=stall` 이 판의 절반 이상. 신호가 0이었던 아래 계단은 다음 회차에서 뺀다. **`GAME_TIMEOUT_MS` 를 판 길이보다 넉넉히 크게 두고, `DURATION` 은 판이 몇 개 완주할 만큼 길게 잡는다** — 시한이 판 길이에 가까우면 `cut=capped` 가 저절로 섞여서 신호를 흐린다. 8판 동시의 100手 판이 7분쯤이므로 계단마다 15분이 필요하다.

**계단 사이에 큐를 비운다.** 앞 계단의 분석 백로그와 열린 세션이 남은 채로 다음을 걸면 그 부하가 섞여서 어느 계단의 숫자인지 알 수 없다. 대시보드의 `AnalysisBacklogPlies` 와 `WsSessionsActive` 가 **0으로 돌아온 뒤에** 다음 계단으로 간다 — 지표 창도 같이 넘어온다(5분 p95 알람이 앞 계단 때문에 울린 적이 있다, [journal §104](../../docs/journal/101-120.md)).

## 도구가 재는 것

서버 지표와 겹치지 않는 것만 둔다. 겹치면 두 숫자가 갈릴 때 어느 쪽이 맞는지 정할 수 없다.

| 지표                      | 무엇                                                                                                                |
| ------------------------- | ------------------------------------------------------------------------------------------------------------------- |
| `showgi_move_cycle`       | 내 수를 보내고 다시 내 차례가 되기까지. **사람이 기다리는 것**                                                      |
| `showgi_queue_wait`       | 짝이 잡히기까지                                                                                                     |
| `showgi_games` · `_plies` | 판 수와 手数                                                                                                        |
| `showgi_interventions`    | 개입 건수 (카테고리 라벨)                                                                                           |
| `showgi_rejects`          | **0이어야 한다.** 하나라도 있으면 도구 버그다                                                                       |
| `showgi_stalls`           | 시한이 끊은 판. **`cut` 으로 갈린다** — `stall`(서버 무응답)만 깨짐 신호이고 `capped`(우리 手数·시간 상한)는 아니다 |

서버 쪽은 [CloudWatch 대시보드](../../docs/journal/101-120.md) 한 장으로 본다. 회차에서 제일 먼저 볼 것은 **`EnginePoolWaitGameSeconds`** 다 — 분석·검토가 섞이지 않은 대국의 대기이고, 그것이 튀면 사람이 기다린 것이다.

## 프로덕션에 걸 때

- **레이트 리밋이 아무 데도 없다**(Caddy·ALB·앱). 막아 줄 것이 없으므로 `http_req_failed` 임계에 `abortOnFail` 이 걸려 있다 — 5xx 가 5%를 넘으면 회차가 스스로 멈춘다
- **태스크가 한 대다.** 부하가 곧 실사용자 장애다
- 알람 셋이 울리고 메일이 간다. 회차 시각을 저널에 적어 둔다
- 끝나면 `cleanup.sql`. `users` 한 줄을 지우면 판·기보·평가치·레이팅·큐가 CASCADE 로 같이 사라진다
- **`LT_UIDS` 없이 걸지 않는다.** 익명 판은 그 정리에 안 걸린다

**부하와 지연을 같은 회차에서 재지 않는다.** 용량은 도쿄 하나에서 계단식으로, 지연은 도쿄+오사카에서 낮은 부하로 — 포화된 서버에서는 리전 간 10ms 차이가 엔진 대기에 삼켜진다.

## 남은 회차

[journal §104](../../docs/journal/101-120.md) 가 엔진 대국 계단 하나를 끝냈다. 순서는 위에서부터다 — 앞의 것이 뒤의 것의 기준선을 만든다.

| 회차                      | 설정                                 | 무엇을 답하나                                                                                                                |
| ------------------------- | ------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------- |
| **깨질 때까지**           | `MODE=engine`, 3판부터 계단, 15분씩  | §104 가 못 찾은 **무너지는 지점**. 8판에서도 5xx·`GamesAborted` 가 0이었다                                                   |
| **수직 스케일 뒤 재측정** | 위와 같은 계단, 4 vCPU               | 2→4 vCPU 가 그 지점을 뚫는가. **뚫은 계단은 다음부터 뺀다**                                                                  |
| **대인전**                | `MODE=match`, 4판                    | **프로덕션에 대인전이 0건이다.** 큐가 짝을 잡나, 레이팅이 움직이나, 실력 추정이 실물로 도나                                  |
| **밴드**                  | `MODE=match`, `seed.sql` 의 700 간격 | 밴드가 대기로 넓어지는가. 100 간격으로는 즉시 붙어서 안 보였다                                                               |
| **섞어서**                | `MODE=both`                          | `borrower` 가 갈리나 — 분석·대국이 같은 2 vCPU 를 다툴 때 누가 굶나. [§101](../../docs/journal/101-120.md)이 지으려던 회차다 |
| **짧은 판**               | `MODE=engine`, `MAX_PLIES=24`        | 완주율을 올리면 **분석 큐가 밀리나.** §104 는 31분에 45판이라 안 밀렸다                                                      |
| **지연**                  | 도쿄 + 오사카, 낮은 부하             | 리전 간 차. **러너 EC2 가 선행**이고, operator IAM 에 VPC 생성과 SSM Run Command 가 없어서 정책 확장부터다                   |
