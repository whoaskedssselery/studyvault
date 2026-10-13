package httpapi_test

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"regexp"
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

func TestDocs_ExternalAssetsAreIntegrityChecked(t *testing.T) {
	body := fetch("/docs").Body.String()

	for _, tag := range []string{"<script", "<link"} {
		for _, part := range strings.Split(body, tag)[1:] {
			el, _, _ := strings.Cut(part, ">")
			if !strings.Contains(el, "https://") {
				continue // это встроенный скрипт, а не внешний файл
			}
			if !regexp.MustCompile(`integrity="sha384-[A-Za-z0-9+/]{64}"`).MatchString(el) {
				t.Errorf("an external asset without a valid SRI hash:%s", el)
			}
			if !strings.Contains(el, `crossorigin="anonymous"`) {
				t.Errorf("an SRI-checked asset needs crossorigin=anonymous:%s", el)
			}
		}
	}
}

func TestDocs_DoesNotPersistTheToken(t *testing.T) {
	body := fetch("/docs").Body.String()

	if strings.Contains(body, "persistAuthorization: true") {
		t.Error("the pasted access token must not be kept in localStorage")
	}
}

func TestDocs_CSPAllowsExactlyTheInlineScriptOnThePage(t *testing.T) {
	rec := fetch("/docs")
	csp := rec.Header().Get("Content-Security-Policy")

	m := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindStringSubmatch(rec.Body.String())
	if m == nil {
		t.Fatal("the page has no inline script")
	}
	sum := sha256.Sum256([]byte(m[1]))
	if want := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"; !strings.Contains(csp, want) {
		t.Errorf("CSP does not allow the page's own script by hash %s:\n%s", want, csp)
	}
	scriptSrc := regexp.MustCompile(`script-src [^;]*`).FindString(csp)
	if strings.Contains(scriptSrc, "unsafe-inline") || strings.Contains(scriptSrc, "unsafe-eval") || strings.Contains(scriptSrc, "*") {
		t.Errorf("script-src is too permissive: %q", scriptSrc)
	}
	for _, must := range []string{"default-src 'none'", "frame-ancestors 'none'", "connect-src 'self'", "base-uri 'none'"} {
		if !strings.Contains(csp, must) {
			t.Errorf("CSP lacks %q:\n%s", must, csp)
		}
	}
	for _, host := range regexp.MustCompile(`https://[^"' ;]+`).FindAllString(rec.Body.String(), -1) {
		if !strings.HasPrefix(host, "https://cdn.jsdelivr.net/") {
			t.Errorf("the page loads %s, which the CSP does not allow", host)
		}
	}
}

func TestResponses_AreNotFrameableAndExecuteNothing(t *testing.T) {
	for _, path := range []string{"/health", "/subjects", "/openapi.yaml", "/docs"} {
		h := fetch(path).Header()
		if h.Get("X-Frame-Options") != "DENY" || h.Get("Referrer-Policy") != "no-referrer" || !strings.Contains(h.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Errorf("%s: missing anti-framing or referrer headers: %v", path, h)
		}
	}
	for _, path := range []string{"/health", "/subjects", "/openapi.yaml"} {
		if csp := fetch(path).Header().Get("Content-Security-Policy"); csp != "default-src 'none'; frame-ancestors 'none'" {
			t.Errorf("%s: CSP = %q, want the strict API policy", path, csp)
		}
	}
}
