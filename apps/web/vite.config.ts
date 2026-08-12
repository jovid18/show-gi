import { fileURLToPath } from 'node:url';

import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// Go 서버의 기본 주소. 다른 포트로 띄웠으면 SERVER_ORIGIN으로 덮는다.
const serverOrigin = process.env.SERVER_ORIGIN ?? 'http://localhost:8080';

const dir = (path: string): string => fileURLToPath(new URL(path, import.meta.url));

export default defineConfig({
  plugins: [react()],

  // **`@`는 `src/app`이다.** tsconfig.json의 paths와 짝을 맞춰야 한다.
  resolve: {
    alias: {
      '@': dir('./src/app'),
    },
  },

  server: {
    // 개발 중에도 서버가 같은 오리진에 있는 것처럼 보이게 한다.
    // 클라이언트가 개발·배포에서 똑같은 상대 경로(`/api`, `/ws`)를 쓰게 하려는 것이고,
    // 덤으로 WebSocket 핸드셰이크의 Origin 검사를 개발 환경에서만 따로 풀 필요가 없어진다.
    // **`changeOrigin: false`가 중요하다.** Vite의 기본값은 Host를 타깃(:8080)으로
    // 바꿔 보내는 것인데, 그러면 서버가 되짚는 주소가 `http://localhost:8080`이 되어
    // Google에 등록한 redirect URI(`http://localhost:5173/...`)와 어긋난다.
    // 프로덕션은 ALB도 Caddy도 Host를 보존하므로, 여기만 맞추면 양쪽이 같아진다.
    proxy: {
      '/healthz': serverOrigin,
      '/api': { target: serverOrigin, changeOrigin: false },
      '/ws': { target: serverOrigin, ws: true, changeOrigin: false },
    },
  },

  build: {
    target: 'es2022',
  },
});
