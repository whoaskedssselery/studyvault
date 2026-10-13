package attachment_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whoaskedssselery/studyvault/backend/internal/attachment"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/note"
)

// Интеграционные тесты идут на настоящем Postgres в транзакции, которая откатывается в конце.
// Хранилище подменено версией в памяти. Без DATABASE_URL они пропускаются.
type env struct {
	tx    pgx.Tx
	svc   *attachment.Service
	st    *memStorage
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

	q, st := db.New(tx), newMemStorage()
	return env{tx: tx, svc: attachment.NewService(q, st), st: st, notes: note.NewService(q)}
}

func (e env) user(t *testing.T, email string) uuid.UUID {
	t.Helper()
	u, err := db.New(e.tx).CreateUser(context.Background(), db.CreateUserParams{Email: email, PasswordHash: "x", DisplayName: email})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func (e env) note(t *testing.T, uid uuid.UUID) uuid.UUID {
	t.Helper()
	n, err := e.notes.Create(context.Background(), uid, note.Input{Title: "n", Content: []byte(`{"type":"doc"}`)})
	if err != nil {
		t.Fatal(err)
	}
	return n.ID
}

func TestDB_OtherUsersAttachments_AreInvisibleAndUntouchable(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	alice, bob := e.user(t, "alice@x.io"), e.user(t, "bob@x.io")
	up, err := e.svc.Create(ctx, alice, valid())
	if err != nil {
		t.Fatal(err)
	}
	e.st.objects[up.Attachment.ObjectKey] = 1234

	if _, err := e.svc.Get(ctx, bob, up.Attachment.ID); !errors.Is(err, attachment.ErrNotFound) {
		t.Errorf("bob reading alice's file: err = %v, want ErrNotFound", err)
	}
	if err := e.svc.Delete(ctx, bob, up.Attachment.ID); !errors.Is(err, attachment.ErrNotFound) {
		t.Errorf("bob deleting alice's file: err = %v, want ErrNotFound", err)
	}
	if len(e.st.removed) != 0 || len(e.st.objects) != 1 {
		t.Error("bob's attempt must not touch the stored file")
	}
	if _, err := e.svc.Get(ctx, alice, up.Attachment.ID); err != nil {
		t.Errorf("alice's file must survive: %v", err)
	}
}

func TestDB_ListByNote_ScopedToTheOwner(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	alice, bob := e.user(t, "alice@x.io"), e.user(t, "bob@x.io")
	alicesNote := e.note(t, alice)

	in := valid()
	in.NoteID = &alicesNote
	for _, name := range []string{"first.pdf", "second.pdf"} {
		in.FileName = name
		if _, err := e.svc.Create(ctx, alice, in); err != nil {
			t.Fatal(err)
		}
	}

	// Внутри одной транзакции now() у обеих записей одинаков, поэтому разводим время явно.
	if _, err := e.tx.Exec(ctx, `UPDATE attachments SET created_at = now() - interval '1 hour' WHERE file_name = 'first.pdf'`); err != nil {
		t.Fatal(err)
	}

	got, err := e.svc.List(ctx, alice, alicesNote)
	if err != nil || len(got) != 2 || got[0].FileName != "first.pdf" || got[1].FileName != "second.pdf" {
		t.Errorf("alice's list: %+v, err %v", got, err)
	}
	if _, err := e.svc.List(ctx, bob, alicesNote); !errors.Is(err, attachment.ErrNoteNotFound) {
		t.Errorf("bob listing alice's note: err = %v, want ErrNoteNotFound", err)
	}
	if _, err := e.svc.Create(ctx, bob, in); !errors.Is(err, attachment.ErrNoteNotFound) {
		t.Errorf("bob attaching to alice's note: err = %v, want ErrNoteNotFound", err)
	}
}

func TestDB_AttachingToASoftDeletedNote_IsRejected(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.user(t, "a@x.io")
	nid := e.note(t, uid)
	if err := e.notes.Delete(ctx, uid, nid); err != nil {
		t.Fatal(err)
	}

	in := valid()
	in.NoteID = &nid
	if _, err := e.svc.Create(ctx, uid, in); !errors.Is(err, attachment.ErrNoteNotFound) {
		t.Fatalf("err = %v, want ErrNoteNotFound", err)
	}
}

func TestDB_FullLifecycle(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.user(t, "a@x.io")

	up, err := e.svc.Create(ctx, uid, valid())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Get(ctx, uid, up.Attachment.ID); !errors.Is(err, attachment.ErrNotUploaded) {
		t.Fatalf("before the upload: err = %v", err)
	}
	e.st.objects[up.Attachment.ObjectKey] = 1234
	dl, err := e.svc.Get(ctx, uid, up.Attachment.ID)
	if err != nil || dl.Attachment.FileName != "lecture.pdf" || dl.URL == "" {
		t.Fatalf("after the upload: %+v, err %v", dl, err)
	}

	if err := e.svc.Delete(ctx, uid, up.Attachment.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Get(ctx, uid, up.Attachment.ID); !errors.Is(err, attachment.ErrNotFound) {
		t.Errorf("after deletion: err = %v, want ErrNotFound", err)
	}
	if len(e.st.objects) != 0 {
		t.Error("the stored file must be gone")
	}
}
