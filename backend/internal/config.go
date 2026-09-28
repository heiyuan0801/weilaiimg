package internal

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Addr           string
	DatabaseURL    string
	RedisURL       string
	StorageDir     string
	WebDir         string
	PublicURL      string
	SessionTTL     time.Duration
	MaxUploadBytes int64
	MaxRemoteBytes int64
	TelegramToken  string
	TelegramChatID string
	// AppSecret encrypts OIDC client secrets at rest. Set APP_SECRET in production.
	AppSecret           string
	CookieSecure        bool
	StripeSecretKey     string
	StripeWebhookSecret string
	AllowedOrigins      string
	CloudflareAPIToken  string
	CloudflareZoneID    string
}

func LoadConfig() Config {
	return Config{
		Addr:                env("ADDR", ":8080"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		RedisURL:            env("REDIS_URL", "redis://localhost:6379/0"),
		StorageDir:          env("STORAGE_DIR", "./uploads"),
		WebDir:              env("WEB_DIR", "../web/dist"),
		PublicURL:           env("PUBLIC_URL", "http://localhost:8080"),
		SessionTTL:          time.Duration(envInt("SESSION_TTL_SECONDS", 86400)) * time.Second,
		MaxUploadBytes:      envInt64("MAX_UPLOAD_BYTES", 20<<20),
		MaxRemoteBytes:      envInt64("MAX_REMOTE_BYTES", 50<<20),
		TelegramToken:       os.Getenv("TELEGRAM_BOT_TOKEN"),
		TelegramChatID:      os.Getenv("TELEGRAM_CHAT_ID"),
		AppSecret:           env("APP_SECRET", ""),
		CookieSecure:        env("COOKIE_SECURE", "false") == "true",
		StripeSecretKey:     os.Getenv("STRIPE_SECRET_KEY"),
		StripeWebhookSecret: os.Getenv("STRIPE_WEBHOOK_SECRET"),
		AllowedOrigins:      os.Getenv("CORS_ALLOWED_ORIGINS"),
		CloudflareAPIToken:  os.Getenv("CLOUDFLARE_API_TOKEN"),
		CloudflareZoneID:    os.Getenv("CLOUDFLARE_ZONE_ID"),
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func envInt(key string, fallback int) int {
	if value, err := strconv.Atoi(os.Getenv(key)); err == nil && value > 0 {
		return value
	}
	return fallback
}
func envInt64(key string, fallback int64) int64 {
	if value, err := strconv.ParseInt(os.Getenv(key), 10, 64); err == nil && value > 0 {
		return value
	}
	return fallback
}
