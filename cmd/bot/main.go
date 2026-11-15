package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	tgbot "github.com/go-telegram/bot"
	"github.com/s2gamb/bot-go/internal/bot"
	"github.com/s2gamb/bot-go/internal/config"
	"github.com/s2gamb/bot-go/internal/crypto"
	"github.com/s2gamb/bot-go/internal/sdp"
	"github.com/s2gamb/bot-go/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.LogLevel,
	}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cipher, err := crypto.NewCipher(cfg.TokenEncryptionKey)
	if err != nil {
		return fmt.Errorf("init cipher: %w", err)
	}

	db, err := store.OpenSQLite(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("init sqlite: %w", err)
	}
	defer func() {
		_ = db.Close()
	}()

	techStore := store.NewTechnicianStore(db, cipher)

	sdpSvc, err := sdp.NewService(
		cfg.SDPBaseURL,
		cfg.SDPServiceToken,
		cfg.SDPTelegramField,
		cfg.SDPInsecureTLS,
	)
	if err != nil {
		return fmt.Errorf("init sdp service: %w", err)
	}

	syncGroupsOnStartup(ctx, db, sdpSvc, logger)

	router := bot.NewRouter(techStore, sdpSvc, cfg.RequestsPerPage, logger)

	opts := []tgbot.Option{
		tgbot.WithDefaultHandler(router.DefaultHandler),
		tgbot.WithErrorsHandler(func(err error) {
			logger.Error("telegram bot error", "err", err)
		}),
	}
	if cfg.WebhookSecret != "" {
		opts = append(opts, tgbot.WithWebhookSecretToken(cfg.WebhookSecret))
	}

	b, err := tgbot.New(cfg.TelegramBotToken, opts...)
	if err != nil {
		return fmt.Errorf("init telegram bot: %w", err)
	}
	router.Register(b)

	switch cfg.BotMode {
	case config.BotModeWebhook:
		return runWebhook(ctx, b, cfg, logger)
	default:
		return runPolling(ctx, b, logger)
	}
}

func runPolling(ctx context.Context, b *tgbot.Bot, logger *slog.Logger) error {
	if _, err := b.DeleteWebhook(ctx, &tgbot.DeleteWebhookParams{}); err != nil {
		logger.Warn("could not delete existing webhook before polling", "err", err)
	}
	logger.Info("starting bot in long-polling mode")
	b.Start(ctx)
	logger.Info("bot stopped")
	return nil
}

func runWebhook(ctx context.Context, b *tgbot.Bot, cfg *config.Config, logger *slog.Logger) error {
	if _, err := b.SetWebhook(ctx, &tgbot.SetWebhookParams{
		URL:         cfg.WebhookURL,
		SecretToken: cfg.WebhookSecret,
	}); err != nil {
		return fmt.Errorf("set telegram webhook: %w", err)
	}

	go b.StartWebhook(ctx)

	srv := &http.Server{
		Addr:              cfg.WebhookListenAddr,
		Handler:           b.WebhookHandler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("starting webhook HTTP server", "addr", cfg.WebhookListenAddr, "url", cfg.WebhookURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		logger.Info("webhook server stopped")
		return nil
	case err := <-errCh:
		return fmt.Errorf("webhook server failed: %w", err)
	}
}

func syncGroupsOnStartup(ctx context.Context, db *sql.DB, sdpSvc *sdp.Service, logger *slog.Logger) {
	syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	mappings, err := sdpSvc.FetchTechnicianGroups(syncCtx)
	if err != nil {
		logger.Warn("startup group sync failed; 'All My Groups' filter may be stale or empty", "err", err)
		return
	}

	rows := make([]store.TechnicianGroup, 0, len(mappings))
	for _, m := range mappings {
		rows = append(rows, store.TechnicianGroup{
			SDPTechnicianID:  m.SDPTechnicianID,
			SupportGroupID:   m.SupportGroupID,
			SupportGroupName: m.SupportGroupName,
		})
	}

	if err := store.NewGroupStore(db).ReplaceAll(syncCtx, rows); err != nil {
		logger.Warn("failed to persist startup group sync", "err", err)
		return
	}

	logger.Info("synced technician support groups on startup", "memberships", len(rows))
}
