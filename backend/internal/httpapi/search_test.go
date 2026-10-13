package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/httpapi"
	"github.com/whoaskedssselery/studyvault/backend/internal/search"
)

type fakeSearch struct {
	results []search.Result
	err     error

	calls   int
	gotUser uuid.UUID
	gotQ    search.Query
}

func (f *fakeSearch) Search(_ context.Context, uid uuid.UUID, q search.Query) ([]search.Result, error) {
	f.calls++
	f.gotUser, f.gotQ = uid, q
	return f.results, f.err
}

type searchEnv struct {
	h    http.Handler
	f    *fakeSearch
	uid  uuid.UUID
	auth string
}

func newSearchEnv(f *fakeSearch) searchEnv {
	uid := uuid.New()
	tok, _ := auth.NewAccessToken(testSecret, uid, time.Now())
	deps := httpapi.Deps{DB: fakePinger{}, Auth: &fakeAuth{}, JWTSecret: testSecret}
	if f != nil {
		deps.Search = f
	}
	return searchEnv{h: httpapi.NewHandler(deps), f: f, uid: uid, auth: "Bearer " + tok}
}

func (e searchEnv) get(query string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/search?"+query, nil)
	if e.auth != "" {
		req.Header.Set("Authorization", e.auth)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func TestSearch_OK_MapsResults(t *testing.T) {
	sid := uuid.New()
	hit := search.Result{ID: uuid.New(), Title: "Algebra", Snippet: "rings and <mark>fields</mark>", UpdatedAt: time.Now(), SubjectID: &sid}
	bare := search.Result{ID: uuid.New(), Title: "Loose", Snippet: "x", UpdatedAt: time.Now()}
	e := newSearchEnv(&fakeSearch{results: []search.Result{hit, bare}})

	rec := e.get("q=fields")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	items := decode(t, rec)["items"].([]any)
	first, second := items[0].(map[string]any), items[1].(map[string]any)
	if first["title"] != "Algebra" || first["snippet"] != "rings and <mark>fields</mark>" || first["subject_id"] != sid.String() {
		t.Errorf("first: %v", first)
	}
	if v, present := second["subject_id"]; !present || v != nil {
		t.Errorf("subject_id must be present and null, got %v (present=%v)", v, present)
	}
	if e.f.gotUser != e.uid || e.f.gotQ.Text != "fields" {
		t.Errorf("service got user ok=%v, query %q", e.f.gotUser == e.uid, e.f.gotQ.Text)
	}
}

func TestSearch_NoResults_ReturnsEmptyArray(t *testing.T) {
	rec := newSearchEnv(&fakeSearch{}).get("q=nothing")

	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"items":[]}` {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestSearch_PassesFiltersAndTrimsTheQuery(t *testing.T) {
	sid := uuid.New()
	e := newSearchEnv(&fakeSearch{})

	rec := e.get("q=" + url.QueryEscape("  Привет мир  ") + "&subject_id=" + sid.String() + "&tag=exam&limit=7")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	q := e.f.gotQ
	if q.Text != "Привет мир" || q.SubjectID == nil || *q.SubjectID != sid || q.Tag == nil || *q.Tag != "exam" || q.Limit != 7 {
		t.Errorf("query: %+v", q)
	}
}

func TestSearch_BadParams_Return400AndSkipTheService(t *testing.T) {
	cases := map[string]string{
		"missing q":          "",
		"empty q":            "q=",
		"blank q":            "q=" + url.QueryEscape("   "),
		"q too long":         "q=" + strings.Repeat("я", 201),
		"limit zero":         "q=x&limit=0",
		"limit too big":      "q=x&limit=51",
		"limit not a number": "q=x&limit=abc",
		"bad subject id":     "q=x&subject_id=nope",
		"empty tag":          "q=x&tag=",
	}
	for name, query := range cases {
		t.Run(name, func(t *testing.T) {
			e := newSearchEnv(&fakeSearch{})
			rec := e.get(query)

			if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "validation_failed" {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
			}
			if e.f.calls != 0 {
				t.Error("service must not be called")
			}
		})
	}
}

func TestSearch_BoundaryValues_AreAccepted(t *testing.T) {
	for _, query := range []string{"q=x&limit=1", "q=x&limit=50", "q=" + strings.Repeat("я", 200)} {
		e := newSearchEnv(&fakeSearch{})
		if rec := e.get(query); rec.Code != http.StatusOK {
			t.Errorf("%.40s: status = %d, want 200", query, rec.Code)
		}
	}
}

func TestSearch_EngineDown_Returns503(t *testing.T) {
	e := newSearchEnv(&fakeSearch{err: search.ErrUnavailable})
	rec := e.get("q=x")

	if rec.Code != http.StatusServiceUnavailable || errorCode(t, rec) != "search_unavailable" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestSearch_NotConfigured_Returns503(t *testing.T) {
	rec := newSearchEnv(nil).get("q=x")

	if rec.Code != http.StatusServiceUnavailable || errorCode(t, rec) != "search_unavailable" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestSearch_InternalError_Returns500WithoutLeak(t *testing.T) {
	rec := newSearchEnv(&fakeSearch{err: errors.New("pq: secret internal detail")}).get("q=x")

	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret internal detail") {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestSearch_RequiresAuth(t *testing.T) {
	e := newSearchEnv(&fakeSearch{})
	e.auth = ""

	if rec := e.get("q=x"); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
	if e.f.calls != 0 {
		t.Error("service must not be called without auth")
	}
}
