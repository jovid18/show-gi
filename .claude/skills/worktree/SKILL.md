---
name: worktree
description: Split the remaining work into non-overlapping tracks, then create a git worktree with its own API container and dev server for each. Use whenever the user wants to work on several things in parallel — "워크트리 만들어줘", "병렬로 작업하게 나눠줘", "일 분담해줘", "worktree 파줘", "두 개로 나눠서 하자". Also handles teardown after the PRs merge.
---

# Worktree (show-gi)

사람이 터미널 탭을 여러 개 열어 각자 PR을 내는 것이 이 레포의 기본 진행 방식이고, 이 스킬은 그 판을 깔아준다 — **일을 겹치지 않게 나누고, 워크트리마다 혼자 도는 로컬 환경을 세운다.**

컨벤션 자체는 [CLAUDE.md](../../../CLAUDE.md) 「워크트리」에 있다. 여기는 절차다.

**구현은 이 스킬이 하지 않는다.** 워크트리를 세우고 브리핑을 남기는 데까지가 끝이고, 각 워크트리에서 실제 작업은 사람이 세션을 따로 열어서 한다.

## 시작 전에

**메인 워크트리(`~/personal/show-gi`)에서 돌린다.** 워크트리 안에서 또 워크트리를 만들면 분기 지점이 `origin/main`이 아니게 된다.

```bash
git -C ~/personal/show-gi rev-parse --show-toplevel   # 메인이 맞는지
docker ps --format '{{.Names}}' | grep -q show-gi-db  # 공유 db가 떠 있는지
test -f ~/personal/show-gi/.env                        # 복사할 원본이 있는지
```

- 메인이 아니면 → 메인 경로를 알려주고 거기서 다시 부르라고 한다.
- db가 없으면 → 메인에서 `docker compose up -d db`.
- `.env`가 없으면 → `cp .env.example .env`. 값이 비어도 앱은 뜬다(기능만 꺼진다).

## 1. 지금 무엇이 돌고 있는지부터 본다

**이 단계를 건너뛰고 분담안을 내지 않는다.** 이미 누가 잡고 있는 파일에 일을 또 배정하면, 병렬로 번 시간을 나중에 리베이스로 다 토해낸다.

```bash
git -C ~/personal/show-gi fetch --prune origin
git worktree list
gh pr list --state open --json number,title,headRefName --jq '.[] | "#\(.number) \(.headRefName) — \(.title)"'
```

워크트리가 하나라도 있으면 **각각에 대해** 다음을 모아 「점유된 파일 집합」을 만든다:

```bash
cd <worktree>
cat WORKTREE.md 2>/dev/null           # 맡은 범위. 이게 있으면 제일 빠르다
git branch --show-current
git status --porcelain                # 커밋 안 된 작업 — diff에는 안 잡힌다
git diff origin/main...HEAD --name-only
```

**커밋 전 변경(`git status`)을 반드시 같이 본다.** 작업 중인 세션은 대개 아직 커밋을 안 했고, `diff origin/main...HEAD`만 보면 그 파일들이 비어 있는 것처럼 보인다.

열려 있는 PR도 같은 취급이다 — 머지되기 전까지 그 파일은 점유 상태다:

```bash
gh pr view <번호> --json files --jq '.files[].path'
```

## 2. 남은 일을 읽는다

[docs/06-status.md](../../../docs/06-status.md) **§5**가 정본이다. 순서까지 거기 적혀 있으니 새로 정하지 않는다. §5는 기능 목록 위에 **「제출물(D6)이 통째로 비어 있다」**를 두고 있다 — 분담안에서 그 줄을 조용히 빠뜨리지 않는다.

## 3. 분담안을 낸다

기본 **2~3 갈래**. 그 이상은 파일이 겹치기 시작하고, api 컨테이너가 엔진 프로세스를 물고 있어 머신도 는다.

각 갈래마다 이만큼 적어 사람에게 확인받는다:

