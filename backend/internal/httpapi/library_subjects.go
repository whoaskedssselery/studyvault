package httpapi

import (
	"net/http"

	"github.com/whoaskedssselery/studyvault/backend/internal/library"
)

// Общие предметы библиотеки: читать может любой, менять модератор.

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

func toLibrarySubject(s library.Subject) LibrarySubject {
	return LibrarySubject{Id: s.ID, Title: s.Title, Color: nullableOf(s.Color), MaterialsCount: s.MaterialsCount}
}
