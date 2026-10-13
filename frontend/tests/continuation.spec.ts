import { test, expect } from '@playwright/test';
import { readFile } from 'node:fs/promises';
test('API diagnostics accept JSON health and unauthenticated errors, export results', async ({ page }) => {
  await page.route('**/backend-api/**', route => route.request().url().endsWith('/health') ? route.fulfill({ json: { status: 'ok' } }) : route.fulfill({ status: 401, json: { error: { code: 'unauthorized', message: 'access token required' } } }));
  await page.goto('/api-status');
  await page.getByRole('button', { name: 'Проверить API', exact: true }).click();
  await expect(page.locator('.api-result')).toHaveCount(3);
  await expect(page.locator('.api-result .api-fail')).toHaveCount(0);
  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Скачать результаты' }).click();
  const file = await download; expect(file.suggestedFilename()).toBe('studyvault-api-check.json');
  const report = JSON.parse(await readFile((await file.path())!, 'utf8')); expect(report.authenticated).toBe(false); expect(report.results.every((r: { ok: boolean }) => r.ok)).toBe(true);
});
test('API diagnostics report non-JSON and unavailable database', async ({ page }) => {
  await page.route('**/backend-api/**', route => route.request().url().endsWith('/health') ? route.fulfill({ status: 503, json: { status: 'unavailable' } }) : route.fulfill({ status: 500, contentType: 'text/html', body: '<h1>error</h1>' }));
  await page.goto('/api-status'); await page.getByRole('button', { name: 'Проверить API', exact: true }).click();
  await expect(page.getByText('JSON получен, но база данных недоступна')).toBeVisible();
  await expect(page.getByText('Ответ не является JSON-объектом с Content-Type application/json')).toHaveCount(2);
});
test('exported Markdown contains note content; deletion is confirmed and persists', async ({ page }) => {
  await page.goto('/notes/note-6');
  const download = page.waitForEvent('download'); await page.getByRole('button', { name: 'Скачать Markdown' }).click();
  const file = await download; expect(file.suggestedFilename()).toBe('studyvault-note-6.md');
  expect(await readFile((await file.path())!, 'utf8')).toContain('interface Shape');
  await page.getByRole('button', { name: 'Удалить материал', exact: true }).click();
  await page.getByRole('button', { name: 'Отмена', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'ООП: классы, наследование и полиморфизм' })).toBeVisible();
  await page.getByRole('button', { name: 'Удалить материал', exact: true }).click();
  await page.getByRole('button', { name: 'Подтвердить удаление', exact: true }).click();
  await page.goto('/notes/note-6'); await expect(page.getByRole('heading', { name: 'Материал не найден' })).toBeVisible();
});
