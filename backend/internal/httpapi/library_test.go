package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/httpapi"
	"github.com/whoaskedssselery/studyvault/backend/internal/library"
	"github.com/whoaskedssselery/studyvault/backend/internal/note"
	"github.com/whoaskedssselery/studyvault/backend/internal/search"
)

// fakeLibrary записывает, с чем его вызвали, и отдаёт заранее заданное.
type fakeLibrary struct {
	err error

	page     library.Page
	material library.Material
	subjects []library.Subject
	comment  library.Comment
	comments library.CommentPage
	rating   library.RatingResult
	queue    library.QueuePage
	decided  db.Note
	att      db.Attachment

	calls    int
	gotUser  *uuid.UUID
	gotID    uuid.UUID
	gotList  library.ListFilter
	gotDec   [2]string
	gotValue int
	gotBody  string
	gotLimit int
	gotCur   string
}

func (f *fakeLibrary) List(_ context.Context, uid *uuid.UUID, fl library.ListFilter) (library.Page, error) {
	f.calls++
	f.gotUser, f.gotList = uid, fl
	return f.page, f.err
}

func (f *fakeLibrary) Get(_ context.Context, uid *uuid.UUID, id uuid.UUID) (library.Material, error) {
	f.calls++
	f.gotUser, f.gotID = uid, id
	return f.material, f.err
}

func (f *fakeLibrary) Attachment(_ context.Context, uid *uuid.UUID, noteID, attID uuid.UUID) (db.Attachment, error) {
	f.calls++
	f.gotUser, f.gotID = uid, noteID
	return f.att, f.err
}

func (f *fakeLibrary) ListSubjects(context.Context) ([]library.Subject, error) {
	f.calls++
	return f.subjects, f.err
}

func (f *fakeLibrary) CreateSubject(_ context.Context, uid uuid.UUID, title string, color *string) (library.Subject, error) {
	f.calls++
	f.gotUser, f.gotBody = &uid, title
	return library.Subject{ID: uuid.New(), Title: title, Color: color}, f.err
}

func (f *fakeLibrary) UpdateSubject(_ context.Context, uid, id uuid.UUID, p library.SubjectPatch) (library.Subject, error) {
	f.calls++
	f.gotUser, f.gotID = &uid, id
	s := library.Subject{ID: id, Title: "x"}
	if p.Title != nil {
		s.Title = *p.Title
	}
	return s, f.err
}

func (f *fakeLibrary) DeleteSubject(_ context.Context, uid, id uuid.UUID) error {
	f.calls++
	f.gotUser, f.gotID = &uid, id
	return f.err
}

func (f *fakeLibrary) ListComments(_ context.Context, id uuid.UUID, limit int, cursor string) (library.CommentPage, error) {
	f.calls++
	f.gotID, f.gotLimit, f.gotCur = id, limit, cursor
	return f.comments, f.err
}

func (f *fakeLibrary) AddComment(_ context.Context, uid, id uuid.UUID, body string) (library.Comment, error) {
	f.calls++
	f.gotUser, f.gotID, f.gotBody = &uid, id, body
	return f.comment, f.err
}

func (f *fakeLibrary) DeleteComment(_ context.Context, uid, id uuid.UUID) error {
	f.calls++
	f.gotUser, f.gotID = &uid, id
	return f.err
}

func (f *fakeLibrary) Rate(_ context.Context, uid, id uuid.UUID, v int) (library.RatingResult, error) {
	f.calls++
	f.gotUser, f.gotID, f.gotValue = &uid, id, v
	return f.rating, f.err
}

func (f *fakeLibrary) AddBookmark(_ context.Context, uid, id uuid.UUID) error {
	f.calls++
	f.gotUser, f.gotID = &uid, id
	return f.err
}

func (f *fakeLibrary) RemoveBookmark(_ context.Context, uid, id uuid.UUID) error {
	f.calls++
	f.gotUser, f.gotID = &uid, id
	return f.err
}

func (f *fakeLibrary) Bookmarks(_ context.Context, uid uuid.UUID, limit int, cursor string) (library.Page, error) {
	f.calls++
	f.gotUser, f.gotLimit, f.gotCur = &uid, limit, cursor
	return f.page, f.err
}

