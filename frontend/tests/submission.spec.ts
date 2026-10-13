import { test, expect } from '@playwright/test';
const base = 'http://127.0.0.1:4174';
test('submitted HTML reads mock.json without external JavaScript or CSS files', async ({ page }) => {
  const assets: string[] = [], errors: string[] = [];
  page.on('request', r => { if (['script', 'stylesheet'].includes(r.resourceType()) && r.url().startsWith(base)) assets.push(r.url()); });
  page.on('pageerror', e => errors.push(e.message));
  await page.route('https://**', route => route.abort()); // The app must work offline except for its two local files.
  const json = page.waitForResponse(r => r.url() === `${base}/mock.json`);
  await page.goto(base);
  expect((await json).ok()).toBe(true);
  await expect(page.getByRole('heading', { name: 'Библиотека знаний.' })).toBeVisible();
  await expect(page.locator('.material-card')).toHaveCount(8);
  expect(assets).toEqual([]); expect(errors).toEqual([]);
  await page.getByRole('button', { name: 'Производные: от определения к практике', exact: true }).click();
  await expect(page).toHaveURL(/#\/notes\/note-1$/);
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Производные: от определения к практике' })).toBeVisible();
});
test('submitted editor publishes through mock moderation', async ({ page }) => {
  await page.goto(`${base}/#/new`);
  await page.getByLabel('Название материала').fill('Материал из сдаваемого HTML');
  await page.getByRole('textbox', { name: 'Текст материала' }).fill('Сборка работает с фейковыми данными и локальным сохранением.');
  await page.getByRole('button', { name: 'Отправить на проверку', exact: true }).click();
  await page.goto(`${base}/#/moderation`);
  await page.locator('.moderation-card').filter({ hasText: 'Материал из сдаваемого HTML' }).getByRole('button', { name: 'Одобрить', exact: true }).click();
  await page.goto(base);
  await page.getByRole('textbox', { name: 'Поиск материалов' }).fill('Материал из сдаваемого HTML');
  await expect(page.locator('.material-card')).toHaveCount(1);
});
