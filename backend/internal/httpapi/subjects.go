package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oapi-codegen/nullable"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/subject"
)

const (
	maxSubjectTitleLen = 200
	minSemester        = 1
	maxSemester        = 12
)

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// SubjectService is satisfied by *subject.Service.
type SubjectService interface {
	List(ctx context.Context, userID uuid.UUID) ([]db.Subject, error)
	Create(ctx context.Context, userID uuid.UUID, in subject.Input) (db.Subject, error)
	Update(ctx context.Context, userID, id uuid.UUID, p subject.Patch) (db.Subject, error)
	Delete(ctx context.Context, userID, id uuid.UUID) error
}

func (a *api) ListSubjects(w http.ResponseWriter, r *http.Request) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	items, err := a.Subjects.List(r.Context(), userID)
	if err != nil {
		slog.Error("list subjects", "err", err)
		internalError(w)
		return
	}
	out := make([]Subject, 0, len(items))
	for _, s := range items {
		out = append(out, toSubject(s))
	}
	writeJSON(w, http.StatusOK, struct {
		Items []Subject `json:"items"`
	}{Items: out})
}

func (a *api) CreateSubject(w http.ResponseWriter, r *http.Request) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req SubjectInput
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	title, err := validSubjectTitle(req.Title)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	_, color := optional(req.Color)
	_, semester := optional(req.Semester)
	if err := validColor(color); err != nil {
		badRequest(w, err.Error())
		return
	}
	sem, err := validSemester(semester)
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	created, err := a.Subjects.Create(r.Context(), userID, subject.Input{Title: title, Color: color, Semester: sem})
	if err != nil {
		slog.Error("create subject", "err", err)
		internalError(w)
		return
	}
	writeJSON(w, http.StatusCreated, toSubject(created))
}

func (a *api) UpdateSubject(w http.ResponseWriter, r *http.Request, id Id) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req SubjectPatch
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	if req.Title == nil && !req.Color.IsSpecified() && !req.Semester.IsSpecified() {
		badRequest(w, "at least one of title, color, semester is required")
		return
	}

	patch := subject.Patch{}
	if req.Title != nil {
		title, err := validSubjectTitle(*req.Title)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		patch.Title = &title
	}
	var color *string
	var semester *int
	patch.SetColor, color = optional(req.Color)
	patch.SetSemester, semester = optional(req.Semester)
	if err := validColor(color); err != nil {
		badRequest(w, err.Error())
		return
	}
	sem, err := validSemester(semester)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	patch.Color, patch.Semester = color, sem

	updated, err := a.Subjects.Update(r.Context(), userID, id, patch)
	switch {
	case errors.Is(err, subject.ErrNotFound):
		notFound(w, "subject not found")
	case err != nil:
		slog.Error("update subject", "err", err)
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, toSubject(updated))
	}
}

func (a *api) DeleteSubject(w http.ResponseWriter, r *http.Request, id Id) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	err := a.Subjects.Delete(r.Context(), userID, id)
	switch {
	case errors.Is(err, subject.ErrNotFound):
		notFound(w, "subject not found")
	case err != nil:
		slog.Error("delete subject", "err", err)
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func validSubjectTitle(raw string) (string, error) {
	return trimmedTitle(raw, maxSubjectTitleLen)
}

func validColor(color *string) error {
	if color != nil && !hexColor.MatchString(*color) {
		return errors.New("color must look like #rrggbb")
	}
	return nil
}

// validSemester проверяет диапазон и сразу отдаёт значение нужного типа: непроверенного приведения int к int16 нет.
func validSemester(n *int) (*int16, error) {
	if n == nil {
		return nil, nil
	}
	if *n < minSemester || *n > maxSemester {
		return nil, errors.New("semester must be between 1 and 12")
	}
	v := int16(*n)
	return &v, nil
}

// toSubject задаёт color и semester явно: незаданный Nullable сериализовался бы как нулевое значение, а не null.
func toSubject(s db.Subject) Subject {
	return Subject{
		Id:        s.ID,
		Title:     s.Title,
		Color:     nullableText(s.Color),
		Semester:  nullableInt2(s.Semester),
		CreatedAt: s.CreatedAt,
	}
}

func nullableText(t pgtype.Text) nullable.Nullable[string] {
	if !t.Valid {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(t.String)
}

func nullableInt2(n pgtype.Int2) nullable.Nullable[int] {
	if !n.Valid {
		return nullable.NewNullNullable[int]()
	}
	return nullable.NewNullableWithValue(int(n.Int16))
}

// optional раскладывает Nullable в «передано ли поле» и значение (nil, если null).
func optional[T any](n nullable.Nullable[T]) (specified bool, value *T) {
	if !n.IsSpecified() {
		return false, nil
	}
	if n.IsNull() {
		return true, nil
	}
	v := n.MustGet()
	return true, &v
}
