package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/whoaskedssselery/studyvault/backend/internal/attachment"
	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/config"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/httpapi"
	"github.com/whoaskedssselery/studyvault/backend/internal/note"
	"github.com/whoaskedssselery/studyvault/backend/internal/search"
	"github.com/whoaskedssselery/studyvault/backend/internal/storage"
	"github.com/whoaskedssselery/studyvault/backend/internal/subject"
	"github.com/whoaskedssselery/studyvault/backend/internal/tag"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	// .env лежит в корне репозитория, а сервис запускается из backend/.
	// Файла может не быть (например, в контейнере), это не ошибка.
	_ = godotenv.Load("../.env")

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	queries := db.New(pool)
	noteOpts := []note.Option{note.WithTx(func(ctx context.Context, fn func(note.Store) error) error {
		return db.InTx(ctx, pool, func(q *db.Queries) error { return fn(q) })
	})}
	deps := httpapi.Deps{
		DB:             pool,
		Auth:           auth.NewService(queries, cfg.JWTSecret),
		Subjects:       subject.NewService(queries),
		Tags:           tag.NewService(queries),
		JWTSecret:      cfg.JWTSecret,
		CookieSecure:   cfg.CookieSecure,
		AllowedOrigins: cfg.CORSOrigins,
		AuthRateLimit:  cfg.AuthRateLimit,
	}

	// Без Meilisearch API работает целиком, кроме /search (503).
	if cfg.MeiliURL != "" {
		meili := search.NewMeili(cfg.MeiliURL, cfg.MeiliKey, search.IndexUID)
		noteOpts = append(noteOpts, note.WithIndexer(meili))
		deps.Search = search.NewService(meili, queries)
	} else {
		slog.Warn("MEILI_URL is not set: search is disabled")
	}
	// Без хранилища файлов API работает целиком, кроме /attachments (503).
	if cfg.S3.Endpoint != "" {
		s3, err := storage.NewS3(storage.Config{
			Endpoint:       cfg.S3.Endpoint,
			AccessKey:      cfg.S3.AccessKey,
			SecretKey:      cfg.S3.SecretKey,
			Bucket:         cfg.S3.Bucket,
			Region:         cfg.S3.Region,
			UseSSL:         cfg.S3.UseSSL,
			PublicEndpoint: cfg.S3.PublicEndpoint,
		})
		if err != nil {
			return fmt.Errorf("file storage: %w", err)
		}
		deps.Attachments = attachment.NewService(queries, s3)
	} else {
		slog.Warn("S3_ENDPOINT is not set: attachments are disabled")
	}

	deps.Notes = note.NewService(queries, noteOpts...)

	srv := httpapi.NewServer(cfg, deps)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
