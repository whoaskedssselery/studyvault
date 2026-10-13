-- name: ListSubjects :many
SELECT * FROM subjects
WHERE user_id = $1
ORDER BY semester, lower(title), id;

-- name: CreateSubject :one
INSERT INTO subjects (user_id, title, color, semester)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- Частичное обновление. title: NULL значит «не менять». Для color и semester NULL тоже
-- осмысленное значение (очистить поле), поэтому «менять или нет» передаётся отдельным флагом.
-- name: UpdateSubject :one
UPDATE subjects SET
    title    = COALESCE(sqlc.narg('title')::text, title),
    color    = CASE WHEN sqlc.arg('set_color')::boolean THEN sqlc.narg('color')::text ELSE color END,
    semester = CASE WHEN sqlc.arg('set_semester')::boolean THEN sqlc.narg('semester')::smallint ELSE semester END
WHERE id = sqlc.arg('id') AND user_id = sqlc.arg('user_id')
RETURNING *;

-- name: DeleteSubject :execrows
DELETE FROM subjects WHERE id = $1 AND user_id = $2;

-- name: CountSubjects :one
SELECT count(*) FROM subjects WHERE user_id = $1;
