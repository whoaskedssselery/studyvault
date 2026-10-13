package note

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
)

const (
	defaultLimit = 30
	maxLimit     = 100
)

var (
	// ErrNotFound возвращается и для чужой, и для удалённой заметки: снаружи они неотличимы.
	ErrNotFound       = errors.New("note not found")
	ErrInvalidSubject = errors.New("subject does not exist")
	ErrInvalidCursor  = errors.New("invalid cursor")
)

type Store interface {
	SubjectOwnedByUser(ctx context.Context, arg db.SubjectOwnedByUserParams) (bool, error)
	NoteOwnedByUser(ctx context.Context, arg db.NoteOwnedByUserParams) (bool, error)
	CreateNote(ctx context.Context, arg db.CreateNoteParams) (db.Note, error)
	GetNote(ctx context.Context, arg db.GetNoteParams) (db.Note, error)
	UpdateNote(ctx context.Context, arg db.UpdateNoteParams) (db.Note, error)
	SoftDeleteNote(ctx context.Context, arg db.SoftDeleteNoteParams) (int64, error)
	ListNotes(ctx context.Context, arg db.ListNotesParams) ([]db.ListNotesRow, error)
	ListNoteTags(ctx context.Context, noteIds []uuid.UUID) ([]db.ListNoteTagsRow, error)
	ReplaceOutgoingLinks(ctx context.Context, arg db.ReplaceOutgoingLinksParams) error
	ReplaceIncomingLinks(ctx context.Context, arg db.ReplaceIncomingLinksParams) error
	ListBacklinks(ctx context.Context, arg db.ListBacklinksParams) ([]db.ListBacklinksRow, error)
	ListGraphNodes(ctx context.Context, userID uuid.UUID) ([]db.ListGraphNodesRow, error)
	ListGraphEdges(ctx context.Context, userID uuid.UUID) ([]db.NoteLink, error)
}

// TxFunc выполняет fn в одной транзакции над хранилищем. В продакшене это db.InTx.
type TxFunc func(ctx context.Context, fn func(Store) error) error

type Option func(*Service)

// WithTx включает транзакции: заметка и её связи записываются атомарно.
func WithTx(run TxFunc) Option { return func(s *Service) { s.inTx = run } }

type Tag struct {
	ID   uuid.UUID
	Name string
}

type Summary struct {
	ID        uuid.UUID
	SubjectID *uuid.UUID
	Title     string
	IsPinned  bool
	CreatedAt time.Time
	UpdatedAt time.Time
	Tags      []Tag
}

type Note struct {
	Summary
	Content []byte
}

type Input struct {
	Title     string
	Content   []byte
	SubjectID *uuid.UUID
	IsPinned  bool
}

// Patch описывает частичное изменение. Content == nil значит «не менять».
// subject_id можно очистить, поэтому «передан ли он» хранится отдельным флагом SetSubject.
type Patch struct {
	Title      *string
	Content    []byte
	SetSubject bool
	SubjectID  *uuid.UUID
	IsPinned   *bool
}

type ListFilter struct {
	SubjectID *uuid.UUID
	Tag       *string
	Pinned    *bool
	Limit     int
	Cursor    string
}

type Page struct {
	Items      []Summary
	NextCursor string // пустая строка: страница последняя
}

type GraphNode struct {
	ID    uuid.UUID
	Title string
}

type GraphEdge struct{ Source, Target uuid.UUID }

type Graph struct {
	Nodes []GraphNode
	Edges []GraphEdge
}

type Service struct {
	store Store
	inTx  TxFunc
}

func NewService(store Store, opts ...Option) *Service {
	s := &Service{store: store}
	// Без явной транзакции (юнит-тесты, вызов уже внутри транзакции) пишем прямо в хранилище.
	s.inTx = func(_ context.Context, fn func(Store) error) error { return fn(s.store) }
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *Service) Create(ctx context.Context, userID uuid.UUID, in Input) (Note, error) {
	if err := s.checkSubject(ctx, userID, in.SubjectID); err != nil {
		return Note{}, err
	}
	var created db.Note
	err := s.inTx(ctx, func(st Store) error {
		n, err := st.CreateNote(ctx, db.CreateNoteParams{
			UserID:      userID,
			SubjectID:   toNullUUID(in.SubjectID),
			Title:       in.Title,
			Content:     in.Content,
			ContentText: ExtractText(in.Content),
			IsPinned:    in.IsPinned,
		})
		if err != nil {
			return fmt.Errorf("create note: %w", err)
		}
		if err := replaceOutgoing(ctx, st, userID, n.ID, in.Content); err != nil {
			return err
		}
		// Другие заметки могли сослаться на это название раньше, чем заметка появилась.
		if err := replaceIncoming(ctx, st, userID, n.ID, in.Title); err != nil {
			return err
		}
		created = n
		return nil
	})
	if err != nil {
		return Note{}, err
	}
	return toNote(created, nil), nil
}

