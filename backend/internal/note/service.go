package note

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/limits"
	"github.com/whoaskedssselery/studyvault/backend/internal/pgconv"
	"github.com/whoaskedssselery/studyvault/backend/internal/search"
)

const (
	defaultLimit = 30
	maxLimit     = 100
)

var (
	// ErrNotFound возвращается и для чужой, и для удалённой заметки: снаружи они неотличимы.
	ErrNotFound       = errors.New("note not found")
	ErrInvalidSubject = errors.New("subject does not exist")
	// ErrInvalidLibrarySubject: общего предмета библиотеки с таким id нет.
	ErrInvalidLibrarySubject = errors.New("library subject does not exist")
	ErrInvalidCursor         = errors.New("invalid cursor")
)

type Store interface {
	SubjectOwnedByUser(ctx context.Context, arg db.SubjectOwnedByUserParams) (bool, error)
	LibrarySubjectExists(ctx context.Context, id uuid.UUID) (bool, error)
	GetLastRejectionReason(ctx context.Context, noteID uuid.UUID) (pgtype.Text, error)
	NoteOwnedByUser(ctx context.Context, arg db.NoteOwnedByUserParams) (bool, error)
	CountUserNotes(ctx context.Context, userID uuid.UUID) (int64, error)
	CreateNote(ctx context.Context, arg db.CreateNoteParams) (db.Note, error)
	GetNote(ctx context.Context, arg db.GetNoteParams) (db.Note, error)
	UpdateNote(ctx context.Context, arg db.UpdateNoteParams) (db.Note, error)
	SoftDeleteNote(ctx context.Context, arg db.SoftDeleteNoteParams) (int64, error)
	ListNotes(ctx context.Context, arg db.ListNotesParams) ([]db.ListNotesRow, error)
	ListNoteTags(ctx context.Context, noteIds []uuid.UUID) ([]db.ListNoteTagsRow, error)
	ReplaceNoteMentions(ctx context.Context, arg db.ReplaceNoteMentionsParams) error
	ReplaceOutgoingLinks(ctx context.Context, arg db.ReplaceOutgoingLinksParams) error
	ReplaceIncomingLinks(ctx context.Context, arg db.ReplaceIncomingLinksParams) error
	ListBacklinks(ctx context.Context, arg db.ListBacklinksParams) ([]db.ListBacklinksRow, error)
	ListGraphNodes(ctx context.Context, userID uuid.UUID) ([]db.ListGraphNodesRow, error)
	ListGraphEdges(ctx context.Context, userID uuid.UUID) ([]db.NoteLink, error)
}

// TxFunc выполняет fn в одной транзакции над хранилищем. В продакшене это db.InTx.
type TxFunc func(ctx context.Context, fn func(Store) error) error

// Indexer ведёт поисковый индекс. Это лишь ускоритель, так что его сбои заметки не ломают.
type Indexer interface {
	Upsert(ctx context.Context, doc search.Doc) error
	Remove(ctx context.Context, id uuid.UUID) error
}

type Option func(*Service)

// WithIndexer включает синхронизацию поискового индекса после каждой записи.
func WithIndexer(idx Indexer) Option { return func(s *Service) { s.idx = idx } }

// WithTx включает транзакции: заметка и её связи записываются атомарно.
func WithTx(run TxFunc) Option { return func(s *Service) { s.inTx = run } }

type Tag struct {
	ID   uuid.UUID
	Name string
}

type Summary struct {
	ID               uuid.UUID
	SubjectID        *uuid.UUID
	Title            string
	IsPinned         bool
	Kind             string // note или cheat_sheet
	Status           string // draft, pending, published или rejected
	LibrarySubjectID *uuid.UUID
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Tags             []Tag
}

type Note struct {
	Summary
	Content         []byte
	Description     *string // краткое описание материала (поле summary в API; имя Summary занято)
	Semester        *int16
	PublishedAt     *time.Time
	Views           int64
	RejectionReason *string // причина последнего отказа, пока заметка в статусе rejected
}

