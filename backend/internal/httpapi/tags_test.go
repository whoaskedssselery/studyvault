package httpapi_test

import (
	"context"
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
	"github.com/whoaskedssselery/studyvault/backend/internal/tag"
)

type fakeTags struct {
	items []db.Tag
	err   error // для List, Create и Delete
	setBy error // для SetNoteTags

	calls          int
	gotUser, gotID uuid.UUID
	gotName        string
	gotNote        uuid.UUID
	gotTagIDs      []uuid.UUID
	setCalled      bool
}

func (f *fakeTags) List(_ context.Context, uid uuid.UUID) ([]db.Tag, error) {
	f.calls++
	f.gotUser = uid
	return f.items, f.err
}

func (f *fakeTags) Create(_ context.Context, uid uuid.UUID, name string) (db.Tag, error) {
	f.calls++
	f.gotUser, f.gotName = uid, name
	if f.err != nil {
		return db.Tag{}, f.err
	}
	return db.Tag{ID: uuid.New(), UserID: uid, Name: name}, nil
}

func (f *fakeTags) Delete(_ context.Context, uid, id uuid.UUID) error {
	f.calls++
	f.gotUser, f.gotID = uid, id
	return f.err
}

func (f *fakeTags) SetNoteTags(_ context.Context, uid, noteID uuid.UUID, ids []uuid.UUID) error {
	f.calls++
	f.setCalled = true
	f.gotUser, f.gotNote, f.gotTagIDs = uid, noteID, ids
	return f.setBy
}

type tagsEnv struct {
	h     http.Handler
	tags  *fakeTags
	notes *fakeNotes
	uid   uuid.UUID
	auth  string
}

func newTagsEnv(tags *fakeTags, notes *fakeNotes) tagsEnv {
	uid := uuid.New()
	tok, _ := auth.NewAccessToken(testSecret, uid, time.Now())
	h := httpapi.NewHandler(httpapi.Deps{DB: fakePinger{}, Auth: &fakeAuth{}, Tags: tags, Notes: notes, JWTSecret: testSecret})
	return tagsEnv{h: h, tags: tags, notes: notes, uid: uid, auth: "Bearer " + tok}
}

func (e tagsEnv) do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", e.auth)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func TestListTags_Empty_ReturnsEmptyArray(t *testing.T) {
	e := newTagsEnv(&fakeTags{}, &fakeNotes{})
	rec := e.do(http.MethodGet, "/v1/tags", "")

	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"items":[]}` {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if e.tags.gotUser != e.uid {
		t.Error("service must receive the user id from the token")
	}
}

func TestListTags_ReturnsIDAndNameOnly(t *testing.T) {
	e := newTagsEnv(&fakeTags{items: []db.Tag{{ID: uuid.New(), UserID: uuid.New(), Name: "exam"}}}, &fakeNotes{})
	rec := e.do(http.MethodGet, "/v1/tags", "")

	item := decode(t, rec)["items"].([]any)[0].(map[string]any)
	if item["name"] != "exam" || item["id"] == nil {
		t.Errorf("item: %v", item)
	}
	if _, leaked := item["user_id"]; leaked {
		t.Error("user_id must not be exposed")
	}
}

func TestCreateTag_OK_Returns201(t *testing.T) {
	e := newTagsEnv(&fakeTags{}, &fakeNotes{})
	rec := e.do(http.MethodPost, "/v1/tags", `{"name":"  exam "}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if e.tags.gotName != "exam" || e.tags.gotUser != e.uid {
		t.Errorf("name = %q, user ok = %v", e.tags.gotName, e.tags.gotUser == e.uid)
	}
	if decode(t, rec)["name"] != "exam" {
		t.Errorf("body: %s", rec.Body)
	}
}

func TestCreateTag_Invalid_Returns400AndSkipsService(t *testing.T) {
	cases := map[string]string{
		"missing name":  `{}`,
		"blank name":    `{"name":"   "}`,
		"name too long": `{"name":"` + strings.Repeat("я", 51) + `"}`,
		"unknown field": `{"name":"a","user_id":"` + uuid.NewString() + `"}`,
		"not a string":  `{"name":5}`,
		"malformed":     `{"name":`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTagsEnv(&fakeTags{}, &fakeNotes{})
			rec := e.do(http.MethodPost, "/v1/tags", body)

			if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "validation_failed" {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
			}
			if e.tags.calls != 0 {
				t.Error("service must not be called")
			}
		})
	}
}

func TestCreateTag_Name50Runes_IsAllowed(t *testing.T) {
	e := newTagsEnv(&fakeTags{}, &fakeNotes{})

	if rec := e.do(http.MethodPost, "/v1/tags", `{"name":"`+strings.Repeat("я", 50)+`"}`); rec.Code != http.StatusCreated {
		t.Fatalf("50 runes (100 bytes) must pass, status = %d", rec.Code)
	}
}

