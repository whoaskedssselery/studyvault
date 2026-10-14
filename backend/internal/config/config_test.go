package config_test

import (
	"log/slog"
	"testing"

	"github.com/whoaskedssselery/studyvault/backend/internal/config"
)

const goodSecret = "0123456789abcdef0123456789abcdef"

func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("JWT_SECRET", goodSecret)
	t.Setenv("HTTP_PORT", "")
	t.Setenv("COOKIE_SECURE", "")
}

func TestLoad_MissingDatabaseURL_ReturnsError(t *testing.T) {
	setValidEnv(t)
	t.Setenv("DATABASE_URL", "")

	if _, err := config.Load(); err == nil {
		t.Fatal("expected error when DATABASE_URL is empty")
	}
}

func TestLoad_DefaultPort(t *testing.T) {
	setValidEnv(t)

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPPort != "8080" {
		t.Errorf("HTTPPort = %q, want %q", cfg.HTTPPort, "8080")
	}
}

func TestLoad_CustomPort(t *testing.T) {
	setValidEnv(t)
	t.Setenv("HTTP_PORT", "9090")

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPPort != "9090" {
		t.Errorf("HTTPPort = %q, want %q", cfg.HTTPPort, "9090")
	}
}

func TestLoad_JWTSecret_RequiredAnd32Chars(t *testing.T) {
	setValidEnv(t)

	for _, s := range []string{"", "too-short"} {
		t.Setenv("JWT_SECRET", s)
		if _, err := config.Load(); err == nil {
			t.Errorf("JWT_SECRET=%q: expected error", s)
		}
	}

	t.Setenv("JWT_SECRET", goodSecret)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if string(cfg.JWTSecret) != goodSecret {
		t.Error("JWTSecret not loaded")
	}
}

func TestLoad_CookieSecure_IsOnUnlessExplicitlyDisabled(t *testing.T) {
	setValidEnv(t)

	cases := map[string]bool{
		"":      true, // по умолчанию безопасно: забыть включить в проде нельзя
		"true":  true,
		"false": false, // локальная разработка по http
		"0":     false,
		"junk":  true, // непонятное значение не должно ослаблять защиту
	}
	for value, want := range cases {
		t.Setenv("COOKIE_SECURE", value)
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("COOKIE_SECURE=%q: %v", value, err)
		}
		if cfg.CookieSecure != want {
			t.Errorf("COOKIE_SECURE=%q: CookieSecure = %v, want %v", value, cfg.CookieSecure, want)
		}
	}
}

func TestLoad_Meilisearch_IsOptional(t *testing.T) {
	setValidEnv(t)
	t.Setenv("MEILI_URL", "")
	t.Setenv("MEILI_MASTER_KEY", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("a missing Meilisearch must not stop the API from starting: %v", err)
	}
	if cfg.MeiliURL != "" {
		t.Errorf("MeiliURL = %q, want empty (search disabled)", cfg.MeiliURL)
	}

	t.Setenv("MEILI_URL", "http://localhost:7700")
	t.Setenv("MEILI_MASTER_KEY", "k")
	cfg, _ = config.Load()
	if cfg.MeiliURL != "http://localhost:7700" || cfg.MeiliKey != "k" {
		t.Errorf("Meilisearch settings not loaded: %+v", cfg)
	}
}

func TestLoad_S3_IsOptionalAndHasDefaults(t *testing.T) {
	setValidEnv(t)
	for _, k := range []string{"S3_ENDPOINT", "S3_ACCESS_KEY", "S3_SECRET_KEY", "S3_BUCKET", "S3_REGION", "S3_USE_SSL", "S3_PUBLIC_ENDPOINT"} {
		t.Setenv(k, "")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("a missing storage must not stop the API from starting: %v", err)
	}
	if cfg.S3.Endpoint != "" {
		t.Errorf("Endpoint = %q, want empty (attachments disabled)", cfg.S3.Endpoint)
	}
	if cfg.S3.Bucket != "attachments" || cfg.S3.UseSSL || cfg.S3.PublicEndpoint != "" {
		t.Errorf("defaults: %+v", cfg.S3)
	}

	t.Setenv("S3_ENDPOINT", "localhost:9000")
	t.Setenv("S3_ACCESS_KEY", "ak")
	t.Setenv("S3_SECRET_KEY", "sk")
	t.Setenv("S3_BUCKET", "files")
	t.Setenv("S3_USE_SSL", "true")
	t.Setenv("S3_PUBLIC_ENDPOINT", "cdn.example.org")
	cfg, _ = config.Load()
	want := config.S3{Endpoint: "localhost:9000", AccessKey: "ak", SecretKey: "sk", Bucket: "files", UseSSL: true, PublicEndpoint: "cdn.example.org"}
	if cfg.S3 != want {
		t.Errorf("S3 = %+v, want %+v", cfg.S3, want)
	}
}

