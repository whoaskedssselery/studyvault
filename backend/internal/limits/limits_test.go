package limits_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/whoaskedssselery/studyvault/backend/internal/limits"
)

func TestReached_WrapsTheSentinelAndSaysWhatWasExceeded(t *testing.T) {
	err := limits.Reached("subjects", 200)

	if !errors.Is(err, limits.ErrReached) {
		t.Fatalf("%v must wrap ErrReached", err)
	}
	if !strings.Contains(err.Error(), "at most 200 subjects") {
		t.Errorf("the message must name the limit: %q", err)
	}
}

// Числа здесь закреплены намеренно: они же написаны в контракте, README и docs/errors.md.
func TestQuotas_MatchTheDocumentation(t *testing.T) {
	got := [...]int64{limits.MaxSubjectsPerUser, limits.MaxTagsPerUser, limits.MaxAttachmentsPerNote, limits.MaxAttachmentsPerUser, limits.MaxAttachmentBytesPerUser}
	want := [...]int64{200, 500, 3, 5000, 1 << 30}
	if got != want {
		t.Errorf("quotas = %v, documented %v", got, want)
	}
}
