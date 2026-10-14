package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

func (f *fakeStore) DeleteExpiredRefreshTokens(_ context.Context, before time.Time) (int64, error) {
	var n int64
	for h, t := range f.tokens {
		if !t.ExpiresAt.After(before) {
			delete(f.tokens, h)
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) DeleteUserRefreshTokens(_ context.Context, userID uuid.UUID) error {
	for h, t := range f.tokens {
		if t.UserID == userID {
			delete(f.tokens, h)
		}
	}
	return nil
}

func (f *fakeStore) GetUserByID(_ context.Context, id uuid.UUID) (db.User, error) {
	for _, u := range f.users {
		if u.ID == id {
			return u, nil
		}
	}
	return db.User{}, pgx.ErrNoRows
}

func (f *fakeStore) UpdateUserProfile(_ context.Context, a db.UpdateUserProfileParams) (db.User, error) {
	for k, u := range f.users {
		if u.ID == a.ID {
			if a.DisplayName.Valid {
				u.DisplayName = a.DisplayName.String
			}
			if a.SetFaculty {
				u.Faculty = a.Faculty
			}
			f.users[k] = u
			return u, nil
		}
	}
	return db.User{}, pgx.ErrNoRows
}

func (f *fakeStore) SetUserRoleByEmail(_ context.Context, a db.SetUserRoleByEmailParams) (int64, error) {
	u, ok := f.users[lower(a.Email)]
	if !ok {
		return 0, nil
	}
	u.Role = a.Role
	f.users[lower(a.Email)] = u
	return 1, nil
}

func (f *fakeStore) UpdateUserPassword(_ context.Context, a db.UpdateUserPasswordParams) (int64, error) {
	for k, u := range f.users {
		if u.ID == a.ID {
			u.PasswordHash = a.PasswordHash
			f.users[k] = u
			return 1, nil
		}
	}
	return 0, nil
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

func TestLogin_UserAgentIsMadeSafeForStorage(t *testing.T) {
	cases := map[string]struct {
		ua      string
		wantNil bool
	}{
		"invalid utf-8":      {ua: "bad\xff\xfeagent"},
		"only invalid bytes": {ua: "\xff\xfe", wantNil: true},
		"cut in a multibyte": {ua: strings.Repeat("a", 254) + "я"}, // «я» занимает 2 байта и режется пополам на границе 255
		"very long":          {ua: strings.Repeat("Mozilla/5.0 ", 200)},
		"long and multibyte": {ua: strings.Repeat("я", 400)},
		"empty":              {ua: "", wantNil: true},
		"control characters": {ua: "evil\x00agent\r\n"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			store := newFakeStore()
			svc := newService(store)
			_, _ = svc.Register(context.Background(), "a@x.io", "password123", "Ann", "")
			store.refresh = nil

			if _, err := svc.Login(context.Background(), "a@x.io", "password123", c.ua); err != nil {
				t.Fatalf("a strange User-Agent must never fail the login: %v", err)
			}

			got := store.refresh[0].UserAgent
			if c.wantNil {
				if got.Valid {
					t.Errorf("nothing usable left, want NULL, got %q", got.String)
				}
				return
			}
			if !got.Valid || !utf8.ValidString(got.String) || len(got.String) > 255 {
				t.Errorf("stored %d bytes, valid utf-8: %v", len(got.String), utf8.ValidString(got.String))
			}
			if strings.ContainsAny(got.String, "\x00\r\n") {
				t.Errorf("control characters must be stripped, got %q", got.String)
			}
		})
	}
}

func TestRegister_StrangeUserAgent_DoesNotLeaveAHalfCreatedAccount(t *testing.T) {
	store := newFakeStore()

	res, err := newService(store).Register(context.Background(), "a@x.io", "password123", "Ann", "bad\xff\xfeagent")

	if err != nil || res.AccessToken == "" {
		t.Fatalf("register must succeed with a strange User-Agent: %v", err)
	}
}

func TestPurgeExpiredTokens_RemovesOnlyExpired(t *testing.T) {
	store := newFakeStore()
	now := time.Now()
	svc := auth.NewService(store, secret, auth.WithBcryptCost(bcrypt.MinCost), auth.WithClock(func() time.Time { return now }))
	old := loggedIn(t, svc) // регистрация и вход выдали по токену, оба истекут через RefreshTokenTTL
	now = now.Add(time.Hour)
	fresh, err := svc.Login(context.Background(), "a@x.io", "password123", "") // истечёт на час позже
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(auth.RefreshTokenTTL - 30*time.Minute) // старые уже истекли, свежий ещё жив

	n, err := svc.PurgeExpiredTokens(context.Background())

	if err != nil || n != 2 {
		t.Fatalf("purged %d (err %v), want the two expired tokens", n, err)
	}
	if _, ok := store.tokens[auth.HashRefreshToken(old.RefreshToken)]; ok {
		t.Error("the expired token must be gone")
	}
	if _, ok := store.tokens[auth.HashRefreshToken(fresh.RefreshToken)]; !ok {
		t.Error("a live token must stay")
	}
}

type failingPurgeStore struct{ *fakeStore }

func (failingPurgeStore) DeleteExpiredRefreshTokens(context.Context, time.Time) (int64, error) {
	return 0, errors.New("db down")
}

func TestPurgeExpiredTokens_StoreError_IsReturned(t *testing.T) {
	svc := newService(failingPurgeStore{newFakeStore()})
	if _, err := svc.PurgeExpiredTokens(context.Background()); err == nil {
		t.Fatal("a failing store must surface as an error")
	}
}

type countingPurgeStore struct {
	*fakeStore
	calls chan struct{}
}

func (c countingPurgeStore) DeleteExpiredRefreshTokens(context.Context, time.Time) (int64, error) {
	c.calls <- struct{}{}
	return 0, nil
}

func TestRunTokenPurge_RunsAtStartAndOnEveryTick_ThenStopsWithItsContext(t *testing.T) {
	calls := make(chan struct{}, 100)
	svc := newService(countingPurgeStore{newFakeStore(), calls})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.RunTokenPurge(ctx, 5*time.Millisecond); close(done) }()

	for i := 0; i < 3; i++ { // первый проход сразу, остальные по таймеру
		select {
		case <-calls:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d purge runs happened", i)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunTokenPurge must return once its context is cancelled")
	}
}

func TestGetUser_ReturnsTheAccount_OrErrUserNotFound(t *testing.T) {
	svc := newService(newFakeStore())
	res := loggedIn(t, svc)

	u, err := svc.GetUser(context.Background(), res.User.ID)
	if err != nil || u.Email != "a@x.io" {
		t.Fatalf("GetUser = %+v, %v", u, err)
	}
	if _, err := svc.GetUser(context.Background(), uuid.New()); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("unknown id: err = %v, want ErrUserNotFound", err)
	}
}

