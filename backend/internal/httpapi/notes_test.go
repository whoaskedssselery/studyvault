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
	"github.com/whoaskedssselery/studyvault/backend/internal/httpapi"
	"github.com/whoaskedssselery/studyvault/backend/internal/note"
)

type fakeNotes struct {
	note      note.Note
	page      note.Page
	backlinks []note.Summary
	graph     note.Graph
	err       error

	calls    int
	gotUser  uuid.UUID
	gotID    uuid.UUID
	gotInput note.Input
	gotPatch note.Patch
	gotList  note.ListFilter
}

func (f *fakeNotes) Create(_ context.Context, uid uuid.UUID, in note.Input) (note.Note, error) {
	f.calls++
	f.gotUser, f.gotInput = uid, in
	if f.err != nil {
		return note.Note{}, f.err
	}
	n := f.note
	n.ID, n.Title, n.Content, n.SubjectID, n.IsPinned = uuid.New(), in.Title, in.Content, in.SubjectID, in.IsPinned
	return n, nil
}

func (f *fakeNotes) Get(_ context.Context, uid, id uuid.UUID) (note.Note, error) {
	f.calls++
	f.gotUser, f.gotID = uid, id
	return f.note, f.err
}

func (f *fakeNotes) Update(_ context.Context, uid, id uuid.UUID, p note.Patch) (note.Note, error) {
	f.calls++
	f.gotUser, f.gotID, f.gotPatch = uid, id, p
	return f.note, f.err
}

func (f *fakeNotes) Delete(_ context.Context, uid, id uuid.UUID) error {
	f.calls++
	f.gotUser, f.gotID = uid, id
	return f.err
}

func (f *fakeNotes) List(_ context.Context, uid uuid.UUID, filter note.ListFilter) (note.Page, error) {
	f.calls++
	f.gotUser, f.gotList = uid, filter
	return f.page, f.err
}

type notesEnv struct {
	h    http.Handler
	f    *fakeNotes
	uid  uuid.UUID
	auth string
}

const validDoc = `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"hi"}]}]}`

func newNotesEnv(f *fakeNotes) notesEnv {
	uid := uuid.New()
	tok, _ := auth.NewAccessToken(testSecret, uid, time.Now())
	h := httpapi.NewHandler(httpapi.Deps{DB: fakePinger{}, Auth: &fakeAuth{}, Notes: f, JWTSecret: testSecret})
	return notesEnv{h: h, f: f, uid: uid, auth: "Bearer " + tok}
}

func (e notesEnv) do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", e.auth)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func sampleNote() note.Note {
	return note.Note{
		Summary: note.Summary{
			ID: uuid.New(), Title: "Sample", CreatedAt: time.Now(), UpdatedAt: time.Now(),
			Tags: []note.Tag{{ID: uuid.New(), Name: "exam"}},
		},
		Content: []byte(validDoc),
	}
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body is not a JSON object: %v (%s)", err, rec.Body)
	}
	return m
}

