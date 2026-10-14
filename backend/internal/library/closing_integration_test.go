package library_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/library"
	"github.com/whoaskedssselery/studyvault/backend/internal/note"
	"github.com/whoaskedssselery/studyvault/backend/internal/search"
)

// tag привязывает к заметке тег автора (создаёт его при необходимости).
func (e *env) tag(t *testing.T, author, noteID uuid.UUID, name string) {
	t.Helper()
	ctx := context.Background()
	var tagID uuid.UUID
	err := e.tx.QueryRow(ctx, `INSERT INTO tags (user_id, name) VALUES ($1, $2)
		ON CONFLICT (user_id, name) DO UPDATE SET name = EXCLUDED.name RETURNING id`, author, name).Scan(&tagID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.tx.Exec(ctx, `INSERT INTO note_tags (note_id, tag_id) VALUES ($1, $2)`, noteID, tagID); err != nil {
		t.Fatal(err)
	}
}

func (e *env) feed(t *testing.T, f library.ListFilter) []uuid.UUID {
	t.Helper()
	var out []uuid.UUID
	for i := 0; i < 20; i++ {
		page, err := e.lib.List(context.Background(), nil, f)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, ids(page.Items)...)
		if page.NextCursor == "" {
			return out
		}
		f.Cursor = page.NextCursor
	}
	t.Fatal("pagination does not end")
	return nil
}

func TestIntegration_FeedCardsCarryTags_AndTheFeedFiltersByTagAndAuthor(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ann, bob, mod := e.user(t, "ann"), e.user(t, "bob"), e.moderator(t, "mod")

	a1 := e.draft(t, ann, "Ann: limits", "x")
	e.tag(t, ann, a1, "exam")
	e.tag(t, ann, a1, "lecture")
	a2 := e.draft(t, ann, "Ann: untagged", "x")
	b1 := e.draft(t, bob, "Bob: limits", "x")
	e.tag(t, bob, b1, "exam") // у другого автора тег с тем же названием
	for _, id := range []uuid.UUID{a1, a2, b1} {
		if _, err := e.lib.Submit(ctx, map[uuid.UUID]uuid.UUID{a1: ann, a2: ann, b1: bob}[id], id); err != nil {
			t.Fatal(err)
		}
		if _, err := e.lib.Decide(ctx, mod, id, library.DecisionApproved, ""); err != nil {
			t.Fatal(err)
		}
	}
	mine := func(list []uuid.UUID) []uuid.UUID { // лента общая с чужими данными в базе: оставляем свои три
		var out []uuid.UUID
		for _, id := range list {
			if id == a1 || id == a2 || id == b1 {
				out = append(out, id)
			}
		}
		return out
	}

	page, _ := e.lib.List(ctx, nil, library.ListFilter{AuthorID: &ann})
	for _, it := range page.Items {
		switch it.ID {
		case a1:
			if len(it.Tags) != 2 || it.Tags[0] != "exam" || it.Tags[1] != "lecture" {
				t.Errorf("tags = %v, want [exam lecture] sorted by name", it.Tags)
			}
		case a2:
			if it.Tags == nil || len(it.Tags) != 0 {
				t.Errorf("an untagged material must carry an empty, non-nil list: %#v", it.Tags)
			}
		}
	}
	if got := mine(e.feed(t, library.ListFilter{AuthorID: &ann})); len(got) != 2 || contains(got, b1) {
		t.Errorf("author filter: %v, want only Ann's two materials", got)
	}
	if got := mine(e.feed(t, library.ListFilter{Tag: ptr("exam")})); len(got) != 2 || !contains(got, a1) || !contains(got, b1) {
		t.Errorf("tag filter: %v, want Ann's tagged and Bob's materials", got)
	}
	if got := mine(e.feed(t, library.ListFilter{Tag: ptr("exam"), AuthorID: &bob})); len(got) != 1 || got[0] != b1 {
		t.Errorf("tag and author together: %v", got)
	}
	if got := mine(e.feed(t, library.ListFilter{Tag: ptr("lecture"), Sort: library.SortRated})); len(got) != 1 || got[0] != a1 {
		t.Errorf("tag filter with another sort: %v", got)
	}
	if got := mine(e.feed(t, library.ListFilter{Tag: ptr("Exam")})); len(got) != 0 {
		t.Errorf("a tag name is matched exactly: %v", got)
	}
	// Закладки тоже отдают теги.
	if err := e.lib.AddBookmark(ctx, bob, a1); err != nil {
		t.Fatal(err)
	}
	fav, _ := e.lib.Bookmarks(ctx, bob, 10, "")
	if len(fav.Items) != 1 || len(fav.Items[0].Tags) != 2 {
		t.Errorf("favourites must carry tags: %+v", fav.Items)
	}
	// Страница материала и карточка согласованы.
	if m, _ := e.lib.Get(ctx, nil, a1); len(m.Tags) != 2 {
		t.Errorf("material page tags = %v", m.Tags)
	}
}

