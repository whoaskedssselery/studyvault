package search_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/meilisearch/meilisearch-go"

	"github.com/whoaskedssselery/studyvault/backend/internal/search"
)

// Тесты адаптера идут на настоящем Meilisearch (docker compose up) в отдельном временном индексе,
// который удаляется в конце. Без MEILI_URL они пропускаются.
func meiliEnv(t *testing.T) (url, key string) {
	t.Helper()
	url, key = os.Getenv("MEILI_URL"), os.Getenv("MEILI_MASTER_KEY")
	if url == "" {
		t.Skip("MEILI_URL is not set")
	}
	return url, key
}

func newMeili(t *testing.T) *search.Meili {
	t.Helper()
	url, key := meiliEnv(t)
	uid := "notes_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	t.Cleanup(func() { _, _ = meilisearch.New(url, meilisearch.WithAPIKey(key)).DeleteIndex(uid) })
	return search.NewMeili(url, key, uid)
}

func settle(t *testing.T, m *search.Meili) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := m.Wait(ctx); err != nil {
		t.Fatalf("waiting for the indexer: %v", err)
	}
}

func doc(user uuid.UUID, title, text string) search.Doc {
	return search.Doc{ID: uuid.New(), UserID: user, Title: title, ContentText: text}
}

func ids(hits []search.Hit) []uuid.UUID {
	out := make([]uuid.UUID, len(hits))
	for i, h := range hits {
		out[i] = h.ID
	}
	return out
}

