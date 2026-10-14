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
// Общие предметы
// ---------------------------------------------------------------------------

func (a *api) ListLibrarySubjects(w http.ResponseWriter, r *http.Request) {
	subjects, err := a.Library.ListSubjects(r.Context())
	if err != nil {
		libraryError(w, r, err, "list library subjects")
		return
	}
	items := make([]LibrarySubject, len(subjects))
	for i, s := range subjects {
		items[i] = toLibrarySubject(s)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *api) CreateLibrarySubject(w http.ResponseWriter, r *http.Request) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req LibrarySubjectInput
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	_, color := optional(req.Color)
	created, err := a.Library.CreateSubject(r.Context(), uid, req.Title, color)
	if err != nil {
		libraryError(w, r, err, "create library subject")
		return
	}
	writeJSON(w, http.StatusCreated, toLibrarySubject(created))
}

func (a *api) UpdateLibrarySubject(w http.ResponseWriter, r *http.Request, id Id) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req LibrarySubjectPatch
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	if req.Title == nil && !req.Color.IsSpecified() {
		badRequest(w, "at least one of title, color is required")
		return
	}
	p := library.SubjectPatch{Title: req.Title}
	p.SetColor, p.Color = optional(req.Color)
	updated, err := a.Library.UpdateSubject(r.Context(), uid, id, p)
	if err != nil {
		libraryError(w, r, err, "update library subject")
		return
	}
	// После правки счётчик материалов не пересчитывается: он нужен только в списке.
	writeJSON(w, http.StatusOK, toLibrarySubject(updated))
}

func (a *api) DeleteLibrarySubject(w http.ResponseWriter, r *http.Request, id Id) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	if err := a.Library.DeleteSubject(r.Context(), uid, id); err != nil {
		libraryError(w, r, err, "delete library subject")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Комментарии, оценки, избранное
// ---------------------------------------------------------------------------

func (a *api) ListComments(w http.ResponseWriter, r *http.Request, id Id, params ListCommentsParams) {
	limit, cursor, ok := pagingParams(w, params.Limit, params.Cursor)
	if !ok {
		return
	}
	page, err := a.Library.ListComments(r.Context(), id, limit, cursor)
	if err != nil {
		libraryError(w, r, err, "list comments")
		return
	}
	items := make([]Comment, len(page.Items))
	for i, c := range page.Items {
		items[i] = toComment(c)
	}
	writeJSON(w, http.StatusOK, CommentPage{Items: items, NextCursor: nextCursor(page.NextCursor)})
}

func (a *api) CreateComment(w http.ResponseWriter, r *http.Request, id Id) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req CommentInput
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	c, err := a.Library.AddComment(r.Context(), uid, id, req.Body)
	if err != nil {
		libraryError(w, r, err, "create comment")
		return
	}
	writeJSON(w, http.StatusCreated, toComment(c))
}

func (a *api) DeleteComment(w http.ResponseWriter, r *http.Request, id Id) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	if err := a.Library.DeleteComment(r.Context(), uid, id); err != nil {
		libraryError(w, r, err, "delete comment")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) RateMaterial(w http.ResponseWriter, r *http.Request, id Id) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req RatingInput
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	res, err := a.Library.Rate(r.Context(), uid, id, req.Value)
	if err != nil {
		libraryError(w, r, err, "rate material")
		return
	}
	writeJSON(w, http.StatusOK, RatingResult{Rating: toRating(res.Average, res.Count), MyRating: int(res.MyRating)})
}

func (a *api) AddBookmark(w http.ResponseWriter, r *http.Request, id Id) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	if err := a.Library.AddBookmark(r.Context(), uid, id); err != nil {
		libraryError(w, r, err, "add bookmark")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) RemoveBookmark(w http.ResponseWriter, r *http.Request, id Id) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	if err := a.Library.RemoveBookmark(r.Context(), uid, id); err != nil {
		libraryError(w, r, err, "remove bookmark")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) ListBookmarks(w http.ResponseWriter, r *http.Request, params ListBookmarksParams) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	limit, cursor, ok := pagingParams(w, params.Limit, params.Cursor)
	if !ok {
		return
	}
	page, err := a.Library.Bookmarks(r.Context(), uid, limit, cursor)
	if err != nil {
		libraryError(w, r, err, "list bookmarks")
		return
	}
	writeJSON(w, http.StatusOK, toLibraryPage(page))
}

// ---------------------------------------------------------------------------
// Публикация и модерация
// ---------------------------------------------------------------------------

func (a *api) SubmitNote(w http.ResponseWriter, r *http.Request, id Id) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	if _, err := a.Library.Submit(r.Context(), uid, id); err != nil {
		libraryError(w, r, err, "submit note")
		return
	}
	// Ответ собирает обычный сервис заметок: так автор видит ровно то, что вернул бы GET /notes/{id}.
	found, err := a.Notes.Get(r.Context(), uid, id)
	a.respondNote(w, r, http.StatusOK, found, err, "submit note")
}

