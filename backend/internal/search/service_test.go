package search_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/search"
)

type fakeEngine struct {
	hits []search.Hit
	err  error

	gotUser  uuid.UUID
	gotQuery string
	gotLimit int
	calls    int
}

func (f *fakeEngine) Search(_ context.Context, userID uuid.UUID, query string, limit int) ([]search.Hit, error) {
	f.calls++
	f.gotUser, f.gotQuery, f.gotLimit = userID, query, limit
	return f.hits, f.err
}

type fakeStore struct {
	rows  []db.FilterLiveNotesRow
	err   error
	got   db.FilterLiveNotesParams
	calls int
}

func (f *fakeStore) FilterLiveNotes(_ context.Context, a db.FilterLiveNotesParams) ([]db.FilterLiveNotesRow, error) {
	f.calls++
	f.got = a
	return f.rows, f.err
}

func row(id uuid.UUID, title string) db.FilterLiveNotesRow {
	return db.FilterLiveNotesRow{ID: id, Title: title, UpdatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
}

func TestSearch_KeepsEngineRankingAndDropsWhatTheDatabaseRejects(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	eng := &fakeEngine{hits: []search.Hit{{ID: a}, {ID: b}, {ID: c}}}
	// b удалён (или чужой): база его не вернула. Строки приходят в произвольном порядке.
	store := &fakeStore{rows: []db.FilterLiveNotesRow{row(c, "C"), row(a, "A")}}

	got, err := search.NewService(eng, store).Search(context.Background(), uuid.New(), search.Query{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].ID != a || got[1].ID != c {
		t.Fatalf("results = %+v, want [a, c] in the engine's order without b", got)
	}
}

func TestSearch_TakesTitleAndSubjectFromTheDatabase(t *testing.T) {
	id, sid := uuid.New(), uuid.New()
	eng := &fakeEngine{hits: []search.Hit{{ID: id, Snippet: "text"}}}
	r := row(id, "Fresh title from Postgres")
	r.SubjectID = uuid.NullUUID{UUID: sid, Valid: true}
	store := &fakeStore{rows: []db.FilterLiveNotesRow{r}}

	got, _ := search.NewService(eng, store).Search(context.Background(), uuid.New(), search.Query{Text: "x"})

	if got[0].Title != "Fresh title from Postgres" || got[0].SubjectID == nil || *got[0].SubjectID != sid || got[0].UpdatedAt.IsZero() {
		t.Errorf("result: %+v", got[0])
	}
}

func TestSearch_ScopesBothStepsToTheUser(t *testing.T) {
	uid, id, sid := uuid.New(), uuid.New(), uuid.New()
	tag := "exam"
	eng := &fakeEngine{hits: []search.Hit{{ID: id}}}
	store := &fakeStore{rows: []db.FilterLiveNotesRow{row(id, "T")}}

	_, err := search.NewService(eng, store).Search(context.Background(), uid, search.Query{Text: "algebra", SubjectID: &sid, Tag: &tag})
	if err != nil {
		t.Fatal(err)
	}

	if eng.gotUser != uid || eng.gotQuery != "algebra" {
		t.Errorf("engine got user %v, query %q", eng.gotUser, eng.gotQuery)
	}
	g := store.got
	if g.UserID != uid || len(g.Ids) != 1 || g.Ids[0] != id {
		t.Errorf("store must re-check the engine's ids for this user: %+v", g)
	}
	if !g.SubjectID.Valid || g.SubjectID.UUID != sid || g.Tag != (pgtype.Text{String: "exam", Valid: true}) {
		t.Errorf("filters must be applied in the database: %+v", g)
	}
}

func TestSearch_NoFilters_SendsNullFilters(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{rows: []db.FilterLiveNotesRow{row(id, "T")}}

	_, _ = search.NewService(&fakeEngine{hits: []search.Hit{{ID: id}}}, store).Search(context.Background(), uuid.New(), search.Query{Text: "x"})

	if store.got.SubjectID.Valid || store.got.Tag.Valid {
		t.Errorf("absent filters must be NULL: %+v", store.got)
	}
}

func TestSearch_LimitDefaultsClampsAndTruncates(t *testing.T) {
	hits := make([]search.Hit, 100)
	rows := make([]db.FilterLiveNotesRow, 100)
	for i := range hits {
		id := uuid.New()
		hits[i] = search.Hit{ID: id}
		rows[i] = row(id, "n")
	}

	cases := []struct{ limit, wantResults, wantFetch int }{
		{0, 20, 80},
		{-3, 20, 80},
		{5, 5, 20},
		{1000, 50, 200},
	}
	for _, c := range cases {
		eng := &fakeEngine{hits: hits}
		got, err := search.NewService(eng, &fakeStore{rows: rows}).Search(context.Background(), uuid.New(), search.Query{Text: "x", Limit: c.limit})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != c.wantResults {
			t.Errorf("limit %d: %d results, want %d", c.limit, len(got), c.wantResults)
		}
		if eng.gotLimit != c.wantFetch {
			t.Errorf("limit %d: asked the engine for %d hits, want %d (headroom for filtered-out ones)", c.limit, eng.gotLimit, c.wantFetch)
		}
	}
}

func TestSearch_NoHits_SkipsTheDatabaseAndReturnsEmptySlice(t *testing.T) {
	store := &fakeStore{}

	got, err := search.NewService(&fakeEngine{}, store).Search(context.Background(), uuid.New(), search.Query{Text: "nothing"})

	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %#v, err %v: want an empty non-nil slice", got, err)
	}
	if store.calls != 0 {
		t.Error("with no candidates there is nothing to re-check")
	}
}

func TestSearch_EngineDown_ReturnsErrUnavailable(t *testing.T) {
	boom := errors.New("connection refused")

	_, err := search.NewService(&fakeEngine{err: boom}, &fakeStore{}).Search(context.Background(), uuid.New(), search.Query{Text: "x"})

	if !errors.Is(err, search.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestSearch_DatabaseError_IsNotReportedAsUnavailable(t *testing.T) {
	id := uuid.New()
	boom := errors.New("pg down")

	_, err := search.NewService(&fakeEngine{hits: []search.Hit{{ID: id}}}, &fakeStore{err: boom}).Search(context.Background(), uuid.New(), search.Query{Text: "x"})

	if errors.Is(err, search.ErrUnavailable) || !errors.Is(err, boom) {
		t.Fatalf("err = %v: a database failure is an internal error, not 'search unavailable'", err)
	}
}

func TestSearch_SnippetIsHTMLEscapedExceptForMarkTags(t *testing.T) {
	id := uuid.New()
	raw := "a <script>alert(1)</script> \x01found\x02 & more \"q\""
	eng := &fakeEngine{hits: []search.Hit{{ID: id, Snippet: raw}}}

	got, _ := search.NewService(eng, &fakeStore{rows: []db.FilterLiveNotesRow{row(id, "T")}}).Search(context.Background(), uuid.New(), search.Query{Text: "x"})

	want := "a &lt;script&gt;alert(1)&lt;/script&gt; <mark>found</mark> &amp; more &#34;q&#34;"
	if got[0].Snippet != want {
		t.Errorf("snippet = %q\nwant      %q", got[0].Snippet, want)
	}
}
