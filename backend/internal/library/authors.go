package library

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/whoaskedssselery/studyvault/backend/internal/pgconv"
)

// AuthorStats — публичные сведения об авторе: кто он и как оценены его опубликованные материалы.
type AuthorStats struct {
	Author
	MaterialsCount int64
	RatingsCount   int64
	RatingAverage  *float64 // nil, пока у его материалов нет ни одной оценки
	Views          int64
}

// RankedAuthor — строка рейтинга.
type RankedAuthor struct {
	Rank int // место, начиная с 1; авторы с равными показателями получают разные места по порядку
	AuthorStats
}

const defaultRankingLimit, maxRankingLimit = 20, 100

// Ranking возвращает лучших авторов. Показатель автора: среднее арифметическое всех оценок всех его опубликованных
// материалов; при равенстве выше тот, у кого больше оценок, затем больше материалов. В рейтинг попадают только авторы
// с опубликованными материалами, а те, у кого нет оценок, стоят в конце. Список не постраничный: это «топ N».
func (s *Service) Ranking(ctx context.Context, limit int) ([]RankedAuthor, error) {
	if limit <= 0 {
		limit = defaultRankingLimit
	}
	limit = min(limit, maxRankingLimit)
	rows, err := s.store.ListAuthorRanking(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("list author ranking: %w", err)
	}
	out := make([]RankedAuthor, len(rows))
	for i, r := range rows {
		out[i] = RankedAuthor{Rank: i + 1, AuthorStats: authorStats(r.ID, r.DisplayName, r.Faculty, r.MaterialsCount, r.RatingsCount, r.RatingAvg, r.Views)}
	}
	return out, nil
}

// AuthorProfile отдаёт публичный профиль автора. У пользователя без опубликованных материалов профиля нет (ErrNotFound):
// иначе по id можно было бы выяснять, кто зарегистрирован.
func (s *Service) AuthorProfile(ctx context.Context, id uuid.UUID) (AuthorStats, error) {
	r, err := s.store.GetAuthorProfile(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthorStats{}, ErrNotFound
	}
	if err != nil {
		return AuthorStats{}, fmt.Errorf("get author profile: %w", err)
	}
	return authorStats(r.ID, r.DisplayName, r.Faculty, r.MaterialsCount, r.RatingsCount, r.RatingAvg, r.Views), nil
}

func authorStats(id uuid.UUID, name string, faculty pgtype.Text, materials, ratings int64, avg float64, views int64) AuthorStats {
	st := AuthorStats{
		Author:         Author{ID: id, DisplayName: name, Faculty: pgconv.FromText(faculty)},
		MaterialsCount: materials,
		RatingsCount:   ratings,
		Views:          views,
	}
	if ratings > 0 {
		st.RatingAverage = &avg
	}
	return st
}
