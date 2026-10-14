package httpapi_test

import (
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
)

// ---------------------------------------------------------------------------
// Сброс пароля по почте
// ---------------------------------------------------------------------------

func TestPasswordReset_IsOpenToGuests_AndAlwaysAnswersTheSame(t *testing.T) {
	e := newAccountEnv(t)

	for _, email := range []string{"known@x.io", "nobody@x.io"} {
		req := httptest.NewRequest("POST", "/v1/auth/password-reset", strings.NewReader(`{"email":"`+email+`"}`))
		guest := httptest.NewRecorder()
		e.h.ServeHTTP(guest, req)
		if guest.Code != http.StatusNoContent || guest.Body.Len() != 0 {
			t.Errorf("%s as a guest: status = %d, body = %q, want an empty 204", email, guest.Code, guest.Body)
		}
	}
	if e.acc.resetEmail != "nobody@x.io" {
		t.Errorf("the service got %q", e.acc.resetEmail)
	}
}

func TestPasswordReset_WithoutAMailServer_Is503(t *testing.T) {
	e := newAccountEnv(t)
	e.acc.resetErr = auth.ErrResetUnavailable
	req := httptest.NewRequest("POST", "/v1/auth/password-reset", strings.NewReader(`{"email":"a@x.io"}`))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || errorCode(t, rec) != "email_unavailable" {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func TestPasswordReset_RejectsBadInput_AndHidesInternalErrors(t *testing.T) {
	for name, body := range map[string]string{"no email": `{}`, "not an email": `{"email":"nope"}`, "not json": `x`, "extra field": `{"email":"a@x.io","x":1}`} {
		e := newAccountEnv(t)
		req := httptest.NewRequest("POST", "/v1/auth/password-reset", strings.NewReader(body))
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest || e.acc.resetEmail != "" {
			t.Errorf("%s: status = %d, the service must not be called", name, rec.Code)
		}
	}
	e := newAccountEnv(t)
	e.acc.resetErr = errors.New("pq: secret internal detail")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/auth/password-reset", strings.NewReader(`{"email":"a@x.io"}`)))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body)
	}
}

func confirmReset(e accountEnv, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/auth/password-reset/confirm", strings.NewReader(body)))
	return rec
}

func TestConfirmPasswordReset_OK_Returns204AndClearsTheCookie(t *testing.T) {
	e := newAccountEnv(t)

	rec := confirmReset(e, `{"token":"abc","new_password":"brand-new-pass"}`)

	if rec.Code != http.StatusNoContent || e.acc.resetToken != "abc" || e.acc.resetPass != "brand-new-pass" {
		t.Fatalf("status = %d, service got %q / %q", rec.Code, e.acc.resetToken, e.acc.resetPass)
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("all sessions are over, the browser's refresh cookie must be cleared")
	}
}

func TestConfirmPasswordReset_BadLinkIs401_BadInputIs400(t *testing.T) {
	e := newAccountEnv(t)
	e.acc.confirmErr = auth.ErrInvalidToken
	if rec := confirmReset(e, `{"token":"x","new_password":"brand-new-pass"}`); rec.Code != http.StatusUnauthorized || errorCode(t, rec) != "invalid_token" {
		t.Errorf("a dead link: status = %d, body = %s", rec.Code, rec.Body)
	}
	e.acc.confirmErr = auth.ErrUserNotFound
	if rec := confirmReset(e, `{"token":"x","new_password":"brand-new-pass"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("a link of a deleted account: status = %d", rec.Code)
	}
	e.acc.confirmErr = errors.New("pq: secret")
	if rec := confirmReset(e, `{"token":"x","new_password":"brand-new-pass"}`); rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("internal error: status = %d", rec.Code)
	}

	e = newAccountEnv(t)
	for name, body := range map[string]string{
		"no token": `{"new_password":"brand-new-pass"}`, "empty token": `{"token":"","new_password":"brand-new-pass"}`,
		"short password": `{"token":"x","new_password":"short"}`, "73 bytes": `{"token":"x","new_password":"` + strings.Repeat("a", 73) + `"}`,
		"no password": `{"token":"x"}`, "not json": `x`,
	} {
		if rec := confirmReset(e, body); rec.Code != http.StatusBadRequest || e.acc.resetToken != "" {
			t.Errorf("%s: status = %d, the service must not be called", name, rec.Code)
		}
	}
}

func TestPasswordReset_IsRateLimited_LikeLogin(t *testing.T) {
	acc := &fakeAccount{}
	h := httpapi.NewHandler(httpapi.Deps{DB: fakePinger{}, Auth: &fakeAuth{}, Account: acc, JWTSecret: testSecret, AuthRateLimit: 2})
	bodies := map[string]string{
		"/v1/auth/password-reset":         `{"email":"a@x.io"}`,
		"/v1/auth/password-reset/confirm": `{"token":"t","new_password":"brand-new-pass"}`,
	}
	send := func(path string) int {
		req := httptest.NewRequest("POST", path, strings.NewReader(bodies[path]))
		req.RemoteAddr = "192.0.2.50:1000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	// запросы ссылок и подтверждения делят один бюджет: иначе письма можно было бы рассылать без счёта
	if a, b, c := send("/v1/auth/password-reset"), send("/v1/auth/password-reset/confirm"), send("/v1/auth/password-reset"); a != 204 || b != 204 || c != http.StatusTooManyRequests {
		t.Errorf("statuses = %d %d %d, want 204 204 429", a, b, c)
	}
}

// ---------------------------------------------------------------------------
// Удаление аккаунта
// ---------------------------------------------------------------------------

func TestDeleteMe_RemovesTheAccountAndItsFiles(t *testing.T) {
	uid := uuid.New()
	acc := &fakeAccount{user: db.User{ID: uid}}
	att := &fakeAttachments{keys: []string{"u/1", "u/2"}}
	tok, _ := auth.NewAccessToken(testSecret, uid, time.Now())
	h := httpapi.NewHandler(httpapi.Deps{DB: fakePinger{}, Auth: &fakeAuth{}, Account: acc, Attachments: att, JWTSecret: testSecret})
	do := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("DELETE", "/v1/me", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := do(`{"password":"my-password"}`)

	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 || acc.gotID != uid || acc.deletePass != "my-password" {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body)
	}
	if len(att.removed) != 2 || att.removed[0] != "u/1" {
		t.Errorf("the stored files must be removed after the account: %v", att.removed)
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		cleared = cleared || (c.Name == "refresh_token" && c.MaxAge < 0)
	}
	if !cleared {
		t.Error("the refresh cookie must be cleared")
	}

	// Неверный пароль: аккаунт и файлы целы.
	acc.deleteErr = auth.ErrInvalidCredentials
	att.removed = nil
	if rec := do(`{"password":"guess"}`); rec.Code != http.StatusForbidden || errorCode(t, rec) != "invalid_credentials" || len(att.removed) != 0 {
		t.Errorf("wrong password: status = %d, removed %v", rec.Code, att.removed)
	}
	acc.deleteErr = auth.ErrUserNotFound
	if rec := do(`{"password":"x"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("an account that is already gone: status = %d", rec.Code)
	}
	acc.deleteErr = errors.New("pq: secret")
	if rec := do(`{"password":"x"}`); rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret") || len(att.removed) != 0 {
		t.Errorf("internal error: status = %d, removed %v", rec.Code, att.removed)
	}
	acc.deleteErr = nil
	att.keysErr = errors.New("db down")
	calls := acc.deleteCalls
	if rec := do(`{"password":"x"}`); rec.Code != http.StatusInternalServerError || acc.deleteCalls != calls {
		t.Errorf("if the files cannot be listed the account must not be deleted: status = %d", rec.Code)
	}
	att.keysErr = nil
	for _, bad := range []string{`{}`, `{"password":""}`, `x`, `{"password":"x","y":1}`} {
		if rec := do(bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", bad, rec.Code)
		}
	}
}

func TestDeleteMe_RequiresAToken_AndIsRateLimited(t *testing.T) {
	e := newAccountEnv(t)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, httptest.NewRequest("DELETE", "/v1/me", strings.NewReader(`{"password":"x"}`)))
	if rec.Code != http.StatusUnauthorized || e.acc.deleteCalls != 0 {
		t.Errorf("without a token: status = %d", rec.Code)
	}

	acc := &fakeAccount{deleteErr: auth.ErrInvalidCredentials}
	uid := uuid.New()
	tok, _ := auth.NewAccessToken(testSecret, uid, time.Now())
	h := httpapi.NewHandler(httpapi.Deps{DB: fakePinger{}, Auth: &fakeAuth{}, Account: acc, JWTSecret: testSecret, AuthRateLimit: 2})
	send := func() int {
		req := httptest.NewRequest("DELETE", "/v1/me", strings.NewReader(`{"password":"guess"}`))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.RemoteAddr = "192.0.2.60:1000"
		r := httptest.NewRecorder()
		h.ServeHTTP(r, req)
		return r.Code
	}
	if a, b, c := send(), send(), send(); a != http.StatusForbidden || b != http.StatusForbidden || c != http.StatusTooManyRequests {
		t.Errorf("statuses = %d %d %d: guessing the password through the deletion endpoint must be limited", a, b, c)
	}
}

