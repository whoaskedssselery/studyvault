package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/whoaskedssselery/studyvault/backend/internal/note"
)

const (
	maxNoteTitleLen = 300
	minPageLimit    = 1
	maxPageLimit    = 100
)

// NoteService is satisfied by *note.Service.
type NoteService interface {
	Create(ctx context.Context, userID uuid.UUID, in note.Input) (note.Note, error)
	Get(ctx context.Context, userID, id uuid.UUID) (note.Note, error)
	Update(ctx context.Context, userID, id uuid.UUID, p note.Patch) (note.Note, error)
	Delete(ctx context.Context, userID, id uuid.UUID) error
	List(ctx context.Context, userID uuid.UUID, f note.ListFilter) (note.Page, error)
	Backlinks(ctx context.Context, userID, id uuid.UUID) ([]note.Summary, error)
	Graph(ctx context.Context, userID uuid.UUID) (note.Graph, error)
}

func (a *api) ListNotes(w http.ResponseWriter, r *http.Request, params ListNotesParams) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	filter := note.ListFilter{SubjectID: params.SubjectId, Pinned: params.Pinned, Tag: params.Tag}
	if params.Limit != nil {
		if *params.Limit < minPageLimit || *params.Limit > maxPageLimit {
			badRequest(w, "limit must be between 1 and 100")
			return
		}
		filter.Limit = *params.Limit
	}
	if params.Tag != nil && *params.Tag == "" {
		badRequest(w, "tag must not be empty")
		return
	}
	if params.Cursor != nil {
		filter.Cursor = *params.Cursor
	}

	page, err := a.Notes.List(r.Context(), userID, filter)
	switch {
	case errors.Is(err, note.ErrInvalidCursor):
		badRequest(w, "cursor is not valid")
	case err != nil:
		slog.ErrorContext(r.Context(), "list notes", "err", err)
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, toNotePage(page))
	}
}

func (a *api) CreateNote(w http.ResponseWriter, r *http.Request) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req NoteInput
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	title, err := trimmedTitle(req.Title, maxNoteTitleLen)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	content, err := encodeDocument(req.Content)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	_, subjectID := optional(req.SubjectId)
	in := note.Input{Title: title, Content: content, SubjectID: subjectID}
	if req.IsPinned != nil {
		in.IsPinned = *req.IsPinned
	}

	created, err := a.Notes.Create(r.Context(), userID, in)
	a.respondNote(w, r, http.StatusCreated, created, err, "create note")
}

func (a *api) GetNote(w http.ResponseWriter, r *http.Request, id Id) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	found, err := a.Notes.Get(r.Context(), userID, id)
	a.respondNote(w, r, http.StatusOK, found, err, "get note")
}

func (a *api) UpdateNote(w http.ResponseWriter, r *http.Request, id Id) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req NotePatch
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	if req.Title == nil && req.Content == nil && req.IsPinned == nil && !req.SubjectId.IsSpecified() {
		badRequest(w, "at least one of title, content, subject_id, is_pinned is required")
		return
	}

	patch := note.Patch{IsPinned: req.IsPinned}
	if req.Title != nil {
		title, err := trimmedTitle(*req.Title, maxNoteTitleLen)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		patch.Title = &title
	}
	if req.Content != nil {
		content, err := encodeDocument(*req.Content)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		patch.Content = content
	}
	patch.SetSubject, patch.SubjectID = optional(req.SubjectId)

	updated, err := a.Notes.Update(r.Context(), userID, id, patch)
	a.respondNote(w, r, http.StatusOK, updated, err, "update note")
}

