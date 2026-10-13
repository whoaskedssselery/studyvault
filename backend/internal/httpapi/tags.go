package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/tag"
)

const (
	maxTagNameLen  = 50
	maxTagsPerNote = 50
)

// TagService is satisfied by *tag.Service.
type TagService interface {
	List(ctx context.Context, userID uuid.UUID) ([]db.Tag, error)
	Create(ctx context.Context, userID uuid.UUID, name string) (db.Tag, error)
	Delete(ctx context.Context, userID, id uuid.UUID) error
	SetNoteTags(ctx context.Context, userID, noteID uuid.UUID, tagIDs []uuid.UUID) error
}

func (a *api) ListTags(w http.ResponseWriter, r *http.Request) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	items, err := a.Tags.List(r.Context(), userID)
	if err != nil {
		slog.Error("list tags", "err", err)
		internalError(w)
		return
	}
	out := make([]Tag, 0, len(items))
	for _, t := range items {
		out = append(out, Tag{Id: t.ID, Name: t.Name})
	}
	writeJSON(w, http.StatusOK, struct {
		Items []Tag `json:"items"`
	}{Items: out})
}

func (a *api) CreateTag(w http.ResponseWriter, r *http.Request) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req TagInput
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	name, err := trimmedField("name", req.Name, maxTagNameLen)
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	created, err := a.Tags.Create(r.Context(), userID, name)
	switch {
	case errors.Is(err, tag.ErrNameTaken):
		writeError(w, http.StatusConflict, "tag_exists", "a tag with this name already exists")
	case err != nil:
		slog.Error("create tag", "err", err)
		internalError(w)
	default:
		writeJSON(w, http.StatusCreated, Tag{Id: created.ID, Name: created.Name})
	}
}

func (a *api) DeleteTag(w http.ResponseWriter, r *http.Request, id Id) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	err := a.Tags.Delete(r.Context(), userID, id)
	switch {
	case errors.Is(err, tag.ErrNotFound):
		notFound(w, "tag not found")
	case err != nil:
		slog.Error("delete tag", "err", err)
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (a *api) SetNoteTags(w http.ResponseWriter, r *http.Request, id Id) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req SetNoteTagsJSONBody
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	if req.TagIds == nil {
		badRequest(w, "tag_ids is required")
		return
	}
	if len(req.TagIds) > maxTagsPerNote {
		badRequest(w, "a note can have at most 50 tags")
		return
	}

	err := a.Tags.SetNoteTags(r.Context(), userID, id, req.TagIds)
	switch {
	case errors.Is(err, tag.ErrNoteNotFound):
		notFound(w, "note not found")
		return
	case errors.Is(err, tag.ErrInvalidTag):
		badRequest(w, "unknown tag id")
		return
	case err != nil:
		slog.Error("set note tags", "err", err)
		internalError(w)
		return
	}

	updated, err := a.Notes.Get(r.Context(), userID, id)
	a.respondNote(w, http.StatusOK, updated, err, "set note tags")
}