func TestIntegration_Search_RespectsTheAuthorAndTagFilters(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ann, bob, mod := e.user(t, "ann"), e.user(t, "bob"), e.moderator(t, "mod")
	a := e.published(t, ann, mod, "Ann on joins", "joins")
	b := e.published(t, bob, mod, "Bob on joins", "joins")
	e.tag(t, ann, a, "sql")
	e.engine.hits = []search.Hit{{ID: a, Snippet: "x"}, {ID: b, Snippet: "x"}}

	page, err := e.lib.List(ctx, nil, library.ListFilter{Query: "joins", AuthorID: &bob})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != b {
		t.Errorf("author filter in search: %v, %v", ids(page.Items), err)
	}
	page, err = e.lib.List(ctx, nil, library.ListFilter{Query: "joins", Tag: ptr("sql")})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != a {
		t.Errorf("tag filter in search: %v, %v", ids(page.Items), err)
	}
}

func TestIntegration_AuthorRankingAndProfile(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ann, bob, cat, dan := e.user(t, "ann"), e.user(t, "bob"), e.user(t, "cat"), e.user(t, "dan")
	mod := e.moderator(t, "mod")
	r1, r2, r3 := e.user(t, "r1"), e.user(t, "r2"), e.user(t, "r3")

	a1, a2 := e.published(t, ann, mod, "A1", "x"), e.published(t, ann, mod, "A2", "x")
	b1 := e.published(t, bob, mod, "B1", "x")
	c1 := e.published(t, cat, mod, "C1", "x")
	_ = e.draft(t, dan, "Dan has nothing published", "x")
	rate := func(id, who uuid.UUID, v int) {
		if _, err := e.lib.Rate(ctx, who, id, v); err != nil {
			t.Fatal(err)
		}
	}
	// Ann: оценки 5, 5 и 4 по двум материалам, среднее 14/3. Bob: 5 и 5, среднее 5. Cat: нет оценок.
	rate(a1, r1, 5)
	rate(a1, r2, 5)
	rate(a2, r3, 4)
	rate(b1, r1, 5)
	rate(b1, r2, 5)
	_, _ = e.lib.Get(ctx, &r1, a1)
	_, _ = e.lib.Get(ctx, &r1, a1)
	_, _ = e.lib.Get(ctx, &r2, c1)

	ranking, err := e.lib.Ranking(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[uuid.UUID]library.RankedAuthor{}
	for _, ra := range ranking {
		pos[ra.ID] = ra
	}
	if _, ok := pos[dan]; ok {
		t.Error("an author without published materials must not be ranked")
	}
	if !(pos[bob].Rank < pos[ann].Rank && pos[ann].Rank < pos[cat].Rank) {
		t.Errorf("ranks: bob %d, ann %d, cat %d, want bob < ann < cat (5.0 > 4.67 > no ratings)", pos[bob].Rank, pos[ann].Rank, pos[cat].Rank)
	}
	st := pos[ann]
	if st.MaterialsCount != 2 || st.RatingsCount != 3 || st.RatingAverage == nil || *st.RatingAverage < 4.66 || *st.RatingAverage > 4.67 || st.Views != 2 {
		t.Errorf("ann = %+v avg %v, want 2 materials, 3 ratings, 14/3, 2 views", st, st.RatingAverage)
	}
	if pos[cat].RatingAverage != nil || pos[cat].RatingsCount != 0 {
		t.Errorf("cat has no ratings, so no average: %+v", pos[cat])
	}
	for i, ra := range ranking {
		if ra.Rank != i+1 {
			t.Errorf("position %d has rank %d", i+1, ra.Rank)
		}
	}
	// Ограничение длины списка.
	if top, _ := e.lib.Ranking(ctx, 1); len(top) != 1 {
		t.Errorf("limit 1 gave %d rows", len(top))
	}
	if def, _ := e.lib.Ranking(ctx, 0); len(def) > 20 {
		t.Errorf("the default limit is 20, got %d", len(def))
	}

	// Профиль автора.
	profile, err := e.lib.AuthorProfile(ctx, ann)
	if err != nil || profile.MaterialsCount != 2 || profile.RatingsCount != 3 || profile.DisplayName != "ann" {
		t.Errorf("profile = %+v, %v", profile, err)
	}
	if _, err := e.lib.AuthorProfile(ctx, dan); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("a user without published materials has no profile: %v", err)
	}
	if _, err := e.lib.AuthorProfile(ctx, uuid.New()); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("an unknown id looks the same: %v", err)
	}
}

