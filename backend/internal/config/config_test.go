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
