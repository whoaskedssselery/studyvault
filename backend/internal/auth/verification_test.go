package auth_test

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
)

// ---------------------------------------------------------------------------
// Подтверждение почты. Методы хранилища-подставки для ссылок подтверждения лежат в этом же файле.
// ---------------------------------------------------------------------------

type verifyRow struct {
	userID  uuid.UUID
	expires time.Time
	used    bool
}

// Таблица ссылок подтверждения лежит в соседней карте: fakeStore из service_test.go знает только про сброс пароля.
var verifyRows = map[*fakeStore]map[string]*verifyRow{}

func (f *fakeStore) verifications() map[string]*verifyRow {
	if verifyRows[f] == nil {
		verifyRows[f] = map[string]*verifyRow{}
	}
	return verifyRows[f]
}

func (f *fakeStore) VerifyUserEmail(_ context.Context, id uuid.UUID) (int64, error) {
	for k, u := range f.users {
		if u.ID == id {
			if u.EmailVerifiedAt == nil {
				now := time.Now()
				u.EmailVerifiedAt = &now
				f.users[k] = u
			}
			return 1, nil
		}
	}
	return 0, nil
}

func (f *fakeStore) CreateEmailVerificationToken(_ context.Context, a db.CreateEmailVerificationTokenParams) error {
	f.verifications()[a.TokenHash] = &verifyRow{userID: a.UserID, expires: a.ExpiresAt}
	return nil
}

func (f *fakeStore) DeleteUserEmailVerificationTokens(_ context.Context, userID uuid.UUID) error {
	for h, r := range f.verifications() {
		if r.userID == userID {
			delete(f.verifications(), h)
		}
	}
	return nil
}

func (f *fakeStore) ClaimEmailVerificationToken(_ context.Context, a db.ClaimEmailVerificationTokenParams) (uuid.UUID, error) {
	r, ok := f.verifications()[a.TokenHash]
	if !ok || r.used || !r.expires.After(a.ExpiresAt) {
		return uuid.Nil, pgx.ErrNoRows
	}
	r.used = true
	return r.userID, nil
}

func (f *fakeStore) LatestEmailVerificationExpiry(_ context.Context, userID uuid.UUID) (time.Time, error) {
	latest := time.Unix(0, 0)
	for _, r := range f.verifications() {
		if r.userID == userID && !r.used && r.expires.After(latest) {
			latest = r.expires
		}
	}
	return latest, nil
}

func (f *fakeStore) DeleteExpiredEmailVerificationTokens(_ context.Context, now time.Time) (int64, error) {
	var n int64
	for h, r := range f.verifications() {
		if !r.expires.After(now) || r.used {
			delete(f.verifications(), h)
			n++
		}
	}
	return n, nil
}

func newVerifyingService(store auth.Store, mailer auth.Mailer, now func() time.Time) *auth.Service {
	return auth.NewService(store, secret, auth.WithBcryptCost(bcrypt.MinCost), auth.WithClock(now),
		auth.WithMailer(mailer, "https://app.example.com/reset-password"),
		auth.WithEmailVerification("https://app.example.com/verify-email?lang=ru"))
}

