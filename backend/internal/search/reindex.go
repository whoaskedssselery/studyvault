package search

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
)

// IndexSource отдаёт живые заметки постранично, по возрастанию id.
type IndexSource interface {
	ListNotesForIndex(ctx context.Context, arg db.ListNotesForIndexParams) ([]db.ListNotesForIndexRow, error)
}

// ReindexPages возвращает функцию для Meili.ReplaceAll: каждый вызов отдаёт следующую страницу
// документов, а пустой список означает конец. Страницы идут по ключу id, поэтому ни одна
// заметка не пропускается и не повторяется, даже если таблица меняется во время обхода.
func ReindexPages(src IndexSource, pageSize int32) func(context.Context) ([]Doc, error) {
	after := uuid.Nil
	return func(ctx context.Context) ([]Doc, error) {
		rows, err := src.ListNotesForIndex(ctx, db.ListNotesForIndexParams{After: after, PageSize: pageSize})
		if err != nil {
			return nil, fmt.Errorf("list notes for index: %w", err)
		}
		docs := make([]Doc, len(rows))
		for i, r := range rows {
			docs[i] = Doc{ID: r.ID, UserID: r.UserID, Title: r.Title, ContentText: r.ContentText, Status: r.Status}
			after = r.ID
		}
		return docs, nil
	}
}
