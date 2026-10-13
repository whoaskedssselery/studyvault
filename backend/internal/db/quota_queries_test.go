package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
)

// Запросы, на которых держатся квоты, должны считать только данные своего пользователя.
func TestQuotaQueries_CountOnlyTheOwnersRows(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	q := db.New(tx)

	mk := func(email string) uuid.UUID {
		u, err := q.CreateUser(ctx, db.CreateUserParams{Email: email, PasswordHash: "x", DisplayName: email})
		if err != nil {
			t.Fatal(err)
		}
		return u.ID
	}
	alice, bob := mk("quota-alice@x.io"), mk("quota-bob@x.io")

	for _, title := range []string{"a", "b", "c"} {
		if _, err := q.CreateSubject(ctx, db.CreateSubjectParams{UserID: alice, Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := q.CreateSubject(ctx, db.CreateSubjectParams{UserID: bob, Title: "bob's"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"x", "y"} {
		if _, err := q.CreateTag(ctx, db.CreateTagParams{UserID: alice, Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	note, err := q.CreateNote(ctx, db.CreateNoteParams{UserID: alice, Title: "n", Content: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	for i, size := range []int64{100, 250} {
		if _, err := q.CreateAttachment(ctx, db.CreateAttachmentParams{
			UserID: alice, NoteID: uuid.NullUUID{UUID: note.ID, Valid: true}, ObjectKey: "k" + string(rune('a'+i)) + uuid.NewString(),
			FileName: "f", MimeType: "text/plain", SizeBytes: size,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Файл без заметки: в размер пользователя входит, в счёт файлов заметки нет.
	if _, err := q.CreateAttachment(ctx, db.CreateAttachmentParams{UserID: alice, ObjectKey: "loose" + uuid.NewString(), FileName: "f", MimeType: "text/plain", SizeBytes: 1000}); err != nil {
		t.Fatal(err)
	}

	check := func(name string, got int64, err error, want int64) {
		t.Helper()
		if err != nil || got != want {
			t.Errorf("%s = %d (err %v), want %d", name, got, err, want)
		}
	}
	n, err := q.CountSubjects(ctx, alice)
	check("alice's subjects", n, err, 3)
	n, err = q.CountSubjects(ctx, bob)
	check("bob's subjects", n, err, 1)
	n, err = q.CountTagsByUser(ctx, alice)
	check("alice's tags", n, err, 2)
	n, err = q.CountTagsByUser(ctx, bob)
	check("bob's tags", n, err, 0)
	n, err = q.CountNoteAttachments(ctx, db.CountNoteAttachmentsParams{NoteID: uuid.NullUUID{UUID: note.ID, Valid: true}, UserID: alice})
	check("files on the note", n, err, 2)
	n, err = q.CountNoteAttachments(ctx, db.CountNoteAttachmentsParams{NoteID: uuid.NullUUID{UUID: note.ID, Valid: true}, UserID: bob})
	check("files on alice's note counted for bob", n, err, 0)
	n, err = q.SumUserAttachmentBytes(ctx, alice)
	check("alice's bytes", n, err, 1350)
	n, err = q.SumUserAttachmentBytes(ctx, bob)
	check("bob's bytes (no files must give 0, not NULL)", n, err, 0)
}
