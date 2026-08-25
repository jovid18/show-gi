# API 명세 — REST 27개와 WebSocket 둘

**이 문서는 「무엇이 어떻게 생겼나」다.** 각 라우트가 **왜** 그 의존성을 갖는지는 [apps/server/README.md](../../apps/server/README.md)의 라우트 표에 있다.

기계용 정본은 [`openapi.yaml`](openapi.yaml) 이다. **WebSocket 은 OpenAPI 가 담지 못하므로 §5·§6이 유일한 명세다.**

|             |                                                                       |
| ----------- | --------------------------------------------------------------------- |
| 기준 주소   | 프로덕션 `https://show-gi.com` · 로컬 `http://localhost:8080`         |
| 인증        | **서명 쿠키 하나**(`HttpOnly`). 토큰 헤더가 없고, Bearer 도 없다      |
| 요청 본문   | `application/json` (POST · PATCH 만)                                  |
| 응답        | 언제나 `application/json`, 단 `204` 와 OAuth 리다이렉트는 본문이 없다 |
| 버전 접두사 | **없다.** `/api/v1` 을 안 붙였다 — 클라이언트가 이 서버 하나뿐이다    |

---

## 1. 두 가지 규약이 응답 전체를 지배한다

**① 실패는 언제나 같은 모양이다.**

```json
{ "error": "not_found", "message": "その対局は見つかりません。" }
```

`error` 는 기계용 영어 코드, `message` 는 **화면에 그대로 나가는 일본어**다. 클라이언트가 코드로 문장을 짓지 않는다 — 짓기 시작하면 어휘가 두 벌이 되고, 한쪽만 고쳐진 채로 남는다.

**② 「없는 기능」과 「지금 못 쓰는 기능」과 「로그인이 필요한 기능」이 갈린다.**

| 상태              | 응답                                  | 뜻                                                                             |
| ----------------- | ------------------------------------- | ------------------------------------------------------------------------------ |
| 경로가 아예 없다  | `404` (Go `ServeMux` 기본)            | 이 배포에 그 기능이 없다 (예: 로그인이 꺼진 배포의 `/api/auth/*`)              |
| DB 가 없다        | `503 store_unavailable`               | 기록을 못 읽는다. 대국은 된다                                                  |
| 엔진이 없다       | `503 engine_unavailable`              | 「그래서 상대가 어떻게 하나」에 답할 수 없다                                   |
| 분석 전용 태스크  | `503 not_served_here`                 | 이 태스크는 사람을 안 받는다(`ROLE=analysis`). `/healthz`·`/metrics` 만 답한다 |
| 로그인 안 했다    | `401 unauthorized` / `login_required` | 로그인하면 되는 자리                                                           |
| 남의 것 · 없는 것 | **같은 `404 not_found`**              | 갈라 주면 번호를 훑어 남의 판의 존재를 알 수 있다                              |

**`404` 를 두 뜻으로 쓰는 것이 의도다.** 「없는 판」과 「남의 판」을 갈라 주면 id 를 하나씩 올려 보며 남의 기록이 있는지 셀 수 있다. `GET /api/rooms/{id}` 가 로그인 안 한 사람에게 `401` 이 아니라 `404` 를 주는 것도 같은 이유다.

---

## 2. 라우트 전체 — 의존성별로 갈린다

「필요」 칸이 그 경로가 무엇 없이 못 서는지다.

### 아무것에도 안 매인 것 (상수 목록 · 헬스)

| 메서드 · 경로        | 필요 | 응답 `200`                                                              |
| -------------------- | ---- | ----------------------------------------------------------------------- |
| `GET /healthz`       | —    | `{ok, engine, db}` — **엔진·DB 가 죽어도 200 이다**                     |
| `GET /metrics`       | —    | Prometheus 텍스트. **밖에서 안 닿는다**(Caddy 가 안 프록시한다)         |
| `GET /api/openings`  | —    | `{openings: [{id, name, note, source}]}` — 상대의 진형 4종              |
| `GET /api/handicaps` | —    | `{handicaps: [{id, name, note}]}` — 手合割 7종. **平手는 목록에 없다**  |
| `GET /api/me`        | —    | `{enabled, user: {id, name} \| null}` — **로그인이 꺼진 배포에도 있다** |