| slug | 브랜치 | 한 줄 목표 | 건드릴 파일 | 왜 안 겹치나 |
| ---- | ------ | ---------- | ----------- | ------------ |

**나누는 기준은 「파일이 안 겹치는가」 하나다.** 주제가 달라도 같은 파일을 고치면 나눈 것이 아니다. 이 레포에서 특히 잘 겹치는 자리:

- `internal/game/session.go` — 세션이 상태를 소유해서 거의 모든 기능이 여기를 지난다. **두 갈래에 동시에 주지 않는다**
- `docs/06-status.md` — 어느 갈래든 마지막에 절을 붙인다. 충돌은 나지만 문서라 풀기 쉽다. 대신 **같은 절 번호를 두 갈래가 쓰지 않도록** 미리 번호를 배정해 준다
- `apps/web/src/components/GameScreen.tsx` — 화면 붙는 일이 다 여기로 모인다

**요약표는 갈래에 나눠 주지 않는다.** `docs/06-status.md` §1의 「지금 실제로 도는 것」과 [docs/05-roadmap.md](../../../docs/05-roadmap.md)의 진도 표는 **셋 다 참조하지만 아무도 소유하지 않는 자리**다. 각자 고치면 세 번 어긋난다 — 첫 회차에서 실제로 手筋 숫자가 갈렸다([§36](../../../docs/journal/21-40.md)).

**마지막에 머지되는 갈래가 요약표를 통합해 고친다**고 배정하고, 그 줄을 그 갈래의 `WORKTREE.md`에 적는다. 절 번호 배정은 의미 충돌만 막지 이것을 못 막는다.

되돌릴 수 없는 마이그레이션(`DROP`·`RENAME`·`NOT NULL` 추가)이 필요한 일은 **분담안에 넣지 않는다.** db가 공유라 그 순간 다른 워크트리의 서버가 깨진다 — 혼자 돌려야 하는 일이라고 말한다.

승인 없이 4로 넘어가지 않는다.

## 4. 워크트리를 세운다

slug마다 아래를 그대로 돈다. **포트는 먼저 정한다** — 기존 워크트리의 `.env`에서 쓰이는 것을 걷어내고, 비어 있는 다음 번호를 준다:

```bash
grep -h SHOWGI_API_PORT ~/personal/show-gi-*/.env 2>/dev/null   # 이미 쓰는 포트
lsof -nP -iTCP:8081 -sTCP:LISTEN                                 # 진짜 비었는지
```

`n`번째 워크트리 = api `8080+n` · vite `5173+n`.

```bash
SLUG=mate-gauge
TYPE=feat
PORT=8081
VITE=5174
DIR=~/personal/show-gi-$SLUG

git -C ~/personal/show-gi worktree add "$DIR" -b "$TYPE/$SLUG" origin/main

cp ~/personal/show-gi/.env "$DIR/.env"
cat >> "$DIR/.env" <<EOF

# ─── 워크트리 ──────────────────────────────────────────────
COMPOSE_PROJECT_NAME=show-gi-$SLUG
SHOWGI_API_NAME=show-gi-$SLUG-api
SHOWGI_API_PORT=$PORT
EOF

cd "$DIR"
pnpm install                          # pnpm store가 공유라 몇 초면 끝난다
docker compose up -d --no-deps api    # 레이어 캐시가 공유라 Go 빌드만 다시 돈다
curl -s localhost:$PORT/healthz       # {"db":true,"engine":true,"ok":true}
```

**`--no-deps`를 빼지 않는다.** db까지 띄우려다 컨테이너명 충돌로 실패한다.

`healthz`가 셋 다 `true`가 아니면 거기서 멈추고 보고한다:

