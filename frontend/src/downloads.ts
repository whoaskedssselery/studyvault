import type { JSONContent } from '@tiptap/react';
import type { Note } from './data';
export function downloadText(name: string, text: string, type = 'text/plain;charset=utf-8') {
  const url = URL.createObjectURL(new Blob([text], { type }));
  const link = document.createElement('a'); link.href = url; link.download = name;
  document.body.append(link); link.click(); link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
function markdown(node: JSONContent): string {
  if (node.type === 'text') { let value = node.text || ''; for (const mark of node.marks || []) { if (mark.type === 'bold') value = `**${value}**`; if (mark.type === 'italic') value = `_${value}_`; if (mark.type === 'code') value = `\`${value}\``; if (mark.type === 'link' && /^https?:\/\//i.test(mark.attrs?.href || '')) value = `[${value}](${mark.attrs!.href})`; } return value; }
  const content = (node.content || []).map(markdown);
  if (node.type === 'heading') return `${'#'.repeat(Number(node.attrs?.level) || 2)} ${content.join('')}\n\n`;
  if (node.type === 'paragraph') return content.join('') + '\n\n';
  if (node.type === 'hardBreak') return '\n';
  if (node.type === 'blockquote') return content.join('').trim().split('\n').map(line => `> ${line}`).join('\n') + '\n\n';
  if (node.type === 'codeBlock') return '```\n' + (node.content || []).map(c => c.text || '').join('') + '\n```\n\n';
  if (node.type === 'bulletList' || node.type === 'orderedList') return content.map((line, i) => `${node.type === 'orderedList' ? `${i + 1}.` : '-'} ${line.trim()}`).join('\n') + '\n\n';
  if (node.type === 'horizontalRule') return '---\n\n';
  return content.join('');
}
export function exportNote(note: Note) { downloadText(`studyvault-${note.id}.md`, `# ${note.title}\n\n${note.subject} · ${note.semester} семестр · ${note.type}\n\n${markdown(note.content)}\nТеги: ${note.tags.join(', ')}\n`, 'text/markdown;charset=utf-8'); }
