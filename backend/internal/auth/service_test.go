package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
)

type fakeStore struct {
	users   map[string]db.User // ключ: email в нижнем регистре
	refresh []db.CreateRefreshTokenParams
	tokens  map[string]*db.RefreshToken // ключ: token_hash
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: map[string]db.User{}, tokens: map[string]*db.RefreshToken{}}
}

func (f *fakeStore) GetRefreshTokenByHash(_ context.Context, hash string) (db.RefreshToken, error) {
	t, ok := f.tokens[hash]
	if !ok {
		return db.RefreshToken{}, pgx.ErrNoRows
	}
	return *t, nil
}

func (f *fakeStore) RevokeRefreshToken(_ context.Context, hash string) (int64, error) {
	t, ok := f.tokens[hash]
	if !ok || t.RevokedAt != nil {
		return 0, nil
	}
	now := time.Now()
	t.RevokedAt = &now
	return 1, nil
}

func (f *fakeStore) RevokeAllUserRefreshTokens(_ context.Context, userID uuid.UUID) error {
	now := time.Now()
	for _, t := range f.tokens {
		if t.UserID == userID && t.RevokedAt == nil {
			t.RevokedAt = &now
		}
	}
	return nil
}

func (f *fakeStore) CreateUser(_ context.Context, a db.CreateUserParams) (db.User, error) {
	key := lower(a.Email)
	if _, ok := f.users[key]; ok {
		return db.User{}, &pgconn.PgError{Code: "23505"}
	}
	u := db.User{ID: uuid.New(), Email: a.Email, PasswordHash: a.PasswordHash, DisplayName: a.DisplayName}
	f.users[key] = u
	return u, nil
}

func (f *fakeStore) GetUserByEmail(_ context.Context, email string) (db.User, error) {
	u, ok := f.users[lower(email)]
	if !ok {
		return db.User{}, pgx.ErrNoRows
	}
	return u, nil
}

func (f *fakeStore) CreateRefreshToken(_ context.Context, a db.CreateRefreshTokenParams) error {
	f.refresh = append(f.refresh, a)
	f.tokens[a.TokenHash] = &db.RefreshToken{
		ID: uuid.New(), UserID: a.UserID, TokenHash: a.TokenHash, UserAgent: a.UserAgent, ExpiresAt: a.ExpiresAt,
	}
	return nil
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func newService(store auth.Store) *auth.Service {
	return auth.NewService(store, secret, auth.WithBcryptCost(bcrypt.MinCost))
}

func TestRegister_StoresHashNotPassword(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)

	res, err := svc.Register(context.Background(), "a@x.io", "password123", "Ann", "ua")
	if err != nil {
		t.Fatal(err)
	}
	u := store.users["a@x.io"]
	if u.PasswordHash == "password123" {
		t.Fatal("password stored in plain text")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte("password123")) != nil {
		t.Error("stored hash does not match the password")
	}
	if res.User.ID != u.ID || res.AccessToken == "" || res.RefreshToken == "" {
		t.Errorf("incomplete result: %+v", res)
	}
}

func TestRegister_DuplicateEmail_ReturnsErrEmailTaken(t *testing.T) {
	svc := newService(newFakeStore())
	if _, err := svc.Register(context.Background(), "a@x.io", "password123", "Ann", ""); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Register(context.Background(), "A@X.IO", "password123", "Ann2", "")
	if !errors.Is(err, auth.ErrEmailTaken) {
		t.Fatalf("err = %v, want ErrEmailTaken", err)
	}
}

func TestLogin_OK_StoresOnlyRefreshHash(t *testing.T) {
	store := newFakeStore()
	svc := newService(store)
	_, _ = svc.Register(context.Background(), "a@x.io", "password123", "Ann", "")
	store.refresh = nil

	res, err := svc.Login(context.Background(), "A@x.io", "password123", "Firefox")
	if err != nil {
		t.Fatal(err)
	}
	if len(store.refresh) != 1 {
		t.Fatalf("refresh tokens stored = %d, want 1", len(store.refresh))
	}
	got := store.refresh[0]
	if got.TokenHash == res.RefreshToken {
		t.Error("raw refresh token was stored")
	}
	if got.TokenHash != auth.HashRefreshToken(res.RefreshToken) {
		t.Error("stored hash does not match the issued token")
	}
	if got.UserID != res.User.ID || !got.ExpiresAt.After(time.Now().Add(24*time.Hour)) {
		t.Errorf("bad refresh record: %+v", got)
	}
	if !got.UserAgent.Valid || got.UserAgent.String != "Firefox" {
		t.Errorf("user agent = %+v", got.UserAgent)
	}
}

