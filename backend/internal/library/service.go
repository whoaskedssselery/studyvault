// Package library — общая библиотека опубликованных материалов: лента и поиск, страница материала,
// комментарии, оценки, избранное, публикация и модерация.
//
// Личные заметки пользователя видит только он сам (это обеспечивают запросы пакета note). Здесь же всё, что
// видно другим, а граница проходит по статусу: гость и студент видят только опубликованное, автор видит
// ещё и свои черновики, материалы на проверке и отклонённые, модератор видит очередь проверки.
package library

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/search"
)

const (
	defaultLimit = 20
	maxLimit     = 100

	// Сколько кандидатов просить у поискового движка на каждый запрошенный результат: часть отсеет проверка в БД.
	candidateFactor = 4
	maxCandidates   = 200

	indexTimeout = 5 * time.Second
)

var (
	// ErrNotFound — материала, комментария или предмета нет, либо он невидим этому пользователю (снаружи это одно и то же).
	ErrNotFound = errors.New("not found")
	// ErrForbidden — действие не по роли: не модератор, свой материал, и тому подобное.
	ErrForbidden = errors.New("forbidden")
	// ErrInvalidTransition — материал не в том статусе, из которого допустим этот переход.
	ErrInvalidTransition = errors.New("invalid status transition")
	ErrInvalidCursor     = errors.New("invalid cursor")
	// ErrInvalid — входные данные не прошли проверку; текст ошибки объясняет, что именно не так.
	ErrInvalid = errors.New("invalid input")
	// ErrSubjectExists — общий предмет с таким названием уже есть.
	ErrSubjectExists = errors.New("library subject already exists")
	// ErrSubjectInUse — к предмету привязаны материалы, удалять его нельзя.
	ErrSubjectInUse = errors.New("library subject is in use")
)

const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

type Store interface {
	GetUserByID(ctx context.Context, id uuid.UUID) (db.User, error)

	ListLibrarySubjects(ctx context.Context) ([]db.ListLibrarySubjectsRow, error)
	CreateLibrarySubject(ctx context.Context, arg db.CreateLibrarySubjectParams) (db.LibrarySubject, error)
	UpdateLibrarySubject(ctx context.Context, arg db.UpdateLibrarySubjectParams) (db.LibrarySubject, error)
	DeleteLibrarySubject(ctx context.Context, id uuid.UUID) (int64, error)

	ListLibraryNewest(ctx context.Context, arg db.ListLibraryNewestParams) ([]db.LibraryItem, error)
	ListLibraryTopRated(ctx context.Context, arg db.ListLibraryTopRatedParams) ([]db.LibraryItem, error)
	ListLibraryMostViewed(ctx context.Context, arg db.ListLibraryMostViewedParams) ([]db.LibraryItem, error)
	ListLibraryByIDs(ctx context.Context, arg db.ListLibraryByIDsParams) ([]db.LibraryItem, error)
	ListViewerState(ctx context.Context, arg db.ListViewerStateParams) ([]db.ListViewerStateRow, error)

	GetMaterial(ctx context.Context, arg db.GetMaterialParams) (db.GetMaterialRow, error)
	IncrementViews(ctx context.Context, id uuid.UUID) error
	GetPublishedAuthor(ctx context.Context, id uuid.UUID) (uuid.UUID, error)
	ListNoteTags(ctx context.Context, noteIds []uuid.UUID) ([]db.ListNoteTagsRow, error)
	ListNoteAttachmentsAny(ctx context.Context, noteID uuid.NullUUID) ([]db.Attachment, error)
	GetNoteAttachment(ctx context.Context, arg db.GetNoteAttachmentParams) (db.Attachment, error)
	GetLastRejectionReason(ctx context.Context, noteID uuid.UUID) (pgtype.Text, error)

	CreateComment(ctx context.Context, arg db.CreateCommentParams) (db.Comment, error)
	ListComments(ctx context.Context, arg db.ListCommentsParams) ([]db.ListCommentsRow, error)
	SoftDeleteComment(ctx context.Context, arg db.SoftDeleteCommentParams) (int64, error)

	UpsertRating(ctx context.Context, arg db.UpsertRatingParams) error
	GetRatingSummary(ctx context.Context, noteID uuid.UUID) (db.GetRatingSummaryRow, error)

	AddBookmark(ctx context.Context, arg db.AddBookmarkParams) error
	RemoveBookmark(ctx context.Context, arg db.RemoveBookmarkParams) error
	ListBookmarkedLibrary(ctx context.Context, arg db.ListBookmarkedLibraryParams) ([]db.ListBookmarkedLibraryRow, error)

	GetNote(ctx context.Context, arg db.GetNoteParams) (db.Note, error)
	SubmitNote(ctx context.Context, arg db.SubmitNoteParams) (db.Note, error)
	GetNoteStatus(ctx context.Context, id uuid.UUID) (db.GetNoteStatusRow, error)
	ApproveNote(ctx context.Context, id uuid.UUID) (db.Note, error)
	RejectNote(ctx context.Context, id uuid.UUID) (db.Note, error)
	InsertModerationDecision(ctx context.Context, arg db.InsertModerationDecisionParams) error
	ListPendingMaterials(ctx context.Context, arg db.ListPendingMaterialsParams) ([]db.ListPendingMaterialsRow, error)
}