> `/healthz` 가 엔진 없이도 200 인 것은 의도다. 여기서 실패하면 ECS 가 재시작을 반복해 사이트 전체가 내려간다 — 대신 `engine` 필드로 드러내고, 배포 워크플로가 그 값을 확인한다.

### 로그인

| 메서드 · 경로                   | 응답                                                     |
| ------------------------------- | -------------------------------------------------------- |
| `GET /api/auth/google/start`    | `302` → Google. 로그인이 꺼져 있으면 **경로 자체가 404** |
| `GET /api/auth/google/callback` | `302` → `/` (성공·실패 어느 쪽이든). `?code` · `?state`  |
| `POST /api/auth/logout`         | `204`. 서버에는 지울 것이 없다 — 세션이 표가 아니다      |

`state` 는 쿠키와 대조한다. 어긋나면 그대로 `/` 로 돌려보낸다.

### 기록 (DB 필요 · 엔진 무관)

| 메서드 · 경로                      | 인증       | 성공                            | 실패                                       |
| ---------------------------------- | ---------- | ------------------------------- | ------------------------------------------ |
| `GET /api/games`                   | 익명 가능  | `200 {games: [GameSummary]}`    | `400 bad_limit` · `500 internal`           |
| `GET /api/games/{id}`              | 익명 가능  | `200 GameDetail`                | `400 bad_id` · `404 not_found` · `500`     |
| `GET /api/games/{id}/summary`      | 익명 가능  | `200 GameSummaryPayload`        | `404 no_summary`(대인전) · 위와 같음       |
| `GET /api/games/{id}/quiz`         | 익명 가능  | `200 {ready, mate?, best?}`     | `404 not_found` · `500`                    |
| `POST /api/games/{id}/quiz/mate`   | 익명 가능  | `200 MateResponse`              | `400 bad_move` · `404 not_ready`/`no_item` |
| `POST /api/games/{id}/quiz/best`   | 익명 가능  | `200 BestResponse`              | 위와 같음                                  |
| `GET /api/resumable`               | 익명 가능  | `200 {game: Resumable \| null}` | `500 internal`                             |
| `POST /api/resumable/{id}/decline` | 로그인     | `204`                           | `400 bad_id` · `404 not_found`             |
| `GET /api/me/profile`              | **로그인** | `200 ProfilePayload`            | `401 unauthorized` · `500`                 |

- `?limit=` 기본 **20**, 최대 **100**. `0` 이하·정수 아님·`int32` 초과는 `400`
- 목록·상세·총평·퀴즈가 **같은 조건**을 지난다: **결과가 나온 자기 판만.** 두는 중인 판도 `abandoned` 도 `404` 다
- 익명 판은 쿠키 없이도 읽힌다 — 「자기 판」의 정의가 로그인한 사람에게는 `user_id`, 익명에게는 `user_id IS NULL` 이다
- `POST /api/resumable/{id}/decline` 은 익명에게 `404` 다 — 이어할 판이 애초에 없다
- **`404 no_summary` 는 대인전이다.** 총평이 세는 것이 개입인데 대인전에는 그것이 없다
- **`result` 는 이 경로들에서 `win`·`loss`·`draw` 셋뿐이다.** 질의가 그렇게 거르므로 `abandoned`·`declined` 는 여기 안 나온다 — DB 쪽 어휘 전체는 [data-model.md §5](data-model.md)
- **한 수도 안 둔 판은 목록에 없다.** 연결만 열렸다 끊긴 판이 실제로 그렇게 남는데, 되짚을 것이 없는 줄이 맨 위에 오면 진짜 대국이 아래로 밀린다

### 가정 수순 · 검토 (엔진 필요)

| 메서드 · 경로                 | 인증       | 필요      | 실패                                                                                     |
| ----------------------------- | ---------- | --------- | ---------------------------------------------------------------------------------------- |
| `POST /api/games/{id}/whatif` | 익명 가능  | DB + 엔진 | `400 bad_ply`/`bad_move`/`bad_line` · `404` · `503 engine_unavailable`                   |
| `POST /api/explore`           | **로그인** | 엔진      | `400 bad_handicap`/`bad_move`/`bad_line` · `401 login_required` · **`429 busy`** · `503` |

**`429 busy`** 는 엔진 슬롯을 3초 기다려도 안 났을 때다 — 다시 눌러 볼 수 있는 실패라 `503` 과 갈라 뒀다. 이 API 에서 `429` 가 나가는 자리는 여기 하나다.

