package tag_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/note"
	"github.com/whoaskedssselery/studyvault/backend/internal/tag"
)

// Интеграционные тесты идут на настоящем Postgres внутри транзакции, которая откатывается в конце.
// Без DATABASE_URL они пропускаются.
type env struct {
	tx    pgx.Tx
	tags  *tag.Service
	notes *note.Service
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
	return env{tx: tx, tags: tag.NewService(q), notes: note.NewService(q)}
}

func (e env) user(t *testing.T, email string) uuid.UUID {
	t.Helper()
	u, err := db.New(e.tx).CreateUser(context.Background(), db.CreateUserParams{Email: email, PasswordHash: "x", DisplayName: email})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func (e env) note(t *testing.T, uid uuid.UUID) note.Note {
	t.Helper()
	n, err := e.notes.Create(context.Background(), uid, note.Input{Title: "n", Content: []byte(`{"type":"doc"}`)})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (e env) tag(t *testing.T, uid uuid.UUID, name string) db.Tag {
	t.Helper()
	tg, err := e.tags.Create(context.Background(), uid, name)
	if err != nil {
		t.Fatal(err)
	}
	return tg
}

func (e env) noteTagNames(t *testing.T, uid, noteID uuid.UUID) []string {
	t.Helper()
	n, err := e.notes.Get(context.Background(), uid, noteID)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tg := range n.Tags {
		names = append(names, tg.Name)
	}
	return names
}

func same(got, want []string) bool {
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

func TestDB_Tags_ArePerUser(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	alice, bob := e.user(t, "alice@x.io"), e.user(t, "bob@x.io")
	exam := e.tag(t, alice, "exam")

	// То же имя у другого пользователя допустимо, у того же самого нет.
	if _, err := e.tags.Create(ctx, bob, "exam"); err != nil {
		t.Errorf("bob may have his own 'exam': %v", err)
	}
	// Нарушение уникальности ломает всю транзакцию Postgres, поэтому пробуем его в savepoint.
	sp, err := e.tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tag.NewService(db.New(sp)).Create(ctx, alice, "exam"); !errors.Is(err, tag.ErrNameTaken) {
		t.Errorf("duplicate for the same user: err = %v, want ErrNameTaken", err)
	}
	_ = sp.Rollback(ctx)

	if err := e.tags.Delete(ctx, bob, exam.ID); !errors.Is(err, tag.ErrNotFound) {
		t.Errorf("bob deleting alice's tag: err = %v, want ErrNotFound", err)
	}
	list, _ := e.tags.List(ctx, alice)
	if len(list) != 1 || list[0].ID != exam.ID {
		t.Errorf("alice's tag must survive: %+v", list)
	}
}

func TestDB_List_SortedByName(t *testing.T) {
	e := newEnv(t)
	uid := e.user(t, "a@x.io")
	for _, n := range []string{"zeta", "alpha", "mid"} {
		e.tag(t, uid, n)
	}

	list, _ := e.tags.List(context.Background(), uid)

	got := []string{list[0].Name, list[1].Name, list[2].Name}
	if !same(got, []string{"alpha", "mid", "zeta"}) {
		t.Errorf("order = %v", got)
	}
}

func TestDB_SetNoteTags_ReplacesTheWholeSet(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.user(t, "a@x.io")
	n := e.note(t, uid)
	a, b, c := e.tag(t, uid, "a"), e.tag(t, uid, "b"), e.tag(t, uid, "c")

	if err := e.tags.SetNoteTags(ctx, uid, n.ID, []uuid.UUID{a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	if got := e.noteTagNames(t, uid, n.ID); !same(got, []string{"a", "b"}) {
		t.Fatalf("after first set: %v", got)
	}

	// {a,b} -> {b,c}: a снимается, c добавляется, b остаётся; повтор безвреден.
	for i := 0; i < 2; i++ {
		if err := e.tags.SetNoteTags(ctx, uid, n.ID, []uuid.UUID{b.ID, c.ID}); err != nil {
			t.Fatal(err)
		}
		if got := e.noteTagNames(t, uid, n.ID); !same(got, []string{"b", "c"}) {
			t.Fatalf("after replace (run %d): %v", i+1, got)
		}
	}

	if err := e.tags.SetNoteTags(ctx, uid, n.ID, []uuid.UUID{}); err != nil {
		t.Fatal(err)
	}
	if got := e.noteTagNames(t, uid, n.ID); len(got) != 0 {
		t.Errorf("an empty set must clear all tags: %v", got)
	}
}

func TestDB_SetNoteTags_ForeignTag_ChangesNothing(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	alice, bob := e.user(t, "alice@x.io"), e.user(t, "bob@x.io")
	n := e.note(t, bob)
	mine, alices := e.tag(t, bob, "mine"), e.tag(t, alice, "alices")
	if err := e.tags.SetNoteTags(ctx, bob, n.ID, []uuid.UUID{mine.ID}); err != nil {
		t.Fatal(err)
	}

	err := e.tags.SetNoteTags(ctx, bob, n.ID, []uuid.UUID{alices.ID})
	if !errors.Is(err, tag.ErrInvalidTag) {
		t.Fatalf("err = %v, want ErrInvalidTag", err)
	}
	err = e.tags.SetNoteTags(ctx, bob, n.ID, []uuid.UUID{mine.ID, uuid.New()})
	if !errors.Is(err, tag.ErrInvalidTag) {
		t.Fatalf("a made-up tag id: err = %v, want ErrInvalidTag", err)
	}
	if got := e.noteTagNames(t, bob, n.ID); !same(got, []string{"mine"}) {
		t.Errorf("a rejected request must leave the set untouched: %v", got)
	}
}

func TestDB_SetNoteTags_ForeignOrDeletedNote_IsNotFound(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	alice, bob := e.user(t, "alice@x.io"), e.user(t, "bob@x.io")
	alicesNote := e.note(t, alice)
	bobsTag := e.tag(t, bob, "t")

	if err := e.tags.SetNoteTags(ctx, bob, alicesNote.ID, []uuid.UUID{bobsTag.ID}); !errors.Is(err, tag.ErrNoteNotFound) {
		t.Errorf("foreign note: err = %v, want ErrNoteNotFound", err)
	}

	gone := e.note(t, bob)
	_ = e.notes.Delete(ctx, bob, gone.ID)
	if err := e.tags.SetNoteTags(ctx, bob, gone.ID, nil); !errors.Is(err, tag.ErrNoteNotFound) {
		t.Errorf("soft-deleted note: err = %v, want ErrNoteNotFound", err)
	}
}

func TestDB_DeleteTag_DetachesItFromNotes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.user(t, "a@x.io")
	n := e.note(t, uid)
	keep, drop := e.tag(t, uid, "keep"), e.tag(t, uid, "drop")
	_ = e.tags.SetNoteTags(ctx, uid, n.ID, []uuid.UUID{keep.ID, drop.ID})

	if err := e.tags.Delete(ctx, uid, drop.ID); err != nil {
		t.Fatal(err)
	}

	if got := e.noteTagNames(t, uid, n.ID); !same(got, []string{"keep"}) {
		t.Errorf("deleting a tag must only detach it: %v", got)
	}
}