func (a *api) ListModerationQueue(w http.ResponseWriter, r *http.Request, params ListModerationQueueParams) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	limit, cursor, ok := pagingParams(w, params.Limit, params.Cursor)
	if !ok {
		return
	}
	page, err := a.Library.Queue(r.Context(), uid, limit, cursor)
	if err != nil {
		libraryError(w, r, err, "list moderation queue")
		return
	}
	items := make([]QueueItem, len(page.Items))
	for i, q := range page.Items {
		items[i] = QueueItem{
			Id: q.ID, Title: q.Title, Summary: nullableOf(q.Summary), Kind: MaterialKind(q.Kind),
			Semester: nullableInt(q.Semester), Subject: nullableSubjectRef(q.Subject), Author: toAuthor(q.Author),
			SubmittedAt: q.SubmittedAt, CreatedAt: q.CreatedAt,
		}
	}
	writeJSON(w, http.StatusOK, QueuePage{Items: items, NextCursor: nextCursor(page.NextCursor)})
}

func (a *api) DecideMaterial(w http.ResponseWriter, r *http.Request, id Id) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req DecisionInput
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	reason := ""
	if req.Reason != nil {
		reason = *req.Reason
	}
	decided, err := a.Library.Decide(r.Context(), uid, id, string(req.Decision), reason)
	if err != nil {
		libraryError(w, r, err, "decide material")
		return
	}
	writeJSON(w, http.StatusOK, DecisionResult{Id: decided.ID, Status: MaterialStatus(decided.Status)})
}

// ---------------------------------------------------------------------------
// Разбор входных данных и сборка ответов
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

func toLibrarySubject(s library.Subject) LibrarySubject {
	return LibrarySubject{Id: s.ID, Title: s.Title, Color: nullableOf(s.Color), MaterialsCount: s.MaterialsCount}
}

func toComment(c library.Comment) Comment {
	return Comment{Id: c.ID, Body: c.Body, Author: toAuthor(c.Author), CreatedAt: c.CreatedAt, EditedAt: nullableTime(c.EditedAt)}
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

// ---------------------------------------------------------------------------
// Отзыв, снятие с публикации, правка комментария, авторы
// ---------------------------------------------------------------------------

func (a *api) WithdrawNote(w http.ResponseWriter, r *http.Request, id Id) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	if _, err := a.Library.Withdraw(r.Context(), uid, id); err != nil {
		libraryError(w, r, err, "withdraw note")
		return
	}
	found, err := a.Notes.Get(r.Context(), uid, id)
	a.respondNote(w, r, http.StatusOK, found, err, "withdraw note")
}

func (a *api) UnpublishMaterial(w http.ResponseWriter, r *http.Request, id Id) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req UnpublishInput
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	down, err := a.Library.Unpublish(r.Context(), uid, id, req.Reason)
	if err != nil {
		libraryError(w, r, err, "unpublish material")
		return
	}
	writeJSON(w, http.StatusOK, DecisionResult{Id: down.ID, Status: MaterialStatus(down.Status)})
}

func (a *api) UpdateComment(w http.ResponseWriter, r *http.Request, id Id) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req CommentPatch
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	c, err := a.Library.EditComment(r.Context(), uid, id, req.Body)
	if err != nil {
		libraryError(w, r, err, "update comment")
		return
	}
	writeJSON(w, http.StatusOK, toComment(c))
}

func (a *api) ListAuthorRanking(w http.ResponseWriter, r *http.Request, params ListAuthorRankingParams) {
	limit := 0
	if params.Limit != nil {
		if *params.Limit < minPageLimit || *params.Limit > maxPageLimit {
			badRequest(w, "limit must be between 1 and 100")
			return
		}
		limit = *params.Limit
	}
	ranked, err := a.Library.Ranking(r.Context(), limit)
	if err != nil {
		libraryError(w, r, err, "list author ranking")
		return
	}
	items := make([]RankedAuthor, len(ranked))
	for i, ra := range ranked {
		s := toAuthorStats(ra.AuthorStats)
		items[i] = RankedAuthor{
			Author: s.Author, MaterialsCount: s.MaterialsCount, Rating: s.Rating, RatingsCount: s.RatingsCount, Views: s.Views, Rank: ra.Rank,
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *api) GetAuthor(w http.ResponseWriter, r *http.Request, id Id) {
	st, err := a.Library.AuthorProfile(r.Context(), id)
	if err != nil {
		libraryError(w, r, err, "get author")
		return
	}
	writeJSON(w, http.StatusOK, toAuthorStats(st))
}

func toAuthorStats(st library.AuthorStats) AuthorStats {
	return AuthorStats{
		Author:         toAuthor(st.Author),
		MaterialsCount: st.MaterialsCount,
		RatingsCount:   st.RatingsCount,
		Rating:         toRating(st.RatingAverage, st.RatingsCount),
		Views:          st.Views,
	}
}

// nonNil заменяет nil-срез пустым: в JSON должно быть [], а не null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