func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (Note, error) {
	n, err := s.store.GetNote(ctx, db.GetNoteParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Note{}, ErrNotFound
	}
	if err != nil {
		return Note{}, fmt.Errorf("get note: %w", err)
	}
	return s.withTags(ctx, n)
}

func (s *Service) Update(ctx context.Context, userID, id uuid.UUID, p Patch) (Note, error) {
	if p.SetSubject {
		if err := s.checkSubject(ctx, userID, p.SubjectID); err != nil {
			return Note{}, err
		}
	}
	arg := db.UpdateNoteParams{
		ID:         id,
		UserID:     userID,
		SetSubject: p.SetSubject,
		SubjectID:  toNullUUID(p.SubjectID),
		Content:    p.Content,
	}
	if p.Title != nil {
		arg.Title = pgtype.Text{String: *p.Title, Valid: true}
	}
	if p.IsPinned != nil {
		arg.IsPinned = pgtype.Bool{Bool: *p.IsPinned, Valid: true}
	}
	if p.Content != nil {
		arg.ContentText = pgtype.Text{String: ExtractText(p.Content), Valid: true}
	}

	var updated db.Note
	err := s.inTx(ctx, func(st Store) error {
		n, err := st.UpdateNote(ctx, arg)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("update note: %w", err)
		}
		// Связи пересобираются только там, где они могли измениться: исходящие при смене
		// текста, входящие при смене названия.
		if p.Content != nil {
			if err := replaceOutgoing(ctx, st, userID, id, p.Content); err != nil {
				return err
			}
		}
		if p.Title != nil {
			if err := replaceIncoming(ctx, st, userID, id, *p.Title); err != nil {
				return err
			}
		}
		updated = n
		return nil
	})
	if err != nil {
		return Note{}, err
	}
	return s.withTags(ctx, updated)
}

func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	n, err := s.store.SoftDeleteNote(ctx, db.SoftDeleteNoteParams{ID: id, UserID: userID})
	if err != nil {
		return fmt.Errorf("delete note: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, f ListFilter) (Page, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	limit = min(limit, maxLimit)

	arg := db.ListNotesParams{
		UserID:    userID,
		SubjectID: toNullUUID(f.SubjectID),
		PageSize:  int32(limit + 1), // лишняя строка нужна, чтобы узнать, есть ли следующая страница
	}
	if f.Pinned != nil {
		arg.Pinned = pgtype.Bool{Bool: *f.Pinned, Valid: true}
	}
	if f.Tag != nil {
		arg.Tag = pgtype.Text{String: *f.Tag, Valid: true}
	}
	if f.Cursor != "" {
		c, err := decodeCursor(f.Cursor)
		if err != nil {
			return Page{}, err
		}
		arg.CursorUpdatedAt = &c.UpdatedAt
		arg.CursorID = uuid.NullUUID{UUID: c.ID, Valid: true}
	}

	rows, err := s.store.ListNotes(ctx, arg)
	if err != nil {
		return Page{}, fmt.Errorf("list notes: %w", err)
	}

	page := Page{}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		page.NextCursor = encodeCursor(last.UpdatedAt, last.ID)
	}
	page.Items, err = s.summaries(ctx, rows)
	if err != nil {
		return Page{}, err
	}
	return page, nil
}

