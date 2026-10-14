import { useEffect, useRef, useState } from 'react';
import { CheckCircle2, Code2, Download, RefreshCw, Server, XCircle } from 'lucide-react';
import { downloadText } from './downloads';
import { apiBaseUrl } from './runtime';
type Result = { path: string; status: number | null; contentType: string; milliseconds: number; ok: boolean; message: string; body: unknown };
const endpoints = ['/health', '/notes', '/subjects'];
const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v);
async function inspect(path: string, signal: AbortSignal): Promise<Result> {
  const start = performance.now(); let status: number | null = null, contentType = '', body: unknown = null;
  try {
    const response = await fetch(`${apiBaseUrl}${path}`, { signal, credentials: 'omit', headers: { Accept: 'application/json' }, cache: 'no-store' });
    status = response.status; contentType = response.headers.get('content-type') || '';
    const raw = await response.text();
    try { body = JSON.parse(raw); } catch { body = raw.slice(0, 5000); }
    const jsonType = /^application\/(?:json|problem\+json)(?:\s*;|$)/i.test(contentType);
    if (!jsonType || !object(body)) return { path, status, contentType, milliseconds: Math.round(performance.now() - start), body, ok: false, message: 'Ответ не является JSON-объектом с Content-Type application/json или application/problem+json' };
    if (status === 502 && object(body.error) && body.error.code === 'api_unavailable') return { path, status, contentType, milliseconds: Math.round(performance.now() - start), body, ok: false, message: 'API не запущен. Библиотека продолжает работать на mock.json.' };
    if (path === '/health') {
      const ok = status === 200 && body.status === 'ok';
      return { path, status, contentType, milliseconds: Math.round(performance.now() - start), body, ok, message: ok ? 'Сервер и база данных доступны' : status === 503 && body.status === 'unavailable' ? 'JSON получен, но база данных недоступна' : 'Ответ /health не соответствует контракту' };
    }
    const problem = body.status === 401 && typeof body.type === 'string' && typeof body.title === 'string' && typeof body.code === 'string' && typeof body.detail === 'string';
    const legacy = object(body.error) && typeof body.error.code === 'string' && typeof body.error.message === 'string';
    const ok = status === 401 && (problem || legacy);
    return { path, status, contentType, milliseconds: Math.round(performance.now() - start), body, ok, message: ok ? 'Без access-токена доступ запрещён, ошибка в формате JSON' : 'Без access-токена ожидалась JSON-ошибка HTTP 401' };
  } catch { return { path, status, contentType, milliseconds: Math.round(performance.now() - start), body, ok: false, message: 'Сервер недоступен или не ответил за 5 секунд. Проверьте запуск API и адрес прокси.' }; }
}
export function ApiStatus() {
  const [results, setResults] = useState<Result[]>([]), [busy, setBusy] = useState(false), [checkedAt, setCheckedAt] = useState('');
  const pending = useRef<AbortController | null>(null);
  useEffect(() => () => pending.current?.abort(), []);
  const run = async () => { if (busy) return; const controller = new AbortController(); pending.current = controller; setBusy(true); const values = await Promise.all(endpoints.map(path => inspect(path, AbortSignal.any([controller.signal, AbortSignal.timeout(5000)])))); if (!controller.signal.aborted) { setResults(values); setCheckedAt(new Date().toISOString()); setBusy(false); } };
  return <><div className="page-heading"><div><div className="eyebrow">ПРОВЕРКА ДЛЯ КОМАНДЫ</div><h1>Ответы API<span className="heading-dot">.</span></h1><p>Проверьте доступность сервера и формат JSON прямо из браузера.</p></div><button className="button primary" disabled={busy} onClick={run}><RefreshCw size={17}/>{busy ? 'Проверяем…' : 'Проверить API'}</button></div>
    <div className="info-box">Библиотека работает на mock.json независимо от API. Здесь выполняются только GET-запросы: проверка /health и защиты /notes, /subjects без access-токена. Демоаккаунт не используется для авторизации на сервере.</div>
    <div className="api-summary"><Server size={24}/><div><strong>Что проверяется</strong><p>/v1/health → 200 и {'{"status":"ok"}'}. Защищённые методы → 401 и JSON с полями status, code, detail (RFC 7807).</p></div></div>
    {!results.length ? <div className="empty"><Code2 size={36}/><h2>Готово к проверке</h2><p>Запустите сервер API и нажмите «Проверить API».<br/>Если сервер ещё не готов, остальные страницы продолжат работать.</p></div> : <section className="api-results" aria-label="Результаты проверки API">{results.map(r => <article className="api-result" key={r.path}><div className="api-result-heading">{r.ok ? <CheckCircle2 className="api-pass"/> : <XCircle className="api-fail"/>}<h2><code>GET {r.path}</code></h2><span className={`api-code ${r.ok ? 'api-pass' : 'api-fail'}`}>{r.status ?? 'Нет ответа'}</span><span className="muted">{r.milliseconds} мс</span></div><p>{r.message}</p><span className="small-text muted">Content-Type: {r.contentType || 'не получен'}</span><details><summary>Посмотреть ответ</summary><pre>{typeof r.body === 'string' ? r.body : JSON.stringify(r.body, null, 2)}</pre></details></article>)}<button className="button secondary" onClick={() => downloadText('studyvault-api-check.json', JSON.stringify({ checkedAt, authenticated: false, results }, null, 2), 'application/json;charset=utf-8')}><Download size={17}/>Скачать результаты</button></section>}
  </>;
}
