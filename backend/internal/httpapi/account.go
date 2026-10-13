package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
)

// AccountService is satisfied by *auth.Service.
type AccountService interface {
	GetUser(ctx context.Context, id uuid.UUID) (db.User, error)
	UpdateDisplayName(ctx context.Context, id uuid.UUID, displayName string) (db.User, error)
	ChangePassword(ctx context.Context, id uuid.UUID, current, next, userAgent string) (auth.Tokens, error)
}

func (a *api) GetMe(w http.ResponseWriter, r *http.Request) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	u, err := a.Account.GetUser(r.Context(), uid)
	a.respondUser(w, r, u, err, "get account")
}

func (a *api) UpdateMe(w http.ResponseWriter, r *http.Request) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req ProfilePatch
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	name, err := trimmedField("display_name", req.DisplayName, maxDisplayNameLen)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	u, err := a.Account.UpdateDisplayName(r.Context(), uid, name)
	a.respondUser(w, r, u, err, "update account")
}

func (a *api) respondUser(w http.ResponseWriter, r *http.Request, u db.User, err error, op string) {
	switch {
	case errors.Is(err, auth.ErrUserNotFound):
		// Токен действителен, а аккаунта уже нет: для клиента это то же, что недействительный токен.
		writeError(w, http.StatusUnauthorized, "unauthorized", "account no longer exists")
	case err != nil:
		slog.ErrorContext(r.Context(), op, "err", err)
		internalError(w)
	default:
		writeJSON(w, http.StatusOK, toUser(u))
	}
}

func (a *api) ChangePassword(w http.ResponseWriter, r *http.Request) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req ChangePasswordRequest
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	if req.CurrentPassword == "" {
		badRequest(w, "current_password is required")
		return
	}
	if len(req.NewPassword) < minPasswordLen || len(req.NewPassword) > maxPasswordBytes {
		badRequest(w, "new_password must be 8 to 72 bytes long")
		return
	}
	if req.NewPassword == req.CurrentPassword {
		badRequest(w, "new_password must differ from the current one")
		return
	}

	tokens, err := a.Account.ChangePassword(r.Context(), uid, req.CurrentPassword, req.NewPassword, userAgent(r))
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusForbidden, "invalid_credentials", "current password is incorrect")
	case errors.Is(err, auth.ErrUserNotFound):
		writeError(w, http.StatusUnauthorized, "unauthorized", "account no longer exists")
	case err != nil:
		slog.ErrorContext(r.Context(), "change password", "err", err)
		internalError(w)
	default:
		a.setRefreshCookie(w, tokens.RefreshToken)
		writeJSON(w, http.StatusOK, TokenResponse{AccessToken: tokens.AccessToken, ExpiresIn: int(auth.AccessTokenTTL.Seconds())})
	}
}
