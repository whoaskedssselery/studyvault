package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/oapi-codegen/runtime/types"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
)

const (
	refreshCookieName = "refresh_token"
	refreshCookiePath = "/auth"
	maxBodyBytes      = 1 << 20
	maxEmailLen       = 254
	minPasswordLen    = 8
	maxPasswordBytes  = 72 // предел bcrypt
	maxDisplayNameLen = 100
)

func (a *api) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	if msg := validateRegister(req); msg != "" {
		badRequest(w, msg)
		return
	}

	res, err := a.Auth.Register(r.Context(), string(req.Email), req.Password, req.DisplayName, userAgent(r))
	switch {
	case errors.Is(err, auth.ErrEmailTaken):
		writeError(w, http.StatusConflict, "email_taken", "email is already registered")
	case err != nil:
		slog.ErrorContext(r.Context(), "register", "err", err)
		internalError(w)
	default:
		a.respondWithSession(w, http.StatusCreated, res)
	}
}

func (a *api) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	if string(req.Email) == "" || req.Password == "" {
		badRequest(w, "email and password are required")
		return
	}

	res, err := a.Auth.Login(r.Context(), string(req.Email), req.Password, userAgent(r))
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
	case err != nil:
		slog.ErrorContext(r.Context(), "login", "err", err)
		internalError(w)
	default:
		a.respondWithSession(w, http.StatusOK, res)
	}
}

func (a *api) Refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(refreshCookieName)
	if err != nil || cookie.Value == "" {
		writeError(w, http.StatusUnauthorized, "invalid_token", "refresh token is missing")
		return
	}

	tokens, err := a.Auth.Refresh(r.Context(), cookie.Value, userAgent(r))
	switch {
	case errors.Is(err, auth.ErrInvalidToken):
		a.clearRefreshCookie(w)
		writeError(w, http.StatusUnauthorized, "invalid_token", "refresh token is invalid or expired")
	case err != nil:
		slog.ErrorContext(r.Context(), "refresh", "err", err)
		internalError(w)
	default:
		a.setRefreshCookie(w, tokens.RefreshToken)
		writeJSON(w, http.StatusOK, TokenResponse{
			AccessToken: tokens.AccessToken,
			ExpiresIn:   int(auth.AccessTokenTTL.Seconds()),
		})
	}
}

func (a *api) Logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(refreshCookieName); err == nil {
		if err := a.Auth.Logout(r.Context(), cookie.Value); err != nil {
			slog.ErrorContext(r.Context(), "logout", "err", err)
			internalError(w)
			return
		}
	}
	a.clearRefreshCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) setRefreshCookie(w http.ResponseWriter, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    value,
		Path:     refreshCookiePath,
		MaxAge:   int(auth.RefreshTokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   a.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *api) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Path:     refreshCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   a.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *api) respondWithSession(w http.ResponseWriter, status int, res auth.Result) {
	a.setRefreshCookie(w, res.RefreshToken)
	writeJSON(w, status, AuthResponse{
		AccessToken: res.AccessToken,
		ExpiresIn:   int(auth.AccessTokenTTL.Seconds()),
		User:        toUser(res.User),
	})
}

func toUser(u db.User) User {
	return User{Id: u.ID, Email: types.Email(u.Email), DisplayName: u.DisplayName, CreatedAt: u.CreatedAt}
}

func validateRegister(req RegisterRequest) string {
	// Формат адреса уже проверил types.Email при разборе JSON; здесь только пустое значение и длина.
	if email := string(req.Email); email == "" || len(email) > maxEmailLen {
		return "email is not valid"
	}
	if len(req.Password) < minPasswordLen || len(req.Password) > maxPasswordBytes {
		return "password must be 8 to 72 bytes long"
	}
	if n := utf8.RuneCountInString(req.DisplayName); n < 1 || n > maxDisplayNameLen {
		return "display_name must be 1 to 100 characters"
	}
	return ""
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("unexpected data after JSON body")
	}
	return nil
}

// userAgent отдаёт заголовок как есть: очищает и ограничивает его auth.Service, прямо перед записью в базу.
func userAgent(r *http.Request) string { return r.UserAgent() }
