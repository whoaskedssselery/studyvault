-- +goose Up

-- Какие названия заметка упоминает как [[Название]]. Нужно, чтобы при создании или переименовании заметки
-- найти тех, кто на неё уже ссылался, поиском по индексу, а не перебором текста всех заметок пользователя
-- (на 20 000 заметок перебор занимал около 740 мс на каждое сохранение).
CREATE TABLE note_mentions (
    note_id   uuid NOT NULL REFERENCES notes (id) ON DELETE CASCADE,
    title_key text NOT NULL, -- название в нижнем регистре, без подписи после «|»
    PRIMARY KEY (note_id, title_key)
);

CREATE INDEX note_mentions_title_key_idx ON note_mentions (title_key);

-- Поиск заметки по названию при разрешении исходящих ссылок.
CREATE INDEX notes_user_title_idx ON notes (user_id, lower(title)) WHERE deleted_at IS NULL;

-- Перенос уже сохранённых заметок: те же правила, что в приложении (note.ParseLinks).
INSERT INTO note_mentions (note_id, title_key)
SELECT DISTINCT n.id, lower(btrim(split_part(m[1], '|', 1)))
FROM notes n
CROSS JOIN LATERAL regexp_matches(n.content_text, '\[\[([^\[\]\n]+)\]\]', 'g') AS m
WHERE btrim(split_part(m[1], '|', 1)) <> '';

-- +goose Down

DROP INDEX notes_user_title_idx;
DROP TABLE note_mentions;
