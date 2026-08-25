#!/bin/sh
# 회차를 거는 자리. 손잡이는 KEY=VALUE 로 받아 그대로 k6 에 넘긴다.
#
# 명령줄에서 직접 부르면 셋이 어긋난다.
#
#   - k6 는 손잡이를 환경변수로만 받으므로(lib/config.js) 명령이 env 접두사로 시작한다.
#     그러면 Bash(k6 run:*) 허용 규칙이 접두사 매칭에 걸리지 않는다
#   - SESSION_SECRET=$(aws ssm ...) 의 명령 치환도 같은 규칙을 못 지난다
#   - main.js 를 상대 경로로 주면 레포 루트가 아닌 곳에서 부를 때 안 찾아진다
#
# 기본값은 여기 두지 않는다. k6 스크립트가 이미 든다 — 손잡이 대부분이 lib/config.js
# 이고 시나리오 넷(MODE·DURATION·VUS_ENGINE·VUS_MATCH)이 main.js 다. 여기 한 벌 더
# 두면 한쪽을 고칠 때 다른 쪽이 조용히 낡는다.
set -eu

repo=$(cd "$(dirname "$0")/../.." && pwd)

prod=0
if [ "${1-}" = "--prod" ]; then
	prod=1
	shift
	: "${BASE:=https://show-gi.com}"
	export BASE
fi

for kv in "$@"; do
	case "$kv" in
	*=*) export "$kv" ;;
	*)
		echo "손잡이는 KEY=VALUE 로 준다: $kv" >&2
		exit 2
		;;
	esac
done

# 원격에 거는가는 플래그가 아니라 주소가 정한다. --prod 없이 BASE 만 넘겨도 같은 곳에
# 걸리므로, 아래 가드를 플래그에 매달면 그 경로로 그냥 지나간다.
case "${BASE-}" in
'' | http://localhost* | http://127.0.0.1* | https://localhost* | https://127.0.0.1*) remote=0 ;;
*) remote=1 ;;
esac

if [ "$remote" = 1 ]; then
	# 익명 판은 cleanup.sql 이 안 걷는다(users 한 줄에서 CASCADE 로 지우는 구조라
	# games.user_id 가 NULL 이면 안 닿는다). 남으면 사람이 손으로 찾아야 한다.
	#
	# 비었는가만 보지 않는다. 숫자가 아닌 값은 lib/config.js 가 걸러 버려서 빈 목록과
	# 같아지고, 그러면 회차가 익명으로 돈다.
	if ! printf '%s' "${LT_UIDS-}" | tr ',' '\n' | grep -qE '^[[:space:]]*[1-9][0-9]*[[:space:]]*$'; then
		echo "원격($BASE)에는 LT_UIDS 에 사용자 번호가 있어야 한다 — 익명 판은 cleanup.sql 에 안 걸린다" >&2
		exit 2
	fi
fi

if [ "$prod" = 1 ] && [ "${SESSION_SECRET-}" = "" ]; then
	# 디스크에 안 남긴다. 그때 읽어 환경변수로만 넘긴다.
	SESSION_SECRET=$(aws ssm get-parameter \
		--name /show-gi/prod/SESSION_SECRET --with-decryption \
		--region ap-northeast-1 --profile show-gi \
		--query Parameter.Value --output text)
	export SESSION_SECRET
fi

# 넘긴 손잡이가 로그 첫 줄에 남는다. 안 넘긴 것은 안 적는다 — 기본값은 k6 스크립트가
# 들고 그 값이 바뀐 적이 있어서(MAX_PLIES 60 → 100), 여기서 「기본」이라고 적으면
# 나중에 읽는 사람이 어느 값으로 잰 회차인지 알 수 없다.
shown=""
for kv in "$@"; do
	case "$kv" in
	BASE=*) ;; # 아래 줄이 이미 적는다
	SESSION_SECRET=*) shown="$shown SESSION_SECRET=***" ;;
	*) shown="$shown $kv" ;;
	esac
done
echo "회차 $(date -u '+%Y-%m-%dT%H:%M:%SZ') — BASE=${BASE-}$shown"

exec k6 run "$repo/tools/loadtest/main.js"
