package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/whoaskedssselery/studyvault/backend/internal/attachment"
	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/library"
	"github.com/whoaskedssselery/studyvault/backend/internal/search"
	"github.com/whoaskedssselery/studyvault/backend/internal/storage"
)

const (
	maxSummaryLen = 500
	maxQueryLen   = 200
)

// LibraryService is satisfied by *library.Service.
type LibraryService interface {
	List(ctx context.Context, uid *uuid.UUID, f library.ListFilter) (library.Page, error)
	Get(ctx context.Context, uid *uuid.UUID, id uuid.UUID) (library.Material, error)
	Attachment(ctx context.Context, uid *uuid.UUID, noteID, attachmentID uuid.UUID) (db.Attachment, error)

	ListSubjects(ctx context.Context) ([]library.Subject, error)
	CreateSubject(ctx context.Context, uid uuid.UUID, title string, color *string) (library.Subject, error)
	UpdateSubject(ctx context.Context, uid, id uuid.UUID, p library.SubjectPatch) (library.Subject, error)
	DeleteSubject(ctx context.Context, uid, id uuid.UUID) error

	ListComments(ctx context.Context, noteID uuid.UUID, limit int, cursor string) (library.CommentPage, error)
	AddComment(ctx context.Context, uid, noteID uuid.UUID, body string) (library.Comment, error)
	DeleteComment(ctx context.Context, uid, id uuid.UUID) error

	Rate(ctx context.Context, uid, noteID uuid.UUID, value int) (library.RatingResult, error)
	AddBookmark(ctx context.Context, uid, noteID uuid.UUID) error
	RemoveBookmark(ctx context.Context, uid, noteID uuid.UUID) error
	Bookmarks(ctx context.Context, uid uuid.UUID, limit int, cursor string) (library.Page, error)

	Submit(ctx context.Context, uid, id uuid.UUID) (db.Note, error)
	Decide(ctx context.Context, moderatorID, id uuid.UUID, decision, reason string) (db.Note, error)
	Queue(ctx context.Context, moderatorID uuid.UUID, limit int, cursor string) (library.QueuePage, error)
	Withdraw(ctx context.Context, uid, id uuid.UUID) (db.Note, error)
	Unpublish(ctx context.Context, moderatorID, id uuid.UUID, reason string) (db.Note, error)
	EditComment(ctx context.Context, uid, id uuid.UUID, body string) (library.Comment, error)
	Ranking(ctx context.Context, limit int) ([]library.RankedAuthor, error)
	AuthorProfile(ctx context.Context, id uuid.UUID) (library.AuthorStats, error)
}

// viewer возвращает id зрителя или nil для гостя. Токен, если он есть, проверил authMiddleware.
func viewer(r *http.Request) *uuid.UUID {
	id, ok := auth.UserID(r.Context())
	if !ok {
		return nil
	}
	return &id
}

// libraryError переводит ошибки сервиса библиотеки в ответы API. Всё незнакомое считается внутренней ошибкой.
func libraryError(w http.ResponseWriter, r *http.Request, err error, op string) {
	switch {
	case errors.Is(err, library.ErrNotFound):
		notFound(w, "not found")
	case errors.Is(err, library.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "you are not allowed to do this")
	case errors.Is(err, library.ErrInvalidTransition):
		writeError(w, http.StatusConflict, "invalid_transition", trimInvalid(err, library.ErrInvalidTransition))
	case errors.Is(err, library.ErrInvalid):
		badRequest(w, trimInvalid(err, library.ErrInvalid))
	case errors.Is(err, library.ErrInvalidCursor):
		badRequest(w, "cursor is not valid")
	case errors.Is(err, library.ErrSubjectExists):
		writeError(w, http.StatusConflict, "subject_exists", "a library subject with this title already exists")
	case errors.Is(err, library.ErrSubjectInUse):
		writeError(w, http.StatusConflict, "subject_in_use", "materials are attached to this subject")
	case errors.Is(err, search.ErrUnavailable):
		slog.WarnContext(r.Context(), op+": search is unavailable", "err", err)
		searchUnavailable(w)
	default:
		slog.ErrorContext(r.Context(), op, "err", err)
		internalError(w)
	}
}

// trimInvalid убирает служебный префикс sentinel-ошибки: клиенту нужен только объясняющий текст.
func trimInvalid(err, sentinel error) string {
	return strings.TrimPrefix(err.Error(), sentinel.Error()+": ")
}

// ---------------------------------------------------------------------------
// Лента, страница материала, файлы
// ---------------------------------------------------------------------------

