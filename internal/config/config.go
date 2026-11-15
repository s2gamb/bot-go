package config

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// BotMode defines how the bot receives updates from Telegram.
type BotMode string

const (
	BotModePolling BotMode = "polling"
	BotModeWebhook BotMode = "webhook"
)

// Config holds the runtime configuration for bot-go.
type Config struct {
	TelegramBotToken   string
	SDPBaseURL         string
	SDPServiceToken    string
	SDPTelegramField   string
	TokenEncryptionKey []byte
	DBPath             string
	RequestsPerPage    int
	BotMode            BotMode
	WebhookURL         string
	WebhookListenAddr  string
	WebhookSecret      string
	SDPInsecureTLS     bool
	LogLevel           slog.Level
}

// Load reads configuration from process environment variables.
func Load() (*Config, error) {
	return LoadFromEnv(os.Getenv)
}

// LoadFromEnv reads and validates configuration using the provided lookup function.
func LoadFromEnv(getenv func(string) string) (*Config, error) {
	cfg := &Config{
		TelegramBotToken:  strings.TrimSpace(getenv("TELEGRAM_BOT_TOKEN")),
		SDPBaseURL:        strings.TrimRight(strings.TrimSpace(getenv("SDP_BASE_URL")), "/"),
		SDPServiceToken:   strings.TrimSpace(getenv("SDP_SERVICE_TOKEN")),
		SDPTelegramField:  strings.TrimSpace(getenv("SDP_TELEGRAM_FIELD")),
		DBPath:            strings.TrimSpace(getenv("DB_PATH")),
		RequestsPerPage:   8,
		BotMode:           BotModePolling,
		WebhookURL:        strings.TrimSpace(getenv("WEBHOOK_URL")),
		WebhookListenAddr: strings.TrimSpace(getenv("WEBHOOK_LISTEN_ADDR")),
		WebhookSecret:     strings.TrimSpace(getenv("WEBHOOK_SECRET")),
		LogLevel:          slog.LevelInfo,
	}

	if cfg.TelegramBotToken == "" {
		return nil, errors.New("config: TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.SDPBaseURL == "" {
		return nil, errors.New("config: SDP_BASE_URL is required")
	}
	if cfg.SDPServiceToken == "" {
		return nil, errors.New("config: SDP_SERVICE_TOKEN is required")
	}
	if cfg.SDPTelegramField == "" {
		return nil, errors.New("config: SDP_TELEGRAM_FIELD is required")
	}

	rawKey := strings.TrimSpace(getenv("TOKEN_ENCRYPTION_KEY"))
	if rawKey == "" {
		return nil, errors.New("config: TOKEN_ENCRYPTION_KEY is required")
	}
	key, err := parseEncryptionKey(rawKey)
	if err != nil {
		return nil, fmt.Errorf("config: invalid TOKEN_ENCRYPTION_KEY: %w", err)
	}
	cfg.TokenEncryptionKey = key

	if cfg.DBPath == "" {
		cfg.DBPath = "./data/bot.db"
	}

	if rawPerPage := strings.TrimSpace(getenv("REQUESTS_PER_PAGE")); rawPerPage != "" {
		n, err := strconv.Atoi(rawPerPage)
		if err != nil || n < 1 || n > 50 {
			return nil, fmt.Errorf("config: REQUESTS_PER_PAGE must be an integer between 1 and 50, got %q", rawPerPage)
		}
		cfg.RequestsPerPage = n
	}

	if rawMode := strings.ToLower(strings.TrimSpace(getenv("BOT_MODE"))); rawMode != "" {
		switch BotMode(rawMode) {
		case BotModePolling, BotModeWebhook:
			cfg.BotMode = BotMode(rawMode)
		default:
			return nil, fmt.Errorf("config: BOT_MODE must be %q or %q, got %q", BotModePolling, BotModeWebhook, rawMode)
		}
	}

	if cfg.WebhookListenAddr == "" {
		cfg.WebhookListenAddr = ":8443"
	}
	if cfg.BotMode == BotModeWebhook && cfg.WebhookURL == "" {
		return nil, errors.New("config: WEBHOOK_URL is required when BOT_MODE=webhook")
	}

	if rawInsecure := strings.TrimSpace(getenv("SDP_INSECURE_TLS")); rawInsecure != "" {
		b, err := strconv.ParseBool(rawInsecure)
		if err != nil {
			return nil, fmt.Errorf("config: invalid SDP_INSECURE_TLS value %q: %w", rawInsecure, err)
		}
		cfg.SDPInsecureTLS = b
	}

	if rawLevel := strings.ToLower(strings.TrimSpace(getenv("LOG_LEVEL"))); rawLevel != "" {
		switch rawLevel {
		case "debug":
			cfg.LogLevel = slog.LevelDebug
		case "info":
			cfg.LogLevel = slog.LevelInfo
		case "warn", "warning":
			cfg.LogLevel = slog.LevelWarn
		case "error":
			cfg.LogLevel = slog.LevelError
		default:
			return nil, fmt.Errorf("config: invalid LOG_LEVEL %q (expected debug, info, warn, error)", rawLevel)
		}
	}

	return cfg, nil
}

// parseEncryptionKey decodes a 32-byte AES-256 key from hex (64 chars) or base64.
func parseEncryptionKey(raw string) ([]byte, error) {
	if len(raw) == 64 {
		if decoded, err := hex.DecodeString(raw); err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}
	if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(raw); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	if len(raw) == 32 {
		return []byte(raw), nil
	}
	return nil, errors.New("must be a 32-byte key encoded as 64 hex characters or base64")
}
