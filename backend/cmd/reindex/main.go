// Команда reindex полностью пересобирает поисковый индекс из Postgres.
// Нужна, если Meilisearch потерял данные или индекс разошёлся с базой:
//
//	cd backend && go run ./cmd/reindex
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/whoaskedssselery/studyvault/backend/internal/config"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/search"
)

const pageSize = 500

func main() {
	if err := run(); err != nil {
		slog.Error("reindex failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	_ = godotenv.Load("../.env")
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.MeiliURL == "" {
		return errors.New("MEILI_URL is not set")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	meili := search.NewMeili(cfg.MeiliURL, cfg.MeiliKey, search.IndexUID)
	started := time.Now()

	n, err := meili.ReplaceAll(ctx, search.ReindexPages(db.New(pool), pageSize))
	if err != nil {
		return err
	}
	if err := meili.Wait(ctx); err != nil {
		return fmt.Errorf("wait for the index: %w", err)
	}
	fmt.Printf("indexed %d notes in %s\n", n, time.Since(started).Round(time.Millisecond))
	return nil
}