func TestCreateNote_OK_Returns201(t *testing.T) {
	e := newNotesEnv(&fakeNotes{note: note.Note{Summary: note.Summary{Tags: []note.Tag{}, CreatedAt: time.Now(), UpdatedAt: time.Now()}}})
	rec := e.do(http.MethodPost, "/notes", `{"title":"  My note ","content":`+validDoc+`}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if e.f.gotUser != e.uid {
		t.Error("service must receive the user id from the token")
	}
	in := e.f.gotInput
	if in.Title != "My note" || in.SubjectID != nil || in.IsPinned {
		t.Errorf("input: %+v", in)
	}
	var stored map[string]any
	if err := json.Unmarshal(in.Content, &stored); err != nil || stored["type"] != "doc" {
		t.Errorf("content passed to the service must be the JSON document: %s", in.Content)
	}

	body := decode(t, rec)
	if v, present := body["subject_id"]; !present || v != nil {
		t.Errorf("subject_id must be present and null, got %v (present=%v)", v, present)
	}
	if tags, ok := body["tags"].([]any); !ok || len(tags) != 0 {
		t.Errorf("tags must be an empty array, got %v", body["tags"])
	}
	if c, _ := body["content"].(map[string]any); c["type"] != "doc" {
		t.Errorf("content must be echoed back: %v", body["content"])
	}
	if _, leaked := body["user_id"]; leaked {
		t.Error("user_id must not be exposed")
	}
}

func TestCreateNote_WithSubjectAndPin(t *testing.T) {
	e := newNotesEnv(&fakeNotes{})
	sid := uuid.New()

	rec := e.do(http.MethodPost, "/notes", `{"title":"n","content":`+validDoc+`,"subject_id":"`+sid.String()+`","is_pinned":true}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if e.f.gotInput.SubjectID == nil || *e.f.gotInput.SubjectID != sid || !e.f.gotInput.IsPinned {
		t.Errorf("input: %+v", e.f.gotInput)
	}
	if decode(t, rec)["subject_id"] != sid.String() {
		t.Error("subject_id must be echoed")
	}
}

func TestCreateNote_NullSubject_IsAccepted(t *testing.T) {
	e := newNotesEnv(&fakeNotes{})
	rec := e.do(http.MethodPost, "/notes", `{"title":"n","content":`+validDoc+`,"subject_id":null}`)

	if rec.Code != http.StatusCreated || e.f.gotInput.SubjectID != nil {
		t.Fatalf("status = %d, subject = %v", rec.Code, e.f.gotInput.SubjectID)
	}
}

func TestCreateNote_Invalid_Returns400AndSkipsService(t *testing.T) {
	cases := map[string]string{
		"missing title":       `{"content":` + validDoc + `}`,
		"blank title":         `{"title":"  ","content":` + validDoc + `}`,
		"title too long":      `{"title":"` + strings.Repeat("я", 301) + `","content":` + validDoc + `}`,
		"missing content":     `{"title":"n"}`,
		"null content":        `{"title":"n","content":null}`,
		"content not a doc":   `{"title":"n","content":{"type":"paragraph"}}`,
		"content has no type": `{"title":"n","content":{"content":[]}}`,
		"content is array":    `{"title":"n","content":[]}`,
		"content is string":   `{"title":"n","content":"text"}`,
		"bad subject uuid":    `{"title":"n","content":` + validDoc + `,"subject_id":"nope"}`,
		"pinned not boolean":  `{"title":"n","content":` + validDoc + `,"is_pinned":"yes"}`,
		"unknown field":       `{"title":"n","content":` + validDoc + `,"user_id":"` + uuid.NewString() + `"}`,
		"malformed":           `{"title":`,
		"empty body":          ``,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			e := newNotesEnv(&fakeNotes{})
			rec := e.do(http.MethodPost, "/notes", body)

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

func TestCreateNote_Title300Runes_IsAllowed(t *testing.T) {
	e := newNotesEnv(&fakeNotes{})
	rec := e.do(http.MethodPost, "/notes", `{"title":"`+strings.Repeat("я", 300)+`","content":`+validDoc+`}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("300 runes (600 bytes) must pass, status = %d", rec.Code)
	}
}

func TestCreateNote_EmptyDocument_IsAllowed(t *testing.T) {
	e := newNotesEnv(&fakeNotes{})
	rec := e.do(http.MethodPost, "/notes", `{"title":"n","content":{"type":"doc"}}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("an empty doc is a valid new note, status = %d", rec.Code)
	}
}

func TestNoteWrites_UnknownSubject_Returns400(t *testing.T) {
	e := newNotesEnv(&fakeNotes{err: note.ErrInvalidSubject})
	id := uuid.NewString()

	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "/notes", `{"title":"n","content":` + validDoc + `,"subject_id":"` + uuid.NewString() + `"}`},
		{http.MethodPatch, "/notes/" + id, `{"subject_id":"` + uuid.NewString() + `"}`},
	} {
		rec := e.do(c.method, c.path, c.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", c.method, rec.Code)
		}
		if errorCode(t, rec) != "validation_failed" || !strings.Contains(rec.Body.String(), "subject") {
			t.Errorf("%s: body = %s", c.method, rec.Body)
		}
	}
}

func TestGetNote_OK(t *testing.T) {
	n := sampleNote()
	e := newNotesEnv(&fakeNotes{note: n})
	rec := e.do(http.MethodGet, "/notes/"+n.ID.String(), "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if e.f.gotID != n.ID || e.f.gotUser != e.uid {
		t.Error("service must get the path id and the token's user id")
	}
	body := decode(t, rec)
	tags, _ := body["tags"].([]any)
	if len(tags) != 1 || tags[0].(map[string]any)["name"] != "exam" {
		t.Errorf("tags: %v", body["tags"])
	}
	if body["title"] != "Sample" || body["content"] == nil {
		t.Errorf("body: %s", rec.Body)
	}
}

func TestNoteByID_NotFound_Returns404(t *testing.T) {
	e := newNotesEnv(&fakeNotes{err: note.ErrNotFound})
	id := uuid.NewString()

	for _, c := range []struct{ method, body string }{
		{http.MethodGet, ``},
		{http.MethodPatch, `{"title":"x"}`},
		{http.MethodDelete, ``},
	} {
		rec := e.do(c.method, "/notes/"+id, c.body)
		if rec.Code != http.StatusNotFound || errorCode(t, rec) != "not_found" {
			t.Errorf("%s: status = %d, body = %s", c.method, rec.Code, rec.Body)
		}
	}
}

func TestNoteByID_InvalidUUID_Returns400(t *testing.T) {
	e := newNotesEnv(&fakeNotes{})

	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		if rec := e.do(method, "/notes/not-a-uuid", `{"title":"x"}`); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", method, rec.Code)
		}
	}
	if e.f.calls != 0 {
		t.Error("service must not be called")
	}
}

