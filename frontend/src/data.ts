import type { JSONContent } from '@tiptap/react';
export type User = { id: string; name: string; initials: string; faculty: string; color: string; email?: string };
export type Attachment = { name: string; size: number; dataUrl: string };
export type Note = {
  id: string; title: string; description: string; subject: string;
  type: 'Конспект' | 'Шпаргалка'; semester: number; tags: string[];
  authorId: string; createdAt: string; content: JSONContent; text: string;
  status: 'published' | 'pending' | 'rejected' | 'draft'; rejection?: string;
  ratings: Record<string, number>; saves: string[];
  comments: { id: string; userId: string; text: string; createdAt: string }[];
  attachments: Attachment[]; views: number;
};
export type Database = { version: 1; users: User[]; notes: Note[] };
export const subjects = ['Все предметы', 'Математика', 'Программирование', 'Базы данных', 'Физика', 'Английский язык', 'История'];
export const subjectColors: Record<string, string> = { Математика: 'purple', Программирование: 'blue', 'Базы данных': 'orange', Физика: 'green', 'Английский язык': 'pink', История: 'orange' };
export const demoUser: User = { id: 'demo', name: 'Алексей Смирнов', initials: 'АС', faculty: 'Информационные технологии · 2 курс', color: 'purple', email: 'alexey@example.test' };
export { repository, storageKey, sessionKey } from './repository';
export function average(note: Note) { const values = Object.values(note.ratings); return values.length ? values.reduce((a, b) => a + b, 0) / values.length : 0; }
export function formatDate(date: string) { return new Intl.DateTimeFormat('ru', { day: 'numeric', month: 'short' }).format(new Date(date)); }