|            | `whatif`                     | `explore`                             |
| ---------- | ---------------------------- | ------------------------------------- |
| 뿌리       | **DB 의 자기 판** `ply` 手目 | **`handicap` 의 0手目**               |
| 요청       | `{ply, moves[]}`             | `{handicap, moves[]}`                 |
| 한 줄 상한 | 60手                         | **200手** (한 판을 통째로 걸어 본다)  |
| 본문 상한  | 8 KiB                        | 16 KiB                                |
| 로그인     | 불필요 (뿌리가 자기 판이다)  | 불필요 ([§100](../journal/82-100.md)) |
| 후보 k     | 3                            | 3                                     |
| 시한       | 20s                          | 슬롯 대기 3s                          |
| 동시 슬롯  | —                            | **1** (풀 3개 중 2개를 대국에 남긴다) |

**둘 다 로그인이 필요 없고, 이유가 다르다.** 가정 수순은 뿌리가 자기 판이라 기록이 자격을 정하고, 검토는 뿌리가 手合割 표라 **여는 기록이 아예 없다**. 검토에는 로그인 벽이 있었는데 걷었다([§100](../journal/82-100.md)) — 엔진을 지키는 것은 이제 **동시 슬롯 1개**뿐이다.

응답은 둘 다 `WhatifNode` 다(검토는 `handicapJa` · `baselineCp` 두 칸이 더 붙는다). `POST` 인데 아무것도 안 바꾼다 — 본문에 수순 배열을 실어야 해서 `POST` 다.

### 검토에서 저장한 국면 (DB 필요 · 로그인 필요)

| 메서드 · 경로                        | 성공                     | 실패                                                   |
| ------------------------------------ | ------------------------ | ------------------------------------------------------ |
| `GET /api/explore/snapshots`         | `200 {snapshots: [...]}` | `401 login_required` · `503 store_unavailable` · `500` |
| `POST /api/explore/snapshots`        | **`201`** `Snapshot`     | `400 bad_name`/`bad_handicap`/`bad_move`/`bad_line`    |
| `PATCH /api/explore/snapshots/{id}`  | `200 {id, name}`         | `400 bad_id`/`bad_name` · `404 not_found`              |
| `DELETE /api/explore/snapshots/{id}` | `204`                    | `400 bad_id` · `404 not_found`                         |

- **판(SFEN)을 안 받는다.** `{name, handicap, moves[]}` 뿐이고, 저장 전에 룰 엔진으로 한 수씩 되짚어 본다(합법성 검사라 엔진 슬롯을 안 잡는다)
- `name` 은 **40 rune** 까지 — 바이트가 아니라 rune 으로 센다. 비면 서버가 手数로 하나 짓는다
- `PATCH` 는 **이름만** 고친다. 수순을 같은 행에 덮어쓰면 옛 이름이 가리키던 자리가 조용히 다른 국면이 된다
- **개수 상한도 이름의 UNIQUE 도 없다** — 목록에 `LIMIT` 을 두면 지울 수 없는 행이 생긴다
- 여기만 `503` 이 `401` 보다 늦게 온다: 검토 자체는 기록 없이도 서지만 저장은 그럴 수가 없다

### 대인전 (룰 엔진과 시계뿐 · 로그인 필요)

| 메서드 · 경로         | 성공                                             | 실패                                   |
| --------------------- | ------------------------------------------------ | -------------------------------------- |
| `POST /api/rooms`     | `200 {id, yourColor, hostName, waiting, isHost}` | `401 unauthorized`                     |
| `GET /api/rooms/{id}` | `200` 같은 모양                                  | **`404 not_found` 하나** (로그인 포함) |

`?color=b`(先手) · `w`(後手) · `r`(振り駒). **振り駒는 서버가 뽑는다** — 클라이언트가 뽑아 `b`/`w` 로 보내면 마음에 안 드는 결과를 다시 뽑을 수 있고, 그러면 振り駒가 아니라 그냥 고르는 것이다.

`GET` 은 **자리를 잡지 않는다.** 앉는 것은 `/ws/match` 다 — 링크를 잘못 누른 사람이 자기도 모르게 자리를 차지하면 정원 2명인 그 방은 아무도 못 들어간다.

### 대인전 대기열 (DB 필요 · 로그인 필요)