func TestLoad_CORSOrigins(t *testing.T) {
	setValidEnv(t)

	t.Setenv("CORS_ORIGINS", "")
	if cfg, _ := config.Load(); len(cfg.CORSOrigins) != 0 {
		t.Errorf("CORS is off by default, got %v", cfg.CORSOrigins)
	}

	t.Setenv("CORS_ORIGINS", " http://localhost:5173 , https://app.example.org,, ")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.CORSOrigins) != 2 || cfg.CORSOrigins[0] != "http://localhost:5173" || cfg.CORSOrigins[1] != "https://app.example.org" {
		t.Errorf("origins = %q", cfg.CORSOrigins)
	}
}

func TestLoad_CORSOrigins_RejectsUnsafeOrMalformedEntries(t *testing.T) {
	setValidEnv(t)

	for _, bad := range []string{
		"*", // с cookie допустимы только явные источники
		"http://localhost:5173,*",
		"localhost:5173",            // без схемы
		"http://localhost:5173/app", // источник — это схема и хост, без пути
		"ftp://example.org",         // не http(s)
		"http://",
	} {
		t.Setenv("CORS_ORIGINS", bad)
		if _, err := config.Load(); err == nil {
			t.Errorf("CORS_ORIGINS=%q must be rejected", bad)
		}
	}
}

func TestLoad_AuthRateLimit(t *testing.T) {
	setValidEnv(t)

	t.Setenv("AUTH_RATE_LIMIT_PER_MIN", "")
	if cfg, _ := config.Load(); cfg.AuthRateLimit != 20 {
		t.Errorf("default = %d, want 20 attempts per minute", cfg.AuthRateLimit)
	}
	t.Setenv("AUTH_RATE_LIMIT_PER_MIN", "5")
	if cfg, _ := config.Load(); cfg.AuthRateLimit != 5 {
		t.Errorf("custom value not honoured: %d", cfg.AuthRateLimit)
	}
	t.Setenv("AUTH_RATE_LIMIT_PER_MIN", "0")
	if cfg, err := config.Load(); err != nil || cfg.AuthRateLimit != 0 {
		t.Errorf("0 must switch the limit off: %d, %v", cfg.AuthRateLimit, err)
	}
	for _, bad := range []string{"-1", "many", "1.5"} {
		t.Setenv("AUTH_RATE_LIMIT_PER_MIN", bad)
		if _, err := config.Load(); err == nil {
			t.Errorf("AUTH_RATE_LIMIT_PER_MIN=%q must be rejected rather than silently ignored", bad)
		}
	}
}

func TestLoad_Logging(t *testing.T) {
	setValidEnv(t)
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("LOG_LEVEL", "")
	cfg, err := config.Load()
	if err != nil || !cfg.LogJSON || cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("defaults: json=%v level=%v err=%v, want json at info", cfg.LogJSON, cfg.LogLevel, err)
	}

	t.Setenv("LOG_FORMAT", "TEXT")
	t.Setenv("LOG_LEVEL", "debug")
	cfg, err = config.Load()
	if err != nil || cfg.LogJSON || cfg.LogLevel != slog.LevelDebug {
		t.Errorf("text/debug: json=%v level=%v err=%v", cfg.LogJSON, cfg.LogLevel, err)
	}

	for _, bad := range [][2]string{{"xml", ""}, {"", "chatty"}} {
		t.Setenv("LOG_FORMAT", bad[0])
		t.Setenv("LOG_LEVEL", bad[1])
		if _, err := config.Load(); err == nil {
			t.Errorf("LOG_FORMAT=%q LOG_LEVEL=%q must be rejected", bad[0], bad[1])
		}
	}
}