func (a *api) ListLibrary(w http.ResponseWriter, r *http.Request, params ListLibraryParams) {
	f := library.ListFilter{SubjectID: params.LibrarySubjectId, AuthorID: params.AuthorId}
	if params.Tag != nil {
		tag := strings.TrimSpace(*params.Tag)
		if n := utf8.RuneCountInString(tag); n < 1 || n > maxTagNameLen {
			badRequest(w, "tag must be 1 to 50 characters")
			return
		}
		f.Tag = &tag
	}
	if params.Q != nil {
		q := strings.TrimSpace(*params.Q)
		if n := utf8.RuneCountInString(q); n < 1 || n > maxQueryLen {
			badRequest(w, "q must be 1 to 200 characters")
			return
		}
		f.Query = q
	}
	if params.Kind != nil {
		if !validKind(string(*params.Kind)) {
			badRequest(w, "kind must be note or cheat_sheet")
			return
		}
		k := string(*params.Kind)
		f.Kind = &k
	}
	if params.Semester != nil {
		if *params.Semester < minSemester || *params.Semester > maxSemester {
			badRequest(w, "semester must be between 1 and 12")
			return
		}
		sem := int16(*params.Semester)
		f.Semester = &sem
	}
	if params.Sort != nil {
		switch *params.Sort {
		case New, Rating, Views:
			f.Sort = library.Sort(*params.Sort)
		default:
			badRequest(w, "sort must be new, rating or views")
			return
		}
	}
	if params.Limit != nil {
		if *params.Limit < minPageLimit || *params.Limit > maxPageLimit {
			badRequest(w, "limit must be between 1 and 100")
			return
		}
		f.Limit = *params.Limit
	}
	if params.Cursor != nil {
		f.Cursor = *params.Cursor
	}

	page, err := a.Library.List(r.Context(), viewer(r), f)
	if err != nil {
		libraryError(w, r, err, "list library")
		return
	}
	writeJSON(w, http.StatusOK, toLibraryPage(page))
}

