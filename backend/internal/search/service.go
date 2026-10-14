package search

import (
	"context"
	"errors"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
)

const (
	defaultLimit = 20
	maxLimit     = 50
	// Сколько кандидатов просить у движка на каждый запрошенный результат: часть из них отсеет
	// проверка в БД и фильтры по предмету и тегу. Потолок — 200 кандидатов.
	candidateFactor = 4
	maxCandidates   = 200

	// Символы-маркеры подсветки. Их нет в нормальном тексте, поэтому после экранирования HTML
	// они однозначно превращаются в теги <mark>.
	markStart = "\x01"
	markEnd   = "\x02"
)

// ErrUnavailable означает, что поисковый движок не отвечает. Заметки при этом доступны как обычно.
var ErrUnavailable = errors.New("search is unavailable")

type Hit struct {
	ID      uuid.UUID
	Snippet string // с маркерами markStart и markEnd вокруг найденных слов
}

// Engine ищет кандидатов по тексту, жёстко ограничиваясь заметками одного пользователя.
type Engine interface {
	Search(ctx context.Context, userID uuid.UUID, query string, limit int) ([]Hit, error)
}

type Store interface {
	FilterLiveNotes(ctx context.Context, arg db.FilterLiveNotesParams) ([]db.FilterLiveNotesRow, error)
}

type Query struct {
	Text      string
	SubjectID *uuid.UUID
	Tag       *string
	Limit     int
}

type Result struct {
	ID        uuid.UUID
	Title     string
	Snippet   string // HTML: весь текст экранирован, найденные слова обёрнуты в <mark>
	SubjectID *uuid.UUID
	UpdatedAt time.Time
}

type Service struct {
	engine Engine
	store  Store
}

func NewService(engine Engine, store Store) *Service { return &Service{engine: engine, store: store} }

// Search берёт у движка кандидатов в порядке релевантности и перепроверяет их в Postgres.
// Движок лишь предлагает: чужие и удалённые заметки отсекает база, даже если индекс отстал.
func (s *Service) Search(ctx context.Context, userID uuid.UUID, q Query) ([]Result, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	limit = min(limit, maxLimit)

	hits, err := s.engine.Search(ctx, userID, q.Text, min(limit*candidateFactor, maxCandidates))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if len(hits) == 0 {
		return []Result{}, nil
	}

	ids := make([]uuid.UUID, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	arg := db.FilterLiveNotesParams{UserID: userID, Ids: ids}
	if q.SubjectID != nil {
		arg.SubjectID = uuid.NullUUID{UUID: *q.SubjectID, Valid: true}
	}
	if q.Tag != nil {
		arg.Tag = pgtype.Text{String: *q.Tag, Valid: true}
	}
	rows, err := s.store.FilterLiveNotes(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("check search results: %w", err)
	}
	live := make(map[uuid.UUID]db.FilterLiveNotesRow, len(rows))
	for _, r := range rows {
		live[r.ID] = r
	}

	results := make([]Result, 0, min(limit, len(rows)))
	for _, h := range hits {
		r, ok := live[h.ID]
		if !ok {
			continue
		}
		res := Result{ID: r.ID, Title: r.Title, Snippet: RenderSnippet(h.Snippet), UpdatedAt: r.UpdatedAt}
		if r.SubjectID.Valid {
			id := r.SubjectID.UUID
			res.SubjectID = &id
		}
		results = append(results, res)
		if len(results) == limit {
			break
		}
	}
	return results, nil
}

// renderSnippet экранирует весь текст (в заметке может быть что угодно, в том числе разметка)
// и только после этого превращает маркеры подсветки в <mark>.
func RenderSnippet(raw string) string {
	escaped := html.EscapeString(raw)
	escaped = strings.ReplaceAll(escaped, markStart, "<mark>")
	return strings.ReplaceAll(escaped, markEnd, "</mark>")
}