func TestIntegration_AuthorRanking_DropsAMaterialThatIsTakenDown(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	ann, mod, rater := e.user(t, "ann"), e.moderator(t, "mod"), e.user(t, "rater")
	id := e.published(t, ann, mod, "Only one", "x")
	if _, err := e.lib.Rate(ctx, rater, id, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.AuthorProfile(ctx, ann); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.Unpublish(ctx, mod, id, "wrong"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.AuthorProfile(ctx, ann); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("without published materials the profile disappears: %v", err)
	}
}

func TestIntegration_Withdraw_OnlyThePendingOwnMaterial(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, other, mod := e.user(t, "author"), e.user(t, "other"), e.moderator(t, "mod")
	id := e.pending(t, author, "Changed my mind", "x")

	if _, err := e.lib.Withdraw(ctx, other, id); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("someone else's note: %v", err)
	}
	if _, err := e.lib.Withdraw(ctx, author, uuid.New()); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("unknown note: %v", err)
	}
	n, err := e.lib.Withdraw(ctx, author, id)
	if err != nil || n.Status != "draft" {
		t.Fatalf("withdraw: %v, %v", n.Status, err)
	}
	if q, _ := e.lib.Queue(ctx, mod, 100, ""); contains(queueIDs(q), id) {
		t.Error("a withdrawn material must leave the queue")
	}
	if _, err := e.lib.Decide(ctx, mod, id, library.DecisionApproved, ""); !errors.Is(err, library.ErrInvalidTransition) {
		t.Errorf("the moderator cannot decide on a withdrawn material: %v", err)
	}
	if _, err := e.lib.Withdraw(ctx, author, id); !errors.Is(err, library.ErrInvalidTransition) {
		t.Errorf("a draft cannot be withdrawn again: %v", err)
	}
	if _, err := e.lib.Submit(ctx, author, id); err != nil {
		t.Errorf("after the withdrawal the material can be submitted again: %v", err)
	}
	pub := e.published(t, author, mod, "Published", "x")
	if _, err := e.lib.Withdraw(ctx, author, pub); !errors.Is(err, library.ErrInvalidTransition) {
		t.Errorf("a published material cannot be withdrawn: %v", err)
	}
}

func queueIDs(p library.QueuePage) []uuid.UUID {
	out := make([]uuid.UUID, len(p.Items))
	for i, it := range p.Items {
		out[i] = it.ID
	}
	return out
}

