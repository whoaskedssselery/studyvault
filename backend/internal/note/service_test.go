package note_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/note"
	"github.com/whoaskedssselery/studyvault/backend/internal/search"
)

type fakeStore struct {
	noteOwned  bool // чем ответит NoteOwnedByUser
	deleteRows int64
	linkErr    error
	outgoing   []db.ReplaceOutgoingLinksParams
	mentions   []db.ReplaceNoteMentionsParams
	incoming   []db.ReplaceIncomingLinksParams
	backlinks  []db.ListBacklinksRow
	nodes      []db.ListGraphNodesRow
	edges      []db.NoteLink

	owned     bool // чем ответит SubjectOwnedByUser
	subjectQs int  // сколько раз проверяли владельца предмета

	rows       []db.ListNotesRow
	listParams []db.ListNotesParams
	tags       []db.ListNoteTagsRow

	created db.CreateNoteParams
	updated db.UpdateNoteParams
	err     error
}

func (f *fakeStore) SubjectOwnedByUser(context.Context, db.SubjectOwnedByUserParams) (bool, error) {
	f.subjectQs++
	return f.owned, f.err
}

func (f *fakeStore) CreateNote(_ context.Context, a db.CreateNoteParams) (db.Note, error) {
	f.created = a
	return db.Note{ID: uuid.New(), UserID: a.UserID, SubjectID: a.SubjectID, Title: a.Title, Content: a.Content, ContentText: a.ContentText}, f.err
}

func (f *fakeStore) GetNote(context.Context, db.GetNoteParams) (db.Note, error) {
	return db.Note{ID: uuid.New()}, f.err
}

func (f *fakeStore) UpdateNote(_ context.Context, a db.UpdateNoteParams) (db.Note, error) {
	f.updated = a
	return db.Note{ID: a.ID, UserID: a.UserID, Title: a.Title.String, ContentText: a.ContentText.String}, f.err
}

func (f *fakeStore) SoftDeleteNote(context.Context, db.SoftDeleteNoteParams) (int64, error) {
	return f.deleteRows, f.err
}

func (f *fakeStore) ListNotes(_ context.Context, a db.ListNotesParams) ([]db.ListNotesRow, error) {
	f.listParams = append(f.listParams, a)
	n := min(int(a.PageSize), len(f.rows))
	return f.rows[:n], f.err
}

func (f *fakeStore) ListNoteTags(context.Context, []uuid.UUID) ([]db.ListNoteTagsRow, error) {
	return f.tags, f.err
}