func (f *fakeLibrary) Submit(_ context.Context, uid, id uuid.UUID) (db.Note, error) {
	f.calls++
	f.gotUser, f.gotID = &uid, id
	return f.decided, f.err
}

func (f *fakeLibrary) Decide(_ context.Context, uid, id uuid.UUID, decision, reason string) (db.Note, error) {
	f.calls++
	f.gotUser, f.gotID, f.gotDec = &uid, id, [2]string{decision, reason}
	n := f.decided
	n.ID = id
	return n, f.err
}

func (f *fakeLibrary) Queue(_ context.Context, uid uuid.UUID, limit int, cursor string) (library.QueuePage, error) {
	f.calls++
	f.gotUser, f.gotLimit, f.gotCur = &uid, limit, cursor
	return f.queue, f.err
}

type libEnv struct {
	h     http.Handler
	f     *fakeLibrary
	notes *fakeNotes
	uid   uuid.UUID
	auth  string
}

func newLibEnv(f *fakeLibrary) libEnv {
	uid := uuid.New()
	tok, _ := auth.NewAccessToken(testSecret, uid, time.Now())
	notes := &fakeNotes{note: sampleNote()}
	h := httpapi.NewHandler(httpapi.Deps{DB: fakePinger{}, Auth: &fakeAuth{}, Library: f, Notes: notes, JWTSecret: testSecret})
	return libEnv{h: h, f: f, notes: notes, uid: uid, auth: "Bearer " + tok}
}

func (e libEnv) do(method, path, body, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e libEnv) as(method, path, body string) *httptest.ResponseRecorder {
	return e.do(method, path, body, e.auth)
}

func sampleItem() library.Item {
	now := time.Now()
	return library.Item{
		ID: uuid.New(), Title: "Limits", Kind: "note", Author: library.Author{ID: uuid.New(), DisplayName: "Ann"}, PublishedAt: &now,
	}
}

func decodeMap(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("not JSON: %v (%s)", err, rec.Body)
	}
	return m
}

// Чтение библиотеки открыто гостям, всё остальное требует токена.
func TestLibrary_GuestsReadButCannotWrite(t *testing.T) {
	e := newLibEnv(&fakeLibrary{page: library.Page{Items: []library.Item{sampleItem()}}, comments: library.CommentPage{}})
	id := uuid.NewString()

	for _, path := range []string{"/v1/library", "/v1/library/subjects", "/v1/library/" + id, "/v1/library/" + id + "/comments", "/v1/library/" + id + "/attachments/" + id} {
		rec := e.do(http.MethodGet, path, "", "")
		if rec.Code == http.StatusUnauthorized {
			t.Errorf("GET %s must be open to guests, got 401", path)
		}
	}
	for _, c := range [][2]string{
		{"POST", "/v1/library/subjects"}, {"PATCH", "/v1/library/subjects/" + id}, {"DELETE", "/v1/library/subjects/" + id},
		{"POST", "/v1/library/" + id + "/comments"}, {"DELETE", "/v1/comments/" + id},
		{"PUT", "/v1/library/" + id + "/rating"}, {"PUT", "/v1/library/" + id + "/bookmark"}, {"DELETE", "/v1/library/" + id + "/bookmark"},
		{"GET", "/v1/bookmarks"}, {"GET", "/v1/moderation/queue"}, {"POST", "/v1/moderation/" + id + "/decision"}, {"POST", "/v1/notes/" + id + "/submit"},
	} {
		rec := e.do(c[0], c[1], `{}`, "")
		if rec.Code != http.StatusUnauthorized || errorCode(t, rec) != "unauthorized" {
			t.Errorf("%s %s without a token: status = %d, want 401", c[0], c[1], rec.Code)
		}
	}
	// До сервиса доходят четыре чтения: запрос файла без хранилища отвечает 503 раньше.
	if e.f.calls != 4 {
		t.Errorf("the service was called %d times, only the 4 guest reads may reach it", e.f.calls)
	}
}

