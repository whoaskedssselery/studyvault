package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
)

// EmailVerificationTTL — сколько живёт ссылка подтверждения почты. EmailVerificationCooldown — как часто на один аккаунт
// можно слать письмо с ней.
const (
	EmailVerificationTTL      = 24 * time.Hour
	EmailVerificationCooldown = time.Minute
)

// ErrVerificationUnavailable: почтовый сервер не настроен, письмо отправить нечем. В таком режиме регистрация сразу
// считает почту подтверждённой (см. Register), так что запрашивать письмо нужно только при настроенной почте.
var ErrVerificationUnavailable = errors.New("e-mail verification is not available: no mail server is configured")

// TooSoonError: письмо с ссылкой уже отправлено недавно. RetryAfter — через сколько можно просить новое.
type TooSoonError struct{ RetryAfter time.Duration }

func (e *TooSoonError) Error() string {
	return fmt.Sprintf("a verification mail was sent recently, try again in %s", e.RetryAfter.Round(time.Second))
}

// WithEmailVerification включает подтверждение почты при регистрации. verifyURL — адрес страницы фронтенда, куда ведёт
// ссылка из письма; токен добавляется к нему параметром token. Работает только вместе с WithMailer: без почтового
// сервера подтверждать нечем, и новые аккаунты сразу считаются подтверждёнными.
func WithEmailVerification(verifyURL string) Option {
	return func(s *Service) { s.verifyURL = verifyURL }
}

func (s *Service) verificationEnabled() bool { return s.mailer != nil && s.verifyURL != "" }

// RequestEmailVerification отправляет вошедшему пользователю новую ссылку подтверждения. Для уже подтверждённой почты
// ничего не делает. Раньше чем через минуту после прошлого письма отвечает *TooSoonError: иначе кнопку «отправить ещё раз»
// можно было бы использовать, чтобы заваливать письмами чужой ящик, если аккаунт заведён на чужой адрес.
func (s *Service) RequestEmailVerification(ctx context.Context, id uuid.UUID) error {
	if !s.verificationEnabled() {
		return ErrVerificationUnavailable
	}
	user, err := s.GetUser(ctx, id)
	if err != nil {
		return err
	}
	if user.EmailVerifiedAt != nil {
		return nil
	}
	latest, err := s.store.LatestEmailVerificationExpiry(ctx, id)
	if err != nil {
		return fmt.Errorf("check the last verification link: %w", err)
	}
	if wait := latest.Sub(s.now().Add(EmailVerificationTTL - EmailVerificationCooldown)); wait > 0 {
		return &TooSoonError{RetryAfter: wait}
	}
	return s.sendVerification(ctx, user)
}

// sendVerification выпускает ссылку (прежние гасит) и отправляет письмо в фоне: сбой почты не должен ломать регистрацию
// и не задерживает ответ.
func (s *Service) sendVerification(ctx context.Context, user db.User) error {
	raw, hash, err := NewRefreshToken()
	if err != nil {
		return fmt.Errorf("generate verification token: %w", err)
	}
	if err := s.store.DeleteUserEmailVerificationTokens(ctx, user.ID); err != nil {
		return fmt.Errorf("drop old verification tokens: %w", err)
	}
	err = s.store.CreateEmailVerificationToken(ctx, db.CreateEmailVerificationTokenParams{
		UserID: user.ID, TokenHash: hash, ExpiresAt: s.now().Add(EmailVerificationTTL),
	})
	if err != nil {
		return fmt.Errorf("store verification token: %w", err)
	}
	link, err := linkWithToken(s.verifyURL, raw)
	if err != nil {
		return fmt.Errorf("build verification link: %w", err)
	}
	to, name := user.Email, user.DisplayName
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		body := fmt.Sprintf("Здравствуйте, %s!\n\nВы зарегистрировались в StudyVault. Чтобы подтвердить почту и получить возможность "+
			"публиковать материалы, оставлять комментарии и ставить оценки, откройте ссылку. Она действует 24 часа:\n\n%s\n\n"+
			"Если вы не регистрировались, просто проигнорируйте это письмо.\n", name, link)
		if err := s.mailer.Send(ctx, to, "StudyVault: подтвердите почту", body); err != nil {
			slog.Error("send the verification mail", "err", err)
		}
	}()
	return nil
}

// ConfirmEmail подтверждает почту по ссылке из письма. Токен «забирается» одним запросом, так что ссылка срабатывает
// один раз даже при одновременных запросах. Открыть её можно и без входа в систему: письмо читают на другом устройстве.
func (s *Service) ConfirmEmail(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return ErrInvalidToken
	}
	userID, err := s.store.ClaimEmailVerificationToken(ctx, db.ClaimEmailVerificationTokenParams{
		TokenHash: HashRefreshToken(rawToken), ExpiresAt: s.now(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidToken
	}
	if err != nil {
		return fmt.Errorf("claim verification token: %w", err)
	}
	n, err := s.store.VerifyUserEmail(ctx, userID)
	if err != nil {
		return fmt.Errorf("verify email: %w", err)
	}
	if n == 0 {
		return ErrUserNotFound
	}
	if err := s.store.DeleteUserEmailVerificationTokens(ctx, userID); err != nil {
		slog.ErrorContext(ctx, "drop used verification tokens", "err", err) // уже использованные чистит и фоновая очистка
	}
	return nil
}
