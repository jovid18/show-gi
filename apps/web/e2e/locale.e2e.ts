import { expect, test } from '@playwright/test';

import { fakeApi } from './fake';

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