| 메서드 · 경로       | 성공                                                                                         | 실패                                         |
| ------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------- |
| `POST /api/queue`   | `200 {status:"waiting", waitedMs, waiting}` 또는 `200 {status:"matched", roomId, yourColor}` | `401 unauthorized` · `503 store_unavailable` |
| `DELETE /api/queue` | `204`                                                                                        | `401 unauthorized` · `503 store_unavailable` |

**`POST` 하나가 셋을 한다** — 줄에 서기 · 살아 있다고 알리기 · 짝짓기. 멱등이라 화면은 이것을 2초마다 부르기만 하고, 멈추면 서버가 12초 뒤 줄에서 걷어낸다(`queue.StaleAfter`).

**대인전의 다른 셋과 달리 DB 에 매여 있다.** 줄이 표에 있어야 모든 인스턴스가 같은 줄을 본다 — 기록이 없는 배포에서는 방은 열리는데 줄은 `503` 이다.

**상대에 대해 아무것도 안 준다.** 짝이 잡힌 답에 이름조차 없다 — 이름은 방에 붙으면 `MatchSnapshot` 이 주고, 레이팅은 어느 쪽으로도 안 나간다.

`matched` 는 **한 번만 나간다.** 읽는 것과 지우는 것이 한 문장이라(`TakeQueueSeat`) 다시 물어보면 그 사람은 줄에 새로 선다.

---

## 3. 주요 응답 스키마

`?` 는 없을 수 있는 칸이다. **`omitempty` 가 아니라 포인터인 칸은 「0 과 없음이 다르다」는 뜻**이다 — cp 의 0 은 호각이라는 실제 값이다.

### `GameSummary` (목록의 한 줄)

```json
{
  "id": 42,
  "myColor": "b",
  "startedAt": "2026-08-22T10:00:00Z",
  "finishedAt": "2026-08-22T10:31:00Z",
  "result": "win",
  "moveCount": 71,
  "interventionCount": 4,
  "handicapJa": "二枚落ち",
  "isMatch": false,
  "analyzing": false
}
```

| 칸                      | 주의                                                                                      |
| ----------------------- | ----------------------------------------------------------------------------------------- |
| `finishedAt` · `result` | 구조체는 없을 수 있게 생겼지만 **이 두 경로에서는 언제나 온다** — 결과가 나온 판만 거른다 |
| `handicapJa`            | 平手면 안 온다. **화면이 이름을 만들지 않는다** — 표는 서버에 있다                        |
| `isMatch`               | 대인전. 화면이 이 값으로 총평과 퀴즈를 닫는다 — 그 둘은 **0 이 아니라 없다**              |
| `analyzing`             | 대인전 판의 평가치를 지금 채우는 중. 「분석 중」과 「남지 않았다」를 가른다               |

### `GameDetail`

`GameSummary` + 네 칸.

```json
{
  "startSfen": "lnsgkgsnl/...",
  "baselineCp": 1386,
  "moves": [{ "ply": 1, "usi": "7g7f", "ja": "▲7六歩", "by": "human", "sfen": "...", "evalCp": 12, "checked": "" }],
  "interventions": [
    {
      "ply": 3,
      "kind": "blunder",
      "category": "hangs_piece",
      "categoryJa": "…",
      "message": "…",
      "deltaWin": 0.31,
      "levelBucket": "beginner",
      "retractedUsi": "8h3c+",
      "retractedJa": "▲3三角成",
      "afterCp": -820,
      "bestCp": 40
    }
  ],
  "undos": [{ "ply": 5, "usi": "3c3d", "ja": "△3四歩", "evalCp": -60 }]
}
```

- **`baselineCp` 는 상세에만 있고 목록에 없다.** 쓰는 곳이 형세 그래프와 후보 줄의 색뿐이다 — 빼면 駒落ち 판의 곡선이 천장에 붙어 어디서 흘렸는지가 안 보인다
- `by` 는 `"human"` \| `"engine"` — **`"b"`/`"w"` 가 아니다**
- `moves[].evalCp` 는 **플레이어 관점** cp 다. DB 는 先手 관점으로 저장하고 여기서 뒤집는다
- `moves[].sfen` 이 비어 있을 수 있다 — 기록이 중간에 끊겼으면 거기서부터 재현이 멈춘다. 그래도 그 수를 목록에서 빼지 않는다 (둔 것은 둔 것이다)
- **`interventions[].ply` 의 수는 `moves` 에 없다.** 그 국면을 보려면 `ply - 1` 手目의 판을 그린다 — `undos` 도 같은 규약이다

