// Package limits собирает квоты на пользователя в одном месте. Без них один аккаунт мог бы бесконечно плодить
// предметы, теги и файлы, а списки, которые отдаются целиком, росли бы без предела.
package limits

import (
	"errors"
	"fmt"
)

const (
	MaxSubjectsPerUser        = 200
	MaxTagsPerUser            = 500
	MaxAttachmentsPerNote     = 3 // по ТЗ
	MaxAttachmentsPerUser     = 5000
	MaxAttachmentBytesPerUser = 1 << 30 // 1 ГиБ на все файлы пользователя
)

// ErrReached сигнализирует о достигнутой квоте. Квоты мягкие: проверка и вставка не атомарны, поэтому при
// одновременных запросах предел можно превысить на число параллельных запросов, но не обойти.
var ErrReached = errors.New("limit reached")

// Reached строит ошибку с понятным пользователю текстом: «limit reached: at most 200 subjects».
func Reached(what string, max int64) error {
	return fmt.Errorf("%w: at most %d %s", ErrReached, max, what)
}
