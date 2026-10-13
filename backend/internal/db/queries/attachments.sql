-- name: CreateAttachment :one
INSERT INTO attachments (user_id, note_id, object_key, file_name, mime_type, size_bytes)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetAttachment :one
SELECT * FROM attachments WHERE id = $1 AND user_id = $2;

-- name: ListAttachmentsByNote :many
SELECT * FROM attachments
WHERE note_id = $1 AND user_id = $2
ORDER BY created_at, id;

-- Возвращает удалённую строку: из неё берётся ключ объекта, который надо убрать из хранилища.
-- name: DeleteAttachment :one
DELETE FROM attachments WHERE id = $1 AND user_id = $2 RETURNING *;
