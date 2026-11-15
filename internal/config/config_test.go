package config

import (
	"encoding/base64"
	"encoding/hex"
	"log/slog"
	"testing"
)

func TestLoadFromEnv_DefaultsAndValidKeys(t *testing.T) {
	hexKey := hex.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	env := map[string]string{
		"TELEGRAM_BOT_TOKEN":   "123:ABC",
		"SDP_BASE_URL":         "https://sdp.local/",
		"SDP_SERVICE_TOKEN":    "svc-token",
		"SDP_TELEGRAM_FIELD":   "udf_sline_tg_id",
		"TOKEN_ENCRYPTION_KEY": hexKey,
	}

	cfg, err := LoadFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.SDPBaseURL != "https://sdp.local" {
		t.Errorf("expected trailing slash trimmed, got %q", cfg.SDPBaseURL)
	}
	if cfg.DBPath != "./data/bot.db" {
		t.Errorf("expected default DBPath, got %q", cfg.DBPath)
	}
	if cfg.RequestsPerPage != 8 {
		t.Errorf("expected default RequestsPerPage 8, got %d", cfg.RequestsPerPage)
	}
	if cfg.BotMode != BotModePolling {
		t.Errorf("expected default BotModePolling, got %q", cfg.BotMode)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("expected LevelInfo, got %v", cfg.LogLevel)
	}

	// Test base64 key + webhook mode
	env["TOKEN_ENCRYPTION_KEY"] = base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	env["BOT_MODE"] = "webhook"
	env["WEBHOOK_URL"] = "https://bot.example.com/webhook"
	env["REQUESTS_PER_PAGE"] = "15"
	env["SDP_INSECURE_TLS"] = "true"
	env["LOG_LEVEL"] = "debug"

	cfg2, err := LoadFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatalf("unexpected error with base64/webhook: %v", err)
	}
	if cfg2.BotMode != BotModeWebhook || cfg2.RequestsPerPage != 15 || !cfg2.SDPInsecureTLS || cfg2.LogLevel != slog.LevelDebug {
		t.Errorf("unexpected parsed config: %+v", cfg2)
	}
}

func TestLoadFromEnv_ValidationErrors(t *testing.T) {
	base := map[string]string{
		"TELEGRAM_BOT_TOKEN":   "123:ABC",
		"SDP_BASE_URL":         "https://sdp.local",
		"SDP_SERVICE_TOKEN":    "svc-token",
		"SDP_TELEGRAM_FIELD":   "udf_sline_tg_id",
		"TOKEN_ENCRYPTION_KEY": "0123456789abcdef0123456789abcdef",
	}

	cases := []struct {
		name     string
		override map[string]string
	}{
		{"missing bot token", map[string]string{"TELEGRAM_BOT_TOKEN": ""}},
		{"missing base url", map[string]string{"SDP_BASE_URL": ""}},
		{"missing service token", map[string]string{"SDP_SERVICE_TOKEN": ""}},
		{"missing tg field", map[string]string{"SDP_TELEGRAM_FIELD": ""}},
		{"bad encryption key", map[string]string{"TOKEN_ENCRYPTION_KEY": "too-short"}},
		{"bad requests per page", map[string]string{"REQUESTS_PER_PAGE": "0"}},
		{"bad bot mode", map[string]string{"BOT_MODE": "invalid"}},
		{"webhook without url", map[string]string{"BOT_MODE": "webhook"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadFromEnv(func(k string) string {
				if v, ok := tc.override[k]; ok {
					return v
				}
				return base[k]
			})
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
		})
	}
}