|                |                                                                                |
| -------------- | ------------------------------------------------------------------------------ |
| `engine:false` | 이미지 빌드가 덜 됐다. `docker compose build api` 출력을 본다                  |
| `db:false`     | 공유 db가 내려갔다. 메인에서 `docker compose up -d db`                         |
| 포트 안 잡힘   | `../shogi` 컨테이너가 물고 있을 수 있다 — `cd ../shogi && docker compose down` |

마지막으로 브리핑을 남긴다(§5). **`git config core.hooksPath .githooks`는 다시 안 해도 된다** — 워크트리는 메인의 `.git/config`를 같이 쓴다.

## 5. `WORKTREE.md`를 남긴다

워크트리 루트에 쓴다. gitignore 대상이라 커밋되지 않는다. 여기서 세션을 여는 사람(또는 Claude)이 **제일 먼저 읽는 파일**이므로, 세션 하나가 이것만 보고 일을 시작할 수 있어야 한다.

아래 그대로 쓴다. **바깥의 네 겹 울타리는 쓰는 파일에 안 들어간다** — 안쪽 코드블록을 escape하지 않으려고 두른 것뿐이다.

````markdown
# <slug>

<한 줄 목표. docs/06-status.md §5의 어느 항목인지 함께.>

## 범위

- <할 것>

**건드리지 않는다** — 다른 워크트리가 잡고 있다:

| 파일 | 어느 워크트리 |
| ---- | ------------- |

## 띄우기

```sh
docker compose up -d --no-deps api
SERVER_ORIGIN=http://localhost:<PORT> pnpm dev --port <VITE>
```

api `<PORT>` · vite `<VITE>` · db는 메인의 `show-gi-db` 공유.

## 끝나면

`/create-pr`. 머지는 사람이 GitHub에서 한다.
docs/06-status.md에 절을 붙인다면 **§<번호>**를 쓴다 (다른 워크트리와 겹치지 않게 배정된 번호).
````

## 6. 사람에게 넘긴다

표 하나로 보고하고 끝낸다. 세션을 대신 열어주지 않는다.

```
~/personal/show-gi-<slug>   feat/<slug>   api :8081  vite :5174   <한 줄 목표>
```

각 탭에서: `cd ~/personal/show-gi-<slug> && claude`

## 7. 철거

PR이 머지된 뒤에. **순서를 지킨다** — 컨테이너를 먼저 내리지 않으면 디렉터리가 사라진 채 컨테이너만 남는다.

```bash
cd ~/personal/show-gi-<slug>
docker compose down --remove-orphans
docker volume rm show-gi-<slug>_show-gi-dbdata   # 안 쓰이지만 선언 때문에 생긴다

cd ~/personal/show-gi
git worktree remove ~/personal/show-gi-<slug>
git branch -d <type>/<slug>
git fetch --prune origin
```

**워크트리에서 `docker compose down -v`를 쓰지 않는다.** 지금은 워크트리 볼륨이 비어 있어 우연히 안전하지만, 같은 손버릇이 메인에서 나오면 `showgi` 데이터베이스가 통째로 날아간다. 볼륨은 위처럼 **이름을 적어서** 지운다.

디렉터리를 손으로 지워 버렸다면 `git worktree prune`으로 메타데이터를 정리한다.

## 알아두면 좋은 것

- **엔진 빌드는 워크트리마다 다시 돌지 않는다.** やねうら王 컴파일 스테이지는 Dockerfile이 같으면 레이어 캐시가 맞는다. 다시 도는 것은 Go 빌드뿐이라 10초 안쪽이다
- **`pnpm install`도 몇 초다.** store가 공유이고 하드링크로 깔린다
- **Go 모듈 캐시도 공유다.** `go test`는 워크트리에서 바로 돈다 — 셋업할 것이 없다
- 워크트리는 메인의 `.git`을 참조한다. **메인 워크트리를 지우면 전부 죽는다**
- 브랜치가 체크아웃된 워크트리가 있으면 메인에서 그 브랜치로 `switch`할 수 없다. 정상이고, 그게 워크트리의 요점이다
