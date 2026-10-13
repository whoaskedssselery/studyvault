package httpapi

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// trimmedField обрезает пробелы по краям и проверяет длину в символах, а не в байтах:
// заголовок из 300 русских букв занимает 600 байт и должен проходить.
func trimmedField(field, raw string, maxLen int) (string, error) {
	value := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(value); n < 1 || n > maxLen {
		return "", fmt.Errorf("%s must be 1 to %d characters", field, maxLen)
	}
	return value, nil
}

func trimmedTitle(raw string, maxLen int) (string, error) { return trimmedField("title", raw, maxLen) }
