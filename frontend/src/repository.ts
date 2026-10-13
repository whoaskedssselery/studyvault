import type { Database } from './data';
import { assertDatabase } from './mock';
import { mockUrl } from './runtime';
export const storageKey = 'studyvault.database.v1';
export const sessionKey = 'studyvault.session.v1';
export function readSession(): string | null { try { const value = localStorage.getItem(sessionKey); return value === null ? 'demo' : value || null; } catch { return 'demo'; } }
export const repository = {
  async read(): Promise<Database> {
    try { const raw = localStorage.getItem(storageKey); if (raw) { const cached: unknown = JSON.parse(raw); assertDatabase(cached); return cached; } } catch { /* A damaged local copy must not crash rendering. Reload the fixture. */ }
    let response: Response;
    try { response = await fetch(mockUrl(), { signal: AbortSignal.timeout(8000), cache: 'no-store' }); }
    catch { throw new Error('Не удалось загрузить mock.json. Откройте проект через локальный HTTP-сервер и повторите попытку.'); }
    if (!response.ok) throw new Error(`Не удалось загрузить mock.json: HTTP ${response.status}. Проверьте наличие файла рядом с index.html.`);
    let data: unknown;
    try { data = await response.json(); } catch { throw new Error('Файл mock.json содержит некорректный JSON. Исправьте файл и повторите загрузку.'); }
    assertDatabase(data);
    return data;
  },
  async write(db: Database) { assertDatabase(db); localStorage.setItem(storageKey, JSON.stringify(db)); return db; },
};
