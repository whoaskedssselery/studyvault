package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/whoaskedssselery/studyvault/backend/internal/httpapi"
)

func fetch(path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	httpapi.NewHandler(httpapi.Deps{DB: fakePinger{}, JWTSecret: testSecret}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestDocs_ServesSwaggerUIWithoutAuth(t *testing.T) {
	rec := fetch("/docs")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 without a token", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "swagger-ui") || !strings.Contains(body, "/openapi.yaml") {
		t.Error("the page must load Swagger UI pointed at /openapi.yaml")
	}
}

func TestOpenAPISpec_IsServedWithoutAuth(t *testing.T) {
	rec := fetch("/openapi.yaml")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "openapi: 3.0.3") || !strings.Contains(body, "/notes/{id}") {
		t.Errorf("not the project's spec: %.80s", body)
	}
}