func TestRegister_WithMail_SendsALinkAndLeavesTheAddressUnverified(t *testing.T) {
	ctx := context.Background()
	store, mailer := newFakeStore(), newMailer()
	svc := newVerifyingService(store, mailer, time.Now)

	res, err := svc.Register(ctx, " A@X.io ", "password123", "Ann", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.User.EmailVerifiedAt != nil || store.users["a@x.io"].EmailVerifiedAt != nil {
		t.Fatal("a new address must not count as verified before the link is opened")
	}
	mail := mailer.next(t)
	if mail.to != "a@x.io" || mail.subject == "" {
		t.Errorf("mail = %+v", mail)
	}
	if !strings.Contains(mail.body, "https://app.example.com/verify-email?") || !strings.Contains(mail.body, "lang=ru") {
		t.Errorf("the link must lead to the configured page and keep its own parameters: %q", mail.body)
	}

	if err := svc.ConfirmEmail(ctx, tokenFrom(t, mail)); err != nil {
		t.Fatalf("ConfirmEmail: %v", err)
	}
	if got, _ := svc.GetUser(ctx, res.User.ID); got.EmailVerifiedAt == nil {
		t.Error("the address must be verified after the link is opened")
	}
}

func TestConfirmEmail_TheLinkWorksOnce(t *testing.T) {
	ctx := context.Background()
	mailer := newMailer()
	svc := newVerifyingService(newFakeStore(), mailer, time.Now)
	if _, err := svc.Register(ctx, "a@x.io", "password123", "Ann", ""); err != nil {
		t.Fatal(err)
	}
	token := tokenFrom(t, mailer.next(t))

	if err := svc.ConfirmEmail(ctx, token); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmEmail(ctx, token); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("the second use: err = %v, want ErrInvalidToken", err)
	}
}

func TestConfirmEmail_ExpiredUnknownAndEmptyTokens_AreRejected(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	mailer := newMailer()
	svc := newVerifyingService(newFakeStore(), mailer, func() time.Time { return now })
	if _, err := svc.Register(ctx, "a@x.io", "password123", "Ann", ""); err != nil {
		t.Fatal(err)
	}
	expired := tokenFrom(t, mailer.next(t))
	now = now.Add(auth.EmailVerificationTTL + time.Second)

	for name, token := range map[string]string{"expired": expired, "unknown": "no-such-token", "empty": ""} {
		if err := svc.ConfirmEmail(ctx, token); !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("%s: err = %v, want ErrInvalidToken", name, err)
		}
	}
}

func TestConfirmEmail_ForADeletedAccount_IsReported(t *testing.T) {
	ctx := context.Background()
	store, mailer := newFakeStore(), newMailer()
	svc := newVerifyingService(store, mailer, time.Now)
	res, err := svc.Register(ctx, "a@x.io", "password123", "Ann", "")
	if err != nil {
		t.Fatal(err)
	}
	token := tokenFrom(t, mailer.next(t))
	if err := svc.DeleteAccount(ctx, res.User.ID, "password123"); err != nil {
		t.Fatal(err)
	}

	if err := svc.ConfirmEmail(ctx, token); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("err = %v, want ErrUserNotFound", err)
	}
}

