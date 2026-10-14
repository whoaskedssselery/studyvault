package httpapi_test

import (
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
)

// Каталог ошибок обязан совпадать с кодом: каждый код, который сервер может вернуть, описан в docs/errors.md,
// и в каталоге нет описаний кодов, которых сервер уже не возвращает.
func TestErrorCatalog_MatchesTheCodesTheServerCanReturn(t *testing.T) {
	doc, err := os.ReadFile("../../../docs/errors.md")
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string]bool{}
	text := strings.ReplaceAll(string(doc), "\r\n", "\n") // на Windows git кладёт CRLF
	for _, m := range regexp.MustCompile(`(?m)^## ([a-z_]+)$`).FindAllStringSubmatch(text, -1) {
		documented[m[1]] = true
	}

	used := map[string]bool{"validation_failed": true, "internal": true, "not_found": true, "limit_reached": true, "unauthorized": true}
	files, _ := os.ReadDir(".")
	call := regexp.MustCompile(`writeError\(\s*w,\s*[^,]+,\s*"([a-z_]+)"`)
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".go") || strings.HasSuffix(f.Name(), "_test.go") || f.Name() == "api.gen.go" {
			continue
		}
		src, _ := os.ReadFile(f.Name())
		for _, m := range call.FindAllStringSubmatch(string(src), -1) {
			used[m[1]] = true
		}
	}

	for code := range used {
		if !documented[code] {
			t.Errorf("the server can return %q, but docs/errors.md does not describe it", code)
		}
	}
	var stale []string
	for code := range documented {
		if !used[code] {
			stale = append(stale, code)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("docs/errors.md describes codes the server never returns: %v", stale)
	}

	spec := fetch("/openapi.yaml").Body.String()
	for code := range used {
		if !strings.Contains(spec, "`"+code+"`") {
			t.Errorf("the contract's error table does not mention %q", code)
		}
	}
}

func TestUnknownRouteAndWrongMethod_AnswerWithTheAPIErrorFormat(t *testing.T) {
	h := newHandler(&fakeAuth{})

	rec := request(h, "GET", "/v1/no/such/route", "", map[string]string{"Authorization": "Bearer " + validToken(t)})
	if rec.Code != 404 || problem(t, rec).Code != "not_found" {
		t.Errorf("unknown route: status = %d, body = %s", rec.Code, rec.Body)
	}
	rec = request(h, "DELETE", "/v1/auth/login", "", nil)
	if rec.Code != 405 || problem(t, rec).Code != "method_not_allowed" {
		t.Errorf("wrong method: status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestProblem_CarriesTheRequestID(t *testing.T) {
	rec := request(newHandler(&fakeAuth{}), "GET", "/v1/subjects", "", map[string]string{"X-Request-ID": "trace-77"})

	p := problem(t, rec)

	if p.RequestId == nil || *p.RequestId != "trace-77" || rec.Header().Get("X-Request-ID") != "trace-77" {
		t.Errorf("request_id = %v, header = %q: the body and the header must carry the same id", p.RequestId, rec.Header().Get("X-Request-ID"))
	}
	if rec.Code != 401 || p.Code != "unauthorized" {
		t.Errorf("status = %d, code = %q", rec.Code, p.Code)
	}
}

func validToken(t *testing.T) string {
	t.Helper()
	tok, err := auth.NewAccessToken(testSecret, uuid.New(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// Проверка токена идёт до разбора параметров пути: без токена нельзя ни узнать, какие id «правильные», ни
// выяснить, существует ли маршрут.
func TestAuth_ComesBeforeParameterParsingAndRouting(t *testing.T) {
	h := newHandler(&fakeAuth{})
	for _, path := range []string{"/v1/notes/not-a-uuid", "/v1/notes?limit=banana", "/v1/no/such/route", "/v1/subjects/xyz"} {
		rec := request(h, "GET", path, "", nil)
		if rec.Code != http.StatusUnauthorized || errorCode(t, rec) != "unauthorized" {
			t.Errorf("GET %s without a token: status = %d, want 401", path, rec.Code)
		}
	}
	if rec := request(h, "GET", "/v1/notes/not-a-uuid", "", map[string]string{"Authorization": "Bearer " + validToken(t)}); rec.Code != http.StatusBadRequest {
		t.Errorf("with a token the bad id is a 400, got %d", rec.Code)
	}
}
