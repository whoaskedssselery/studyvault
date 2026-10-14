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
	"github.com/whoaskedssselery/studyvault/backend/internal/library"
	"github.com/whoaskedssselery/studyvault/backend/internal/mail"
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

func newLogHandler(cfg config.Config) slog.Handler {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.LogJSON {
		return slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.NewTextHandler(os.Stdout, opts)
}

func run() error {
	// .env лежит в корне репозитория, а сервис запускается из backend/.
	// Файла может не быть (например, в контейнере), это не ошибка.
	_ = godotenv.Load("../.env")

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	slog.SetDefault(slog.New(httpapi.NewLogHandler(newLogHandler(cfg))))

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
	libOpts := []library.Option{library.WithTx(func(ctx context.Context, fn func(library.Store) error) error {
		return db.InTx(ctx, pool, func(q *db.Queries) error { return fn(q) })
	})}
	var authOpts []auth.Option
	if cfg.SMTP.Host != "" {
		mailer, err := mail.NewSMTP(mail.Config{
			Host: cfg.SMTP.Host, Port: cfg.SMTP.Port, Username: cfg.SMTP.Username, Password: cfg.SMTP.Password, From: cfg.SMTP.From,
		})
		if err != nil {
			return fmt.Errorf("mail: %w", err)
		}
		authOpts = append(authOpts, auth.WithMailer(mailer, cfg.SMTP.ResetURL), auth.WithEmailVerification(cfg.SMTP.VerifyURL))
	} else {
		slog.Warn("SMTP_HOST is not set: password reset and e-mail verification are disabled, new accounts count as verified")
	}
	authSvc := auth.NewService(queries, cfg.JWTSecret, authOpts...)
	deps := httpapi.Deps{
		DB:             pool,
		Auth:           authSvc,
		Account:        authSvc,
		Subjects:       subject.NewService(queries),
		Tags:           tag.NewService(queries),
		JWTSecret:      cfg.JWTSecret,
		CookieSecure:   cfg.CookieSecure,
		AllowedOrigins: cfg.CORSOrigins,
		AuthRateLimit:  cfg.AuthRateLimit,
		TrustedProxies: cfg.TrustedProxies,
	}

	// Без Meilisearch API работает целиком, кроме /search (503).
	if cfg.MeiliURL != "" {
		meili := search.NewMeili(cfg.MeiliURL, cfg.MeiliKey, search.IndexUID)
		noteOpts = append(noteOpts, note.WithIndexer(meili))
		libOpts = append(libOpts, library.WithIndexer(meili), library.WithEngine(meili))
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
	deps.Library = library.NewService(queries, libOpts...)

	// Горутина живёт до сигнала остановки: ctx отменяется вместе с ним.
	go authSvc.RunTokenPurge(ctx, time.Hour)

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
