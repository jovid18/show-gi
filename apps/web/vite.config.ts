import { fileURLToPath } from 'node:url';

import react from '@vitejs/plugin-react';
import { defineConfig } from 'vite';

// Go 서버의 기본 주소. 다른 포트로 띄웠으면 SERVER_ORIGIN으로 덮는다.
const serverOrigin = process.env.SERVER_ORIGIN ?? 'http://localhost:8080';

export default defineConfig({
  plugins: [react()],

  // `@`는 src다. tsconfig.json의 paths와 짝을 맞춰야 한다.
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },

  server: {
    // 개발 중에도 서버가 같은 오리진에 있는 것처럼 보이게 한다.
    // 클라이언트가 개발·배포에서 똑같은 상대 경로(`/api`, `/ws`)를 쓰게 하려는 것이고,
    // 덤으로 WebSocket 핸드셰이크의 Origin 검사를 개발 환경에서만 따로 풀 필요가 없어진다.
    proxy: {
      '/healthz': serverOrigin,
      '/api': serverOrigin,
      '/ws': { target: serverOrigin, ws: true },
    },
  },

  build: {
    target: 'es2022',
  },
});
