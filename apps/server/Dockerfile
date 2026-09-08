# API 서버 + USI 엔진. 엔진을 별도 컨테이너로 빼지 않는다 — 네트워크 서비스가
# 아니라 stdin/stdout으로 말하는 하위 프로세스라서, 같은 컨테이너에 있어야
# 프로세스 감시와 재기동이 성립한다.

FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api

# ── USI 엔진 ────────────────────────────────────────────────────────────────
#
# やねうら王(GPLv3)을 소스에서 빌드하고 평가함수로 水匠5를 얹는다.
#
# fairy-stockfish에서 갈아탄 것은 강함이 아니라 **`go mate`가 없어서**다.
# 그쪽은 `go mate`를 조용히 무시하고 일반 탐색으로 떨어져서, 詰み 게이지가
# 그럴듯한 쓰레기를 표시하게 된다 (docs/06-status.md §9).
#
# 엔진은 파이프 너머의 별도 프로세스라 GPL이 우리 코드로 전파되지 않는다.
# 다만 이미지에 바이너리를 담는 것은 배포이므로, 어느 커밋을 빌드했는지 여기에 남긴다.
FROM debian:bookworm-slim AS engine

ARG YANEURAOU_TAG=v9.40
# 水匠5. やねうら王 릴리스에서 무료로 공개된 단체 파일이다(藤井聡太가 쓰던 그것).
# 최신 水匠은 지원자 전용 배포라 쓸 수 없다.
ARG SUISHO_URL=https://github.com/yaneurao/YaneuraOu/releases/download/suisho5/Suisho5.7z

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
    build-essential git ca-certificates wget p7zip-full \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build
RUN git clone --depth 1 --branch "${YANEURAOU_TAG}" https://github.com/yaneurao/YaneuraOu.git

# ARMV8. dotprod는 NNUE 추론 속도 최적화인데 NEON 인트린식이 컴파일에서 깨진다 —
# 없어도 동작하고, 실측에서 이 조합만 통과했다.
RUN cd YaneuraOu/source \
    && make -j"$(nproc)" tournament TARGET_CPU=ARMV8 COMPILER=g++ \
    && mkdir -p /opt/yaneuraou/eval \
    && cp YaneuraOu-by-gcc /opt/yaneuraou/engine

# 詰将棋 solver는 **별도 에디션**이다. 위의 탐색 엔진은 `go mate`에 `checkmate`로
# 답하지 않고 그냥 `bestmove`를 돌려준다 — fairy-stockfish와 같은 함정이라 실측으로 확인했다.
# 詰み 게이지(D4)는 이 바이너리로 돈다.
RUN cd YaneuraOu/source \
    && make clean \
    && make -j"$(nproc)" tournament TARGET_CPU=ARMV8 COMPILER=g++ \
    YANEURAOU_EDITION=YANEURAOU_MATE_ENGINE \
    && cp YaneuraOu-by-gcc /opt/yaneuraou/mate

RUN wget -qO /tmp/suisho.7z "${SUISHO_URL}" \
    && 7z e -so /tmp/suisho.7z nn.bin > /opt/yaneuraou/eval/nn.bin \
    && rm /tmp/suisho.7z \
    && test -s /opt/yaneuraou/eval/nn.bin

# 엔진은 평가함수를 자기 작업 디렉터리 기준(`eval/nn.bin`)으로 찾는다.
# 서버의 작업 디렉터리가 무엇이든 상관없도록 여기서 옮겨 놓고 실행한다.
RUN printf '#!/bin/sh\ncd /opt/yaneuraou\nexec ./engine "$@"\n' > /opt/yaneuraou/run \
    && printf '#!/bin/sh\ncd /opt/yaneuraou\nexec ./mate "$@"\n' > /opt/yaneuraou/run-mate \
    && chmod +x /opt/yaneuraou/run /opt/yaneuraou/run-mate

# ── 런타임 ──────────────────────────────────────────────────────────────────
#
# trixie(13)다. 위의 빌드 스테이지들과 세대가 다른 것이 의도다.
#
# bookworm 이 oldstable 이 되면서 perl 과 openssl 의 CVE 에 패치가 오지 않는다. 첫 스캔에서
# CRITICAL 이 넷이었고(journal §134) 그 넷이 perl 5.36.0-7+deb12u3 셋과
# openssl 3.0.20-1~deb12u2 하나인데, **apt-get upgrade 로는 고쳐지지 않는다** — 그 자리에서
# Candidate 가 Installed 와 같다. trixie 는 perl 5.40.1-6 · openssl 3.5.7-1~deb13u2 다.
#
# 엔진 스테이지는 옮기지 않았다. 그쪽은 이미지에 실리지 않고(/opt/yaneuraou 만 복사된다) CVE 에
# 기여하지 않는데, ARMV8 빌드가 통과한 조합이 하나뿐이라 건드리는 값이 크다(위).
# bookworm 에서 컴파일한 바이너리가 여기서 도는 것은 실측했다 — 탐색·詰み 둘 다
# usiok 와 readyok 를 준다(glibc 는 옛 바이너리에 하위 호환이고 libgomp.so.1 은 soname 이 같다).
FROM debian:trixie-slim

# ca-certificates 하나다. 엔진이 요구하는 공유 라이브러리는 libstdc++ · libm · libc ·
# libgcc_s 넷이고 전부 베이스에 이미 있다 — 한때 같이 깔던 libgomp1 은 ldd 로 확인해 보니
# 두 바이너리 어느 쪽도 링크하지 않는다(journal §134).
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=engine /opt/yaneuraou /opt/yaneuraou
ENV ENGINE_CMD=/opt/yaneuraou/run

# 水匠5가 요구하는 값이다. 기본값(24가 아님)으로 두면 cp 척도가 통째로 달라지고,
# 블런더 임계치가 그 위에서 잡힌다.
ENV ENGINE_FV_SCALE=24

# 詰将棋 solver. 세 자리가 쓴다 — 종반 판정 · 詰み 게이지 · 되짚기 퀴즈 생성.
# 개수는 ENGINE_MATE_POOL_SIZE 가 정한다(기본 2, docs/06-status.md §53).
ENV ENGINE_MATE_CMD=/opt/yaneuraou/run-mate

# 루트로 돌리지 않는다. 엔진은 신뢰할 수 없는 국면 문자열을 파싱하는 외부 바이너리다.
RUN useradd --system --create-home --uid 10001 app
USER app

COPY --from=build /out/api /usr/local/bin/api
EXPOSE 8080
CMD ["api", "-addr", ":8080"]
