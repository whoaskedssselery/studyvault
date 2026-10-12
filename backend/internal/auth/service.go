package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
)

var (
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
)

const pgUniqueViolation = "23505"

type Store interface {
	CreateUser(ctx context.Context, arg db.CreateUserParams) (db.User, error)
	GetUserByEmail(ctx context.Context, email string) (db.User, error)
	CreateRefreshToken(ctx context.Context, arg db.CreateRefreshTokenParams) error
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
	return s.issue(ctx, user, userAgent)
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
	return s.issue(ctx, user, userAgent)
}

func (s *Service) issue(ctx context.Context, user db.User, userAgent string) (Result, error) {
	now := s.now()
	access, err := NewAccessToken(s.secret, user.ID, now)
	if err != nil {
		return Result{}, fmt.Errorf("sign access token: %w", err)
	}
	raw, hash, err := NewRefreshToken()
	if err != nil {
		return Result{}, fmt.Errorf("generate refresh token: %w", err)
	}
	err = s.store.CreateRefreshToken(ctx, db.CreateRefreshTokenParams{
		UserID:    user.ID,
		TokenHash: hash,
		UserAgent: pgtype.Text{String: userAgent, Valid: userAgent != ""},
		ExpiresAt: now.Add(RefreshTokenTTL),
	})
	if err != nil {
		return Result{}, fmt.Errorf("store refresh token: %w", err)
	}
	return Result{User: user, AccessToken: access, RefreshToken: raw}, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
