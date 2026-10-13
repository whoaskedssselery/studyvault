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
	"github.com/whoaskedssselery/studyvault/backend/internal/subject"
)

type fakeSubjects struct {
	items []db.Subject
	err   error

	calls          int
	gotUser, gotID uuid.UUID
	gotInput       subject.Input
	gotPatch       subject.Patch
}

func (f *fakeSubjects) List(_ context.Context, uid uuid.UUID) ([]db.Subject, error) {
	f.calls++
	f.gotUser = uid
	return f.items, f.err
}

func (f *fakeSubjects) Create(_ context.Context, uid uuid.UUID, in subject.Input) (db.Subject, error) {
	f.calls++
	f.gotUser, f.gotInput = uid, in
	if f.err != nil {
		return db.Subject{}, f.err
	}
	s := db.Subject{ID: uuid.New(), UserID: uid, Title: in.Title, CreatedAt: time.Now()}
	if in.Color != nil {
		s.Color = pgtype.Text{String: *in.Color, Valid: true}
	}
	if in.Semester != nil {
		s.Semester = pgtype.Int2{Int16: *in.Semester, Valid: true}
	}
	return s, nil
}

func (f *fakeSubjects) Update(_ context.Context, uid, id uuid.UUID, p subject.Patch) (db.Subject, error) {
	f.calls++
	f.gotUser, f.gotID, f.gotPatch = uid, id, p
	if f.err != nil {
		return db.Subject{}, f.err
	}
	return db.Subject{ID: id, UserID: uid, Title: "t", CreatedAt: time.Now()}, nil
}

func (f *fakeSubjects) Delete(_ context.Context, uid, id uuid.UUID) error {
	f.calls++
	f.gotUser, f.gotID = uid, id
	return f.err
}

type subjectsEnv struct {
	h    http.Handler
	f    *fakeSubjects
	uid  uuid.UUID
	auth string
}

func newSubjectsEnv(f *fakeSubjects) subjectsEnv {
	uid := uuid.New()
	tok, _ := auth.NewAccessToken(testSecret, uid, time.Now())
	h := httpapi.NewHandler(httpapi.Deps{DB: fakePinger{}, Auth: &fakeAuth{}, Subjects: f, JWTSecret: testSecret})
	return subjectsEnv{h: h, f: f, uid: uid, auth: "Bearer " + tok}
}

func (e subjectsEnv) do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", e.auth)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func TestListSubjects_Empty_ReturnsEmptyArrayNotNull(t *testing.T) {
	e := newSubjectsEnv(&fakeSubjects{})
	rec := e.do(http.MethodGet, "/v1/subjects", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"items":[]}` {
		t.Errorf("body = %s, want {\"items\":[]}", got)
	}
	if e.f.gotUser != e.uid {
		t.Error("service must receive the user id from the token")
	}
}

func TestListSubjects_NullableFieldsSerializeAsNull(t *testing.T) {
	withBoth := db.Subject{ID: uuid.New(), Title: "A", Color: pgtype.Text{String: "#112233", Valid: true}, Semester: pgtype.Int2{Int16: 4, Valid: true}, CreatedAt: time.Now()}
	bare := db.Subject{ID: uuid.New(), Title: "B", CreatedAt: time.Now()}
	e := newSubjectsEnv(&fakeSubjects{items: []db.Subject{withBoth, bare}})

	rec := e.do(http.MethodGet, "/v1/subjects", "")

	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) != 2 {
		t.Fatalf("items = %d", len(body.Items))
	}
	if body.Items[0]["color"] != "#112233" || body.Items[0]["semester"] != float64(4) {
		t.Errorf("values lost: %v", body.Items[0])
	}
	for _, k := range []string{"color", "semester"} {
		v, present := body.Items[1][k]
		if !present || v != nil {
			t.Errorf("%s must be present and null, got %v (present=%v)", k, v, present)
		}
	}
	if _, leaked := body.Items[0]["user_id"]; leaked {
		t.Error("user_id must not be exposed")
	}
}

func TestCreateSubject_OK_Returns201(t *testing.T) {
	e := newSubjectsEnv(&fakeSubjects{})
	rec := e.do(http.MethodPost, "/v1/subjects", `{"title":"  Algebra  ","color":"#4f46e5","semester":3}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	in := e.f.gotInput
	if in.Title != "Algebra" {
		t.Errorf("title must be trimmed, got %q", in.Title)
	}
	if in.Color == nil || *in.Color != "#4f46e5" || in.Semester == nil || *in.Semester != 3 {
		t.Errorf("optional fields not passed: %+v", in)
	}
	if e.f.gotUser != e.uid {
		t.Error("service must receive the user id from the token")
	}
}