// TxFunc выполняет fn в одной транзакции над хранилищем (в продакшене это db.InTx).
type TxFunc func(ctx context.Context, fn func(Store) error) error

// Indexer ведёт поисковый индекс; это лишь ускоритель, его сбои на материалы не влияют.
type Indexer interface {
	Upsert(ctx context.Context, doc search.Doc) error
}

// Engine ищет кандидатов среди опубликованных материалов; решает всё равно база.
type Engine interface {
	SearchPublished(ctx context.Context, query string, limit int) ([]search.Hit, error)
}

type Option func(*Service)

func WithTx(run TxFunc) Option              { return func(s *Service) { s.inTx = run } }
func WithIndexer(idx Indexer) Option        { return func(s *Service) { s.idx = idx } }
func WithEngine(e Engine) Option            { return func(s *Service) { s.engine = e } }
func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

type Service struct {
	store  Store
	inTx   TxFunc
	idx    Indexer
	engine Engine
	now    func() time.Time
}

func NewService(store Store, opts ...Option) *Service {
	s := &Service{store: store, now: time.Now}
	s.inTx = func(_ context.Context, fn func(Store) error) error { return fn(s.store) }
	for _, o := range opts {
		o(s)
	}
	return s
}

// ---------------------------------------------------------------------------
// Типы результата
// ---------------------------------------------------------------------------

type SubjectRef struct {
	ID    uuid.UUID
	Title string
	Color *string
}

type Author struct {
	ID          uuid.UUID
	DisplayName string
	Faculty     *string
}

// Item — карточка материала в списке.
type Item struct {
	ID             uuid.UUID
	Title          string
	Summary        *string
	Kind           string
	Semester       *int16
	Subject        *SubjectRef
	Author         Author
	RatingAverage  *float64 // nil, пока нет ни одной оценки
	RatingCount    int64
	Views          int64
	CommentsCount  int64
	BookmarksCount int64
	PublishedAt    *time.Time
	MyRating       *int16 // оценка зрителя; nil у гостя
	Bookmarked     bool
	Snippet        string // только в результатах поиска: HTML с <mark>
}

type Page struct {
	Items      []Item
	NextCursor string // пусто: страница последняя
}

