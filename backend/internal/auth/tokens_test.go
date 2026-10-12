package auth_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
)

var secret = []byte("test-secret-test-secret-test-secret")

func TestAccessToken_RoundTrip(t *testing.T) {
	now := time.Now()
	id := uuid.New()

	tok, err := auth.NewAccessToken(secret, id, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := auth.ParseAccessToken(secret, tok, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ParseAccessToken: %v", err)
	}
	if got != id {
		t.Errorf("user id = %v, want %v", got, id)
	}
}

func TestAccessToken_Expired_ReturnsError(t *testing.T) {
	now := time.Now()
	tok, _ := auth.NewAccessToken(secret, uuid.New(), now)

	if _, err := auth.ParseAccessToken(secret, tok, now.Add(auth.AccessTokenTTL+time.Second)); err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestAccessToken_WrongSecret_ReturnsError(t *testing.T) {
	now := time.Now()
	tok, _ := auth.NewAccessToken(secret, uuid.New(), now)

	if _, err := auth.ParseAccessToken([]byte("another-secret-another-secret-123"), tok, now); err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestAccessToken_AlgNone_ReturnsError(t *testing.T) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   uuid.NewString(),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := auth.ParseAccessToken(secret, tok, now); err == nil {
		t.Fatal("unsigned token must be rejected")
	}
}

func TestAccessToken_NoExpiry_ReturnsError(t *testing.T) {
	claims := jwt.RegisteredClaims{Subject: uuid.NewString()}
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)

	if _, err := auth.ParseAccessToken(secret, tok, time.Now()); err == nil {
		t.Fatal("token without exp must be rejected")
	}
}

func TestAccessToken_Garbage_ReturnsError(t *testing.T) {
	if _, err := auth.ParseAccessToken(secret, "not-a-jwt", time.Now()); err == nil {
		t.Fatal("expected error for garbage token")
	}
}

func TestNewRefreshToken_HashMatchesAndIsUnique(t *testing.T) {
	raw1, hash1, err := auth.NewRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	raw2, _, _ := auth.NewRefreshToken()

	if raw1 == raw2 {
		t.Error("refresh tokens must be unique")
	}
	if auth.HashRefreshToken(raw1) != hash1 {
		t.Error("HashRefreshToken(raw) must equal the returned hash")
	}
	if raw1 == hash1 {
		t.Error("hash must differ from the raw token")
	}
}
