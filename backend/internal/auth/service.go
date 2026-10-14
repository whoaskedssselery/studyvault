package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
)

var (
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrInvalidToken       = errors.New("invalid or expired refresh token")
	ErrUserNotFound       = errors.New("user not found")
)

const pgUniqueViolation = "23505"

type Store interface {
	CreateUser(ctx context.Context, arg db.CreateUserParams) (db.User, error)
	GetUserByEmail(ctx context.Context, email string) (db.User, error)
	CreateRefreshToken(ctx context.Context, arg db.CreateRefreshTokenParams) error
	GetRefreshTokenByHash(ctx context.Context, tokenHash string) (db.RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenHash string) (int64, error)
	RevokeAllUserRefreshTokens(ctx context.Context, userID uuid.UUID) error
	DeleteExpiredRefreshTokens(ctx context.Context, expiresAt time.Time) (int64, error)
	DeleteUser(ctx context.Context, id uuid.UUID) (int64, error)
	CreatePasswordResetToken(ctx context.Context, arg db.CreatePasswordResetTokenParams) error
	DeleteUserPasswordResetTokens(ctx context.Context, userID uuid.UUID) error
	ClaimPasswordResetToken(ctx context.Context, arg db.ClaimPasswordResetTokenParams) (uuid.UUID, error)
	DeleteExpiredPasswordResetTokens(ctx context.Context, expiresAt time.Time) (int64, error)
	DeleteUserRefreshTokens(ctx context.Context, userID uuid.UUID) error
	GetUserByID(ctx context.Context, id uuid.UUID) (db.User, error)
	UpdateUserProfile(ctx context.Context, arg db.UpdateUserProfileParams) (db.User, error)
	SetUserRoleByEmail(ctx context.Context, arg db.SetUserRoleByEmailParams) (int64, error)
	UpdateUserPassword(ctx context.Context, arg db.UpdateUserPasswordParams) (int64, error)
}

// Tokens — пара токенов, которую получает клиент.
type Tokens struct {
	AccessToken  string
	RefreshToken string
}

type Result struct {
	User         db.User
	AccessToken  string
	RefreshToken string
}

type Service struct {
	store     Store
	secret    []byte
	cost      int
	now       func() time.Time
	dummyHash []byte // для выравнивания времени ответа, если пользователя нет
	mailer    Mailer // nil: сброс пароля по почте выключен
	resetURL  string
}

type Option func(*Service)

func WithBcryptCost(cost int) Option { return func(s *Service) { s.cost = cost } }

func WithClock(now func() time.Time) Option { return func(s *Service) { s.now = now } }

func NewService(store Store, secret []byte, opts ...Option) *Service {
	s := &Service{store: store, secret: secret, cost: 12, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	s.dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password"), s.cost)
	return s
}

func (s *Service) Register(ctx context.Context, email, password, displayName, userAgent string) (Result, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.cost)
	if err != nil {
		return Result{}, fmt.Errorf("hash password: %w", err)
	}
	user, err := s.store.CreateUser(ctx, db.CreateUserParams{
		Email:        normalizeEmail(email),
		PasswordHash: string(hash),
		DisplayName:  displayName,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
			return Result{}, ErrEmailTaken
		}
		return Result{}, fmt.Errorf("create user: %w", err)
	}
	return s.startSession(ctx, user, userAgent)
}

func (s *Service) Login(ctx context.Context, email, password, userAgent string) (Result, error) {
	user, err := s.store.GetUserByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, pgx.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return Result{}, ErrInvalidCredentials
	}
	if err != nil {
		return Result{}, fmt.Errorf("get user: %w", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return Result{}, ErrInvalidCredentials
	}
	return s.startSession(ctx, user, userAgent)
}

