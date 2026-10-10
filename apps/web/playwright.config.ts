import { defineConfig, devices } from '@playwright/test';

// 서버 없이 화면만 띄운다. `/api` 와 `/ws` 는 테스트가 브라우저 안에서 가로챈다(e2e/fake.ts).
// 프록시가 향하는 곳을 닫힌 포트로 두어, 가로채지 않은 요청이 로컬 api 에 붙지 않게 한다.
const port = 4179;

export default defineConfig({
  testDir: './e2e',
  // vitest 가 `*.test.ts` 를 집으므로 이름을 가른다.
  testMatch: '**/*.e2e.ts',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['github'], ['list']] : 'list',
  use: {
    baseURL: `http://localhost:${port}`,
    locale: 'ja-JP',
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: {
    command: `vite --port ${port} --strictPort`,
    url: `http://localhost:${port}`,
    reuseExistingServer: !process.env.CI,
    env: { SERVER_ORIGIN: 'http://127.0.0.1:9' },
  },
});