func (a *api) GetMaterial(w http.ResponseWriter, r *http.Request, id Id) {
	m, err := a.Library.Get(r.Context(), viewer(r), id)
	if err != nil {
		libraryError(w, r, err, "get material")
		return
	}
	body, err := toMaterial(m)
	if err != nil {
		slog.ErrorContext(r.Context(), "get material: stored document is not valid JSON", "err", err)
		internalError(w)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

func (a *api) GetMaterialAttachment(w http.ResponseWriter, r *http.Request, id Id, attachmentId openapi_types.UUID) {
	if a.Attachments == nil {
		storageUnavailable(w)
		return
	}
	row, err := a.Library.Attachment(r.Context(), viewer(r), id, attachmentId)
	if err != nil {
		libraryError(w, r, err, "get material attachment")
		return
	}
	dl, err := a.Attachments.Link(r.Context(), row)
	switch {
	case errors.Is(err, attachment.ErrNotUploaded):
		writeError(w, http.StatusConflict, "not_uploaded", "the file has not been uploaded yet")
	case errors.Is(err, storage.ErrUnavailable):
		slog.WarnContext(r.Context(), "get material attachment: storage is unavailable", "err", err)
		storageUnavailable(w)
	case err != nil:
		slog.ErrorContext(r.Context(), "get material attachment", "err", err)
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, AttachmentDownload{Attachment: toAttachment(dl.Attachment), DownloadUrl: dl.URL})
	}
}

// ---------------------------------------------------------------------------
// Разбор входных данных и сборка ответов (общие для всех файлов library_*.go)
// ---------------------------------------------------------------------------

func validKind(k string) bool { return k == "note" || k == "cheat_sheet" }

type materialFields struct {
	kind           string
	description    *string
	semester       *int16
	librarySubject *uuid.UUID
}

// materialMeta проверяет поля материала из тела создания или правки заметки и раскладывает их для сервиса.
// Непустое второе значение — текст ошибки 400.
func materialMeta(kind *MaterialKind, summary nullable.Nullable[string], semester nullable.Nullable[int], subject nullable.Nullable[uuid.UUID]) (materialFields, string) {
	var f materialFields
	if kind != nil {
		if !validKind(string(*kind)) {
			return f, "kind must be note or cheat_sheet"
		}
		f.kind = string(*kind)
	}
	if _, v := optional(summary); v != nil {
		text := strings.TrimSpace(*v)
		if utf8.RuneCountInString(text) > maxSummaryLen {
			return f, "summary must be at most 500 characters"
		}
		if text != "" {
			f.description = &text
		}
	}
	if _, v := optional(semester); v != nil {
		if *v < minSemester || *v > maxSemester {
			return f, "semester must be between 1 and 12"
		}
		s := int16(*v)
		f.semester = &s
	}
	_, f.librarySubject = optional(subject)
	return f, ""
}

func pagingParams(w http.ResponseWriter, limit *Limit, cursor *Cursor) (int, string, bool) {
	l := 0
	if limit != nil {
		if *limit < minPageLimit || *limit > maxPageLimit {
			badRequest(w, "limit must be between 1 and 100")
			return 0, "", false
		}
		l = *limit
	}
	c := ""
	if cursor != nil {
		c = *cursor
	}
	return l, c, true
}

func nextCursor(c string) nullable.Nullable[string] {
	if c == "" {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(c)
}

// nullableOf, nullableInt и nullableTime задают null явно: незаданный Nullable сериализовался бы как нулевое значение.
func nullableOf[T any](p *T) nullable.Nullable[T] {
	if p == nil {
		return nullable.NewNullNullable[T]()
	}
	return nullable.NewNullableWithValue(*p)
}

func nullableInt(p *int16) nullable.Nullable[int] {
	if p == nil {
		return nullable.NewNullNullable[int]()
	}
	return nullable.NewNullableWithValue(int(*p))
}

func nullableTime(p *time.Time) nullable.Nullable[time.Time] { return nullableOf(p) }

func nullableSubjectRef(s *library.SubjectRef) nullable.Nullable[SubjectRef] {
	if s == nil {
		return nullable.NewNullNullable[SubjectRef]()
	}
	return nullable.NewNullableWithValue(SubjectRef{Id: s.ID, Title: s.Title, Color: nullableOf(s.Color)})
}

func toAuthor(a library.Author) Author {
	return Author{Id: a.ID, DisplayName: a.DisplayName, Faculty: nullableOf(a.Faculty)}
}

func toRating(avg *float64, count int64) RatingSummary {
	return RatingSummary{Average: nullableOf(avg), Count: count}
}

func toLibraryItem(it library.Item) LibraryItem {
	var published time.Time
	if it.PublishedAt != nil {
		published = *it.PublishedAt
	}
	snippet := nullable.NewNullNullable[string]()
	if it.Snippet != "" {
		snippet = nullable.NewNullableWithValue(it.Snippet)
	}
	return LibraryItem{
		Id:             it.ID,
		Title:          it.Title,
		Summary:        nullableOf(it.Summary),
		Kind:           MaterialKind(it.Kind),
		Semester:       nullableInt(it.Semester),
		Subject:        nullableSubjectRef(it.Subject),
		Author:         toAuthor(it.Author),
		Rating:         toRating(it.RatingAverage, it.RatingCount),
		Views:          it.Views,
		CommentsCount:  it.CommentsCount,
		BookmarksCount: it.BookmarksCount,
		PublishedAt:    published,
		MyRating:       nullableInt(it.MyRating),
		Bookmarked:     it.Bookmarked,
		Tags:           nonNil(it.Tags),
		Snippet:        snippet,
	}
}

func toLibraryPage(p library.Page) LibraryPage {
	items := make([]LibraryItem, len(p.Items))
	for i, it := range p.Items {
		items[i] = toLibraryItem(it)
	}
	return LibraryPage{Items: items, NextCursor: nextCursor(p.NextCursor)}
}

func toMaterial(m library.Material) (Material, error) {
	var doc TiptapDoc
	if err := json.Unmarshal(m.Content, &doc); err != nil {
		return Material{}, err
	}
	files := make([]Attachment, len(m.Attachments))
	for i, f := range m.Attachments {
		files[i] = toAttachment(f)
	}
	tags := m.Tags
	if tags == nil {
		tags = []string{}
	}
	return Material{
		Id:              m.ID,
		Title:           m.Title,
		Summary:         nullableOf(m.Summary),
		Kind:            MaterialKind(m.Kind),
		Semester:        nullableInt(m.Semester),
		Subject:         nullableSubjectRef(m.Subject),
		Author:          toAuthor(m.Author),
		Rating:          toRating(m.RatingAverage, m.RatingCount),
		Views:           m.Views,
		CommentsCount:   m.CommentsCount,
		BookmarksCount:  m.BookmarksCount,
		PublishedAt:     nullableTime(m.PublishedAt),
		MyRating:        nullableInt(m.MyRating),
		Bookmarked:      m.Bookmarked,
		Status:          MaterialStatus(m.Status),
		RejectionReason: nullableOf(m.RejectionReason),
		Content:         doc,
		Tags:            tags,
		Attachments:     files,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}, nil
}

// nonNil заменяет nil-срез пустым: в JSON должно быть [], а не null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
