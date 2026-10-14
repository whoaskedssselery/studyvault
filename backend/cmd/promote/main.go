// Команда promote назначает роль существующему пользователю. Это единственный способ сделать модератора:
// через HTTP API роль назначить нельзя, чтобы клиент не мог повысить себя сам.
//
//	cd backend && go run ./cmd/promote moderator@example.com            # роль moderator
//	cd backend && go run ./cmd/promote moderator@example.com student    # вернуть роль student
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/config"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("promote failed", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: promote <email> [moderator|student]")
	}
	email, role := args[0], auth.RoleModerator
	if len(args) == 2 {
		role = args[1]
	}

	_ = godotenv.Load("../.env")
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	svc := auth.NewService(db.New(pool), cfg.JWTSecret)
	switch err := svc.SetRole(ctx, email, role); {
	case errors.Is(err, auth.ErrUserNotFound):
		return fmt.Errorf("no user with the email %q: register first", email)
	case err != nil:
		return err
	}
	fmt.Printf("%s is now a %s\n", email, role)
	return nil
}
