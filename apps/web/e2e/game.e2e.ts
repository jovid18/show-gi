import { expect, test, type Page } from '@playwright/test';

import type { WhatIfNode } from '../src/app/protocol/whatif';

import { AFTER_34, AFTER_76, FakeGame, HIRATE, fakeApi } from './fake';

const square = (page: Page, label: string) => page.getByRole('button', { name: label, exact: true });

/** 홈에서 대국 준비 화면을 지나 판을 연다. */
async function open(page: Page, color: '先手' | '後手' = '先手'): Promise<FakeGame> {
  await fakeApi(page);
  const game = await FakeGame.install(page);
  await page.goto('/');
  await page.getByRole('link', { name: /^対局/ }).click();
  await page.getByRole('button', { name: new RegExp(`^${color}`) }).click();
  await page.getByRole('button', { name: '対局をはじめる' }).click();
  return game;
}

test('指した手が届き、相手の応手が盤と棋譜に載る', async ({ page }) => {
  const game = await open(page);
  expect((await game.connected()).searchParams.get('color')).toBe('b');

  game.snapshot();
  await expect(page.getByText('あなたの番です。')).toBeVisible();

  await square(page, '7七 歩').click();
  await square(page, '7六').click();
  expect(await game.next('move')).toEqual({ type: 'move', usi: '7g7f' });

  const first = { usi: '7g7f', ja: '▲7六歩', by: 'human' } as const;
  game.snapshot({
    sfen: AFTER_76,
    ply: 1,
    turn: 'w',
    yourTurn: false,
    thinking: true,
    legalMoves: null,
    moves: [first],
  });
  await expect(page.getByText('相手が考えています。')).toBeVisible();

  game.snapshot({
    sfen: AFTER_34,
    ply: 2,
    moves: [first, { usi: '3c3d', ja: '△3四歩', by: 'engine' }],
    canUndo: true,
  });
  await expect(page.getByText('あなたの番です。')).toBeVisible();
  await expect(page.getByText('△3四歩')).toBeVisible();
  await expect(square(page, '3四 歩')).toBeVisible();
  await expect(square(page, '7六 歩')).toBeVisible();
});

test('悪手は戻され、理由と「そのまま指していたら」が出る', async ({ page }) => {
  const game = await open(page);
  await game.connected();
  game.snapshot();

  await square(page, '7七 歩').click();
  await square(page, '7六').click();
  await game.next('move');

  // 서버는 물러진 판(처음 국면)과 개입을 한 스냅샷에 싣는다.
  game.snapshot({
    intervention: {
      kind: 'blunder',
      category: 'hangs_piece',
      retractedUsi: '7g7f',
      retractedJa: '▲7六歩',
      retractedSfen: AFTER_76,
      deltaWin: 0.3,
      lostMate: false,
      message: '角がそのまま取られてしまいます。',
    },
  });

  const card = page.getByRole('alert').filter({ hasText: 'を戻しました' });
  await expect(card).toBeVisible();
  await expect(card).toContainText('▲7六歩');
  await expect(card).toContainText('角がそのまま取られてしまいます。');
  await expect(card).toContainText('勝率 −30%');

  // 카드가 뜨면 물러진 수 뒤의 국면을 묻는다. 답의 후보가 카드에 줄로 선다.
  await game.next('whatif');
  const node: WhatIfNode = {
    basePly: 0,
    ply: 1,
    sfen: AFTER_76,
    turn: 'w',
    yourTurn: false,
    status: 'playing',
    legalMoves: null,
    line: [],
    candidates: [{ usi: '3c3d', ja: '△3四歩', evalCp: -50, lossCp: 0 }],
  };
  game.send({ type: 'whatif', whatif: node });
  await expect(card.getByRole('button', { name: /△3四歩/ })).toBeVisible();

  await card.getByRole('button', { name: '指し直す' }).click();
  await expect(card).toBeHidden();
  await expect(page.getByText('あなたの番です。')).toBeVisible();
  await expect(square(page, '7七 歩')).toBeEnabled();
});

test('投了すると結果のあとに総評が届き、もう一局で準備画面に戻る', async ({ page }) => {
  const game = await open(page);
  await game.connected();
  game.snapshot();

  await page.getByRole('button', { name: '投了', exact: true }).click();
  await page.getByRole('button', { name: '投了する' }).click();
  await game.next('resign');

  game.snapshot({ status: 'resigned', winner: 'engine', yourTurn: false, legalMoves: null });
  await expect(page.getByText('投了しました。')).toBeVisible();

  // 총평은 기록 저장을 기다려 늦게 온다(GameSummary).
  game.send({
    type: 'summary',
    summary: { body: 'まだ序盤でした。次の一局で続きを指しましょう。', stats: { playerMoves: 0, interventions: 0 } },
  });
  const summary = page.getByRole('region', { name: 'この対局のふりかえり' });
  await expect(summary).toContainText('まだ序盤でした。');

  await page.getByRole('button', { name: 'もう一局' }).click();
  await expect(page.getByText('対局のじゅんび')).toBeVisible();
});

test('後手で指すと自分の駒が手前に来る', async ({ page }) => {
  const game = await open(page, '後手');
  expect((await game.connected()).searchParams.get('color')).toBe('w');

  game.snapshot({ sfen: HIRATE, yourColor: 'w', yourTurn: false, thinking: true, legalMoves: null });
  await expect(page.getByText('相手が考えています。')).toBeVisible();

  // 판의 칸은 왼쪽 위부터 늘어선다. 뒤집히지 않았으면 첫 칸이 9一(後手의 香)이다.
  const squares = page.locator('.board .square');
  await expect(squares.first()).toHaveAccessibleName('1九 香');
  await expect(squares.last()).toHaveAccessibleName('9一 香');
});
