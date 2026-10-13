// Optional local server. The submission itself only needs index.html + mock.json.
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
const root = dirname(fileURLToPath(import.meta.url));
const option = process.argv.indexOf('--port');
const port = Number(option === -1 ? 4173 : process.argv[option + 1]);
if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('Port must be between 1 and 65535');
const target = new URL(process.env.STUDYVAULT_API_TARGET || 'http://127.0.0.1:8080');
if (!['http:', 'https:'].includes(target.protocol)) throw new Error('API target must be an HTTP(S) URL');
function json(res, status, value) { res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(value)); }
const server = createServer(async (req, res) => {
  const pathname = new URL(req.url, 'http://localhost').pathname;
  if (!['GET', 'HEAD'].includes(req.method)) return json(res, 405, { error: { code: 'method_not_allowed', message: 'This local server only reads files and diagnostic API endpoints.' } });
  if (pathname.startsWith('/backend-api/')) {
    const endpoint = pathname.slice('/backend-api'.length);
    if (!['/health', '/notes', '/subjects'].includes(endpoint)) return json(res, 404, { error: { code: 'not_found', message: 'Unknown diagnostic endpoint' } });
    try { const response = await fetch(new URL(endpoint, target), { headers: { Accept: 'application/json' }, signal: AbortSignal.timeout(5000), redirect: 'error' }); const body = await response.text(); res.writeHead(response.status, { 'Content-Type': response.headers.get('content-type') || 'text/plain', 'Cache-Control': 'no-store' }); res.end(req.method === 'HEAD' ? '' : body); }
    catch { json(res, 502, { error: { code: 'api_unavailable', message: 'Start the API server or set STUDYVAULT_API_TARGET.' } }); }
    return;
  }
  const file = { '/': 'index.html', '/index.html': 'index.html', '/mock.json': 'mock.json' }[pathname];
  if (!file) return json(res, 404, { error: { code: 'not_found', message: 'Use hash routes: /#/notes/note-1' } });
  try { const data = await readFile(resolve(root, file)); res.writeHead(200, { 'Content-Type': file.endsWith('.json') ? 'application/json; charset=utf-8' : 'text/html; charset=utf-8', 'Cache-Control': 'no-store' }); res.end(req.method === 'HEAD' ? undefined : data); }
  catch { json(res, 404, { error: { code: 'missing_file', message: 'Run npm run package:submission from frontend first.' } }); }
});
server.listen(port, '127.0.0.1', () => console.log(`StudyVault submission: http://127.0.0.1:${port}/`));
server.on('error', error => { console.error(error.message); process.exitCode = 1; });
