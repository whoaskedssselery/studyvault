package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/cors"
)

// corsMiddleware пускает в API браузерный фронтенд только с явно перечисленных источников.
// Credentials включены ради refresh-cookie, поэтому подстановочный источник недопустим
// (config.Load его и не принимает). При пустом списке заголовки CORS не добавляются вовсе.
func corsMiddleware(origins []string) func(http.Handler) http.Handler {
	if len(origins) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	return cors.Handler(cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           int((10 * time.Minute).Seconds()),
	})
}

// securityHeaders: nosniff для всех ответов; ответы API несут токены и личные данные, поэтому
// их нельзя кэшировать. Страницы документации статичны и от этого правила освобождены.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !strings.HasPrefix(r.URL.Path, "/docs") && r.URL.Path != "/openapi.yaml" {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
