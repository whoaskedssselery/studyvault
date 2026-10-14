package library

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/pgconv"
)

const maxCommentLen = 2000

type Comment struct {
	ID        uuid.UUID
	Body      string
	Author    Author
	CreatedAt time.Time
	EditedAt  *time.Time // nil, если комментарий не правили
}

type CommentPage struct {
	Items      []Comment
	NextCursor string
}

// ListComments отдаёт комментарии опубликованного материала, старые сверху. Гость тоже может их читать.
func (s *Service) ListComments(ctx context.Context, noteID uuid.UUID, limit int, cursor string) (CommentPage, error) {
	if _, err := s.publishedAuthor(ctx, noteID); err != nil {
		return CommentPage{}, err
	}
	limit = clampLimit(limit)
	arg := db.ListCommentsParams{NoteID: noteID, PageSize: int32(limit + 1)}
	if cursor != "" {
		var c timeCursor
		if err := decodeCursor(cursor, &c); err != nil {
			return CommentPage{}, err
		}
		arg.CursorCreatedAt = &c.At
		arg.CursorID = uuid.NullUUID{UUID: c.ID, Valid: true}
	}
	rows, err := s.store.ListComments(ctx, arg)
	if err != nil {
		return CommentPage{}, fmt.Errorf("list comments: %w", err)
	}
	page := CommentPage{}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		page.NextCursor = encodeCursor(timeCursor{At: last.CreatedAt, ID: last.ID})
	}
	page.Items = make([]Comment, len(rows))
	for i, r := range rows {
		page.Items[i] = Comment{
			ID:        r.ID,
			Body:      r.Body,
			Author:    Author{ID: r.UserID, DisplayName: r.AuthorName, Faculty: pgconv.FromText(r.AuthorFaculty)},
			CreatedAt: r.CreatedAt,
			EditedAt:  r.EditedAt,
		}
	}
	return page, nil
}

// AddComment сохраняет комментарий к опубликованному материалу. Пустой текст отвергается, а материал, которого нет
// или который не опубликован, даёт ErrNotFound.
func (s *Service) AddComment(ctx context.Context, uid, noteID uuid.UUID, body string) (Comment, error) {
	body = strings.TrimSpace(body)
	if n := utf8.RuneCountInString(body); n < 1 || n > maxCommentLen {
		return Comment{}, fmt.Errorf("%w: comment must be 1 to %d characters", ErrInvalid, maxCommentLen)
	}
	if err := s.requireVerified(ctx, uid); err != nil {
		return Comment{}, err
	}
	if _, err := s.publishedAuthor(ctx, noteID); err != nil {
		return Comment{}, err
	}
	author, err := s.store.GetUserByID(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return Comment{}, ErrNotFound
	}
	if err != nil {
		return Comment{}, fmt.Errorf("get user: %w", err)
	}
	c, err := s.store.CreateComment(ctx, db.CreateCommentParams{NoteID: noteID, UserID: uid, Body: body})
	if err != nil {
		return Comment{}, fmt.Errorf("create comment: %w", err)
	}
	return commentFromRow(c, author), nil
}

func commentFromRow(c db.Comment, author db.User) Comment {
	return Comment{
		ID:        c.ID,
		Body:      c.Body,
		Author:    Author{ID: author.ID, DisplayName: author.DisplayName, Faculty: pgconv.FromText(author.Faculty)},
		CreatedAt: c.CreatedAt,
		EditedAt:  c.EditedAt,
	}
}

