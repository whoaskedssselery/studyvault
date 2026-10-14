package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const minJWTSecretLen = 32

// Попыток входа, регистрации и обновления токена в минуту с одного адреса.
const defaultAuthRateLimit = 20

type Config struct {
	HTTPPort       string
	DatabaseURL    string
	JWTSecret      []byte
	CookieSecure   bool
	MeiliURL       string // пусто: поиск отключён
	MeiliKey       string
	S3             S3           // Endpoint пуст: файлы отключены
	CORSOrigins    []string     // пусто: CORS выключен
	AuthRateLimit  int          // попыток входа в минуту с одного адреса; 0: без лимита
	TrustedProxies []*net.IPNet // доверенные прокси (TRUSTED_PROXIES): от них принимается X-Forwarded-For
	SMTP           SMTP         // Host пуст: сброс пароля по почте выключен
	LogJSON        bool         // формат лога: JSON (по умолчанию) или текст для разработки
	LogLevel       slog.Level   // по умолчанию info
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
	// Secure по умолчанию включён: refresh-cookie не уйдёт по голому http. Для локальной разработки
	// по http его выключают явно (COOKIE_SECURE=false); непонятное значение защиту не ослабляет.
	cfg.CookieSecure = true
	if v, err := strconv.ParseBool(os.Getenv("COOKIE_SECURE")); err == nil {
		cfg.CookieSecure = v
	}

	cfg.S3 = S3{
		Endpoint:       os.Getenv("S3_ENDPOINT"),
		AccessKey:      os.Getenv("S3_ACCESS_KEY"),
		SecretKey:      os.Getenv("S3_SECRET_KEY"),
		Bucket:         getenv("S3_BUCKET", "attachments"),
		Region:         os.Getenv("S3_REGION"),
		PublicEndpoint: os.Getenv("S3_PUBLIC_ENDPOINT"),
	}
	cfg.S3.UseSSL, _ = strconv.ParseBool(os.Getenv("S3_USE_SSL"))

	origins, err := parseOrigins(os.Getenv("CORS_ORIGINS"))
	if err != nil {
		return Config{}, err
	}
	cfg.CORSOrigins = origins

	proxies, err := parseProxies(os.Getenv("TRUSTED_PROXIES"))
	if err != nil {
		return Config{}, err
	}
	cfg.TrustedProxies = proxies

	cfg.AuthRateLimit = defaultAuthRateLimit
	if v := os.Getenv("AUTH_RATE_LIMIT_PER_MIN"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return Config{}, fmt.Errorf("AUTH_RATE_LIMIT_PER_MIN must be a non-negative integer, got %q", v)
		}
		cfg.AuthRateLimit = n
	}
	cfg.SMTP = SMTP{
		Host: os.Getenv("SMTP_HOST"), Port: getenv("SMTP_PORT", "587"), Username: os.Getenv("SMTP_USERNAME"),
		Password: os.Getenv("SMTP_PASSWORD"), From: os.Getenv("SMTP_FROM"), ResetURL: os.Getenv("PASSWORD_RESET_URL"),
	}
	if cfg.SMTP.Host != "" {
		if cfg.SMTP.From == "" || cfg.SMTP.ResetURL == "" {
			return Config{}, errors.New("SMTP_HOST needs SMTP_FROM and PASSWORD_RESET_URL")
		}
		if u, err := url.Parse(cfg.SMTP.ResetURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return Config{}, fmt.Errorf("PASSWORD_RESET_URL must be an http(s) address, got %q", cfg.SMTP.ResetURL)
		}
		if (cfg.SMTP.Username == "") != (cfg.SMTP.Password == "") {
			return Config{}, errors.New("SMTP_USERNAME and SMTP_PASSWORD must be set together")
		}
	}
	switch f := strings.ToLower(os.Getenv("LOG_FORMAT")); f {
	case "", "json":
		cfg.LogJSON = true
	case "text":
	default:
		return Config{}, fmt.Errorf("LOG_FORMAT must be json or text, got %q", f)
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(v)); err != nil {
			return Config{}, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error, got %q", v)
		}
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// SMTP описывает почтовый сервер для писем со ссылкой сброса пароля.
type SMTP struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
	ResetURL string // адрес страницы фронтенда, куда ведёт ссылка из письма; токен добавляется параметром token
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

// parseOrigins разбирает список источников через запятую. Допускаются только явные адреса
// вида схема://хост[:порт]: API отдаёт cookie, а с ними звёздочка в CORS запрещена.
func parseOrigins(raw string) ([]string, error) {
	var origins []string
	for _, o := range strings.Split(raw, ",") {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		u, err := url.Parse(o)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
			return nil, fmt.Errorf("CORS_ORIGINS: %q is not a valid origin (want scheme://host[:port], no wildcards)", o)
		}
		origins = append(origins, strings.TrimSuffix(o, "/"))
	}
	return origins, nil
}

// parseProxies разбирает список доверенных прокси: адреса или сети в записи CIDR через запятую. Широкую сеть вроде
// 0.0.0.0/0 принимать нельзя: она сделала бы X-Forwarded-For доверенным для всех, то есть подделываемым.
func parseProxies(raw string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.Contains(item, "/") {
			ip := net.ParseIP(item)
			if ip == nil {
				return nil, fmt.Errorf("TRUSTED_PROXIES: %q is not an address or a CIDR network", item)
			}
			bits := 128
			if ip.To4() != nil {
				bits = 32
			}
			item = fmt.Sprintf("%s/%d", ip, bits)
		}
		_, n, err := net.ParseCIDR(item)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXIES: %q is not an address or a CIDR network", item)
		}
		if ones, _ := n.Mask.Size(); ones == 0 {
			return nil, fmt.Errorf("TRUSTED_PROXIES: %q would trust every address", item)
		}
		out = append(out, n)
	}
	return out, nil
}
