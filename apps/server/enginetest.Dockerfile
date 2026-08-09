# 실엔진 테스트용 이미지 — Go 툴체인 + 배포 이미지에서 꺼낸 엔진.
#
# 엔진 관련 테스트는 CI에서 돌지 않는다(러너에 엔진이 없다). 그런데 **엔진을 바꿀 때
# 첫 관문이 그 테스트들**이라, 돌리는 방법이 레포에 없으면 관문이 없는 것과 같다.
#
#   docker build --platform linux/arm64 -t show-gi-api .
#   docker build --platform linux/arm64 -t show-gi-enginetest -f enginetest.Dockerfile .
#
#   docker run --rm --platform linux/arm64 --cpus 4 -v "$PWD:/src:ro" show-gi-enginetest sh -c '
#     cp -r /src /work && cd /work &&
#     SHOWGI_USI_CMD=/opt/yaneuraou/run go test ./... -run RealEngine -v'
#
# 소스를 마운트해서 복사하는 것은, 마운트가 읽기 전용이라 go 가 캐시를 못 쓰기 때문이다.
ARG API_IMAGE=show-gi-api
FROM ${API_IMAGE} AS engine

FROM golang:1.26-bookworm
RUN apt-get update \
    && apt-get install -y --no-install-recommends libgomp1 \
    && rm -rf /var/lib/apt/lists/*
COPY --from=engine /opt/yaneuraou /opt/yaneuraou