func TestLibrary_AnInvalidTokenIsNotTreatedAsAGuest(t *testing.T) {
	e := newLibEnv(&fakeLibrary{})
	rec := e.do(http.MethodGet, "/v1/library", "", "Bearer garbage")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d: an expired token must make the client refresh instead of silently showing the guest view", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/v1/library", "", "Basic abc"); rec.Code != http.StatusUnauthorized {
		t.Errorf("a non-bearer header: status = %d", rec.Code)
	}
}

func TestLibrary_ViewerIsPassedOnlyWithAValidToken(t *testing.T) {
	e := newLibEnv(&fakeLibrary{})
	e.do(http.MethodGet, "/v1/library", "", "")
	if e.f.gotUser != nil {
		t.Errorf("a guest has no viewer, got %v", e.f.gotUser)
	}
	e.as(http.MethodGet, "/v1/library", "")
	if e.f.gotUser == nil || *e.f.gotUser != e.uid {
		t.Errorf("viewer = %v, want %v", e.f.gotUser, e.uid)
	}
}

func TestListLibrary_PassesFiltersAndMapsTheCard(t *testing.T) {
	item := sampleItem()
	avg := 4.5
	item.RatingAverage, item.RatingCount, item.Views = &avg, 2, 10
	item.Subject = &library.SubjectRef{ID: uuid.New(), Title: "Math"}
	item.Snippet = "a <mark>b</mark>"
	e := newLibEnv(&fakeLibrary{page: library.Page{Items: []library.Item{item}, NextCursor: "abc"}})
	subj := uuid.New()

	rec := e.do(http.MethodGet, "/v1/library?library_subject_id="+subj.String()+"&kind=cheat_sheet&semester=3&sort=rating&limit=7&cursor=xyz&q=%20limits%20", "", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	f := e.f.gotList
	if f.SubjectID == nil || *f.SubjectID != subj || f.Kind == nil || *f.Kind != "cheat_sheet" || f.Semester == nil || *f.Semester != 3 ||
		f.Sort != library.SortRated || f.Limit != 7 || f.Cursor != "xyz" || f.Query != "limits" {
		t.Errorf("filter = %+v", f)
	}
	body := decodeMap(t, rec)
	card := body["items"].([]any)[0].(map[string]any)
	if body["next_cursor"] != "abc" || card["title"] != "Limits" || card["snippet"] != "a <mark>b</mark>" || card["views"] != float64(10) {
		t.Errorf("body = %s", rec.Body)
	}
	rating := card["rating"].(map[string]any)
	if rating["average"] != 4.5 || rating["count"] != float64(2) {
		t.Errorf("rating = %v", rating)
	}
	// Незаполненное отдаётся как null, а не как нулевое значение.
	for _, field := range []string{"summary", "semester", "my_rating"} {
		if v, present := card[field]; !present || v != nil {
			t.Errorf("%s must be present and null, got %v (present %v)", field, v, present)
		}
	}
	if card["bookmarked"] != false {
		t.Errorf("bookmarked = %v", card["bookmarked"])
	}
}

func TestListLibrary_NoRatingsMeansNullAverage(t *testing.T) {
	e := newLibEnv(&fakeLibrary{page: library.Page{Items: []library.Item{sampleItem()}}})
	rec := e.do(http.MethodGet, "/v1/library", "", "")
	rating := decodeMap(t, rec)["items"].([]any)[0].(map[string]any)["rating"].(map[string]any)
	if v, present := rating["average"]; !present || v != nil || rating["count"] != float64(0) {
		t.Errorf("rating = %v: with no ratings the average is null (\"Нет оценок\"), not 0", rating)
	}
	if next := decodeMap(t, rec)["next_cursor"]; next != nil {
		t.Errorf("next_cursor = %v, want null", next)
	}
}

func TestListLibrary_RejectsBadParameters(t *testing.T) {
	e := newLibEnv(&fakeLibrary{})
	for name, query := range map[string]string{
		"unknown kind":     "kind=poster",
		"semester 0":       "semester=0",
		"semester 13":      "semester=13",
		"unknown sort":     "sort=random",
		"limit 0":          "limit=0",
		"limit 101":        "limit=101",
		"blank q":          "q=%20%20",
		"q over 200":       "q=" + strings.Repeat("a", 201),
		"subject not uuid": "library_subject_id=abc",
		"semester not int": "semester=x",
	} {
		rec := e.do(http.MethodGet, "/v1/library?"+query, "", "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}
	if e.f.calls != 0 {
		t.Error("invalid parameters must not reach the service")
	}
}

func TestLibraryErrors_AreMappedToTheDocumentedStatusesAndCodes(t *testing.T) {
	id := uuid.NewString()
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{library.ErrNotFound, 404, "not_found"},
		{library.ErrForbidden, 403, "forbidden"},
		{library.ErrInvalidTransition, 409, "invalid_transition"},
		{fmt.Errorf("%w: a rejection needs a reason", library.ErrInvalid), 400, "validation_failed"},
		{library.ErrInvalidCursor, 400, "validation_failed"},
		{library.ErrSubjectExists, 409, "subject_exists"},
		{library.ErrSubjectInUse, 409, "subject_in_use"},
		{fmt.Errorf("%w: boom", search.ErrUnavailable), 503, "search_unavailable"},
		{errors.New("pq: secret internal detail"), 500, "internal"},
	}
	for _, c := range cases {
		e := newLibEnv(&fakeLibrary{err: c.err})
		rec := e.as(http.MethodGet, "/v1/library/"+id, "")
		if rec.Code != c.status || errorCode(t, rec) != c.code {
			t.Errorf("%v: status = %d code = %q, want %d %q", c.err, rec.Code, errorCode(t, rec), c.status, c.code)
		}
		if strings.Contains(rec.Body.String(), "secret") || strings.Contains(rec.Body.String(), "invalid input:") {
			t.Errorf("%v: the body leaks internals or a service prefix: %s", c.err, rec.Body)
		}
	}
}

func TestGetMaterial_MapsEverythingIncludingNulls(t *testing.T) {
	now := time.Now()
	m := library.Material{
		Item:      sampleItem(),
		Status:    "published",
		Content:   []byte(validDoc),
		Tags:      nil,
		CreatedAt: now, UpdatedAt: now,
		Attachments: []db.Attachment{{ID: uuid.New(), FileName: "a.pdf", MimeType: "application/pdf", SizeBytes: 5, CreatedAt: now}},
	}
	e := newLibEnv(&fakeLibrary{material: m})

	rec := e.do(http.MethodGet, "/v1/library/"+m.ID.String(), "", "")

	if rec.Code != http.StatusOK || e.f.gotID != m.ID {
		t.Fatalf("status = %d, looked up %v", rec.Code, e.f.gotID)
	}
	body := decodeMap(t, rec)
	if body["status"] != "published" || body["content"].(map[string]any)["type"] != "doc" {
		t.Errorf("body = %s", rec.Body)
	}
	if tags, ok := body["tags"].([]any); !ok || len(tags) != 0 {
		t.Errorf("tags must be [] not null: %v", body["tags"])
	}
	if files := body["attachments"].([]any); len(files) != 1 {
		t.Errorf("attachments = %v", files)
	}
	for _, field := range []string{"rejection_reason", "summary", "subject"} {
		if v, present := body[field]; !present || v != nil {
			t.Errorf("%s must be present and null, got %v", field, v)
		}
	}
	for _, secret := range []string{"object_key", "user_id"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Errorf("the response must not expose %q", secret)
		}
	}
}

func TestGetMaterial_StoredDocumentThatIsNotJSON_Is500(t *testing.T) {
	e := newLibEnv(&fakeLibrary{material: library.Material{Item: sampleItem(), Content: []byte("not json")}})
	if rec := e.do(http.MethodGet, "/v1/library/"+uuid.NewString(), "", ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestGetMaterialAttachment_ReturnsALinkAndMapsStorageProblems(t *testing.T) {
	row := db.Attachment{ID: uuid.New(), ObjectKey: "secret-key", FileName: "a.pdf", MimeType: "application/pdf", SizeBytes: 5, CreatedAt: time.Now()}
	f := &fakeLibrary{att: row}
	att := &fakeAttachments{}
	uid := uuid.New()
	tok, _ := auth.NewAccessToken(testSecret, uid, time.Now())
	h := httpapi.NewHandler(httpapi.Deps{DB: fakePinger{}, Auth: &fakeAuth{}, Library: f, Attachments: att, JWTSecret: testSecret})
	get := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/v1/library/"+uuid.NewString()+"/attachments/"+row.ID.String(), nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := get()
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "https://s3.test/get/secret-key") || len(att.linked) != 1 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), `"object_key"`) {
		t.Error("the object key is internal")
	}

	f.err = library.ErrNotFound
	if rec := get(); rec.Code != http.StatusNotFound {
		t.Errorf("an invisible material: status = %d", rec.Code)
	}
	if len(att.linked) != 1 {
		t.Error("no link may be issued for a material the viewer cannot see")
	}
}

