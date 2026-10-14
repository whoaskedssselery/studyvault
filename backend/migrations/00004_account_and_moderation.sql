-- +goose Up

-- Сброс пароля по почте. Сам токен не хранится, только его sha256, как у refresh-токенов.
CREATE TABLE password_reset_tokens (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash text        NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX password_reset_tokens_user_idx ON password_reset_tokens (user_id);
CREATE INDEX password_reset_tokens_expires_idx ON password_reset_tokens (expires_at);

-- Правка комментария автором: время последней правки видно читателям.
ALTER TABLE comments ADD COLUMN edited_at timestamptz;

-- Снятие опубликованного материала с публикации модератором записывается в ту же историю решений.
-- Модератор, чей аккаунт удалён, не должен мешать удалению: решение остаётся, а автор решения становится неизвестным.
ALTER TABLE moderation_decisions
    DROP CONSTRAINT moderation_decisions_decision_check,
    DROP CONSTRAINT moderation_decisions_check,
    DROP CONSTRAINT moderation_decisions_moderator_id_fkey,
    ALTER COLUMN moderator_id DROP NOT NULL,
    ADD CONSTRAINT moderation_decisions_moderator_id_fkey FOREIGN KEY (moderator_id) REFERENCES users (id) ON DELETE SET NULL,
    ADD CONSTRAINT moderation_decisions_decision_check CHECK (decision IN ('approved', 'rejected', 'unpublished')),
    ADD CONSTRAINT moderation_decisions_reason_check CHECK (
        decision = 'approved' OR (reason IS NOT NULL AND char_length(btrim(reason)) BETWEEN 1 AND 1000));

-- Теги материала в карточке библиотеки. Столбец добавляется в конец: так представление можно заменить на месте.
CREATE OR REPLACE VIEW library_items AS
SELECT n.id,
       n.user_id,
       n.title,
       n.summary,
       n.kind,
       n.semester,
       n.published_at,
       n.views,
       n.library_subject_id,
       ls.title                      AS subject_title,
       ls.color                      AS subject_color,
       u.display_name                AS author_name,
       u.faculty                     AS author_faculty,
       COALESCE(r.cnt, 0)::bigint    AS rating_count,
       COALESCE(r.avg, 0)::float8    AS rating_avg,
       COALESCE(c.cnt, 0)::bigint    AS comments_count,
       COALESCE(b.cnt, 0)::bigint    AS bookmarks_count,
       COALESCE(tg.names, ARRAY[]::text[])::text[] AS tags
FROM notes n
JOIN users u ON u.id = n.user_id
LEFT JOIN library_subjects ls ON ls.id = n.library_subject_id
LEFT JOIN LATERAL (SELECT count(*) AS cnt, avg(value) AS avg FROM note_ratings WHERE note_id = n.id) r ON true
LEFT JOIN LATERAL (SELECT count(*) AS cnt FROM comments WHERE note_id = n.id AND deleted_at IS NULL) c ON true
LEFT JOIN LATERAL (SELECT count(*) AS cnt FROM note_bookmarks WHERE note_id = n.id) b ON true
LEFT JOIN LATERAL (SELECT array_agg(t.name ORDER BY t.name) AS names
                   FROM note_tags nt JOIN tags t ON t.id = nt.tag_id WHERE nt.note_id = n.id) tg ON true
WHERE n.status = 'published' AND n.deleted_at IS NULL;

-- +goose Down

DROP VIEW library_items;
CREATE VIEW library_items AS
SELECT n.id,
       n.user_id,
       n.title,
       n.summary,
       n.kind,
       n.semester,
       n.published_at,
       n.views,
       n.library_subject_id,
       ls.title                      AS subject_title,
       ls.color                      AS subject_color,
       u.display_name                AS author_name,
       u.faculty                     AS author_faculty,
       COALESCE(r.cnt, 0)::bigint    AS rating_count,
       COALESCE(r.avg, 0)::float8    AS rating_avg,
       COALESCE(c.cnt, 0)::bigint    AS comments_count,
       COALESCE(b.cnt, 0)::bigint    AS bookmarks_count
FROM notes n
JOIN users u ON u.id = n.user_id
LEFT JOIN library_subjects ls ON ls.id = n.library_subject_id
LEFT JOIN LATERAL (SELECT count(*) AS cnt, avg(value) AS avg FROM note_ratings WHERE note_id = n.id) r ON true
LEFT JOIN LATERAL (SELECT count(*) AS cnt FROM comments WHERE note_id = n.id AND deleted_at IS NULL) c ON true
LEFT JOIN LATERAL (SELECT count(*) AS cnt FROM note_bookmarks WHERE note_id = n.id) b ON true
WHERE n.status = 'published' AND n.deleted_at IS NULL;

DELETE FROM moderation_decisions WHERE decision = 'unpublished' OR moderator_id IS NULL;
ALTER TABLE moderation_decisions
    DROP CONSTRAINT moderation_decisions_reason_check,
    DROP CONSTRAINT moderation_decisions_decision_check,
    DROP CONSTRAINT moderation_decisions_moderator_id_fkey,
    ALTER COLUMN moderator_id SET NOT NULL,
    ADD CONSTRAINT moderation_decisions_moderator_id_fkey FOREIGN KEY (moderator_id) REFERENCES users (id),
    ADD CONSTRAINT moderation_decisions_decision_check CHECK (decision IN ('approved', 'rejected')),
    ADD CONSTRAINT moderation_decisions_check CHECK (
        decision = 'approved' OR (reason IS NOT NULL AND char_length(btrim(reason)) BETWEEN 1 AND 1000));

ALTER TABLE comments DROP COLUMN edited_at;
DROP TABLE password_reset_tokens;
