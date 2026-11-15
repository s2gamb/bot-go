package bot

import (
	"context"
	"errors"
	"fmt"
	stdhtml "html"
	"strings"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/s2gamb/bot-go/internal/sdp"
	"github.com/s2gamb/bot-go/internal/store"
	sdpgo "github.com/s2gamb/sdp-go"
)

func (r *Router) handleAuthCommand(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}

	chatID := update.Message.Chat.ID
	userID := update.Message.From.ID
	msgID := update.Message.ID
	text := strings.TrimSpace(update.Message.Text)

	// Always attempt to delete the user's /auth message immediately so the raw API token is not left in chat history.
	if _, err := b.DeleteMessage(ctx, &tgbot.DeleteMessageParams{
		ChatID:    chatID,
		MessageID: msgID,
	}); err != nil {
		r.logger.Debug("could not delete /auth message", "chat_id", chatID, "message_id", msgID, "err", err)
	}

	token := extractCommandArg(text)
	if token == "" {
		_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID:    chatID,
			ParseMode: models.ParseModeHTML,
			Text:      "🔑 <b>Usage:</b> <code>/auth &lt;your_supportcenter_api_token&gt;</code>\n\n<i>Your message will be automatically deleted for security.</i>",
		})
		return
	}

	// Step 1: Verify that this Telegram User ID is registered on a Technician in SupportCenter Plus.
	tech, err := r.sdp.FindTechnicianByTelegramID(ctx, userID)
	if err != nil {
		if errors.Is(err, sdpgo.ErrTechnicianNotFound) {
			r.logger.Warn("unauthorized telegram user attempted /auth", "telegram_user_id", userID)
			_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
				ChatID:    chatID,
				ParseMode: models.ParseModeHTML,
				Text:      fmt.Sprintf("⛔ Access denied. Your Telegram ID (<code>%d</code>) is not linked to any technician profile in SupportCenter Plus.", userID),
			})
			return
		}
		if errors.Is(err, sdpgo.ErrMultipleTechniciansFound) {
			r.logger.Error("multiple technicians matched telegram user id", "telegram_user_id", userID)
			_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
				ChatID:    chatID,
				ParseMode: models.ParseModeHTML,
				Text:      "⚠️ Multiple technician profiles in SupportCenter Plus share your Telegram ID. Please contact an administrator.",
			})
			return
		}
		r.logger.Error("failed to lookup technician in SupportCenter", "telegram_user_id", userID, "err", err)
		_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID:    chatID,
			ParseMode: models.ParseModeHTML,
			Text:      "⚠️ Could not verify your identity with SupportCenter Plus. Please try again later.",
		})
		return
	}

	// Step 2: Validate the technician's own API token against SupportCenter Plus.
	if err := r.sdp.ValidateTechnicianToken(ctx, token); err != nil {
		r.logger.Warn("technician provided invalid API token", "telegram_user_id", userID, "sdp_tech_id", tech.ID, "err", err)
		_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID:    chatID,
			ParseMode: models.ParseModeHTML,
			Text:      "❌ The provided SupportCenter Plus API token was rejected. Please check your token and try <code>/auth &lt;token&gt;</code> again.",
		})
		return
	}

	// Step 3: Encrypt and persist in SQLite.
	techName := strings.TrimSpace(tech.Name)
	if techName == "" {
		techName = "Technician #" + tech.ID
	}

	if err := r.store.Upsert(ctx, store.TechnicianRecord{
		TelegramUserID:    userID,
		SDPTechnicianID:   tech.ID,
		SDPTechnicianName: techName,
	}, token); err != nil {
		r.logger.Error("failed to store encrypted technician token", "telegram_user_id", userID, "err", err)
		_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID:    chatID,
			ParseMode: models.ParseModeHTML,
			Text:      "⚠️ Failed to save your encrypted token. Please try again.",
		})
		return
	}

	r.logger.Info("technician authenticated", "telegram_user_id", userID, "sdp_tech_id", tech.ID, "name", techName)

	_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:    chatID,
		ParseMode: models.ParseModeHTML,
		Text: fmt.Sprintf(
			"✅ Authenticated as <b>%s</b> (ID: <code>%s</code>).\n\nUse /start or the button below to browse your tickets.",
			stdhtml.EscapeString(techName),
			stdhtml.EscapeString(tech.ID),
		),
		ReplyMarkup: &models.InlineKeyboardMarkup{
			InlineKeyboard: [][]models.InlineKeyboardButton{
				{
					{
						Text:         "📋 Open My Tickets",
						CallbackData: EncodeListCallback(sdp.FilterMyOpen, 1),
					},
				},
			},
		},
	})
}

