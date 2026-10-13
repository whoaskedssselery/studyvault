package note

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// Курсор приходит из query string, то есть от клиента: любая строка должна давать либо курсор, либо
// ErrInvalidCursor, и всё, что мы сами выдали, должно разбираться обратно без потерь.
func FuzzDecodeCursor(f *testing.F) {
	f.Add(encodeCursor(time.Now().UTC(), uuid.New()))
	for _, seed := range []string{"", "!!!", "e30", "bnVsbA", "eyJ0IjoiMDAwMS0wMS0wMVQwMDowMDowMFoiLCJpZCI6IjAwMDAwMDAwLTAwMDAtMDAwMC0wMDAwLTAwMDAwMDAwMDAwMCJ9"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		c, err := decodeCursor(s)
		if err != nil {
			return
		}
		if c.UpdatedAt.IsZero() || c.ID == uuid.Nil {
			t.Fatalf("an accepted cursor must carry a time and an id: %+v", c)
		}
		back, err := decodeCursor(encodeCursor(c.UpdatedAt, c.ID))
		if err != nil || !back.UpdatedAt.Equal(c.UpdatedAt) || back.ID != c.ID {
			t.Fatalf("re-encoding changed the cursor: %+v -> %+v (%v)", c, back, err)
		}
	})
}