// Refresh меняет действующий refresh-токен на новую пару (ротация).
// Предъявление уже использованного токена считается кражей: отзываются все токены пользователя.
func (s *Service) Refresh(ctx context.Context, rawToken, userAgent string) (Tokens, error) {
	if rawToken == "" {
		return Tokens{}, ErrInvalidToken
	}
	hash := HashRefreshToken(rawToken)

	stored, err := s.store.GetRefreshTokenByHash(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tokens{}, ErrInvalidToken
	}
	if err != nil {
		return Tokens{}, fmt.Errorf("get refresh token: %w", err)
	}

	if stored.RevokedAt != nil {
		if err := s.store.RevokeAllUserRefreshTokens(ctx, stored.UserID); err != nil {
			return Tokens{}, fmt.Errorf("revoke session after token reuse: %w", err)
		}
		return Tokens{}, ErrInvalidToken
	}
	if !stored.ExpiresAt.After(s.now()) {
		return Tokens{}, ErrInvalidToken
	}

	// Из двух параллельных запросов с одним токеном отзовёт только один, второй получит 0 строк.
	n, err := s.store.RevokeRefreshToken(ctx, hash)
	if err != nil {
		return Tokens{}, fmt.Errorf("revoke refresh token: %w", err)
	}
	if n == 0 {
		return Tokens{}, ErrInvalidToken
	}
	return s.issue(ctx, stored.UserID, userAgent)
}

// Logout отзывает токен. Неизвестный или пустой токен не ошибка: результат тот же, клиент разлогинен.
func (s *Service) Logout(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	if _, err := s.store.RevokeRefreshToken(ctx, HashRefreshToken(rawToken)); err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	return nil
}

func (s *Service) startSession(ctx context.Context, user db.User, userAgent string) (Result, error) {
	t, err := s.issue(ctx, user.ID, userAgent)
	if err != nil {
		return Result{}, err
	}
	return Result{User: user, AccessToken: t.AccessToken, RefreshToken: t.RefreshToken}, nil
}

func (s *Service) issue(ctx context.Context, userID uuid.UUID, userAgent string) (Tokens, error) {
	now := s.now()
	ua := sanitizeUserAgent(userAgent)
	access, err := NewAccessToken(s.secret, userID, now)
	if err != nil {
		return Tokens{}, fmt.Errorf("sign access token: %w", err)
	}
	raw, hash, err := NewRefreshToken()
	if err != nil {
		return Tokens{}, fmt.Errorf("generate refresh token: %w", err)
	}
	err = s.store.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
		UserID:    userID,
		TokenHash: hash,
		UserAgent: pgtype.Text{String: ua, Valid: ua != ""},
		ExpiresAt: now.Add(RefreshTokenTTL),
	})
	if err != nil {
		return Tokens{}, fmt.Errorf("store refresh token: %w", err)
	}
	return Tokens{AccessToken: access, RefreshToken: raw}, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

const maxUserAgentBytes = 255

// sanitizeUserAgent делает заголовок безопасным для текстовой колонки: User-Agent присылает клиент и
// может содержать что угодно, а невалидный UTF-8 Postgres отвергает, и вход падал бы с ошибкой 500.
// Битые байты и управляющие символы убираются, длина ограничивается по границе символа.
func sanitizeUserAgent(ua string) string {
	ua = strings.ToValidUTF8(ua, "")
	ua = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ua)
	if len(ua) > maxUserAgentBytes {
		ua = ua[:maxUserAgentBytes]
		for !utf8.ValidString(ua) { // срез мог попасть внутрь многобайтового символа
			ua = ua[:len(ua)-1]
		}
	}
	return strings.TrimSpace(ua)
}

// PurgeExpiredTokens удаляет истёкшие refresh-токены и ссылки сброса пароля (истёкшие и уже использованные) и возвращает их число. Без этого таблица росла бы вечно:
// каждый вход и каждое обновление добавляют строку. Отозванные, но ещё не истёкшие токены остаются,
// потому что по ним распознаётся повторное предъявление уже использованного токена.
func (s *Service) PurgeExpiredTokens(ctx context.Context) (int64, error) {
	n, err := s.store.DeleteExpiredRefreshTokens(ctx, s.now())
	if err != nil {
		return 0, fmt.Errorf("purge expired refresh tokens: %w", err)
	}
	m, err := s.store.DeleteExpiredPasswordResetTokens(ctx, s.now())
	if err != nil {
		return n, fmt.Errorf("purge expired password reset tokens: %w", err)
	}
	return n + m, nil
}

