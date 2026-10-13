package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
)

func TestAuthMiddleware_PutsUserIDInContext(t *testing.T) {
	secret := []byte("test-secret-test-secret-test-secret")
	id := uuid.New()
	tok, _ := auth.NewAccessToken(secret, id, time.Now())

	var got uuid.UUID
	var ok bool
	h := authMiddleware(secret)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, ok = auth.UserID(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/tags", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !ok || got != id {
		t.Fatalf("user id in context = %v (ok=%v), want %v", got, ok, id)
	}
}
