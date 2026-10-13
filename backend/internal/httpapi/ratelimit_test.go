package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/httpapi"
)

func limitedHandler(perMinute int) http.Handler {
	return httpapi.NewHandler(httpapi.Deps{
		DB: fakePinger{}, JWTSecret: testSecret, AuthRateLimit: perMinute,
		Auth: &fakeAuth{loginErr: auth.ErrInvalidCredentials},
	})
}

func attempt(h http.Handler, method, path, remoteAddr, header string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(`{"email":"a@x.io","password":"wrong"}`))
	req.RemoteAddr = remoteAddr
	if header != "" {
		req.Header.Set("X-Forwarded-For", header)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthRateLimit_StopsPasswordGuessing(t *testing.T) {
	h := limitedHandler(3)

	for i := 1; i <= 3; i++ {
		if rec := attempt(h, http.MethodPost, "/auth/login", "192.0.2.1:5000", ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401 (a wrong password, not yet limited)", i, rec.Code)
		}
	}
	rec := attempt(h, http.MethodPost, "/auth/login", "192.0.2.1:5000", "")

	if rec.Code != http.StatusTooManyRequests || errorCode(t, rec) != "rate_limited" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	secs, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || secs < 1 || secs > 60 {
		t.Errorf("Retry-After = %q, want 1..60 seconds", rec.Header().Get("Retry-After"))
	}
}

func TestAuthRateLimit_BudgetIsSharedByLoginRegisterAndRefresh(t *testing.T) {
	h := limitedHandler(2)

	attempt(h, http.MethodPost, "/auth/login", "192.0.2.1:1", "")
	attempt(h, http.MethodPost, "/auth/register", "192.0.2.1:2", "") // другой порт, тот же клиент

	if rec := attempt(h, http.MethodPost, "/auth/refresh", "192.0.2.1:3", ""); rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429: a client must not get a fresh budget per endpoint or per source port", rec.Code)
	}
}

func TestAuthRateLimit_OtherClientsAreNotAffected(t *testing.T) {
	h := limitedHandler(1)
	attempt(h, http.MethodPost, "/auth/login", "192.0.2.1:1", "")

	if rec := attempt(h, http.MethodPost, "/auth/login", "192.0.2.1:2", ""); rec.Code != http.StatusTooManyRequests {
		t.Fatal("the first client must be limited")
	}
	if rec := attempt(h, http.MethodPost, "/auth/login", "198.51.100.7:1", ""); rec.Code == http.StatusTooManyRequests {
		t.Error("a different client must not be limited by someone else's attempts")
	}
}

func TestAuthRateLimit_SpoofedForwardedHeaderGivesNoFreshBudget(t *testing.T) {
	h := limitedHandler(1)
	attempt(h, http.MethodPost, "/auth/login", "192.0.2.1:1", "")

	rec := attempt(h, http.MethodPost, "/auth/login", "192.0.2.1:2", "203.0.113.99")

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429: the limit is keyed by the real connection, not by a header the client controls", rec.Code)
	}
}

func TestAuthRateLimit_OnlyCountsTheAuthEndpoints(t *testing.T) {
	h := limitedHandler(1)

	for i := 0; i < 5; i++ {
		if rec := attempt(h, http.MethodGet, "/health", "192.0.2.1:1", ""); rec.Code != http.StatusOK {
			t.Fatalf("/health must never be limited, status = %d", rec.Code)
		}
	}
	if rec := attempt(h, http.MethodPost, "/auth/logout", "192.0.2.1:1", ""); rec.Code == http.StatusTooManyRequests {
		t.Error("logout is not a guessing target and must not be limited")
	}
	if rec := attempt(h, http.MethodPost, "/auth/login", "192.0.2.1:1", ""); rec.Code == http.StatusTooManyRequests {
		t.Error("the other traffic must not have used up the login budget")
	}
}

func TestAuthRateLimit_ZeroDisablesIt(t *testing.T) {
	h := limitedHandler(0)

	for i := 0; i < 50; i++ {
		if rec := attempt(h, http.MethodPost, "/auth/login", "192.0.2.1:1", ""); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d was limited although the limit is off", i+1)
		}
	}
}