func TestIntegration_Unpublish_TakesAMaterialDownWithAReason_KeepsItsSocialData_AndAllowsAResubmit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	idx := &recordingIndexer{}
	lib := library.NewService(e.q, library.WithIndexer(idx))
	author, reader, mod, student := e.user(t, "author"), e.user(t, "reader"), e.moderator(t, "mod"), e.user(t, "student")
	id := e.published(t, author, mod, "Had a mistake", "x")
	if _, err := lib.Rate(ctx, reader, id, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.AddComment(ctx, reader, id, "nice"); err != nil {
		t.Fatal(err)
	}
	if err := lib.AddBookmark(ctx, reader, id); err != nil {
		t.Fatal(err)
	}

	if _, err := lib.Unpublish(ctx, student, id, "no right"); !errors.Is(err, library.ErrForbidden) {
		t.Fatalf("a student must not take material down: %v", err)
	}
	if _, err := lib.Unpublish(ctx, mod, id, "   "); !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("a reason is required: %v", err)
	}
	if _, err := lib.Unpublish(ctx, mod, id, strings.Repeat("я", 1001)); !errors.Is(err, library.ErrInvalid) {
		t.Fatalf("a reason longer than 1000 characters: %v", err)
	}
	if m, _ := lib.Get(ctx, nil, id); m.Status != "published" {
		t.Fatal("refused attempts must change nothing")
	}

	n, err := lib.Unpublish(ctx, mod, id, "Theorem is stated wrongly")
	if err != nil || n.Status != "rejected" || n.PublishedAt != nil {
		t.Fatalf("unpublish: %+v, %v", n.Status, err)
	}
	if last := idx.docs[len(idx.docs)-1]; last.ID != id || last.Status != "rejected" {
		t.Errorf("the search index must learn about the takedown: %+v", last)
	}
	if _, err := lib.Get(ctx, nil, id); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("the material must leave the library: %v", err)
	}
	if m, err := lib.Get(ctx, &author, id); err != nil || m.Status != "rejected" || m.RejectionReason == nil || *m.RejectionReason != "Theorem is stated wrongly" {
		t.Errorf("the author sees the reason: %+v, %v", m.RejectionReason, err)
	}
	if own, _ := e.notes.Get(ctx, author, id); own.RejectionReason == nil || *own.RejectionReason != "Theorem is stated wrongly" {
		t.Errorf("the personal API shows the reason as well: %v", own.RejectionReason)
	}
	if fav, _ := lib.Bookmarks(ctx, reader, 10, ""); contains(ids(fav.Items), id) {
		t.Error("a taken-down material leaves the favourites")
	}
	if _, err := lib.Unpublish(ctx, mod, id, "again"); !errors.Is(err, library.ErrInvalidTransition) {
		t.Errorf("it is not published any more: %v", err)
	}
	if _, err := lib.Unpublish(ctx, mod, uuid.New(), "x"); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("unknown material: %v", err)
	}

	// Автор исправляет и отправляет заново; после одобрения оценки, комментарий и избранное на месте.
	if _, err := lib.Submit(ctx, author, id); err != nil {
		t.Fatalf("resubmit: %v", err)
	}
	if _, err := lib.Decide(ctx, mod, id, library.DecisionApproved, ""); err != nil {
		t.Fatal(err)
	}
	m, err := lib.Get(ctx, &reader, id)
	if err != nil || m.RatingCount != 1 || m.CommentsCount != 1 || !m.Bookmarked || m.MyRating == nil || *m.MyRating != 4 {
		t.Errorf("social data must survive the takedown: ratings %d, comments %d, bookmarked %v, %v", m.RatingCount, m.CommentsCount, m.Bookmarked, err)
	}
	var kinds []string
	rows, err := e.tx.Query(ctx, `SELECT decision FROM moderation_decisions WHERE note_id = $1 ORDER BY decision`, id) // внутри транзакции теста now() одинаков, порядок по времени не определён
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var d string
		_ = rows.Scan(&d)
		kinds = append(kinds, d)
	}
	rows.Close()
	if strings.Join(kinds, ",") != "approved,approved,unpublished" {
		t.Errorf("decision history = %v", kinds)
	}
}

