package httpapi_test

import (
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

// ---------------------------------------------------------------------------
// Подтверждение почты
// ---------------------------------------------------------------------------

func confirmEmail(e accountEnv, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/auth/email-verification/confirm", strings.NewReader(body)))
	return rec
}

func TestConfirmEmail_IsOpenToGuests_AndAnswers204(t *testing.T) {
	e := newAccountEnv(t)

	rec := confirmEmail(e, `{"token":"abc"}`) // без токена доступа: письмо открывают на другом устройстве

	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 || e.acc.verifyToken != "abc" {
		t.Errorf("status = %d, body = %q, service got %q", rec.Code, rec.Body, e.acc.verifyToken)
	}
}

func TestConfirmEmail_BadLinkIs401_BadInputIs400_InternalErrorsAreHidden(t *testing.T) {
	e := newAccountEnv(t)
	for _, err := range []error{auth.ErrInvalidToken, auth.ErrUserNotFound} {
		e.acc.verifyConfirmErr = err
		if rec := confirmEmail(e, `{"token":"x"}`); rec.Code != http.StatusUnauthorized || errorCode(t, rec) != "invalid_token" {
			t.Errorf("%v: status = %d, body = %s", err, rec.Code, rec.Body)
		}
	}
	e.acc.verifyConfirmErr = errors.New("pq: secret internal detail")
	if rec := confirmEmail(e, `{"token":"x"}`); rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("internal error: status = %d, body = %s", rec.Code, rec.Body)
	}

	e = newAccountEnv(t)
	for name, body := range map[string]string{"no token": `{}`, "empty token": `{"token":""}`, "not json": `x`, "extra field": `{"token":"x","y":1}`} {
		if rec := confirmEmail(e, body); rec.Code != http.StatusBadRequest || e.acc.verifyToken != "" {
			t.Errorf("%s: status = %d, the service must not be called", name, rec.Code)
		}
	}
}

func TestRequestEmailVerification_NeedsAToken_AndAsksTheServiceAboutTheCaller(t *testing.T) {
	e := newAccountEnv(t)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/me/email-verification", nil))
	if rec.Code != http.StatusUnauthorized || e.acc.verifyReqCalls != 0 {
		t.Fatalf("without a token: status = %d, calls = %d", rec.Code, e.acc.verifyReqCalls)
	}

	rec = e.do("POST", "/v1/me/email-verification", "")

	if rec.Code != http.StatusNoContent || e.acc.verifyReqFor != e.uid {
		t.Errorf("status = %d, asked for %v, want 204 for %v", rec.Code, e.acc.verifyReqFor, e.uid)
	}
}

func TestRequestEmailVerification_ErrorsAreMappedToTheDocumentedAnswers(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{auth.ErrVerificationUnavailable, 503, "email_unavailable"},
		{auth.ErrUserNotFound, 401, "unauthorized"},
		{&auth.TooSoonError{RetryAfter: 40*time.Second + 300*time.Millisecond}, 429, "rate_limited"},
		{errors.New("pq: secret internal detail"), 500, "internal"},
	}
	for _, c := range cases {
		e := newAccountEnv(t)
		e.acc.verifyReqErr = c.err
		rec := e.do("POST", "/v1/me/email-verification", "")
		if rec.Code != c.status || errorCode(t, rec) != c.code {
			t.Errorf("%v: status = %d code = %q, want %d %q", c.err, rec.Code, errorCode(t, rec), c.status, c.code)
		}
		if strings.Contains(rec.Body.String(), "secret") {
			t.Errorf("%v: the body leaks internals: %s", c.err, rec.Body)
		}
		if c.status == 429 && rec.Header().Get("Retry-After") != "41" {
			t.Errorf("Retry-After = %q, want 41 (rounded up)", rec.Header().Get("Retry-After"))
		}
	}
}

func TestEmailVerification_IsRateLimited_LikeLogin(t *testing.T) {
	uid := uuid.New()
	acc := &fakeAccount{user: db.User{ID: uid}}
	tok, _ := auth.NewAccessToken(testSecret, uid, time.Now())
	h := httpapi.NewHandler(httpapi.Deps{DB: fakePinger{}, Auth: &fakeAuth{}, Account: acc, JWTSecret: testSecret, AuthRateLimit: 2})
	send := func(path, body string) int {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.RemoteAddr = "192.0.2.60:1000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	// запрос письма и подтверждение делят один бюджет с входом: иначе письма можно было бы рассылать без счёта
	a := send("/v1/me/email-verification", "")
	b := send("/v1/auth/email-verification/confirm", `{"token":"t"}`)
	c := send("/v1/me/email-verification", "")
	if a != 204 || b != 204 || c != http.StatusTooManyRequests {
		t.Errorf("statuses = %d %d %d, want 204 204 429", a, b, c)
	}
}

func TestMe_ShowsWhetherTheAddressIsVerified(t *testing.T) {
	for _, verified := range []bool{false, true} {
		e := newAccountEnv(t)
		if verified {
			now := time.Now()
			e.acc.user.EmailVerifiedAt = &now
		}

		body := decodeMap(t, e.do("GET", "/v1/me", ""))

		if got, ok := body["email_verified"].(bool); !ok || got != verified {
			t.Errorf("verified=%v: email_verified = %v", verified, body["email_verified"])
		}
	}
}
