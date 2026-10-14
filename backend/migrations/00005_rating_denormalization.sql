-- +goose Up

-- Рейтинг материала хранится в самой заметке. Раньше среднюю оценку и число оценок считали подзапросом для каждой
-- строки ленты, и сортировка по оценке перебирала всю библиотеку (на 20 000 материалов около 470 мс, и время росло вместе
-- с библиотекой). Теперь по этим столбцам есть индекс, и топ читается за миллисекунды.
ALTER TABLE notes
    ADD COLUMN rating_count bigint           NOT NULL DEFAULT 0,
    ADD COLUMN rating_avg   double precision NOT NULL DEFAULT 0;

UPDATE notes n SET rating_count = r.cnt, rating_avg = r.avg
FROM (SELECT note_id, count(*)::bigint AS cnt, avg(value)::float8 AS avg FROM note_ratings GROUP BY note_id) r
WHERE r.note_id = n.id;

-- Столбцы пересчитывает триггер, а не приложение: так они совпадают с таблицей оценок при любом способе записи,
-- включая каскадное удаление оценок вместе с аккаунтом. Сначала блокируется строка заметки: при двух одновременных
-- оценках второй пересчёт ждёт первого и видит его результат, а не затирает его устаревшим числом. Блокировка именно
-- FOR NO KEY UPDATE: внешний ключ оценки уже держит на заметке FOR KEY SHARE, и обычная FOR UPDATE конфликтовала бы
-- с ней у двух одновременных оценщиков (взаимная блокировка, её нашёл тест на настоящих соединениях).
-- +goose StatementBegin
CREATE FUNCTION refresh_note_rating() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    target uuid := COALESCE(NEW.note_id, OLD.note_id);
BEGIN
    PERFORM 1 FROM notes WHERE id = target FOR NO KEY UPDATE;
    UPDATE notes SET rating_count = s.cnt, rating_avg = s.avg
    FROM (SELECT count(*)::bigint AS cnt, COALESCE(avg(value), 0)::float8 AS avg FROM note_ratings WHERE note_id = target) s
    WHERE notes.id = target;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER note_ratings_refresh
    AFTER INSERT OR UPDATE OR DELETE ON note_ratings
    FOR EACH ROW EXECUTE FUNCTION refresh_note_rating();

CREATE INDEX notes_rating_idx ON notes (rating_avg DESC, rating_count DESC, id DESC)
    WHERE status = 'published' AND deleted_at IS NULL;

-- Поиск тега по названию (фильтр ленты по тегу): без индекса каждый запрос перебирал все теги всех авторов.
CREATE INDEX tags_name_idx ON tags (name);

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
       n.rating_count                AS rating_count,
       n.rating_avg                  AS rating_avg,
       COALESCE(c.cnt, 0)::bigint    AS comments_count,
       COALESCE(b.cnt, 0)::bigint    AS bookmarks_count,
       COALESCE(tg.names, ARRAY[]::text[])::text[] AS tags
FROM notes n
JOIN users u ON u.id = n.user_id
LEFT JOIN library_subjects ls ON ls.id = n.library_subject_id
LEFT JOIN LATERAL (SELECT count(*) AS cnt FROM comments WHERE note_id = n.id AND deleted_at IS NULL) c ON true
LEFT JOIN LATERAL (SELECT count(*) AS cnt FROM note_bookmarks WHERE note_id = n.id) b ON true
LEFT JOIN LATERAL (SELECT array_agg(t.name ORDER BY t.name) AS names
                   FROM note_tags nt JOIN tags t ON t.id = nt.tag_id WHERE nt.note_id = n.id) tg ON true
WHERE n.status = 'published' AND n.deleted_at IS NULL;

-- +goose Down

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

DROP INDEX tags_name_idx;
DROP INDEX notes_rating_idx;
DROP TRIGGER note_ratings_refresh ON note_ratings;
DROP FUNCTION refresh_note_rating();
ALTER TABLE notes DROP COLUMN rating_avg, DROP COLUMN rating_count;
