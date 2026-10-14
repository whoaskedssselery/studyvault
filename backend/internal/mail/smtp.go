// Package mail отправляет письма по SMTP. Нужен для ссылки сброса пароля.
package mail

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// Config — подключение к почтовому серверу. Без логина и пароля письма уходят без авторизации (так работает
// локальный Mailpit); с логином используется PLAIN поверх STARTTLS, который net/smtp включает сам, если сервер его предлагает.
type Config struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string // адрес отправителя, например studyvault@example.com
}

type SMTP struct {
	cfg  Config
	send func(addr string, a smtp.Auth, from string, to []string, msg []byte) error // подменяется в тестах
}

func NewSMTP(cfg Config) (*SMTP, error) {
	if cfg.Host == "" || cfg.Port == "" {
		return nil, errors.New("smtp host and port are required")
	}
	if _, err := mail.ParseAddress(cfg.From); err != nil {
		return nil, fmt.Errorf("smtp from address: %w", err)
	}
	return &SMTP{cfg: cfg, send: smtp.SendMail}, nil
}

// Send отправляет текстовое письмо. Адрес получателя проверяется как почтовый адрес: перевод строки в нём позволил бы
// подмешать чужие заголовки (header injection). Тема и тело кодируются, поэтому кириллица доходит без искажений.
func (s *SMTP) Send(ctx context.Context, to, subject, body string) error {
	addr, err := mail.ParseAddress(to)
	if err != nil || strings.ContainsAny(to, "\r\n") {
		return fmt.Errorf("recipient %q is not a valid address", to)
	}
	msg := buildMessage(s.cfg.From, addr.Address, subject, body, time.Now())

	var auth smtp.Auth
	if s.cfg.Username != "" {
		auth = smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	}
	done := make(chan error, 1)
	go func() {
		done <- s.send(net.JoinHostPort(s.cfg.Host, s.cfg.Port), auth, s.cfg.From, []string{addr.Address}, msg)
	}()
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("send mail: %w", err)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("send mail: %w", ctx.Err())
	}
}

func buildMessage(from, to, subject, body string, now time.Time) []byte {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + mime.BEncoding.Encode("UTF-8", subject) + "\r\n")
	b.WriteString("Date: " + now.Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	enc := base64.StdEncoding.EncodeToString([]byte(body))
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	return []byte(b.String())
}