type Input struct {
	Title            string
	Content          []byte
	SubjectID        *uuid.UUID
	IsPinned         bool
	Kind             string // пусто: note
	Description      *string
	Semester         *int16
	LibrarySubjectID *uuid.UUID
}

// Patch описывает частичное изменение. Content == nil значит «не менять».
// subject_id можно очистить, поэтому «передан ли он» хранится отдельным флагом SetSubject.
type Patch struct {
	Title      *string
	Content    []byte
	SetSubject bool
	SubjectID  *uuid.UUID
	IsPinned   *bool

	// Поля материала. Правка любого из них, как и заголовка или текста, возвращает опубликованный
	// материал на проверку.
	Kind              *string
	SetDescription    bool
	Description       *string
	SetSemester       bool
	Semester          *int16
	SetLibrarySubject bool
	LibrarySubjectID  *uuid.UUID
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
	idx   Indexer // может быть nil: тогда индекс не ведётся
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
	if err := s.checkLibrarySubject(ctx, in.LibrarySubjectID); err != nil {
		return Note{}, err
	}
	// Квота мягкая, как и остальные (см. пакет limits): при одновременных запросах предел можно превысить на их число.
	if n, err := s.store.CountUserNotes(ctx, userID); err != nil {
		return Note{}, fmt.Errorf("count notes: %w", err)
	} else if n >= limits.MaxNotesPerUser {
		return Note{}, limits.Reached("notes", limits.MaxNotesPerUser)
	}
	kind := in.Kind
	if kind == "" {
		kind = KindNote
	}
	var created db.Note
	err := s.inTx(ctx, func(st Store) error {
		n, err := st.CreateNote(ctx, db.CreateNoteParams{
			UserID:           userID,
			SubjectID:        pgconv.ToNullUUID(in.SubjectID),
			Title:            in.Title,
			Content:          in.Content,
			ContentText:      ExtractText(in.Content),
			IsPinned:         in.IsPinned,
			Kind:             kind,
			Summary:          pgconv.ToText(in.Description),
			Semester:         pgconv.ToInt2(in.Semester),
			LibrarySubjectID: pgconv.ToNullUUID(in.LibrarySubjectID),
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
	s.index(ctx, searchDoc(userID, created))
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
	note, err := s.withTags(ctx, n)
	if err != nil {
		return Note{}, err
	}
	if n.Status == StatusRejected {
		reason, err := s.store.GetLastRejectionReason(ctx, n.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Note{}, fmt.Errorf("get rejection reason: %w", err)
		}
		if err == nil && reason.Valid {
			note.RejectionReason = &reason.String
		}
	}
	return note, nil
}

func (s *Service) Update(ctx context.Context, userID, id uuid.UUID, p Patch) (Note, error) {
	if p.SetSubject {
		if err := s.checkSubject(ctx, userID, p.SubjectID); err != nil {
			return Note{}, err
		}
	}
	if p.SetLibrarySubject {
		if err := s.checkLibrarySubject(ctx, p.LibrarySubjectID); err != nil {
			return Note{}, err
		}
	}
	arg := db.UpdateNoteParams{
		ID:                id,
		UserID:            userID,
		SetSubject:        p.SetSubject,
		SubjectID:         pgconv.ToNullUUID(p.SubjectID),
		Content:           p.Content,
		SetSummary:        p.SetDescription,
		Summary:           pgconv.ToText(p.Description),
		SetSemester:       p.SetSemester,
		Semester:          pgconv.ToInt2(p.Semester),
		SetLibrarySubject: p.SetLibrarySubject,
		LibrarySubjectID:  pgconv.ToNullUUID(p.LibrarySubjectID),
	}
	if p.Kind != nil {
		arg.Kind = pgtype.Text{String: *p.Kind, Valid: true}
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
	// Индекс хранит и статус: правка опубликованного материала снимает его с публикации, и поиск по библиотеке
	// должен перестать его находить. Поэтому документ обновляется при любой правке самого материала.
	if p.Title != nil || p.Content != nil || p.Kind != nil || p.SetDescription || p.SetSemester || p.SetLibrarySubject {
		s.index(ctx, searchDoc(userID, updated))
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
	s.unindex(ctx, id)
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
		SubjectID: pgconv.ToNullUUID(f.SubjectID),
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

// checkLibrarySubject проверяет, что общий предмет библиотеки существует.
func (s *Service) checkLibrarySubject(ctx context.Context, id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	ok, err := s.store.LibrarySubjectExists(ctx, *id)
	if err != nil {
		return fmt.Errorf("check library subject: %w", err)
	}
	if !ok {
		return ErrInvalidLibrarySubject
	}
	return nil
}

// Типы материала и статусы публикации. Строки совпадают со значениями CHECK-ограничений в БД.
const (
	KindNote       = "note"
	KindCheatSheet = "cheat_sheet"

	StatusDraft     = "draft"
	StatusPending   = "pending"
	StatusPublished = "published"
	StatusRejected  = "rejected"
)

func searchDoc(userID uuid.UUID, n db.Note) search.Doc {
	return search.Doc{ID: n.ID, UserID: userID, Title: n.Title, ContentText: n.ContentText, Status: n.Status}
}

const indexTimeout = 5 * time.Second

// index и unindex вызываются после успешной записи в БД. Сбой индекса только логируется:
// источник правды — Postgres, а индекс чинится повторным сохранением или командой reindex.
// Контекст отвязан от запроса, чтобы обрыв соединения клиентом не оставил индекс отставшим.
func (s *Service) index(ctx context.Context, doc search.Doc) {
	if s.idx == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), indexTimeout)
	defer cancel()
	if err := s.idx.Upsert(ctx, doc); err != nil {
		slog.Error("search index: upsert failed", "note", doc.ID, "err", err)
	}
}

func (s *Service) unindex(ctx context.Context, id uuid.UUID) {
	if s.idx == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), indexTimeout)
	defer cancel()
	if err := s.idx.Remove(ctx, id); err != nil {
		slog.Error("search index: remove failed", "note", id, "err", err)
	}
}

func replaceOutgoing(ctx context.Context, st Store, userID, noteID uuid.UUID, content []byte) error {
	titles := ParseLinks(content)
	if titles == nil {
		titles = []string{} // пустой массив, а не NULL: иначе в SQL не отработает снятие связей
	}
	// Сначала упоминания: по ним другие заметки найдут эту, когда появятся.
	if err := st.ReplaceNoteMentions(ctx, db.ReplaceNoteMentionsParams{NoteID: noteID, Titles: titles}); err != nil {
		return fmt.Errorf("replace note mentions: %w", err)
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
			ID:               r.ID,
			SubjectID:        pgconv.FromNullUUID(r.SubjectID),
			Title:            r.Title,
			IsPinned:         r.IsPinned,
			Kind:             r.Kind,
			Status:           r.Status,
			LibrarySubjectID: pgconv.FromNullUUID(r.LibrarySubjectID),
			CreatedAt:        r.CreatedAt,
			UpdatedAt:        r.UpdatedAt,
			Tags:             tags[r.ID],
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
			ID:               n.ID,
			SubjectID:        pgconv.FromNullUUID(n.SubjectID),
			Title:            n.Title,
			IsPinned:         n.IsPinned,
			Kind:             n.Kind,
			Status:           n.Status,
			LibrarySubjectID: pgconv.FromNullUUID(n.LibrarySubjectID),
			CreatedAt:        n.CreatedAt,
			UpdatedAt:        n.UpdatedAt,
			Tags:             tags,
		},
		Content:     n.Content,
		Description: pgconv.FromText(n.Summary),
		Semester:    pgconv.FromInt2(n.Semester),
		PublishedAt: n.PublishedAt,
		Views:       n.Views,
	}
}
