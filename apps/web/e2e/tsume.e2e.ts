import { expect, test, type Page } from '@playwright/test';

import { fakeApi } from './fake';
import type { TsumeResponse, TsumeSolve } from '../src/app/protocol/tsume';

// 푼 詰め将棋의 기록(journal §155). 다시 열면 같은 국면으로 풀기를 부르고, 풀면 목록을 다시 묻는다.

const HEAD_GOLD = '4k4/9/4P4/9/9/9/9/9/9 b G 1';

const MATE: TsumeResponse = {
  status: 'mate',
  plies: 1,
  shortest: true,
  message: '1手詰めです。',
  tree: { usi: 'G*5b', ja: '▲5二金', sfen: '4k4/4G4/4P4/9/9/9/9/9/9 w - 2', rest: 0 },
};

/** `before` 는 풀기 전의 목록, `after` 는 한 번이라도 푼 뒤의 목록이다. 개발 서버의 StrictMode 가 처음 묻기를 두 번 하므로 횟수로 가르지 않는다. */
async function signedIn(page: Page, before: TsumeSolve[], after = before): Promise<{ solved: string[] }> {
  await fakeApi(page);
  await page.route('**/api/me', (r) => r.fulfill({ json: { enabled: true, user: { id: 7, name: 'さとし' } } }));
  await page.route('**/api/position/check', async (r) => {
    const body = r.request().postDataJSON() as { sfen?: string; text?: string };
    await r.fulfill({ json: { sfen: body.sfen ?? body.text, faults: [], warnings: [] } });
  });
  const solved: string[] = [];
  await page.route('**/api/tsume/solve', async (r) => {
    solved.push((r.request().postDataJSON() as { sfen: string }).sfen);
    await r.fulfill({ json: MATE });
  });
  await page.route('**/api/tsume/solves', (r) => r.fulfill({ json: { solves: solved.length === 0 ? before : after } }));
  return { solved };
}

test('解いた詰将棋を押すと、その局面をもう一度解く', async ({ page }) => {
  const saved: TsumeSolve = {
    id: 1,
    name: '',
    sfen: HEAD_GOLD,
    status: 'mate',
    plies: 1,
    solvedAt: '2026-10-27T00:00:00Z',
  };
  const fake = await signedIn(page, [saved]);
  await page.goto('/tsume');

  const panel = page.getByRole('region', { name: '解いた詰将棋' });
  await panel.getByRole('button', { name: /1手詰め/ }).click();

  await expect(page.getByText('1手詰めです。')).toBeVisible();
  expect(fake.solved).toEqual([HEAD_GOLD]);
});

test('解くと、解いた詰将棋の一覧を読み直す', async ({ page }) => {
  const saved: TsumeSolve = {
    id: 1,
    name: '',
    sfen: HEAD_GOLD,
    status: 'mate',
    plies: 1,
    solvedAt: '2026-10-27T00:00:00Z',
  };
  await signedIn(page, [], [saved]);
  await page.goto('/tsume');

  const panel = page.getByRole('region', { name: '解いた詰将棋' });
  await expect(panel.getByText('詰みを調べた局面は、ここに残ります。', { exact: false })).toBeVisible();

  await page.getByLabel('文字で入力する（SFEN・USI・KIF）').fill(HEAD_GOLD);
  await page.getByRole('button', { name: 'この局面を使う' }).click();
  await page.getByRole('button', { name: '詰みを調べる' }).click();

  await expect(page.getByText('1手詰めです。')).toBeVisible();
  await expect(panel.getByRole('button', { name: /1手詰め/ })).toBeVisible();
});