func (a *api) DeleteNote(w http.ResponseWriter, r *http.Request, id Id) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	err := a.Notes.Delete(r.Context(), userID, id)
	switch {
	case errors.Is(err, note.ErrNotFound):
		notFound(w, "note not found")
	case err != nil:
		slog.ErrorContext(r.Context(), "delete note", "err", err)
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// respondNote общий для create, get и update: они возвращают заметку и одинаково разбирают ошибки.
func (a *api) respondNote(w http.ResponseWriter, r *http.Request, status int, n note.Note, err error, op string) {
	switch {
	case errors.Is(err, note.ErrNotFound):
		notFound(w, "note not found")
	case errors.Is(err, note.ErrInvalidSubject):
		badRequest(w, "subject does not exist")
	case err != nil:
		slog.ErrorContext(r.Context(), op, "err", err)
		internalError(w)
	default:
		body, convErr := toNote(n)
		if convErr != nil {
			slog.ErrorContext(r.Context(), op+": stored document is not valid JSON", "err", convErr)
			internalError(w)
			return
		}
		writeJSON(w, status, body)
	}
}

// encodeDocument проверяет, что content похож на документ Tiptap, и превращает его в JSON для хранения.
func encodeDocument(doc TiptapDoc) ([]byte, error) {
	if doc == nil {
		return nil, errors.New("content is required")
	}
	if doc["type"] != "doc" {
		return nil, errors.New(`content must be a Tiptap document: {"type":"doc",...}`)
	}
	return json.Marshal(doc)
}

// toNoteSummary задаёт subject_id и tags явно: незаданный Nullable сериализовался бы как нулевое
// значение, а не null, а nil-срез дал бы null вместо [].
func toNoteSummary(s note.Summary) NoteSummary {
	tags := make([]Tag, 0, len(s.Tags))
	for _, t := range s.Tags {
		tags = append(tags, Tag{Id: t.ID, Name: t.Name})
	}
	return NoteSummary{
		Id:        s.ID,
		SubjectId: nullableUUID(s.SubjectID),
		Title:     s.Title,
		IsPinned:  s.IsPinned,
		Tags:      tags,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

func toNote(n note.Note) (Note, error) {
	var doc TiptapDoc
	if err := json.Unmarshal(n.Content, &doc); err != nil {
		return Note{}, err
	}
	s := toNoteSummary(n.Summary)
	return Note{
		Id:        s.Id,
		SubjectId: s.SubjectId,
		Title:     s.Title,
		IsPinned:  s.IsPinned,
		Tags:      s.Tags,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
		Content:   doc,
	}, nil
}

func toNotePage(p note.Page) NotePage {
	items := make([]NoteSummary, 0, len(p.Items))
	for _, s := range p.Items {
		items = append(items, toNoteSummary(s))
	}
	next := nullable.NewNullNullable[string]()
	if p.NextCursor != "" {
		next = nullable.NewNullableWithValue(p.NextCursor)
	}
	return NotePage{Items: items, NextCursor: next}
}

func nullableUUID(id *uuid.UUID) nullable.Nullable[uuid.UUID] {
	if id == nil {
		return nullable.NewNullNullable[uuid.UUID]()
	}
	return nullable.NewNullableWithValue(*id)
}

func (a *api) ListBacklinks(w http.ResponseWriter, r *http.Request, id Id) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	linked, err := a.Notes.Backlinks(r.Context(), userID, id)
	switch {
	case errors.Is(err, note.ErrNotFound):
		notFound(w, "note not found")
	case err != nil:
		slog.ErrorContext(r.Context(), "list backlinks", "err", err)
		internalError(w)
	default:
		items := make([]NoteSummary, 0, len(linked))
		for _, s := range linked {
			items = append(items, toNoteSummary(s))
		}
		writeJSON(w, http.StatusOK, struct {
			Items []NoteSummary `json:"items"`
		}{Items: items})
	}
}

func (a *api) GetGraph(w http.ResponseWriter, r *http.Request) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	g, err := a.Notes.Graph(r.Context(), userID)
	if err != nil {
		slog.ErrorContext(r.Context(), "get graph", "err", err)
		internalError(w)
		return
	}

	var out Graph
	out.Nodes = make([]struct {
		Id    uuid.UUID `json:"id"`
		Title string    `json:"title"`
	}, 0, len(g.Nodes))
	out.Edges = make([]struct {
		Source uuid.UUID `json:"source"`
		Target uuid.UUID `json:"target"`
	}, 0, len(g.Edges))
	for _, n := range g.Nodes {
		out.Nodes = append(out.Nodes, struct {
			Id    uuid.UUID `json:"id"`
			Title string    `json:"title"`
		}{Id: n.ID, Title: n.Title})
	}
	for _, e := range g.Edges {
		out.Edges = append(out.Edges, struct {
			Source uuid.UUID `json:"source"`
			Target uuid.UUID `json:"target"`
		}{Source: e.Source, Target: e.Target})
	}
	writeJSON(w, http.StatusOK, out)
}
