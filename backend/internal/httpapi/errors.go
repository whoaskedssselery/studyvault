package httpapi

import "net/http"

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
