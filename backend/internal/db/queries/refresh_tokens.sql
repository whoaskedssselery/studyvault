-- name: CreateRefreshToken :exec
INSERT INTO refresh_tokens (user_id, token_hash, user_agent, expires_at)
VALUES ($1, $2, $3, $4);

-- name: GetRefreshTokenByHash :one
SELECT * FROM refresh_tokens WHERE token_hash = $1;

-- Возвращает число изменённых строк: 0 значит, что токен уже отозван (или его нет).
-- Это делает ротацию атомарной: из двух параллельных запросов выиграет только один.
-- name: RevokeRefreshToken :execrows
UPDATE refresh_tokens SET revoked_at = now()
WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: RevokeAllUserRefreshTokens :exec
UPDATE refresh_tokens SET revoked_at = now()
WHERE user_id = $1 AND revoked_at IS NULL;

-- Удаляются только истёкшие токены. Отозванные, но ещё не истёкшие должны остаться:
-- по ним распознаётся повторное предъявление использованного токена.
-- name: DeleteExpiredRefreshTokens :execrows
DELETE FROM refresh_tokens WHERE expires_at <= $1;

-- Смена пароля стирает сессии, а не отзывает их: устаревшее устройство, предъявив старый токен, получит
-- обычный отказ, а не срабатывание защиты от кражи, которое закрыло бы и новую сессию.
-- name: DeleteUserRefreshTokens :exec
DELETE FROM refresh_tokens WHERE user_id = $1;
