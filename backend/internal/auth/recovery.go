package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
)

// PasswordResetTTL — сколько живёт ссылка для сброса пароля.
const PasswordResetTTL = time.Hour

// ErrResetUnavailable: почтовый сервер не настроен, письма с ссылкой отправить нечем. Это не зависит от того,
// есть ли аккаунт с такой почтой, поэтому ответ ничего не раскрывает.
var ErrResetUnavailable = errors.New("password reset is not available: no mail server is configured")

// Mailer отправляет письма. В продакшене это SMTP (пакет mail), в тестах подставка.
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

// WithMailer включает сброс пароля по почте. resetURL — адрес страницы фронтенда, куда ведёт ссылка из письма;
// токен добавляется к нему параметром token.
func WithMailer(m Mailer, resetURL string) Option {
	return func(s *Service) { s.mailer, s.resetURL = m, resetURL }
}

// RequestPasswordReset отправляет на почту ссылку для сброса пароля, если такой аккаунт есть. Для неизвестной почты
// ничего не происходит, а ответ и время ответа те же: письмо уходит в фоне, поэтому по скорости ответа
// существование аккаунта не угадать. Новая ссылка гасит прежние. Сбой отправки только логируется по той же причине.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	if s.mailer == nil {
		return ErrResetUnavailable
	}
	user, err := s.store.GetUserByEmail(ctx, normalizeEmail(email))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	raw, hash, err := NewRefreshToken()
	if err != nil {
		return fmt.Errorf("generate reset token: %w", err)
	}
	if err := s.store.DeleteUserPasswordResetTokens(ctx, user.ID); err != nil {
		return fmt.Errorf("drop old reset tokens: %w", err)
	}
	err = s.store.CreatePasswordResetToken(ctx, db.CreatePasswordResetTokenParams{
		UserID: user.ID, TokenHash: hash, ExpiresAt: s.now().Add(PasswordResetTTL),
	})
	if err != nil {
		return fmt.Errorf("store reset token: %w", err)
	}

	link, err := resetLink(s.resetURL, raw)
	if err != nil {
		return fmt.Errorf("build reset link: %w", err)
	}
	to, name := user.Email, user.DisplayName
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		body := fmt.Sprintf("Здравствуйте, %s!\n\nВы запросили сброс пароля в StudyVault. Чтобы задать новый пароль, откройте ссылку. "+
			"Она действует 1 час и работает один раз:\n\n%s\n\nЕсли вы ничего не запрашивали, просто проигнорируйте это письмо: пароль не изменится.\n", name, link)
		if err := s.mailer.Send(ctx, to, "StudyVault: сброс пароля", body); err != nil {
			slog.Error("send the password reset mail", "err", err)
		}
	}()
	return nil
}

// ConfirmPasswordReset задаёт новый пароль по ссылке из письма и завершает все сессии пользователя: если пароль
// пытались угнать, старые сессии не должны пережить его смену. Токен «забирается» одним запросом, так что
// ссылка срабатывает один раз даже при одновременных запросах.
func (s *Service) ConfirmPasswordReset(ctx context.Context, rawToken, newPassword string) error {
	if rawToken == "" {
		return ErrInvalidToken
	}
	userID, err := s.store.ClaimPasswordResetToken(ctx, db.ClaimPasswordResetTokenParams{
		TokenHash: HashRefreshToken(rawToken), ExpiresAt: s.now(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidToken
	}
	if err != nil {
		return fmt.Errorf("claim reset token: %w", err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), s.cost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := s.store.DeleteUserRefreshTokens(ctx, userID); err != nil {
		return fmt.Errorf("end sessions: %w", err)
	}
	n, err := s.store.UpdateUserPassword(ctx, db.UpdateUserPasswordParams{ID: userID, PasswordHash: string(hash)})
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if n == 0 {
		return ErrUserNotFound
	}
	if err := s.store.DeleteUserPasswordResetTokens(ctx, userID); err != nil {
		slog.ErrorContext(ctx, "drop used reset tokens", "err", err) // уже использованные чистит и фоновая очистка
	}
	return nil
}

// DeleteAccount удаляет аккаунт после проверки пароля. Каскадом уходят заметки, предметы, теги, комментарии, оценки,
// избранное и сессии пользователя, а его опубликованные материалы исчезают из библиотеки. Решения модератора
// остаются в истории без указания, кто их принял.
func (s *Service) DeleteAccount(ctx context.Context, id uuid.UUID, password string) error {
	u, err := s.GetUser(ctx, id)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return ErrInvalidCredentials
	}
	n, err := s.store.DeleteUser(ctx, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// resetLink добавляет токен к адресу страницы сброса, сохраняя уже имеющиеся параметры адреса.
func resetLink(base, token string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("reset URL %q is not an http(s) address", base)
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
