#!/bin/bash
# 인스턴스에서 도는 배포 스크립트. 사람이 직접 부르지 않는다 —
# GitHub Actions가 SSM Run Command로 실행한다 (.github/workflows/images.yml).
#
#   deploy.sh <git-sha>
#
# 커밋 SHA 하나가 배포 전체를 정의한다: 그 커밋의 compose 파일로, 그 커밋에서 구운
# 이미지를 띄운다. 둘이 갈라지면 "코드는 고쳤는데 왜 그대로지"가 생기고, 그건
# 마감 주에 가장 비싼 종류의 혼란이다.
#
# 되돌리기도 같은 명령이다 — 이전 SHA를 주면 그 시점으로 통째로 돌아간다.

set -euo pipefail

SHA="${1:?usage: deploy.sh <git-sha>}"
REPO_DIR="${REPO_DIR:-/opt/show-gi}"
REGION="${AWS_REGION:-ap-northeast-1}"
COMPOSE=(docker compose -f docker-compose.yml -f docker-compose.prod.yml)

cd "$REPO_DIR"

echo "· $SHA 로 체크아웃"
git fetch --quiet origin main
git checkout --quiet --force "$SHA"

# shellcheck source=./env.sh
. deploy/env.sh

# 파라미터에도 IMAGE_TAG가 있지만, 배포하는 커밋이 항상 이긴다.
# 이 한 줄이 "compose 파일과 이미지가 같은 커밋에서 온다"를 보장한다.
export IMAGE_TAG="$SHA"

echo "· ECR 로그인"
aws ecr get-login-password --region "$REGION" \
	| docker login --username AWS --password-stdin "$REGISTRY" >/dev/null

# 스키마는 여기서 건드리지 않는다. DDL은 사람이 DB 클라이언트로 넣는다 —
# 배포가 테이블을 바꾸기 시작하면, 되돌릴 수 없는 변경이 자동으로 실행된다.
# 절차는 deploy/README.md의 "스키마 변경".

echo "· 이미지 받기"
"${COMPOSE[@]}" pull --quiet

echo "· 올리기"
"${COMPOSE[@]}" up -d --remove-orphans

# 컨테이너가 떴다는 것과 앱이 산다는 것은 다르다. 실제로 응답하는지 본다.
echo -n "· 헬스체크"
for _ in $(seq 1 30); do
	if curl -fsS --max-time 2 http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
		echo " — OK"
		# 이전 이미지가 쌓이면 30GiB 루트 볼륨이 조용히 찬다
		docker image prune -f >/dev/null
		echo "✓ $SHA 배포 완료"
		exit 0
	fi
	echo -n "."
	sleep 2
done

echo
echo "✗ 60초 안에 헬스체크가 통과하지 않았다" >&2
"${COMPOSE[@]}" ps >&2
"${COMPOSE[@]}" logs --tail=50 api >&2
exit 1
