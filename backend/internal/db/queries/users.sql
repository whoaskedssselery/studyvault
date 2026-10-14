-- name: CreateUser :one
INSERT INTO users (email, password_hash, display_name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE lower(email) = lower($1);

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- Имя меняется, только если передано; факультет можно очистить, поэтому «менять или нет» задаёт флаг.
-- name: UpdateUserProfile :one
UPDATE users SET
    display_name = COALESCE(sqlc.narg('display_name')::text, display_name),
    faculty      = CASE WHEN sqlc.arg('set_faculty')::boolean THEN sqlc.narg('faculty')::text ELSE faculty END,
    updated_at   = now()
WHERE id = sqlc.arg('id')
RETURNING *;

-- Назначение роли: только доверенная операция на сервере (cmd/promote), через API она недоступна.
-- name: SetUserRoleByEmail :execrows
UPDATE users SET role = sqlc.arg('role'), updated_at = now() WHERE lower(email) = lower(sqlc.arg('email'));

-- name: UpdateUserPassword :execrows
UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1;