func doc(text string) []byte {
	return []byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"` + text + `"}]}]}`)
}

func TestCreate_StoresExtractedText(t *testing.T) {
	f := &fakeStore{}
	_, err := note.NewService(f).Create(context.Background(), uuid.New(), note.Input{Title: "T", Content: doc("hello")})
	if err != nil {
		t.Fatal(err)
	}
	if f.created.ContentText != "hello" {
		t.Errorf("content_text = %q, want %q", f.created.ContentText, "hello")
	}
	if f.subjectQs != 0 {
		t.Error("no subject given, so no ownership check is expected")
	}
}

func TestCreate_SubjectOfSomeoneElse_IsRejectedBeforeInsert(t *testing.T) {
	f := &fakeStore{owned: false}
	sid := uuid.New()

	_, err := note.NewService(f).Create(context.Background(), uuid.New(), note.Input{Title: "T", Content: doc("x"), SubjectID: &sid})

	if !errors.Is(err, note.ErrInvalidSubject) {
		t.Fatalf("err = %v, want ErrInvalidSubject", err)
	}
	if f.created.Title != "" {
		t.Error("the note must not be inserted")
	}
}

func TestUpdate_MapsPatch(t *testing.T) {
	f := &fakeStore{owned: true}
	svc := note.NewService(f)
	uid, id, sid := uuid.New(), uuid.New(), uuid.New()
	title, pinned := "New", true

	_, err := svc.Update(context.Background(), uid, id, note.Patch{
		Title: &title, Content: doc("fresh"), SetSubject: true, SubjectID: &sid, IsPinned: &pinned,
	})
	if err != nil {
		t.Fatal(err)
	}
	u := f.updated
	if u.ID != id || u.UserID != uid {
		t.Errorf("update must be scoped by id and user: %+v", u)
	}
	if u.ContentText.String != "fresh" || !u.ContentText.Valid {
		t.Errorf("content_text must follow content: %+v", u.ContentText)
	}
	if !u.SetSubject || u.SubjectID.UUID != sid || !u.IsPinned.Bool || u.Title.String != "New" {
		t.Errorf("fields not mapped: %+v", u)
	}

	// Без content: content и content_text не трогаем.
	if _, err := svc.Update(context.Background(), uid, id, note.Patch{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if f.updated.Content != nil || f.updated.ContentText.Valid {
		t.Errorf("content must stay untouched: %+v", f.updated)
	}
	if f.updated.SetSubject {
		t.Error("subject was not sent and must stay untouched")
	}
}

func TestUpdate_ClearSubject_SkipsOwnershipCheck(t *testing.T) {
	f := &fakeStore{}
	_, err := note.NewService(f).Update(context.Background(), uuid.New(), uuid.New(), note.Patch{SetSubject: true})
	if err != nil {
		t.Fatal(err)
	}
	if !f.updated.SetSubject || f.updated.SubjectID.Valid || f.subjectQs != 0 {
		t.Errorf("clearing must set NULL without lookups: %+v (lookups: %d)", f.updated, f.subjectQs)
	}
}

func TestUpdate_ForeignSubject_IsRejected(t *testing.T) {
	f := &fakeStore{owned: false}
	sid := uuid.New()

	_, err := note.NewService(f).Update(context.Background(), uuid.New(), uuid.New(), note.Patch{SetSubject: true, SubjectID: &sid})

	if !errors.Is(err, note.ErrInvalidSubject) {
		t.Fatalf("err = %v, want ErrInvalidSubject", err)
	}
}

func TestStoreNoRows_BecomesErrNotFound(t *testing.T) {
	f := &fakeStore{err: pgx.ErrNoRows}
	svc := note.NewService(f)
	ctx := context.Background()

	if _, err := svc.Get(ctx, uuid.New(), uuid.New()); !errors.Is(err, note.ErrNotFound) {
		t.Errorf("Get: err = %v", err)
	}
	if _, err := svc.Update(ctx, uuid.New(), uuid.New(), note.Patch{}); !errors.Is(err, note.ErrNotFound) {
		t.Errorf("Update: err = %v", err)
	}
}

func TestDelete_ZeroRows_ReturnsErrNotFound(t *testing.T) {
	if err := note.NewService(&fakeStore{}).Delete(context.Background(), uuid.New(), uuid.New()); !errors.Is(err, note.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func rows(n int) []db.ListNotesRow {
	base := time.Date(2026, 10, 1, 12, 0, 0, 123456000, time.UTC)
	out := make([]db.ListNotesRow, n)
	for i := range out {
		out[i] = db.ListNotesRow{ID: uuid.New(), Title: "n", UpdatedAt: base.Add(-time.Duration(i) * time.Minute)}
	}
	return out
}

func TestList_FetchesOneExtraRowToKnowIfThereIsANextPage(t *testing.T) {
	f := &fakeStore{rows: rows(5)}
	svc := note.NewService(f)

	page, err := svc.List(context.Background(), uuid.New(), note.ListFilter{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if f.listParams[0].PageSize != 4 {
		t.Errorf("page size asked from the store = %d, want limit+1 = 4", f.listParams[0].PageSize)
	}
	if len(page.Items) != 3 || page.NextCursor == "" {
		t.Fatalf("items = %d, cursor = %q: want 3 items and a cursor", len(page.Items), page.NextCursor)
	}
	if f.listParams[0].CursorUpdatedAt != nil {
		t.Error("first page has no cursor")
	}

	// Курсор указывает на последнюю отданную заметку; он же уходит в запрос следующей страницы.
	last := page.Items[2]
	if _, err := svc.List(context.Background(), uuid.New(), note.ListFilter{Limit: 3, Cursor: page.NextCursor}); err != nil {
		t.Fatal(err)
	}
	p := f.listParams[1]
	if p.CursorUpdatedAt == nil || !p.CursorUpdatedAt.Equal(last.UpdatedAt) || p.CursorID.UUID != last.ID {
		t.Errorf("cursor does not point at the last item: %+v vs %+v", p, last)
	}
}

func TestList_LastPage_HasNoCursor(t *testing.T) {
	f := &fakeStore{rows: rows(3)}

	page, _ := note.NewService(f).List(context.Background(), uuid.New(), note.ListFilter{Limit: 3})

	if len(page.Items) != 3 || page.NextCursor != "" {
		t.Errorf("items = %d, cursor = %q: want 3 items and no cursor", len(page.Items), page.NextCursor)
	}
}

func TestList_InvalidCursor_ReturnsErrInvalidCursor(t *testing.T) {
	svc := note.NewService(&fakeStore{})

	for _, c := range []string{"garbage", "!!!", "e30", "eyJ0IjoiIiwiaWQiOiIifQ"} {
		if _, err := svc.List(context.Background(), uuid.New(), note.ListFilter{Cursor: c}); !errors.Is(err, note.ErrInvalidCursor) {
			t.Errorf("cursor %q: err = %v, want ErrInvalidCursor", c, err)
		}
	}
}

func TestList_LimitDefaultsAndClamp(t *testing.T) {
	f := &fakeStore{}
	svc := note.NewService(f)

	for _, limit := range []int{0, -5, 1000} {
		_, _ = svc.List(context.Background(), uuid.New(), note.ListFilter{Limit: limit})
	}
	got := []int32{f.listParams[0].PageSize, f.listParams[1].PageSize, f.listParams[2].PageSize}
	want := []int32{31, 31, 101}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("limit case %d: page size = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestList_AttachesTagsToTheirNotes(t *testing.T) {
	f := &fakeStore{rows: rows(2)}
	f.tags = []db.ListNoteTagsRow{
		{NoteID: f.rows[1].ID, TagID: uuid.New(), Name: "exam"},
		{NoteID: f.rows[1].ID, TagID: uuid.New(), Name: "math"},
	}

	page, _ := note.NewService(f).List(context.Background(), uuid.New(), note.ListFilter{Limit: 10})

	if len(page.Items[0].Tags) != 0 || page.Items[0].Tags == nil {
		t.Errorf("a note without tags must have an empty non-nil slice: %#v", page.Items[0].Tags)
	}
	if len(page.Items[1].Tags) != 2 || page.Items[1].Tags[0].Name != "exam" {
		t.Errorf("tags not attached: %#v", page.Items[1].Tags)
	}
}

func (f *fakeStore) NoteOwnedByUser(context.Context, db.NoteOwnedByUserParams) (bool, error) {
	return f.noteOwned, f.err
}

func (f *fakeStore) ReplaceNoteMentions(_ context.Context, a db.ReplaceNoteMentionsParams) error {
	f.mentions = append(f.mentions, a)
	return f.linkErr
}

func (f *fakeStore) ReplaceOutgoingLinks(_ context.Context, a db.ReplaceOutgoingLinksParams) error {
	f.outgoing = append(f.outgoing, a)
	return f.linkErr
}

func (f *fakeStore) ReplaceIncomingLinks(_ context.Context, a db.ReplaceIncomingLinksParams) error {
	f.incoming = append(f.incoming, a)
	return f.linkErr
}

func (f *fakeStore) ListBacklinks(context.Context, db.ListBacklinksParams) ([]db.ListBacklinksRow, error) {
	return f.backlinks, f.err
}

func (f *fakeStore) ListGraphNodes(context.Context, uuid.UUID) ([]db.ListGraphNodesRow, error) {
	return f.nodes, f.err
}

func (f *fakeStore) ListGraphEdges(context.Context, uuid.UUID) ([]db.NoteLink, error) {
	return f.edges, f.err
}

// countingTx запускает fn сразу на том же хранилище и считает, сколько транзакций открыли.
func countingTx(f *fakeStore, runs *int) note.Option {
	return note.WithTx(func(_ context.Context, fn func(note.Store) error) error {
		*runs++
		return fn(f)
	})
}

func TestCreate_RebuildsBothDirectionsOfLinksInOneTransaction(t *testing.T) {
	f := &fakeStore{}
	runs := 0
	svc := note.NewService(f, countingTx(f, &runs))
	uid := uuid.New()

	n, err := svc.Create(context.Background(), uid, note.Input{Title: "Calculus", Content: docOf(paragraph("[[Algebra]] and [[algebra]] and [[Logic]]"))})
	if err != nil {
		t.Fatal(err)
	}

	if runs != 1 {
		t.Errorf("transactions opened = %d, want exactly 1", runs)
	}
	if len(f.mentions) != 1 || f.mentions[0].NoteID != n.ID || len(f.mentions[0].Titles) != 2 ||
		f.mentions[0].Titles[0] != "Algebra" || f.mentions[0].Titles[1] != "Logic" {
		t.Errorf("the titles the note mentions must be recorded, so a note created later finds them by index: %+v", f.mentions)
	}
	if len(f.outgoing) != 1 || f.outgoing[0].SourceID != n.ID || f.outgoing[0].UserID != uid ||
		len(f.outgoing[0].Titles) != 2 || f.outgoing[0].Titles[0] != "Algebra" || f.outgoing[0].Titles[1] != "Logic" {
		t.Errorf("outgoing: %+v", f.outgoing)
	}
	if len(f.incoming) != 1 || f.incoming[0].TargetID != n.ID || f.incoming[0].Title != "Calculus" || f.incoming[0].UserID != uid {
		t.Errorf("incoming (notes that already mention this title): %+v", f.incoming)
	}
}

func TestCreate_NoLinksInText_StillSendsAnEmptyNonNilTitleList(t *testing.T) {
	f := &fakeStore{}
	_, _ = note.NewService(f).Create(context.Background(), uuid.New(), note.Input{Title: "T", Content: docOf(paragraph("plain"))})

	if len(f.outgoing) != 1 || f.outgoing[0].Titles == nil || len(f.outgoing[0].Titles) != 0 {
		t.Errorf("an empty list must be sent as an empty array, not NULL: %#v", f.outgoing)
	}
}

func TestUpdate_LinkRebuildDependsOnWhatChanged(t *testing.T) {
	uid, id := uuid.New(), uuid.New()
	title, pinned := "Renamed", true
	cases := map[string]struct {
		patch           note.Patch
		wantOut, wantIn int
	}{
		"content only": {note.Patch{Content: docOf(paragraph("[[X]]"))}, 1, 0},
		"title only":   {note.Patch{Title: &title}, 0, 1},
		"both":         {note.Patch{Title: &title, Content: docOf(paragraph("[[X]]"))}, 1, 1},
		"pin only":     {note.Patch{IsPinned: &pinned}, 0, 0},
		"subject only": {note.Patch{SetSubject: true}, 0, 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeStore{}
			if _, err := note.NewService(f).Update(context.Background(), uid, id, c.patch); err != nil {
				t.Fatal(err)
			}
			if len(f.mentions) != c.wantOut {
				t.Errorf("mentions are rewritten exactly when the text changes: %d times, want %d", len(f.mentions), c.wantOut)
			}
			if len(f.outgoing) != c.wantOut || len(f.incoming) != c.wantIn {
				t.Errorf("outgoing rebuilds = %d (want %d), incoming = %d (want %d)", len(f.outgoing), c.wantOut, len(f.incoming), c.wantIn)
			}
		})
	}
}

func TestUpdate_RenameUsesTheNewTitleForIncomingLinks(t *testing.T) {
	f := &fakeStore{}
	title := "Brand new"
	_, _ = note.NewService(f).Update(context.Background(), uuid.New(), uuid.New(), note.Patch{Title: &title})

	// Фейк возвращает заметку без названия, поэтому важно, что берётся именно переданное.
	if len(f.incoming) != 1 || f.incoming[0].Title != "Brand new" {
		t.Errorf("incoming: %+v", f.incoming)
	}
}

func TestLinkRebuildFailure_FailsTheWholeOperation(t *testing.T) {
	boom := errors.New("links broke")
	f := &fakeStore{linkErr: boom}
	svc := note.NewService(f)

	if _, err := svc.Create(context.Background(), uuid.New(), note.Input{Title: "T", Content: docOf(paragraph("x"))}); !errors.Is(err, boom) {
		t.Errorf("Create: err = %v, want the link error (so the transaction rolls back)", err)
	}
	title := "x"
	if _, err := svc.Update(context.Background(), uuid.New(), uuid.New(), note.Patch{Title: &title}); !errors.Is(err, boom) {
		t.Errorf("Update: err = %v, want the link error", err)
	}
}

func TestBacklinks_NoteNotOwned_ReturnsErrNotFound(t *testing.T) {
	_, err := note.NewService(&fakeStore{noteOwned: false}).Backlinks(context.Background(), uuid.New(), uuid.New())

	if !errors.Is(err, note.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestBacklinks_ReturnsSummariesWithTags(t *testing.T) {
	f := &fakeStore{noteOwned: true, backlinks: []db.ListBacklinksRow{{ID: uuid.New(), Title: "A"}, {ID: uuid.New(), Title: "B"}}}
	f.tags = []db.ListNoteTagsRow{{NoteID: f.backlinks[1].ID, TagID: uuid.New(), Name: "exam"}}

	got, err := note.NewService(f).Backlinks(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Tags == nil || len(got[0].Tags) != 0 || len(got[1].Tags) != 1 {
		t.Errorf("got %+v", got)
	}
}

func TestGraph_EmptyIsNonNil(t *testing.T) {
	g, err := note.NewService(&fakeStore{}).Graph(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if g.Nodes == nil || g.Edges == nil {
		t.Errorf("empty graph must hold empty slices, not nil: %+v", g)
	}
}

func TestGraph_MapsRows(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	f := &fakeStore{
		nodes: []db.ListGraphNodesRow{{ID: a, Title: "A"}, {ID: b, Title: "B"}},
		edges: []db.NoteLink{{SourceNoteID: a, TargetNoteID: b}},
	}

	g, _ := note.NewService(f).Graph(context.Background(), uuid.New())

	if len(g.Nodes) != 2 || g.Nodes[1].Title != "B" || len(g.Edges) != 1 || g.Edges[0].Source != a || g.Edges[0].Target != b {
		t.Errorf("graph: %+v", g)
	}
}

type fakeIndexer struct {
	events  []string
	docs    []search.Doc
	removed []uuid.UUID
	err     error
}

func (f *fakeIndexer) Upsert(_ context.Context, d search.Doc) error {
	f.events = append(f.events, "upsert")
	f.docs = append(f.docs, d)
	return f.err
}

func (f *fakeIndexer) Remove(_ context.Context, id uuid.UUID) error {
	f.events = append(f.events, "remove")
	f.removed = append(f.removed, id)
	return f.err
}

// orderedTx запоминает момент фиксации транзакции в том же журнале, что и индексатор.
func orderedTx(f *fakeStore, idx *fakeIndexer) note.Option {
	return note.WithTx(func(_ context.Context, fn func(note.Store) error) error {
		if err := fn(f); err != nil {
			idx.events = append(idx.events, "rollback")
			return err
		}
		idx.events = append(idx.events, "commit")
		return nil
	})
}

func TestCreate_IndexesAfterTheTransactionCommits(t *testing.T) {
	f, idx := &fakeStore{}, &fakeIndexer{}
	uid := uuid.New()
	svc := note.NewService(f, orderedTx(f, idx), note.WithIndexer(idx))

	n, err := svc.Create(context.Background(), uid, note.Input{Title: "Algebra", Content: doc("groups")})
	if err != nil {
		t.Fatal(err)
	}

	if fmt.Sprint(idx.events) != "[commit upsert]" {
		t.Fatalf("events = %v, want the index updated only after the commit", idx.events)
	}
	d := idx.docs[0]
	if d.ID != n.ID || d.UserID != uid || d.Title != "Algebra" || d.ContentText != "groups" {
		t.Errorf("indexed doc: %+v", d)
	}
}

func TestCreate_RolledBack_IsNotIndexed(t *testing.T) {
	f, idx := &fakeStore{linkErr: errors.New("links broke")}, &fakeIndexer{}
	svc := note.NewService(f, orderedTx(f, idx), note.WithIndexer(idx))

	if _, err := svc.Create(context.Background(), uuid.New(), note.Input{Title: "T", Content: doc("x")}); err == nil {
		t.Fatal("expected the link error")
	}

	if fmt.Sprint(idx.events) != "[rollback]" {
		t.Errorf("events = %v: a note that was rolled back must never reach the index", idx.events)
	}
}

func TestUpdate_ReindexesOnlyWhenTitleOrTextChange(t *testing.T) {
	title, pinned := "New title", true
	cases := map[string]struct {
		patch note.Patch
		want  int
	}{
		"title":   {note.Patch{Title: &title}, 1},
		"content": {note.Patch{Content: doc("fresh text")}, 1},
		"both":    {note.Patch{Title: &title, Content: doc("fresh text")}, 1},
		"pin":     {note.Patch{IsPinned: &pinned}, 0},
		"subject": {note.Patch{SetSubject: true}, 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f, idx := &fakeStore{}, &fakeIndexer{}
			uid, id := uuid.New(), uuid.New()

			if _, err := note.NewService(f, note.WithIndexer(idx)).Update(context.Background(), uid, id, c.patch); err != nil {
				t.Fatal(err)
			}
			if len(idx.docs) != c.want {
				t.Fatalf("reindexed %d time(s), want %d", len(idx.docs), c.want)
			}
			if c.want == 1 && (idx.docs[0].ID != id || idx.docs[0].UserID != uid) {
				t.Errorf("doc: %+v", idx.docs[0])
			}
		})
	}
}

func TestUpdate_NotFound_IsNotIndexed(t *testing.T) {
	f, idx := &fakeStore{err: pgx.ErrNoRows}, &fakeIndexer{}
	title := "x"

	_, err := note.NewService(f, note.WithIndexer(idx)).Update(context.Background(), uuid.New(), uuid.New(), note.Patch{Title: &title})

	if !errors.Is(err, note.ErrNotFound) || len(idx.events) != 0 {
		t.Errorf("err = %v, index events = %v", err, idx.events)
	}
}

func TestDelete_RemovesFromTheIndex(t *testing.T) {
	idx := &fakeIndexer{}
	id := uuid.New()
	f := &fakeStore{deleteRows: 1}

	if err := note.NewService(f, note.WithIndexer(idx)).Delete(context.Background(), uuid.New(), id); err != nil {
		t.Fatal(err)
	}
	if len(idx.removed) != 1 || idx.removed[0] != id {
		t.Errorf("removed = %v, want [%v]", idx.removed, id)
	}

	idx2 := &fakeIndexer{}
	f.deleteRows = 0
	if err := note.NewService(f, note.WithIndexer(idx2)).Delete(context.Background(), uuid.New(), id); !errors.Is(err, note.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if len(idx2.events) != 0 {
		t.Errorf("deleting a missing note must not touch the index: %v", idx2.events)
	}
}

func TestIndexerFailure_NeverFailsTheOperation(t *testing.T) {
	f, idx := &fakeStore{deleteRows: 1}, &fakeIndexer{err: errors.New("meilisearch is down")}
	svc := note.NewService(f, note.WithIndexer(idx))
	ctx, uid := context.Background(), uuid.New()
	title := "x"

	if _, err := svc.Create(ctx, uid, note.Input{Title: "T", Content: doc("x")}); err != nil {
		t.Errorf("Create must succeed when only the index is down: %v", err)
	}
	if _, err := svc.Update(ctx, uid, uuid.New(), note.Patch{Title: &title}); err != nil {
		t.Errorf("Update must succeed when only the index is down: %v", err)
	}
	if err := svc.Delete(ctx, uid, uuid.New()); err != nil {
		t.Errorf("Delete must succeed when only the index is down: %v", err)
	}
	if len(idx.events) != 3 {
		t.Errorf("the index must have been attempted each time: %v", idx.events)
	}
}