func TestCreateSubject_NullOptionals_AreAccepted(t *testing.T) {
	e := newSubjectsEnv(&fakeSubjects{})
	rec := e.do(http.MethodPost, "/v1/subjects", `{"title":"X","color":null,"semester":null}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if e.f.gotInput.Color != nil || e.f.gotInput.Semester != nil {
		t.Errorf("null must mean absent: %+v", e.f.gotInput)
	}
}

func TestCreateSubject_Invalid_Returns400AndSkipsService(t *testing.T) {
	cases := map[string]string{
		"missing title":    `{}`,
		"blank title":      `{"title":"   "}`,
		"title too long":   `{"title":"` + strings.Repeat("я", 201) + `"}`,
		"color not hex":    `{"title":"A","color":"red"}`,
		"color short hex":  `{"title":"A","color":"#fff"}`,
		"semester zero":    `{"title":"A","semester":0}`,
		"semester too big": `{"title":"A","semester":13}`,
		"semester string":  `{"title":"A","semester":"3"}`,
		"unknown field":    `{"title":"A","user_id":"` + uuid.NewString() + `"}`,
		"malformed":        `{"title":`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			e := newSubjectsEnv(&fakeSubjects{})
			rec := e.do(http.MethodPost, "/v1/subjects", body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
			}
			if errorCode(t, rec) != "validation_failed" {
				t.Errorf("code = %q", errorCode(t, rec))
			}
			if e.f.calls != 0 {
				t.Error("service must not be called")
			}
		})
	}
}

func TestCreateSubject_Title200Runes_IsAllowed(t *testing.T) {
	e := newSubjectsEnv(&fakeSubjects{})
	rec := e.do(http.MethodPost, "/v1/subjects", `{"title":"`+strings.Repeat("я", 200)+`"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("200 runes (400 bytes) must pass, status = %d", rec.Code)
	}
}

func TestUpdateSubject_PassesThreeStates(t *testing.T) {
	id := uuid.New()
	e := newSubjectsEnv(&fakeSubjects{})

	// title меняем, color очищаем (null), semester не передан.
	rec := e.do(http.MethodPatch, "/v1/subjects/"+id.String(), `{"title":"New","color":null}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	p := e.f.gotPatch
	if p.Title == nil || *p.Title != "New" {
		t.Errorf("title: %+v", p)
	}
	if !p.SetColor || p.Color != nil {
		t.Errorf("color: null must mean clear: %+v", p)
	}
	if p.SetSemester {
		t.Errorf("semester was not sent and must stay untouched: %+v", p)
	}
	if e.f.gotID != id || e.f.gotUser != e.uid {
		t.Error("service must get the path id and the token's user id")
	}
}

func TestUpdateSubject_SetsValues(t *testing.T) {
	e := newSubjectsEnv(&fakeSubjects{})
	e.do(http.MethodPatch, "/v1/subjects/"+uuid.NewString(), `{"color":"#AABBCC","semester":7}`)

	p := e.f.gotPatch
	if !p.SetColor || p.Color == nil || *p.Color != "#AABBCC" || !p.SetSemester || p.Semester == nil || *p.Semester != 7 {
		t.Errorf("values not passed: %+v", p)
	}
}

func TestUpdateSubject_Invalid_Returns400(t *testing.T) {
	cases := map[string]string{
		"empty object":    `{}`,
		"only null title": `{"title":null}`,
		"blank title":     `{"title":""}`,
		"bad color":       `{"color":"blue"}`,
		"bad semester":    `{"semester":99}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			e := newSubjectsEnv(&fakeSubjects{})
			rec := e.do(http.MethodPatch, "/v1/subjects/"+uuid.NewString(), body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
			}
			if e.f.calls != 0 {
				t.Error("service must not be called")
			}
		})
	}
}

func TestSubjectByID_NotFound_Returns404(t *testing.T) {
	e := newSubjectsEnv(&fakeSubjects{err: subject.ErrNotFound})
	id := uuid.NewString()

	for _, c := range []struct{ method, body string }{
		{http.MethodPatch, `{"title":"x"}`},
		{http.MethodDelete, ``},
	} {
		rec := e.do(c.method, "/v1/subjects/"+id, c.body)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", c.method, rec.Code)
		}
		if errorCode(t, rec) != "not_found" {
			t.Errorf("%s: code = %q", c.method, errorCode(t, rec))
		}
	}
}

func TestSubjectByID_InvalidUUID_Returns400(t *testing.T) {
	e := newSubjectsEnv(&fakeSubjects{})

	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		rec := e.do(method, "/v1/subjects/not-a-uuid", `{"title":"x"}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", method, rec.Code)
		}
	}
	if e.f.calls != 0 {
		t.Error("service must not be called")
	}
}

func TestDeleteSubject_OK_Returns204(t *testing.T) {
	e := newSubjectsEnv(&fakeSubjects{})
	id := uuid.New()
	rec := e.do(http.MethodDelete, "/v1/subjects/"+id.String(), "")

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Error("204 must have no body")
	}
	if e.f.gotID != id || e.f.gotUser != e.uid {
		t.Error("delete must be scoped by path id and token's user id")
	}
}

func TestSubjects_ServiceError_Returns500WithoutLeak(t *testing.T) {
	e := newSubjectsEnv(&fakeSubjects{err: errors.New("pq: secret internal detail")})

	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/subjects", ""},
		{http.MethodPost, "/v1/subjects", `{"title":"A"}`},
		{http.MethodPatch, "/v1/subjects/" + uuid.NewString(), `{"title":"A"}`},
		{http.MethodDelete, "/v1/subjects/" + uuid.NewString(), ""},
	} {
		rec := e.do(c.method, c.path, c.body)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s %s: status = %d, want 500", c.method, c.path, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "secret internal detail") {
			t.Errorf("%s %s leaks the internal error", c.method, c.path)
		}
	}
}

func TestSubjects_RequireAuth(t *testing.T) {
	e := newSubjectsEnv(&fakeSubjects{})
	e.auth = ""

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/v1/subjects"},
		{http.MethodPost, "/v1/subjects"},
		{http.MethodPatch, "/v1/subjects/" + uuid.NewString()},
		{http.MethodDelete, "/v1/subjects/" + uuid.NewString()},
	} {
		if rec := e.do(c.method, c.path, `{}`); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", c.method, c.path, rec.Code)
		}
	}
	if e.f.calls != 0 {
		t.Error("service must not be called without auth")
	}
}
