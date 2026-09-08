# 웹 이미지 = 빌드된 정적 파일 + Caddy. 컨테이너 하나가 진입점 전부다.
#
# 빌드 컨텍스트는 레포 루트다 (pnpm 워크스페이스라 루트의 lockfile이 필요하다):
#   docker build -f apps/web/Dockerfile .

FROM node:22-alpine AS build
RUN corepack enable
WORKDIR /repo

# 소스보다 매니페스트를 먼저 복사해야 소스만 바뀌었을 때 install 레이어가 재사용된다.
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json apps/web/
RUN pnpm install --frozen-lockfile

COPY tsconfig.base.json ./
COPY apps/web apps/web
RUN pnpm --filter @show-gi/web build

FROM caddy:2.10-alpine

# 태그가 고정이라 이미지 안의 알파인 패키지가 낡는다. 첫 스캔의 CRITICAL 넷이 전부
# openssl 3.5.5-r0 이었고(journal §134) 여기서는 upgrade 가 실제로 듣는다 —
# libssl3 · libcrypto3 가 3.5.8-r0 이 되고 musl 과 ca-certificates 도 같이 올라간다.
#
# Caddy 자체는 Go 라 이 라이브러리를 안 쓴다. 지우는 것은 「침입한 뒤에 쓸 도구」다.
RUN apk --no-cache upgrade

COPY --from=build /repo/apps/web/dist /srv
COPY apps/web/Caddyfile /etc/caddy/Caddyfile
EXPOSE 80 443
