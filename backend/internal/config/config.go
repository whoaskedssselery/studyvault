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
	MeiliURL     string // пусто: поиск отключён
	MeiliKey     string
	S3           S3 // Endpoint пуст: файлы отключены
}

func Load() (Config, error) {
	cfg := Config{
		HTTPPort:    getenv("HTTP_PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		JWTSecret:   []byte(os.Getenv("JWT_SECRET")),
		MeiliURL:    os.Getenv("MEILI_URL"),
		MeiliKey:    os.Getenv("MEILI_MASTER_KEY"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if len(cfg.JWTSecret) < minJWTSecretLen {
		return Config{}, errors.New("JWT_SECRET is required and must be at least 32 characters")
	}
	// Для локальной разработки по http cookie с Secure не сохранится, поэтому по умолчанию выключено.
	cfg.CookieSecure, _ = strconv.ParseBool(os.Getenv("COOKIE_SECURE"))

	cfg.S3 = S3{
		Endpoint:       os.Getenv("S3_ENDPOINT"),
		AccessKey:      os.Getenv("S3_ACCESS_KEY"),
		SecretKey:      os.Getenv("S3_SECRET_KEY"),
		Bucket:         getenv("S3_BUCKET", "attachments"),
		Region:         os.Getenv("S3_REGION"),
		PublicEndpoint: os.Getenv("S3_PUBLIC_ENDPOINT"),
	}
	cfg.S3.UseSSL, _ = strconv.ParseBool(os.Getenv("S3_USE_SSL"))
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// S3 описывает S3-совместимое хранилище файлов.
type S3 struct {
	Endpoint       string
	AccessKey      string
	SecretKey      string
	Bucket         string
	Region         string // пусто: us-east-1
	UseSSL         bool
	PublicEndpoint string // адрес в presigned-ссылках для клиента, если он отличается от Endpoint
}
