package note_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/whoaskedssselery/studyvault/backend/internal/note"
)

// Документы приходят от пользователя, поэтому разборщики не должны паниковать ни на каком входе и обязаны
// держать свои обещания. Запуск: go test -fuzz=FuzzParseLinks -fuzztime=30s ./internal/note
func FuzzParseLinks(f *testing.F) {
	for _, seed := range []string{
		`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"see [[Limits]] and [[Limits|again]]"}]}]}`,
		`{"type":"doc","content":[{"type":"text","text":"[[ ]] [[a]] [[a|b|c]] [[[[x]]]] [[\n]]"}]}`,
		`{"type":"doc"}`, `null`, `[]`, `"[[x]]"`, `{`, ``, "\xff\xfe",
		`{"content":[{"content":[{"content":[{"text":"[[deep]]"}]}]}]}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, content []byte) {
		titles := note.ParseLinks(content)

		if len(titles) > 100 {
			t.Fatalf("%d links, the limit is 100", len(titles))
		}
		seen := map[string]bool{}
		for _, title := range titles {
			if title == "" || title != strings.TrimSpace(title) {
				t.Fatalf("title %q must be non-empty and trimmed", title)
			}
			if strings.ContainsAny(title, "[]\n|") {
				t.Fatalf("title %q contains a character that cannot be part of a link target", title)
			}
			key := strings.ToLower(title)
			if seen[key] {
				t.Fatalf("title %q is repeated", title)
			}
			seen[key] = true
		}
	})
}

func FuzzExtractText(f *testing.F) {
	for _, seed := range []string{
		`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"привет"}]}]}`,
		`{"type":"text","text":"\ud800"}`, `null`, `42`, `{`, ``, "\xff",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, content []byte) {
		if text := note.ExtractText(content); !utf8.ValidString(text) {
			t.Fatalf("the extracted text must be valid UTF-8 (it goes into a text column): %q", text)
		}
	})
}
