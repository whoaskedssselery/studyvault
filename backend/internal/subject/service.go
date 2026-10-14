package subject

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/limits"
	"github.com/whoaskedssselery/studyvault/backend/internal/pgconv"
)

// ErrNotFound возвращается и для чужого предмета: снаружи они неотличимы.
var ErrNotFound = errors.New("subject not found")

type Store interface {
	ListSubjects(ctx context.Context, userID uuid.UUID) ([]db.Subject, error)
	CountSubjects(ctx context.Context, userID uuid.UUID) (int64, error)
	CreateSubject(ctx context.Context, arg db.CreateSubjectParams) (db.Subject, error)
	UpdateSubject(ctx context.Context, arg db.UpdateSubjectParams) (db.Subject, error)
	DeleteSubject(ctx context.Context, arg db.DeleteSubjectParams) (int64, error)
}

type Input struct {
	Title    string
	Color    *string
	Semester *int16
}

// Patch описывает частичное изменение. Для color и semester null (очистить поле) и «не передано»
// различаются флагами Set*: при SetColor=true и Color=nil цвет очищается.
type Patch struct {
	Title       *string
	SetColor    bool
	Color       *string
	SetSemester bool
	Semester    *int16
}

type Service struct{ store Store }

func NewService(store Store) *Service { return &Service{store: store} }

func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]db.Subject, error) {
	items, err := s.store.ListSubjects(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list subjects: %w", err)
	}
	return items, nil
}

func (s *Service) Create(ctx context.Context, userID uuid.UUID, in Input) (db.Subject, error) {
	n, err := s.store.CountSubjects(ctx, userID)
	if err != nil {
		return db.Subject{}, fmt.Errorf("count subjects: %w", err)
	}
	if n >= limits.MaxSubjectsPerUser {
		return db.Subject{}, limits.Reached("subjects", limits.MaxSubjectsPerUser)
	}
	subj, err := s.store.CreateSubject(ctx, db.CreateSubjectParams{
		UserID:   userID,
		Title:    in.Title,
		Color:    pgconv.ToText(in.Color),
		Semester: pgconv.ToInt2(in.Semester),
	})
	if err != nil {
		return db.Subject{}, fmt.Errorf("create subject: %w", err)
	}
	return subj, nil
}

func (s *Service) Update(ctx context.Context, userID, id uuid.UUID, p Patch) (db.Subject, error) {
	subj, err := s.store.UpdateSubject(ctx, db.UpdateSubjectParams{
		ID:          id,
		UserID:      userID,
		Title:       pgconv.ToText(p.Title),
		SetColor:    p.SetColor,
		Color:       pgconv.ToText(p.Color),
		SetSemester: p.SetSemester,
		Semester:    pgconv.ToInt2(p.Semester),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Subject{}, ErrNotFound
	}
	if err != nil {
		return db.Subject{}, fmt.Errorf("update subject: %w", err)
	}
	return subj, nil
}

func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	n, err := s.store.DeleteSubject(ctx, db.DeleteSubjectParams{ID: id, UserID: userID})
	if err != nil {
		return fmt.Errorf("delete subject: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