// ---------------------------------------------------------------------------
// Отзыв, снятие, правка комментария, авторы, фильтры ленты
// ---------------------------------------------------------------------------

func TestWithdrawNote_ReturnsTheNoteAsTheAuthorSeesIt(t *testing.T) {
	f := &fakeLibrary{}
	e := newLibEnv(f)
	n := sampleNote()
	n.Status = "draft"
	e.notes.note = n

	rec := e.as(http.MethodPost, "/v1/notes/"+n.ID.String()+"/withdraw", "")

	if rec.Code != http.StatusOK || f.gotID != n.ID || *f.gotUser != e.uid || decodeMap(t, rec)["status"] != "draft" {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	f.err = library.ErrInvalidTransition
	if rec := e.as(http.MethodPost, "/v1/notes/"+n.ID.String()+"/withdraw", ""); rec.Code != http.StatusConflict || errorCode(t, rec) != "invalid_transition" {
		t.Errorf("a material that is not pending: status = %d", rec.Code)
	}
	f.err = library.ErrNotFound
	if rec := e.as(http.MethodPost, "/v1/notes/"+n.ID.String()+"/withdraw", ""); rec.Code != http.StatusNotFound {
		t.Errorf("someone else's note: status = %d", rec.Code)
	}
}

func TestUnpublish_PassesTheReason_AndMapsTheRules(t *testing.T) {
	f := &fakeLibrary{decided: db.Note{Status: "rejected"}}
	e := newLibEnv(f)
	id := uuid.New()

	rec := e.as(http.MethodPost, "/v1/moderation/"+id.String()+"/unpublish", `{"reason":"inaccurate theorem"}`)

	if rec.Code != http.StatusOK || f.gotBody != "inaccurate theorem" || f.gotID != id {
		t.Fatalf("status = %d, reason = %q", rec.Code, f.gotBody)
	}
	if body := decodeMap(t, rec); body["status"] != "rejected" || body["id"] != id.String() {
		t.Errorf("body = %s", rec.Body)
	}
	for _, c := range []struct {
		err    error
		status int
		code   string
	}{{library.ErrForbidden, 403, "forbidden"}, {library.ErrInvalidTransition, 409, "invalid_transition"}, {library.ErrNotFound, 404, "not_found"},
		{fmt.Errorf("%w: needs a reason", library.ErrInvalid), 400, "validation_failed"}} {
		f.err = c.err
		if rec := e.as(http.MethodPost, "/v1/moderation/"+id.String()+"/unpublish", `{"reason":"x"}`); rec.Code != c.status || errorCode(t, rec) != c.code {
			t.Errorf("%v: status = %d code = %q", c.err, rec.Code, errorCode(t, rec))
		}
	}
	f.err = nil
	for _, bad := range []string{`nope`, `{"reason":5}`, `{"reason":"x","y":1}`} {
		if rec := e.as(http.MethodPost, "/v1/moderation/"+id.String()+"/unpublish", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d", bad, rec.Code)
		}
	}
}

func TestUpdateComment_ReturnsTheEditedComment(t *testing.T) {
	now := time.Now()
	edited := library.Comment{ID: uuid.New(), Body: "fixed typo", Author: library.Author{ID: uuid.New(), DisplayName: "Bob"}, CreatedAt: now, EditedAt: &now}
	f := &fakeLibrary{edited: edited}
	e := newLibEnv(f)

	rec := e.as(http.MethodPatch, "/v1/comments/"+edited.ID.String(), `{"body":"fixed typo"}`)

	body := decodeMap(t, rec)
	if rec.Code != http.StatusOK || f.gotBody != "fixed typo" || f.gotID != edited.ID || body["edited_at"] == nil {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	f.err = library.ErrNotFound
	if rec := e.as(http.MethodPatch, "/v1/comments/"+edited.ID.String(), `{"body":"x"}`); rec.Code != http.StatusNotFound {
		t.Errorf("someone else's comment: status = %d", rec.Code)
	}
	if rec := e.as(http.MethodPatch, "/v1/comments/"+edited.ID.String(), `{"body":5}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a malformed body: status = %d", rec.Code)
	}
}

func TestComments_ANewCommentHasANullEditedAt(t *testing.T) {
	c := library.Comment{ID: uuid.New(), Body: "x", Author: library.Author{ID: uuid.New(), DisplayName: "Bob"}, CreatedAt: time.Now()}
	e := newLibEnv(&fakeLibrary{comments: library.CommentPage{Items: []library.Comment{c}}})
	items := decodeMap(t, e.do(http.MethodGet, "/v1/library/"+uuid.NewString()+"/comments", "", ""))["items"].([]any)
	if v, present := items[0].(map[string]any)["edited_at"]; !present || v != nil {
		t.Errorf("edited_at must be present and null, got %v", v)
	}
}

func TestAuthorRanking_IsPublic_AndMapsTheStats(t *testing.T) {
	avg := 4.5
	ranked := []library.RankedAuthor{
		{Rank: 1, AuthorStats: library.AuthorStats{Author: library.Author{ID: uuid.New(), DisplayName: "Ann"}, MaterialsCount: 3, RatingsCount: 10, RatingAverage: &avg, Views: 99}},
		{Rank: 2, AuthorStats: library.AuthorStats{Author: library.Author{ID: uuid.New(), DisplayName: "Bob"}, MaterialsCount: 1}},
	}
	f := &fakeLibrary{ranking: ranked}
	e := newLibEnv(f)

	rec := e.do(http.MethodGet, "/v1/ranking/authors?limit=5", "", "")

	if rec.Code != http.StatusOK || f.gotLimit != 5 {
		t.Fatalf("status = %d, limit = %d", rec.Code, f.gotLimit)
	}
	items := decodeMap(t, rec)["items"].([]any)
	first, second := items[0].(map[string]any), items[1].(map[string]any)
	if first["rank"] != float64(1) || first["rating"].(map[string]any)["average"] != 4.5 || first["materials_count"] != float64(3) || first["views"] != float64(99) {
		t.Errorf("first = %v", first)
	}
	if v, present := second["rating"].(map[string]any)["average"]; !present || v != nil {
		t.Errorf("an author without ratings has a null average, got %v", v)
	}
	for _, bad := range []string{"limit=0", "limit=101", "limit=x"} {
		if rec := e.do(http.MethodGet, "/v1/ranking/authors?"+bad, "", ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d", bad, rec.Code)
		}
	}
	if rec := e.do(http.MethodGet, "/v1/ranking/authors", "", "Bearer garbage"); rec.Code != http.StatusUnauthorized {
		t.Errorf("a broken token is still an error: status = %d", rec.Code)
	}
}

func TestGetAuthor_IsPublic_And404ForAnyoneWithoutPublishedWork(t *testing.T) {
	id := uuid.New()
	f := &fakeLibrary{author: library.AuthorStats{Author: library.Author{ID: id, DisplayName: "Ann"}, MaterialsCount: 2}}
	e := newLibEnv(f)

	rec := e.do(http.MethodGet, "/v1/authors/"+id.String(), "", "")

	body := decodeMap(t, rec)
	if rec.Code != http.StatusOK || f.gotID != id || body["author"].(map[string]any)["display_name"] != "Ann" || body["materials_count"] != float64(2) {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	f.err = library.ErrNotFound
	if rec := e.do(http.MethodGet, "/v1/authors/"+uuid.NewString(), "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/v1/authors/not-a-uuid", "", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("a malformed id: status = %d", rec.Code)
	}
}

func TestListLibrary_PassesTheAuthorAndTagFilters_AndCardsCarryTags(t *testing.T) {
	item := sampleItem()
	item.Tags = []string{"exam", "lecture"}
	f := &fakeLibrary{page: library.Page{Items: []library.Item{item, sampleItem()}}}
	e := newLibEnv(f)
	author := uuid.New()

	rec := e.do(http.MethodGet, "/v1/library?author_id="+author.String()+"&tag=%20exam%20", "", "")

	if rec.Code != http.StatusOK || f.gotList.AuthorID == nil || *f.gotList.AuthorID != author || f.gotList.Tag == nil || *f.gotList.Tag != "exam" {
		t.Fatalf("status = %d, filter = %+v", rec.Code, f.gotList)
	}
	items := decodeMap(t, rec)["items"].([]any)
	tags := items[0].(map[string]any)["tags"].([]any)
	if len(tags) != 2 || tags[0] != "exam" {
		t.Errorf("tags = %v", tags)
	}
	if empty, ok := items[1].(map[string]any)["tags"].([]any); !ok || len(empty) != 0 {
		t.Errorf("a material without tags must give [], not null: %v", items[1].(map[string]any)["tags"])
	}
	for _, bad := range []string{"tag=", "tag=%20%20", "tag=" + strings.Repeat("a", 51), "author_id=x"} {
		if rec := e.do(http.MethodGet, "/v1/library?"+bad, "", ""); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", bad, rec.Code)
		}
	}
}

// Любой эндпоинт библиотеки при неожиданной ошибке сервиса отвечает 500 в общем формате и не выдаёт подробностей.
func TestLibraryEndpoints_UnexpectedErrorsAreHiddenBehind500(t *testing.T) {
	id := uuid.NewString()
	for _, c := range []struct{ method, path, body string }{
		{"GET", "/v1/library", ""}, {"GET", "/v1/library/" + id, ""}, {"GET", "/v1/library-subjects", ""},
		{"POST", "/v1/library-subjects", `{"title":"x"}`}, {"PATCH", "/v1/library-subjects/" + id, `{"title":"x"}`}, {"DELETE", "/v1/library-subjects/" + id, ""},
		{"GET", "/v1/library/" + id + "/comments", ""}, {"POST", "/v1/library/" + id + "/comments", `{"body":"x"}`},
		{"PATCH", "/v1/comments/" + id, `{"body":"x"}`}, {"DELETE", "/v1/comments/" + id, ""},
		{"PUT", "/v1/library/" + id + "/rating", `{"value":3}`}, {"PUT", "/v1/library/" + id + "/bookmark", ""},
		{"DELETE", "/v1/library/" + id + "/bookmark", ""}, {"GET", "/v1/bookmarks", ""},
		{"GET", "/v1/moderation/queue", ""}, {"POST", "/v1/moderation/" + id + "/decision", `{"decision":"approved"}`},
		{"POST", "/v1/moderation/" + id + "/unpublish", `{"reason":"x"}`}, {"POST", "/v1/notes/" + id + "/submit", ""},
		{"POST", "/v1/notes/" + id + "/withdraw", ""}, {"GET", "/v1/ranking/authors", ""}, {"GET", "/v1/authors/" + id, ""},
	} {
		e := newLibEnv(&fakeLibrary{err: errors.New("pq: secret internal detail")})
		rec := e.as(c.method, c.path, c.body)
		if rec.Code != http.StatusInternalServerError || errorCode(t, rec) != "internal" || strings.Contains(rec.Body.String(), "secret") {
			t.Errorf("%s %s: status = %d, body = %s", c.method, c.path, rec.Code, rec.Body)
		}
	}
}
