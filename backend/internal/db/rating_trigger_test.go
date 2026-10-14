package db_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
)

// Столбцы рейтинга в заметке обязаны совпадать с таблицей оценок при любом способе записи.
func storedRating(t *testing.T, q db.DBTX, noteID uuid.UUID) (count int64, avg float64) {
	t.Helper()
	if err := q.QueryRow(context.Background(), `SELECT rating_count, rating_avg FROM notes WHERE id = $1`, noteID).Scan(&count, &avg); err != nil {
		t.Fatal(err)
	}
	return count, avg
}

func TestRatingTrigger_KeepsTheNoteInStepWithItsRatings(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	q := db.New(tx)
	mk := func(name string) uuid.UUID {
		u, err := q.CreateUser(ctx, db.CreateUserParams{Email: name + "-" + uuid.NewString() + "@x.io", PasswordHash: "x", DisplayName: name})
		if err != nil {
			t.Fatal(err)
		}
		return u.ID
	}
	author, a, b, c := mk("author"), mk("a"), mk("b"), mk("c")
	n, err := q.CreateNote(ctx, db.CreateNoteParams{UserID: author, Title: "n", Content: []byte(`{}`), Kind: "note"})
	if err != nil {
		t.Fatal(err)
	}
	rate := func(user uuid.UUID, v int16) {
		t.Helper()
		if err := q.UpsertRating(ctx, db.UpsertRatingParams{NoteID: n.ID, UserID: user, Value: v}); err != nil {
			t.Fatal(err)
		}
	}
	expect := func(step string, wantCount int64, wantAvg float64) {
		t.Helper()
		count, avg := storedRating(t, tx, n.ID)
		if count != wantCount || avg != wantAvg {
			t.Errorf("%s: stored %d ratings avg %v, want %d avg %v", step, count, avg, wantCount, wantAvg)
		}
	}

	expect("no ratings yet", 0, 0)
	rate(a, 5)
	expect("one rating", 1, 5)
	rate(b, 2)
	expect("two ratings", 2, 3.5)
	rate(a, 1) // повторная оценка заменяет прежнюю
	expect("a re-rating", 2, 1.5)
	rate(c, 4)
	expect("three ratings", 3, 7.0/3)

	// Каскадное удаление аккаунта оценщика тоже обновляет заметку.
	if _, err := q.DeleteUser(ctx, c); err != nil {
		t.Fatal(err)
	}
	expect("after a rater's account is deleted", 2, 1.5)

	if _, err := tx.Exec(ctx, `DELETE FROM note_ratings WHERE note_id = $1`, n.ID); err != nil {
		t.Fatal(err)
	}
	expect("all ratings removed", 0, 0)
}

// Одновременные оценки одной заметки: итог совпадает с таблицей, ни одна оценка не потеряна. Идёт на настоящих
// соединениях и коммитит данные, поэтому после себя всё удаляет.
func TestRatingTrigger_ConcurrentRatingsDoNotLoseAnyUpdate(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	q := db.New(pool)
	author, err := q.CreateUser(ctx, db.CreateUserParams{Email: "race-author-" + uuid.NewString() + "@x.io", PasswordHash: "x", DisplayName: "race"})
	if err != nil {
		t.Fatal(err)
	}
	const raters = 12
	users := make([]uuid.UUID, raters)
	for i := range users {
		u, err := q.CreateUser(ctx, db.CreateUserParams{Email: fmt.Sprintf("race-%d-%s@x.io", i, uuid.NewString()), PasswordHash: "x", DisplayName: "r"})
		if err != nil {
			t.Fatal(err)
		}
		users[i] = u.ID
	}
	n, err := q.CreateNote(ctx, db.CreateNoteParams{UserID: author.ID, Title: "race", Content: []byte(`{}`), Kind: "note"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1 OR id = ANY($2)`, author.ID, users)
	})

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, u := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := q.UpsertRating(ctx, db.UpsertRatingParams{NoteID: n.ID, UserID: u, Value: int16(1 + i%5)}); err != nil {
				t.Error(err)
			}
		}()
	}
	close(start)
	wg.Wait()

	var wantCount int64
	var wantAvg float64
	if err := pool.QueryRow(ctx, `SELECT count(*), avg(value)::float8 FROM note_ratings WHERE note_id = $1`, n.ID).Scan(&wantCount, &wantAvg); err != nil {
		t.Fatal(err)
	}
	count, avg := storedRating(t, pool, n.ID)
	if wantCount != raters || count != wantCount || avg != wantAvg {
		t.Errorf("table: %d ratings avg %v; note: %d ratings avg %v; want all %d counted (a lost update shows as a smaller count)", wantCount, wantAvg, count, avg, raters)
	}
}
