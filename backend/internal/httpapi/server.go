package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/config"
)

// Pinger is satisfied by *pgxpool.Pool.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Authenticator is satisfied by *auth.Service.
type Authenticator interface {
	Register(ctx context.Context, email, password, displayName, userAgent string) (auth.Result, error)
	Login(ctx context.Context, email, password, userAgent string) (auth.Result, error)
	Refresh(ctx context.Context, rawToken, userAgent string) (auth.Tokens, error)
	Logout(ctx context.Context, rawToken string) error
}

type Deps struct {
	DB           Pinger
	Auth         Authenticator
	Subjects     SubjectService
	JWTSecret    []byte
	CookieSecure bool
}

// api реализует ServerInterface; ещё не готовые методы отвечают 501 через Unimplemented.
type api struct {
	Unimplemented
	Deps
}

func NewServer(cfg config.Config, deps Deps) *http.Server {
	return &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           NewHandler(deps),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func NewHandler(deps Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)

	return HandlerWithOptions(&api{Deps: deps}, ChiServerOptions{
		BaseRouter:  r,
		Middlewares: []MiddlewareFunc{authMiddleware(deps.JWTSecret)},
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			badRequest(w, err.Error())
		},
	})
}

func (a *api) GetHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := a.DB.Ping(ctx); err != nil {
		slog.Error("health check: database ping failed", "err", err)
		writeJSON(w, http.StatusServiceUnavailable, Health{Status: Unavailable})
		return
	}
	writeJSON(w, http.StatusOK, Health{Status: Ok})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write response", "err", err)
	}
}
