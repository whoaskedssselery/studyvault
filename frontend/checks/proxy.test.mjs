import { before, after, test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { fileURLToPath } from 'node:url';
let upstream, child, base;
const requests = [];
before(async () => {
  upstream = createServer(async (req, res) => {
    let body = ''; for await (const chunk of req) body += chunk;
    requests.push({ method: req.method, url: req.url, headers: req.headers, body });
    res.writeHead(200, { 'Content-Type': 'application/json', 'Set-Cookie': 'refresh_token=synthetic; HttpOnly; Path=/v1/auth; SameSite=Lax', 'Retry-After': '20', 'X-Request-ID': 'fixture-request' });
    res.end(JSON.stringify({ status: 'ok' }));
  });
  upstream.listen(0, '127.0.0.1'); await once(upstream, 'listening');
  const probe = createServer(); probe.listen(0, '127.0.0.1'); await once(probe, 'listening'); const port = probe.address().port; await new Promise(r => probe.close(r));
  base = `http://127.0.0.1:${port}`;
  child = spawn(process.execPath, [fileURLToPath(new URL('../../deliverables/frontend/serve.mjs', import.meta.url)), '--port', String(port)], { env: { ...process.env, STUDYVAULT_API_TARGET: `http://127.0.0.1:${upstream.address().port}` }, stdio: ['ignore', 'pipe', 'pipe'] });
  await Promise.race([once(child.stdout, 'data'), new Promise((_, reject) => setTimeout(() => reject(new Error('Proxy startup timeout')), 5000).unref())]);
});
after(async () => { if (child && child.exitCode === null) { child.kill(); await once(child, 'exit'); } upstream?.closeAllConnections(); if (upstream) await new Promise(r => upstream.close(r)); });
test('proxy forwards JSON writes, auth headers, cursors and rewrites refresh-cookie paths', async () => {
  const result = await fetch(`${base}/backend-api/auth/login`, { method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: 'Bearer synthetic', Cookie: 'refresh_token=synthetic' }, body: JSON.stringify({ email: 'synthetic@example.test', password: 'synthetic-password' }) });
  assert.equal(result.status, 200); assert.match(result.headers.get('set-cookie'), /Path=\/backend-api\/auth/); assert.equal(result.headers.get('retry-after'), '20'); assert.equal(result.headers.get('x-request-id'), 'fixture-request');
  await fetch(`${base}/backend-api/notes?cursor=opaque%2Bvalue&limit=30`);
  assert.equal(requests[0].method, 'POST'); assert.equal(requests[0].url, '/v1/auth/login'); assert.equal(JSON.parse(requests[0].body).email, 'synthetic@example.test'); assert.equal(requests[0].headers.authorization, 'Bearer synthetic'); assert.equal(requests[0].headers.cookie, 'refresh_token=synthetic'); assert.equal(requests[1].url, '/v1/notes?cursor=opaque%2Bvalue&limit=30');
});
test('proxy exposes only the scoped API and static submission files', async () => {
  assert.equal((await fetch(`${base}/backend-api/admin`)).status, 404);
  assert.equal((await fetch(`${base}/../../backend/go.mod`)).status, 404);
  assert.equal((await fetch(`${base}/mock.json`, { method: 'POST', body: 'overwrite' })).status, 405);
});
test('proxy supports the complete library, moderation and mail contract', async () => {
  const id = '11111111-1111-4111-8111-111111111111';
  for (const [method, path] of [['GET', '/library?q=SQL'], ['GET', '/library-subjects'], ['GET', `/library/${id}/comments`], ['PUT', `/library/${id}/bookmark`], ['PATCH', `/comments/${id}`], ['POST', `/notes/${id}/submit`], ['GET', '/moderation/queue'], ['POST', `/moderation/${id}/decision`], ['GET', '/ranking/authors'], ['GET', `/authors/${id}`], ['POST', '/auth/email-verification/confirm'], ['POST', '/auth/password-reset/confirm'], ['DELETE', '/me']]) {
    const response = await fetch(`${base}/backend-api${path}`, { method, headers: { 'Content-Type': 'application/json' }, body: method === 'GET' ? undefined : JSON.stringify({ test: true }) });
    assert.equal(response.status, 200, `${method} ${path}`);
    assert.equal(requests.at(-1).url, `/v1${path}`);
    assert.equal(requests.at(-1).method, method);
  }
});
