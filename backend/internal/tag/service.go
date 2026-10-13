package tag

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/limits"
)

var (
	// ErrNotFound и ErrNoteNotFound возвращаются и для чужих сущностей: снаружи они неотличимы.
	ErrNotFound     = errors.New("tag not found")
	ErrNoteNotFound = errors.New("note not found")
	ErrNameTaken    = errors.New("tag name already exists")
	// ErrInvalidTag: среди переданных id есть несуществующий или принадлежащий другому пользователю.
	ErrInvalidTag = errors.New("unknown tag id")
)

const pgUniqueViolation = "23505"

type Store interface {
	ListTags(ctx context.Context, userID uuid.UUID) ([]db.Tag, error)
	CreateTag(ctx context.Context, arg db.CreateTagParams) (db.Tag, error)
	DeleteTag(ctx context.Context, arg db.DeleteTagParams) (int64, error)
	NoteOwnedByUser(ctx context.Context, arg db.NoteOwnedByUserParams) (bool, error)
	CountTagsByUser(ctx context.Context, userID uuid.UUID) (int64, error)
	CountUserTags(ctx context.Context, arg db.CountUserTagsParams) (int64, error)
	ReplaceNoteTags(ctx context.Context, arg db.ReplaceNoteTagsParams) error
}

type Service struct{ store Store }

func NewService(store Store) *Service { return &Service{store: store} }

func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]db.Tag, error) {
	tags, err := s.store.ListTags(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	return tags, nil
}

func (s *Service) Create(ctx context.Context, userID uuid.UUID, name string) (db.Tag, error) {
	n, err := s.store.CountTagsByUser(ctx, userID)
	if err != nil {
		return db.Tag{}, fmt.Errorf("count tags: %w", err)
	}
	if n >= limits.MaxTagsPerUser {
		return db.Tag{}, limits.Reached("tags", limits.MaxTagsPerUser)
	}
	t, err := s.store.CreateTag(ctx, db.CreateTagParams{UserID: userID, Name: name})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return db.Tag{}, ErrNameTaken
	}
	if err != nil {
		return db.Tag{}, fmt.Errorf("create tag: %w", err)
	}
	return t, nil
}

func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	n, err := s.store.DeleteTag(ctx, db.DeleteTagParams{ID: id, UserID: userID})
	if err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetNoteTags заменяет набор тегов заметки целиком. Пустой список снимает все теги.
// Если хотя бы один id неизвестен пользователю, набор не меняется вовсе.
func (s *Service) SetNoteTags(ctx context.Context, userID, noteID uuid.UUID, tagIDs []uuid.UUID) error {
	owned, err := s.store.NoteOwnedByUser(ctx, db.NoteOwnedByUserParams{ID: noteID, UserID: userID})
	if err != nil {
		return fmt.Errorf("check note: %w", err)
	}
	if !owned {
		return ErrNoteNotFound
	}

	ids := unique(tagIDs)
	if len(ids) > 0 {
		found, err := s.store.CountUserTags(ctx, db.CountUserTagsParams{UserID: userID, TagIds: ids})
		if err != nil {
			return fmt.Errorf("check tags: %w", err)
		}
		if int(found) != len(ids) {
			return ErrInvalidTag
		}
	}

	err = s.store.ReplaceNoteTags(ctx, db.ReplaceNoteTagsParams{NoteID: noteID, UserID: userID, TagIds: ids})
	if err != nil {
		return fmt.Errorf("replace note tags: %w", err)
	}
	return nil
}

// unique убирает повторы и всегда возвращает не nil-срез: nil ушёл бы в SQL как NULL, а не как пустой массив.
func unique(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
