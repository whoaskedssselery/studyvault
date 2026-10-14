-- +goose Up

-- Подтверждение почты. NULL: адрес не подтверждён. Аккаунты, созданные до этой миграции, считаются подтверждёнными:
-- требовать письмо с людей, которые уже пользуются сервисом, нечестно.
ALTER TABLE users ADD COLUMN email_verified_at timestamptz;
UPDATE users SET email_verified_at = created_at;

-- Сам токен не хранится, только его sha256, как у refresh-токенов и ссылок сброса пароля.
CREATE TABLE email_verification_tokens (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash text        NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX email_verification_tokens_user_idx ON email_verification_tokens (user_id);
CREATE INDEX email_verification_tokens_expires_idx ON email_verification_tokens (expires_at);

-- +goose Down

DROP TABLE email_verification_tokens;
ALTER TABLE users DROP COLUMN email_verified_at;