// Material — страница материала.
type Material struct {
	Item
	Status          string
	RejectionReason *string
	Content         []byte
	Tags            []string
	Attachments     []db.Attachment
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Sort — порядок ленты.
type Sort string

const (
	SortNew   Sort = "new"
	SortRated Sort = "rating"
	SortViews Sort = "views"
)

type ListFilter struct {
	Query     string
	SubjectID *uuid.UUID
	Kind      *string
	Semester  *int16
	Sort      Sort
	Limit     int
	Cursor    string
}

// ---------------------------------------------------------------------------
// Лента и поиск
// ---------------------------------------------------------------------------

// List отдаёт опубликованные материалы. uid — зритель: nil для гостя. Зритель нужен лишь для личных
// полей карточки (моя оценка, избранное), права на список от него не зависят.
func (s *Service) List(ctx context.Context, uid *uuid.UUID, f ListFilter) (Page, error) {
	limit := clampLimit(f.Limit)

	var (
		items []db.LibraryItem
		next  string
		snips map[uuid.UUID]string
		err   error
	)
	if f.Query != "" {
		items, snips, err = s.searchItems(ctx, f, limit)
	} else {
		items, next, err = s.browse(ctx, f, limit)
	}
	if err != nil {
		return Page{}, err
	}

	out := make([]Item, len(items))
	for i, li := range items {
		out[i] = itemFromView(li)
		out[i].Snippet = snips[li.ID]
	}
	if err := s.attachViewerState(ctx, uid, out); err != nil {
		return Page{}, err
	}
	return Page{Items: out, NextCursor: next}, nil
}

func (s *Service) browse(ctx context.Context, f ListFilter, limit int) ([]db.LibraryItem, string, error) {
	subject, kind, semester := toNullUUID(f.SubjectID), toText(f.Kind), toInt2(f.Semester)
	pageSize := int32(limit + 1) // лишняя строка нужна, чтобы узнать, есть ли следующая страница

	var rows []db.LibraryItem
	var err error
	switch f.Sort {
	case SortRated:
		arg := db.ListLibraryTopRatedParams{LibrarySubjectID: subject, Kind: kind, Semester: semester, PageSize: pageSize}
		if f.Cursor != "" {
			var c ratedCursor
			if err := decodeCursor(f.Cursor, &c); err != nil {
				return nil, "", err
			}
			arg.CursorRating = pgtype.Float8{Float64: c.Rating, Valid: true}
			arg.CursorCount = pgtype.Int8{Int64: c.Count, Valid: true}
			arg.CursorID = uuid.NullUUID{UUID: c.ID, Valid: true}
		}
		rows, err = s.store.ListLibraryTopRated(ctx, arg)
	case SortViews:
		arg := db.ListLibraryMostViewedParams{LibrarySubjectID: subject, Kind: kind, Semester: semester, PageSize: pageSize}
		if f.Cursor != "" {
			var c viewsCursor
			if err := decodeCursor(f.Cursor, &c); err != nil {
				return nil, "", err
			}
			arg.CursorViews = pgtype.Int8{Int64: c.Views, Valid: true}
			arg.CursorID = uuid.NullUUID{UUID: c.ID, Valid: true}
		}
		rows, err = s.store.ListLibraryMostViewed(ctx, arg)
	default:
		arg := db.ListLibraryNewestParams{LibrarySubjectID: subject, Kind: kind, Semester: semester, PageSize: pageSize}
		if f.Cursor != "" {
			var c timeCursor
			if err := decodeCursor(f.Cursor, &c); err != nil {
				return nil, "", err
			}
			arg.CursorPublishedAt = &c.At
			arg.CursorID = uuid.NullUUID{UUID: c.ID, Valid: true}
		}
		rows, err = s.store.ListLibraryNewest(ctx, arg)
	}
	if err != nil {
		return nil, "", fmt.Errorf("list library: %w", err)
	}

	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		switch f.Sort {
		case SortRated:
			next = encodeCursor(ratedCursor{Rating: last.RatingAvg, Count: last.RatingCount, ID: last.ID})
		case SortViews:
			next = encodeCursor(viewsCursor{Views: last.Views, ID: last.ID})
		default:
			next = encodeCursor(timeCursor{At: *last.PublishedAt, ID: last.ID})
		}
	}
	return rows, next, nil
}