// RunTokenPurge чистит истёкшие токены сразу и затем каждые every, пока не отменён ctx. Ошибка очистки не
// фатальна (следующий проход повторит), её видно только в логе.
func (s *Service) RunTokenPurge(ctx context.Context, every time.Duration) {
	purge := func() {
		n, err := s.PurgeExpiredTokens(ctx)
		switch {
		case ctx.Err() != nil:
		case err != nil:
			slog.ErrorContext(ctx, "purge refresh tokens", "err", err)
		case n > 0:
			slog.InfoContext(ctx, "purged expired refresh tokens", "count", n)
		}
	}
	purge()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			purge()
		}
	}
}

// GetUser возвращает аккаунт по id из access-токена. ErrUserNotFound значит, что аккаунта уже нет.
func (s *Service) GetUser(ctx context.Context, id uuid.UUID) (db.User, error) {
	u, err := s.store.GetUserByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, ErrUserNotFound
	}
	if err != nil {
		return db.User{}, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

// ProfilePatch — частичное изменение профиля. nil в DisplayName значит «не менять»; факультет можно очистить,
// поэтому «передан ли он» хранится отдельным флагом.
type ProfilePatch struct {
	DisplayName *string
	SetFaculty  bool
	Faculty     *string
}

func (s *Service) UpdateProfile(ctx context.Context, id uuid.UUID, p ProfilePatch) (db.User, error) {
	arg := db.UpdateUserProfileParams{ID: id, SetFaculty: p.SetFaculty}
	if p.DisplayName != nil {
		arg.DisplayName = pgtype.Text{String: *p.DisplayName, Valid: true}
	}
	if p.Faculty != nil {
		arg.Faculty = pgtype.Text{String: *p.Faculty, Valid: true}
	}
	u, err := s.store.UpdateUserProfile(ctx, arg)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.User{}, ErrUserNotFound
	}
	if err != nil {
		return db.User{}, fmt.Errorf("update profile: %w", err)
	}
	return u, nil
}

// Роли пользователя. Назначить роль можно только доверенной операцией на сервере (SetRole, cmd/promote):
// через HTTP API этого нет.
const (
	RoleStudent   = "student"
	RoleModerator = "moderator"
)

// SetRole назначает роль пользователю с этой почтой. ErrUserNotFound: такой почты нет.
func (s *Service) SetRole(ctx context.Context, email, role string) error {
	if role != RoleStudent && role != RoleModerator {
		return fmt.Errorf("unknown role %q", role)
	}
	n, err := s.store.SetUserRoleByEmail(ctx, db.SetUserRoleByEmailParams{Email: email, Role: role})
	if err != nil {
		return fmt.Errorf("set role: %w", err)
	}
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ChangePassword проверяет текущий пароль, сохраняет новый и стирает все сессии пользователя, включая
// сессии на других устройствах: если пароль утёк, они не должны пережить его смену. Возвращает новую пару
// токенов, чтобы текущий клиент остался в системе. Сессии стираются до записи пароля: при сбое посередине
// пользователь лишь войдёт заново, а не останется со старыми сессиями и новым паролем.
func (s *Service) ChangePassword(ctx context.Context, id uuid.UUID, current, next, userAgent string) (Tokens, error) {
	u, err := s.GetUser(ctx, id)
	if err != nil {
		return Tokens{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) != nil {
		return Tokens{}, ErrInvalidCredentials
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), s.cost)
	if err != nil {
		return Tokens{}, fmt.Errorf("hash password: %w", err)
	}
	if err := s.store.DeleteUserRefreshTokens(ctx, id); err != nil {
		return Tokens{}, fmt.Errorf("end sessions: %w", err)
	}
	n, err := s.store.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{ID: id, PasswordHash: string(hash)})
	if err != nil {
		return Tokens{}, fmt.Errorf("update password: %w", err)
	}
	if n == 0 {
		return Tokens{}, ErrUserNotFound
	}
	return s.issue(ctx, id, userAgent)
}
