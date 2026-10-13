package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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
)

const pgUniqueViolation = "23505"

type Store interface {
	CreateUser(ctx context.Context, arg db.CreateUserParams) (db.User, error)
	GetUserByEmail(ctx context.Context, email string) (db.User, error)
	CreateRefreshToken(ctx context.Context, arg db.CreateRefreshTokenParams) error
	GetRefreshTokenByHash(ctx context.Context, tokenHash string) (db.RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenHash string) (int64, error)
	RevokeAllUserRefreshTokens(ctx context.Context, userID uuid.UUID) error
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
		UserAgent: pgtype.Text{String: userAgent, Valid: userAgent != ""},
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