func TestRegister_WithoutMail_TheAddressCountsAsVerified(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	svc := newService(store)

	res, err := svc.Register(ctx, "a@x.io", "password123", "Ann", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.User.EmailVerifiedAt == nil || store.users["a@x.io"].EmailVerifiedAt == nil {
		t.Error("without a mail server there is nothing to verify with: accounts must start verified")
	}
	if err := svc.RequestEmailVerification(ctx, res.User.ID); !errors.Is(err, auth.ErrVerificationUnavailable) {
		t.Errorf("RequestEmailVerification: err = %v, want ErrVerificationUnavailable", err)
	}
}

func TestRegister_MailWithoutAVerifyPage_TheAddressCountsAsVerified(t *testing.T) {
	store, mailer := newFakeStore(), newMailer()
	svc := newRecoveryService(store, mailer, time.Now) // WithMailer без WithEmailVerification

	if _, err := svc.Register(context.Background(), "a@x.io", "password123", "Ann", ""); err != nil {
		t.Fatal(err)
	}
	if store.users["a@x.io"].EmailVerifiedAt == nil {
		t.Error("without a verify page the address must count as verified")
	}
	mailer.none(t)
}

func TestRegister_AMailFailureDoesNotCancelTheRegistration(t *testing.T) {
	mailer := newMailer()
	mailer.err = errors.New("smtp down")
	svc := newVerifyingService(newFakeStore(), mailer, time.Now)

	res, err := svc.Register(context.Background(), "a@x.io", "password123", "Ann", "")
	if err != nil || res.AccessToken == "" {
		t.Fatalf("Register = %+v, %v; a mail failure must not cancel it", res, err)
	}
}

// Хранилище, в котором выбранные шаги подтверждения почты падают.
type verifyFailures struct {
	*fakeStore
	create, deleteOld, latest, verify, claim, purge error
	verifyRows                                      *int64
}

func (v verifyFailures) CreateEmailVerificationToken(ctx context.Context, a db.CreateEmailVerificationTokenParams) error {
	if v.create != nil {
		return v.create
	}
	return v.fakeStore.CreateEmailVerificationToken(ctx, a)
}

func (v verifyFailures) DeleteUserEmailVerificationTokens(ctx context.Context, id uuid.UUID) error {
	if v.deleteOld != nil {
		return v.deleteOld
	}
	return v.fakeStore.DeleteUserEmailVerificationTokens(ctx, id)
}

func (v verifyFailures) LatestEmailVerificationExpiry(ctx context.Context, id uuid.UUID) (time.Time, error) {
	if v.latest != nil {
		return time.Time{}, v.latest
	}
	return v.fakeStore.LatestEmailVerificationExpiry(ctx, id)
}

func (v verifyFailures) VerifyUserEmail(ctx context.Context, id uuid.UUID) (int64, error) {
	if v.verify != nil {
		return 0, v.verify
	}
	if v.verifyRows != nil {
		return *v.verifyRows, nil
	}
	return v.fakeStore.VerifyUserEmail(ctx, id)
}

func (v verifyFailures) ClaimEmailVerificationToken(ctx context.Context, a db.ClaimEmailVerificationTokenParams) (uuid.UUID, error) {
	if v.claim != nil {
		return uuid.Nil, v.claim
	}
	return v.fakeStore.ClaimEmailVerificationToken(ctx, a)
}

func (v verifyFailures) DeleteExpiredEmailVerificationTokens(ctx context.Context, now time.Time) (int64, error) {
	if v.purge != nil {
		return 0, v.purge
	}
	return v.fakeStore.DeleteExpiredEmailVerificationTokens(ctx, now)
}

// Сбой выпуска ссылки не отменяет регистрацию: пользователь запросит ссылку заново.
func TestRegister_ALinkFailureDoesNotCancelTheRegistration(t *testing.T) {
	for name, fail := range map[string]func(*fakeStore) verifyFailures{
		"the link cannot be stored": func(f *fakeStore) verifyFailures { return verifyFailures{fakeStore: f, create: errors.New("db down")} },
		"old links cannot be dropped": func(f *fakeStore) verifyFailures {
			return verifyFailures{fakeStore: f, deleteOld: errors.New("db down")}
		},
	} {
		mailer := newMailer()
		svc := newVerifyingService(fail(newFakeStore()), mailer, time.Now)

		if _, err := svc.Register(context.Background(), "a@x.io", "password123", "Ann", ""); err != nil {
			t.Errorf("%s: Register: %v", name, err)
		}
		mailer.none(t)
	}
}

func TestRegister_WithoutMail_AFailureToMarkTheAddressIsReported(t *testing.T) {
	svc := auth.NewService(verifyFailures{fakeStore: newFakeStore(), verify: errors.New("db down")}, secret, auth.WithBcryptCost(bcrypt.MinCost))

	if _, err := svc.Register(context.Background(), "a@x.io", "password123", "Ann", ""); err == nil {
		t.Error("the failure must be reported: an unverified account could never publish")
	}
}

func TestRequestEmailVerification_SendsANewLink_AndCancelsTheOldOne(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	mailer := newMailer()
	svc := newVerifyingService(newFakeStore(), mailer, func() time.Time { return now })
	res, err := svc.Register(ctx, "a@x.io", "password123", "Ann", "")
	if err != nil {
		t.Fatal(err)
	}
	first := tokenFrom(t, mailer.next(t))

	now = now.Add(auth.EmailVerificationCooldown + time.Second)
	if err := svc.RequestEmailVerification(ctx, res.User.ID); err != nil {
		t.Fatal(err)
	}
	second := tokenFrom(t, mailer.next(t))

	if err := svc.ConfirmEmail(ctx, first); !errors.Is(err, auth.ErrInvalidToken) {
		t.Errorf("the old link must stop working: err = %v", err)
	}
	if err := svc.ConfirmEmail(ctx, second); err != nil {
		t.Errorf("the new link must work: %v", err)
	}
}

func TestRequestEmailVerification_AtMostOneMailPerMinute_AndTheAnswerSaysWhen(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	mailer := newMailer()
	svc := newVerifyingService(newFakeStore(), mailer, func() time.Time { return now })
	res, err := svc.Register(ctx, "a@x.io", "password123", "Ann", "")
	if err != nil {
		t.Fatal(err)
	}
	mailer.next(t) // письмо при регистрации

	now = now.Add(20 * time.Second)
	err = svc.RequestEmailVerification(ctx, res.User.ID)
	var soon *auth.TooSoonError
	if !errors.As(err, &soon) {
		t.Fatalf("err = %v, want *TooSoonError", err)
	}
	if soon.RetryAfter < 39*time.Second || soon.RetryAfter > 41*time.Second {
		t.Errorf("RetryAfter = %v, want about 40s", soon.RetryAfter)
	}
	if soon.Error() == "" {
		t.Error("the error must explain itself")
	}
	mailer.none(t)
}

func TestRequestEmailVerification_ForAVerifiedAddress_DoesNothing(t *testing.T) {
	ctx := context.Background()
	mailer := newMailer()
	svc := newVerifyingService(newFakeStore(), mailer, time.Now)
	res, err := svc.Register(ctx, "a@x.io", "password123", "Ann", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmEmail(ctx, tokenFrom(t, mailer.next(t))); err != nil {
		t.Fatal(err)
	}

	if err := svc.RequestEmailVerification(ctx, res.User.ID); err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	mailer.none(t)
}

func TestRequestEmailVerification_UnknownAccount_FailureOnAnyStepIsReported(t *testing.T) {
	ctx := context.Background()
	svc := newVerifyingService(newFakeStore(), newMailer(), time.Now)
	if err := svc.RequestEmailVerification(ctx, uuid.New()); !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("unknown account: err = %v, want ErrUserNotFound", err)
	}

	for name, fail := range map[string]func(*fakeStore) verifyFailures{
		"the last link cannot be checked": func(f *fakeStore) verifyFailures { return verifyFailures{fakeStore: f, latest: errors.New("db down")} },
		"the link cannot be stored":       func(f *fakeStore) verifyFailures { return verifyFailures{fakeStore: f, create: errors.New("db down")} },
		"old links cannot be dropped": func(f *fakeStore) verifyFailures {
			return verifyFailures{fakeStore: f, deleteOld: errors.New("db down")}
		},
	} {
		base := newFakeStore()
		res, err := newVerifyingService(base, newMailer(), time.Now).Register(ctx, "a@x.io", "password123", "Ann", "")
		if err != nil {
			t.Fatal(err)
		}
		clear(base.verifications()) // следов первого письма нет: упираемся именно в сбой, а не в паузу
		failing := newVerifyingService(fail(base), newMailer(), time.Now)
		if err := failing.RequestEmailVerification(ctx, res.User.ID); err == nil {
			t.Errorf("%s: the failure must be reported", name)
		}
	}
}

func TestConfirmEmail_FailureOnAnyStepIsReported(t *testing.T) {
	ctx := context.Background()
	zero := int64(0)
	for name, fail := range map[string]func(*fakeStore) verifyFailures{
		"the link cannot be claimed":  func(f *fakeStore) verifyFailures { return verifyFailures{fakeStore: f, claim: errors.New("db down")} },
		"the address cannot be saved": func(f *fakeStore) verifyFailures { return verifyFailures{fakeStore: f, verify: errors.New("db down")} },
		"the account vanished":        func(f *fakeStore) verifyFailures { return verifyFailures{fakeStore: f, verifyRows: &zero} },
	} {
		base, mailer := newFakeStore(), newMailer()
		if _, err := newVerifyingService(base, mailer, time.Now).Register(ctx, "a@x.io", "password123", "Ann", ""); err != nil {
			t.Fatal(err)
		}
		token := tokenFrom(t, mailer.next(t))

		err := newVerifyingService(fail(base), newMailer(), time.Now).ConfirmEmail(ctx, token)
		if err == nil {
			t.Errorf("%s: the failure must be reported", name)
		}
		if errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("%s: a store failure must not look like a bad link: %v", name, err)
		}
	}
}

