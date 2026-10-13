package note

import (
	"encoding/json"
	"strings"
)

const maxTextDepth = 100

// ExtractText собирает из документа Tiptap плоский текст для поиска.
// Слитые в один абзац куски текста (жирный, курсив) склеиваются без разделителей,
// каждый блок (абзац, заголовок, пункт списка) заканчивается переводом строки.
// Невалидный JSON даёт пустую строку: сам документ при этом сохраняется как есть.
func ExtractText(content []byte) string {
	var root map[string]any
	if err := json.Unmarshal(content, &root); err != nil || root == nil {
		return ""
	}
	var sb strings.Builder
	walk(root, &sb, 0)
	return strings.TrimSpace(sb.String())
}

func walk(node map[string]any, sb *strings.Builder, depth int) {
	if depth > maxTextDepth {
		return
	}
	switch node["type"] {
	case "text":
		if s, ok := node["text"].(string); ok {
			sb.WriteString(s)
		}
		return
	case "hardBreak":
		sb.WriteByte('\n')
		return
	}

	children, _ := node["content"].([]any)
	for _, c := range children {
		if child, ok := c.(map[string]any); ok {
			walk(child, sb, depth+1)
		}
	}
	if len(children) > 0 && node["type"] != "doc" {
		endLine(sb)
	}
}

func endLine(sb *strings.Builder) {
	if s := sb.String(); len(s) > 0 && s[len(s)-1] != '\n' {
		sb.WriteByte('\n')
	}
}