// searchItems берёт у движка кандидатов в порядке релевантности и перепроверяет их в Postgres: чужие черновики,
// снятые с публикации и удалённые материалы отсекает база, даже если индекс отстал.
func (s *Service) searchItems(ctx context.Context, f ListFilter, limit int) ([]db.LibraryItem, map[uuid.UUID]string, error) {
	if s.engine == nil {
		return nil, nil, search.ErrUnavailable
	}
	hits, err := s.engine.SearchPublished(ctx, f.Query, min(limit*candidateFactor, maxCandidates))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", search.ErrUnavailable, err)
	}
	if len(hits) == 0 {
		return nil, nil, nil
	}
	ids := make([]uuid.UUID, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	rows, err := s.store.ListLibraryByIDs(ctx, db.ListLibraryByIDsParams{
		Ids: ids, LibrarySubjectID: toNullUUID(f.SubjectID), Kind: toText(f.Kind), Semester: toInt2(f.Semester),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("check search results: %w", err)
	}
	byID := make(map[uuid.UUID]db.LibraryItem, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	items := make([]db.LibraryItem, 0, min(limit, len(rows)))
	snips := make(map[uuid.UUID]string, len(rows))
	for _, h := range hits {
		r, ok := byID[h.ID]
		if !ok {
			continue
		}
		items = append(items, r)
		snips[r.ID] = search.RenderSnippet(h.Snippet)
		if len(items) == limit {
			break
		}
	}
	return items, snips, nil
}

// attachViewerState дополняет карточки личным состоянием зрителя: его оценкой и отметкой «в избранном».
func (s *Service) attachViewerState(ctx context.Context, uid *uuid.UUID, items []Item) error {
	if uid == nil || len(items) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(items))
	for i, it := range items {
		ids[i] = it.ID
	}
	rows, err := s.store.ListViewerState(ctx, db.ListViewerStateParams{ViewerID: *uid, NoteIds: ids})
	if err != nil {
		return fmt.Errorf("list viewer state: %w", err)
	}
	state := make(map[uuid.UUID]db.ListViewerStateRow, len(rows))
	for _, r := range rows {
		state[r.NoteID] = r
	}
	for i := range items {
		st := state[items[i].ID]
		if st.MyRating.Valid {
			v := st.MyRating.Int16
			items[i].MyRating = &v
		}
		items[i].Bookmarked = st.Bookmarked
	}
	return nil
}

// ---------------------------------------------------------------------------
// Страница материала
// ---------------------------------------------------------------------------

// Get возвращает материал, если зритель вправе его видеть. Чужой черновик, материал на проверке или отклонённый
// отвечают ErrNotFound, как несуществующий. Просмотр опубликованного материала засчитывается, кроме просмотра автором.
func (s *Service) Get(ctx context.Context, uid *uuid.UUID, id uuid.UUID) (Material, error) {
	moderator := false
	if uid != nil {
		var err error
		if moderator, err = s.isModerator(ctx, *uid); err != nil {
			return Material{}, err
		}
	}
	row, err := s.store.GetMaterial(ctx, db.GetMaterialParams{ID: id, ViewerID: toNullUUID(uid), IsModerator: moderator})
	if errors.Is(err, pgx.ErrNoRows) {
		return Material{}, ErrNotFound
	}
	if err != nil {
		return Material{}, fmt.Errorf("get material: %w", err)
	}

	m := materialFromRow(row)
	if m.Status == statusPublished && (uid == nil || *uid != row.UserID) {
		if err := s.store.IncrementViews(ctx, id); err != nil {
			slog.WarnContext(ctx, "count a view", "note", id, "err", err) // счётчик не стоит ошибки чтения
		} else {
			m.Views++
		}
	}

	tags, err := s.store.ListNoteTags(ctx, []uuid.UUID{id})
	if err != nil {
		return Material{}, fmt.Errorf("list tags: %w", err)
	}
	m.Tags = make([]string, 0, len(tags))
	for _, t := range tags {
		m.Tags = append(m.Tags, t.Name)
	}
	m.Attachments, err = s.store.ListNoteAttachmentsAny(ctx, uuid.NullUUID{UUID: id, Valid: true})
	if err != nil {
		return Material{}, fmt.Errorf("list attachments: %w", err)
	}
	if m.Status == statusRejected {
		reason, err := s.store.GetLastRejectionReason(ctx, id)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Material{}, fmt.Errorf("get rejection reason: %w", err)
		}
		if err == nil && reason.Valid {
			m.RejectionReason = &reason.String
		}
	}

	one := []Item{m.Item}
	if err := s.attachViewerState(ctx, uid, one); err != nil {
		return Material{}, err
	}
	m.Item = one[0]
	return m, nil
}

// Attachment возвращает запись о файле материала при тех же правилах видимости, что и Get. Саму ссылку на
// скачивание выдаёт сервис вложений.
func (s *Service) Attachment(ctx context.Context, uid *uuid.UUID, noteID, attachmentID uuid.UUID) (db.Attachment, error) {
	moderator := false
	if uid != nil {
		var err error
		if moderator, err = s.isModerator(ctx, *uid); err != nil {
			return db.Attachment{}, err
		}
	}
	if _, err := s.store.GetMaterial(ctx, db.GetMaterialParams{ID: noteID, ViewerID: toNullUUID(uid), IsModerator: moderator}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Attachment{}, ErrNotFound
		}
		return db.Attachment{}, fmt.Errorf("check material: %w", err)
	}
	att, err := s.store.GetNoteAttachment(ctx, db.GetNoteAttachmentParams{ID: attachmentID, NoteID: uuid.NullUUID{UUID: noteID, Valid: true}})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Attachment{}, ErrNotFound
	}
	if err != nil {
		return db.Attachment{}, fmt.Errorf("get attachment: %w", err)
	}
	return att, nil
}

// ---------------------------------------------------------------------------
// Вспомогательное
// ---------------------------------------------------------------------------

const (
	statusPublished = "published"
	statusRejected  = "rejected"
)

func (s *Service) isModerator(ctx context.Context, uid uuid.UUID) (bool, error) {
	u, err := s.store.GetUserByID(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get user: %w", err)
	}
	return u.Role == auth.RoleModerator, nil
}

// requireModerator возвращает ErrForbidden, если пользователь не модератор (или его уже нет).
func (s *Service) requireModerator(ctx context.Context, uid uuid.UUID) error {
	ok, err := s.isModerator(ctx, uid)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}

func clampLimit(n int) int {
	if n <= 0 {
		return defaultLimit
	}
	return min(n, maxLimit)
}