func TestUpdateNote_PassesThreeStatesOfSubject(t *testing.T) {
	id, sid := uuid.New(), uuid.New()

	t.Run("null clears", func(t *testing.T) {
		e := newNotesEnv(&fakeNotes{note: sampleNote()})
		rec := e.do(http.MethodPatch, "/notes/"+id.String(), `{"subject_id":null}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
		}
		if p := e.f.gotPatch; !p.SetSubject || p.SubjectID != nil {
			t.Errorf("null must clear the subject: %+v", p)
		}
	})
	t.Run("value sets", func(t *testing.T) {
		e := newNotesEnv(&fakeNotes{note: sampleNote()})
		e.do(http.MethodPatch, "/notes/"+id.String(), `{"subject_id":"`+sid.String()+`"}`)
		if p := e.f.gotPatch; !p.SetSubject || p.SubjectID == nil || *p.SubjectID != sid {
			t.Errorf("value must be set: %+v", p)
		}
	})
	t.Run("absent keeps", func(t *testing.T) {
		e := newNotesEnv(&fakeNotes{note: sampleNote()})
		e.do(http.MethodPatch, "/notes/"+id.String(), `{"title":"only title"}`)
		p := e.f.gotPatch
		if p.SetSubject || p.Content != nil || p.IsPinned != nil || p.Title == nil || *p.Title != "only title" {
			t.Errorf("untouched fields must stay untouched: %+v", p)
		}
		if e.f.gotID != id || e.f.gotUser != e.uid {
			t.Error("service must get the path id and the token's user id")
		}
	})
}

func TestUpdateNote_ContentAndPin(t *testing.T) {
	e := newNotesEnv(&fakeNotes{note: sampleNote()})
	e.do(http.MethodPatch, "/notes/"+uuid.NewString(), `{"content":`+validDoc+`,"is_pinned":false}`)

	p := e.f.gotPatch
	var stored map[string]any
	if err := json.Unmarshal(p.Content, &stored); err != nil || stored["type"] != "doc" {
		t.Errorf("content: %s", p.Content)
	}
	if p.IsPinned == nil || *p.IsPinned {
		t.Errorf("is_pinned=false must be passed explicitly: %+v", p.IsPinned)
	}
}

func TestUpdateNote_Invalid_Returns400(t *testing.T) {
	cases := map[string]string{
		"empty object":      `{}`,
		"only null title":   `{"title":null}`,
		"blank title":       `{"title":""}`,
		"content not a doc": `{"content":{"type":"x"}}`,
		"bad subject":       `{"subject_id":"nope"}`,
		"unknown field":     `{"deleted_at":"2026-01-01T00:00:00Z"}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			e := newNotesEnv(&fakeNotes{})
			rec := e.do(http.MethodPatch, "/notes/"+uuid.NewString(), body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
			}
			if e.f.calls != 0 {
				t.Error("service must not be called")
			}
		})
	}
}

func TestDeleteNote_OK_Returns204(t *testing.T) {
	e := newNotesEnv(&fakeNotes{})
	id := uuid.New()
	rec := e.do(http.MethodDelete, "/notes/"+id.String(), "")

	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body)
	}
	if e.f.gotID != id || e.f.gotUser != e.uid {
		t.Error("delete must be scoped by path id and token's user id")
	}
}

