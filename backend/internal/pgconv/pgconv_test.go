package pgconv_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/pgconv"
)

func TestNilIsNullAndBack(t *testing.T) {
	if got := pgconv.ToText(nil); got.Valid {
		t.Errorf("nil text must be NULL: %+v", got)
	}
	if got := pgconv.ToInt2(nil); got.Valid {
		t.Errorf("nil int must be NULL: %+v", got)
	}
	if got := pgconv.ToNullUUID(nil); got.Valid {
		t.Errorf("nil uuid must be NULL: %+v", got)
	}
	if pgconv.FromText(pgconv.ToText(nil)) != nil || pgconv.FromInt2(pgconv.ToInt2(nil)) != nil || pgconv.FromNullUUID(pgconv.ToNullUUID(nil)) != nil {
		t.Error("NULL must come back as nil")
	}
}

func TestValuesSurviveARoundTrip_AndEmptyIsNotNull(t *testing.T) {
	empty, n, id := "", int16(0), uuid.New()
	if got := pgconv.FromText(pgconv.ToText(&empty)); got == nil || *got != "" {
		t.Errorf("an empty string is a value, not NULL: %v", got)
	}
	if got := pgconv.FromInt2(pgconv.ToInt2(&n)); got == nil || *got != 0 {
		t.Errorf("zero is a value, not NULL: %v", got)
	}
	if got := pgconv.FromNullUUID(pgconv.ToNullUUID(&id)); got == nil || *got != id {
		t.Errorf("uuid: %v", got)
	}
}

// Результат не должен указывать внутрь исходной структуры: иначе правка по указателю тихо меняла бы строку из базы.
func TestResultsAreCopies(t *testing.T) {
	text := pgconv.ToText(ptr("abc"))
	a := pgconv.FromText(text)
	*a = "changed"
	if text.String != "abc" {
		t.Error("FromText must return a copy")
	}
	i := pgconv.ToInt2(ptr(int16(5)))
	b := pgconv.FromInt2(i)
	*b = 9
	if i.Int16 != 5 {
		t.Error("FromInt2 must return a copy")
	}
	u := pgconv.ToNullUUID(ptr(uuid.New()))
	orig := u.UUID
	c := pgconv.FromNullUUID(u)
	*c = uuid.New()
	if u.UUID != orig {
		t.Error("FromNullUUID must return a copy")
	}
}

func ptr[T any](v T) *T { return &v }
