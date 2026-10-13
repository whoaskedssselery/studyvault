package search_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/search"
)

type fakeSource struct {
	rows  []db.ListNotesForIndexRow // уже отсортированы по id
	err   error
	calls []db.ListNotesForIndexParams
}

func (f *fakeSource) ListNotesForIndex(_ context.Context, a db.ListNotesForIndexParams) ([]db.ListNotesForIndexRow, error) {
	f.calls = append(f.calls, a)
	if f.err != nil {
		return nil, f.err
	}
	var out []db.ListNotesForIndexRow
	for _, r := range f.rows {
		if r.ID.String() > a.After.String() && len(out) < int(a.PageSize) {
			out = append(out, r)
		}
	}
	return out, nil
}

func TestReindexPages_WalksEveryRowOnceByKeyset(t *testing.T) {
	var rows []db.ListNotesForIndexRow
	for i := 0; i < 5; i++ {
		rows = append(rows, db.ListNotesForIndexRow{ID: uuid.MustParse("00000000-0000-0000-0000-00000000000" + string(rune('1'+i))), UserID: uuid.New(), Title: "t", ContentText: "c"})
	}
	src := &fakeSource{rows: rows}
	next := search.ReindexPages(src, 2)

	var seen []uuid.UUID
	for pages := 0; ; pages++ {
		docs, err := next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(docs) == 0 {
			break
		}
		for _, d := range docs {
			seen = append(seen, d.ID)
		}
		if pages > 10 {
			t.Fatal("does not terminate")
		}
	}

	if len(seen) != 5 {
		t.Fatalf("saw %d rows, want 5 exactly once: %v", len(seen), seen)
	}
	for i, id := range seen {
		if id != rows[i].ID {
			t.Errorf("position %d: %v, want %v", i, id, rows[i].ID)
		}
	}
	if src.calls[0].After != uuid.Nil || src.calls[1].After != rows[1].ID {
		t.Errorf("each page must continue after the last id of the previous one: %+v", src.calls)
	}
}

func TestReindexPages_MapsRowsToDocuments(t *testing.T) {
	r := db.ListNotesForIndexRow{ID: uuid.New(), UserID: uuid.New(), Title: "T", ContentText: "body"}

	docs, _ := search.ReindexPages(&fakeSource{rows: []db.ListNotesForIndexRow{r}}, 10)(context.Background())

	if len(docs) != 1 || docs[0] != (search.Doc{ID: r.ID, UserID: r.UserID, Title: "T", ContentText: "body"}) {
		t.Errorf("docs = %+v", docs)
	}
}

func TestReindexPages_SourceError_IsReturned(t *testing.T) {
	boom := errors.New("db down")

	_, err := search.ReindexPages(&fakeSource{err: boom}, 10)(context.Background())

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