func TestListNotes_Empty_ReturnsEmptyItemsAndNullCursor(t *testing.T) {
	e := newNotesEnv(&fakeNotes{})
	rec := e.do(http.MethodGet, "/notes", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := decode(t, rec)
	if items, ok := body["items"].([]any); !ok || len(items) != 0 {
		t.Errorf("items must be an empty array, got %v", body["items"])
	}
	if v, present := body["next_cursor"]; !present || v != nil {
		t.Errorf("next_cursor must be present and null, got %v (present=%v)", v, present)
	}
	f := e.f.gotList
	if f.SubjectID != nil || f.Tag != nil || f.Pinned != nil || f.Limit != 0 || f.Cursor != "" {
		t.Errorf("no filters expected: %+v", f)
	}
}

func TestListNotes_ReturnsSummariesWithoutContent(t *testing.T) {
	n := sampleNote()
	e := newNotesEnv(&fakeNotes{page: note.Page{Items: []note.Summary{n.Summary}, NextCursor: "next-page"}})
	rec := e.do(http.MethodGet, "/notes", "")

	body := decode(t, rec)
	if body["next_cursor"] != "next-page" {
		t.Errorf("next_cursor = %v", body["next_cursor"])
	}
	item := body["items"].([]any)[0].(map[string]any)
	if _, has := item["content"]; has {
		t.Error("list items must not carry the document")
	}
	if v, present := item["subject_id"]; !present || v != nil {
		t.Errorf("subject_id must be present and null, got %v", v)
	}
	if tags := item["tags"].([]any); len(tags) != 1 {
		t.Errorf("tags: %v", item["tags"])
	}
}

func TestListNotes_ParsesFilters(t *testing.T) {
	e := newNotesEnv(&fakeNotes{})
	sid := uuid.New()
	rec := e.do(http.MethodGet, "/notes?subject_id="+sid.String()+"&tag=exam&pinned=true&limit=5&cursor=abc", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	f := e.f.gotList
	if f.SubjectID == nil || *f.SubjectID != sid || f.Tag == nil || *f.Tag != "exam" ||
		f.Pinned == nil || !*f.Pinned || f.Limit != 5 || f.Cursor != "abc" {
		t.Errorf("filters: %+v", f)
	}
	if e.f.gotUser != e.uid {
		t.Error("service must receive the user id from the token")
	}
}

func TestListNotes_PinnedFalse_IsPassedAsFalse(t *testing.T) {
	e := newNotesEnv(&fakeNotes{})
	e.do(http.MethodGet, "/notes?pinned=false", "")

	if p := e.f.gotList.Pinned; p == nil || *p {
		t.Errorf("pinned=false must filter unpinned notes, got %v", p)
	}
}

func TestListNotes_BadParams_Return400(t *testing.T) {
	for _, q := range []string{"limit=0", "limit=101", "limit=-1", "limit=abc", "subject_id=nope", "pinned=maybe", "tag="} {
		t.Run(q, func(t *testing.T) {
			e := newNotesEnv(&fakeNotes{})
			rec := e.do(http.MethodGet, "/notes?"+q, "")

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if e.f.calls != 0 {
				t.Error("service must not be called")
			}
		})
	}
}

func TestListNotes_LimitBounds_AreAccepted(t *testing.T) {
	for _, q := range []string{"limit=1", "limit=100"} {
		e := newNotesEnv(&fakeNotes{})
		if rec := e.do(http.MethodGet, "/notes?"+q, ""); rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", q, rec.Code)
		}
	}
}

func TestListNotes_InvalidCursor_Returns400(t *testing.T) {
	e := newNotesEnv(&fakeNotes{err: note.ErrInvalidCursor})
	rec := e.do(http.MethodGet, "/notes?cursor=garbage", "")

	if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "validation_failed" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestNotes_ServiceError_Returns500WithoutLeak(t *testing.T) {
	e := newNotesEnv(&fakeNotes{err: errors.New("pq: secret internal detail")})

	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/notes", ""},
		{http.MethodPost, "/notes", `{"title":"n","content":` + validDoc + `}`},
		{http.MethodGet, "/notes/" + uuid.NewString(), ""},
		{http.MethodPatch, "/notes/" + uuid.NewString(), `{"title":"A"}`},
		{http.MethodDelete, "/notes/" + uuid.NewString(), ""},
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

func TestNotes_RequireAuth(t *testing.T) {
	e := newNotesEnv(&fakeNotes{})
	e.auth = ""

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/notes"},
		{http.MethodPost, "/notes"},
		{http.MethodGet, "/notes/" + uuid.NewString()},
		{http.MethodPatch, "/notes/" + uuid.NewString()},
		{http.MethodDelete, "/notes/" + uuid.NewString()},
	} {
		if rec := e.do(c.method, c.path, `{}`); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", c.method, c.path, rec.Code)
		}
	}
	if e.f.calls != 0 {
		t.Error("service must not be called without auth")
	}
}
