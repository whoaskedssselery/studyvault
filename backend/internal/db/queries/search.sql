-- Перепроверка результатов поиска в Postgres: Meilisearch лишь предлагает кандидатов, а решает БД.
-- Чужие и удалённые заметки отсекаются здесь, даже если индекс устарел.
-- name: FilterLiveNotes :many
SELECT n.id, n.title, n.subject_id, n.updated_at
FROM notes n
WHERE n.user_id = sqlc.arg('user_id')
  AND n.deleted_at IS NULL
  AND n.id = ANY(sqlc.arg('ids')::uuid[])
  AND (sqlc.narg('subject_id')::uuid IS NULL OR n.subject_id = sqlc.narg('subject_id'))
  AND (sqlc.narg('tag')::text IS NULL OR EXISTS (
        SELECT 1 FROM note_tags nt
        JOIN tags t ON t.id = nt.tag_id
        WHERE nt.note_id = n.id AND t.user_id = n.user_id AND t.name = sqlc.narg('tag')));

-- Постраничная выгрузка живых заметок для полной пересборки индекса.
-- name: ListNotesForIndex :many
SELECT id, user_id, title, content_text
FROM notes
WHERE deleted_at IS NULL AND id > sqlc.arg('after')
ORDER BY id
LIMIT sqlc.arg('page_size');