func TestMeili_ReturnsOnlyTheCallersDocuments(t *testing.T) {
	m := newMeili(t)
	ctx := context.Background()
	alice, bob := uuid.New(), uuid.New()
	a, b := doc(alice, "Algebra", "groups and rings"), doc(bob, "Algebra", "groups and fields")
	for _, d := range []search.Doc{a, b} {
		if err := m.Upsert(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	settle(t, m)

	hits, err := m.Search(ctx, alice, "groups", 20)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(hits); len(got) != 1 || got[0] != a.ID {
		t.Fatalf("alice must see only her own note, got %v", got)
	}
	hits, _ = m.Search(ctx, bob, "groups", 20)
	if got := ids(hits); len(got) != 1 || got[0] != b.ID {
		t.Fatalf("bob must see only his own note, got %v", got)
	}
	hits, _ = m.Search(ctx, uuid.New(), "groups", 20)
	if len(hits) != 0 {
		t.Fatalf("a stranger must see nothing, got %d hits", len(hits))
	}
}

func TestMeili_FindsCyrillicAndToleratesTypos(t *testing.T) {
	m := newMeili(t)
	ctx := context.Background()
	uid := uuid.New()
	ru, en := doc(uid, "Лекция", "Линейная алгебра: векторные пространства"), doc(uid, "Lecture", "Abstract algebra and group theory")
	_ = m.Upsert(ctx, ru)
	_ = m.Upsert(ctx, en)
	settle(t, m)

	for query, want := range map[string]uuid.UUID{
		"алгебра":    ru.ID,
		"АЛГЕБРА":    ru.ID,
		"векторныее": ru.ID, // опечатка
		"group":      en.ID,
		"abstrct":    en.ID, // опечатка (опечатки допускаются со слов от 5 букв)
	} {
		hits, err := m.Search(ctx, uid, query, 20)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, h := range hits {
			found = found || h.ID == want
		}
		if !found {
			t.Errorf("query %q did not find the expected note (hits: %d)", query, len(hits))
		}
	}
}

func TestMeili_MatchesInTheTitleToo(t *testing.T) {
	m := newMeili(t)
	ctx := context.Background()
	uid := uuid.New()
	d := doc(uid, "Thermodynamics", "nothing relevant in the body")
	_ = m.Upsert(ctx, d)
	settle(t, m)

	hits, _ := m.Search(ctx, uid, "thermodynamics", 20)

	if len(hits) != 1 || hits[0].ID != d.ID {
		t.Fatalf("a title-only match must be found: %v", ids(hits))
	}
}

func TestMeili_RemoveAndUpdate(t *testing.T) {
	m := newMeili(t)
	ctx := context.Background()
	uid := uuid.New()
	d := doc(uid, "Note", "first version mentions apples")
	_ = m.Upsert(ctx, d)
	settle(t, m)

	d.ContentText = "second version mentions pears"
	_ = m.Upsert(ctx, d)
	settle(t, m)
	if hits, _ := m.Search(ctx, uid, "apples", 20); len(hits) != 0 {
		t.Error("the old text must be gone after an upsert")
	}
	if hits, _ := m.Search(ctx, uid, "pears", 20); len(hits) != 1 {
		t.Error("the new text must be searchable")
	}

	if err := m.Remove(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	settle(t, m)
	if hits, _ := m.Search(ctx, uid, "pears", 20); len(hits) != 0 {
		t.Error("a removed note must not be found")
	}
}

func TestMeili_SnippetIsCroppedAndHighlighted(t *testing.T) {
	m := newMeili(t)
	ctx := context.Background()
	uid := uuid.New()
	long := strings.Repeat("filler words go here ", 60) + "the needle sits in the middle " + strings.Repeat("and more filler ", 60)
	_ = m.Upsert(ctx, doc(uid, "Long", long))
	settle(t, m)

	hits, _ := m.Search(ctx, uid, "needle", 20)

	if len(hits) != 1 {
		t.Fatalf("hits = %d", len(hits))
	}
	s := hits[0].Snippet
	if !strings.Contains(s, "\x01needle\x02") {
		t.Errorf("the match must be wrapped in the marker characters: %q", s)
	}
	if len(s) > len(long)/4 {
		t.Errorf("the snippet must be cropped around the match, got %d of %d bytes", len(s), len(long))
	}
}

func TestMeili_StripsMarkerCharactersFromUserText(t *testing.T) {
	m := newMeili(t)
	ctx := context.Background()
	uid := uuid.New()
	_ = m.Upsert(ctx, doc(uid, "T", "sneaky \x01fake\x02 markers and a needle"))
	settle(t, m)

	hits, _ := m.Search(ctx, uid, "needle", 20)

	s := hits[0].Snippet
	if strings.Count(s, "\x01") != 1 || strings.Count(s, "\x02") != 1 {
		t.Errorf("only the real highlight may carry markers, got %q", s)
	}
}

func TestMeili_SettingsAreIdempotentAcrossAdapters(t *testing.T) {
	url, key := meiliEnv(t)
	uid := "notes_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	t.Cleanup(func() { _, _ = meilisearch.New(url, meilisearch.WithAPIKey(key)).DeleteIndex(uid) })
	ctx := context.Background()
	user := uuid.New()

	first, second := search.NewMeili(url, key, uid), search.NewMeili(url, key, uid)
	d := doc(user, "T", "shared index")
	if err := first.Upsert(ctx, d); err != nil {
		t.Fatal(err)
	}
	settle(t, first)

	hits, err := second.Search(ctx, user, "shared", 20)
	if err != nil || len(hits) != 1 {
		t.Fatalf("a second adapter on the same index: hits = %d, err = %v", len(hits), err)
	}
}

func TestMeili_Unreachable_ReturnsErrors(t *testing.T) {
	m := search.NewMeili("http://127.0.0.1:1", "key", "notes")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := m.Search(ctx, uuid.New(), "x", 10); err == nil {
		t.Error("Search must fail when the server is down")
	}
	if err := m.Upsert(ctx, doc(uuid.New(), "T", "x")); err == nil {
		t.Error("Upsert must fail when the server is down")
	}
}

func TestMeili_ReplaceAll_DropsStaleDocuments(t *testing.T) {
	m := newMeili(t)
	ctx := context.Background()
	uid := uuid.New()
	stale, fresh := doc(uid, "Stale", "ghost"), doc(uid, "Fresh", "ghost")
	_ = m.Upsert(ctx, stale)
	settle(t, m)

	calls := 0
	n, err := m.ReplaceAll(ctx, func(context.Context) ([]search.Doc, error) {
		calls++
		if calls == 1 {
			return []search.Doc{fresh}, nil
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	settle(t, m)

	if n != 1 {
		t.Errorf("indexed %d documents, want 1", n)
	}
	hits, _ := m.Search(ctx, uid, "ghost", 20)
	if got := ids(hits); len(got) != 1 || got[0] != fresh.ID {
		t.Errorf("only the fresh document may remain, got %v", got)
	}
}