func TestIntegration_Unpublish_LosesARaceWithTheAuthorsEdit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, mod := e.user(t, "author"), e.moderator(t, "mod")
	id := e.published(t, author, mod, "Edited meanwhile", "x")
	if _, err := e.notes.Update(ctx, author, id, note.Patch{Title: ptr("New title")}); err != nil { // уже на проверке
		t.Fatal(err)
	}
	if _, err := e.lib.Unpublish(ctx, mod, id, "too late"); !errors.Is(err, library.ErrInvalidTransition) {
		t.Errorf("err = %v, a pending material is not published", err)
	}
}

func TestIntegration_EditComment_OnlyTheAuthor_OnlyWhilePublished_ShowsEditedAt(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, bob, eve, mod := e.user(t, "author"), e.user(t, "bob"), e.user(t, "eve"), e.moderator(t, "mod")
	id := e.published(t, author, mod, "Discussed", "x")
	c, err := e.lib.AddComment(ctx, bob, id, "first version")
	if err != nil || c.EditedAt != nil {
		t.Fatalf("a new comment has no edit time: %+v, %v", c, err)
	}

	if _, err := e.lib.EditComment(ctx, eve, c.ID, "hijacked"); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("a stranger editing: %v", err)
	}
	if _, err := e.lib.EditComment(ctx, bob, c.ID, "   "); !errors.Is(err, library.ErrInvalid) {
		t.Errorf("an empty edit: %v", err)
	}
	if _, err := e.lib.EditComment(ctx, bob, c.ID, strings.Repeat("я", 2001)); !errors.Is(err, library.ErrInvalid) {
		t.Errorf("an over-long edit: %v", err)
	}
	if _, err := e.lib.EditComment(ctx, bob, uuid.New(), "x"); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("unknown comment: %v", err)
	}
	edited, err := e.lib.EditComment(ctx, bob, c.ID, "  second version  ")
	if err != nil || edited.Body != "second version" || edited.EditedAt == nil || edited.Author.DisplayName != "bob" || edited.ID != c.ID {
		t.Fatalf("edit: %+v, %v", edited, err)
	}
	list, _ := e.lib.ListComments(ctx, id, 10, "")
	if len(list.Items) != 1 || list.Items[0].Body != "second version" || list.Items[0].EditedAt == nil {
		t.Errorf("the list must show the edit: %+v", list.Items)
	}

	// Удалённый комментарий не правится.
	if err := e.lib.DeleteComment(ctx, bob, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.EditComment(ctx, bob, c.ID, "undead"); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("a deleted comment: %v", err)
	}
	// Когда материал снят с публикации, комментарии к нему не правятся.
	c2, _ := e.lib.AddComment(ctx, bob, id, "another")
	if _, err := e.lib.Unpublish(ctx, mod, id, "down"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.EditComment(ctx, bob, c2.ID, "after takedown"); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("a comment of an unpublished material: %v", err)
	}
}

