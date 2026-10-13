package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/whoaskedssselery/studyvault/backend/internal/httpapi"
)

func handlerWithOrigins(origins ...string) http.Handler {
	return httpapi.NewHandler(httpapi.Deps{DB: fakePinger{}, Auth: &fakeAuth{}, JWTSecret: testSecret, AllowedOrigins: origins})
}

func request(h http.Handler, method, path, origin string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCORS_AllowedOrigin_GetsCredentialedHeaders(t *testing.T) {
	h := handlerWithOrigins("http://localhost:5173")

	rec := request(h, http.MethodGet, "/v1/health", "http://localhost:5173", nil)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("Allow-Origin = %q, want the exact origin", got)
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("the refresh cookie needs Allow-Credentials: true")
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
		t.Error("responses depend on Origin, so Vary must say so")
	}
}

func TestCORS_UnknownOrigin_GetsNothing(t *testing.T) {
	h := handlerWithOrigins("http://localhost:5173")

	for _, origin := range []string{"http://evil.example", "http://localhost:5174", "https://localhost:5173", "null"} {
		rec := request(h, http.MethodGet, "/v1/health", origin, nil)
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("origin %q must not be allowed", origin)
		}
	}
}

func TestCORS_Preflight_IsAnsweredWithoutAuth(t *testing.T) {
	h := handlerWithOrigins("http://localhost:5173")

	rec := request(h, http.MethodOptions, "/v1/notes", "http://localhost:5173", map[string]string{
		"Access-Control-Request-Method":  "POST",
		"Access-Control-Request-Headers": "authorization,content-type",
	})

	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d (a browser sends it without a token, so it must not be 401)", rec.Code)
	}
	headers := strings.ToLower(rec.Header().Get("Access-Control-Allow-Headers"))
	// Библиотека подтверждает ровно тот метод, о котором спросил браузер, поэтому проверяем каждый отдельно.
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		p := request(h, http.MethodOptions, "/v1/notes", "http://localhost:5173", map[string]string{"Access-Control-Request-Method": m})
		if got := p.Header().Get("Access-Control-Allow-Methods"); got != m {
			t.Errorf("preflight for %s: Allow-Methods = %q, want it approved", m, got)
		}
	}
	if rec.Header().Get("Access-Control-Allow-Methods") != "POST" {
		t.Errorf("the requested method must be approved, got %q", rec.Header().Get("Access-Control-Allow-Methods"))
	}
	if !strings.Contains(headers, "authorization") || !strings.Contains(headers, "content-type") {
		t.Errorf("Allow-Headers = %q", headers)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Error("the preflight must name the origin")
	}
}

func TestCORS_PreflightFromUnknownOrigin_IsNotApproved(t *testing.T) {
	rec := request(handlerWithOrigins("http://localhost:5173"), http.MethodOptions, "/v1/notes", "http://evil.example", map[string]string{
		"Access-Control-Request-Method": "POST",
	})

	if rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Errorf("a stranger's preflight must not be approved: %v", rec.Header())
	}
}

func TestCORS_NotConfigured_AddsNoHeaders(t *testing.T) {
	rec := request(handlerWithOrigins(), http.MethodGet, "/v1/health", "http://localhost:5173", nil)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("without a configured list no origin is allowed")
	}
}

func TestSecurityHeaders_OnEveryResponse(t *testing.T) {
	h := handlerWithOrigins()

	for _, path := range []string{"/v1/health", "/nope", "/v1/notes", "/docs", "/openapi.yaml"} {
		rec := request(h, http.MethodGet, path, "", nil)
		if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: missing X-Content-Type-Options: nosniff", path)
		}
	}
}

func TestAPIResponses_AreNotCached_ButDocsAre(t *testing.T) {
	h := handlerWithOrigins()

	for _, path := range []string{"/v1/health", "/v1/notes", "/nope"} {
		if cc := request(h, http.MethodGet, path, "", nil).Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: Cache-Control = %q, want no-store (responses carry tokens and personal data)", path, cc)
		}
	}
	for _, path := range []string{"/docs", "/openapi.yaml"} {
		if cc := request(h, http.MethodGet, path, "", nil).Header().Get("Cache-Control"); cc == "no-store" {
			t.Errorf("%s: the static documentation may be cached", path)
		}
	}
}

// Из чужого источника браузер отдаёт скрипту только базовые заголовки ответа. Без Expose-Headers фронтенд не смог бы
// показать пользователю id запроса для обращения в поддержку и узнать, через сколько повторить после 429.
func TestCORS_ExposesRequestIDAndRetryAfter_AndAcceptsRequestIDFromTheClient(t *testing.T) {
	h := handlerWithOrigins("http://localhost:5173")

	rec := request(h, http.MethodGet, "/v1/health", "http://localhost:5173", nil)
	exposed := strings.ToLower(rec.Header().Get("Access-Control-Expose-Headers"))
	if !strings.Contains(exposed, "x-request-id") || !strings.Contains(exposed, "retry-after") {
		t.Errorf("Expose-Headers = %q, want X-Request-ID and Retry-After", exposed)
	}

	pre := request(h, http.MethodOptions, "/v1/notes", "http://localhost:5173", map[string]string{
		"Access-Control-Request-Method":  "GET",
		"Access-Control-Request-Headers": "authorization,x-request-id",
	})
	if !strings.Contains(strings.ToLower(pre.Header().Get("Access-Control-Allow-Headers")), "x-request-id") {
		t.Errorf("a client-chosen X-Request-ID must pass the preflight: %v", pre.Header())
	}
}
