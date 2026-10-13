package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/search"
)

const (
	maxSearchQueryLen = 200
	minSearchLimit    = 1
	maxSearchLimit    = 50
)

// SearchService is satisfied by *search.Service.
type SearchService interface {
	Search(ctx context.Context, userID uuid.UUID, q search.Query) ([]search.Result, error)
}

func (a *api) SearchNotes(w http.ResponseWriter, r *http.Request, params SearchNotesParams) {
	userID, ok := currentUser(w, r)
	if !ok {
		return
	}
	text := strings.TrimSpace(params.Q)
	if n := utf8.RuneCountInString(text); n < 1 || n > maxSearchQueryLen {
		badRequest(w, "q must be 1 to 200 characters")
		return
	}
	q := search.Query{Text: text, SubjectID: params.SubjectId, Tag: params.Tag}
	if params.Limit != nil {
		if *params.Limit < minSearchLimit || *params.Limit > maxSearchLimit {
			badRequest(w, "limit must be between 1 and 50")
			return
		}
		q.Limit = *params.Limit
	}
	if params.Tag != nil && *params.Tag == "" {
		badRequest(w, "tag must not be empty")
		return
	}
	if a.Search == nil {
		searchUnavailable(w)
		return
	}

	results, err := a.Search.Search(r.Context(), userID, q)
	switch {
	case errors.Is(err, search.ErrUnavailable):
		slog.Warn("search is unavailable", "err", err)
		searchUnavailable(w)
	case err != nil:
		slog.Error("search", "err", err)
		internalError(w)
	default:
		items := make([]SearchHit, 0, len(results))
		for _, res := range results {
			items = append(items, SearchHit{
				Id:        res.ID,
				Title:     res.Title,
				Snippet:   res.Snippet,
				SubjectId: nullableUUID(res.SubjectID),
				UpdatedAt: res.UpdatedAt,
			})
		}
		writeJSON(w, http.StatusOK, struct {
			Items []SearchHit `json:"items"`
		}{Items: items})
	}
}

func searchUnavailable(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "search_unavailable", "search is temporarily unavailable")
}
