-- +goose Up
-- +goose StatementBegin

CREATE TABLE users (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text        NOT NULL,
    password_hash text        NOT NULL,
    display_name  text        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX users_email_lower_key ON users (lower(email));

CREATE TABLE refresh_tokens (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash text        NOT NULL UNIQUE,
    user_agent text,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX refresh_tokens_user_id_idx ON refresh_tokens (user_id);
CREATE INDEX refresh_tokens_expires_at_idx ON refresh_tokens (expires_at);

CREATE TABLE subjects (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title      text        NOT NULL,
    color      text,
    semester   smallint,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX subjects_user_id_idx ON subjects (user_id);

CREATE TABLE notes (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    subject_id   uuid        REFERENCES subjects (id) ON DELETE SET NULL,
    title        text        NOT NULL,
    content      jsonb       NOT NULL,
    content_text text        NOT NULL DEFAULT '',
    is_pinned    boolean     NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    deleted_at   timestamptz
);

CREATE INDEX notes_user_updated_idx ON notes (user_id, updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX notes_subject_id_idx ON notes (subject_id);

CREATE TABLE tags (
    id      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name    text NOT NULL,
    UNIQUE (user_id, name)
);

CREATE TABLE note_tags (
    note_id uuid NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
    tag_id  uuid NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (note_id, tag_id)
);

CREATE INDEX note_tags_tag_id_idx ON note_tags (tag_id);

CREATE TABLE note_links (
    source_note_id uuid NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
    target_note_id uuid NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
    PRIMARY KEY (source_note_id, target_note_id),
    CHECK (source_note_id <> target_note_id)
);

CREATE INDEX note_links_target_idx ON note_links (target_note_id);

CREATE TABLE attachments (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    note_id    uuid        REFERENCES notes (id) ON DELETE SET NULL,
    object_key text        NOT NULL UNIQUE,
    file_name  text        NOT NULL,
    mime_type  text        NOT NULL,
    size_bytes bigint      NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX attachments_note_id_idx ON attachments (note_id);
CREATE INDEX attachments_user_id_idx ON attachments (user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE attachments;
DROP TABLE note_links;
DROP TABLE note_tags;
DROP TABLE tags;
DROP TABLE notes;
DROP TABLE subjects;
DROP TABLE refresh_tokens;
DROP TABLE users;

-- +goose StatementEnd
