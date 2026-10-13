package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// Все эндпоинты контракта живут под /v1; пути без префикса, кроме пробы живости, больше не существуют.
func TestVersioning_UnprefixedPathsAreGone(t *testing.T) {
	h := newHandler(&fakeAuth{})
	for _, c := range [][2]string{
		{http.MethodPost, "/auth/login"},
		{http.MethodPost, "/auth/register"},
		{http.MethodPost, "/auth/refresh"},
		{http.MethodGet, "/subjects"},
		{http.MethodGet, "/notes"},
		{http.MethodGet, "/me"},
		{http.MethodGet, "/search"},
		{http.MethodGet, "/graph"},
	} {
		rec := request(h, c[0], c[1], "", nil)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status = %d, want 404 (the API lives under /v1)", c[0], c[1], rec.Code)
		}
	}
}

func TestVersioning_V2IsNotServedYet(t *testing.T) {
	if rec := request(newHandler(&fakeAuth{}), http.MethodPost, "/v2/auth/login", "", nil); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestVersioning_HealthIsAvailableWithAndWithoutThePrefix(t *testing.T) {
	for _, path := range []string{"/health", "/v1/health"} {
		rec := request(newHandler(&fakeAuth{}), http.MethodGet, path, "", nil)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok"`) {
			t.Errorf("%s: status = %d, body = %s", path, rec.Code, rec.Body)
		}
	}
}

func TestVersioning_RefreshCookieIsScopedToTheVersionedAuthPath(t *testing.T) {
	rec := post(t, newHandler(&fakeAuth{}), "/v1/auth/login", `{"email":"a@x.io","password":"password123"}`)

	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" {
			if c.Path != "/v1/auth" {
				t.Errorf("cookie path = %q, want /v1/auth: the browser would not send it to the refresh endpoint otherwise", c.Path)
			}
			return
		}
	}
	t.Fatal("no refresh cookie")
}

func TestVersioning_DocsAndSpecStayUnversioned(t *testing.T) {
	for _, path := range []string{"/docs", "/openapi.yaml"} {
		if rec := fetch(path); rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d", path, rec.Code)
		}
	}
	if !strings.Contains(fetch("/openapi.yaml").Body.String(), "- url: /v1") {
		t.Error("the contract must declare /v1 as its server so that Swagger UI sends requests there")
	}
}
