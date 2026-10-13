package db_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
)

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Skipf("database is not reachable: %v", err)
	}
	return pool
}

func userExists(t *testing.T, pool *pgxpool.Pool, email string) bool {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE email = $1`, email).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n > 0
}

func TestInTx_ErrorRollsBackEverything(t *testing.T) {
	pool := newPool(t)
	const email = "intx-rollback@example.com"
	boom := errors.New("boom")

	err := db.InTx(context.Background(), pool, func(q *db.Queries) error {
		if _, err := q.CreateUser(context.Background(), db.CreateUserParams{Email: email, PasswordHash: "x", DisplayName: "x"}); err != nil {
			return err
		}
		return boom
	})

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the callback's error", err)
	}
	if userExists(t, pool, email) {
		t.Fatal("the user written before the error must be rolled back")
	}
}

func TestInTx_SuccessCommits(t *testing.T) {
	pool := newPool(t)
	const email = "intx-commit@example.com"
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email) })

	err := db.InTx(context.Background(), pool, func(q *db.Queries) error {
		_, err := q.CreateUser(context.Background(), db.CreateUserParams{Email: email, PasswordHash: "x", DisplayName: "x"})
		return err
	})

	if err != nil {
		t.Fatal(err)
	}
	if !userExists(t, pool, email) {
		t.Fatal("a successful callback must be committed")
	}
}

func TestInTx_PanicRollsBackAndRepanics(t *testing.T) {
	pool := newPool(t)
	const email = "intx-panic@example.com"

	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic must propagate")
			}
		}()
		_ = db.InTx(context.Background(), pool, func(q *db.Queries) error {
			_, _ = q.CreateUser(context.Background(), db.CreateUserParams{Email: email, PasswordHash: "x", DisplayName: "x"})
			panic("oops")
		})
	}()

	if userExists(t, pool, email) {
		t.Fatal("a panicking callback must be rolled back")
	}
}
