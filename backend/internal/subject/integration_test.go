package subject_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/subject"
)

// Интеграционные тесты идут на настоящем Postgres (docker compose up) внутри транзакции,
// которая откатывается в конце, так что данные не остаются. Без DATABASE_URL они пропускаются.
func newTx(t *testing.T) (pgx.Tx, *subject.Service) {
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
	return tx, subject.NewService(db.New(tx))
}

func newUser(t *testing.T, tx pgx.Tx, email string) uuid.UUID {
	t.Helper()
	u, err := db.New(tx).CreateUser(context.Background(), db.CreateUserParams{Email: email, PasswordHash: "x", DisplayName: email})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func ptr[T any](v T) *T { return &v }

func TestDB_OtherUsersSubjects_AreInvisibleAndUntouchable(t *testing.T) {
	tx, svc := newTx(t)
	ctx := context.Background()
	alice, bob := newUser(t, tx, "alice@x.io"), newUser(t, tx, "bob@x.io")

	s, err := svc.Create(ctx, alice, subject.Input{Title: "Algebra"})
	if err != nil {
		t.Fatal(err)
	}

	if list, _ := svc.List(ctx, bob); len(list) != 0 {
		t.Errorf("bob sees %d of alice's subjects", len(list))
	}
	if _, err := svc.Update(ctx, bob, s.ID, subject.Patch{Title: ptr("hacked")}); !errors.Is(err, subject.ErrNotFound) {
		t.Errorf("bob updating alice's subject: err = %v, want ErrNotFound", err)
	}
	if err := svc.Delete(ctx, bob, s.ID); !errors.Is(err, subject.ErrNotFound) {
		t.Errorf("bob deleting alice's subject: err = %v, want ErrNotFound", err)
	}

	list, _ := svc.List(ctx, alice)
	if len(list) != 1 || list[0].Title != "Algebra" {
		t.Errorf("alice's subject was changed or removed: %+v", list)
	}
}

func TestDB_Update_PartialSemantics(t *testing.T) {
	tx, svc := newTx(t)
	ctx := context.Background()
	uid := newUser(t, tx, "a@x.io")
	s, _ := svc.Create(ctx, uid, subject.Input{Title: "Physics", Color: ptr("#112233"), Semester: ptr(int16(2))})

	// Только title: color и semester остаются.
	got, err := svc.Update(ctx, uid, s.ID, subject.Patch{Title: ptr("Physics II")})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Physics II" || got.Color.String != "#112233" || got.Semester.Int16 != 2 {
		t.Errorf("title-only patch changed other fields: %+v", got)
	}

	// color: null очищает только color.
	got, _ = svc.Update(ctx, uid, s.ID, subject.Patch{SetColor: true})
	if got.Color.Valid || got.Semester.Int16 != 2 || got.Title != "Physics II" {
		t.Errorf("clearing color affected other fields: %+v", got)
	}

	// semester получает новое значение, color так и остаётся пустым.
	got, _ = svc.Update(ctx, uid, s.ID, subject.Patch{SetSemester: true, Semester: ptr(int16(5))})
	if got.Semester.Int16 != 5 || got.Color.Valid {
		t.Errorf("setting semester went wrong: %+v", got)
	}
}

func TestDB_List_OrderedBySemesterThenTitle(t *testing.T) {
	tx, svc := newTx(t)
	ctx := context.Background()
	uid := newUser(t, tx, "a@x.io")
	for _, in := range []subject.Input{
		{Title: "zeta"},
		{Title: "beta", Semester: ptr(int16(2))},
		{Title: "Alpha", Semester: ptr(int16(2))},
		{Title: "gamma", Semester: ptr(int16(1))},
	} {
		if _, err := svc.Create(ctx, uid, in); err != nil {
			t.Fatal(err)
		}
	}

	list, _ := svc.List(ctx, uid)
	var titles []string
	for _, s := range list {
		titles = append(titles, s.Title)
	}
	want := []string{"gamma", "Alpha", "beta", "zeta"}
	if len(titles) != len(want) {
		t.Fatalf("got %v", titles)
	}
	for i := range want {
		if titles[i] != want[i] {
			t.Fatalf("order = %v, want %v", titles, want)
		}
	}
}

func TestDB_DeleteSubject_KeepsItsNotes(t *testing.T) {
	tx, svc := newTx(t)
	ctx := context.Background()
	uid := newUser(t, tx, "a@x.io")
	s, _ := svc.Create(ctx, uid, subject.Input{Title: "History"})

	var noteID uuid.UUID
	err := tx.QueryRow(ctx,
		`INSERT INTO notes (user_id, subject_id, title, content) VALUES ($1, $2, 'n', '{}') RETURNING id`,
		uid, s.ID).Scan(&noteID)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.Delete(ctx, uid, s.ID); err != nil {
		t.Fatal(err)
	}

	var subjectID uuid.NullUUID
	var deletedAt *string
	err = tx.QueryRow(ctx, `SELECT subject_id, deleted_at::text FROM notes WHERE id = $1`, noteID).Scan(&subjectID, &deletedAt)
	if err != nil {
		t.Fatalf("the note must survive: %v", err)
	}
	if subjectID.Valid || deletedAt != nil {
		t.Errorf("note must be detached, not removed: subject_id=%+v deleted_at=%v", subjectID, deletedAt)
	}
}