func TestLogin_WrongPassword_ReturnsErrInvalidCredentials(t *testing.T) {
	svc := newService(newFakeStore())
	_, _ = svc.Register(context.Background(), "a@x.io", "password123", "Ann", "")

	_, err := svc.Login(context.Background(), "a@x.io", "wrong-password", "")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
}

func TestLogin_UnknownEmail_ReturnsSameError(t *testing.T) {
	svc := newService(newFakeStore())

	_, err := svc.Login(context.Background(), "nobody@x.io", "password123", "")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials (no user enumeration)", err)
	}
}

func loggedIn(t *testing.T, svc *auth.Service) auth.Result {
	t.Helper()
	if _, err := svc.Register(context.Background(), "a@x.io", "password123", "Ann", ""); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Login(context.Background(), "a@x.io", "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestRefresh_Rotates_OldTokenRejected(t *testing.T) {
	svc := newService(newFakeStore())
	first := loggedIn(t, svc)

	next, err := svc.Refresh(context.Background(), first.RefreshToken, "ua")
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if next.RefreshToken == first.RefreshToken || next.AccessToken == "" {
		t.Error("refresh must issue a new pair")
	}
	if id, err := auth.ParseAccessToken(secret, next.AccessToken, time.Now()); err != nil || id != first.User.ID {
		t.Errorf("new access token is for %v (err %v), want %v", id, err, first.User.ID)
	}

	if _, err := svc.Refresh(context.Background(), next.RefreshToken, ""); err != nil {
		t.Errorf("new token must be usable: %v", err)
	}
	if _, err := svc.Refresh(context.Background(), first.RefreshToken, ""); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("old token: err = %v, want ErrInvalidToken", err)
	}
}

func TestRefresh_ReuseOfRotatedToken_RevokesWholeSession(t *testing.T) {
	svc := newService(newFakeStore())
	first := loggedIn(t, svc)
	next, _ := svc.Refresh(context.Background(), first.RefreshToken, "")

	// Кто-то предъявляет уже использованный токен: считаем его украденным.
	_, _ = svc.Refresh(context.Background(), first.RefreshToken, "")

	if _, err := svc.Refresh(context.Background(), next.RefreshToken, ""); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("legitimate newer token must also be revoked, err = %v", err)
	}
}

func TestRefresh_UnknownToken_ReturnsErrInvalidToken(t *testing.T) {
	svc := newService(newFakeStore())

	for _, tok := range []string{"", "garbage", "AAAA"} {
		if _, err := svc.Refresh(context.Background(), tok, ""); !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("token %q: err = %v, want ErrInvalidToken", tok, err)
		}
	}
}

func TestRefresh_Expired_ReturnsErrInvalidToken(t *testing.T) {
	store := newFakeStore()
	now := time.Now()
	svc := auth.NewService(store, secret, auth.WithBcryptCost(bcrypt.MinCost), auth.WithClock(func() time.Time { return now }))
	res := loggedIn(t, svc)

	now = now.Add(auth.RefreshTokenTTL + time.Minute)

	if _, err := svc.Refresh(context.Background(), res.RefreshToken, ""); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestLogout_RevokesToken(t *testing.T) {
	svc := newService(newFakeStore())
	res := loggedIn(t, svc)

	if err := svc.Logout(context.Background(), res.RefreshToken); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Refresh(context.Background(), res.RefreshToken, ""); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("after logout: err = %v, want ErrInvalidToken", err)
	}
}

func TestLogout_UnknownOrEmptyToken_IsNoError(t *testing.T) {
	svc := newService(newFakeStore())

	for _, tok := range []string{"", "never-issued"} {
		if err := svc.Logout(context.Background(), tok); err != nil {
			t.Errorf("token %q: %v", tok, err)
		}
	}
}
