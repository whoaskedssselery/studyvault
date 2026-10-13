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

	"github.com/whoaskedssselery/studyvault/backend/internal/attachment"
	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/httpapi"
	"github.com/whoaskedssselery/studyvault/backend/internal/storage"
)

type fakeAttachments struct {
	err      error
	upload   attachment.Upload
	download attachment.Download
	list     []db.Attachment

	calls          int
	gotUser, gotID uuid.UUID
	gotInput       attachment.Input
}

func (f *fakeAttachments) Create(_ context.Context, uid uuid.UUID, in attachment.Input) (attachment.Upload, error) {
	f.calls++
	f.gotUser, f.gotInput = uid, in
	if f.err != nil {
		return attachment.Upload{}, f.err
	}
	return f.upload, nil
}

func (f *fakeAttachments) Get(_ context.Context, uid, id uuid.UUID) (attachment.Download, error) {
	f.calls++
	f.gotUser, f.gotID = uid, id
	return f.download, f.err
}

func (f *fakeAttachments) List(_ context.Context, uid, noteID uuid.UUID) ([]db.Attachment, error) {
	f.calls++
	f.gotUser, f.gotID = uid, noteID
	return f.list, f.err
}

func (f *fakeAttachments) Delete(_ context.Context, uid, id uuid.UUID) error {
	f.calls++
	f.gotUser, f.gotID = uid, id
	return f.err
}

type attEnv struct {
	h    http.Handler
	f    *fakeAttachments
	uid  uuid.UUID
	auth string
}

func newAttEnv(f *fakeAttachments) attEnv {
	uid := uuid.New()
	tok, _ := auth.NewAccessToken(testSecret, uid, time.Now())
	deps := httpapi.Deps{DB: fakePinger{}, Auth: &fakeAuth{}, JWTSecret: testSecret}
	if f != nil {
		deps.Attachments = f
	}
	return attEnv{h: httpapi.NewHandler(deps), f: f, uid: uid, auth: "Bearer " + tok}
}

func (e attEnv) do(method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if e.auth != "" {
		req.Header.Set("Authorization", e.auth)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func sampleRow() db.Attachment {
	return db.Attachment{
		ID: uuid.New(), UserID: uuid.New(), ObjectKey: "secret-owner/secret-key",
		FileName: "lecture.pdf", MimeType: "application/pdf", SizeBytes: 1234, CreatedAt: time.Now(),
	}
}

const validAttachment = `{"file_name":"lecture.pdf","mime_type":"application/pdf","size_bytes":1234}`

func TestCreateAttachment_OK_Returns201WithAnUploadLink(t *testing.T) {
	row := sampleRow()
	e := newAttEnv(&fakeAttachments{upload: attachment.Upload{Attachment: row, URL: "https://s3.test/put/x?sig=1"}})

	rec := e.do(http.MethodPost, "/attachments", validAttachment)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	body := decode(t, rec)
	if body["upload_url"] != "https://s3.test/put/x?sig=1" {
		t.Errorf("upload_url = %v", body["upload_url"])
	}
	att := body["attachment"].(map[string]any)
	if att["file_name"] != "lecture.pdf" || att["size_bytes"] != float64(1234) || att["id"] != row.ID.String() {
		t.Errorf("attachment: %v", att)
	}
	if v, present := att["note_id"]; !present || v != nil {
		t.Errorf("note_id must be present and null, got %v", v)
	}
	for _, leak := range []string{"object_key", "user_id", "secret-key", "secret-owner"} {
		if strings.Contains(rec.Body.String(), leak) {
			t.Errorf("the response must not expose %q", leak)
		}
	}
	if e.f.gotUser != e.uid {
		t.Error("service must receive the user id from the token")
	}
}

func TestCreateAttachment_PassesTheNoteAndDeclaredValues(t *testing.T) {
	nid := uuid.New()
	e := newAttEnv(&fakeAttachments{})

	e.do(http.MethodPost, "/attachments", `{"note_id":"`+nid.String()+`","file_name":"  scan.png ","mime_type":"image/png","size_bytes":99}`)

	in := e.f.gotInput
	if in.NoteID == nil || *in.NoteID != nid || in.FileName != "scan.png" || in.MimeType != "image/png" || in.SizeBytes != 99 {
		t.Errorf("input: %+v", in)
	}
}

func TestCreateAttachment_FileNameIsReducedToABaseName(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":      "passwd",
		`C:\Users\me\diary.txt`: "diary.txt",
		"folder/sub/notes.md":   "notes.md",
		"plain.pdf":             "plain.pdf",
		"Лекция №1.pdf":         "Лекция №1.pdf",
	}
	for in, want := range cases {
		e := newAttEnv(&fakeAttachments{})
		body := `{"file_name":` + jsonString(in) + `,"mime_type":"application/pdf","size_bytes":1}`
		if rec := e.do(http.MethodPost, "/attachments", body); rec.Code != http.StatusCreated {
			t.Errorf("%q: status = %d, body = %s", in, rec.Code, rec.Body)
			continue
		}
		if e.f.gotInput.FileName != want {
			t.Errorf("%q: stored as %q, want %q", in, e.f.gotInput.FileName, want)
		}
	}
}

func jsonString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

func TestCreateAttachment_Invalid_Returns400(t *testing.T) {
	cases := map[string]string{
		"missing name":       `{"mime_type":"application/pdf","size_bytes":1}`,
		"blank name":         `{"file_name":"   ","mime_type":"application/pdf","size_bytes":1}`,
		"name is only a dir": `{"file_name":"folder/","mime_type":"application/pdf","size_bytes":1}`,
		"newline in name":    `{"file_name":"a\nb.pdf","mime_type":"application/pdf","size_bytes":1}`,
		"control char":       `{"file_name":"a\u0000b.pdf","mime_type":"application/pdf","size_bytes":1}`,
		"name too long":      `{"file_name":"` + strings.Repeat("я", 256) + `","mime_type":"application/pdf","size_bytes":1}`,
		"missing size":       `{"file_name":"a.pdf","mime_type":"application/pdf"}`,
		"size as string":     `{"file_name":"a.pdf","mime_type":"application/pdf","size_bytes":"5"}`,
		"unknown field":      `{"file_name":"a.pdf","mime_type":"application/pdf","size_bytes":1,"object_key":"x"}`,
		"bad note id":        `{"note_id":"nope","file_name":"a.pdf","mime_type":"application/pdf","size_bytes":1}`,
		"malformed":          `{"file_name":`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			e := newAttEnv(&fakeAttachments{err: attachment.ErrInvalid})
			rec := e.do(http.MethodPost, "/attachments", body)

			if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "validation_failed" {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
			}
		})
	}
}