### `GameSummaryPayload` (총평)

```json
{
  "body": "…（日本語の総評）",
  "gameId": 42,
  "stats": {
    "playerMoves": 36,
    "interventions": 4,
    "categories": [{ "code": "hangs_piece", "nameJa": "…", "count": 2 }],
    "focus": { "ply": 23, "category": "missed_mate", "nameJa": "…" }
  },
  "skill": { "before": { "step": 3, "max": 9, "nameJa": "10級" }, "after": { "step": 4, "max": 9, "nameJa": "9級" } }
}
```

- `gameId` 는 **대국 화면이 부를 때만** 온다. 되짚기에서는 0 이다 — 이미 그 판을 열고 있다
- `skill` 은 **되짚기에서 언제나 `null`** 이다. 추정치는 사람에게 붙는 값이라 지난 판을 여는 지금은 이미 다른 값이고, 그때의 값을 판마다 저장하지 않았다
- `skill.before` 는 첫 판이나 익명이면 없다

### `ProfilePayload`

```json
{
  "name": "…",
  "rank": { "step": 4, "max": 9, "nameJa": "9級" },
  "record": { "games": 12, "win": 5, "loss": 6, "draw": 1 },
  "interventions": 31,
  "weaknesses": [{ "code": "hangs_piece", "nameJa": "…", "count": 9, "share": 0.29 }],
  "styles": [{ "code": "hon_mino", "nameJa": "本美濃囲い", "kind": "castle", "games": 4 }]
}
```

**집계 셋(`record` · `weaknesses` · `styles`)이 대인전을 뺀다**(`match_id IS NULL`). 상대와 둔 판이 그 사람의 「崩れやすいところ」를 흔들지 않는다. `rank` 는 아직 잰 적이 없으면 없다.

### `WhatifNode` (가정 수순 · 검토 공용)

```json
{
  "basePly": 23,
  "ply": 25,
  "sfen": "…",
  "turn": "b",
  "yourTurn": true,
  "checked": "5a",
  "status": "playing",
  "legalMoves": ["7g7f", "..."],
  "evalCp": -120,
  "mateIn": 0,
  "line": [{ "ply": 24, "usi": "…", "ja": "…", "by": "human", "sfen": "…" }],
  "candidates": [{ "usi": "…", "ja": "…", "evalCp": 40, "lossCp": 160, "mateIn": 0 }],
  "handicapJa": "二枚落ち",
  "baselineCp": 1386
}
```

`handicapJa` · `baselineCp` 는 **`/api/explore` 에만** 붙는다. `checked` 는 王手를 받고 있는 玉의 칸(`"5a"`)이고, 아니면 빈 값이다 — **클라이언트는 규칙을 모르므로 서버가 준다.**

### 퀴즈

`GET .../quiz` 의 `ready:false` 는 **아직 만드는 중**이고 「문항 없음」과 다르다. 생성이 수십 초 걸려서, 그 사이에 화면이 「問題はありません」을 그리면 거짓이 된다 — 판이 끝나는 자리에서 문항이 0개여도 행을 남기는 것이 이 값을 위해서다.

**정답이 응답에 안 실린다. 채점이 서버에 있다.**

| 문항            | 요청                      | `legalMoves` 의 범위                  |
| --------------- | ------------------------- | ------------------------------------- |
| 詰み (`mate`)   | `{moves[], attempt?}`     | **王手인 수만** — 攻方은 매 수 王手다 |
| 최선수 (`best`) | `{index, move, attempt?}` | **합법수 전부**                       |

- `mate` 는 **내가 낸 수만** 보낸다. 玉方의 응수는 저장된 트리에서 서버가 꺼낸다 — **엔진을 안 쓴다**
- `attempt` 는 화면이 센다. 서버에 안 남는다 — 몇 번 틀렸는지는 그 판의 사실이 아니라 지금 이 화면에서 하고 있는 일이다
- **`attempt` 를 크게 적어 정답을 살 수 없다.** 나가는 것은 `hint`(「7九の銀」) 뿐이고 세 번째 오답에서만 온다. `mateResponse` 에는 정답 수가 아예 없다
- `mateResponse.outcome` = `ongoing` \| `solved` \| `wrong` \| `not_check`
- `bestResponse.answer` 는 **맞혔을 때만** 온다

---

## 4. WebSocket 둘 — 무엇이 갈리나