func (r *Router) handleLogoutCommand(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.Message == nil || update.Message.From == nil {
		return
	}
	chatID := update.Message.Chat.ID
	userID := update.Message.From.ID

	err := r.store.Delete(ctx, userID)
	if errors.Is(err, store.ErrNotFound) {
		_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID:    chatID,
			ParseMode: models.ParseModeHTML,
			Text:      "ℹ️ You do not have a linked API token stored.",
		})
		return
	}
	if err != nil {
		r.logger.Error("failed to delete technician token", "telegram_user_id", userID, "err", err)
		_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID:    chatID,
			ParseMode: models.ParseModeHTML,
			Text:      "⚠️ Failed to remove your stored token.",
		})
		return
	}

	r.logger.Info("technician logged out", "telegram_user_id", userID)
	_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:    chatID,
		ParseMode: models.ParseModeHTML,
		Text:      "🔒 Your stored SupportCenter Plus API token has been removed. Use <code>/auth &lt;token&gt;</code> to link again.",
	})
}

func (r *Router) handleHelpCommand(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:    update.Message.Chat.ID,
		ParseMode: models.ParseModeHTML,
		Text: "🤖 <b>SupportCenter Plus Bot</b>\n\n" +
			"<b>Commands:</b>\n" +
			"• <code>/start</code> — Browse tickets (filter &amp; paginate via inline buttons)\n" +
			"• <code>/auth &lt;token&gt;</code> — Link your personal SupportCenter API token\n" +
			"• <code>/logout</code> — Remove your stored API token\n" +
			"• <code>/help</code> — Show this help message",
	})
}

func (r *Router) sendAuthPrompt(ctx context.Context, b *tgbot.Bot, chatID, userID int64) {
	// Check if their Telegram ID is recognized in SupportCenter first so unauthorized users get a clear rejection.
	tech, err := r.sdp.FindTechnicianByTelegramID(ctx, userID)
	if err != nil {
		if errors.Is(err, sdpgo.ErrTechnicianNotFound) {
			_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
				ChatID:    chatID,
				ParseMode: models.ParseModeHTML,
				Text:      fmt.Sprintf("⛔ Access denied. Your Telegram ID (<code>%d</code>) is not registered on any technician profile in SupportCenter Plus.", userID),
			})
			return
		}
		r.logger.Warn("could not verify technician during auth prompt", "telegram_user_id", userID, "err", err)
	}

	greeting := "👋 Welcome!"
	if tech != nil && strings.TrimSpace(tech.Name) != "" {
		greeting = fmt.Sprintf("👋 Hello, <b>%s</b>!", stdhtml.EscapeString(strings.TrimSpace(tech.Name)))
	}

	_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
		ChatID:    chatID,
		ParseMode: models.ParseModeHTML,
		Text: greeting + "\n\nPlease link your SupportCenter Plus API key by sending:\n" +
			"<code>/auth &lt;your_api_token&gt;</code>\n\n" +
			"<i>Your message will be deleted immediately and the token stored encrypted.</i>",
	})
}

func extractCommandArg(text string) string {
	fields := strings.Fields(text)
	if len(fields) < 2 {
		return ""
	}
	return strings.TrimSpace(strings.Join(fields[1:], " "))
}