func TestCreateAttachment_ServiceRejects_Returns400(t *testing.T) {
	e := newAttEnv(&fakeAttachments{err: errors.Join(attachment.ErrInvalid, errors.New(`file type "text/html" is not allowed`))})
	rec := e.do(http.MethodPost, "/attachments", `{"file_name":"a.html","mime_type":"text/html","size_bytes":5}`)

	if rec.Code != http.StatusBadRequest || errorCode(t, rec) != "validation_failed" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestAttachments_ErrorMapping(t *testing.T) {
	id := uuid.NewString()
	cases := []struct {
		name         string
		err          error
		method, path string
		body         string
		wantStatus   int
		wantCode     string
	}{
		{"create: note not found", attachment.ErrNoteNotFound, http.MethodPost, "/attachments", validAttachment, http.StatusNotFound, "not_found"},
		{"create: storage down", storage.ErrUnavailable, http.MethodPost, "/attachments", validAttachment, http.StatusServiceUnavailable, "storage_unavailable"},
		{"create: internal", errors.New("pq: secret internal detail"), http.MethodPost, "/attachments", validAttachment, http.StatusInternalServerError, "internal"},
		{"get: not found", attachment.ErrNotFound, http.MethodGet, "/attachments/" + id, "", http.StatusNotFound, "not_found"},
		{"get: not uploaded", attachment.ErrNotUploaded, http.MethodGet, "/attachments/" + id, "", http.StatusConflict, "not_uploaded"},
		{"get: storage down", storage.ErrUnavailable, http.MethodGet, "/attachments/" + id, "", http.StatusServiceUnavailable, "storage_unavailable"},
		{"get: internal", errors.New("pq: secret internal detail"), http.MethodGet, "/attachments/" + id, "", http.StatusInternalServerError, "internal"},
		{"list: note not found", attachment.ErrNoteNotFound, http.MethodGet, "/attachments?note_id=" + id, "", http.StatusNotFound, "not_found"},
		{"list: internal", errors.New("pq: secret internal detail"), http.MethodGet, "/attachments?note_id=" + id, "", http.StatusInternalServerError, "internal"},
		{"delete: not found", attachment.ErrNotFound, http.MethodDelete, "/attachments/" + id, "", http.StatusNotFound, "not_found"},
		{"delete: internal", errors.New("pq: secret internal detail"), http.MethodDelete, "/attachments/" + id, "", http.StatusInternalServerError, "internal"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := newAttEnv(&fakeAttachments{err: c.err}).do(c.method, c.path, c.body)

			if rec.Code != c.wantStatus || errorCode(t, rec) != c.wantCode {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
			}
			if strings.Contains(rec.Body.String(), "secret internal detail") {
				t.Error("response leaks the internal error")
			}
		})
	}
}