func itemFromView(li db.LibraryItem) Item {
	it := Item{
		ID:             li.ID,
		Title:          li.Title,
		Summary:        fromText(li.Summary),
		Kind:           li.Kind,
		Semester:       fromInt2(li.Semester),
		Author:         Author{ID: li.UserID, DisplayName: li.AuthorName, Faculty: fromText(li.AuthorFaculty)},
		RatingCount:    li.RatingCount,
		Views:          li.Views,
		CommentsCount:  li.CommentsCount,
		BookmarksCount: li.BookmarksCount,
		PublishedAt:    li.PublishedAt,
	}
	if li.RatingCount > 0 {
		avg := li.RatingAvg
		it.RatingAverage = &avg
	}
	if li.LibrarySubjectID.Valid {
		it.Subject = &SubjectRef{ID: li.LibrarySubjectID.UUID, Title: li.SubjectTitle.String, Color: fromText(li.SubjectColor)}
	}
	return it
}

func itemFromBookmark(r db.ListBookmarkedLibraryRow) Item {
	return itemFromView(db.LibraryItem{
		ID: r.ID, UserID: r.UserID, Title: r.Title, Summary: r.Summary, Kind: r.Kind, Semester: r.Semester,
		PublishedAt: r.PublishedAt, Views: r.Views, LibrarySubjectID: r.LibrarySubjectID,
		SubjectTitle: r.SubjectTitle, SubjectColor: r.SubjectColor, AuthorName: r.AuthorName, AuthorFaculty: r.AuthorFaculty,
		RatingCount: r.RatingCount, RatingAvg: r.RatingAvg, CommentsCount: r.CommentsCount, BookmarksCount: r.BookmarksCount,
	})
}

func materialFromRow(r db.GetMaterialRow) Material {
	li := db.LibraryItem{
		ID: r.ID, UserID: r.UserID, Title: r.Title, Summary: r.Summary, Kind: r.Kind, Semester: r.Semester,
		PublishedAt: r.PublishedAt, Views: r.Views, LibrarySubjectID: r.LibrarySubjectID,
		SubjectTitle: r.SubjectTitle, SubjectColor: r.SubjectColor, AuthorName: r.AuthorName, AuthorFaculty: r.AuthorFaculty,
		RatingCount: r.RatingCount, RatingAvg: r.RatingAvg, CommentsCount: r.CommentsCount, BookmarksCount: r.BookmarksCount,
	}
	return Material{
		Item:      itemFromView(li),
		Status:    r.Status,
		Content:   r.Content,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

// index обновляет документ в поисковом индексе после записи в БД. Сбой лишь логируется: источник правды Postgres,
// а индекс чинится повторной записью или командой reindex. Контекст отвязан от запроса, чтобы обрыв соединения
// клиентом не оставил индекс отставшим.
func (s *Service) index(ctx context.Context, n db.Note) {
	if s.idx == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), indexTimeout)
	defer cancel()
	doc := search.Doc{ID: n.ID, UserID: n.UserID, Title: n.Title, ContentText: n.ContentText, Status: n.Status}
	if err := s.idx.Upsert(ctx, doc); err != nil {
		slog.Error("search index: upsert failed", "note", n.ID, "err", err)
	}
}

// Курсоры непрозрачны для клиента: это JSON в base64. Содержимое курсора не секрет, но и не контракт.
type timeCursor struct {
	At time.Time `json:"t"`
	ID uuid.UUID `json:"id"`
}

type ratedCursor struct {
	Rating float64   `json:"r"`
	Count  int64     `json:"c"`
	ID     uuid.UUID `json:"id"`
}

type viewsCursor struct {
	Views int64     `json:"v"`
	ID    uuid.UUID `json:"id"`
}

func encodeCursor(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string, dst any) error {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return ErrInvalidCursor
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return ErrInvalidCursor
	}
	// Нулевой id значит «курсор пустой или чужой формы».
	switch c := dst.(type) {
	case *timeCursor:
		if c.At.IsZero() || c.ID == uuid.Nil {
			return ErrInvalidCursor
		}
	case *ratedCursor:
		if c.ID == uuid.Nil {
			return ErrInvalidCursor
		}
	case *viewsCursor:
		if c.ID == uuid.Nil {
			return ErrInvalidCursor
		}
	}
	return nil
}

func toNullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}

func toText(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func fromText(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	v := t.String
	return &v
}

func toInt2(v *int16) pgtype.Int2 {
	if v == nil {
		return pgtype.Int2{}
	}
	return pgtype.Int2{Int16: *v, Valid: true}
}

func fromInt2(v pgtype.Int2) *int16 {
	if !v.Valid {
		return nil
	}
	x := v.Int16
	return &x
}
