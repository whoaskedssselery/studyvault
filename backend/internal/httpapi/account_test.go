package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/httpapi"
)

type fakeAccount struct {
	user                db.User
	getErr, updateErr   error
	changeErr           error
	gotID               uuid.UUID
	gotName             string
	gotPatch            auth.ProfilePatch
	gotCurrent, gotNext string
	gotUA               string
	changeCalls         int
	updates             int

	resetErr, confirmErr, deleteErr   error
	resetEmail, resetToken, resetPass string
	deletePass                        string
	deleteCalls                       int

	verifyReqErr, verifyConfirmErr error
	verifyReqFor                   uuid.UUID
	verifyReqCalls                 int
	verifyToken                    string
}

func (f *fakeAccount) GetUser(_ context.Context, id uuid.UUID) (db.User, error) {
	f.gotID = id
	return f.user, f.getErr
}

func (f *fakeAccount) UpdateProfile(_ context.Context, id uuid.UUID, p auth.ProfilePatch) (db.User, error) {
	f.gotID, f.gotPatch = id, p
	f.updates++
	u := f.user
	if p.DisplayName != nil {
		u.DisplayName = *p.DisplayName
		f.gotName = *p.DisplayName
	}
	if p.SetFaculty {
		u.Faculty = pgtype.Text{}
		if p.Faculty != nil {
			u.Faculty = pgtype.Text{String: *p.Faculty, Valid: true}
		}
	}
	return u, f.updateErr
}

func (f *fakeAccount) RequestPasswordReset(_ context.Context, email string) error {
	f.resetEmail = email
	return f.resetErr
}

func (f *fakeAccount) ConfirmPasswordReset(_ context.Context, token, pass string) error {
	f.resetToken, f.resetPass = token, pass
	return f.confirmErr
}

func (f *fakeAccount) RequestEmailVerification(_ context.Context, id uuid.UUID) error {
	f.verifyReqFor = id
	f.verifyReqCalls++
	return f.verifyReqErr
}

func (f *fakeAccount) ConfirmEmail(_ context.Context, token string) error {
	f.verifyToken = token
	return f.verifyConfirmErr
}

func (f *fakeAccount) DeleteAccount(_ context.Context, id uuid.UUID, password string) error {
	f.gotID, f.deletePass = id, password
	f.deleteCalls++
	return f.deleteErr
}

func (f *fakeAccount) ChangePassword(_ context.Context, id uuid.UUID, current, next, ua string) (auth.Tokens, error) {
	f.gotID, f.gotCurrent, f.gotNext, f.gotUA = id, current, next, ua
	f.changeCalls++
	if f.changeErr != nil {
		return auth.Tokens{}, f.changeErr
	}
	return auth.Tokens{AccessToken: "fresh.access.token", RefreshToken: "fresh-raw-refresh"}, nil
}

type accountEnv struct {
	h      http.Handler
	acc    *fakeAccount
	uid    uuid.UUID
	bearer string
}

func newAccountEnv(t *testing.T) accountEnv {
	t.Helper()
	uid := uuid.New()
	acc := &fakeAccount{user: db.User{ID: uid, Email: "a@x.io", DisplayName: "Ann", CreatedAt: time.Now()}}
	tok, err := auth.NewAccessToken(testSecret, uid, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	h := httpapi.NewHandler(httpapi.Deps{DB: fakePinger{}, Auth: &fakeAuth{}, Account: acc, JWTSecret: testSecret, CookieSecure: true})
	return accountEnv{h: h, acc: acc, uid: uid, bearer: "Bearer " + tok}
}

func (e accountEnv) do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", e.bearer)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "phone")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func TestAccount_RequireAToken(t *testing.T) {
	e := newAccountEnv(t)
	for _, c := range [][2]string{{"GET", "/v1/me"}, {"PATCH", "/v1/me"}, {"POST", "/v1/me/password"}} {
		req := httptest.NewRequest(c[0], c[1], strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: status = %d, want 401", c[0], c[1], rec.Code)
		}
	}
}

func TestGetMe_ReturnsTheCallersAccount(t *testing.T) {
	e := newAccountEnv(t)

	rec := e.do("GET", "/v1/me", "")

	if rec.Code != http.StatusOK || e.acc.gotID != e.uid {
		t.Fatalf("status = %d, looked up %v, want %v", rec.Code, e.acc.gotID, e.uid)
	}
	var u struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &u)
	if u.ID != e.uid.String() || u.Email != "a@x.io" {
		t.Errorf("body = %s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), "password") {
		t.Errorf("the response must never carry password material: %s", rec.Body)
	}
}

