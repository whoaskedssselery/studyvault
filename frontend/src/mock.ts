import type { Database } from './data';
const record = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
const strings = (value: unknown): value is string[] => Array.isArray(value) && value.every(v => typeof v === 'string');
const date = (value: unknown) => typeof value === 'string' && Number.isFinite(Date.parse(value));
function documentNode(value: unknown, depth = 0): boolean { return depth < 50 && record(value) && typeof value.type === 'string' && (value.text === undefined || typeof value.text === 'string') && (value.content === undefined || Array.isArray(value.content) && value.content.every(v => documentNode(v, depth + 1))) && (value.marks === undefined || Array.isArray(value.marks) && value.marks.every(m => record(m) && typeof m.type === 'string')); }
export function assertDatabase(value: unknown): asserts value is Database {
  const fail = () => { throw new Error('Данные не соответствуют формату StudyVault. Проверьте mock.json: version, users, notes и ссылки на авторов.'); };
  if (!record(value) || value.version !== 1 || !Array.isArray(value.users) || !Array.isArray(value.notes)) return fail();
  const userIds = new Set<string>(), noteIds = new Set<string>();
  for (const u of value.users) {
    if (!record(u) || !['id', 'name', 'initials', 'faculty', 'color'].every(k => typeof u[k] === 'string') || !u.id || userIds.has(u.id as string) || (u.email !== undefined && typeof u.email !== 'string')) return fail();
    userIds.add(u.id as string);
  }
  if (!userIds.has('demo')) return fail();
  for (const n of value.notes) {
    if (!record(n) || !['id', 'title', 'description', 'subject', 'authorId', 'text'].every(k => typeof n[k] === 'string') || !n.id || noteIds.has(n.id as string) || !userIds.has(n.authorId as string) || !date(n.createdAt) || !['Конспект', 'Шпаргалка'].includes(n.type as string) || !['draft', 'pending', 'published', 'rejected'].includes(n.status as string) || !Number.isInteger(n.semester) || Number(n.semester) < 1 || Number(n.semester) > 12 || !strings(n.tags) || !strings(n.saves) || n.saves.some(id => !userIds.has(id)) || !Number.isFinite(n.views) || Number(n.views) < 0 || !documentNode(n.content) || !record(n.content) || n.content.type !== 'doc' || !record(n.ratings) || !Object.values(n.ratings).every(v => typeof v === 'number' && Number.isFinite(v) && v >= 1 && v <= 5) || (n.rejection !== undefined && typeof n.rejection !== 'string')) return fail();
    if (!Array.isArray(n.comments) || !n.comments.every(c => record(c) && typeof c.id === 'string' && typeof c.text === 'string' && userIds.has(c.userId as string) && date(c.createdAt))) return fail();
    if (!Array.isArray(n.attachments) || !n.attachments.every(a => record(a) && typeof a.name === 'string' && typeof a.size === 'number' && a.size >= 0 && typeof a.dataUrl === 'string' && /^data:(application\/pdf|image\/(png|jpeg|webp));base64,/.test(a.dataUrl))) return fail();
    noteIds.add(n.id as string);
  }
}
