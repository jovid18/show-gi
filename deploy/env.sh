#!/bin/sh
# SSM Parameter Store의 값을 셸 환경으로 내보낸다. 인스턴스에서 이렇게 쓴다:
#
#   . deploy/env.sh
#   docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
#
# **파일로 떨어뜨리지 않는 것이 요점이다.** compose는 셸 환경을 읽고, 컨테이너가
# 만들어지는 순간 값이 컨테이너 설정에 들어간다. 그래서 재부팅 후 자동 재시작에도
# 다시 불러올 필요가 없고, 서버 디스크에 평문 .env가 남지 않는다.
#
# 자격증명은 인스턴스 역할에서 온다 — 이 스크립트에 키가 없다.

set -eu

PREFIX="${SSM_PREFIX:-/show-gi/prod}"
REGION="${AWS_REGION:-ap-northeast-1}"

# --with-decryption이 SecureString을 푼다. 인스턴스 역할에 kms:Decrypt가 있어야 한다.
# 파라미터가 10개를 넘으면 페이지가 나뉘므로 --max-items 없이 전부 훑는다.
_params="$(aws ssm get-parameters-by-path \
	--path "$PREFIX" \
	--with-decryption \
	--region "$REGION" \
	--query 'Parameters[].[Name,Value]' \
	--output text)"

if [ -z "$_params" ]; then
	echo "✗ $PREFIX 아래에 파라미터가 없다. deploy/README.md의 등록 절차를 볼 것" >&2
	return 1 2>/dev/null || exit 1
fi

# 이름의 마지막 조각이 변수명이다: /show-gi/prod/ACME_EMAIL → ACME_EMAIL
# 값에 공백이 들어갈 수 있으므로 탭으로 자른다 (--output text가 탭 구분이다).
while IFS="$(printf '\t')" read -r _name _value; do
	[ -n "$_name" ] || continue
	_key="${_name##*/}"
	export "$_key=$_value"
done <<EOF
$_params
EOF

unset _params _name _value _key
echo "· $PREFIX 에서 환경변수를 불러왔다"
