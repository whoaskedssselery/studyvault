package library_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/library"
	"github.com/whoaskedssselery/studyvault/backend/internal/note"
	"github.com/whoaskedssselery/studyvault/backend/internal/search"
)

// Интеграционные тесты идут на настоящем Postgres внутри транзакции, которая откатывается в конце.
// Это важно: права доступа библиотеки держатся на SQL, и юнит-тесты с подменой хранилища их не проверили бы.
type env struct {
	tx      pgx.Tx
	q       *db.Queries
	lib     *library.Service
	notes   *note.Service
	subject db.LibrarySubject
	engine  *fakeEngine
}

// fakeEngine подменяет Meilisearch: возвращает заранее заданных «кандидатов», как мог бы отстающий индекс.
type fakeEngine struct{ hits []search.Hit }

func (f *fakeEngine) SearchPublished(context.Context, string, int) ([]search.Hit, error) {
	return f.hits, nil
}

func newEnv(t *testing.T) *env {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Skipf("database is not reachable: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	q := db.New(tx)
	e := &env{tx: tx, q: q, engine: &fakeEngine{}}
	e.lib = library.NewService(q, library.WithEngine(e.engine))
	e.notes = note.NewService(q)
	e.subject, err = q.CreateLibrarySubject(ctx, db.CreateLibrarySubjectParams{Title: "Math " + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) user(t *testing.T, name string) uuid.UUID {
	t.Helper()
	u, err := e.q.CreateUser(context.Background(), db.CreateUserParams{
		Email: name + "-" + uuid.NewString() + "@x.io", PasswordHash: "x", DisplayName: name,
	})
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

func (e *env) moderator(t *testing.T, name string) uuid.UUID {
	t.Helper()
	id := e.user(t, name)
	if _, err := e.tx.Exec(context.Background(), `UPDATE users SET role = 'moderator' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func doc(text string) []byte {
	return []byte(`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"` + text + `"}]}]}`)
}

// draft создаёт заметку с общим предметом: она готова к отправке на проверку.
func (e *env) draft(t *testing.T, author uuid.UUID, title, text string) uuid.UUID {
	t.Helper()
	n, err := e.notes.Create(context.Background(), author, note.Input{
		Title: title, Content: doc(text), LibrarySubjectID: &e.subject.ID, Kind: note.KindNote,
	})
	if err != nil {
		t.Fatal(err)
	}
	return n.ID
}

func (e *env) pending(t *testing.T, author uuid.UUID, title, text string) uuid.UUID {
	t.Helper()
	id := e.draft(t, author, title, text)
	if _, err := e.lib.Submit(context.Background(), author, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *env) published(t *testing.T, author, mod uuid.UUID, title, text string) uuid.UUID {
	t.Helper()
	id := e.pending(t, author, title, text)
	if _, err := e.lib.Decide(context.Background(), mod, id, library.DecisionApproved, ""); err != nil {
		t.Fatal(err)
	}
	return id
}

func ptr[T any](v T) *T { return &v }

// attempt выполняет действие в точке сохранения и откатывает её. Нарушение ограничения БД (дубликат, внешний
// ключ) отравляет всю транзакцию теста, а в проде каждый запрос идёт в своей транзакции, и этого не бывает.
func (e *env) attempt(t *testing.T, fn func(svc *library.Service)) {
	t.Helper()
	ctx := context.Background()
	sp, err := e.tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sp.Rollback(ctx) }()
	fn(library.NewService(db.New(sp)))
}

func ids(items []library.Item) []uuid.UUID {
	out := make([]uuid.UUID, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func contains(list []uuid.UUID, id uuid.UUID) bool {
	for _, x := range list {
		if x == id {
			return true
		}
	}
	return false
}

// Сквозной сценарий из ТЗ: создание -> модерация -> чтение -> комментарий -> оценка.
func TestIntegration_EndToEnd_CreateModerateReadCommentRate(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	alice, bob, carol, mod := e.user(t, "alice"), e.user(t, "bob"), e.user(t, "carol"), e.moderator(t, "mia")

	id := e.draft(t, alice, "Limits", "limit of a sequence")
	if _, err := e.lib.Get(ctx, nil, id); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("a draft must be invisible to a guest, err = %v", err)
	}
	if _, err := e.lib.Submit(ctx, alice, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.Get(ctx, &bob, id); !errors.Is(err, library.ErrNotFound) {
		t.Fatalf("a pending material must be invisible to another student, err = %v", err)
	}
	if m, err := e.lib.Get(ctx, &mod, id); err != nil || m.Status != "pending" {
		t.Fatalf("the moderator must see a pending material: %+v, %v", m.Status, err)
	}
	if _, err := e.lib.Decide(ctx, bob, id, library.DecisionApproved, ""); !errors.Is(err, library.ErrForbidden) {
		t.Fatalf("a student must not decide, err = %v", err)
	}
	if _, err := e.lib.Decide(ctx, mod, id, library.DecisionApproved, ""); err != nil {
		t.Fatal(err)
	}

	// Опубликовано: видят гость и студент, в ленте он есть.
	if m, err := e.lib.Get(ctx, nil, id); err != nil || m.Status != "published" || m.PublishedAt == nil {
		t.Fatalf("a guest must see a published material: %+v, %v", m, err)
	}
	page, err := e.lib.List(ctx, nil, library.ListFilter{})
	if err != nil || !contains(ids(page.Items), id) {
		t.Fatalf("the published material is missing from the feed: %v, %v", ids(page.Items), err)
	}

	// Комментарий сохраняется и виден после повторного чтения; пустой текст отвергается.
	if _, err := e.lib.AddComment(ctx, bob, id, "   "); !errors.Is(err, library.ErrInvalid) {
		t.Errorf("an empty comment must be rejected, err = %v", err)
	}
	c, err := e.lib.AddComment(ctx, bob, id, "  Thanks, helpful!  ")
	if err != nil || c.Body != "Thanks, helpful!" || c.Author.DisplayName != "bob" {
		t.Fatalf("comment = %+v, %v", c, err)
	}
	comments, err := e.lib.ListComments(ctx, id, 10, "")
	if err != nil || len(comments.Items) != 1 || comments.Items[0].ID != c.ID {
		t.Fatalf("comments = %+v, %v", comments, err)
	}

	// Оценки: допустимы только 1-5, повторная обновляет прежнюю, среднее равно среднему по таблице.
	for _, bad := range []int{0, 6, -1} {
		if _, err := e.lib.Rate(ctx, bob, id, bad); !errors.Is(err, library.ErrInvalid) {
			t.Errorf("rating %d must be rejected, err = %v", bad, err)
		}
	}
	if _, err := e.lib.Rate(ctx, alice, id, 5); !errors.Is(err, library.ErrForbidden) {
		t.Errorf("rating one's own material must be forbidden, err = %v", err)
	}
	if _, err := e.lib.Rate(ctx, bob, id, 4); err != nil {
		t.Fatal(err)
	}
	res, err := e.lib.Rate(ctx, carol, id, 5)
	if err != nil || res.Count != 2 || res.Average == nil || *res.Average != 4.5 {
		t.Fatalf("after two ratings: %+v, %v", res, err)
	}
	res, err = e.lib.Rate(ctx, bob, id, 2) // bob меняет мнение
	if err != nil || res.Count != 2 || *res.Average != 3.5 || res.MyRating != 2 {
		t.Fatalf("after a re-rating: %+v, %v (a second rating of the same user must update, not add)", res, err)
	}
	var sum float64
	var n int
	if err := e.tx.QueryRow(ctx, `SELECT COALESCE(avg(value), 0)::float8, count(*) FROM note_ratings WHERE note_id = $1`, id).Scan(&sum, &n); err != nil {
		t.Fatal(err)
	}
	if n != 2 || sum != 3.5 {
		t.Errorf("the table says %v over %d ratings", sum, n)
	}

	// Личное состояние зрителя и счётчики в карточке.
	m, err := e.lib.Get(ctx, &bob, id)
	if err != nil || m.MyRating == nil || *m.MyRating != 2 || m.CommentsCount != 1 || m.RatingCount != 2 {
		t.Fatalf("material for bob: my=%v comments=%d ratings=%d, %v", m.MyRating, m.CommentsCount, m.RatingCount, err)
	}
	if g, _ := e.lib.Get(ctx, nil, id); g.MyRating != nil || g.Bookmarked {
		t.Error("a guest has neither a rating nor bookmarks")
	}
}

func TestIntegration_NoRatingsMeansNoAverage(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, mod := e.user(t, "a"), e.moderator(t, "m")
	id := e.published(t, author, mod, "Fresh", "text")

	m, err := e.lib.Get(ctx, nil, id)
	if err != nil || m.RatingAverage != nil || m.RatingCount != 0 {
		t.Errorf("a material without ratings must report no average (\"Нет оценок\"), got %v / %d, %v", m.RatingAverage, m.RatingCount, err)
	}
}

// Кто что видит: гость, автор, другой студент, модератор. Для каждого статуса.
func TestIntegration_VisibilityMatrix(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, other, mod := e.user(t, "author"), e.user(t, "other"), e.moderator(t, "mod")

	draft := e.draft(t, author, "Draft", "d")
	pending := e.pending(t, author, "Pending", "p")
	published := e.published(t, author, mod, "Published", "x")
	rejected := e.pending(t, author, "Rejected", "r")
	if _, err := e.lib.Decide(ctx, mod, rejected, library.DecisionRejected, "needs sources"); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		id      uuid.UUID
		viewer  *uuid.UUID
		visible bool
	}{
		{"draft/guest", draft, nil, false}, {"draft/other", draft, &other, false}, {"draft/moderator", draft, &mod, false}, {"draft/author", draft, &author, true},
		{"pending/guest", pending, nil, false}, {"pending/other", pending, &other, false}, {"pending/moderator", pending, &mod, true}, {"pending/author", pending, &author, true},
		{"rejected/guest", rejected, nil, false}, {"rejected/other", rejected, &other, false}, {"rejected/moderator", rejected, &mod, true}, {"rejected/author", rejected, &author, true},
		{"published/guest", published, nil, true}, {"published/other", published, &other, true}, {"published/moderator", published, &mod, true}, {"published/author", published, &author, true},
	}
	for _, c := range cases {
		_, err := e.lib.Get(ctx, c.viewer, c.id)
		if c.visible && err != nil || !c.visible && !errors.Is(err, library.ErrNotFound) {
			t.Errorf("%s: visible=%v, err = %v", c.name, c.visible, err)
		}
	}

	// Лента и избранное показывают только опубликованное, кто бы ни спрашивал.
	for _, viewer := range []*uuid.UUID{nil, &author, &other, &mod} {
		page, err := e.lib.List(ctx, viewer, library.ListFilter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, hidden := range []uuid.UUID{draft, pending, rejected} {
			if contains(ids(page.Items), hidden) {
				t.Errorf("the feed leaks a non-published material %v", hidden)
			}
		}
		if !contains(ids(page.Items), published) {
			t.Error("the feed lost the published material")
		}
	}

	// Комментарии, оценки и избранное работают только с опубликованным: у остальных материалов их «нет».
	for _, hidden := range []uuid.UUID{draft, pending, rejected} {
		if _, err := e.lib.ListComments(ctx, hidden, 10, ""); !errors.Is(err, library.ErrNotFound) {
			t.Errorf("comments of a non-published material must be a 404, err = %v", err)
		}
		if _, err := e.lib.AddComment(ctx, other, hidden, "hi"); !errors.Is(err, library.ErrNotFound) {
			t.Errorf("commenting a non-published material must be a 404, err = %v", err)
		}
		if _, err := e.lib.Rate(ctx, other, hidden, 5); !errors.Is(err, library.ErrNotFound) {
			t.Errorf("rating a non-published material must be a 404, err = %v", err)
		}
		if err := e.lib.AddBookmark(ctx, other, hidden); !errors.Is(err, library.ErrNotFound) {
			t.Errorf("bookmarking a non-published material must be a 404, err = %v", err)
		}
	}
}

// Личный API заметок остаётся личным: опубликованное чужое не видно через /notes.
func TestIntegration_PublishedMaterialsStayPrivateInThePersonalNotesAPI(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, other, mod := e.user(t, "author"), e.user(t, "other"), e.moderator(t, "mod")
	id := e.published(t, author, mod, "Public", "x")

	if _, err := e.notes.Get(ctx, other, id); !errors.Is(err, note.ErrNotFound) {
		t.Errorf("another user must not read a published note through the personal API, err = %v", err)
	}
	page, err := e.notes.List(ctx, other, note.ListFilter{})
	if err != nil || len(page.Items) != 0 {
		t.Errorf("another user's personal list must stay empty: %+v, %v", page.Items, err)
	}
	if _, err := e.notes.Update(ctx, other, id, note.Patch{Title: ptr("hacked")}); !errors.Is(err, note.ErrNotFound) {
		t.Errorf("another user must not edit a published note, err = %v", err)
	}
	if err := e.notes.Delete(ctx, other, id); !errors.Is(err, note.ErrNotFound) {
		t.Errorf("another user must not delete a published note, err = %v", err)
	}
	if m, err := e.lib.Get(ctx, nil, id); err != nil || m.Title != "Public" {
		t.Errorf("the material must be unchanged: %+v, %v", m.Title, err)
	}
}

func TestIntegration_Rejection_NeedsAReason_AuthorSeesIt_ResubmitGoesBackToReview(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, mod := e.user(t, "author"), e.moderator(t, "mod")
	id := e.pending(t, author, "Draft essay", "first version")

	if _, err := e.lib.Decide(ctx, mod, id, library.DecisionRejected, "   "); !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("a rejection without a reason must be refused, err = %v", err)
	}
	if n, _ := e.lib.Get(ctx, &mod, id); n.Status != "pending" {
		t.Errorf("a refused decision must change nothing, status = %s", n.Status)
	}
	if _, err := e.lib.Decide(ctx, mod, id, library.DecisionRejected, "Add sources"); err != nil {
		t.Fatal(err)
	}

	own, err := e.notes.Get(ctx, author, id)
	if err != nil || own.Status != "rejected" || own.RejectionReason == nil || *own.RejectionReason != "Add sources" {
		t.Fatalf("the author must see the rejection and its reason: %+v, %v", own.RejectionReason, err)
	}
	if m, err := e.lib.Get(ctx, &author, id); err != nil || m.RejectionReason == nil || *m.RejectionReason != "Add sources" {
		t.Errorf("the material page must carry the reason too: %v, %v", m.RejectionReason, err)
	}

	if _, err := e.notes.Update(ctx, author, id, note.Patch{Content: doc("second version with sources")}); err != nil {
		t.Fatal(err)
	}
	if n, _ := e.notes.Get(ctx, author, id); n.Status != "rejected" {
		t.Errorf("editing a rejected material must not publish or hide the status, got %s", n.Status)
	}
	if _, err := e.lib.Submit(ctx, author, id); err != nil {
		t.Fatalf("a corrected material must be submittable again: %v", err)
	}
	if n, _ := e.notes.Get(ctx, author, id); n.Status != "pending" || n.RejectionReason != nil {
		t.Errorf("after resubmitting: status = %s, reason = %v", n.Status, n.RejectionReason)
	}

	// История решений сохранена.
	var count int
	if err := e.tx.QueryRow(ctx, `SELECT count(*) FROM moderation_decisions WHERE note_id = $1 AND decision = 'rejected' AND reason = 'Add sources'`, id).Scan(&count); err != nil || count != 1 {
		t.Errorf("rejection decisions recorded = %d, %v", count, err)
	}
}

func TestIntegration_EditingAPublishedMaterial_SendsItBackToReview(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, mod := e.user(t, "author"), e.moderator(t, "mod")
	other := e.user(t, "reader")

	for name, patch := range map[string]note.Patch{
		"text":         {Content: doc("changed")},
		"title":        {Title: ptr("New title")},
		"kind":         {Kind: ptr(note.KindCheatSheet)},
		"summary":      {SetDescription: true, Description: ptr("new summary")},
		"semester":     {SetSemester: true, Semester: ptr(int16(3))},
		"library subj": {SetLibrarySubject: true, LibrarySubjectID: &e.subject.ID},
	} {
		id := e.published(t, author, mod, "Orig "+name, "orig")
		if err := e.lib.AddBookmark(ctx, other, id); err != nil {
			t.Fatal(err)
		}
		updated, err := e.notes.Update(ctx, author, id, patch)
		if err != nil {
			t.Fatal(name, err)
		}
		if updated.Status != "pending" || updated.PublishedAt != nil {
			t.Errorf("%s: after the edit status = %s, published_at = %v", name, updated.Status, updated.PublishedAt)
		}
		if _, err := e.lib.Get(ctx, nil, id); !errors.Is(err, library.ErrNotFound) {
			t.Errorf("%s: the edited material must leave the library, err = %v", name, err)
		}
		page, _ := e.lib.List(ctx, nil, library.ListFilter{})
		if contains(ids(page.Items), id) {
			t.Errorf("%s: the edited material is still in the feed", name)
		}
		fav, _ := e.lib.Bookmarks(ctx, other, 50, "")
		if contains(ids(fav.Items), id) {
			t.Errorf("%s: the edited material is still in somebody's favourites", name)
		}
		// Закрепление и личный предмет публикацию не трогают.
	}

	id := e.published(t, author, mod, "Stable", "orig")
	pin := true
	updated, err := e.notes.Update(ctx, author, id, note.Patch{IsPinned: &pin})
	if err != nil || updated.Status != "published" {
		t.Errorf("pinning must not unpublish: status = %s, %v", updated.Status, err)
	}
}

func TestIntegration_Decide_Rules(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, mod, mod2 := e.user(t, "author"), e.moderator(t, "mod"), e.moderator(t, "mod2")

	// Свой материал модератор рассматривать не может.
	own := e.pending(t, mod, "Mod's own", "x")
	if _, err := e.lib.Decide(ctx, mod, own, library.DecisionApproved, ""); !errors.Is(err, library.ErrForbidden) {
		t.Errorf("a moderator must not decide on their own material, err = %v", err)
	}
	// Но это может сделать другой модератор.
	if _, err := e.lib.Decide(ctx, mod2, own, library.DecisionApproved, ""); err != nil {
		t.Errorf("another moderator may: %v", err)
	}

	id := e.pending(t, author, "Once", "x")
	if _, err := e.lib.Decide(ctx, mod, id, library.DecisionApproved, ""); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{library.DecisionApproved, library.DecisionRejected} {
		if _, err := e.lib.Decide(ctx, mod2, id, d, "late"); !errors.Is(err, library.ErrInvalidTransition) {
			t.Errorf("a second %s decision must be an invalid transition, err = %v", d, err)
		}
	}
	draft := e.draft(t, author, "Not submitted", "x")
	if _, err := e.lib.Decide(ctx, mod, draft, library.DecisionApproved, ""); !errors.Is(err, library.ErrInvalidTransition) {
		t.Errorf("a draft cannot be decided, err = %v", err)
	}
	if _, err := e.lib.Decide(ctx, mod, uuid.New(), library.DecisionApproved, ""); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("unknown material: err = %v", err)
	}
	if _, err := e.lib.Decide(ctx, mod, id, "maybe", ""); !errors.Is(err, library.ErrInvalid) {
		t.Errorf("unknown decision: err = %v", err)
	}

	var decisions int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM moderation_decisions WHERE note_id = $1`, id).Scan(&decisions)
	if decisions != 1 {
		t.Errorf("exactly one decision must be on record for the material, got %d", decisions)
	}
}

