package auth_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
)

// Как и в остальных пакетах: настоящий Postgres внутри транзакции, которая откатывается.
func newTxService(t *testing.T) (*auth.Service, pgx.Tx) {
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
	return auth.NewService(db.New(tx), secret, auth.WithBcryptCost(bcrypt.MinCost)), tx
}

func tokenCount(t *testing.T, tx pgx.Tx, email string) int {
	t.Helper()
	var n int
	err := tx.QueryRow(context.Background(),
		`SELECT count(*) FROM refresh_tokens r JOIN users u ON u.id = r.user_id WHERE u.email = $1`, email).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestIntegration_PurgeDeletesExpiredButKeepsRevokedLiveTokens(t *testing.T) {
	svc, tx := newTxService(t)
	ctx := context.Background()
	const email = "purge-test@x.io"
	first, err := svc.Register(ctx, email, "password123", "Pat", "") // токен 1
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := svc.Login(ctx, email, "password123", "") // токен 2, его мы отзовём ротацией
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Refresh(ctx, rotated.RefreshToken, ""); err != nil { // токен 2 отозван, токен 3 жив
		t.Fatal(err)
	}
	// Токен 1 состарился.
	if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET expires_at = now() - interval '1 day' WHERE token_hash = $1`,
		auth.HashRefreshToken(first.RefreshToken)); err != nil {
		t.Fatal(err)
	}
	if got := tokenCount(t, tx, email); got != 3 {
		t.Fatalf("setup: %d tokens, want 3", got)
	}

	if _, err := svc.PurgeExpiredTokens(ctx); err != nil {
		t.Fatal(err)
	}

	if got := tokenCount(t, tx, email); got != 2 {
		t.Errorf("%d tokens left, want 2 (the expired one is gone, the revoked live one and the active one stay)", got)
	}
	// Повторное предъявление отозванного токена по-прежнему распознаётся как кража и закрывает сессию.
	if _, err := svc.Refresh(ctx, rotated.RefreshToken, ""); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("reuse of a revoked token: err = %v, want ErrInvalidToken", err)
	}
	var live int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM refresh_tokens r JOIN users u ON u.id = r.user_id
		WHERE u.email = $1 AND r.revoked_at IS NULL`, email).Scan(&live)
	if live != 0 {
		t.Errorf("%d live tokens after a reuse, want the whole session revoked", live)
	}
}
