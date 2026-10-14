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

-- Удаление аккаунта: каскадом уходят заметки, предметы, теги, файлы (записи), комментарии, оценки, избранное и сессии.
-- Решения модератора остаются, но без указания, кто их принял.
-- name: DeleteUser :execrows
DELETE FROM users WHERE id = $1;

-- Сброс пароля по почте.
-- name: CreatePasswordResetToken :exec
INSERT INTO password_reset_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3);

-- Прежние неиспользованные ссылки пользователя гасятся, когда он просит новую: действует только последняя.
-- name: DeleteUserPasswordResetTokens :exec
DELETE FROM password_reset_tokens WHERE user_id = $1;

-- Токен «забирается» одним запросом: из двух одновременных подтверждений выиграет одно.
-- name: ClaimPasswordResetToken :one
UPDATE password_reset_tokens SET used_at = now()
WHERE token_hash = $1 AND used_at IS NULL AND expires_at > $2
RETURNING user_id;

-- name: DeleteExpiredPasswordResetTokens :execrows
DELETE FROM password_reset_tokens WHERE expires_at <= $1 OR used_at IS NOT NULL;
