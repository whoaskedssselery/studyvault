package httpapi

import (
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
)

// Подтверждение почты: письмо со ссылкой уходит при регистрации, ссылку можно запросить заново и подтвердить.

// RequestEmailVerification отправляет вошедшему пользователю новую ссылку. Чаще раза в минуту на аккаунт письма не
// уходят: здесь, в отличие от сброса пароля, ответ может это сказать, потому что запрос идёт от владельца аккаунта.
func (a *api) RequestEmailVerification(w http.ResponseWriter, r *http.Request) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	err := a.Account.RequestEmailVerification(r.Context(), uid)
	var soon *auth.TooSoonError
	switch {
	case errors.Is(err, auth.ErrVerificationUnavailable):
		writeError(w, http.StatusServiceUnavailable, "email_unavailable", "e-mail verification is not available: the server has no mail configured")
	case errors.As(err, &soon):
		w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(soon.RetryAfter.Seconds()))))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "a verification mail was sent a moment ago, try again later")
	case errors.Is(err, auth.ErrUserNotFound):
		writeError(w, http.StatusUnauthorized, "unauthorized", "account no longer exists")
	case err != nil:
		slog.ErrorContext(r.Context(), "request e-mail verification", "err", err)
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// ConfirmEmail подтверждает почту по токену из письма. Метод открыт без токена доступа.
func (a *api) ConfirmEmail(w http.ResponseWriter, r *http.Request) {
	var req EmailVerificationConfirm
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	if req.Token == "" {
		badRequest(w, "token is required")
		return
	}
	err := a.Account.ConfirmEmail(r.Context(), req.Token)
	switch {
	case errors.Is(err, auth.ErrInvalidToken), errors.Is(err, auth.ErrUserNotFound):
		writeError(w, http.StatusUnauthorized, "invalid_token", "the verification link is invalid, expired or already used")
	case err != nil:
		slog.ErrorContext(r.Context(), "confirm e-mail", "err", err)
		internalError(w)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
