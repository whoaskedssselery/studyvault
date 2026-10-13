import { readFile, writeFile, mkdir, copyFile } from 'node:fs/promises';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
const frontend = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const destination = resolve(frontend, '../deliverables/frontend');
let html = await readFile(resolve(frontend, 'dist/index.html'), 'utf8');
const js = /<script\b[^>]*src="([^"]+)"[^>]*><\/script>/.exec(html);
const css = /<link\b[^>]*href="([^"]+\.css)"[^>]*>/.exec(html);
if (!js || !css) throw new Error('Expected one Vite entry script and stylesheet. Run npm run build first.');
function builtPath(asset) { const result = resolve(frontend, 'dist', asset.replace(/^\//, '')); if (!result.startsWith(resolve(frontend, 'dist') + '/'.replace('/', process.platform === 'win32' ? '\\' : '/'))) throw new Error('Unexpected build asset path'); return result; }
const script = (await readFile(builtPath(js[1]), 'utf8')).replace(/<\/script/gi, '<\\/script');
const style = (await readFile(builtPath(css[1]), 'utf8')).replace(/<\/style/gi, '<\\/style');
// Use callbacks: a bundle may contain literal "$&" or "$`" sequences that
// String.replace would otherwise interpret as replacement instructions.
html = html.replace(js[0], () => `<script type="module">${script}</script>`).replace(css[0], () => `<style>${style}</style>`).replace('<head>', '<head><meta name="studyvault-standalone" content="true">');
await mkdir(destination, { recursive: true });
await writeFile(resolve(destination, 'index.html'), html);
await copyFile(resolve(frontend, 'public/mock.json'), resolve(destination, 'mock.json'));
console.log('Submission ready: deliverables/frontend/index.html + mock.json (no npm needed to run).');
