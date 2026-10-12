-- name: CreateRefreshToken :exec
INSERT INTO refresh_tokens (user_id, token_hash, user_agent, expires_at)
VALUES ($1, $2, $3, $4);
