package httpapi

import (
	"net/http"

	"github.com/whoaskedssselery/studyvault/backend/internal/library"
)

// Публичные сведения об авторах: рейтинг и профиль.

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
