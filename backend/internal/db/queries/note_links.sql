-- Исходящие связи заметки: ссылки [[Название]] из её текста, разрешённые по названию среди живых
-- заметок того же пользователя (без регистра, без самой заметки). Одним запросом: лишние рёбра
-- удаляются, недостающие добавляются. Пустой список названий снимает все исходящие связи.
-- name: ReplaceOutgoingLinks :exec
WITH targets AS (
    SELECT n.id
    FROM notes n
    WHERE n.user_id = sqlc.arg('user_id')
      AND n.deleted_at IS NULL
      AND n.id <> sqlc.arg('source_id')
      AND lower(n.title) IN (SELECT lower(x) FROM unnest(sqlc.arg('titles')::text[]) AS x)
), removed AS (
    DELETE FROM note_links
    WHERE source_note_id = sqlc.arg('source_id')
      AND target_note_id NOT IN (SELECT id FROM targets)
)
INSERT INTO note_links (source_note_id, target_note_id)
SELECT sqlc.arg('source_id'), id FROM targets
ON CONFLICT DO NOTHING;

-- Что заметка упоминает как [[Название]]: хранится отдельно, чтобы входящие связи искать по индексу.
-- Названия приходят как написаны в тексте, ключом становится нижний регистр. Устаревшие записи удаляются.
-- name: ReplaceNoteMentions :exec
WITH wanted AS (
    SELECT DISTINCT lower(x) AS k FROM unnest(sqlc.arg('titles')::text[]) AS x
), removed AS (
    DELETE FROM note_mentions
    WHERE note_id = sqlc.arg('note_id') AND title_key NOT IN (SELECT k FROM wanted)
)
INSERT INTO note_mentions (note_id, title_key)
SELECT sqlc.arg('note_id'), k FROM wanted
ON CONFLICT DO NOTHING;

-- Входящие связи заметки: кто из живых заметок пользователя упоминает её текущее название.
-- Нужно, когда заметка создана или переименована позже, чем на неё сослались. Поиск идёт по индексу
-- упоминаний. Устаревшие рёбра (текст больше не содержит такого названия) удаляются.
-- name: ReplaceIncomingLinks :exec
WITH sources AS (
    SELECT n.id
    FROM note_mentions m
    JOIN notes n ON n.id = m.note_id
    WHERE m.title_key = lower(sqlc.arg('title')::text)
      AND n.user_id = sqlc.arg('user_id')
      AND n.deleted_at IS NULL
      AND n.id <> sqlc.arg('target_id')
), removed AS (
    DELETE FROM note_links
    WHERE target_note_id = sqlc.arg('target_id')
      AND source_note_id NOT IN (SELECT id FROM sources)
)
INSERT INTO note_links (source_note_id, target_note_id)
SELECT id, sqlc.arg('target_id') FROM sources
ON CONFLICT DO NOTHING;

-- name: ListBacklinks :many
SELECT n.id, n.subject_id, n.title, n.is_pinned, n.kind, n.status, n.library_subject_id, n.created_at, n.updated_at
FROM note_links l
JOIN notes n ON n.id = l.source_note_id
WHERE l.target_note_id = $1 AND n.user_id = $2 AND n.deleted_at IS NULL
ORDER BY n.updated_at DESC, n.id DESC;

-- name: ListGraphNodes :many
SELECT id, title FROM notes
WHERE user_id = $1 AND deleted_at IS NULL
ORDER BY id;

-- name: ListGraphEdges :many
SELECT l.source_note_id, l.target_note_id
FROM note_links l
JOIN notes s ON s.id = l.source_note_id
JOIN notes t ON t.id = l.target_note_id
WHERE s.user_id = $1 AND t.user_id = $1
  AND s.deleted_at IS NULL AND t.deleted_at IS NULL
ORDER BY l.source_note_id, l.target_note_id;
