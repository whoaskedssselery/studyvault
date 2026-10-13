package note_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/note"
)

type fakeStore struct {
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
	return db.Note{ID: uuid.New(), UserID: a.UserID, SubjectID: a.SubjectID, Title: a.Title, Content: a.Content}, f.err
}

func (f *fakeStore) GetNote(context.Context, db.GetNoteParams) (db.Note, error) {
	return db.Note{ID: uuid.New()}, f.err
}

func (f *fakeStore) UpdateNote(_ context.Context, a db.UpdateNoteParams) (db.Note, error) {
	f.updated = a
	return db.Note{ID: a.ID}, f.err
}

func (f *fakeStore) SoftDeleteNote(context.Context, db.SoftDeleteNoteParams) (int64, error) {
	return 0, f.err
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
