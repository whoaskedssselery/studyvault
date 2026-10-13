package note

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Курсор — это (updated_at, id) последней заметки страницы, упакованные в непрозрачную строку.
type cursor struct {
	UpdatedAt time.Time `json:"t"`
	ID        uuid.UUID `json:"id"`
}

func encodeCursor(updatedAt time.Time, id uuid.UUID) string {
	b, _ := json.Marshal(cursor{UpdatedAt: updatedAt, ID: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cursor{}, ErrInvalidCursor
	}
	var c cursor
	if err := json.Unmarshal(raw, &c); err != nil || c.UpdatedAt.IsZero() || c.ID == uuid.Nil {
		return cursor{}, ErrInvalidCursor
	}
	return c, nil
}
