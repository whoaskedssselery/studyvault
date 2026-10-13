package note

import (
	"regexp"
	"strings"
)

const maxLinksPerNote = 100

// Название внутри [[...]] не может содержать скобки и перевод строки, поэтому ссылка
// не склеивается из двух абзацев.
var wikiLink = regexp.MustCompile(`\[\[([^\[\]\n]+)\]\]`)

// ParseLinks возвращает названия заметок, на которые ссылается документ через [[Название]].
// Повторы (без учёта регистра) схлопываются, пустые ссылки пропускаются, запись [[Название|подпись]]
// даёт «Название». Ссылки ищутся в плоском тексте, поэтому форматирование внутри них (жирный,
// курсив) не мешает. Больше 100 ссылок на заметку не разбираются.
func ParseLinks(content []byte) []string {
	var titles []string
	seen := map[string]bool{}
	for _, m := range wikiLink.FindAllStringSubmatch(ExtractText(content), -1) {
		title, _, _ := strings.Cut(m[1], "|")
		title = strings.TrimSpace(title)
		key := strings.ToLower(title)
		if title == "" || seen[key] {
			continue
		}
		seen[key] = true
		titles = append(titles, title)
		if len(titles) == maxLinksPerNote {
			break
		}
	}
	return titles
}
