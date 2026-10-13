package note_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/whoaskedssselery/studyvault/backend/internal/note"
)

func paragraph(text string) string {
	return `{"type":"paragraph","content":[{"type":"text","text":"` + text + `"}]}`
}

func docOf(blocks ...string) []byte {
	return []byte(`{"type":"doc","content":[` + strings.Join(blocks, ",") + `]}`)
}

func TestParseLinks(t *testing.T) {
	cases := map[string]struct {
		doc  []byte
		want []string
	}{
		"single":                   {docOf(paragraph("see [[Algebra]] now")), []string{"Algebra"}},
		"several in order":         {docOf(paragraph("[[B]] then [[A]]")), []string{"B", "A"}},
		"duplicates ignoring case": {docOf(paragraph("[[Alg]] [[alg]] [[ALG]] [[Calc]]")), []string{"Alg", "Calc"}},
		"spaces are trimmed":       {docOf(paragraph("[[   Spaced out  ]]")), []string{"Spaced out"}},
		"alias is dropped":         {docOf(paragraph("[[Real title|shown text]]")), []string{"Real title"}},
		"empty brackets":           {docOf(paragraph("[[]] and [[   ]]")), nil},
		"unbalanced":               {docOf(paragraph("[[open and close]] but [[never closed")), []string{"open and close"}},
		"cyrillic":                 {docOf(paragraph("см. [[Производные функций]]")), []string{"Производные функций"}},
		"across blocks":            {docOf(paragraph("[[Start"), paragraph("end]]")), nil},
		"one per block":            {docOf(paragraph("[[A]]"), paragraph("[[B]]")), []string{"A", "B"}},
		"no links":                 {docOf(paragraph("plain text")), nil},
		"invalid json":             {[]byte(`not json`), nil},
		"empty document":           {[]byte(`{"type":"doc"}`), nil},
		"split by formatting": {
			[]byte(`{"type":"doc","content":[{"type":"paragraph","content":[
				{"type":"text","text":"[[Alg"},
				{"type":"text","marks":[{"type":"bold"}],"text":"eb"},
				{"type":"text","text":"ra]]"}]}]}`),
			[]string{"Algebra"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := note.ParseLinks(c.doc)
			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestParseLinks_CapsTheNumberOfLinks(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 150; i++ {
		fmt.Fprintf(&sb, "[[note %d]] ", i)
	}

	got := note.ParseLinks(docOf(paragraph(sb.String())))

	if len(got) != 100 {
		t.Errorf("got %d links, want the first 100", len(got))
	}
	if got[0] != "note 0" || got[99] != "note 99" {
		t.Errorf("must keep the first 100 in order: %q ... %q", got[0], got[99])
	}
}
