package note_test

import (
	"strings"
	"testing"

	"github.com/whoaskedssselery/studyvault/backend/internal/note"
)

func TestExtractText(t *testing.T) {
	cases := map[string]struct{ doc, want string }{
		"one paragraph": {
			`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Hello world"}]}]}`,
			"Hello world",
		},
		"paragraphs are separated by a newline": {
			`{"type":"doc","content":[
				{"type":"paragraph","content":[{"type":"text","text":"One"}]},
				{"type":"paragraph","content":[{"type":"text","text":"Two"}]}]}`,
			"One\nTwo",
		},
		"marked text runs are joined without separators": {
			`{"type":"doc","content":[{"type":"paragraph","content":[
				{"type":"text","text":"Hel"},
				{"type":"text","text":"lo","marks":[{"type":"bold"}]}]}]}`,
			"Hello",
		},
		"headings and nested lists": {
			`{"type":"doc","content":[
				{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Title"}]},
				{"type":"bulletList","content":[
					{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]}]},
					{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"b"}]}]}]}]}`,
			"Title\na\nb",
		},
		"hard break becomes a newline": {
			`{"type":"doc","content":[{"type":"paragraph","content":[
				{"type":"text","text":"a"},{"type":"hardBreak"},{"type":"text","text":"b"}]}]}`,
			"a\nb",
		},
		"nodes without text are ignored": {
			`{"type":"doc","content":[
				{"type":"image","attrs":{"src":"x.png"}},
				{"type":"paragraph","content":[{"type":"text","text":"after"}]}]}`,
			"after",
		},
		"wiki links stay in the text": {
			`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"see [[Algebra]] now"}]}]}`,
			"see [[Algebra]] now",
		},
		"empty document": {`{"type":"doc"}`, ""},
		"unicode":        {`{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Привет, мир"}]}]}`, "Привет, мир"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := note.ExtractText([]byte(c.doc)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestExtractText_Garbage_ReturnsEmpty(t *testing.T) {
	for _, in := range []string{``, `not json`, `[]`, `"str"`, `null`, `{"type":"doc","content":"oops"}`} {
		if got := note.ExtractText([]byte(in)); got != "" {
			t.Errorf("input %q: got %q, want empty", in, got)
		}
	}
}

func TestExtractText_VeryDeepNesting_DoesNotPanic(t *testing.T) {
	const depth = 20000
	doc := strings.Repeat(`{"type":"x","content":[`, depth) + strings.Repeat(`]}`, depth)

	_ = note.ExtractText([]byte(doc)) // главное, что не падает и не зависает
}