func TestGetAttachment_OK_ReturnsADownloadLink(t *testing.T) {
	row := sampleRow()
	e := newAttEnv(&fakeAttachments{download: attachment.Download{Attachment: row, URL: "https://s3.test/get/x?sig=2"}})

	rec := e.do(http.MethodGet, "/attachments/"+row.ID.String(), "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if decode(t, rec)["download_url"] != "https://s3.test/get/x?sig=2" {
		t.Errorf("body: %s", rec.Body)
	}
	if e.f.gotID != row.ID || e.f.gotUser != e.uid {
		t.Error("service must get the path id and the token's user id")
	}
	if strings.Contains(rec.Body.String(), "secret-key") {
		t.Error("the object key must not be exposed")
	}
}

func TestListAttachments(t *testing.T) {
	nid := uuid.New()
	row := sampleRow()
	row.NoteID = uuid.NullUUID{UUID: nid, Valid: true}
	e := newAttEnv(&fakeAttachments{list: []db.Attachment{row}})

	rec := e.do(http.MethodGet, "/attachments?note_id="+nid.String(), "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	item := decode(t, rec)["items"].([]any)[0].(map[string]any)
	if item["note_id"] != nid.String() || item["file_name"] != "lecture.pdf" {
		t.Errorf("item: %v", item)
	}
	if e.f.gotID != nid || e.f.gotUser != e.uid {
		t.Error("service must get the note id and the token's user id")
	}

	empty := newAttEnv(&fakeAttachments{}).do(http.MethodGet, "/attachments?note_id="+nid.String(), "")
	if strings.TrimSpace(empty.Body.String()) != `{"items":[]}` {
		t.Errorf("an empty list must be an empty array: %s", empty.Body)
	}
}

func TestListAttachments_BadNoteID_Returns400(t *testing.T) {
	for _, q := range []string{"", "?note_id=", "?note_id=nope"} {
		e := newAttEnv(&fakeAttachments{})
		if rec := e.do(http.MethodGet, "/attachments"+q, ""); rec.Code != http.StatusBadRequest || e.f.calls != 0 {
			t.Errorf("%q: status = %d, calls = %d", q, rec.Code, e.f.calls)
		}
	}
}

func TestDeleteAttachment_OK_Returns204(t *testing.T) {
	e := newAttEnv(&fakeAttachments{})
	id := uuid.New()

	rec := e.do(http.MethodDelete, "/attachments/"+id.String(), "")

	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body)
	}
	if e.f.gotID != id || e.f.gotUser != e.uid {
		t.Error("delete must be scoped by path id and token's user id")
	}
}

func TestAttachments_InvalidUUID_Returns400(t *testing.T) {
	e := newAttEnv(&fakeAttachments{})

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		if rec := e.do(method, "/attachments/nope", ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", method, rec.Code)
		}
	}
	if e.f.calls != 0 {
		t.Error("service must not be called")
	}
}

func TestAttachments_NotConfigured_Returns503(t *testing.T) {
	e := newAttEnv(nil)
	id := uuid.NewString()

	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "/attachments", validAttachment},
		{http.MethodGet, "/attachments/" + id, ""},
		{http.MethodGet, "/attachments?note_id=" + id, ""},
		{http.MethodDelete, "/attachments/" + id, ""},
	} {
		rec := e.do(c.method, c.path, c.body)
		if rec.Code != http.StatusServiceUnavailable || errorCode(t, rec) != "storage_unavailable" {
			t.Errorf("%s %s: status = %d, body = %s", c.method, c.path, rec.Code, rec.Body)
		}
	}
}

func TestAttachments_RequireAuth(t *testing.T) {
	e := newAttEnv(&fakeAttachments{})
	e.auth = ""
	id := uuid.NewString()

	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/attachments"},
		{http.MethodGet, "/attachments/" + id},
		{http.MethodGet, "/attachments?note_id=" + id},
		{http.MethodDelete, "/attachments/" + id},
	} {
		if rec := e.do(c.method, c.path, `{}`); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: status = %d, want 401", c.method, c.path, rec.Code)
		}
	}
	if e.f.calls != 0 {
		t.Error("service must not be called without auth")
	}
}