// Удаление аккаунта убирает всё, что принадлежит пользователю, и не трогает чужое.
func TestIntegration_DeleteAccount_CascadesToEverythingTheUserOwns(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	svc := auth.NewService(e.q, []byte("0123456789abcdef0123456789abcdef"), auth.WithBcryptCost(bcrypt.MinCost))
	reg, err := svc.Register(ctx, "leaver-"+uuid.NewString()+"@x.io", "password123", "Leaver", "")
	if err != nil {
		t.Fatal(err)
	}
	leaver := reg.User.ID
	stay, mod := e.user(t, "stays"), e.moderator(t, "mod")
	mine := e.published(t, leaver, mod, "Leaver's material", "x")
	theirs := e.published(t, stay, mod, "Stays", "x")
	if _, err := e.lib.AddComment(ctx, leaver, theirs, "my comment"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.Rate(ctx, leaver, theirs, 5); err != nil {
		t.Fatal(err)
	}
	if err := e.lib.AddBookmark(ctx, leaver, theirs); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.Rate(ctx, stay, mine, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := e.q.CreateAttachment(ctx, db.CreateAttachmentParams{
		UserID: leaver, NoteID: uuid.NullUUID{UUID: mine, Valid: true}, ObjectKey: "key-" + uuid.NewString(), FileName: "a.pdf", MimeType: "application/pdf", SizeBytes: 1,
	}); err != nil {
		t.Fatal(err)
	}
	keys, err := e.q.ListUserObjectKeys(ctx, leaver)
	if err != nil || len(keys) != 1 {
		t.Fatalf("object keys before the deletion: %v, %v", keys, err)
	}

	if err := svc.DeleteAccount(ctx, leaver, "wrong-password"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	if err := svc.DeleteAccount(ctx, leaver, "password123"); err != nil {
		t.Fatal(err)
	}

	count := func(query string, args ...any) int {
		var n int
		if err := e.tx.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for name, q := range map[string]string{
		"users":         `SELECT count(*) FROM users WHERE id = $1`,
		"notes":         `SELECT count(*) FROM notes WHERE user_id = $1`,
		"sessions":      `SELECT count(*) FROM refresh_tokens WHERE user_id = $1`,
		"comments":      `SELECT count(*) FROM comments WHERE user_id = $1`,
		"ratings given": `SELECT count(*) FROM note_ratings WHERE user_id = $1`,
		"bookmarks":     `SELECT count(*) FROM note_bookmarks WHERE user_id = $1`,
		"attachments":   `SELECT count(*) FROM attachments WHERE user_id = $1`,
	} {
		if n := count(q, leaver); n != 0 {
			t.Errorf("%s of a deleted user: %d rows left", name, n)
		}
	}
	if _, err := e.lib.Get(ctx, nil, mine); !errors.Is(err, library.ErrNotFound) {
		t.Errorf("the deleted user's material must disappear from the library: %v", err)
	}
	m, err := e.lib.Get(ctx, nil, theirs)
	if err != nil || m.CommentsCount != 0 || m.RatingCount != 0 || m.BookmarksCount != 0 {
		t.Errorf("another user's material loses only the leaver's comment, rating and bookmark: %+v, %v", m, err)
	}
}

func TestIntegration_DeletingAModerator_KeepsTheirDecisionsWithoutAuthor(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, mod := e.user(t, "author"), e.moderator(t, "mod")
	id := e.published(t, author, mod, "Approved long ago", "x")
	if _, err := e.q.DeleteUser(ctx, mod); err != nil {
		t.Fatalf("deleting a moderator must not be blocked by their decisions: %v", err)
	}
	var moderator *uuid.UUID
	if err := e.tx.QueryRow(ctx, `SELECT moderator_id FROM moderation_decisions WHERE note_id = $1`, id).Scan(&moderator); err != nil {
		t.Fatalf("the decision must stay in the history: %v", err)
	}
	if moderator != nil {
		t.Errorf("the author of the decision is unknown now, got %v", moderator)
	}
	if m, err := e.lib.Get(ctx, nil, id); err != nil || m.Status != "published" {
		t.Errorf("the material stays published: %v, %v", m.Status, err)
	}
}

// Сброс пароля на настоящей базе: токен хранится хешем, один раз, и гасит сессии.
type captureMailer struct{ bodies chan string }

func (c *captureMailer) Send(_ context.Context, _, _, body string) error {
	c.bodies <- body
	return nil
}

func TestIntegration_PasswordReset_OnARealDatabase(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	mailer := &captureMailer{bodies: make(chan string, 4)}
	svc := auth.NewService(e.q, []byte("0123456789abcdef0123456789abcdef"), auth.WithBcryptCost(bcrypt.MinCost),
		auth.WithMailer(mailer, "https://app.example.com/reset"))
	email := "reset-" + uuid.NewString() + "@x.io"
	reg, err := svc.Register(ctx, email, "password123", "Resetter", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestPasswordReset(ctx, email); err != nil {
		t.Fatal(err)
	}
	var body string
	select {
	case body = <-mailer.bodies:
	case <-time.After(3 * time.Second):
		t.Fatal("no mail")
	}
	token := body[strings.Index(body, "token=")+len("token="):]
	token = strings.Fields(token)[0]

	var stored, expires int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM password_reset_tokens WHERE token_hash = $1`, auth.HashRefreshToken(token)).Scan(&stored)
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM password_reset_tokens WHERE token_hash = $1`, token).Scan(&expires)
	if stored != 1 || expires != 0 {
		t.Fatalf("only the hash may be stored: hash rows %d, raw rows %d", stored, expires)
	}

	if err := svc.ConfirmPasswordReset(ctx, token, "brand-new-pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, email, "brand-new-pass", ""); err != nil {
		t.Errorf("the new password: %v", err)
	}
	if _, err := svc.Refresh(ctx, reg.RefreshToken, ""); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("the old session must be dead: %v", err)
	}
	if err := svc.ConfirmPasswordReset(ctx, token, "another-pass-1"); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("a link works once: %v", err)
	}
	var left int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM password_reset_tokens WHERE user_id = $1`, reg.User.ID).Scan(&left)
	if left != 0 {
		t.Errorf("used links must not linger: %d", left)
	}
	// Просроченная ссылка.
	if err := svc.RequestPasswordReset(ctx, email); err != nil {
		t.Fatal(err)
	}
	body = <-mailer.bodies
	expired := strings.Fields(body[strings.Index(body, "token=")+6:])[0]
	if _, err := e.tx.Exec(ctx, `UPDATE password_reset_tokens SET expires_at = now() - interval '1 minute' WHERE user_id = $1`, reg.User.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmPasswordReset(ctx, expired, "brand-new-pass-2"); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("an expired link: %v", err)
	}
}

// Сам запрос «забрать токен» должен быть атомарным и одноразовым, независимо от того, что потом чистит сервис.
func TestIntegration_ClaimPasswordResetToken_IsOneShotAndHonoursExpiry(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	user := e.user(t, "claimer")
	now := time.Now()
	create := func(hash string, expires time.Time) {
		t.Helper()
		if err := e.q.CreatePasswordResetToken(ctx, db.CreatePasswordResetTokenParams{UserID: user, TokenHash: hash, ExpiresAt: expires}); err != nil {
			t.Fatal(err)
		}
	}
	claim := func(hash string, at time.Time) (uuid.UUID, error) {
		return e.q.ClaimPasswordResetToken(ctx, db.ClaimPasswordResetTokenParams{TokenHash: hash, ExpiresAt: at})
	}
	create("live-"+uuid.NewString(), now.Add(time.Hour))
	create("old-"+uuid.NewString(), now.Add(-time.Minute))
	var live, old string
	_ = e.tx.QueryRow(ctx, `SELECT token_hash FROM password_reset_tokens WHERE token_hash LIKE 'live-%' AND user_id = $1`, user).Scan(&live)
	_ = e.tx.QueryRow(ctx, `SELECT token_hash FROM password_reset_tokens WHERE token_hash LIKE 'old-%' AND user_id = $1`, user).Scan(&old)

	if got, err := claim(live, now); err != nil || got != user {
		t.Fatalf("the first claim: %v, %v", got, err)
	}
	if _, err := claim(live, now); err == nil {
		t.Error("a token must be claimable once")
	}
	if _, err := claim(old, now); err == nil {
		t.Error("an expired token must not be claimable")
	}
	if _, err := claim("no-such-hash", now); err == nil {
		t.Error("an unknown token must not be claimable")
	}
}
