package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
)

// errorTypeBase — адрес каталога ошибок; к нему добавляется якорь с кодом: .../docs/errors.md#not_found.
const errorTypeBase = "https://github.com/whoaskedssselery/studyvault/blob/main/docs/errors.md#"

// writeError пишет ошибку в формате RFC 7807 (application/problem+json). Поля code и request_id расширяют
// стандартные: по первому клиент выбирает поведение, по второму ответ находят в логе сервера.
func writeError(w http.ResponseWriter, status int, code, message string) {
	p := Problem{
		Type:   errorTypeBase + code,
		Title:  http.StatusText(status),
		Status: status,
		Detail: message,
		Code:   code,
	}
	if id := w.Header().Get(requestIDHeader); id != "" {
		p.RequestId = &id
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(p); err != nil {
		slog.Error("write error response", "err", err)
	}
}

func badRequest(w http.ResponseWriter, message string) {
	writeError(w, http.StatusBadRequest, "validation_failed", message)
}

func internalError(w http.ResponseWriter) {
	writeError(w, http.StatusInternalServerError, "internal", "internal server error")
}

func notFound(w http.ResponseWriter, message string) {
	writeError(w, http.StatusNotFound, "not_found", message)
}

// currentUser достаёт id пользователя, который положил authMiddleware.
func currentUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, ok := auth.UserID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
	}
	return id, ok
}

// limitReached отвечает 409: запрос корректен, но у пользователя исчерпана квота (см. пакет limits).
func limitReached(w http.ResponseWriter, err error) {
	writeError(w, http.StatusConflict, "limit_reached", err.Error())
}
