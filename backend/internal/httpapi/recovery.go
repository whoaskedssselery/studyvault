package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
)

// Сброс пароля по почте: запрос ссылки и подтверждение новым паролем. Оба метода открыты без токена.

func (a *api) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req PasswordResetRequest
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	if email := string(req.Email); email == "" || len(email) > maxEmailLen {
		badRequest(w, "email is not valid")
		return
	}
	err := a.Account.RequestPasswordReset(r.Context(), string(req.Email))
	switch {
	case errors.Is(err, auth.ErrResetUnavailable):
		writeError(w, http.StatusServiceUnavailable, "email_unavailable", "password reset is not available: the server has no mail configured")
	case err != nil:
		slog.ErrorContext(r.Context(), "request password reset", "err", err)
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent) // одинаково для известной и неизвестной почты
	}
}

func (a *api) ConfirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req PasswordResetConfirm
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	if req.Token == "" {
		badRequest(w, "token is required")
		return
	}
	if len(req.NewPassword) < minPasswordLen || len(req.NewPassword) > maxPasswordBytes {
		badRequest(w, "new_password must be 8 to 72 bytes long")
		return
	}
	err := a.Account.ConfirmPasswordReset(r.Context(), req.Token, req.NewPassword)
	switch {
	case errors.Is(err, auth.ErrInvalidToken), errors.Is(err, auth.ErrUserNotFound):
		writeError(w, http.StatusUnauthorized, "invalid_token", "the reset link is invalid, expired or already used")
	case err != nil:
		slog.ErrorContext(r.Context(), "confirm password reset", "err", err)
		internalError(w)
	default:
		a.clearRefreshCookie(w)
		w.WriteHeader(http.StatusNoContent)
	}
}

// DeleteMe удаляет аккаунт после подтверждения паролем. Файлы пользователя убираются из хранилища уже после
// удаления записей: ключи берутся заранее, потому что потом их не найти.
func (a *api) DeleteMe(w http.ResponseWriter, r *http.Request) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req DeleteAccountRequest
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	if req.Password == "" {
		badRequest(w, "password is required")
		return
	}
	var keys []string
	if a.Attachments != nil {
		var err error
		if keys, err = a.Attachments.UserObjectKeys(r.Context(), uid); err != nil {
			slog.ErrorContext(r.Context(), "delete account: list files", "err", err)
			internalError(w)
			return
		}
	}

	err := a.Account.DeleteAccount(r.Context(), uid, req.Password)
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusForbidden, "invalid_credentials", "password is incorrect")
	case errors.Is(err, auth.ErrUserNotFound):
		writeError(w, http.StatusUnauthorized, "unauthorized", "account no longer exists")
	case err != nil:
		slog.ErrorContext(r.Context(), "delete account", "err", err)
		internalError(w)
	default:
		if a.Attachments != nil {
			a.Attachments.RemoveObjects(r.Context(), keys)
		}
		a.clearRefreshCookie(w)
		w.WriteHeader(http.StatusNoContent)
	}
}
