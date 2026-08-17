---
name: playtest
description: Run a playtest on prod — three agents play three full games in parallel against the live engine, then their experience is consolidated into docs/playtests/. Use whenever the user asks for a playtest or a play-through — "플레이테스트 돌려줘", "3판 돌려줘", "에이전트로 게임 시켜봐", "playtest", "play a test game". This is a product-UX test of the intervention and hint layers, not a code review.
---

# Playtest (show-gi)

**목적은 이기는 것이 아니라 「개입과 힌트가 사람에게 실제로 통하는가」를 재는 것이다.** 에이전트 셋이 prod에서 각자 한 판씩 끝까지 두고, 그 경험을 한 문서로 합친다.

수동으로 한 판 둔 선행 기록이 [docs/08-playtest.md](../../../docs/08-playtest.md)에 있다. **리포트 구조와 평가 항목은 그것을 따른다.**

## 하드 규칙

- **prod에서 둔다.** `wss://show-gi.com/ws/game`. 실제 데이터를 prod에 쌓는 것이 이 테스트의 목적이다
- **DB는 읽기 전용이다.** `SELECT`만. `INSERT`·`UPDATE`·`DELETE`·DDL 금지. 대국 데이터는 앱이 쓰는 것이지 우리가 쓰는 것이 아니다
- **대국 중에는 DB·서버 로그·엔진 평가치를 보지 않는다.** 보는 순간 이 테스트가 재려던 것이 사라진다. 에이전트는 판과 합법수만 본다
- **끝까지 둔다.** 詰み 또는 투료까지. 手数 상한을 두지 않는다
- **일부러 지지 않는다.** 성향은 「어떻게 생각하는가」이지 「일부러 나쁜 수를 둔다」가 아니다
- 사람이 보는 문자열은 전부 일본어다. 한글이 화면 문자열에 섞였으면 그것 자체가 결함이니 리포트에 적는다

## 토큰 규율 — 이 스킬의 절반은 이것이다

서버는 한 수마다 `legalMoves` 200개가 든 스냅샷을 통째로 보낸다. **그것을 문맥에 넣으면 한 판에 수십만 토큰이 든다.** 그래서:

- 에이전트는 서버에 직접 붙지 않는다. **`bridge.mjs` 프록시를 거친다.** 프록시가 판을 그리고 줄여서 평문 15줄만 돌려준다 (한 수당 ~180 토큰)
- **원본 JSON은 전부 jsonl 파일로 흐른다.** 리포트는 문맥이 아니라 그 파일과 DB에서 만든다
- 에이전트는 **jsonl을 절대 읽지 않는다**. `cat`·`Read` 금지. 크기가 수 MB가 된다
- 합법수는 필요할 때만, 자리를 좁혀서 본다 — `/l?p=7f`
- 에이전트의 최종 보고는 **40줄 이내**다. 기보·개입 원문은 이미 파일에 있으니 옮겨 적지 않는다

## 절차

### 1. 준비

```sh
RUN=$(date +%Y-%m-%d)                     # 회차 폴더 이름에 쓴다
DIR=<scratchpad>/playtest-$RUN            # jsonl 을 둘 곳. 레포 안에 두지 않는다
mkdir -p "$DIR"
date -u +%FT%TZ                           # ← 이 시각을 적어둔다. 리포트에서 게임을 고를 때 쓴다
```

prod이 살아 있는지 먼저 본다. `engine`이 `false`면 대국이 성립하지 않으니 거기서 멈춘다.

```sh
curl -s https://show-gi.com/healthz        # {"db":true,"engine":true,"ok":true}
```

### 2. 브리지 셋을 띄운다

에이전트마다 **포트와 로그가 달라야 한다.** 한 프로세스가 한 대국을 붙들고 있으므로, 죽으면 그 판이 사라진다.

```sh
SK=.claude/skills/playtest
for i in 1 2 3; do
  PORT=$((9970+i)) AGENT=a$i LOG=$DIR/a$i.jsonl WS_URL=wss://show-gi.com/ws/game \
    node $SK/bridge.mjs > $DIR/a$i.out 2>&1 &
done
sleep 3; tail -n1 $DIR/a*.out
```

세 개가 각각 `a1 :9971 -> wss://...` 를 찍으면 준비된 것이다.

### 3. 에이전트 셋을 동시에 띄운다

**반드시 한 메시지에서 세 번 호출한다.** 순차로 띄우면 병렬이 아니다.

성향은 셋을 다르게 준다 — 개입 카테고리와 힌트 계단이 골고루 드러나야 검증 범위가 넓어진다.

| 에이전트 | 포트 | 성향                                                                                                                                      | 노리는 관찰                                      |
| -------- | ---- | ----------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------ |
| `a1`     | 9971 | **공격형** — 계속 밀어붙이고 교환을 마다하지 않는다. 玉 囲い는 최소한만                                                                   | `greedy_capture` · 종반 개입                     |
| `a2`     | 9972 | **신중형** — 玉을 굳히고 재료를 지킨다. 확실할 때만 교환한다                                                                              | 개입이 적은 판. 「개입이 안 뜨는 것도 경험인가」 |
| `a3`     | 9973 | **초심자형** — 초심자가 실제로 하는 사고로 둔다. **利き을 한 번만 세고**, 눈에 보이는 駒는 잡고 싶어 한다. 일부러 나쁜 수를 두지는 않는다 | `hangs_piece` · 힌트 계단                        |

각 에이전트에게 주는 지시에 다음을 **그대로** 넣는다:

