import { test, expect } from '@playwright/test';
import path from 'node:path';
const base = 'http://127.0.0.1:4174';
test('capture the submitted app and unavailable real API for the report', async ({ page }) => {
  const folder = path.resolve('..', 'docs', 'assignment-2', 'screenshots');
  await page.route('https://**', route => route.abort());
  for (const [route, name] of [['/', '01-library'], ['/notes/note-2', '02-material'], ['/new', '03-editor'], ['/moderation', '04-moderation'], ['/api-status', '05-api-ready']]) {
    await page.goto(`${base}/#${route}`);
    await expect(page.locator('h1').first()).toBeVisible();
    if (route === '/new') {
      await page.getByLabel('Название материала').fill('Индексы в PostgreSQL: когда они помогают');
      await page.getByLabel('Краткое описание').fill('Конспект для практики по базам данных. Создаётся без ожидания API.');
      await page.getByRole('combobox', { name: /^Предмет/ }).selectOption('Базы данных');
      await page.getByRole('textbox', { name: 'Текст материала' }).fill('Индекс помогает быстрее находить строки, но требует памяти и замедляет вставку и обновление данных.\n\nПример\nCREATE INDEX notes_user_id_idx ON notes (user_id);\n\nПроверьте план запроса с EXPLAIN ANALYZE. Индекс полезен не для каждого запроса.');
      await page.getByLabel('Теги').fill('PostgreSQL, индексы, SQL');
      await page.getByLabel('Теги').blur();
    }
    await page.screenshot({ path: path.join(folder, `${name}.png`), fullPage: route !== '/' });
  }
  // This is a real local proxy request, not a mocked success. The endpoint may
  // be ready on another developer's machine, so capture the actual result.
  await page.getByRole('button', { name: 'Проверить API', exact: true }).click();
  await expect(page.locator('.api-result')).toHaveCount(3);
  await page.screenshot({ path: path.join(folder, '06-api-response.png'), fullPage: true });
  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Скачать результаты' }).click();
  await (await download).saveAs(path.join(folder, '..', 'api-check.json'));
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto(base);
  await expect(page.locator('.material-card')).toHaveCount(8);
  await page.screenshot({ path: path.join(folder, '07-mobile.png') });
});
test('mobile pages have no horizontal overflow or rendering errors', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  const errors: string[] = []; page.on('pageerror', error => errors.push(error.message));
  await page.route('https://**', route => route.abort());
  for (const route of ['/', '/notes/note-2', '/new', '/ranking', '/moderation', '/api-status', '/login', '/register']) {
    await page.goto(`${base}/#${route}`); await expect(page.locator('h1').first()).toBeVisible();
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  }
  expect(errors).toEqual([]);
});
