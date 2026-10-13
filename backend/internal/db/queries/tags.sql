-- name: ListTags :many
SELECT * FROM tags WHERE user_id = $1 ORDER BY name;

-- name: CreateTag :one
INSERT INTO tags (user_id, name) VALUES ($1, $2) RETURNING *;

-- name: DeleteTag :execrows
DELETE FROM tags WHERE id = $1 AND user_id = $2;

-- name: NoteOwnedByUser :one
SELECT EXISTS (SELECT 1 FROM notes WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL);

-- name: CountUserTags :one
SELECT count(*) FROM tags WHERE user_id = $1 AND id = ANY(sqlc.arg('tag_ids')::uuid[]);

-- Заменяет набор тегов заметки одним запросом, поэтому либо применяется целиком, либо нет:
-- лишние связи удаляются, недостающие добавляются. Теги чужих пользователей не вставятся
-- даже при ошибке в вызывающем коде.
-- name: ReplaceNoteTags :exec
WITH removed AS (
    DELETE FROM note_tags
    WHERE note_id = sqlc.arg('note_id') AND tag_id <> ALL(sqlc.arg('tag_ids')::uuid[])
)
INSERT INTO note_tags (note_id, tag_id)
SELECT sqlc.arg('note_id'), t.id
FROM tags t
WHERE t.user_id = sqlc.arg('user_id') AND t.id = ANY(sqlc.arg('tag_ids')::uuid[])
ON CONFLICT DO NOTHING;
