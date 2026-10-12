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

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/httpapi"
)

var testSecret = []byte("test-secret-test-secret-test-secret")

type fakeAuth struct {
	registerErr, loginErr error
	calls                 int
	lastEmail, lastUA     string
}

func (f *fakeAuth) result() auth.Result {
	return auth.Result{
		User:         db.User{ID: uuid.New(), Email: "a@x.io", DisplayName: "Ann", CreatedAt: time.Now()},
		AccessToken:  "access.jwt.token",
		RefreshToken: "raw-refresh-token",
	}
}

func (f *fakeAuth) Register(_ context.Context, email, _, _, ua string) (auth.Result, error) {
	f.calls++
	f.lastEmail, f.lastUA = email, ua
	if f.registerErr != nil {
		return auth.Result{}, f.registerErr
	}
	return f.result(), nil
}

func (f *fakeAuth) Login(_ context.Context, email, _, ua string) (auth.Result, error) {
	f.calls++
	f.lastEmail, f.lastUA = email, ua
	if f.loginErr != nil {
		return auth.Result{}, f.loginErr
	}
	return f.result(), nil
}

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "test-agent")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func get(h http.Handler, path, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func newHandler(a httpapi.Authenticator) http.Handler {
	return httpapi.NewHandler(httpapi.Deps{DB: fakePinger{}, Auth: a, JWTSecret: testSecret})
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not an error JSON: %v (%s)", err, rec.Body.String())
	}
	return body.Error.Code
}

const validRegister = `{"email":"a@x.io","password":"password123","display_name":"Ann"}`

func TestRegister_OK_Returns201AndSetsRefreshCookie(t *testing.T) {
	f := &fakeAuth{}
	rec := post(t, newHandler(f), "/auth/register", validRegister)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		User        struct {
			Email       string `json:"email"`
			DisplayName string `json:"display_name"`
		} `json:"user"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body.AccessToken != "access.jwt.token" || body.ExpiresIn != 900 || body.User.Email != "a@x.io" {
		t.Errorf("unexpected body: %s", rec.Body)
	}
	if strings.Contains(rec.Body.String(), "raw-refresh-token") || strings.Contains(rec.Body.String(), "password_hash") {
		t.Error("body must not contain the refresh token or password hash")
	}

	var c *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "refresh_token" {
			c = ck
		}
	}
	if c == nil {
		t.Fatal("refresh_token cookie is not set")
	}
	if c.Value != "raw-refresh-token" || !c.HttpOnly || c.Path != "/auth" || c.SameSite != http.SameSiteLaxMode {
		t.Errorf("bad cookie: %+v", c)
	}
	if f.lastUA != "test-agent" {
		t.Errorf("user agent passed = %q", f.lastUA)
	}
}

func TestRegister_DuplicateEmail_Returns409(t *testing.T) {
	rec := post(t, newHandler(&fakeAuth{registerErr: auth.ErrEmailTaken}), "/auth/register", validRegister)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if code := errorCode(t, rec); code != "email_taken" {
		t.Errorf("code = %q", code)
	}
}

func TestRegister_InvalidBody_Returns400AndSkipsService(t *testing.T) {
	cases := map[string]string{
		"short password":  `{"email":"a@x.io","password":"short","display_name":"Ann"}`,
		"password > 72 B": `{"email":"a@x.io","password":"` + strings.Repeat("a", 73) + `","display_name":"Ann"}`,
		"bad email":       `{"email":"not-an-email","password":"password123","display_name":"Ann"}`,
		"missing email":   `{"password":"password123","display_name":"Ann"}`,
		"empty name":      `{"email":"a@x.io","password":"password123","display_name":"   "}`,
		"unknown field":   `{"email":"a@x.io","password":"password123","display_name":"Ann","admin":true}`,
		"malformed json":  `{"email":`,
		"empty body":      ``,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			f := &fakeAuth{}
			rec := post(t, newHandler(f), "/auth/register", body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
			}
			if code := errorCode(t, rec); code != "validation_failed" {
				t.Errorf("code = %q", code)
			}
			if f.calls != 0 {
				t.Error("service must not be called for invalid input")
			}
		})
	}
}

func TestRegister_EmailWithDisplayName_IsReducedToBareAddress(t *testing.T) {
	f := &fakeAuth{}
	rec := post(t, newHandler(f), "/auth/register", `{"email":"Ann <a@x.io>","password":"password123","display_name":"Ann"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if f.lastEmail != "a@x.io" {
		t.Errorf("service got email %q, want the bare address", f.lastEmail)
	}
}

func TestRegister_ServiceError_Returns500WithoutLeak(t *testing.T) {
	rec := post(t, newHandler(&fakeAuth{registerErr: errors.New("pq: secret internal detail")}), "/auth/register", validRegister)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret internal detail") {
		t.Error("response leaks the internal error")
	}
}

func TestLogin_OK_Returns200AndSetsCookie(t *testing.T) {
	rec := post(t, newHandler(&fakeAuth{}), "/auth/login", `{"email":"a@x.io","password":"password123"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if len(rec.Result().Cookies()) != 1 {
		t.Error("expected the refresh cookie")
	}
}

func TestLogin_WrongPassword_Returns401(t *testing.T) {
	rec := post(t, newHandler(&fakeAuth{loginErr: auth.ErrInvalidCredentials}), "/auth/login", `{"email":"a@x.io","password":"nope"}`)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if code := errorCode(t, rec); code != "invalid_credentials" {
		t.Errorf("code = %q", code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Error("no cookie may be set on failure")
	}
}

func TestLogin_MissingFields_Returns400(t *testing.T) {
	for _, body := range []string{`{}`, `{"email":"a@x.io"}`, `{"password":"x"}`} {
		rec := post(t, newHandler(&fakeAuth{}), "/auth/login", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, rec.Code)
		}
	}
}

func TestProtectedRoute_RequiresValidAccessToken(t *testing.T) {
	h := newHandler(&fakeAuth{})
	valid, _ := auth.NewAccessToken(testSecret, uuid.New(), time.Now())
	expired, _ := auth.NewAccessToken(testSecret, uuid.New(), time.Now().Add(-time.Hour))
	foreign, _ := auth.NewAccessToken([]byte("another-secret-another-secret-123"), uuid.New(), time.Now())

	cases := []struct {
		name, header string
		want         int
	}{
		{"no header", "", http.StatusUnauthorized},
		{"not bearer", "Basic abc", http.StatusUnauthorized},
		{"garbage", "Bearer garbage", http.StatusUnauthorized},
		{"expired", "Bearer " + expired, http.StatusUnauthorized},
		{"wrong secret", "Bearer " + foreign, http.StatusUnauthorized},
		{"valid passes the middleware", "Bearer " + valid, http.StatusNotImplemented},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := get(h, "/subjects", c.header)
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d", rec.Code, c.want)
			}
			if c.want == http.StatusUnauthorized && errorCode(t, rec) != "unauthorized" {
				t.Errorf("code = %q", errorCode(t, rec))
			}
		})
	}
}

func TestPublicRoutes_NeedNoToken(t *testing.T) {
	h := newHandler(&fakeAuth{})

	if rec := get(h, "/health", ""); rec.Code != http.StatusOK {
		t.Errorf("/health status = %d", rec.Code)
	}
	if rec := post(t, h, "/auth/login", `{"email":"a@x.io","password":"x"}`); rec.Code == http.StatusUnauthorized {
		t.Error("/auth/login must not require a token")
	}
}