func TestUpdateProfile_NameFacultyAndClearing(t *testing.T) {
	ctx := context.Background()
	svc := newService(newFakeStore())
	res := loggedIn(t, svc)
	anna, it := "Anna", "Information Technology"

	u, err := svc.UpdateProfile(ctx, res.User.ID, auth.ProfilePatch{DisplayName: &anna, SetFaculty: true, Faculty: &it})
	if err != nil || u.DisplayName != "Anna" || u.Faculty.String != it || !u.Faculty.Valid {
		t.Fatalf("UpdateProfile = %+v, %v", u, err)
	}

	// Имя не передано: остаётся прежним. Факультет передан как null: очищается.
	u, err = svc.UpdateProfile(ctx, res.User.ID, auth.ProfilePatch{SetFaculty: true})
	if err != nil || u.DisplayName != "Anna" || u.Faculty.Valid {
		t.Fatalf("partial update = %+v, %v", u, err)
	}

	if _, err := svc.UpdateProfile(ctx, uuid.New(), auth.ProfilePatch{DisplayName: &anna}); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("unknown id: err = %v, want ErrUserNotFound", err)
	}
}

func TestSetRole_OnlyKnownRoles_OnlyExistingUsers(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	svc := newService(store)
	res := loggedIn(t, svc)

	if err := svc.SetRole(ctx, "A@X.io", auth.RoleModerator); err != nil { // регистр почты не важен
		t.Fatal(err)
	}
	if got, _ := svc.GetUser(ctx, res.User.ID); got.Role != auth.RoleModerator {
		t.Errorf("role = %q, want moderator", got.Role)
	}
	if err := svc.SetRole(ctx, "nobody@x.io", auth.RoleModerator); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("unknown email: err = %v, want ErrUserNotFound", err)
	}
	if err := svc.SetRole(ctx, "a@x.io", "admin"); err == nil {
		t.Error("an unknown role must be rejected")
	}
}

func TestChangePassword_NewPasswordWorks_OldDoesNot_AllOldSessionsDie(t *testing.T) {
	ctx := context.Background()
	svc := newService(newFakeStore())
	phone := loggedIn(t, svc)
	laptop, err := svc.Login(ctx, "a@x.io", "password123", "laptop")
	if err != nil {
		t.Fatal(err)
	}

	tokens, err := svc.ChangePassword(ctx, phone.User.ID, "password123", "brand-new-pass", "phone")

	if err != nil || tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("ChangePassword = %+v, %v", tokens, err)
	}
	if _, err := svc.Login(ctx, "a@x.io", "password123", ""); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("old password: err = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Login(ctx, "a@x.io", "brand-new-pass", ""); err != nil {
		t.Errorf("new password must work: %v", err)
	}
	for name, old := range map[string]string{"phone": phone.RefreshToken, "laptop": laptop.RefreshToken} {
		if _, err := svc.Refresh(ctx, old, ""); !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("%s session survived the password change: err = %v", name, err)
		}
	}
	if _, err := svc.Refresh(ctx, tokens.RefreshToken, ""); err != nil {
		t.Errorf("the session that changed the password must stay signed in: %v", err)
	}
}

func TestChangePassword_WrongCurrentPassword_ChangesNothing(t *testing.T) {
	ctx := context.Background()
	svc := newService(newFakeStore())
	res := loggedIn(t, svc)

	if _, err := svc.ChangePassword(ctx, res.User.ID, "not-my-password", "brand-new-pass", ""); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("err = %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Login(ctx, "a@x.io", "password123", ""); err != nil {
		t.Errorf("the old password must still work: %v", err)
	}
	if _, err := svc.Refresh(ctx, res.RefreshToken, ""); err != nil {
		t.Errorf("a failed change must not end the session: %v", err)
	}
}

func TestChangePassword_UnknownUser(t *testing.T) {
	svc := newService(newFakeStore())
	if _, err := svc.ChangePassword(context.Background(), uuid.New(), "a", "b", ""); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("err = %v, want ErrUserNotFound", err)
	}
}