- 너는 플레이어다. 포트 `<PORT>`의 프록시로만 판을 본다
- 명령은 이것뿐이다:
  `curl -s :PORT/s` 판 · `curl -sd '<usi>' :PORT/m` 착수 · `curl -s ':PORT/l?p=<칸>'` 그 칸의 합법수 · `curl -s :PORT/why` 직전 개입 전문 · `curl -s :PORT/f` 최종 요약
- **DB·서버 로그·엔진 평가치를 보지 마라.** `docker`·`psql`·레포의 Go 코드를 읽지 마라
- **jsonl 로그를 읽지 마라**
- 착수가 `!! RETRACTED`로 돌아오면 **그 문장을 읽고 왜 나쁜지 스스로 판단한 뒤** 다른 수를 골라라. 후보를 무작정 넣어보지 마라 — 그렇게 하면 이 테스트가 재려던 것이 사라진다
- `status`가 `checkmate`·`resigned`·`repetition`이 될 때까지 둔다
- 끝나면 `curl -s :PORT/f`를 한 번 부르고, **40줄 이내로** 보고한다:
  1. 결과와 手数
  2. 개입 중 **이해된 것**과 **이해되지 않은 것** — 문구를 그대로 인용하고, 무엇이 부족했는지
  3. 반박 수순(`咎め`)이 문구보다 도움이 됐는가
  4. 힌트가 떴다면 몇 단계까지 열렸고, 각 단계가 실제로 도움이 됐는가
  5. **내가 틀린 이유** — 무엇을 빠뜨려서 블런더가 나왔는가
  6. 한글이 섞여 나온 문자열이 있었는가

### 4. 대국이 끝나면 브리지를 내린다

```sh
pkill -f "skills/playtest/bridge.mjs"
```

**대국이 끝난 뒤에 내린다.** 먼저 내리면 그 판이 DB에 결과 없이 남는다.

### 5. prod DB에서 사실을 꺼낸다 — 읽기 전용

에이전트의 기억이 아니라 **DB가 근거다.** §1에서 적어둔 시각(`FROM`)으로 이 회차의 판만 고른다.

접속 문자열은 SSM에 있다([deploy/README.md](../../../deploy/README.md) §4). **값을 화면에 찍지 않는다.**

```sh
PGURL=$(aws ssm get-parameter --name /show-gi/prod/DATABASE_URL --with-decryption \
          --query Parameter.Value --output text)
q() { docker run --rm -e PGURL="$PGURL" postgres:17 psql "$PGURL" -X -c "$1"; }
```

`psql`이 로컬에 없으면 위처럼 postgres 이미지를 쓴다. 로컬 `show-gi-db` 컨테이너의 psql로 붙어도 같다.

꺼낼 것은 [queries.sql](queries.sql)에 있다. 최소한 이 넷:

1. 이 회차의 `games` 3행 — `id`·`result`·`started_at`·`finished_at`
2. 각 판의 手数 — `game_moves` count
3. `interventions` 전문 — `ply`·`category`·`delta_win`·`retracted_usi`
4. 카테고리 분포 — 세 판 합계와 판별

**[docs/08-playtest.md](../../../docs/08-playtest.md) §11에서 관측된 것들을 매번 다시 본다.** 그때는 로컬에서 `positions`가 테스트 픽스처 2행뿐이었다. prod에서도 같은지 확인하고, 달라졌으면 그것 자체가 회차의 발견이다.

### 6. 문서를 쓴다

`docs/playtests/YYYY-MM-DD-NN.md`. `NN`은 그날의 회차(01부터). 절 구성:

1. **한 줄** — 세 판 결과와 이번 회차에서 가장 중요한 발견 하나
2. **조건** — 대상(prod·커밋), 상대 설정, 에이전트 셋의 성향
3. **결과 표** — 판별 결과·手数·개입 횟수·힌트 단계
4. **개입 분포** — 카테고리별 합계. `other` 비율을 [journal](../../../docs/journal/06-20.md) §17의 65~70%와 대조한다
5. **문구별 평가** — 카테고리마다 「통했다 / 안 통했다」와 근거. **에이전트 셋의 판단이 갈리면 갈렸다고 적는다.** 합의된 것처럼 쓰지 않는다
6. **힌트 계단** — 몇 단계까지 열렸나, 각 단계가 실제로 도움이 됐나
7. **에이전트가 틀린 이유** — 유형별로 묶는다. 이것이 카테고리 설계의 입력이다
8. **회차 발견** — 이번에 새로 보인 것. 없으면 「없음」이라고 적는다
9. **재현** — 실행한 질의 그대로

그리고 `docs/playtests/README.md`에 한 줄 추가한다 — 날짜·판수·개입 총계·핵심 발견. 없으면 만든다.

**확인하지 못한 것은 `[미확정]`으로 적는다.** 특히 브라우저 화면을 보지 않고 프록시로만 뒀으므로, 렌더된 카드의 가독성은 매번 `[미확정]`이다.

## 자주 물리는 곳

- **`ILLEGAL … not in legalMoves`** — 대개 成을 빠뜨린 것이다. `7c8d`와 `7c8d+`는 다른 수다. **出発マス가 敵陣이면 成할 수 있다**
- **`!! TIMEOUT`** — depth 12 탐색이 오래 걸린 것이다. 대국은 살아 있으니 `/s`로 다시 본다. 브리지를 재시작하면 판이 사라진다
- **`CLOSED`** — 연결이 끊겼고 그 판은 끝이다. 되살릴 수 없다
- **개입이 반복되는데 힌트가 안 뜬다** — 같은 국면에서 세 번은 물러져야 열린다. 국면이 바뀌면 계단이 처음으로 돌아간다
- **포트가 이미 쓰인다** — 이전 회차 브리지가 남아 있다. `pkill -f "skills/playtest/bridge.mjs"`
