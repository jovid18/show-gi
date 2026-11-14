import { expect, test, type Page } from '@playwright/test';

import { fakeApi } from './fake';
import type { HisshiResponse, TsumeResponse, TsumeSolve } from '../src/app/protocol/tsume';

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
    shortest: true,
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
    shortest: true,
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

test('いまの盤と同じ詰将棋を押し直しても、詰みを調べるは閉じない', async ({ page }) => {
  const saved: TsumeSolve = {
    id: 1,
    name: '',
    sfen: HEAD_GOLD,
    status: 'mate',
    plies: 1,
    shortest: true,
    solvedAt: '2026-10-27T00:00:00Z',
  };
  const fake = await signedIn(page, [saved]);
  await page.goto('/tsume');

  const panel = page.getByRole('region', { name: '解いた詰将棋' });
  const solveButton = page.getByRole('button', { name: '詰みを調べる' });
  await panel.getByRole('button', { name: /1手詰め/ }).click();
  await expect(solveButton).toBeEnabled();

  await panel.getByRole('button', { name: /1手詰め/ }).click();
  await expect.poll(() => fake.solved.length).toBe(2);
  await expect(solveButton).toBeEnabled();
});

test('名前を変えると一覧に出て、空にすると結果に戻る。削除は二度押す', async ({ page }) => {
  const saved: TsumeSolve = {
    id: 1,
    name: '',
    sfen: HEAD_GOLD,
    status: 'mate',
    plies: 1,
    shortest: true,
    solvedAt: '2026-10-27T00:00:00Z',
  };
  await signedIn(page, [saved]);
  const changed: { method: string; body: unknown }[] = [];
  await page.route('**/api/tsume/solves/1', async (r) => {
    const req = r.request();
    changed.push({ method: req.method(), body: req.method() === 'PATCH' ? req.postDataJSON() : null });
    await r.fulfill({ status: 204 });
  });
  await page.goto('/tsume');

  const panel = page.getByRole('region', { name: '解いた詰将棋' });
  await panel.getByRole('button', { name: '名前' }).click();
  await panel.getByLabel('新しい名前').fill('頭金');
  await panel.getByRole('button', { name: '変更' }).click();
  await expect(panel.getByRole('button', { name: /頭金/ })).toBeVisible();

  await panel.getByRole('button', { name: '名前' }).click();
  await panel.getByLabel('新しい名前').fill('');
  await panel.getByRole('button', { name: '変更' }).click();
  await expect(panel.getByRole('button', { name: /^1手詰め/ })).toBeVisible();

  await panel.getByRole('button', { name: '削除' }).click();
  expect(changed.map((c) => c.method)).toEqual(['PATCH', 'PATCH']);
  await panel.getByRole('button', { name: '本当に削除' }).click();
  await expect(panel.getByText('詰みを調べた局面は、ここに残ります。', { exact: false })).toBeVisible();
  expect(changed).toEqual([
    { method: 'PATCH', body: { name: '頭金' } },
    { method: 'PATCH', body: { name: '' } },
    { method: 'DELETE', body: null },
  ]);
});

test('最短を確かめられなかった詰みは、一覧で「N手詰め」と言い切らない', async ({ page }) => {
  const saved: TsumeSolve = {
    id: 1,
    name: '',
    sfen: HEAD_GOLD,
    status: 'mate',
    plies: 7,
    shortest: false,
    solvedAt: '2026-10-27T00:00:00Z',
  };
  await signedIn(page, [saved]);
  await page.goto('/tsume');

  const panel = page.getByRole('region', { name: '解いた詰将棋' });
  await expect(panel.getByRole('button', { name: /7手で詰み/ })).toBeVisible();
  await expect(panel.getByText('7手詰め')).toHaveCount(0);
});

// 1手必至의 답(journal §156). 응수 그룹의 나머지는 접혀 있고, 詰めろ 그룹에는 다른 응수가 없어도 표시가 붙는다.
const HISSHI_START = '5RSk1/6g1l/6s1p/9/9/9/9/9/9 b Sr2b3gs4n3l17p 1';
const HISSHI_AFTER = '5RSk1/6g1l/6sSp/9/9/9/9/9/9 w r2b3gs4n3l17p 2';

const HISSHI: HisshiResponse = {
  status: 'hisshi',
  message: '1手必至です。',
  answers: ['▲2三銀'],
  tree: {
    usi: 'S*2c',
    ja: '▲2三銀',
    sfen: HISSHI_AFTER,
    rest: 2,
    replies: [
      {
        usi: 'P*9i',
        ja: '△9九歩',
        sfen: HISSHI_AFTER,
        rest: 1,
        replies: [{ usi: 'S*2a', ja: '▲2一銀', sfen: HISSHI_AFTER, rest: 0 }],
        same: [
          { usi: 'P*8i', ja: '△8九歩', sfen: HISSHI_AFTER, rest: 1 },
          { usi: 'P*7i', ja: '△7九歩', sfen: HISSHI_AFTER, rest: 1 },
          { usi: 'P*6i', ja: '△6九歩', sfen: HISSHI_AFTER, rest: 1 },
        ],
      },
      {
        usi: '3b3a',
        ja: '△3一金',
        sfen: HISSHI_AFTER,
        rest: 1,
        threat: true,
        replies: [{ usi: '2c2b+', ja: '▲2二銀成', sfen: HISSHI_AFTER, rest: 0 }],
      },
    ],
  },
};

test('必至を調べると、同じ手順の応手は畳まれ、詰めろの手順には印が付く', async ({ page }) => {
  await signedIn(page, []);
  const asked: string[] = [];
  await page.route('**/api/tsume/hisshi', async (r) => {
    asked.push((r.request().postDataJSON() as { sfen: string }).sfen);
    await r.fulfill({ json: HISSHI });
  });
  await page.goto('/tsume');

  await page.getByLabel('文字で入力する（SFEN・USI・KIF）').fill(HISSHI_START);
  await page.getByRole('button', { name: 'この局面を使う' }).click();
  await page.getByRole('button', { name: '必至を調べる' }).click();

  await expect(page.getByText('1手必至です。')).toBeVisible();
  expect(asked).toEqual([HISSHI_START]);

  await page.getByRole('button', { name: '同じ手順で詰む応手 ほか3' }).click();
  await expect(page.getByRole('button', { name: '△7九歩' })).toBeVisible();

  // 詰めろ 그룹은 「ほかの応手」 아래에 있고, 같은 응수가 없어도 표시가 붙는다.
  await expect(page.getByText('受けになっていない')).toHaveCount(0);
  await page.getByRole('button', { name: 'ほかの応手 1' }).click();
  await expect(page.getByText('受けになっていない')).toBeVisible();
});
