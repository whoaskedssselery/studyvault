package httpapi

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// trimmedTitle обрезает пробелы по краям и проверяет длину в символах, а не в байтах:
// заголовок из 300 русских букв занимает 600 байт и должен проходить.
func trimmedTitle(raw string, maxLen int) (string, error) {
	title := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(title); n < 1 || n > maxLen {
		return "", fmt.Errorf("title must be 1 to %d characters", maxLen)
	}
	return title, nil
}