// Ссылка сброса пароля приходит на почту аккаунта: открыв её, пользователь доказал, что адрес его.
func TestConfirmPasswordReset_AlsoVerifiesTheAddress(t *testing.T) {
	ctx := context.Background()
	store, mailer := newFakeStore(), newMailer()
	svc := newVerifyingService(store, mailer, time.Now)
	if _, err := svc.Register(ctx, "a@x.io", "password123", "Ann", ""); err != nil {
		t.Fatal(err)
	}
	mailer.next(t) // письмо подтверждения, ссылка из него не открывается
	if store.users["a@x.io"].EmailVerifiedAt != nil {
		t.Fatal("precondition: the address is not verified yet")
	}
	if err := svc.RequestPasswordReset(ctx, "a@x.io"); err != nil {
		t.Fatal(err)
	}

	if err := svc.ConfirmPasswordReset(ctx, tokenFrom(t, mailer.next(t)), "brand-new-pass"); err != nil {
		t.Fatal(err)
	}
	if store.users["a@x.io"].EmailVerifiedAt == nil {
		t.Error("opening the reset link proves the address, it must become verified")
	}
}

func TestConfirmPasswordReset_ASlipWhileVerifyingDoesNotUndoTheReset(t *testing.T) {
	ctx := context.Background()
	base, mailer := newFakeStore(), newMailer()
	if _, err := newVerifyingService(base, mailer, time.Now).Register(ctx, "a@x.io", "password123", "Ann", ""); err != nil {
		t.Fatal(err)
	}
	mailer.next(t)
	svc := newVerifyingService(verifyFailures{fakeStore: base, verify: errors.New("db down")}, mailer, time.Now)
	if err := svc.RequestPasswordReset(ctx, "a@x.io"); err != nil {
		t.Fatal(err)
	}

	if err := svc.ConfirmPasswordReset(ctx, tokenFrom(t, mailer.next(t)), "brand-new-pass"); err != nil {
		t.Errorf("the password is already changed, a verification slip must not report a failure: %v", err)
	}
}

func TestPurgeExpiredTokens_AlsoRemovesVerificationLinks(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store, mailer := newFakeStore(), newMailer()
	svc := newVerifyingService(store, mailer, func() time.Time { return now })
	if _, err := svc.Register(ctx, "a@x.io", "password123", "Ann", ""); err != nil {
		t.Fatal(err)
	}
	mailer.next(t)
	now = now.Add(auth.EmailVerificationTTL + time.Minute)

	if _, err := svc.PurgeExpiredTokens(ctx); err != nil {
		t.Fatal(err)
	}
	if n := len(store.verifications()); n != 0 {
		t.Errorf("%d expired links left", n)
	}

	failing := newVerifyingService(verifyFailures{fakeStore: store, purge: errors.New("db down")}, mailer, time.Now)
	if _, err := failing.PurgeExpiredTokens(ctx); err == nil {
		t.Error("a purge failure must be reported")
	}
}