func smtpEnv(t *testing.T, host, from, resetURL, user, pass string) {
	t.Helper()
	setValidEnv(t)
	t.Setenv("SMTP_HOST", host)
	t.Setenv("SMTP_PORT", "")
	t.Setenv("SMTP_FROM", from)
	t.Setenv("PASSWORD_RESET_URL", resetURL)
	t.Setenv("SMTP_USERNAME", user)
	t.Setenv("SMTP_PASSWORD", pass)
}

func TestLoad_SMTP_IsOptional_AndAllOrNothing(t *testing.T) {
	smtpEnv(t, "", "", "", "", "")
	cfg, err := config.Load()
	if err != nil || cfg.SMTP.Host != "" {
		t.Fatalf("without SMTP_HOST the feature is off: %+v, %v", cfg.SMTP, err)
	}

	smtpEnv(t, "mailpit", "studyvault@example.com", "https://app.example.com/reset", "", "")
	cfg, err = config.Load()
	if err != nil || cfg.SMTP.Host != "mailpit" || cfg.SMTP.Port != "587" || cfg.SMTP.From != "studyvault@example.com" {
		t.Errorf("a complete config: %+v, %v (the port defaults to 587)", cfg.SMTP, err)
	}

	for name, args := range map[string][5]string{
		"no sender":               {"mailpit", "", "https://app.example.com/reset", "", ""},
		"no reset page":           {"mailpit", "a@example.com", "", "", ""},
		"reset page is not a url": {"mailpit", "a@example.com", "javascript:alert(1)", "", ""},
		"relative reset page":     {"mailpit", "a@example.com", "/reset", "", ""},
		"user without password":   {"mailpit", "a@example.com", "https://app.example.com/reset", "user", ""},
		"password without user":   {"mailpit", "a@example.com", "https://app.example.com/reset", "", "secret"},
	} {
		smtpEnv(t, args[0], args[1], args[2], args[3], args[4])
		if _, err := config.Load(); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}
}

func TestLoad_EmailVerifyURL_DefaultsToTheSiteOfTheResetPage_AndMustBeAnHTTPAddress(t *testing.T) {
	smtpEnv(t, "mailpit", "studyvault@example.com", "https://app.example.com/reset-password?lang=ru", "", "")
	t.Setenv("EMAIL_VERIFY_URL", "")
	cfg, err := config.Load()
	if err != nil || cfg.SMTP.VerifyURL != "https://app.example.com/verify-email" {
		t.Errorf("default = %q, %v, want /verify-email on the same site", cfg.SMTP.VerifyURL, err)
	}

	t.Setenv("EMAIL_VERIFY_URL", "https://other.example.com/confirm")
	cfg, err = config.Load()
	if err != nil || cfg.SMTP.VerifyURL != "https://other.example.com/confirm" {
		t.Errorf("an explicit address must win: %q, %v", cfg.SMTP.VerifyURL, err)
	}

	for _, bad := range []string{"javascript:alert(1)", "/verify-email", "ftp://x.example.com/v"} {
		t.Setenv("EMAIL_VERIFY_URL", bad)
		if _, err := config.Load(); err == nil {
			t.Errorf("EMAIL_VERIFY_URL=%q must be rejected", bad)
		}
	}
}

func TestLoad_TrustedProxies(t *testing.T) {
	setValidEnv(t)
	t.Setenv("TRUSTED_PROXIES", "")
	cfg, err := config.Load()
	if err != nil || len(cfg.TrustedProxies) != 0 {
		t.Fatalf("no proxies by default: %v, %v", cfg.TrustedProxies, err)
	}

	t.Setenv("TRUSTED_PROXIES", "172.16.0.0/12, 10.1.2.3 ,fd00::/8")
	cfg, err = config.Load()
	if err != nil || len(cfg.TrustedProxies) != 3 {
		t.Fatalf("proxies = %v, %v", cfg.TrustedProxies, err)
	}
	if got := cfg.TrustedProxies[1].String(); got != "10.1.2.3/32" {
		t.Errorf("a bare address is a /32 network, got %s", got)
	}

	for _, bad := range []string{"nonsense", "10.0.0.0/33", "0.0.0.0/0", "::/0", "10.0.0.0/8,oops"} {
		t.Setenv("TRUSTED_PROXIES", bad)
		if _, err := config.Load(); err == nil {
			t.Errorf("TRUSTED_PROXIES=%q must be rejected (a catch-all network would make X-Forwarded-For forgeable)", bad)
		}
	}
}
