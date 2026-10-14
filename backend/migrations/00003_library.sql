-- +goose Up

-- Роль и факультет пользователя. Роль меняется только доверенной операцией на сервере (cmd/promote),
-- через API её назначить нельзя.
ALTER TABLE users
    ADD COLUMN role    text NOT NULL DEFAULT 'student' CHECK (role IN ('student', 'moderator')),
    ADD COLUMN faculty text CHECK (faculty IS NULL OR char_length(faculty) BETWEEN 1 AND 100);

-- Общий список предметов библиотеки. Им управляет модератор. Личные предметы пользователя (subjects)
-- остаются отдельно и относятся только к его личным заметкам.
CREATE TABLE library_subjects (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    title      text        NOT NULL CHECK (char_length(btrim(title)) BETWEEN 1 AND 200),
    color      text        CHECK (color IS NULL OR color ~ '^#[0-9a-fA-F]{6}$'),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX library_subjects_title_key ON library_subjects (lower(title));

-- Заметка становится материалом библиотеки, когда проходит проверку.
-- Допустимые переходы: draft -> pending, pending -> published | rejected, rejected -> pending.
-- Правка опубликованного материала возвращает его в pending (published_at при этом обнуляется).
ALTER TABLE notes
    ADD COLUMN kind               text     NOT NULL DEFAULT 'note' CHECK (kind IN ('note', 'cheat_sheet')),
    ADD COLUMN summary            text     CHECK (summary IS NULL OR char_length(summary) <= 500),
    ADD COLUMN semester           smallint CHECK (semester IS NULL OR semester BETWEEN 1 AND 12),
    ADD COLUMN status             text     NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'pending', 'published', 'rejected')),
    ADD COLUMN published_at       timestamptz,
    ADD COLUMN views              bigint   NOT NULL DEFAULT 0,
    ADD COLUMN library_subject_id uuid     REFERENCES library_subjects (id) ON DELETE RESTRICT,
    ADD CONSTRAINT notes_published_at_matches_status CHECK ((status = 'published') = (published_at IS NOT NULL));

CREATE INDEX notes_published_idx ON notes (published_at DESC, id DESC) WHERE status = 'published' AND deleted_at IS NULL;
CREATE INDEX notes_views_idx ON notes (views DESC, id DESC) WHERE status = 'published' AND deleted_at IS NULL;
CREATE INDEX notes_pending_idx ON notes (updated_at, id) WHERE status = 'pending' AND deleted_at IS NULL;
CREATE INDEX notes_library_subject_idx ON notes (library_subject_id) WHERE status = 'published' AND deleted_at IS NULL;

CREATE TABLE comments (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    note_id    uuid        NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    body       text        NOT NULL CHECK (char_length(btrim(body)) BETWEEN 1 AND 2000),
    created_at timestamptz NOT NULL DEFAULT now(),
    deleted_at timestamptz
);

CREATE INDEX comments_note_idx ON comments (note_id, created_at, id) WHERE deleted_at IS NULL;

-- Одна оценка на пару (материал, пользователь); повторная оценка обновляет прежнюю.
CREATE TABLE note_ratings (
    note_id    uuid        NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    value      smallint    NOT NULL CHECK (value BETWEEN 1 AND 5),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (note_id, user_id)
);

-- История решений модератора. При отказе причина обязательна.
CREATE TABLE moderation_decisions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    note_id      uuid        NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
    moderator_id uuid        NOT NULL REFERENCES users (id),
    decision     text        NOT NULL CHECK (decision IN ('approved', 'rejected')),
    reason       text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (decision = 'approved' OR (reason IS NOT NULL AND char_length(btrim(reason)) BETWEEN 1 AND 1000))
);

CREATE INDEX moderation_decisions_note_idx ON moderation_decisions (note_id, created_at DESC);

CREATE TABLE note_bookmarks (
    note_id    uuid        NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (note_id, user_id)
);

CREATE INDEX note_bookmarks_user_idx ON note_bookmarks (user_id, created_at DESC);

-- Карточка материала для списков библиотеки: сам материал, автор, общий предмет и сводка по оценкам,
-- комментариям и избранному. Содержимого здесь нет: оно тяжёлое и нужно только на странице материала.
-- Показываются только опубликованные и неудалённые материалы.
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

-- +goose Down

DROP VIEW library_items;
DROP TABLE note_bookmarks;
DROP TABLE moderation_decisions;
DROP TABLE note_ratings;
DROP TABLE comments;
ALTER TABLE notes
    DROP CONSTRAINT notes_published_at_matches_status,
    DROP COLUMN library_subject_id,
    DROP COLUMN views,
    DROP COLUMN published_at,
    DROP COLUMN status,
    DROP COLUMN semester,
    DROP COLUMN summary,
    DROP COLUMN kind;
DROP TABLE library_subjects;
ALTER TABLE users DROP COLUMN faculty, DROP COLUMN role;
