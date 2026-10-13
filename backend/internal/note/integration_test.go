package note_test

import (
	"context"
	"errors"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/note"
	"github.com/whoaskedssselery/studyvault/backend/internal/subject"
)

// Интеграционные тесты идут на настоящем Postgres (docker compose up) внутри транзакции,
// которая откатывается в конце. Без DATABASE_URL они пропускаются.
type env struct {
	tx    pgx.Tx
	notes *note.Service
	subs  *subject.Service
}

func newEnv(t *testing.T) env {
	t.Helper()
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
	return env{tx: tx, notes: note.NewService(q), subs: subject.NewService(q)}
}

func (e env) user(t *testing.T, email string) uuid.UUID {
	t.Helper()
	u, err := db.New(e.tx).CreateUser(context.Background(), db.CreateUserParams{Email: email, PasswordHash: "x", DisplayName: email})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func (e env) note(t *testing.T, uid uuid.UUID, title string) note.Note {
	t.Helper()
	n, err := e.notes.Create(context.Background(), uid, note.Input{Title: title, Content: doc(title)})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (e env) setUpdatedAt(t *testing.T, id uuid.UUID, at time.Time) {
	t.Helper()
	if _, err := e.tx.Exec(context.Background(), `UPDATE notes SET updated_at = $1 WHERE id = $2`, at, id); err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestDB_OtherUsersNotes_AreInvisibleAndUntouchable(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	alice, bob := e.user(t, "alice@x.io"), e.user(t, "bob@x.io")
	n := e.note(t, alice, "secret")

	if _, err := e.notes.Get(ctx, bob, n.ID); !errors.Is(err, note.ErrNotFound) {
		t.Errorf("bob reading alice's note: err = %v, want ErrNotFound", err)
	}
	if _, err := e.notes.Update(ctx, bob, n.ID, note.Patch{Title: ptr("hacked")}); !errors.Is(err, note.ErrNotFound) {
		t.Errorf("bob updating alice's note: err = %v, want ErrNotFound", err)
	}
	if err := e.notes.Delete(ctx, bob, n.ID); !errors.Is(err, note.ErrNotFound) {
		t.Errorf("bob deleting alice's note: err = %v, want ErrNotFound", err)
	}
	if page, _ := e.notes.List(ctx, bob, note.ListFilter{}); len(page.Items) != 0 {
		t.Errorf("bob sees %d of alice's notes", len(page.Items))
	}

	got, err := e.notes.Get(ctx, alice, n.ID)
	if err != nil || got.Title != "secret" {
		t.Errorf("alice's note was changed or removed: %+v (err %v)", got, err)
	}
}

func TestDB_SoftDelete_HidesTheNoteButKeepsTheRow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.user(t, "a@x.io")
	n := e.note(t, uid, "to delete")
	keep := e.note(t, uid, "keep")

	if err := e.notes.Delete(ctx, uid, n.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := e.notes.Get(ctx, uid, n.ID); !errors.Is(err, note.ErrNotFound) {
		t.Errorf("Get: err = %v, want ErrNotFound", err)
	}
	if _, err := e.notes.Update(ctx, uid, n.ID, note.Patch{Title: ptr("x")}); !errors.Is(err, note.ErrNotFound) {
		t.Errorf("Update: err = %v, want ErrNotFound", err)
	}
	if err := e.notes.Delete(ctx, uid, n.ID); !errors.Is(err, note.ErrNotFound) {
		t.Errorf("second Delete: err = %v, want ErrNotFound", err)
	}
	page, _ := e.notes.List(ctx, uid, note.ListFilter{})
	if len(page.Items) != 1 || page.Items[0].ID != keep.ID {
		t.Errorf("list must contain only the surviving note: %+v", page.Items)
	}

	var deletedAt *time.Time
	if err := e.tx.QueryRow(ctx, `SELECT deleted_at FROM notes WHERE id = $1`, n.ID).Scan(&deletedAt); err != nil {
		t.Fatalf("the row must still exist: %v", err)
	}
	if deletedAt == nil {
		t.Error("deleted_at must be set")
	}
}

func TestDB_SubjectOfAnotherUser_IsRejected(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	alice, bob := e.user(t, "alice@x.io"), e.user(t, "bob@x.io")
	alicesSubject, _ := e.subs.Create(ctx, alice, subject.Input{Title: "Algebra"})

	_, err := e.notes.Create(ctx, bob, note.Input{Title: "t", Content: doc("x"), SubjectID: &alicesSubject.ID})
	if !errors.Is(err, note.ErrInvalidSubject) {
		t.Errorf("Create with a foreign subject: err = %v, want ErrInvalidSubject", err)
	}

	mine := e.note(t, bob, "mine")
	_, err = e.notes.Update(ctx, bob, mine.ID, note.Patch{SetSubject: true, SubjectID: &alicesSubject.ID})
	if !errors.Is(err, note.ErrInvalidSubject) {
		t.Errorf("Update with a foreign subject: err = %v, want ErrInvalidSubject", err)
	}
	if got, _ := e.notes.Get(ctx, bob, mine.ID); got.SubjectID != nil {
		t.Error("the note must not be attached to the foreign subject")
	}

	// Свой предмет проходит.
	own, _ := e.subs.Create(ctx, bob, subject.Input{Title: "Own"})
	if _, err := e.notes.Update(ctx, bob, mine.ID, note.Patch{SetSubject: true, SubjectID: &own.ID}); err != nil {
		t.Errorf("own subject must be accepted: %v", err)
	}
}

func TestDB_Update_PartialSemanticsAndUpdatedAt(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.user(t, "a@x.io")
	s, _ := e.subs.Create(ctx, uid, subject.Input{Title: "S"})
	n, err := e.notes.Create(ctx, uid, note.Input{Title: "Old", Content: doc("old text"), SubjectID: &s.ID})
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Microsecond)
	e.setUpdatedAt(t, n.ID, past)

	// Только title: контент, предмет и закрепление остаются, updated_at двигается вперёд.
	got, err := e.notes.Update(ctx, uid, n.ID, note.Patch{Title: ptr("New")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "New" || got.SubjectID == nil || *got.SubjectID != s.ID || got.IsPinned {
		t.Errorf("title-only patch changed other fields: %+v", got.Summary)
	}
	if !got.UpdatedAt.After(past) {
		t.Errorf("updated_at must move forward: %v vs %v", got.UpdatedAt, past)
	}
	var text string
	_ = e.tx.QueryRow(ctx, `SELECT content_text FROM notes WHERE id = $1`, n.ID).Scan(&text)
	if text != "old text" {
		t.Errorf("content_text must be kept, got %q", text)
	}

	// Новый content пересобирает content_text.
	if _, err := e.notes.Update(ctx, uid, n.ID, note.Patch{Content: doc("new text")}); err != nil {
		t.Fatal(err)
	}
	_ = e.tx.QueryRow(ctx, `SELECT content_text FROM notes WHERE id = $1`, n.ID).Scan(&text)
	if text != "new text" {
		t.Errorf("content_text must follow content, got %q", text)
	}

	// subject: null очищает предмет, закрепление ставится независимо.
	got, _ = e.notes.Update(ctx, uid, n.ID, note.Patch{SetSubject: true, IsPinned: ptr(true)})
	if got.SubjectID != nil || !got.IsPinned || got.Title != "New" {
		t.Errorf("clearing the subject went wrong: %+v", got.Summary)
	}
}

func TestDB_List_KeysetPaginationWalksEveryNoteExactlyOnce(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.user(t, "a@x.io")
	base := time.Now().UTC().Truncate(time.Microsecond)

	// Пять заметок, две из них с одинаковым updated_at: порядок среди них решает id.
	offsets := []time.Duration{0, -1 * time.Hour, -2 * time.Hour, -2 * time.Hour, -3 * time.Hour}
	var all []note.Note
	for i, off := range offsets {
		n := e.note(t, uid, string(rune('a'+i)))
		e.setUpdatedAt(t, n.ID, base.Add(off))
		n.UpdatedAt = base.Add(off)
		all = append(all, n)
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].UpdatedAt.Equal(all[j].UpdatedAt) {
			return all[i].UpdatedAt.After(all[j].UpdatedAt)
		}
		return all[i].ID.String() > all[j].ID.String()
	})

	var walked []uuid.UUID
	cursor, pages := "", 0
	for {
		page, err := e.notes.List(ctx, uid, note.ListFilter{Limit: 2, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range page.Items {
			walked = append(walked, it.ID)
		}
		pages++
		if page.NextCursor == "" {
			break
		}
		if pages > 10 {
			t.Fatal("pagination does not terminate")
		}
		cursor = page.NextCursor
	}

	if pages != 3 || len(walked) != len(all) {
		t.Fatalf("pages = %d, notes = %d, want 3 pages and %d notes", pages, len(walked), len(all))
	}
	for i := range all {
		if walked[i] != all[i].ID {
			t.Fatalf("position %d: got %v, want %v (ties must be broken by id)", i, walked[i], all[i].ID)
		}
	}
}

func TestDB_List_Filters(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	alice, bob := e.user(t, "alice@x.io"), e.user(t, "bob@x.io")
	math, _ := e.subs.Create(ctx, alice, subject.Input{Title: "Math"})

	inMath, _ := e.notes.Create(ctx, alice, note.Input{Title: "in math", Content: doc("a"), SubjectID: &math.ID, IsPinned: true})
	loose := e.note(t, alice, "loose")
	bobs := e.note(t, bob, "bobs")

	var tagID, bobTagID uuid.UUID
	if err := e.tx.QueryRow(ctx, `INSERT INTO tags (user_id, name) VALUES ($1, 'exam') RETURNING id`, alice).Scan(&tagID); err != nil {
		t.Fatal(err)
	}
	// У Боба тег с тем же именем: фильтр Алисы не должен его цеплять.
	if err := e.tx.QueryRow(ctx, `INSERT INTO tags (user_id, name) VALUES ($1, 'exam') RETURNING id`, bob).Scan(&bobTagID); err != nil {
		t.Fatal(err)
	}
	e.tx.Exec(ctx, `INSERT INTO note_tags (note_id, tag_id) VALUES ($1, $2), ($3, $4)`, loose.ID, tagID, bobs.ID, bobTagID)

	ids := func(f note.ListFilter) []uuid.UUID {
		page, err := e.notes.List(ctx, alice, f)
		if err != nil {
			t.Fatal(err)
		}
		var out []uuid.UUID
		for _, it := range page.Items {
			out = append(out, it.ID)
		}
		return out
	}
	only := func(got []uuid.UUID, want uuid.UUID) bool { return len(got) == 1 && got[0] == want }

	if got := ids(note.ListFilter{SubjectID: &math.ID}); !only(got, inMath.ID) {
		t.Errorf("subject filter: %v", got)
	}
	if got := ids(note.ListFilter{Pinned: ptr(true)}); !only(got, inMath.ID) {
		t.Errorf("pinned=true filter: %v", got)
	}
	if got := ids(note.ListFilter{Pinned: ptr(false)}); !only(got, loose.ID) {
		t.Errorf("pinned=false filter: %v", got)
	}
	if got := ids(note.ListFilter{Tag: ptr("exam")}); !only(got, loose.ID) {
		t.Errorf("tag filter must match only alice's own tagged note: %v", got)
	}
	if got := ids(note.ListFilter{Tag: ptr("nope")}); len(got) != 0 {
		t.Errorf("unknown tag: %v", got)
	}
	if got := ids(note.ListFilter{}); len(got) != 2 {
		t.Errorf("no filters: want alice's 2 notes, got %d", len(got))
	}
}

func TestDB_TagsAreReturnedWithNotes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.user(t, "a@x.io")
	n := e.note(t, uid, "tagged")

	var t1, t2 uuid.UUID
	e.tx.QueryRow(ctx, `INSERT INTO tags (user_id, name) VALUES ($1, 'zeta') RETURNING id`, uid).Scan(&t1)
	e.tx.QueryRow(ctx, `INSERT INTO tags (user_id, name) VALUES ($1, 'alpha') RETURNING id`, uid).Scan(&t2)
	e.tx.Exec(ctx, `INSERT INTO note_tags (note_id, tag_id) VALUES ($1, $2), ($1, $3)`, n.ID, t1, t2)

	got, err := e.notes.Get(ctx, uid, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tags) != 2 || got.Tags[0].Name != "alpha" || got.Tags[1].Name != "zeta" {
		t.Errorf("tags must be returned sorted by name: %+v", got.Tags)
	}
	page, _ := e.notes.List(ctx, uid, note.ListFilter{})
	if len(page.Items) != 1 || len(page.Items[0].Tags) != 2 {
		t.Errorf("list items must carry tags too: %+v", page.Items)
	}
}

func (e env) noteWith(t *testing.T, uid uuid.UUID, title, text string) note.Note {
	t.Helper()
	n, err := e.notes.Create(context.Background(), uid, note.Input{Title: title, Content: docOf(paragraph(text))})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (e env) setText(t *testing.T, uid, id uuid.UUID, text string) {
	t.Helper()
	if _, err := e.notes.Update(context.Background(), uid, id, note.Patch{Content: docOf(paragraph(text))}); err != nil {
		t.Fatal(err)
	}
}

func (e env) rename(t *testing.T, uid, id uuid.UUID, title string) {
	t.Helper()
	if _, err := e.notes.Update(context.Background(), uid, id, note.Patch{Title: &title}); err != nil {
		t.Fatal(err)
	}
}

func (e env) backlinkTitles(t *testing.T, uid, id uuid.UUID) []string {
	t.Helper()
	items, err := e.notes.Backlinks(context.Background(), uid, id)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, it := range items {
		titles = append(titles, it.Title)
	}
	sort.Strings(titles)
	return titles
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestDB_Links_OutgoingAreResolvedByTitle(t *testing.T) {
	e := newEnv(t)
	uid := e.user(t, "a@x.io")
	algebra := e.noteWith(t, uid, "Algebra", "")
	calculus := e.noteWith(t, uid, "Calculus", "")

	src := e.noteWith(t, uid, "Notes", "see [[algebra]], [[Missing]] and [[Notes]]")

	if got := e.backlinkTitles(t, uid, algebra.ID); !equal(got, []string{"Notes"}) {
		t.Errorf("case-insensitive link must resolve: %v", got)
	}
	if got := e.backlinkTitles(t, uid, calculus.ID); len(got) != 0 {
		t.Errorf("unrelated note got a backlink: %v", got)
	}
	if got := e.backlinkTitles(t, uid, src.ID); len(got) != 0 {
		t.Errorf("a link to a missing note and a self link must not create edges: %v", got)
	}

	e.setText(t, uid, src.ID, "now about [[Calculus]]")
	if got := e.backlinkTitles(t, uid, algebra.ID); len(got) != 0 {
		t.Errorf("the old edge must be removed: %v", got)
	}
	if got := e.backlinkTitles(t, uid, calculus.ID); !equal(got, []string{"Notes"}) {
		t.Errorf("the new edge must appear: %v", got)
	}

	e.setText(t, uid, src.ID, "no links any more")
	if got := e.backlinkTitles(t, uid, calculus.ID); len(got) != 0 {
		t.Errorf("removing the link from the text must remove the edge: %v", got)
	}
}

func TestDB_Links_ForwardReferencesResolveWhenTheTargetAppears(t *testing.T) {
	e := newEnv(t)
	uid := e.user(t, "a@x.io")
	src := e.noteWith(t, uid, "Source", "waiting for [[Later]]")

	later := e.noteWith(t, uid, "Later", "")
	if got := e.backlinkTitles(t, uid, later.ID); !equal(got, []string{"Source"}) {
		t.Fatalf("a note created after the link must pick it up: %v", got)
	}

	e.rename(t, uid, later.ID, "Renamed")
	if got := e.backlinkTitles(t, uid, later.ID); len(got) != 0 {
		t.Errorf("after the rename the text no longer points here: %v", got)
	}

	e.rename(t, uid, later.ID, "later") // регистр не важен
	if got := e.backlinkTitles(t, uid, later.ID); !equal(got, []string{"Source"}) {
		t.Errorf("renaming back must restore the edge: %v", got)
	}
	_ = src
}

func TestDB_Links_NeverCrossUsers(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.user(t, "alice@x.io"), e.user(t, "bob@x.io")
	bobsAlgebra := e.noteWith(t, bob, "Algebra", "")

	alicesSource := e.noteWith(t, alice, "Mine", "[[Algebra]]")

	if got := e.backlinkTitles(t, bob, bobsAlgebra.ID); len(got) != 0 {
		t.Errorf("alice's link must not point at bob's note: %v", got)
	}
	g, _ := e.notes.Graph(context.Background(), alice)
	if len(g.Edges) != 0 {
		t.Errorf("alice's graph must have no edges yet: %+v", g.Edges)
	}

	// Когда у самой Алисы появляется «Algebra», ссылка находит именно её заметку.
	alicesAlgebra := e.noteWith(t, alice, "Algebra", "")
	if got := e.backlinkTitles(t, alice, alicesAlgebra.ID); !equal(got, []string{"Mine"}) {
		t.Errorf("link must resolve to alice's own note: %v", got)
	}
	if got := e.backlinkTitles(t, bob, bobsAlgebra.ID); len(got) != 0 {
		t.Errorf("bob's note must stay unlinked: %v", got)
	}
	_ = alicesSource
}

func TestDB_Links_DuplicateTitles_LinkToEveryMatch(t *testing.T) {
	e := newEnv(t)
	uid := e.user(t, "a@x.io")
	first, second := e.noteWith(t, uid, "Twin", "one"), e.noteWith(t, uid, "twin", "two")

	e.noteWith(t, uid, "Source", "[[Twin]]")

	for _, n := range []note.Note{first, second} {
		if got := e.backlinkTitles(t, uid, n.ID); !equal(got, []string{"Source"}) {
			t.Errorf("note %q must be linked: %v", n.Title, got)
		}
	}
}

func TestDB_Links_SoftDeletedNotesDisappearFromGraphAndBacklinks(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.user(t, "a@x.io")
	b := e.noteWith(t, uid, "B", "")
	a := e.noteWith(t, uid, "A", "[[B]]")

	g, _ := e.notes.Graph(ctx, uid)
	if len(g.Nodes) != 2 || len(g.Edges) != 1 || g.Edges[0].Source != a.ID || g.Edges[0].Target != b.ID {
		t.Fatalf("graph before deletion: %+v", g)
	}

	if err := e.notes.Delete(ctx, uid, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.notes.Backlinks(ctx, uid, b.ID); !errors.Is(err, note.ErrNotFound) {
		t.Errorf("backlinks of a deleted note: err = %v, want ErrNotFound", err)
	}
	g, _ = e.notes.Graph(ctx, uid)
	if len(g.Nodes) != 1 || g.Nodes[0].ID != a.ID || len(g.Edges) != 0 {
		t.Errorf("a deleted target must leave the graph with its edges: %+v", g)
	}

	// Новая заметка с тем же названием подхватывает старую ссылку.
	b2 := e.noteWith(t, uid, "B", "")
	if got := e.backlinkTitles(t, uid, b2.ID); !equal(got, []string{"A"}) {
		t.Errorf("a re-created target must be linked: %v", got)
	}

	// Удалённый источник не показывается в обратных ссылках.
	if err := e.notes.Delete(ctx, uid, a.ID); err != nil {
		t.Fatal(err)
	}
	if got := e.backlinkTitles(t, uid, b2.ID); len(got) != 0 {
		t.Errorf("a deleted source must not be listed: %v", got)
	}
}

func TestDB_Backlinks_ForeignNote_IsNotFound(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.user(t, "alice@x.io"), e.user(t, "bob@x.io")
	alices := e.noteWith(t, alice, "Private", "")

	if _, err := e.notes.Backlinks(context.Background(), bob, alices.ID); !errors.Is(err, note.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestDB_Graph_ContainsOnlyOwnNotes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	alice, bob := e.user(t, "alice@x.io"), e.user(t, "bob@x.io")
	e.noteWith(t, alice, "A1", "[[A2]]")
	e.noteWith(t, alice, "A2", "")
	e.noteWith(t, bob, "B1", "[[B2]]")
	e.noteWith(t, bob, "B2", "")

	g, err := e.notes.Graph(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, n := range g.Nodes {
		titles = append(titles, n.Title)
	}
	sort.Strings(titles)
	if !equal(titles, []string{"A1", "A2"}) || len(g.Edges) != 1 {
		t.Errorf("alice's graph: nodes %v, edges %d", titles, len(g.Edges))
	}
}

// В таблице note_links не должно быть рёбер между заметками разных пользователей, даже таких,
// которые запросы чтения всё равно спрятали бы.
func (e env) crossUserEdges(t *testing.T, users ...uuid.UUID) int {
	t.Helper()
	var n int
	// Только рёбра пользователей этого теста: результат не зависит от остального содержимого базы.
	err := e.tx.QueryRow(context.Background(), `
		SELECT count(*) FROM note_links l
		JOIN notes s ON s.id = l.source_note_id
		JOIN notes t ON t.id = l.target_note_id
		WHERE s.user_id <> t.user_id AND (s.user_id = ANY($1) OR t.user_id = ANY($1))`, users).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDB_Links_NothingIsStoredAcrossUsers(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.user(t, "alice@x.io"), e.user(t, "bob@x.io")

	// Исходящие: у Боба уже есть «Algebra», ссылается Алиса.
	e.noteWith(t, bob, "Algebra", "")
	e.noteWith(t, alice, "Mine", "[[Algebra]]")
	if n := e.crossUserEdges(t, alice, bob); n != 0 {
		t.Errorf("outgoing resolution stored %d cross-user edge(s)", n)
	}

	// Входящие: Алиса сослалась на «Zeta» раньше, чем Боб создал заметку с таким названием.
	e.noteWith(t, alice, "Waiting", "[[Zeta]]")
	zeta := e.noteWith(t, bob, "Zeta", "")
	e.rename(t, bob, zeta.ID, "Zeta")
	if n := e.crossUserEdges(t, alice, bob); n != 0 {
		t.Errorf("incoming resolution stored %d cross-user edge(s)", n)
	}
}

func (e env) mentionKeys(t *testing.T, noteID uuid.UUID) []string {
	t.Helper()
	rows, err := e.tx.Query(context.Background(), `SELECT title_key FROM note_mentions WHERE note_id = $1 ORDER BY title_key`, noteID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
	}
	return keys
}

func TestDB_Mentions_AreStoredLowercasedAndDeduplicated(t *testing.T) {
	e := newEnv(t)
	uid := e.user(t, "a@x.io")

	n := e.noteWith(t, uid, "Source", "see [[Algebra]], [[algebra]] and [[LOGIC|shown]]")

	if got := e.mentionKeys(t, n.ID); !equal(got, []string{"algebra", "logic"}) {
		t.Errorf("mention keys = %v", got)
	}
}

func TestDB_Mentions_FollowTheText(t *testing.T) {
	e := newEnv(t)
	uid := e.user(t, "a@x.io")
	n := e.noteWith(t, uid, "Source", "[[Ghost]] and [[Keep]]")

	e.setText(t, uid, n.ID, "only [[Keep]] now")

	if got := e.mentionKeys(t, n.ID); !equal(got, []string{"keep"}) {
		t.Errorf("a removed link must stop being a mention: %v", got)
	}
	ghost := e.noteWith(t, uid, "Ghost", "")
	if got := e.backlinkTitles(t, uid, ghost.ID); len(got) != 0 {
		t.Errorf("a link that was deleted from the text must not come back when a note with that title appears: %v", got)
	}
}

func TestDB_Links_AliasedForwardReferenceResolvesWhenTheTargetAppears(t *testing.T) {
	e := newEnv(t)
	uid := e.user(t, "a@x.io")
	e.noteWith(t, uid, "Source", "waiting for [[Later|a nicer label]]")

	later := e.noteWith(t, uid, "Later", "")

	if got := e.backlinkTitles(t, uid, later.ID); !equal(got, []string{"Source"}) {
		t.Errorf("[[Title|alias]] must resolve to the note called Title, got %v", got)
	}
}

func TestDB_Links_DeletedSourceNeverLinksToALaterNote(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.user(t, "a@x.io")
	src := e.noteWith(t, uid, "Source", "[[Later]]")
	if err := e.notes.Delete(ctx, uid, src.ID); err != nil {
		t.Fatal(err)
	}

	later := e.noteWith(t, uid, "Later", "")

	if got := e.backlinkTitles(t, uid, later.ID); len(got) != 0 {
		t.Errorf("a soft-deleted note must not link to anything: %v", got)
	}
}
