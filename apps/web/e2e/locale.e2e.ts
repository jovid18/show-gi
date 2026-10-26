import { expect, test } from '@playwright/test';

import { FakeGame, fakeApi } from './fake';

test('言語を切り替えると画面が韓国語になり、再読み込みしても残る', async ({ page }) => {
  await fakeApi(page);
  await page.goto('/');
  await expect(page.getByRole('link', { name: /^対局/ })).toBeVisible();

  await page.getByRole('combobox', { name: '言語' }).selectOption('ko');
  await expect(page.getByRole('link', { name: /^대국/ })).toBeVisible();
  await expect(page.locator('html')).toHaveAttribute('lang', 'ko');
  await expect(page).toHaveTitle(/끼어들 때/);

  await page.reload();
  await expect(page.getByRole('link', { name: /^대국/ })).toBeVisible();
  await expect(page.getByRole('combobox', { name: '언어' })).toHaveValue('ko');
});

test.describe('ブラウザが韓国語なら', () => {
  test.use({ locale: 'ko-KR' });

  test('はじめから韓国語で開く', async ({ page }) => {
    await fakeApi(page);
    await page.goto('/');
    await expect(page.getByRole('link', { name: /^대국/ })).toBeVisible();
  });
});

// 연결을 쥔 훅은 문구를 ref 로 읽는다. 문구가 effect 의존성에 들어가면 언어를 바꿀 때 판이 새로 열린다.
test('対局中に言語を切り替えても接続し直さない', async ({ page }) => {
  await fakeApi(page);
  const game = await FakeGame.install(page);
  await page.goto('/');
  await page.getByRole('link', { name: /^対局/ }).click();
  await page.getByRole('button', { name: /^先手/ }).click();
  await page.getByRole('button', { name: '対局をはじめる' }).click();
  await game.connected();
  game.snapshot();
  await expect(page.getByText('あなたの番です。')).toBeVisible();

  await page.getByRole('combobox', { name: '言語' }).selectOption('ko');
  await expect(page.getByText('당신 차례입니다.')).toBeVisible();

  await page.getByRole('button', { name: '7七 歩', exact: true }).click();
  await page.getByRole('button', { name: '7六', exact: true }).click();
  expect(await game.next('move')).toEqual({ type: 'move', usi: '7g7f' });
  expect(game.connections).toBe(1);
});
