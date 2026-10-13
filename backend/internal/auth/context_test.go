package auth_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
)

func TestUserIDContext_RoundTrip(t *testing.T) {
	id := uuid.New()

	got, ok := auth.UserID(auth.WithUserID(context.Background(), id))

	if !ok || got != id {
		t.Errorf("UserID = %v, %v, want %v", got, ok, id)
	}
	if _, ok := auth.UserID(context.Background()); ok {
		t.Error("an empty context must not report a user")
	}
}