func TestCreateTag_Duplicate_Returns409(t *testing.T) {
	e := newTagsEnv(&fakeTags{err: tag.ErrNameTaken}, &fakeNotes{})
	rec := e.do(http.MethodPost, "/v1/tags", `{"name":"exam"}`)

	if rec.Code != http.StatusConflict || errorCode(t, rec) != "tag_exists" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestDeleteTag(t *testing.T) {
	id := uuid.New()

	e := newTagsEnv(&fakeTags{}, &fakeNotes{})
	rec := e.do(http.MethodDelete, "/v1/tags/"+id.String(), "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body)
	}
	if e.tags.gotID != id || e.tags.gotUser != e.uid {
		t.Error("delete must be scoped by path id and token's user id")
	}

	e = newTagsEnv(&fakeTags{err: tag.ErrNotFound}, &fakeNotes{})
	rec = e.do(http.MethodDelete, "/v1/tags/"+id.String(), "")
	if rec.Code != http.StatusNotFound || errorCode(t, rec) != "not_found" {
		t.Errorf("missing tag: status = %d, body = %s", rec.Code, rec.Body)
	}

	e = newTagsEnv(&fakeTags{}, &fakeNotes{})
	if rec := e.do(http.MethodDelete, "/v1/tags/nope", ""); rec.Code != http.StatusBadRequest || e.tags.calls != 0 {
		t.Errorf("bad uuid: status = %d, calls = %d", rec.Code, e.tags.calls)
	}
}

func TestSetNoteTags_OK_ReplacesAndReturnsTheNote(t *testing.T) {
	n := sampleNote()
	e := newTagsEnv(&fakeTags{}, &fakeNotes{note: n})
	a, b := uuid.New(), uuid.New()

	rec := e.do(http.MethodPut, "/v1/notes/"+n.ID.String()+"/tags", `{"tag_ids":["`+a.String()+`","`+b.String()+`"]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if e.tags.gotNote != n.ID || e.tags.gotUser != e.uid || len(e.tags.gotTagIDs) != 2 {
		t.Errorf("service got note=%v user ok=%v ids=%v", e.tags.gotNote, e.tags.gotUser == e.uid, e.tags.gotTagIDs)
	}
	if e.notes.gotID != n.ID || e.notes.gotUser != e.uid {
		t.Error("the updated note must be re-read for this user")
	}
	body := decode(t, rec)
	if body["title"] != "Sample" || len(body["tags"].([]any)) != 1 || body["content"] == nil {
		t.Errorf("response must be the full note: %s", rec.Body)
	}
}

func TestSetNoteTags_EmptyArray_IsAllowedAndClears(t *testing.T) {
	e := newTagsEnv(&fakeTags{}, &fakeNotes{note: sampleNote()})
	rec := e.do(http.MethodPut, "/v1/notes/"+uuid.NewString()+"/tags", `{"tag_ids":[]}`)

	if rec.Code != http.StatusOK || !e.tags.setCalled || len(e.tags.gotTagIDs) != 0 {
		t.Fatalf("status = %d, called = %v", rec.Code, e.tags.setCalled)
	}
}

func TestSetNoteTags_Invalid_Returns400AndSkipsService(t *testing.T) {
	many := make([]string, 51)
	for i := range many {
		many[i] = `"` + uuid.NewString() + `"`
	}
	cases := map[string]string{
		"missing tag_ids": `{}`,
		"null tag_ids":    `{"tag_ids":null}`,
		"not an array":    `{"tag_ids":"x"}`,
		"bad uuid":        `{"tag_ids":["nope"]}`,
		"more than 50":    `{"tag_ids":[` + strings.Join(many, ",") + `]}`,
		"unknown field":   `{"tag_ids":[],"note_id":"` + uuid.NewString() + `"}`,
		"empty body":      ``,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTagsEnv(&fakeTags{}, &fakeNotes{note: sampleNote()})
			rec := e.do(http.MethodPut, "/v1/notes/"+uuid.NewString()+"/tags", body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
			}
			if e.tags.setCalled {
				t.Error("service must not be called")
			}
		})
	}
}

func TestSetNoteTags_ServiceErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
		code string
	}{
		{"note not found", tag.ErrNoteNotFound, http.StatusNotFound, "not_found"},
		{"unknown tag", tag.ErrInvalidTag, http.StatusBadRequest, "validation_failed"},
		{"internal", errors.New("pq: secret internal detail"), http.StatusInternalServerError, "internal"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newTagsEnv(&fakeTags{setBy: c.err}, &fakeNotes{note: sampleNote()})
			rec := e.do(http.MethodPut, "/v1/notes/"+uuid.NewString()+"/tags", `{"tag_ids":[]}`)

			if rec.Code != c.want || errorCode(t, rec) != c.code {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
			}
			if strings.Contains(rec.Body.String(), "secret internal detail") {
				t.Error("response leaks the internal error")
			}
			if e.notes.calls != 0 {
				t.Error("the note must not be re-read after a failed update")
			}
		})
	}
}

func TestTags_ServiceError_Returns500WithoutLeak(t *testing.T) {
	e := newTagsEnv(&fakeTags{err: errors.New("pq: secret internal detail")}, &fakeNotes{})

	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/v1/tags", ""},
		{http.MethodPost, "/v1/tags", `{"name":"a"}`},
		{http.MethodDelete, "/v1/tags/" + uuid.NewString(), ""},
	} {
		rec := e.do(c.method, c.path, c.body)
		if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret internal detail") {
			t.Errorf("%s %s: status = %d, body = %s", c.method, c.path, rec.Code, rec.Body)
		}
	}
}

func TestTags_RequireAuth(t *testing.T) {
	e := newTagsEnv(&fakeTags{}, &fakeNotes{})
	e.auth = ""

	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/v1/tags"},
		{http.MethodPost, "/v1/tags"},
		{http.MethodDelete, "/v1/tags/" + uuid.NewString()},
		{http.MethodPut, "/v1/notes/" + uuid.NewString() + "/tags"},
	} {
		if rec := e.do(c.method, c.path, `{}`); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", c.method, c.path, rec.Code)
		}
	}
	if e.tags.calls != 0 {
		t.Error("service must not be called without auth")
	}
}