func TestIntegration_Submit_Rules(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, other, mod := e.user(t, "author"), e.user(t, "other"), e.moderator(t, "mod")

	noSubject, err := e.notes.Create(ctx, author, note.Input{Title: "No subject", Content: doc("text")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.Submit(ctx, author, noSubject.ID); !errors.Is(err, library.ErrInvalid) || !strings.Contains(err.Error(), "library subject") {
		t.Errorf("submitting without a library subject: err = %v", err)
	}

	empty, err := e.notes.Create(ctx, author, note.Input{Title: "Empty", Content: []byte(`{"type":"doc","content":[]}`), LibrarySubjectID: &e.subject.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.Submit(ctx, author, empty.ID); !errors.Is(err, library.ErrInvalid) {
		t.Errorf("submitting a material with neither text nor files: err = %v", err)
	}
	// Файл вместо текста допустим: шпаргалка может быть одним PDF.
	if _, err := e.q.CreateAttachment(ctx, db.CreateAttachmentParams{
		UserID: author, NoteID: uuid.NullUUID{UUID: empty.ID, Valid: true}, ObjectKey: "k-" + uuid.NewString(),
		FileName: "sheet.pdf", MimeType: "application/pdf", SizeBytes: 100,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.Submit(ctx, author, empty.ID); err != nil {
		t.Errorf("a material with a file must be submittable: %v", err)
	}

	id := e.draft(t, author, "Mine", "x")
	if _, err := e.lib.Submit(ctx, other, id); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("submitting someone else's note: err = %v", err)
	}
	if _, err := e.lib.Submit(ctx, author, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.Submit(ctx, author, id); !errors.Is(err, library.ErrInvalidTransition) {
		t.Errorf("submitting a pending material again: err = %v", err)
	}
	if _, err := e.lib.Decide(ctx, mod, id, library.DecisionApproved, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.Submit(ctx, author, id); !errors.Is(err, library.ErrInvalidTransition) {
		t.Errorf("submitting a published material: err = %v", err)
	}
	if _, err := e.lib.Submit(ctx, author, uuid.New()); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("unknown note: err = %v", err)
	}
}

func TestIntegration_Views_CountedForReadersNotForTheAuthor(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, reader, mod := e.user(t, "author"), e.user(t, "reader"), e.moderator(t, "mod")
	id := e.published(t, author, mod, "Popular", "x")

	for i := 0; i < 3; i++ {
		_, _ = e.lib.Get(ctx, nil, id)
	}
	_, _ = e.lib.Get(ctx, &reader, id)
	for i := 0; i < 5; i++ {
		_, _ = e.lib.Get(ctx, &author, id) // автор свои просмотры не накручивает
	}
	m, _ := e.lib.Get(ctx, &mod, id)

	if m.Views != 5 { // три гостя, читатель и сам этот просмотр модератором
		t.Errorf("views = %d, want 5 (3 guests + reader + moderator, never the author)", m.Views)
	}
	var stored int64
	_ = e.tx.QueryRow(ctx, `SELECT views FROM notes WHERE id = $1`, id).Scan(&stored)
	if stored != 5 {
		t.Errorf("stored views = %d", stored)
	}

	// Непубликуемое не копит просмотры.
	draft := e.draft(t, author, "Private", "x")
	_, _ = e.lib.Get(ctx, &author, draft)
	_ = e.tx.QueryRow(ctx, `SELECT views FROM notes WHERE id = $1`, draft).Scan(&stored)
	if stored != 0 {
		t.Errorf("a draft must not collect views, got %d", stored)
	}
}

func TestIntegration_Feed_FiltersSortsAndPaginates(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, mod := e.user(t, "author"), e.moderator(t, "mod")
	r1, r2, r3 := e.user(t, "r1"), e.user(t, "r2"), e.user(t, "r3")

	var all []uuid.UUID
	for i := 0; i < 5; i++ {
		id := e.published(t, author, mod, fmt.Sprintf("Material %d", i), "x")
		// Разные моменты публикации: now() внутри транзакции у всех одинаков.
		if _, err := e.tx.Exec(ctx, `UPDATE notes SET published_at = now() - make_interval(mins => $2) WHERE id = $1`, id, 10-i); err != nil {
			t.Fatal(err)
		}
		all = append(all, id)
	}
	// Оценки: материал 3 лучший, затем 1 и 0 (у них равные средние, но у 1 оценок больше).
	rate := func(id uuid.UUID, uid uuid.UUID, v int) {
		if _, err := e.lib.Rate(ctx, uid, id, v); err != nil {
			t.Fatal(err)
		}
	}
	rate(all[3], r1, 5)
	rate(all[3], r2, 5)
	rate(all[1], r1, 4)
	rate(all[1], r2, 4)
	rate(all[0], r1, 4)
	// Просмотры: материал 2 самый просматриваемый.
	for i := 0; i < 3; i++ {
		_, _ = e.lib.Get(ctx, &r3, all[2])
	}
	_, _ = e.lib.Get(ctx, &r3, all[4])

	mine := map[uuid.UUID]bool{}
	for _, id := range all {
		mine[id] = true
	}
	collect := func(f library.ListFilter) []uuid.UUID {
		t.Helper()
		var out []uuid.UUID
		for pages := 0; pages < 20; pages++ {
			page, err := e.lib.List(ctx, nil, f)
			if err != nil {
				t.Fatal(err)
			}
			for _, it := range page.Items {
				if mine[it.ID] {
					out = append(out, it.ID)
				}
			}
			if page.NextCursor == "" {
				return out
			}
			f.Cursor = page.NextCursor
		}
		t.Fatal("pagination does not end")
		return nil
	}

	f := library.ListFilter{SubjectID: &e.subject.ID, Limit: 2}
	newest := collect(f)
	if len(newest) != 5 {
		t.Fatalf("paging by 2 returned %d of 5 materials (no row may be skipped or repeated)", len(newest))
	}
	for i, want := range []uuid.UUID{all[4], all[3], all[2], all[1], all[0]} { // позже опубликован - выше
		if newest[i] != want {
			t.Errorf("sort=new position %d: got %v want %v", i, newest[i], want)
		}
	}

	f.Sort = library.SortRated
	rated := collect(f)
	if len(rated) != 5 || rated[0] != all[3] || rated[1] != all[1] || rated[2] != all[0] {
		t.Errorf("sort=rating: %v, want %v first, then %v, %v", rated, all[3], all[1], all[0])
	}

	f.Sort = library.SortViews
	popular := collect(f)
	if len(popular) != 5 || popular[0] != all[2] || popular[1] != all[4] {
		t.Errorf("sort=views: %v, want %v then %v first", popular, all[2], all[4])
	}

	// Фильтры.
	other, err := e.q.CreateLibrarySubject(ctx, db.CreateLibrarySubjectParams{Title: "Other " + uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	page, err := e.lib.List(ctx, nil, library.ListFilter{SubjectID: &other.ID})
	if err != nil || len(page.Items) != 0 {
		t.Errorf("a subject without materials must give an empty page: %v, %v", ids(page.Items), err)
	}
	if _, err := e.notes.Update(ctx, author, all[0], note.Patch{Kind: ptr(note.KindCheatSheet), SetSemester: true, Semester: ptr(int16(4))}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.Submit(ctx, author, all[0]); !errors.Is(err, library.ErrInvalidTransition) {
		t.Fatalf("a re-pending material is already pending: %v", err)
	}
	if _, err := e.lib.Decide(ctx, mod, all[0], library.DecisionApproved, ""); err != nil {
		t.Fatal(err)
	}
	sheet := collect(library.ListFilter{SubjectID: &e.subject.ID, Kind: ptr("cheat_sheet"), Semester: ptr(int16(4))})
	if len(sheet) != 1 || sheet[0] != all[0] {
		t.Errorf("kind+semester filter: %v, want only %v", sheet, all[0])
	}

	for _, bad := range []string{"%%%", "e30", ""} {
		if bad == "" {
			continue
		}
		if _, err := e.lib.List(ctx, nil, library.ListFilter{Cursor: bad}); !errors.Is(err, library.ErrInvalidCursor) {
			t.Errorf("cursor %q: err = %v, want ErrInvalidCursor", bad, err)
		}
	}
}

func TestIntegration_Search_TrustsTheDatabaseNotTheIndex(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, other, mod := e.user(t, "author"), e.user(t, "other"), e.moderator(t, "mod")
	live := e.published(t, author, mod, "Derivatives", "rate of change")
	draft := e.draft(t, author, "Secret draft", "rate of change")
	pending := e.pending(t, other, "Pending", "rate of change")
	demoted := e.published(t, author, mod, "Was public", "rate of change")
	if _, err := e.notes.Update(ctx, author, demoted, note.Patch{Title: ptr("Edited")}); err != nil {
		t.Fatal(err)
	}
	deleted := e.published(t, author, mod, "Deleted", "rate of change")
	if err := e.notes.Delete(ctx, author, deleted); err != nil {
		t.Fatal(err)
	}

	// Отстающий индекс считает все эти материалы опубликованными и ещё подсовывает посторонний id.
	e.engine.hits = []search.Hit{
		{ID: draft, Snippet: "x"}, {ID: pending, Snippet: "x"}, {ID: demoted, Snippet: "x"}, {ID: deleted, Snippet: "x"},
		{ID: uuid.New(), Snippet: "ghost"},
		{ID: live, Snippet: "rate of \x01change\x02 <script>alert(1)</script>"},
	}

	page, err := e.lib.List(ctx, nil, library.ListFilter{Query: "change"})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page.Items); len(got) != 1 || got[0] != live {
		t.Fatalf("search returned %v, want only the live published material %v", got, live)
	}
	if page.NextCursor != "" {
		t.Error("search results come as one page")
	}
	snip := page.Items[0].Snippet
	if !strings.Contains(snip, "<mark>change</mark>") || strings.Contains(snip, "<script>") {
		t.Errorf("snippet must highlight and escape: %q", snip)
	}
}

func TestIntegration_Search_WithoutAnEngine_IsUnavailable(t *testing.T) {
	e := newEnv(t)
	svc := library.NewService(e.q) // без движка
	if _, err := svc.List(context.Background(), nil, library.ListFilter{Query: "x"}); !errors.Is(err, search.ErrUnavailable) {
		t.Errorf("err = %v, want search.ErrUnavailable (the rest of the API must keep working)", err)
	}
	if _, err := svc.List(context.Background(), nil, library.ListFilter{}); err != nil {
		t.Errorf("browsing without a search engine must work: %v", err)
	}
}

func TestIntegration_Bookmarks(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, reader, mod := e.user(t, "author"), e.user(t, "reader"), e.moderator(t, "mod")
	a := e.published(t, author, mod, "A", "x")
	b := e.published(t, author, mod, "B", "x")

	for i := 0; i < 2; i++ { // повтор ничего не меняет
		if err := e.lib.AddBookmark(ctx, reader, a); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.lib.AddBookmark(ctx, reader, b); err != nil {
		t.Fatal(err)
	}
	if _, err := e.tx.Exec(ctx, `UPDATE note_bookmarks SET created_at = now() - interval '1 hour' WHERE note_id = $1`, a); err != nil {
		t.Fatal(err)
	}

	page, err := e.lib.Bookmarks(ctx, reader, 1, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != b || page.NextCursor == "" || !page.Items[0].Bookmarked {
		t.Fatalf("first page: %v, %q, %v (newest favourite first)", ids(page.Items), page.NextCursor, err)
	}
	next, err := e.lib.Bookmarks(ctx, reader, 1, page.NextCursor)
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != a || next.NextCursor != "" {
		t.Fatalf("second page: %v, %q, %v", ids(next.Items), next.NextCursor, err)
	}
	if m, _ := e.lib.Get(ctx, &reader, a); m.BookmarksCount != 1 || !m.Bookmarked {
		t.Errorf("bookmarks count = %d, bookmarked = %v", m.BookmarksCount, m.Bookmarked)
	}
	if others, _ := e.lib.Bookmarks(ctx, author, 10, ""); len(others.Items) != 0 {
		t.Errorf("favourites are personal: %v", ids(others.Items))
	}

	if err := e.lib.RemoveBookmark(ctx, reader, a); err != nil {
		t.Fatal(err)
	}
	if err := e.lib.RemoveBookmark(ctx, reader, a); err != nil {
		t.Errorf("removing what is not there is not an error: %v", err)
	}
	if left, _ := e.lib.Bookmarks(ctx, reader, 10, ""); len(left.Items) != 1 || left.Items[0].ID != b {
		t.Errorf("after removal: %v", ids(left.Items))
	}
}

func TestIntegration_Comments_OrderPaginationAndDeletion(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, bob, eve, mod := e.user(t, "author"), e.user(t, "bob"), e.user(t, "eve"), e.moderator(t, "mod")
	id := e.published(t, author, mod, "Discussed", "x")

	var cs []library.Comment
	for i := 0; i < 3; i++ {
		c, err := e.lib.AddComment(ctx, bob, id, fmt.Sprintf("comment %d", i))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.tx.Exec(ctx, `UPDATE comments SET created_at = now() - make_interval(mins => $2) WHERE id = $1`, c.ID, 10-i); err != nil {
			t.Fatal(err)
		}
		cs = append(cs, c)
	}
	p1, err := e.lib.ListComments(ctx, id, 2, "")
	if err != nil || len(p1.Items) != 2 || p1.Items[0].ID != cs[0].ID || p1.Items[1].ID != cs[1].ID || p1.NextCursor == "" {
		t.Fatalf("page 1: %+v, %v (oldest first)", p1, err)
	}
	p2, err := e.lib.ListComments(ctx, id, 2, p1.NextCursor)
	if err != nil || len(p2.Items) != 1 || p2.Items[0].ID != cs[2].ID || p2.NextCursor != "" {
		t.Fatalf("page 2: %+v, %v", p2, err)
	}

	// Чужой комментарий студент удалить не может, для него он «не существует».
	if err := e.lib.DeleteComment(ctx, eve, cs[0].ID); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("a stranger deleting a comment: err = %v", err)
	}
	if err := e.lib.DeleteComment(ctx, bob, cs[0].ID); err != nil {
		t.Errorf("the author deletes the own comment: %v", err)
	}
	if err := e.lib.DeleteComment(ctx, mod, cs[1].ID); err != nil {
		t.Errorf("a moderator deletes any comment: %v", err)
	}
	if err := e.lib.DeleteComment(ctx, bob, cs[0].ID); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("deleting twice: err = %v", err)
	}
	left, _ := e.lib.ListComments(ctx, id, 10, "")
	if len(left.Items) != 1 || left.Items[0].ID != cs[2].ID {
		t.Errorf("remaining comments: %+v", left.Items)
	}
	if m, _ := e.lib.Get(ctx, nil, id); m.CommentsCount != 1 {
		t.Errorf("comments_count = %d, deleted comments must not count", m.CommentsCount)
	}
	long := strings.Repeat("я", 2001)
	if _, err := e.lib.AddComment(ctx, bob, id, long); !errors.Is(err, library.ErrInvalid) {
		t.Errorf("2001 characters: err = %v", err)
	}
	if _, err := e.lib.AddComment(ctx, bob, id, strings.Repeat("я", 2000)); err != nil {
		t.Errorf("2000 multibyte characters must be accepted: %v", err)
	}
}

func TestIntegration_Queue_OnlyForModerators_OldestFirst_Paginated(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, mod := e.user(t, "author"), e.moderator(t, "mod")

	if _, err := e.lib.Queue(ctx, author, 10, ""); !errors.Is(err, library.ErrForbidden) {
		t.Fatalf("a student must not see the queue, err = %v", err)
	}
	var queued []uuid.UUID
	for i := 0; i < 3; i++ {
		id := e.pending(t, author, fmt.Sprintf("Queued %d", i), "x")
		if _, err := e.tx.Exec(ctx, `UPDATE notes SET updated_at = now() - make_interval(mins => $2) WHERE id = $1`, id, 10-i); err != nil {
			t.Fatal(err)
		}
		queued = append(queued, id)
	}
	e.draft(t, author, "Not in queue", "x")
	e.published(t, author, mod, "Already published", "x")

	mine := map[uuid.UUID]bool{queued[0]: true, queued[1]: true, queued[2]: true}
	var order []uuid.UUID
	cursor := ""
	for i := 0; i < 20; i++ {
		page, err := e.lib.Queue(ctx, mod, 2, cursor)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range page.Items {
			if mine[it.ID] {
				order = append(order, it.ID)
			}
			if it.Author.DisplayName == "" || it.Subject == nil {
				t.Errorf("queue item must carry the author and the subject: %+v", it)
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(order) != 3 || order[0] != queued[0] || order[1] != queued[1] || order[2] != queued[2] {
		t.Errorf("queue order = %v, want %v (oldest first)", order, queued)
	}
	if _, err := e.lib.Queue(ctx, mod, 10, "!!!"); !errors.Is(err, library.ErrInvalidCursor) {
		t.Errorf("bad cursor: err = %v", err)
	}
}

func TestIntegration_LibrarySubjects_ModeratorOnly_UniqueAndProtectedWhileUsed(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	student, mod, author := e.user(t, "student"), e.moderator(t, "mod"), e.user(t, "author")
	title := "Physics " + uuid.NewString()

	if _, err := e.lib.CreateSubject(ctx, student, title, nil); !errors.Is(err, library.ErrForbidden) {
		t.Fatalf("a student must not create subjects, err = %v", err)
	}
	s, err := e.lib.CreateSubject(ctx, mod, "  "+title+"  ", ptr("#4f46e5"))
	if err != nil || s.Title != title {
		t.Fatalf("create: %+v, %v", s, err)
	}
	e.attempt(t, func(svc *library.Service) {
		if _, err := svc.CreateSubject(ctx, mod, strings.ToUpper(title), nil); !errors.Is(err, library.ErrSubjectExists) {
			t.Errorf("a duplicate title (any case) must be refused, err = %v", err)
		}
	})
	for name, c := range map[string]*string{"no hash": ptr("4f46e5"), "short": ptr("#fff"), "word": ptr("red")} {
		if _, err := e.lib.CreateSubject(ctx, mod, "Color "+name+uuid.NewString(), c); !errors.Is(err, library.ErrInvalid) {
			t.Errorf("color %q must be refused, err = %v", *c, err)
		}
	}
	if _, err := e.lib.CreateSubject(ctx, mod, "   ", nil); !errors.Is(err, library.ErrInvalid) {
		t.Errorf("an empty title must be refused, err = %v", err)
	}

	renamed := title + " II"
	u, err := e.lib.UpdateSubject(ctx, mod, s.ID, library.SubjectPatch{Title: &renamed, SetColor: true})
	if err != nil || u.Title != renamed || u.Color != nil {
		t.Fatalf("update: %+v, %v (color must be cleared)", u, err)
	}
	if _, err := e.lib.UpdateSubject(ctx, student, s.ID, library.SubjectPatch{Title: &renamed}); !errors.Is(err, library.ErrForbidden) {
		t.Errorf("a student updating: err = %v", err)
	}
	if _, err := e.lib.UpdateSubject(ctx, mod, uuid.New(), library.SubjectPatch{Title: &renamed}); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("unknown subject: err = %v", err)
	}

	// Пока к предмету привязан материал, удалить его нельзя; счётчик показывает только опубликованные.
	n, err := e.notes.Create(ctx, author, note.Input{Title: "On physics", Content: doc("x"), LibrarySubjectID: &s.ID})
	if err != nil {
		t.Fatal(err)
	}
	e.attempt(t, func(svc *library.Service) {
		if err := svc.DeleteSubject(ctx, mod, s.ID); !errors.Is(err, library.ErrSubjectInUse) {
			t.Errorf("deleting a used subject: err = %v", err)
		}
	})
	if _, err := e.lib.Submit(ctx, author, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.Decide(ctx, mod, n.ID, library.DecisionApproved, ""); err != nil {
		t.Fatal(err)
	}
	subjects, _ := e.lib.ListSubjects(ctx)
	for _, sub := range subjects {
		if sub.ID == s.ID && sub.MaterialsCount != 1 {
			t.Errorf("materials_count = %d, want 1", sub.MaterialsCount)
		}
	}
	if _, err := e.notes.Update(ctx, author, n.ID, note.Patch{SetLibrarySubject: true}); err != nil { // отвязали
		t.Fatal(err)
	}
	if err := e.lib.DeleteSubject(ctx, mod, s.ID); err != nil {
		t.Errorf("deleting an unused subject: %v", err)
	}
	if err := e.lib.DeleteSubject(ctx, mod, s.ID); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("deleting twice: err = %v", err)
	}
	if err := e.lib.DeleteSubject(ctx, student, uuid.New()); !errors.Is(err, library.ErrForbidden) {
		t.Errorf("a student deleting: err = %v", err)
	}
}

func TestIntegration_NoteCreation_ValidatesTheLibrarySubject(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author := e.user(t, "author")
	missing := uuid.New()

	if _, err := e.notes.Create(ctx, author, note.Input{Title: "x", Content: doc("x"), LibrarySubjectID: &missing}); !errors.Is(err, note.ErrInvalidLibrarySubject) {
		t.Errorf("creating with an unknown library subject: err = %v", err)
	}
	n, err := e.notes.Create(ctx, author, note.Input{Title: "x", Content: doc("x")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.notes.Update(ctx, author, n.ID, note.Patch{SetLibrarySubject: true, LibrarySubjectID: &missing}); !errors.Is(err, note.ErrInvalidLibrarySubject) {
		t.Errorf("updating to an unknown library subject: err = %v", err)
	}
	if n.Status != "draft" || n.Kind != "note" {
		t.Errorf("a new note is a draft of kind note, got %s/%s", n.Status, n.Kind)
	}
}

func TestIntegration_Attachments_FollowMaterialVisibility(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, other, mod := e.user(t, "author"), e.user(t, "other"), e.moderator(t, "mod")
	id := e.draft(t, author, "With a file", "x")
	att, err := e.q.CreateAttachment(ctx, db.CreateAttachmentParams{
		UserID: author, NoteID: uuid.NullUUID{UUID: id, Valid: true}, ObjectKey: "k-" + uuid.NewString(),
		FileName: "a.pdf", MimeType: "application/pdf", SizeBytes: 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := e.lib.Attachment(ctx, nil, id, att.ID); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("a draft's file for a guest: err = %v", err)
	}
	if _, err := e.lib.Attachment(ctx, &author, id, att.ID); err != nil {
		t.Errorf("the author reaches the own file: %v", err)
	}
	if _, err := e.lib.Submit(ctx, author, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.Attachment(ctx, &other, id, att.ID); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("a pending material's file for another student: err = %v", err)
	}
	if _, err := e.lib.Attachment(ctx, &mod, id, att.ID); err != nil {
		t.Errorf("the moderator reviews the file: %v", err)
	}
	if _, err := e.lib.Decide(ctx, mod, id, library.DecisionApproved, ""); err != nil {
		t.Fatal(err)
	}
	got, err := e.lib.Attachment(ctx, nil, id, att.ID)
	if err != nil || got.ID != att.ID {
		t.Errorf("a published material's file for a guest: %v, %v", got.ID, err)
	}
	// Файл другого материала по id этого не отдаётся.
	otherNote := e.published(t, author, mod, "Another", "x")
	if _, err := e.lib.Attachment(ctx, nil, otherNote, att.ID); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("a file requested through a foreign material: err = %v", err)
	}
	m, err := e.lib.Get(ctx, nil, id)
	if err != nil || len(m.Attachments) != 1 {
		t.Errorf("the page must list the file: %d, %v", len(m.Attachments), err)
	}
}

func TestIntegration_Roles_OnlyTheServerAssignsThem(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	svc := auth.NewService(e.q, []byte("0123456789abcdef0123456789abcdef"))
	res, err := svc.Register(ctx, "role-"+uuid.NewString()+"@x.io", "password123", "R", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.User.Role != auth.RoleStudent {
		t.Fatalf("a new account is a student, got %q", res.User.Role)
	}
	if _, err := e.lib.Queue(ctx, res.User.ID, 10, ""); !errors.Is(err, library.ErrForbidden) {
		t.Errorf("a fresh account must not moderate, err = %v", err)
	}
	if err := svc.SetRole(ctx, res.User.Email, auth.RoleModerator); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.Queue(ctx, res.User.ID, 10, ""); err != nil {
		t.Errorf("after the promotion the account moderates: %v", err)
	}
	// Роль проверяется по базе при каждом запросе: понижение действует сразу, без перевыпуска токенов.
	if err := svc.SetRole(ctx, res.User.Email, auth.RoleStudent); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.Queue(ctx, res.User.ID, 10, ""); !errors.Is(err, library.ErrForbidden) {
		t.Errorf("a demoted moderator keeps access: err = %v", err)
	}
}

func TestIntegration_Timestamps(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, mod := e.user(t, "author"), e.moderator(t, "mod")
	id := e.published(t, author, mod, "Dated", "x")
	m, err := e.lib.Get(ctx, nil, id)
	if err != nil || m.PublishedAt == nil || time.Since(*m.PublishedAt) > time.Minute {
		t.Errorf("published_at = %v, %v", m.PublishedAt, err)
	}
}
