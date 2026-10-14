package mail

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	netmail "net/mail"
	"net/smtp"
	"strings"
	"testing"
	"time"
)

type captured struct {
	addr string
	auth smtp.Auth
	from string
	to   []string
	msg  []byte
}

func testSMTP(t *testing.T, cfg Config) (*SMTP, *captured) {
	t.Helper()
	m, err := NewSMTP(cfg)
	if err != nil {
		t.Fatal(err)
	}
	c := &captured{}
	m.send = func(addr string, a smtp.Auth, from string, to []string, msg []byte) error {
		*c = captured{addr, a, from, to, msg}
		return nil
	}
	return m, c
}

func TestSend_BuildsAReadableUTF8MessageWithEncodedHeaders(t *testing.T) {
	m, c := testSMTP(t, Config{Host: "mail.local", Port: "1025", From: "StudyVault <studyvault@example.com>"})
	body := "Здравствуйте, Анна!\n" + strings.Repeat("Длинная строка для проверки переноса. ", 8) + "\nhttps://app.example.com/reset?token=abc_DEF-123\n"

	if err := m.Send(context.Background(), "anna@example.com", "StudyVault: сброс пароля", body); err != nil {
		t.Fatal(err)
	}

	if c.addr != "mail.local:1025" || c.from != "StudyVault <studyvault@example.com>" || len(c.to) != 1 || c.to[0] != "anna@example.com" {
		t.Errorf("envelope = %+v", c)
	}
	if c.auth != nil {
		t.Error("without a username the mail goes out unauthenticated (a local Mailpit)")
	}
	msg, err := netmail.ReadMessage(strings.NewReader(string(c.msg)))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || subject != "StudyVault: сброс пароля" {
		t.Errorf("subject = %q, %v", subject, err)
	}
	if ct := msg.Header.Get("Content-Type"); !strings.Contains(ct, "charset=UTF-8") || msg.Header.Get("MIME-Version") != "1.0" {
		t.Errorf("content type = %q", ct)
	}
	raw, _ := io.ReadAll(msg.Body)
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\r\n") {
		if len(line) > 76 {
			t.Errorf("a base64 line of %d characters breaks the 76-character limit", len(line))
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(strings.TrimSpace(string(raw)), "\r\n", ""))
	if err != nil || string(decoded) != body {
		t.Errorf("the body does not survive the round trip: %v", err)
	}
}

func TestSend_RejectsRecipientsThatCouldInjectHeaders(t *testing.T) {
	m, c := testSMTP(t, Config{Host: "h", Port: "25", From: "a@example.com"})
	for _, to := range []string{"x@example.com\r\nBcc: victim@example.com", "x@example.com\nSubject: hi", "not an address", ""} {
		if err := m.Send(context.Background(), to, "s", "b"); err == nil {
			t.Errorf("%q must be rejected", to)
		}
		if c.addr != "" {
			t.Fatalf("nothing may be sent for %q", to)
		}
	}
}

func TestSend_UsesPlainAuthWhenCredentialsAreSet(t *testing.T) {
	m, c := testSMTP(t, Config{Host: "smtp.example.com", Port: "587", Username: "user", Password: "secret", From: "a@example.com"})
	if err := m.Send(context.Background(), "b@example.com", "s", "b"); err != nil {
		t.Fatal(err)
	}
	if c.auth == nil {
		t.Error("credentials were configured, authentication is expected")
	}
}

func TestSend_ReportsServerErrorsAndHonoursTheContext(t *testing.T) {
	m, _ := testSMTP(t, Config{Host: "h", Port: "25", From: "a@example.com"})
	m.send = func(string, smtp.Auth, string, []string, []byte) error { return errors.New("550 mailbox unavailable") }
	if err := m.Send(context.Background(), "b@example.com", "s", "b"); err == nil || !strings.Contains(err.Error(), "550") {
		t.Errorf("err = %v", err)
	}

	m.send = func(string, smtp.Auth, string, []string, []byte) error { time.Sleep(time.Second); return nil }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := m.Send(ctx, "b@example.com", "s", "b"); !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > 500*time.Millisecond {
		t.Errorf("a hung server must not hold the caller: %v after %v", err, time.Since(started))
	}
}

func TestNewSMTP_ValidatesItsConfig(t *testing.T) {
	for name, cfg := range map[string]Config{
		"no host":    {Port: "25", From: "a@example.com"},
		"no port":    {Host: "h", From: "a@example.com"},
		"no sender":  {Host: "h", Port: "25"},
		"bad sender": {Host: "h", Port: "25", From: "nope"},
	} {
		if _, err := NewSMTP(cfg); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}
