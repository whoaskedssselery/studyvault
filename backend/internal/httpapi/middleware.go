package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
)

// Всё, чего нет в этом списке, требует access-токен: новый маршрут по умолчанию закрыт.
var publicPaths = map[string]bool{
	apiPrefix + "/health":        true,
	apiPrefix + "/auth/register": true,
	apiPrefix + "/auth/login":    true,
	apiPrefix + "/auth/refresh":  true,
	apiPrefix + "/auth/logout":   true,
}

// authMiddleware стоит на уровне роутера, до разбора параметров пути: запрос без токена получает 401, а не 400 за
// неверный id, и неизвестный путь внутри /v1 не выдаёт, что такого маршрута нет. За пределами /v1 (документация,
// проба /health) токен не нужен.
// isPublicRead — GET-запросы к библиотеке (лента, поиск, страница материала, комментарии, файлы, предметы).
func isPublicRead(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	p := r.URL.Path
	return p == apiPrefix+"/library" || strings.HasPrefix(p, apiPrefix+"/library/")
}

func authMiddleware(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, apiPrefix+"/") || publicPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			header := r.Header.Get("Authorization")
			// Чтение библиотеки открыто гостям. Токен там необязателен, но если он передан, он должен быть верным:
			// иначе клиент не узнает, что пора обновить сессию, и молча увидит страницу как гость.
			if header == "" && isPublicRead(r) {
				next.ServeHTTP(w, r)
				return
			}
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
