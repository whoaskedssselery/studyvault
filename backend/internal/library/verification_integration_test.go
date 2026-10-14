package library_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/library"
)

// Без подтверждённой почты нельзя публиковать, комментировать и оценивать; личные заметки и избранное доступны.
func TestIntegration_UnverifiedAddress_CannotPublishCommentOrRate_ButCanKeepPrivateNotes(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	author, mod := e.user(t, "author"), e.moderator(t, "mod")
	fresh := e.unverified(t, "fresh")
	liveID := e.published(t, author, mod, "Live", "text")
	freshDraft := e.draft(t, fresh, "Fresh draft", "my notes") // личные заметки доступны всем
	comment, err := e.lib.AddComment(ctx, author, liveID, "mine")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := e.lib.Submit(ctx, fresh, freshDraft); !errors.Is(err, library.ErrEmailNotVerified) {
		t.Errorf("Submit: err = %v, want ErrEmailNotVerified", err)
	}
	if _, err := e.lib.AddComment(ctx, fresh, liveID, "hi"); !errors.Is(err, library.ErrEmailNotVerified) {
		t.Errorf("AddComment: err = %v, want ErrEmailNotVerified", err)
	}
	if _, err := e.lib.EditComment(ctx, fresh, comment.ID, "hi"); !errors.Is(err, library.ErrEmailNotVerified) {
		t.Errorf("EditComment: err = %v, want ErrEmailNotVerified", err)
	}
	if _, err := e.lib.Rate(ctx, fresh, liveID, 5); !errors.Is(err, library.ErrEmailNotVerified) {
		t.Errorf("Rate: err = %v, want ErrEmailNotVerified", err)
	}
	if err := e.lib.AddBookmark(ctx, fresh, liveID); err != nil {
		t.Errorf("a bookmark is private to the reader, it must work: %v", err)
	}
	if _, err := e.lib.Get(ctx, &fresh, liveID); err != nil {
		t.Errorf("reading must work: %v", err)
	}

	// Ничего из запрещённого не осталось в базе.
	var comments, ratings int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM comments WHERE user_id = $1`, fresh).Scan(&comments)
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM note_ratings WHERE user_id = $1`, fresh).Scan(&ratings)
	var status string
	_ = e.tx.QueryRow(ctx, `SELECT status FROM notes WHERE id = $1`, freshDraft).Scan(&status)
	if comments != 0 || ratings != 0 || status != "draft" {
		t.Errorf("comments=%d ratings=%d status=%q: a refused action must leave nothing behind", comments, ratings, status)
	}
}

func TestIntegration_AMissingAccount_IsForbiddenNotAnError(t *testing.T) {
	e := newEnv(t)
	ghost := uuid.New()
	if _, err := e.lib.Submit(context.Background(), ghost, uuid.New()); !errors.Is(err, library.ErrForbidden) {
		t.Errorf("err = %v, want ErrForbidden", err)
	}
}

// Весь путь на настоящей базе: регистрация с письмом, ссылка, подтверждение, и только потом публикация.
func TestIntegration_VerifyByMail_UnlocksPublishing(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	mailer := &captureMailer{bodies: make(chan string, 4)}
	svc := auth.NewService(e.q, []byte("0123456789abcdef0123456789abcdef"), auth.WithBcryptCost(bcrypt.MinCost),
		auth.WithMailer(mailer, "https://app.example.com/reset"), auth.WithEmailVerification("https://app.example.com/verify-email"))
	email := "verify-" + uuid.NewString() + "@x.io"

	reg, err := svc.Register(ctx, email, "password123", "Newcomer", "")
	if err != nil {
		t.Fatal(err)
	}
	if reg.User.EmailVerifiedAt != nil {
		t.Fatal("a new address must be unverified")
	}
	var body string
	select {
	case body = <-mailer.bodies:
	case <-time.After(3 * time.Second):
		t.Fatal("no mail")
	}
	token := strings.Fields(body[strings.Index(body, "token=")+len("token="):])[0]
	draft := e.draft(t, reg.User.ID, "Newcomer notes", "text")
	if _, err := e.lib.Submit(ctx, reg.User.ID, draft); !errors.Is(err, library.ErrEmailNotVerified) {
		t.Fatalf("before the link: err = %v, want ErrEmailNotVerified", err)
	}

	// В базе только хеш ссылки.
	var hashed, raw int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM email_verification_tokens WHERE token_hash = $1`, auth.HashRefreshToken(token)).Scan(&hashed)
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM email_verification_tokens WHERE token_hash = $1`, token).Scan(&raw)
	if hashed != 1 || raw != 0 {
		t.Fatalf("only the hash may be stored: hash rows %d, raw rows %d", hashed, raw)
	}

	if err := svc.ConfirmEmail(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := e.lib.Submit(ctx, reg.User.ID, draft); err != nil {
		t.Errorf("after the link: %v", err)
	}
	if err := svc.ConfirmEmail(ctx, token); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("a link works once: %v", err)
	}
}

// Ссылка срабатывает один раз и истекает в самом запросе, а не только за счёт того, что сервис чистит за собой.
func TestIntegration_VerificationLink_IsSingleUseAndExpires_InSQL(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.unverified(t, "u")
	const hash = "hash-of-a-link"
	if _, err := e.tx.Exec(ctx, `INSERT INTO email_verification_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, now() + interval '1 hour')`, uid, hash); err != nil {
		t.Fatal(err)
	}
	claim := func() error {
		_, err := e.q.ClaimEmailVerificationToken(ctx, db.ClaimEmailVerificationTokenParams{TokenHash: hash, ExpiresAt: time.Now()})
		return err
	}
	if err := claim(); err != nil {
		t.Fatalf("the first claim: %v", err)
	}
	if err := claim(); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("the second claim: err = %v, want no rows", err)
	}

	// Просроченная ссылка не забирается, даже если её никто не использовал.
	if _, err := e.tx.Exec(ctx, `UPDATE email_verification_tokens SET used_at = NULL, expires_at = now() - interval '1 second' WHERE token_hash = $1`, hash); err != nil {
		t.Fatal(err)
	}
	if err := claim(); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("an expired link: err = %v, want no rows", err)
	}
}

func TestIntegration_DeletingAnAccount_TakesItsVerificationLinks(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	uid := e.unverified(t, "gone")
	if _, err := e.tx.Exec(ctx, `INSERT INTO email_verification_tokens (user_id, token_hash, expires_at) VALUES ($1, 'h', now() + interval '1 hour')`, uid); err != nil {
		t.Fatal(err)
	}

	if _, err := e.q.DeleteUser(ctx, uid); err != nil {
		t.Fatal(err)
	}

	var n int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM email_verification_tokens WHERE user_id = $1`, uid).Scan(&n)
	if n != 0 {
		t.Errorf("%d links outlived the account", n)
	}
}
