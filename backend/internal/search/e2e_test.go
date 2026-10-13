package search_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/note"
	"github.com/whoaskedssselery/studyvault/backend/internal/search"
	"github.com/whoaskedssselery/studyvault/backend/internal/subject"
)

// Сквозные тесты: настоящие Postgres (в транзакции, которая откатывается) и Meilisearch (временный индекс).
type e2e struct {
	tx     pgx.Tx
	meili  *search.Meili
	notes  *note.Service
	subs   *subject.Service
	search *search.Service
}

func newE2E(t *testing.T) e2e {
	t.Helper()
	m := newMeili(t) // пропустит тест без MEILI_URL
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Skipf("database is not reachable: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	q := db.New(tx)
	return e2e{
		tx:     tx,
		meili:  m,
		notes:  note.NewService(q, note.WithIndexer(m)),
		subs:   subject.NewService(q),
		search: search.NewService(m, q),
	}
}

func (e e2e) user(t *testing.T, email string) uuid.UUID {
	t.Helper()
	u, err := db.New(e.tx).CreateUser(context.Background(), db.CreateUserParams{Email: email, PasswordHash: "x", DisplayName: email})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func text(s string) []byte {
	return []byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"` + s + `"}]}]}`)
}

func (e e2e) create(t *testing.T, uid uuid.UUID, title, body string) note.Note {
	t.Helper()
	n, err := e.notes.Create(context.Background(), uid, note.Input{Title: title, Content: text(body)})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (e e2e) find(t *testing.T, uid uuid.UUID, q search.Query) []search.Result {
	t.Helper()
	settle(t, e.meili)
	res, err := e.search.Search(context.Background(), uid, q)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func resultIDs(res []search.Result) []uuid.UUID {
	out := make([]uuid.UUID, len(res))
	for i, r := range res {
		out[i] = r.ID
	}
	return out
}

func TestE2E_OneUserNeverSeesAnothersNotes(t *testing.T) {
	e := newE2E(t)
	alice, bob := e.user(t, "alice@x.io"), e.user(t, "bob@x.io")
	a := e.create(t, alice, "Alice on heat", "thermodynamics notes")
	b := e.create(t, bob, "Bob on heat", "thermodynamics notes")

	if got := resultIDs(e.find(t, alice, search.Query{Text: "thermodynamics"})); len(got) != 1 || got[0] != a.ID {
		t.Errorf("alice sees %v, want only %v", got, a.ID)
	}
	if got := resultIDs(e.find(t, bob, search.Query{Text: "thermodynamics"})); len(got) != 1 || got[0] != b.ID {
		t.Errorf("bob sees %v, want only %v", got, b.ID)
	}
}

func TestE2E_DeletedNotesAreNotReturned(t *testing.T) {
	e := newE2E(t)
	ctx := context.Background()
	uid := e.user(t, "a@x.io")
	viaService := e.create(t, uid, "Removed properly", "quantum entanglement")
	stale := e.create(t, uid, "Removed behind the index's back", "quantum entanglement")
	keep := e.create(t, uid, "Kept", "quantum entanglement")

	if got := e.find(t, uid, search.Query{Text: "quantum"}); len(got) != 3 {
		t.Fatalf("before deletion: %d results, want 3", len(got))
	}

	if err := e.notes.Delete(ctx, uid, viaService.ID); err != nil {
		t.Fatal(err)
	}
	// Заметка помечена удалённой напрямую в БД, а индекс об этом не знает: так выглядит сбой синхронизации.
	if _, err := e.tx.Exec(ctx, `UPDATE notes SET deleted_at = now() WHERE id = $1`, stale.ID); err != nil {
		t.Fatal(err)
	}

	got := resultIDs(e.find(t, uid, search.Query{Text: "quantum"}))
	if len(got) != 1 || got[0] != keep.ID {
		t.Errorf("results = %v, want only the surviving note %v (a stale index entry must be filtered by the database)", got, keep.ID)
	}
}

func TestE2E_FiltersBySubjectAndTag(t *testing.T) {
	e := newE2E(t)
	ctx := context.Background()
	alice, bob := e.user(t, "alice@x.io"), e.user(t, "bob@x.io")
	math, _ := e.subs.Create(ctx, alice, subject.Input{Title: "Math"})

	inMath, err := e.notes.Create(ctx, alice, note.Input{Title: "Limits", Content: text("convergence of sequences"), SubjectID: &math.ID})
	if err != nil {
		t.Fatal(err)
	}
	tagged := e.create(t, alice, "Series", "convergence of series")
	var aliceTag, bobTag uuid.UUID
	_ = e.tx.QueryRow(ctx, `INSERT INTO tags (user_id, name) VALUES ($1, 'exam') RETURNING id`, alice).Scan(&aliceTag)
	_ = e.tx.QueryRow(ctx, `INSERT INTO tags (user_id, name) VALUES ($1, 'exam') RETURNING id`, bob).Scan(&bobTag)
	_, _ = e.tx.Exec(ctx, `INSERT INTO note_tags (note_id, tag_id) VALUES ($1, $2)`, tagged.ID, aliceTag)
	e.create(t, alice, "Plain", "convergence in general")

	exam := "exam"
	if got := resultIDs(e.find(t, alice, search.Query{Text: "convergence"})); len(got) != 3 {
		t.Errorf("no filters: %d results, want 3", len(got))
	}
	if got := resultIDs(e.find(t, alice, search.Query{Text: "convergence", SubjectID: &math.ID})); len(got) != 1 || got[0] != inMath.ID {
		t.Errorf("subject filter: %v", got)
	}
	if got := resultIDs(e.find(t, alice, search.Query{Text: "convergence", Tag: &exam})); len(got) != 1 || got[0] != tagged.ID {
		t.Errorf("tag filter: %v", got)
	}
	if got := e.find(t, bob, search.Query{Text: "convergence", Tag: &exam}); len(got) != 0 {
		t.Errorf("bob has the same tag name but none of alice's notes: %v", resultIDs(got))
	}
	_ = bobTag
}

func TestE2E_EditsAreReflectedInSearch(t *testing.T) {
	e := newE2E(t)
	ctx := context.Background()
	uid := e.user(t, "a@x.io")
	n := e.create(t, uid, "Draft", "mentions photosynthesis")

	if len(e.find(t, uid, search.Query{Text: "photosynthesis"})) != 1 {
		t.Fatal("the new note must be searchable")
	}

	if _, err := e.notes.Update(ctx, uid, n.ID, note.Patch{Content: text("now about mitochondria")}); err != nil {
		t.Fatal(err)
	}
	if got := e.find(t, uid, search.Query{Text: "photosynthesis"}); len(got) != 0 {
		t.Error("the old text must stop matching")
	}
	if got := e.find(t, uid, search.Query{Text: "mitochondria"}); len(got) != 1 || got[0].ID != n.ID {
		t.Errorf("the new text must match: %v", resultIDs(got))
	}

	title := "Cell biology"
	if _, err := e.notes.Update(ctx, uid, n.ID, note.Patch{Title: &title}); err != nil {
		t.Fatal(err)
	}
	got := e.find(t, uid, search.Query{Text: "biology"})
	if len(got) != 1 || got[0].Title != "Cell biology" {
		t.Errorf("the new title must match and be shown: %+v", got)
	}
}

func TestE2E_SnippetHighlightsAndEscapes(t *testing.T) {
	e := newE2E(t)
	uid := e.user(t, "a@x.io")
	e.create(t, uid, "Markup", "use <b>bold</b> & the keyword here")

	got := e.find(t, uid, search.Query{Text: "keyword"})

	if len(got) != 1 {
		t.Fatalf("results = %d", len(got))
	}
	if !strings.Contains(got[0].Snippet, "<mark>keyword</mark>") || strings.Contains(got[0].Snippet, "<b>") || !strings.Contains(got[0].Snippet, "&lt;b&gt;") {
		t.Errorf("snippet must keep <mark> and escape everything else: %q", got[0].Snippet)
	}
}

func TestE2E_ReplaceAllRebuildsFromTheDatabase(t *testing.T) {
	e := newE2E(t)
	ctx := context.Background()
	uid := e.user(t, "a@x.io")
	live := e.create(t, uid, "Live", "resurrected phrase")
	gone := e.create(t, uid, "Gone", "resurrected phrase")
	if err := e.notes.Delete(ctx, uid, gone.ID); err != nil {
		t.Fatal(err)
	}
	// Индекс «потерялся»: пересобираем его целиком из БД.
	if _, err := e.meili.ReplaceAll(ctx, search.ReindexPages(db.New(e.tx), 2)); err != nil {
		t.Fatal(err)
	}

	got := resultIDs(e.find(t, uid, search.Query{Text: "resurrected"}))

	if len(got) != 1 || got[0] != live.ID {
		t.Errorf("after a rebuild only live notes are searchable, got %v", got)
	}
}
