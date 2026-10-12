package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
)

// Всё, чего нет в этом списке, требует access-токен: новый маршрут по умолчанию закрыт.
var publicPaths = map[string]bool{
	"/health":        true,
	"/auth/register": true,
	"/auth/login":    true,
	"/auth/refresh":  true,
	"/auth/logout":   true,
}

func authMiddleware(secret []byte) MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if publicPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			header := r.Header.Get("Authorization")
			token, found := strings.CutPrefix(header, "Bearer ")
			if !found || token == "" {
				writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
				return
			}
			userID, err := auth.ParseAccessToken(secret, token, time.Now())
			if err != nil {
				writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithUserID(r.Context(), userID)))
		})
	}
}