| 축                   | `/ws/game` (AI 연습 대국) | `/ws/match` (사람끼리)               |
| -------------------- | ------------------------- | ------------------------------------ |
| 필요                 | **엔진** (없으면 `503`)   | 룰 엔진과 시계뿐                     |
| 로그인               | 불필요 (익명 판이 남는다) | **필수**                             |
| 개입 · 힌트 · 待った | **있다**                  | **없다** — 하나도                    |
| 상대 차례의 합법수   | 안 보낸다                 | **안 보낸다** (부정행위 보조가 된다) |
| 실력 추정            | 두는 중에 돈다            | 판이 끝난 뒤 분석기가 다시 잰다      |
| 시계                 | 없다                      | **1手 60초**                         |
| 한 판의 `games` 행   | 1개                       | **2개** (각 사람 관점)               |
| 총평                 | 있다 (`summary` 메시지)   | 없다 (`gameId` 만 온다)              |

**「개입은 AI 연습 대국 한정」이 실제로 걸린 자리가 여기다.** 온라인 쇼기 플랫폼은 전부 대국 중 소프트 참조를 금지하고, 그래서 사람끼리 두는 판에는 판정 자체가 없다.

**대국 세션은 서버 메모리에 있고 연결에 매여 있다.** 배포하면 진행 중인 대국이 끊긴다 — 끊긴 판은 `abandoned` 로 남고 `/api/resumable` 이 그것을 찾는다.

---

## 5. `/ws/game` — 대국 프로토콜

```
GET /ws/game?color=b&opening=<id>&handicap=<id>&resume=<gameId>
```

| 쿼리       | 값                                 | 주의                                                      |
| ---------- | ---------------------------------- | --------------------------------------------------------- |
| `color`    | `b`(先手) · `w`(後手). 그 외는 `b` | —                                                         |
| `opening`  | `/api/openings` 의 `id`            | **상대**의 진형이다                                       |
| `handicap` | `/api/handicaps` 의 `id`           | **`color` 와 `opening` 을 덮는다** — 사람은 언제나 下手다 |
| `resume`   | `games.id`                         | 中断된 자기 판을 이어 둔다                                |

**연결 하나가 대국 하나다. 요청/응답이 아니다** — 서버가 먼저 말을 건다(상대의 수 · 개입 · 힌트 · 총평). 45초마다 ping 을 보낸다(ALB 의 900초 유휴 시한보다 충분히 짧게).

### 클라이언트 → 서버

```ts
{ type: "move",   usi: string }
{ type: "undo" }
{ type: "hint" }
{ type: "resign" }
{ type: "whatif", ply: number, moves: string[] }
```

**그 외 타입은 `bad_move` 로 거절된다.**

| 타입     | 성공하면                                        | 제한                                                     |
| -------- | ----------------------------------------------- | -------------------------------------------------------- |
| `move`   | 구독 채널로 `snapshot`. **여기서 또 안 보낸다** | 판정 중에는 다음 수를 못 둔다 (`not_your_turn`)          |
| `undo`   | `snapshot`                                      | 판당 **3회**. 자기 수와 상대의 응수까지 **2手** 되감는다 |
| `hint`   | **성공해도 즉시 안 뜬다** — 탐색이 끝나면 온다  | 판당 **6회**. 한 국면은 **2단계**까지 (`hint_seen`)      |
| `resign` | `snapshot` (`status: "resigned"`)               | —                                                        |

힌트의 계단은 **1 = 「어느 駒인가」 · 2 = 「어떻게 움직이나」**이고 3은 없다. 한 국면의 답을 통째로 보려면 2회를 쓰므로, 판당 6회는 **최대 세 수**다.
| `whatif` | `whatif` 또는 `whatif_error` | **대국 중에는 물러진 수 뒤에서만** (`locked`) |

### 서버 → 클라이언트

```ts
{ type: "snapshot",     snapshot: Snapshot }
{ type: "error",        reason: string, message: string }
{ type: "whatif",       whatif: WhatifNode }
{ type: "whatif_error", reason: string, message: string }
{ type: "summary",      summary: GameSummaryPayload }
```

**`snapshot` 은 언제나 전체 상태다. 부분 갱신이 없다** — 롤백 뒤에 화면과 서버가 어긋나면 알아낼 방법이 없기 때문이다. 클라이언트는 규칙을 하나도 모르고(二歩도 打ち歩詰め도), 서버가 준 `legalMoves` 만 믿는다.

