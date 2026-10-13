import { test, expect } from '@playwright/test';
import { readFile } from 'node:fs/promises';
const fixture = JSON.parse(await readFile(new URL('../public/mock.json', import.meta.url), 'utf8'));
test('a fresh browser loads the separate JSON fixture', async ({ page }) => {
  const response = page.waitForResponse(r => new URL(r.url()).pathname === '/mock.json');
  await page.goto('/');
  expect((await response).ok()).toBe(true);
  await expect(page.locator('.material-card')).toHaveCount(8);
});
test('missing JSON has a useful error and retry recovers', async ({ page }) => {
  let unavailable = true;
  await page.route('**/mock.json', route => unavailable ? route.fulfill({ status: 404, body: 'Not found' }) : route.fulfill({ json: fixture }));
  await page.goto('/');
  await expect(page.getByRole('alert')).toContainText('HTTP 404');
  unavailable = false;
  await page.getByRole('button', { name: 'Повторить загрузку' }).click();
  await expect(page.locator('.material-card')).toHaveCount(8);
});
test('invalid JSON and missing authors are handled before rendering', async ({ page }) => {
  await page.route('**/mock.json', route => route.fulfill({ contentType: 'application/json', body: '{broken' }));
  await page.goto('/');
  await expect(page.getByRole('alert')).toContainText('некорректный JSON');
  await page.unroute('**/mock.json');
  const bad = structuredClone(fixture); bad.notes[0].authorId = 'missing';
  await page.route('**/mock.json', route => route.fulfill({ json: bad }));
  await page.getByRole('button', { name: 'Повторить загрузку' }).click();
  await expect(page.getByRole('alert')).toContainText('ссылки на авторов');
});
test('empty notes show an empty state; damaged local data falls back to JSON', async ({ page }) => {
  await page.addInitScript(() => localStorage.setItem('studyvault.database.v1', '{broken'));
  await page.route('**/mock.json', route => route.fulfill({ json: { ...fixture, notes: [] } }));
  await page.goto('/');
  await expect(page.getByRole('heading', { name: 'Пока ничего не нашлось' })).toBeVisible();
});