// DeleteComment удаляет комментарий (мягко). Удалить может автор или модератор; для остальных комментарий
// неотличим от несуществующего.
func (s *Service) DeleteComment(ctx context.Context, uid, id uuid.UUID) error {
	moderator, err := s.isModerator(ctx, uid)
	if err != nil {
		return err
	}
	n, err := s.store.SoftDeleteComment(ctx, db.SoftDeleteCommentParams{ID: id, UserID: uid, IsModerator: moderator})
	if err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// RatingResult — сводка после оценки.
type RatingResult struct {
	Average  *float64
	Count    int64
	MyRating int16
}

// Rate ставит оценку 1-5; повторная оценка обновляет прежнюю. Оценивать можно только чужой опубликованный материал.
func (s *Service) Rate(ctx context.Context, uid, noteID uuid.UUID, value int) (RatingResult, error) {
	if value < 1 || value > 5 {
		return RatingResult{}, fmt.Errorf("%w: rating must be a whole number from 1 to 5", ErrInvalid)
	}
	if err := s.requireVerified(ctx, uid); err != nil {
		return RatingResult{}, err
	}
	author, err := s.publishedAuthor(ctx, noteID)
	if err != nil {
		return RatingResult{}, err
	}
	if author == uid {
		return RatingResult{}, ErrForbidden
	}
	if err := s.store.UpsertRating(ctx, db.UpsertRatingParams{NoteID: noteID, UserID: uid, Value: int16(value)}); err != nil {
		return RatingResult{}, fmt.Errorf("save rating: %w", err)
	}
	sum, err := s.store.GetRatingSummary(ctx, noteID)
	if err != nil {
		return RatingResult{}, fmt.Errorf("rating summary: %w", err)
	}
	res := RatingResult{Count: sum.RatingCount, MyRating: int16(value)}
	if sum.RatingCount > 0 {
		avg := sum.RatingAvg
		res.Average = &avg
	}
	return res, nil
}

// AddBookmark кладёт опубликованный материал в избранное; повтор ничего не меняет.
func (s *Service) AddBookmark(ctx context.Context, uid, noteID uuid.UUID) error {
	if _, err := s.publishedAuthor(ctx, noteID); err != nil {
		return err
	}
	if err := s.store.AddBookmark(ctx, db.AddBookmarkParams{NoteID: noteID, UserID: uid}); err != nil {
		return fmt.Errorf("add bookmark: %w", err)
	}
	return nil
}

// RemoveBookmark убирает материал из избранного. Если его там не было, это не ошибка: результат тот же.
func (s *Service) RemoveBookmark(ctx context.Context, uid, noteID uuid.UUID) error {
	if err := s.store.RemoveBookmark(ctx, db.RemoveBookmarkParams{NoteID: noteID, UserID: uid}); err != nil {
		return fmt.Errorf("remove bookmark: %w", err)
	}
	return nil
}

// Bookmarks отдаёт избранное пользователя, новые сверху. Снятые с публикации материалы пропадают из списка.
func (s *Service) Bookmarks(ctx context.Context, uid uuid.UUID, limit int, cursor string) (Page, error) {
	limit = clampLimit(limit)
	arg := db.ListBookmarkedLibraryParams{UserID: uid, PageSize: int32(limit + 1)}
	if cursor != "" {
		var c timeCursor
		if err := decodeCursor(cursor, &c); err != nil {
			return Page{}, err
		}
		arg.CursorBookmarkedAt = &c.At
		arg.CursorID = uuid.NullUUID{UUID: c.ID, Valid: true}
	}
	rows, err := s.store.ListBookmarkedLibrary(ctx, arg)
	if err != nil {
		return Page{}, fmt.Errorf("list bookmarks: %w", err)
	}
	page := Page{}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		page.NextCursor = encodeCursor(timeCursor{At: last.BookmarkedAt, ID: last.ID})
	}
	page.Items = make([]Item, len(rows))
	for i, r := range rows {
		page.Items[i] = itemFromBookmark(r)
	}
	if err := s.attachViewerState(ctx, &uid, page.Items); err != nil {
		return Page{}, err
	}
	return page, nil
}

// publishedAuthor возвращает автора материала, если тот опубликован; иначе ErrNotFound.
func (s *Service) publishedAuthor(ctx context.Context, noteID uuid.UUID) (uuid.UUID, error) {
	author, err := s.store.GetPublishedAuthor(ctx, noteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("get material: %w", err)
	}
	return author, nil
}

// EditComment меняет текст своего комментария. Чужой комментарий и комментарий к материалу, который уже не опубликован,
// неотличимы от несуществующих. Время правки видно читателям.
func (s *Service) EditComment(ctx context.Context, uid, id uuid.UUID, body string) (Comment, error) {
	body = strings.TrimSpace(body)
	if n := utf8.RuneCountInString(body); n < 1 || n > maxCommentLen {
		return Comment{}, fmt.Errorf("%w: comment must be 1 to %d characters", ErrInvalid, maxCommentLen)
	}
	if err := s.requireVerified(ctx, uid); err != nil {
		return Comment{}, err
	}
	c, err := s.store.UpdateComment(ctx, db.UpdateCommentParams{ID: id, UserID: uid, Body: body})
	if errors.Is(err, pgx.ErrNoRows) {
		return Comment{}, ErrNotFound
	}
	if err != nil {
		return Comment{}, fmt.Errorf("update comment: %w", err)
	}
	author, err := s.store.GetUserByID(ctx, uid)
	if err != nil {
		return Comment{}, fmt.Errorf("get user: %w", err)
	}
	return commentFromRow(c, author), nil
}
