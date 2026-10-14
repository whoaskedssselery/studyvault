package httpapi

import (
	"net/http"

	"github.com/whoaskedssselery/studyvault/backend/internal/library"
)

// Комментарии, оценки и избранное.

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

func toComment(c library.Comment) Comment {
	return Comment{Id: c.ID, Body: c.Body, Author: toAuthor(c.Author), CreatedAt: c.CreatedAt, EditedAt: nullableTime(c.EditedAt)}
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
