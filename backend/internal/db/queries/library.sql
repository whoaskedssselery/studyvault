-- ---------------------------------------------------------------------------
-- Общий список предметов библиотеки (ведёт модератор)
-- ---------------------------------------------------------------------------

-- name: ListLibrarySubjects :many
SELECT ls.id, ls.title, ls.color, ls.created_at,
       (SELECT count(*) FROM notes n
         WHERE n.library_subject_id = ls.id AND n.status = 'published' AND n.deleted_at IS NULL)::bigint AS materials_count
FROM library_subjects ls
ORDER BY lower(ls.title);

-- name: CreateLibrarySubject :one
INSERT INTO library_subjects (title, color) VALUES ($1, $2)
RETURNING *;

-- name: UpdateLibrarySubject :one
UPDATE library_subjects SET
    title = COALESCE(sqlc.narg('title')::text, title),
    color = CASE WHEN sqlc.arg('set_color')::boolean THEN sqlc.narg('color')::text ELSE color END
WHERE id = sqlc.arg('id')
RETURNING *;

-- Предмет, на который ссылаются материалы, удалить нельзя (ON DELETE RESTRICT): вернётся ошибка внешнего ключа.
-- name: DeleteLibrarySubject :execrows
DELETE FROM library_subjects WHERE id = $1;

-- ---------------------------------------------------------------------------
-- Лента библиотеки: только опубликованные материалы (так устроено представление library_items)
-- ---------------------------------------------------------------------------

