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
# 기본값은 여기 두지 않는다. 두면 lib/config.js 와 두 벌이 되고, 한쪽을 고칠 때
# 다른 쪽이 조용히 낡아 「무슨 값으로 잰 회차인가」가 갈린다.
set -eu

repo=$(cd "$(dirname "$0")/../.." && pwd)

prod=0
if [ "${1-}" = "--prod" ]; then
	prod=1
	shift
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

if [ "$prod" = 1 ]; then
	# 익명 판은 cleanup.sql 이 안 걷는다. 프로덕션에 남으면 사람이 손으로 찾아야 한다.
	if [ -z "${LT_UIDS-}" ]; then
		echo "--prod 에는 LT_UIDS 가 있어야 한다 — 익명 판은 cleanup.sql 에 안 걸린다" >&2
		exit 2
	fi
	export BASE="${BASE:-https://show-gi.com}"
	# 디스크에 안 남긴다. 그때 읽어 환경변수로만 넘긴다.
	SESSION_SECRET=$(aws ssm get-parameter \
		--name /show-gi/prod/SESSION_SECRET --with-decryption \
		--region ap-northeast-1 --profile show-gi \
		--query Parameter.Value --output text)
	export SESSION_SECRET
fi

# 무엇으로 잰 회차인지가 로그 첫 줄에 남는다. 저널이 그 줄을 그대로 인용한다.
echo "회차 $(date -u '+%Y-%m-%dT%H:%M:%SZ') — BASE=${BASE-기본} MODE=${MODE-기본}" \
	"VUS_ENGINE=${VUS_ENGINE-기본} VUS_MATCH=${VUS_MATCH-기본} DURATION=${DURATION-기본}" \
	"THINK_MS=${THINK_MS-기본} MAX_PLIES=${MAX_PLIES-기본}"

exec k6 run "$repo/tools/loadtest/main.js"
