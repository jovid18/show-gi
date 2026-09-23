#!/usr/bin/env bash
# Orca 워크트리의 setup·archive 훅. orca.yaml 이 부른다.
#
#   setup    .env 에 워크트리 블록(compose 프로젝트·api 이름·포트)을 붙이고 pnpm install
#   archive  그 워크트리의 api 컨테이너와 선언용 볼륨을 내린다
#
# /worktree 스킬 §4·§7 과 같은 일을 한다. 규약은 CLAUDE.md 「워크트리」.
# api 컨테이너는 띄우지 않는다. 문서만 고치는 워크트리도 많고, 컨테이너마다 엔진 프로세스를 문다.
set -euo pipefail

root=${ORCA_ROOT_PATH:?ORCA_ROOT_PATH 가 없다. Orca 밖에서는 /worktree 스킬을 쓴다}
wt=${ORCA_WORKTREE_PATH:-$PWD}
marker='# ─── 워크트리 (Orca) ─'

# 메인에서 돌면 compose 프로젝트 show-gi 를 건드린다. db 가 거기 붙어 있다.
if [ "$(cd "$wt" && pwd -P)" = "$(cd "$root" && pwd -P)" ]; then
  echo "메인 워크트리다. 아무것도 하지 않는다" >&2
  exit 0
fi

slug=$(basename "$wt" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9-\n' '-')

setup() {
  cd "$wt"
  # .worktreeinclude 가 메인의 .env 를 복사해 둔다. 메인에 없으면 예시에서 시작한다.
  [ -f .env ] || cp .env.example .env

  # 다시 돌려도 블록이 두 벌 생기지 않게 지운 뒤 붙인다.
  if grep -qF "$marker" .env; then
    sed -i '' "/^$marker/,\$d" .env
  fi

  # n 번째 워크트리 = api 8080+n. 다른 워크트리의 .env 와 실제 LISTEN 을 둘 다 피한다.
  local used n port
  used=$(git -C "$root" worktree list --porcelain | sed -n 's/^worktree //p' | while read -r p; do
    [ "$p" = "$wt" ] || grep -h '^SHOWGI_API_PORT=' "$p/.env" 2>/dev/null || true
  done | cut -d= -f2)
  n=1
  while :; do
    port=$((8080 + n))
    if ! grep -qx "$port" <<<"$used" && ! lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
      break
    fi
    n=$((n + 1))
  done

  cat >>.env <<EOF
$marker
COMPOSE_PROJECT_NAME=show-gi-$slug
SHOWGI_API_NAME=show-gi-$slug-api
SHOWGI_API_PORT=$port
# vite: SERVER_ORIGIN=http://localhost:$port pnpm dev --port $((5173 + n))
EOF

  pnpm install --frozen-lockfile

  echo
  echo "api :$port · vite :$((5173 + n)) · db 는 메인의 show-gi-db 공유"
  echo "  docker compose up -d --no-deps api"
  echo "  SERVER_ORIGIN=http://localhost:$port pnpm dev --port $((5173 + n))"
}

archive() {
  cd "$wt"
  local project
  project=$(sed -n 's/^COMPOSE_PROJECT_NAME=//p' .env 2>/dev/null | tail -1)
  # 블록이 없으면 compose 가 디렉터리명으로 프로젝트를 잡는다. 추측으로 내리지 않는다.
  if [ -z "$project" ] || [ "$project" = show-gi ]; then
    echo "워크트리 compose 프로젝트가 아니다 ($project). 건너뛴다" >&2
    exit 0
  fi
  # down -v 를 쓰지 않는다. 볼륨은 이름을 적어서 지운다 (/worktree 스킬 §7).
  docker compose down --remove-orphans || true
  docker volume rm "${project}_show-gi-dbdata" >/dev/null 2>&1 || true
}

case ${1:-} in
  setup) setup ;;
  archive) archive ;;
  *)
    echo "usage: $0 setup|archive" >&2
    exit 2
    ;;
esac