-- Новые сверху. Курсор: (published_at, id) последней строки предыдущей страницы.
-- name: ListLibraryNewest :many
SELECT li.* FROM library_items li
WHERE (sqlc.narg('library_subject_id')::uuid IS NULL OR li.library_subject_id = sqlc.narg('library_subject_id'))
  AND (sqlc.narg('kind')::text IS NULL OR li.kind = sqlc.narg('kind'))
  AND (sqlc.narg('semester')::smallint IS NULL OR li.semester = sqlc.narg('semester'))
  AND (sqlc.narg('author_id')::uuid IS NULL OR li.user_id = sqlc.narg('author_id'))
  AND (sqlc.narg('tag')::text IS NULL OR sqlc.narg('tag')::text = ANY (li.tags))
  AND (sqlc.narg('cursor_published_at')::timestamptz IS NULL
       OR (li.published_at, li.id) < (sqlc.narg('cursor_published_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY li.published_at DESC, li.id DESC
LIMIT sqlc.arg('page_size');

-- Лучшие по средней оценке; при равенстве выигрывает материал с большим числом оценок.
-- name: ListLibraryTopRated :many
SELECT li.* FROM library_items li
WHERE (sqlc.narg('library_subject_id')::uuid IS NULL OR li.library_subject_id = sqlc.narg('library_subject_id'))
  AND (sqlc.narg('kind')::text IS NULL OR li.kind = sqlc.narg('kind'))
  AND (sqlc.narg('semester')::smallint IS NULL OR li.semester = sqlc.narg('semester'))
  AND (sqlc.narg('author_id')::uuid IS NULL OR li.user_id = sqlc.narg('author_id'))
  AND (sqlc.narg('tag')::text IS NULL OR sqlc.narg('tag')::text = ANY (li.tags))
  AND (sqlc.narg('cursor_rating')::float8 IS NULL
       OR (li.rating_avg, li.rating_count, li.id)
          < (sqlc.narg('cursor_rating')::float8, sqlc.narg('cursor_count')::bigint, sqlc.narg('cursor_id')::uuid))
ORDER BY li.rating_avg DESC, li.rating_count DESC, li.id DESC
LIMIT sqlc.arg('page_size');

-- Самые просматриваемые.
-- name: ListLibraryMostViewed :many
SELECT li.* FROM library_items li
WHERE (sqlc.narg('library_subject_id')::uuid IS NULL OR li.library_subject_id = sqlc.narg('library_subject_id'))
  AND (sqlc.narg('kind')::text IS NULL OR li.kind = sqlc.narg('kind'))
  AND (sqlc.narg('semester')::smallint IS NULL OR li.semester = sqlc.narg('semester'))
  AND (sqlc.narg('author_id')::uuid IS NULL OR li.user_id = sqlc.narg('author_id'))
  AND (sqlc.narg('tag')::text IS NULL OR sqlc.narg('tag')::text = ANY (li.tags))
  AND (sqlc.narg('cursor_views')::bigint IS NULL
       OR (li.views, li.id) < (sqlc.narg('cursor_views')::bigint, sqlc.narg('cursor_id')::uuid))
ORDER BY li.views DESC, li.id DESC
LIMIT sqlc.arg('page_size');

-- Перепроверка результатов поиска: движок предлагает кандидатов, а решает база. Снятый с публикации
-- или удалённый материал сюда не попадает, даже если индекс отстал.
-- name: ListLibraryByIDs :many
SELECT li.* FROM library_items li
WHERE li.id = ANY(sqlc.arg('ids')::uuid[])
  AND (sqlc.narg('library_subject_id')::uuid IS NULL OR li.library_subject_id = sqlc.narg('library_subject_id'))
  AND (sqlc.narg('kind')::text IS NULL OR li.kind = sqlc.narg('kind'))
  AND (sqlc.narg('semester')::smallint IS NULL OR li.semester = sqlc.narg('semester'))
  AND (sqlc.narg('author_id')::uuid IS NULL OR li.user_id = sqlc.narg('author_id'))
  AND (sqlc.narg('tag')::text IS NULL OR sqlc.narg('tag')::text = ANY (li.tags));

-- Личное состояние зрителя для набора материалов: его оценка и отметка «в избранном».
-- name: ListViewerState :many
SELECT n.id AS note_id,
       r.value AS my_rating,
       EXISTS (SELECT 1 FROM note_bookmarks b WHERE b.note_id = n.id AND b.user_id = sqlc.arg('viewer_id'))::boolean AS bookmarked
FROM notes n
LEFT JOIN note_ratings r ON r.note_id = n.id AND r.user_id = sqlc.arg('viewer_id')
WHERE n.id = ANY(sqlc.arg('note_ids')::uuid[]);

-- ---------------------------------------------------------------------------
-- Страница материала. Видимость: опубликованный видят все; черновик, отклонённый и материал на проверке — автор;
-- модератор видит всё, кроме чужих черновиков. Для остальных запрос не вернёт строк (то есть 404).
-- ---------------------------------------------------------------------------

-- name: GetMaterial :one
SELECT n.id, n.user_id, n.title, n.summary, n.kind, n.semester, n.status, n.published_at, n.views,
       n.library_subject_id, n.content, n.created_at, n.updated_at,
       ls.title                    AS subject_title,
       ls.color                    AS subject_color,
       u.display_name              AS author_name,
       u.faculty                   AS author_faculty,
       COALESCE(r.cnt, 0)::bigint  AS rating_count,
       COALESCE(r.avg, 0)::float8  AS rating_avg,
       COALESCE(c.cnt, 0)::bigint  AS comments_count,
       COALESCE(b.cnt, 0)::bigint  AS bookmarks_count
FROM notes n
JOIN users u ON u.id = n.user_id
LEFT JOIN library_subjects ls ON ls.id = n.library_subject_id
LEFT JOIN LATERAL (SELECT count(*) AS cnt, avg(value) AS avg FROM note_ratings WHERE note_id = n.id) r ON true
LEFT JOIN LATERAL (SELECT count(*) AS cnt FROM comments WHERE note_id = n.id AND deleted_at IS NULL) c ON true
LEFT JOIN LATERAL (SELECT count(*) AS cnt FROM note_bookmarks WHERE note_id = n.id) b ON true
WHERE n.id = sqlc.arg('id')
  AND n.deleted_at IS NULL
  AND (n.status = 'published'
       OR n.user_id = sqlc.narg('viewer_id')::uuid
       OR (sqlc.arg('is_moderator')::boolean AND n.status <> 'draft'));

-- Просмотр засчитывается только у опубликованных материалов.
-- name: IncrementViews :exec
UPDATE notes SET views = views + 1 WHERE id = $1 AND status = 'published' AND deleted_at IS NULL;

-- Автор опубликованного материала. Нет строки: материала нет, он не опубликован или удалён.
-- name: GetPublishedAuthor :one
SELECT user_id FROM notes WHERE id = $1 AND status = 'published' AND deleted_at IS NULL;

-- name: ListNoteAttachmentsAny :many
SELECT * FROM attachments WHERE note_id = $1 ORDER BY created_at, id;

-- name: GetNoteAttachment :one
SELECT * FROM attachments WHERE id = $1 AND note_id = $2;

-- ---------------------------------------------------------------------------
-- Комментарии
-- ---------------------------------------------------------------------------

-- name: CreateComment :one
INSERT INTO comments (note_id, user_id, body) VALUES ($1, $2, $3)
RETURNING *;

-- Старые сверху. Курсор: (created_at, id) последней строки предыдущей страницы.
-- name: ListComments :many
SELECT c.id, c.note_id, c.user_id, c.body, c.created_at, c.edited_at,
       u.display_name AS author_name, u.faculty AS author_faculty
FROM comments c
JOIN users u ON u.id = c.user_id
WHERE c.note_id = sqlc.arg('note_id')
  AND c.deleted_at IS NULL
  AND (sqlc.narg('cursor_created_at')::timestamptz IS NULL
       OR (c.created_at, c.id) > (sqlc.narg('cursor_created_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY c.created_at, c.id
LIMIT sqlc.arg('page_size');

-- Удалить комментарий может его автор или модератор. 0 строк: комментария нет или он чужой.
-- name: SoftDeleteComment :execrows
UPDATE comments SET deleted_at = now()
WHERE id = sqlc.arg('id') AND deleted_at IS NULL
  AND (user_id = sqlc.arg('user_id') OR sqlc.arg('is_moderator')::boolean);

-- ---------------------------------------------------------------------------
-- Оценки
-- ---------------------------------------------------------------------------

-- name: UpsertRating :exec
INSERT INTO note_ratings (note_id, user_id, value) VALUES ($1, $2, $3)
ON CONFLICT (note_id, user_id) DO UPDATE SET value = EXCLUDED.value, updated_at = now();

-- name: GetRatingSummary :one
SELECT count(*)::bigint AS rating_count, COALESCE(avg(value), 0)::float8 AS rating_avg
FROM note_ratings WHERE note_id = $1;

-- ---------------------------------------------------------------------------
-- Избранное
-- ---------------------------------------------------------------------------

-- name: AddBookmark :exec
INSERT INTO note_bookmarks (note_id, user_id) VALUES ($1, $2)
ON CONFLICT (note_id, user_id) DO NOTHING;

-- name: RemoveBookmark :exec
DELETE FROM note_bookmarks WHERE note_id = $1 AND user_id = $2;

-- Избранное пользователя: новые сверху; снятые с публикации материалы пропадают (их нет в library_items).
-- name: ListBookmarkedLibrary :many
SELECT li.id, li.user_id, li.title, li.summary, li.kind, li.semester, li.published_at, li.views,
       li.library_subject_id, li.subject_title, li.subject_color, li.author_name, li.author_faculty,
       li.rating_count, li.rating_avg, li.comments_count, li.bookmarks_count, li.tags,
       b.created_at AS bookmarked_at
FROM note_bookmarks b
JOIN library_items li ON li.id = b.note_id
WHERE b.user_id = sqlc.arg('user_id')
  AND (sqlc.narg('cursor_bookmarked_at')::timestamptz IS NULL
       OR (b.created_at, li.id) < (sqlc.narg('cursor_bookmarked_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY b.created_at DESC, li.id DESC
LIMIT sqlc.arg('page_size');

-- ---------------------------------------------------------------------------
-- Публикация и модерация
-- ---------------------------------------------------------------------------

-- Отправка автором на проверку: из черновика или после отказа. 0 строк: заметки нет или статус не тот.
-- name: SubmitNote :one
UPDATE notes SET status = 'pending', updated_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL AND status IN ('draft', 'rejected')
RETURNING *;

-- Состояние материала для решения модератора.
-- name: GetNoteStatus :one
SELECT user_id, status FROM notes WHERE id = $1 AND deleted_at IS NULL;

-- Решение принимается только по материалу, который сейчас на проверке (защита от гонки двух модераторов).
-- name: ApproveNote :one
UPDATE notes SET status = 'published', published_at = now()
WHERE id = $1 AND status = 'pending' AND deleted_at IS NULL
RETURNING *;

-- name: RejectNote :one
UPDATE notes SET status = 'rejected', published_at = NULL
WHERE id = $1 AND status = 'pending' AND deleted_at IS NULL
RETURNING *;

-- name: InsertModerationDecision :exec
INSERT INTO moderation_decisions (note_id, moderator_id, decision, reason) VALUES ($1, $2, $3, $4);

-- Причина последнего отказа: автор видит её рядом со своим материалом.
-- name: GetLastRejectionReason :one
SELECT reason FROM moderation_decisions
WHERE note_id = $1 AND decision IN ('rejected', 'unpublished')
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- Очередь модератора: старые сверху. Курсор: (updated_at, id) последней строки предыдущей страницы.
-- name: ListPendingMaterials :many
SELECT n.id, n.user_id, n.title, n.summary, n.kind, n.semester, n.library_subject_id,
       ls.title AS subject_title, ls.color AS subject_color,
       u.display_name AS author_name, u.faculty AS author_faculty,
       n.created_at, n.updated_at
FROM notes n
JOIN users u ON u.id = n.user_id
LEFT JOIN library_subjects ls ON ls.id = n.library_subject_id
WHERE n.status = 'pending' AND n.deleted_at IS NULL
  AND (sqlc.narg('cursor_updated_at')::timestamptz IS NULL
       OR (n.updated_at, n.id) > (sqlc.narg('cursor_updated_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
ORDER BY n.updated_at, n.id
LIMIT sqlc.arg('page_size');

-- Изменение вложений опубликованного материала возвращает его на проверку: иначе файл можно было бы
-- подменить уже после одобрения.
-- name: DemotePublishedNote :exec
UPDATE notes SET status = 'pending', published_at = NULL, updated_at = now()
WHERE id = $1 AND user_id = $2 AND status = 'published' AND deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- Отзыв с проверки и снятие с публикации
-- ---------------------------------------------------------------------------

-- Автор забирает материал с проверки обратно в черновики. 0 строк: заметки нет или она не на проверке.
-- name: WithdrawNote :one
UPDATE notes SET status = 'draft', updated_at = now()
WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL AND status = 'pending'
RETURNING *;

-- Модератор снимает опубликованный материал с публикации. Он становится отклонённым: автор видит причину,
-- исправляет и отправляет заново. Условие по статусу защищает от гонки с правкой автора.
-- name: UnpublishNote :one
UPDATE notes SET status = 'rejected', published_at = NULL
WHERE id = $1 AND status = 'published' AND deleted_at IS NULL
RETURNING *;

-- ---------------------------------------------------------------------------
-- Рейтинг и профили авторов
-- ---------------------------------------------------------------------------

-- Рейтинг автора: среднее арифметическое всех оценок всех его опубликованных материалов. Выше средняя оценка,
-- при равенстве больше оценок, затем больше материалов. У автора без оценок rating_avg равен 0, и он идёт в конце списка
-- (есть ли оценки, решает ratings_count: средняя без оценок не показывается).
-- name: ListAuthorRanking :many
SELECT u.id, u.display_name, u.faculty,
       count(*)::bigint                                  AS materials_count,
       COALESCE(sum(li.rating_count), 0)::bigint         AS ratings_count,
       COALESCE(sum(li.rating_avg * li.rating_count) / NULLIF(sum(li.rating_count), 0), 0)::float8 AS rating_avg,
       COALESCE(sum(li.views), 0)::bigint                AS views
FROM library_items li
JOIN users u ON u.id = li.user_id
GROUP BY u.id
ORDER BY rating_avg DESC NULLS LAST, ratings_count DESC, materials_count DESC, u.id
LIMIT sqlc.arg('page_size');

-- Профиль автора. Строка есть, только если у пользователя есть хотя бы один опубликованный материал: по остальным
-- аккаунтам профиль не отдаётся, чтобы нельзя было перебирать id и узнавать, кто зарегистрирован.
-- name: GetAuthorProfile :one
SELECT u.id, u.display_name, u.faculty,
       count(*)::bigint                                  AS materials_count,
       COALESCE(sum(li.rating_count), 0)::bigint         AS ratings_count,
       COALESCE(sum(li.rating_avg * li.rating_count) / NULLIF(sum(li.rating_count), 0), 0)::float8 AS rating_avg,
       COALESCE(sum(li.views), 0)::bigint                AS views
FROM library_items li
JOIN users u ON u.id = li.user_id
WHERE u.id = $1
GROUP BY u.id;

-- ---------------------------------------------------------------------------
-- Правка комментария
-- ---------------------------------------------------------------------------

-- Править комментарий может только его автор и только пока материал опубликован.
-- name: UpdateComment :one
UPDATE comments c SET body = sqlc.arg('body'), edited_at = now()
WHERE c.id = sqlc.arg('id') AND c.user_id = sqlc.arg('user_id') AND c.deleted_at IS NULL
  AND EXISTS (SELECT 1 FROM notes n WHERE n.id = c.note_id AND n.status = 'published' AND n.deleted_at IS NULL)
RETURNING c.*;
