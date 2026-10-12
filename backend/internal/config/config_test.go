package config_test

import (
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

func TestLoad_CookieSecure(t *testing.T) {
	setValidEnv(t)

	if cfg, _ := config.Load(); cfg.CookieSecure {
		t.Error("CookieSecure must default to false")
	}
	t.Setenv("COOKIE_SECURE", "true")
	if cfg, _ := config.Load(); !cfg.CookieSecure {
		t.Error("CookieSecure=true not honoured")
	}
}