`summary` 는 판이 끝난 뒤 **한 번** 온다. 결과 문구보다 늦게 도착한다 — 기록이 다 쓰이기를 기다린다.

### `Snapshot`

```json
{
  "sfen": "…",
  "ply": 24,
  "turn": "b",
  "yourTurn": true,
  "inCheck": false,
  "thinking": false,
  "judging": false,
  "yourColor": "b",
  "opponentOpening": "shiken_bisha",
  "handicapJa": "二枚落ち",
  "baselineCp": 1386,
  "opponentStrength": 3,
  "legalMoves": ["7g7f", "…"],
  "moves": [{ "usi": "…", "ja": "▲7六歩", "by": "human", "sfen": "…", "checks": [{ "from": "…", "to": "…" }] }],
  "status": "playing",
  "winner": "",
  "mateHeat": 2,
  "undoLeft": 2,
  "canUndo": true,
  "hintLeft": 1,
  "canHint": true,
  "styleTags": [],
  "tagHints": [],
  "intervention": null,
  "hint": null,
  "notice": null
}
```

| 칸                                 | 무엇                                                                                                                   |
| ---------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `judging`                          | 방금 둔 수를 판정하는 중. **이 사이에는 다음 수를 못 둔다**                                                            |
| `thinking`                         | 상대가 탐색하는 중                                                                                                     |
| `mateHeat`                         | 詰み 게이지. **상대 玉 쪽 하나만** 그린다                                                                              |
| `canUndo` / `canHint`              | **화면이 이걸 다시 짓지 않는다.** `yourTurn && undoLeft > 0` 으로는 「되돌릴 자기 수가 아직 없는 첫 手」가 안 걸러진다 |
| `opponentStrength`                 | 적응형 상대의 세기 **5단계**. 단계가 바뀔 때만 스냅샷이 나간다                                                         |
| `status`                           | `playing` \| `checkmate` \| `stalemate`(쇼기에서는 이것도 패배) \| `resigned` \| `repetition` \| `aborted`             |
| `intervention` · `hint` · `notice` | 새 수를 두면 **셋 다 지운다** — 남아 있으면 방금 둔 수를 가리키는 것처럼 보인다                                        |

`notice` 는 「확인 자체를 못 했다」다. 판정이 실패했을 때 뜬다 — **개입이 없는 화면은 「이 수는 괜찮았다」와 똑같이 생겼는데**, 그 경우는 다른 일이다.

### `Intervention` (제지형 카드)

```json
{
  "kind": "blunder",
  "category": "hangs_piece",
  "retractedUsi": "8h3c+",
  "retractedJa": "▲3三角成",
  "retractedSfen": "…",
  "retractedChecks": [],
  "deltaWin": 0.31,
  "lostMate": false,
  "message": "…（日本語）",
  "refutation": [{ "usi": "…", "ja": "…", "by": "engine", "sfen": "…" }]
}
```

`message` 는 **결정적 템플릿**이 만든 일본어다 — 같은 사실에 언제나 같은 문장이 나간다. `refutation` 이 「그 수를 뒀으면 상대가 이렇게 咎める」다.

### 거절 코드

**프로토콜 수준** (`rejectMessages`):

| `reason`          | 언제                                   |
| ----------------- | -------------------------------------- |
| `not_your_turn`   | 상대 차례 · **판정 중**                |
| `finished`        | 이미 끝난 판                           |
| `bad_move`        | USI 형식이 아니다 · 모르는 메시지 타입 |
| `no_undo_left`    | 3회를 다 썼다                          |
| `nothing_to_undo` | 되돌릴 자기 수가 아직 없다             |
| `no_hint_left`    | 힌트를 다 썼다                         |
| `hint_seen`       | 이 국면의 힌트는 이미 줬다             |
| `no_hint`         | 힌트를 못 만들었다                     |
| `internal`        | 서버 문제                              |

**룰 위반**은 `internal/shogi` 가 코드를 낸다. **`reason` 이 공백을 포함한 영어 구절**이라 `snake_case` 로 맞추면 안 걸린다:

```
illegal · off board · not droppable · no piece in hand · square occupied
nifu · dead piece drop · must promote · uchifuzume · empty square
not your piece · unreachable square · own piece at destination
cannot promote · outside promotion zone · must resolve check · leaves king in check
```

