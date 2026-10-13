package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
)

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, Error{Error: struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message}})
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
