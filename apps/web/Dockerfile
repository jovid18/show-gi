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
COPY --from=build /repo/apps/web/dist /srv
COPY apps/web/Caddyfile /etc/caddy/Caddyfile
EXPOSE 80 443
