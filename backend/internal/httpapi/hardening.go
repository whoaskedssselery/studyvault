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
		AllowedOrigins: origins,
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders: []string{"Authorization", "Content-Type", requestIDHeader},
		// Без этого скрипт фронтенда не увидел бы эти заголовки ответа: из чужого источника браузер скрывает всё, кроме базовых.
		ExposedHeaders:   []string{requestIDHeader, "Retry-After"},
		AllowCredentials: true,
		MaxAge:           int((10 * time.Minute).Seconds()),
	})
}

// securityHeaders: nosniff, запрет встраивания и строгая CSP для всех ответов; ответы API несут токены и личные данные, поэтому
// их нельзя кэшировать. Страницы документации статичны и от этого правила освобождены.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if r.URL.Path == "/docs" {
			h.Set("Content-Security-Policy", docsCSP)
		} else {
			h.Set("Content-Security-Policy", apiCSP)
		}
		if !strings.HasPrefix(r.URL.Path, "/docs") && r.URL.Path != "/openapi.yaml" {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
