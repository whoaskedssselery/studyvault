package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/attachment"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/storage"
)

const maxFileNameLen = 255

// AttachmentService is satisfied by *attachment.Service.
type AttachmentService interface {
	Create(ctx context.Context, userID uuid.UUID, in attachment.Input) (attachment.Upload, error)
	Get(ctx context.Context, userID, id uuid.UUID) (attachment.Download, error)
	List(ctx context.Context, userID, noteID uuid.UUID) ([]db.Attachment, error)
	Delete(ctx context.Context, userID, id uuid.UUID) error
}

func (a *api) CreateAttachment(w http.ResponseWriter, r *http.Request) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req AttachmentInput
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	name, err := cleanFileName(req.FileName)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if a.Attachments == nil {
		storageUnavailable(w)
		return
	}
	_, noteID := optional(req.NoteId)

	up, err := a.Attachments.Create(r.Context(), userID, attachment.Input{
		NoteID:    noteID,
		FileName:  name,
		MimeType:  string(req.MimeType),
		SizeBytes: req.SizeBytes,
	})
	if err != nil {
		a.attachmentError(w, r, err, "create attachment")
		return
	}
	writeJSON(w, http.StatusCreated, AttachmentUpload{Attachment: toAttachment(up.Attachment), UploadUrl: up.URL})
}

func (a *api) GetAttachment(w http.ResponseWriter, r *http.Request, id Id) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	if a.Attachments == nil {
		storageUnavailable(w)
		return
	}
	dl, err := a.Attachments.Get(r.Context(), userID, id)
	if err != nil {
		a.attachmentError(w, r, err, "get attachment")
		return
	}
	writeJSON(w, http.StatusOK, AttachmentDownload{Attachment: toAttachment(dl.Attachment), DownloadUrl: dl.URL})
}

func (a *api) ListAttachments(w http.ResponseWriter, r *http.Request, params ListAttachmentsParams) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	if a.Attachments == nil {
		storageUnavailable(w)
		return
	}
	rows, err := a.Attachments.List(r.Context(), userID, params.NoteId)
	if err != nil {
		a.attachmentError(w, r, err, "list attachments")
		return
	}
	items := make([]Attachment, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAttachment(row))
	}
	writeJSON(w, http.StatusOK, struct {
		Items []Attachment `json:"items"`
	}{Items: items})
}

func (a *api) DeleteAttachment(w http.ResponseWriter, r *http.Request, id Id) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	if a.Attachments == nil {
		storageUnavailable(w)
		return
	}
	if err := a.Attachments.Delete(r.Context(), userID, id); err != nil {
		a.attachmentError(w, r, err, "delete attachment")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) attachmentError(w http.ResponseWriter, r *http.Request, err error, op string) {
	switch {
	case errors.Is(err, attachment.ErrNotFound):
		notFound(w, "attachment not found")
	case errors.Is(err, attachment.ErrNoteNotFound):
		notFound(w, "note not found")
	case errors.Is(err, attachment.ErrNotUploaded):
		writeError(w, http.StatusConflict, "not_uploaded", "the file has not been uploaded yet")
	case errors.Is(err, attachment.ErrInvalid):
		badRequest(w, err.Error())
	case errors.Is(err, storage.ErrUnavailable):
		slog.WarnContext(r.Context(), op+": storage is unavailable", "err", err)
		storageUnavailable(w)
	default:
		slog.ErrorContext(r.Context(), op, "err", err)
		internalError(w)
	}
}

func storageUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "file storage is temporarily unavailable")
}

// cleanFileName оставляет только само имя файла: путь отбрасывается, управляющие символы запрещены.
// Имя потом попадает в заголовок скачивания, поэтому переводы строк в нём недопустимы.
func cleanFileName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", errors.New("file_name must not contain control characters")
	}
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > maxFileNameLen {
		return "", fmt.Errorf("file_name must be 1 to %d characters", maxFileNameLen)
	}
	return name, nil
}

// toAttachment не отдаёт ни ключ объекта, ни владельца.
func toAttachment(r db.Attachment) Attachment {
	var noteID *uuid.UUID
	if r.NoteID.Valid {
		id := r.NoteID.UUID
		noteID = &id
	}
	return Attachment{
		Id:        r.ID,
		NoteId:    nullableUUID(noteID),
		FileName:  r.FileName,
		MimeType:  r.MimeType,
		SizeBytes: r.SizeBytes,
		CreatedAt: r.CreatedAt,
	}
}
