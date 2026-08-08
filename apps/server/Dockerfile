# API 서버 + USI 엔진. 엔진을 별도 컨테이너로 빼지 않는 이유는 엔진이 네트워크
# 서비스가 아니라 stdin/stdout으로 말하는 하위 프로세스이기 때문이다 — 같은
# 컨테이너에 있어야 프로세스 감시와 재기동이 성립한다.

FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends fairy-stockfish ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# 데비안은 게임 바이너리를 /usr/games에 넣는데 이 경로는 기본 PATH에 없다.
# 빼먹으면 런타임에 "엔진 실행 실패"만 뜨고 원인이 안 보인다.
ENV PATH="/usr/games:${PATH}"
ENV ENGINE_CMD=fairy-stockfish

# 루트로 돌리지 않는다. 엔진은 신뢰할 수 없는 국면 문자열을 파싱하는 외부 바이너리다.
RUN useradd --system --create-home --uid 10001 app
USER app

COPY --from=build /out/api /usr/local/bin/api
EXPOSE 8080
CMD ["api", "-addr", ":8080"]