> 정상 경로에서는 여기 안 온다 — 클라이언트가 서버에서 받은 합법수만 고르므로, 도달하면 국면이 어긋났다는 뜻(서버 버그)이다. 그래도 문구는 반칙 이름만이 아니라 「무엇이 문제인가」까지 적는다 — 「二歩ってなに」에서 막히기 때문이다.

**`whatif_error`** (`whatifMessages` — HTTP 세 표면과 **같은 표**를 쓴다):
`bad_ply` · `bad_move` · `bad_line` · `engine_unavailable` · `busy` · `locked`(대국 중만) · `login_required`(검토만)

---

## 6. `/ws/match` — 대인전 프로토콜

```
GET /ws/match?room=<id>
```

로그인 필수. 정원 2명. 방 id 는 **영숫자 8자**(연번이 아니다 — id 가 곧 열쇠다).

### 클라이언트 → 서버

```ts
{ type: "move", usi: string }
{ type: "resign" }
```

**`undo` 도 `hint` 도 `whatif` 도 없다.** 프로토콜에 아예 없는 것이 주석보다 확실하다.

### 서버 → 클라이언트

```ts
{ type: "waiting",  room: RoomPayload }
{ type: "snapshot", snapshot: MatchSnapshot }
{ type: "error",    reason: string, message: string }
{ type: "record",   gameId: number }
```

`waiting` 은 초대 링크를 그리는 데 필요한 것들을 싣는다. `record` 는 판이 끝난 뒤 한 번 오고 「振り返り」 링크가 그 값으로 만들어진다 — **총평이 아니다.**

### `MatchSnapshot`

`Snapshot` 보다 훨씬 얇다.

```json
{
  "sfen": "…",
  "ply": 24,
  "turn": "b",
  "yourTurn": true,
  "inCheck": false,
  "yourColor": "b",
  "legalMoves": [],
  "moves": [],
  "status": "playing",
  "winner": "",
  "opponentName": "…",
  "opponentOnline": true,
  "turnLimitMs": 60000,
  "turnLeftMs": 41200
}
```

**상대에 대해 나가는 것은 표시 이름 하나다.** 段級도 전적도 없다 — 「이 사람은 종반에 약하다」가 대인전 상대에게 넘어가면 안 된다.

`legalMoves` 는 **상대 차례에는 빈 배열**이다. 주면 상대의 수를 화면에서 훑어볼 수 있고, 그건 부정행위 보조다.

### 거절 코드

`not_your_turn` · `finished` · `bad_move` · **`room_closed`** (아무도 안 들어온 채 30분이 지났거나, 방을 만든 사람이 그 뒤로 방을 더 만들어 이 방이 밀려났다 — 호스트당 열린 방 **1개**).

「아직 상대가 안 들어왔다」가 **없다.** 그 상태에서는 착수가 도달할 자리가 없다.

---

## 7. 이 API 에 **없는** 것

명세에서 없는 것을 적어 두는 자리다 — 있는 줄 알고 찾는 시간이 더 비싸다.

| 없는 것                   | 왜                                                                                |
| ------------------------- | --------------------------------------------------------------------------------- |
| 버전 접두사 (`/api/v1`)   | 클라이언트가 이 서버 하나뿐이다                                                   |
| 페이지네이션 커서         | `{games: [...]}` 로 감싸 둔 것이 나중에 커서를 붙일 자리다. `?limit` 만 있다      |
| 부분 갱신 (PATCH 판 상태) | 스냅샷은 언제나 전체다                                                            |
| `rating_est` 를 주는 경로 | **어느 API 도 안 돌려준다** — 매칭이 쓰는 내부 값이다                             |
| 대기열의 알림 채널        | 폴링이다. 기다리는 동안 서버가 할 말이 없고, 알림은 인스턴스 사이의 통로를 만든다 |
| 대기열의 手合·手番 선택   | 큐는 **平手 확정 · 先手 랜덤**이다. 手合은 미리 만드는 방에만 있다                |
| 대국 목록의 필터·검색     | 아직 안 필요했다                                                                  |
| Bearer 토큰 · API 키      | 브라우저 하나뿐이라 서명 쿠키로 끝난다                                            |
| Rate limit 헤더           | 검토의 **엔진 슬롯 1개**가 그 자리를 대신한다 (`busy`)                            |
| WebSocket 재연결 프로토콜 | 세션이 연결에 매여 있다. 끊기면 `abandoned` → `/api/resumable`                    |