// Backlinks возвращает заметки, которые ссылаются на заданную через [[...]].
func (s *Service) Backlinks(ctx context.Context, userID, id uuid.UUID) ([]Summary, error) {
	owned, err := s.store.NoteOwnedByUser(ctx, db.NoteOwnedByUserParams{ID: id, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("check note: %w", err)
	}
	if !owned {
		return nil, ErrNotFound
	}
	linked, err := s.store.ListBacklinks(ctx, db.ListBacklinksParams{TargetNoteID: id, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("list backlinks: %w", err)
	}
	rows := make([]db.ListNotesRow, len(linked))
	for i, r := range linked {
		rows[i] = db.ListNotesRow(r) // поля совпадают один в один
	}
	return s.summaries(ctx, rows)
}

// Graph возвращает все живые заметки пользователя и связи между ними.
func (s *Service) Graph(ctx context.Context, userID uuid.UUID) (Graph, error) {
	nodes, err := s.store.ListGraphNodes(ctx, userID)
	if err != nil {
		return Graph{}, fmt.Errorf("list graph nodes: %w", err)
	}
	edges, err := s.store.ListGraphEdges(ctx, userID)
	if err != nil {
		return Graph{}, fmt.Errorf("list graph edges: %w", err)
	}
	g := Graph{Nodes: make([]GraphNode, 0, len(nodes)), Edges: make([]GraphEdge, 0, len(edges))}
	for _, n := range nodes {
		g.Nodes = append(g.Nodes, GraphNode{ID: n.ID, Title: n.Title})
	}
	for _, e := range edges {
		g.Edges = append(g.Edges, GraphEdge{Source: e.SourceNoteID, Target: e.TargetNoteID})
	}
	return g, nil
}

// checkSubject не даёт привязать заметку к предмету другого пользователя: внешний ключ
// в БД проверяет только существование предмета, но не владельца.
func (s *Service) checkSubject(ctx context.Context, userID uuid.UUID, subjectID *uuid.UUID) error {
	if subjectID == nil {
		return nil
	}
	owned, err := s.store.SubjectOwnedByUser(ctx, db.SubjectOwnedByUserParams{ID: *subjectID, UserID: userID})
	if err != nil {
		return fmt.Errorf("check subject: %w", err)
	}
	if !owned {
		return ErrInvalidSubject
	}
	return nil
}

func replaceOutgoing(ctx context.Context, st Store, userID, noteID uuid.UUID, content []byte) error {
	titles := ParseLinks(content)
	if titles == nil {
		titles = []string{} // пустой массив, а не NULL: иначе в SQL не отработает снятие связей
	}
	err := st.ReplaceOutgoingLinks(ctx, db.ReplaceOutgoingLinksParams{SourceID: noteID, UserID: userID, Titles: titles})
	if err != nil {
		return fmt.Errorf("replace outgoing links: %w", err)
	}
	return nil
}

func replaceIncoming(ctx context.Context, st Store, userID, noteID uuid.UUID, title string) error {
	err := st.ReplaceIncomingLinks(ctx, db.ReplaceIncomingLinksParams{TargetID: noteID, UserID: userID, Title: title})
	if err != nil {
		return fmt.Errorf("replace incoming links: %w", err)
	}
	return nil
}

func (s *Service) summaries(ctx context.Context, rows []db.ListNotesRow) ([]Summary, error) {
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	tags, err := s.tagsByNote(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, len(rows))
	for i, r := range rows {
		out[i] = Summary{
			ID:        r.ID,
			SubjectID: fromNullUUID(r.SubjectID),
			Title:     r.Title,
			IsPinned:  r.IsPinned,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
			Tags:      tags[r.ID],
		}
		if out[i].Tags == nil {
			out[i].Tags = []Tag{}
		}
	}
	return out, nil
}

func (s *Service) withTags(ctx context.Context, n db.Note) (Note, error) {
	tags, err := s.tagsByNote(ctx, []uuid.UUID{n.ID})
	if err != nil {
		return Note{}, err
	}
	return toNote(n, tags[n.ID]), nil
}

func (s *Service) tagsByNote(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID][]Tag, error) {
	out := map[uuid.UUID][]Tag{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.store.ListNoteTags(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list note tags: %w", err)
	}
	for _, r := range rows {
		out[r.NoteID] = append(out[r.NoteID], Tag{ID: r.TagID, Name: r.Name})
	}
	return out, nil
}

func toNote(n db.Note, tags []Tag) Note {
	if tags == nil {
		tags = []Tag{}
	}
	return Note{
		Summary: Summary{
			ID:        n.ID,
			SubjectID: fromNullUUID(n.SubjectID),
			Title:     n.Title,
			IsPinned:  n.IsPinned,
			CreatedAt: n.CreatedAt,
			UpdatedAt: n.UpdatedAt,
			Tags:      tags,
		},
		Content: n.Content,
	}
}

func toNullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}

func fromNullUUID(id uuid.NullUUID) *uuid.UUID {
	if !id.Valid {
		return nil
	}
	v := id.UUID
	return &v
}