func TestGetMaterialAttachment_WithoutStorage_Is503(t *testing.T) {
	h := httpapi.NewHandler(httpapi.Deps{DB: fakePinger{}, Auth: &fakeAuth{}, Library: &fakeLibrary{}, JWTSecret: testSecret})
	req := httptest.NewRequest(http.MethodGet, "/v1/library/"+uuid.NewString()+"/attachments/"+uuid.NewString(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || errorCode(t, rec) != "storage_unavailable" {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestRateMaterial_PassesTheValueAndReturnsTheSummary(t *testing.T) {
	avg := 3.5
	e := newLibEnv(&fakeLibrary{rating: library.RatingResult{Average: &avg, Count: 2, MyRating: 2}})
	id := uuid.New()

	rec := e.as(http.MethodPut, "/v1/library/"+id.String()+"/rating", `{"value":2}`)

	if rec.Code != http.StatusOK || e.f.gotValue != 2 || e.f.gotID != id || *e.f.gotUser != e.uid {
		t.Fatalf("status = %d, got %d for %v by %v", rec.Code, e.f.gotValue, e.f.gotID, e.f.gotUser)
	}
	body := decodeMap(t, rec)
	if body["my_rating"] != float64(2) || body["rating"].(map[string]any)["average"] != 3.5 {
		t.Errorf("body = %s", rec.Body)
	}
	// Отсутствующее поле даёт 0, и его отвергает сервис (см. интеграционные тесты): здесь проверяется разбор тела.
	for _, bad := range []string{`{"value":"5"}`, `{"value":5.5}`, `nope`, `{"value":5,"x":1}`} {
		if rec := e.as(http.MethodPut, "/v1/library/"+id.String()+"/rating", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", bad, rec.Code)
		}
	}
}

func TestComments_CreateListAndDelete(t *testing.T) {
	now := time.Now()
	c := library.Comment{ID: uuid.New(), Body: "Nice", Author: library.Author{ID: uuid.New(), DisplayName: "Bob"}, CreatedAt: now}
	e := newLibEnv(&fakeLibrary{comment: c, comments: library.CommentPage{Items: []library.Comment{c}, NextCursor: "n"}})
	id := uuid.New()

	rec := e.as(http.MethodPost, "/v1/library/"+id.String()+"/comments", `{"body":"Nice"}`)
	if rec.Code != http.StatusCreated || e.f.gotBody != "Nice" || !strings.Contains(rec.Body.String(), `"display_name":"Bob"`) {
		t.Fatalf("create: status = %d, body = %s", rec.Code, rec.Body)
	}

	rec = e.do(http.MethodGet, "/v1/library/"+id.String()+"/comments?limit=5&cursor=c1", "", "")
	if rec.Code != http.StatusOK || e.f.gotLimit != 5 || e.f.gotCur != "c1" || decodeMap(t, rec)["next_cursor"] != "n" {
		t.Errorf("list: status = %d, limit = %d, cursor = %q", rec.Code, e.f.gotLimit, e.f.gotCur)
	}
	if rec := e.do(http.MethodGet, "/v1/library/"+id.String()+"/comments?limit=0", "", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("limit=0: status = %d", rec.Code)
	}

	if rec := e.as(http.MethodDelete, "/v1/comments/"+c.ID.String(), ""); rec.Code != http.StatusNoContent || rec.Body.Len() != 0 || e.f.gotID != c.ID {
		t.Errorf("delete: status = %d, body = %q", rec.Code, rec.Body)
	}
	e.f.err = library.ErrNotFound
	if rec := e.as(http.MethodDelete, "/v1/comments/"+c.ID.String(), ""); rec.Code != http.StatusNotFound {
		t.Errorf("deleting a stranger's comment: status = %d", rec.Code)
	}
}

func TestBookmarks_AddRemoveList(t *testing.T) {
	e := newLibEnv(&fakeLibrary{page: library.Page{Items: []library.Item{sampleItem()}}})
	id := uuid.New()

	if rec := e.as(http.MethodPut, "/v1/library/"+id.String()+"/bookmark", ""); rec.Code != http.StatusNoContent || e.f.gotID != id {
		t.Errorf("add: status = %d", rec.Code)
	}
	if rec := e.as(http.MethodDelete, "/v1/library/"+id.String()+"/bookmark", ""); rec.Code != http.StatusNoContent {
		t.Errorf("remove: status = %d", rec.Code)
	}
	if rec := e.as(http.MethodGet, "/v1/bookmarks?limit=3", ""); rec.Code != http.StatusOK || e.f.gotLimit != 3 || *e.f.gotUser != e.uid {
		t.Errorf("list: status = %d, limit = %d", rec.Code, e.f.gotLimit)
	}
	e.f.err = library.ErrNotFound
	if rec := e.as(http.MethodPut, "/v1/library/"+id.String()+"/bookmark", ""); rec.Code != http.StatusNotFound {
		t.Errorf("bookmarking an unpublished material: status = %d", rec.Code)
	}
}

func TestSubmitNote_ReturnsTheNoteAsTheAuthorSeesIt(t *testing.T) {
	f := &fakeLibrary{}
	e := newLibEnv(f)
	n := sampleNote()
	n.Status = "pending"
	e.notes.note = n
	id := n.ID

	rec := e.as(http.MethodPost, "/v1/notes/"+id.String()+"/submit", "")

	if rec.Code != http.StatusOK || f.gotID != id || *f.gotUser != e.uid {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if body := decodeMap(t, rec); body["status"] != "pending" || body["title"] != "Sample" {
		t.Errorf("body = %s", rec.Body)
	}

	f.err = fmt.Errorf("%w: choose a library subject", library.ErrInvalid)
	if rec := e.as(http.MethodPost, "/v1/notes/"+id.String()+"/submit", ""); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "choose a library subject") {
		t.Errorf("incomplete material: status = %d, body = %s", rec.Code, rec.Body)
	}
	f.err = library.ErrInvalidTransition
	if rec := e.as(http.MethodPost, "/v1/notes/"+id.String()+"/submit", ""); rec.Code != http.StatusConflict || errorCode(t, rec) != "invalid_transition" {
		t.Errorf("wrong status: status = %d", rec.Code)
	}
}

func TestDecideMaterial_PassesTheDecisionAndTheReason(t *testing.T) {
	f := &fakeLibrary{decided: db.Note{Status: "rejected"}}
	e := newLibEnv(f)
	id := uuid.New()

	rec := e.as(http.MethodPost, "/v1/moderation/"+id.String()+"/decision", `{"decision":"rejected","reason":"add sources"}`)

	if rec.Code != http.StatusOK || f.gotDec != [2]string{"rejected", "add sources"} || f.gotID != id {
		t.Fatalf("status = %d, got %v", rec.Code, f.gotDec)
	}
	if body := decodeMap(t, rec); body["status"] != "rejected" || body["id"] != id.String() {
		t.Errorf("body = %s", rec.Body)
	}
	if rec := e.as(http.MethodPost, "/v1/moderation/"+id.String()+"/decision", `{"decision":"approved"}`); rec.Code != http.StatusOK || f.gotDec != [2]string{"approved", ""} {
		t.Errorf("approve: status = %d, got %v", rec.Code, f.gotDec)
	}
	f.err = library.ErrForbidden
	if rec := e.as(http.MethodPost, "/v1/moderation/"+id.String()+"/decision", `{"decision":"approved"}`); rec.Code != http.StatusForbidden {
		t.Errorf("a student: status = %d", rec.Code)
	}
	for _, bad := range []string{`nope`, `{"decision":"approved","x":1}`, `{"decision":5}`} {
		if rec := e.as(http.MethodPost, "/v1/moderation/"+id.String()+"/decision", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", bad, rec.Code)
		}
	}
}

func TestModerationQueue_MapsItemsAndPaging(t *testing.T) {
	now := time.Now()
	item := library.QueueItem{ID: uuid.New(), Title: "Waiting", Kind: "cheat_sheet", Author: library.Author{ID: uuid.New(), DisplayName: "Ann"}, SubmittedAt: now, CreatedAt: now}
	e := newLibEnv(&fakeLibrary{queue: library.QueuePage{Items: []library.QueueItem{item}, NextCursor: "q2"}})

	rec := e.as(http.MethodGet, "/v1/moderation/queue?limit=4&cursor=q1", "")

	if rec.Code != http.StatusOK || e.f.gotLimit != 4 || e.f.gotCur != "q1" {
		t.Fatalf("status = %d, limit = %d, cursor = %q", rec.Code, e.f.gotLimit, e.f.gotCur)
	}
	body := decodeMap(t, rec)
	card := body["items"].([]any)[0].(map[string]any)
	if body["next_cursor"] != "q2" || card["title"] != "Waiting" || card["kind"] != "cheat_sheet" || card["subject"] != nil {
		t.Errorf("body = %s", rec.Body)
	}
	e.f.err = library.ErrForbidden
	if rec := e.as(http.MethodGet, "/v1/moderation/queue", ""); rec.Code != http.StatusForbidden || errorCode(t, rec) != "forbidden" {
		t.Errorf("a student: status = %d", rec.Code)
	}
}

func TestLibrarySubjects_ListCreateUpdateDelete(t *testing.T) {
	f := &fakeLibrary{subjects: []library.Subject{{ID: uuid.New(), Title: "Math", MaterialsCount: 3}}}
	e := newLibEnv(f)
	id := uuid.New()

	rec := e.do(http.MethodGet, "/v1/library/subjects", "", "")
	items := decodeMap(t, rec)["items"].([]any)
	if rec.Code != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["materials_count"] != float64(3) {
		t.Errorf("list: %d %s", rec.Code, rec.Body)
	}
	if v, present := items[0].(map[string]any)["color"]; !present || v != nil {
		t.Errorf("color must be present and null: %v", v)
	}

	rec = e.as(http.MethodPost, "/v1/library/subjects", `{"title":"Databases","color":"#f97316"}`)
	if rec.Code != http.StatusCreated || f.gotBody != "Databases" {
		t.Errorf("create: status = %d, body = %s", rec.Code, rec.Body)
	}
	if rec := e.as(http.MethodPost, "/v1/library/subjects", `{"title":"x","color":5}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a malformed body: status = %d", rec.Code)
	}

	if rec := e.as(http.MethodPatch, "/v1/library/subjects/"+id.String(), `{"title":"Renamed"}`); rec.Code != http.StatusOK || f.gotID != id {
		t.Errorf("update: status = %d", rec.Code)
	}
	if rec := e.as(http.MethodPatch, "/v1/library/subjects/"+id.String(), `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("update without fields: status = %d", rec.Code)
	}
	if rec := e.as(http.MethodDelete, "/v1/library/subjects/"+id.String(), ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete: status = %d", rec.Code)
	}
	f.err = library.ErrSubjectInUse
	if rec := e.as(http.MethodDelete, "/v1/library/subjects/"+id.String(), ""); rec.Code != http.StatusConflict || errorCode(t, rec) != "subject_in_use" {
		t.Errorf("delete a used subject: status = %d", rec.Code)
	}
}

// Новые поля заметки: тип, описание, семестр и общий предмет попадают в сервис и проверяются.
func TestNotes_MaterialFieldsAreValidatedAndPassedThrough(t *testing.T) {
	f := &fakeNotes{}
	e := newNotesEnv(f)
	subject := uuid.New()

	rec := e.do(http.MethodPost, "/v1/notes", fmt.Sprintf(
		`{"title":"T","content":%s,"kind":"cheat_sheet","summary":"  short  ","semester":3,"library_subject_id":%q}`, validDoc, subject))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	in := f.gotInput
	if in.Kind != "cheat_sheet" || in.Description == nil || *in.Description != "short" || in.Semester == nil || *in.Semester != 3 ||
		in.LibrarySubjectID == nil || *in.LibrarySubjectID != subject {
		t.Errorf("input = %+v", in)
	}

	for name, fields := range map[string]string{
		"bad kind":       `"kind":"poster"`,
		"semester 13":    `"semester":13`,
		"semester 0":     `"semester":0`,
		"summary 501":    `"summary":"` + strings.Repeat("я", 501) + `"`,
		"subject not id": `"library_subject_id":"x"`,
	} {
		rec := e.do(http.MethodPost, "/v1/notes", fmt.Sprintf(`{"title":"T","content":%s,%s}`, validDoc, fields))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, rec.Code)
		}
	}
	if rec := e.do(http.MethodPost, "/v1/notes", fmt.Sprintf(`{"title":"T","content":%s,"summary":"`+strings.Repeat("я", 500)+`"}`, validDoc)); rec.Code != http.StatusCreated {
		t.Errorf("500 multibyte characters of summary must pass: %d", rec.Code)
	}
}

func TestNotes_PatchMaterialFields_NullClearsThem(t *testing.T) {
	f := &fakeNotes{note: sampleNote()}
	e := newNotesEnv(f)
	id := uuid.New()

	rec := e.do(http.MethodPatch, "/v1/notes/"+id.String(), `{"summary":null,"semester":null,"library_subject_id":null}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	p := f.gotPatch
	if !p.SetDescription || p.Description != nil || !p.SetSemester || p.Semester != nil || !p.SetLibrarySubject || p.LibrarySubjectID != nil {
		t.Errorf("null must clear the fields: %+v", p)
	}
	if p.Kind != nil {
		t.Error("kind was not sent and must stay untouched")
	}

	if rec := e.do(http.MethodPatch, "/v1/notes/"+id.String(), `{"kind":"cheat_sheet"}`); rec.Code != http.StatusOK || f.gotPatch.Kind == nil || *f.gotPatch.Kind != "cheat_sheet" {
		t.Errorf("kind patch: status = %d, %+v", rec.Code, f.gotPatch)
	}
	if rec := e.do(http.MethodPatch, "/v1/notes/"+id.String(), `{}`); rec.Code != http.StatusBadRequest {
		t.Errorf("empty patch: status = %d", rec.Code)
	}
	f.err = note.ErrInvalidLibrarySubject
	if rec := e.do(http.MethodPatch, "/v1/notes/"+id.String(), fmt.Sprintf(`{"library_subject_id":%q}`, uuid.New())); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "library subject") {
		t.Errorf("unknown library subject: status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestNoteResponses_CarryTheMaterialFields(t *testing.T) {
	n := sampleNote()
	n.Kind, n.Status = "cheat_sheet", "rejected"
	reason := "needs sources"
	n.RejectionReason = &reason
	semester := int16(2)
	n.Semester = &semester
	n.Views = 9
	e := newNotesEnv(&fakeNotes{note: n})

	body := decodeMap(t, e.do(http.MethodGet, "/v1/notes/"+n.ID.String(), ""))

	if body["kind"] != "cheat_sheet" || body["status"] != "rejected" || body["rejection_reason"] != "needs sources" || body["semester"] != float64(2) || body["views"] != float64(9) {
		t.Errorf("body = %v", body)
	}
	for _, field := range []string{"summary", "published_at", "library_subject_id"} {
		if v, present := body[field]; !present || v != nil {
			t.Errorf("%s must be present and null, got %v", field, v)
		}
	}
}