func TestGetMe_AccountGone_Returns401(t *testing.T) {
	e := newAccountEnv(t)
	e.acc.getErr = auth.ErrUserNotFound

	if rec := e.do("GET", "/v1/me", ""); rec.Code != http.StatusUnauthorized || errorCode(t, rec) != "unauthorized" {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestGetMe_StoreError_Returns500WithoutDetails(t *testing.T) {
	e := newAccountEnv(t)
	e.acc.getErr = errors.New("pq: secret internal detail")

	rec := e.do("GET", "/v1/me", "")

	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestUpdateMe_TrimsAndSavesTheName(t *testing.T) {
	e := newAccountEnv(t)

	rec := e.do("PATCH", "/v1/me", `{"display_name":"  Анна Студентова  "}`)

	if rec.Code != http.StatusOK || e.acc.gotName != "Анна Студентова" {
		t.Fatalf("status = %d, saved %q", rec.Code, e.acc.gotName)
	}
	if !strings.Contains(rec.Body.String(), "Анна Студентова") {
		t.Errorf("body = %s", rec.Body)
	}
}

func TestUpdateMe_RejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"empty":       `{"display_name":""}`,
		"blank":       `{"display_name":"   "}`,
		"too long":    `{"display_name":"` + strings.Repeat("я", 101) + `"}`,
		"missing":     `{}`,
		"unknown key": `{"display_name":"Ann","email":"evil@x.io"}`,
		"not json":    `nope`,
	}
	for name, body := range cases {
		e := newAccountEnv(t)
		rec := e.do("PATCH", "/v1/me", body)
		if rec.Code != http.StatusBadRequest || e.acc.updates != 0 {
			t.Errorf("%s: status = %d, updates = %d, want 400 and nothing saved", name, rec.Code, e.acc.updates)
		}
	}
}

func TestUpdateMe_100MultibyteCharsAreAllowed(t *testing.T) {
	e := newAccountEnv(t)
	if rec := e.do("PATCH", "/v1/me", `{"display_name":"`+strings.Repeat("я", 100)+`"}`); rec.Code != http.StatusOK {
		t.Errorf("status = %d: the limit counts characters, not bytes", rec.Code)
	}
}

func TestUpdateMe_AccountGone_Returns401(t *testing.T) {
	e := newAccountEnv(t)
	e.acc.updateErr = auth.ErrUserNotFound

	if rec := e.do("PATCH", "/v1/me", `{"display_name":"X"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestChangePassword_OK_ReturnsNewTokensAndCookie(t *testing.T) {
	e := newAccountEnv(t)

	rec := e.do("POST", "/v1/me/password", `{"current_password":"old-password","new_password":"brand-new-pass"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if e.acc.gotID != e.uid || e.acc.gotCurrent != "old-password" || e.acc.gotNext != "brand-new-pass" || e.acc.gotUA != "phone" {
		t.Errorf("service got %+v", e.acc)
	}
	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.AccessToken != "fresh.access.token" || body.ExpiresIn != 900 {
		t.Errorf("body = %s", rec.Body)
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value != "fresh-raw-refresh" || !cookie.HttpOnly || !cookie.Secure || cookie.Path != "/v1/auth" {
		t.Errorf("refresh cookie = %+v", cookie)
	}
	if strings.Contains(rec.Body.String(), "brand-new-pass") || strings.Contains(rec.Body.String(), "old-password") {
		t.Error("passwords must never be echoed")
	}
}

func TestChangePassword_WrongCurrentPassword_Returns403(t *testing.T) {
	e := newAccountEnv(t)
	e.acc.changeErr = auth.ErrInvalidCredentials

	rec := e.do("POST", "/v1/me/password", `{"current_password":"guess","new_password":"brand-new-pass"}`)

	// 403, а не 401: клиент не должен принять это за просроченный access-токен и начать обновлять сессию.
	if rec.Code != http.StatusForbidden || errorCode(t, rec) != "invalid_credentials" {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Error("a failed change must not touch the cookie")
	}
}

func TestChangePassword_RejectsBadInput_WithoutCallingTheService(t *testing.T) {
	cases := map[string]string{
		"no current":      `{"new_password":"brand-new-pass"}`,
		"empty current":   `{"current_password":"","new_password":"brand-new-pass"}`,
		"short new":       `{"current_password":"old-password","new_password":"short"}`,
		"73 byte new":     `{"current_password":"old-password","new_password":"` + strings.Repeat("a", 73) + `"}`,
		"same as current": `{"current_password":"old-password","new_password":"old-password"}`,
		"no new":          `{"current_password":"old-password"}`,
		"unknown field":   `{"current_password":"old-password","new_password":"brand-new-pass","x":1}`,
		"not json":        `nope`,
	}
	for name, body := range cases {
		e := newAccountEnv(t)
		rec := e.do("POST", "/v1/me/password", body)
		if rec.Code != http.StatusBadRequest || e.acc.changeCalls != 0 {
			t.Errorf("%s: status = %d, calls = %d, want 400 and no call", name, rec.Code, e.acc.changeCalls)
		}
	}
}

func TestChangePassword_72BytesIsTheLimit(t *testing.T) {
	e := newAccountEnv(t)
	pw := strings.Repeat("я", 36) // 72 байта в UTF-8
	if rec := e.do("POST", "/v1/me/password", `{"current_password":"old-password","new_password":"`+pw+`"}`); rec.Code != http.StatusOK {
		t.Errorf("72 bytes: status = %d", rec.Code)
	}
}

func TestChangePassword_AccountGone_Returns401(t *testing.T) {
	e := newAccountEnv(t)
	e.acc.changeErr = auth.ErrUserNotFound
	if rec := e.do("POST", "/v1/me/password", `{"current_password":"old-password","new_password":"brand-new-pass"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestChangePassword_InternalError_HidesDetails(t *testing.T) {
	e := newAccountEnv(t)
	e.acc.changeErr = errors.New("pq: secret internal detail")
	rec := e.do("POST", "/v1/me/password", `{"current_password":"old-password","new_password":"brand-new-pass"}`)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestChangePassword_IsRateLimited(t *testing.T) {
	e := newAccountEnv(t)
	h := httpapi.NewHandler(httpapi.Deps{DB: fakePinger{}, Auth: &fakeAuth{}, Account: e.acc, JWTSecret: testSecret, AuthRateLimit: 2})
	e.acc.changeErr = auth.ErrInvalidCredentials
	send := func() int {
		req := httptest.NewRequest("POST", "/v1/me/password", strings.NewReader(`{"current_password":"guess","new_password":"brand-new-pass"}`))
		req.Header.Set("Authorization", e.bearer)
		req.RemoteAddr = "192.0.2.9:1000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if a, b, c := send(), send(), send(); a != http.StatusForbidden || b != http.StatusForbidden || c != http.StatusTooManyRequests {
		t.Errorf("statuses = %d %d %d, want 403 403 429: a stolen access token must not allow unlimited password guessing", a, b, c)
	}
}

func TestGetMe_ReturnsRoleAndFaculty(t *testing.T) {
	e := newAccountEnv(t)
	e.acc.user.Role = "moderator"
	e.acc.user.Faculty = pgtype.Text{String: "Information Technology", Valid: true}

	var u struct {
		Role    string  `json:"role"`
		Faculty *string `json:"faculty"`
	}
	_ = json.Unmarshal(e.do("GET", "/v1/me", "").Body.Bytes(), &u)
	if u.Role != "moderator" || u.Faculty == nil || *u.Faculty != "Information Technology" {
		t.Errorf("role = %q, faculty = %v", u.Role, u.Faculty)
	}

	e.acc.user.Faculty = pgtype.Text{}
	body := e.do("GET", "/v1/me", "").Body.String()
	if !strings.Contains(body, `"faculty":null`) {
		t.Errorf("a missing faculty must be an explicit null: %s", body)
	}
}

func TestUpdateMe_Faculty_SetTrimClearAndReject(t *testing.T) {
	e := newAccountEnv(t)

	if rec := e.do("PATCH", "/v1/me", `{"faculty":"  Physics  "}`); rec.Code != http.StatusOK || !e.acc.gotPatch.SetFaculty || e.acc.gotPatch.Faculty == nil || *e.acc.gotPatch.Faculty != "Physics" {
		t.Fatalf("set: status = %d, patch = %+v", rec.Code, e.acc.gotPatch)
	}
	if e.acc.gotPatch.DisplayName != nil {
		t.Error("the name was not sent and must stay untouched")
	}
	if rec := e.do("PATCH", "/v1/me", `{"faculty":null}`); rec.Code != http.StatusOK || !e.acc.gotPatch.SetFaculty || e.acc.gotPatch.Faculty != nil {
		t.Errorf("null must clear the faculty: status = %d, patch = %+v", rec.Code, e.acc.gotPatch)
	}
	before := e.acc.updates
	for _, bad := range []string{`{"faculty":""}`, `{"faculty":"   "}`, `{"faculty":"` + strings.Repeat("я", 101) + `"}`} {
		if rec := e.do("PATCH", "/v1/me", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", bad, rec.Code)
		}
	}
	if e.acc.updates != before {
		t.Error("an invalid faculty must not reach the service")
	}
	if rec := e.do("PATCH", "/v1/me", `{"role":"moderator"}`); rec.Code != http.StatusBadRequest || e.acc.updates != before {
		t.Errorf("a role can never be set through the API: status = %d", rec.Code)
	}
	if rec := e.do("PATCH", "/v1/me", `{"display_name":"Ann","faculty":"Law"}`); rec.Code != http.StatusOK || *e.acc.gotPatch.DisplayName != "Ann" || *e.acc.gotPatch.Faculty != "Law" {
		t.Errorf("both at once: status = %d, patch = %+v", rec.Code, e.acc.gotPatch)
	}
}
