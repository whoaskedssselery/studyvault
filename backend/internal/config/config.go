package config

import (
	"errors"
	"os"
	"strconv"
)

const minJWTSecretLen = 32

type Config struct {
	HTTPPort     string
	DatabaseURL  string
	JWTSecret    []byte
	CookieSecure bool
}

func Load() (Config, error) {
	cfg := Config{
		HTTPPort:    getenv("HTTP_PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		JWTSecret:   []byte(os.Getenv("JWT_SECRET")),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if len(cfg.JWTSecret) < minJWTSecretLen {
		return Config{}, errors.New("JWT_SECRET is required and must be at least 32 characters")
	}
	// Для локальной разработки по http cookie с Secure не сохранится, поэтому по умолчанию выключено.
	cfg.CookieSecure, _ = strconv.ParseBool(os.Getenv("COOKIE_SECURE"))
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
