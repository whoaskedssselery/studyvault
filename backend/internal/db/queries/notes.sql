-- name: SubjectOwnedByUser :one
SELECT EXISTS (SELECT 1 FROM subjects WHERE id = $1 AND user_id = $2);

-- name: CreateNote :one
INSERT INTO notes (user_id, subject_id, title, content, content_text, is_pinned)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetNote :one
SELECT * FROM notes
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;

-- Список без содержимого. Страницы по ключу (updated_at, id): курсор — это последняя строка
-- предыдущей страницы. Все фильтры необязательные: NULL значит «не фильтровать».
-- name: ListNotes :many
SELECT n.id, n.subject_id, n.title, n.is_pinned, n.created_at, n.updated_at
FROM notes n
WHERE n.user_id = sqlc.arg('user_id')
  AND n.deleted_at IS NULL
  AND (sqlc.narg('subject_id')::uuid IS NULL OR n.subject_id = sqlc.narg('subject_id'))
  AND (sqlc.narg('pinned')::boolean IS NULL OR n.is_pinned = sqlc.narg('pinned'))
  AND (sqlc.narg('tag')::text IS NULL OR EXISTS (
        SELECT 1 FROM note_tags nt
        JOIN tags t ON t.id = nt.tag_id
        WHERE nt.note_id = n.id AND t.user_id = n.user_id AND t.name = sqlc.narg('tag')))
  AND (sqlc.narg('cursor_updated_at')::timestamptz IS NULL
       OR (n.updated_at, n.id) < (sqlc.narg('cursor_updated_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY n.updated_at DESC, n.id DESC
LIMIT sqlc.arg('page_size');

-- name: ListNoteTags :many
SELECT nt.note_id, t.id AS tag_id, t.name
FROM note_tags nt
JOIN tags t ON t.id = nt.tag_id
WHERE nt.note_id = ANY(sqlc.arg('note_ids')::uuid[])
ORDER BY t.name;

-- Частичное обновление. NULL в title, content, content_text и is_pinned значит «не менять».
-- subject_id может быть очищен, поэтому «менять или нет» передаётся флагом set_subject.
-- name: UpdateNote :one
UPDATE notes SET
    title        = COALESCE(sqlc.narg('title')::text, title),
    content      = COALESCE(sqlc.narg('content')::jsonb, content),
    content_text = COALESCE(sqlc.narg('content_text')::text, content_text),
    is_pinned    = COALESCE(sqlc.narg('is_pinned')::boolean, is_pinned),
    subject_id   = CASE WHEN sqlc.arg('set_subject')::boolean THEN sqlc.narg('subject_id')::uuid ELSE subject_id END,
    updated_at   = now()
WHERE id = sqlc.arg('id') AND user_id = sqlc.arg('user_id') AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteNote :execrows
UPDATE notes SET deleted_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL;
