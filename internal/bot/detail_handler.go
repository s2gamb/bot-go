package bot

import (
	"context"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/s2gamb/bot-go/internal/format"
	"github.com/s2gamb/bot-go/internal/sdp"
	"github.com/s2gamb/bot-go/internal/store"
)

// DetailActionProvider defines an extension point for pluggable sub-dialogs on the ticket detail view
// (e.g., Tasks, Notes & Conversations, Time Entries, Resolution, Image Viewing in v2).
type DetailActionProvider interface {
	// ID returns a short unique identifier used in callback_data ("a:<ID>:<ticketID>:<filter>:<page>").
	ID() string
	// Button returns the inline button to display on the ticket detail view, or nil if not applicable.
	Button(ticketID string, filter sdp.FilterKind, page int) *models.InlineKeyboardButton
	// Handle processes the callback when the technician clicks the provider's button.
	Handle(
		ctx context.Context,
		b *tgbot.Bot,
		cq *models.CallbackQuery,
		tech *store.TechnicianRecord,
		token string,
		ticketID string,
		filter sdp.FilterKind,
		page int,
	)
}

func (r *Router) handleDetailCallback(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.CallbackQuery == nil {
		return
	}
	sess, ok := sessionFromContext(ctx)
	if !ok {
		return
	}

	ticketID, filter, page, valid := DecodeDetailCallback(update.CallbackQuery.Data)
	if !valid {
		_, _ = b.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            "Invalid ticket request.",
		})
		return
	}

	chatID, messageID, msgOK := extractCallbackChatAndMessageID(update.CallbackQuery.Message)
	if !msgOK {
		return
	}

	_, _ = b.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
		CallbackQueryID: update.CallbackQuery.ID,
	})

	req, err := r.sdp.GetRequest(ctx, sess.Token, ticketID)
	if err != nil {
		r.handleSDPError(ctx, b, chatID, messageID, sess.Record.TelegramUserID, err)
		return
	}

	text := format.FormatTicketDetail(req, r.sdp.TicketWebURL(req.ID))

	var rows [][]models.InlineKeyboardButton

	// Pluggable v2 action providers (grouped up to 2 buttons per row)
	var actionRow []models.InlineKeyboardButton
	for _, provider := range r.actionOrder {
		if provider == nil {
			continue
		}
		btn := provider.Button(req.ID, filter, page)
		if btn == nil {
			continue
		}
		actionRow = append(actionRow, *btn)
		if len(actionRow) == 2 {
			rows = append(rows, actionRow)
			actionRow = nil
		}
	}
	if len(actionRow) > 0 {
		rows = append(rows, actionRow)
	}

	// Bottom navigation row
	rows = append(rows, []models.InlineKeyboardButton{
		{
			Text:         "🔄 Refresh",
			CallbackData: EncodeDetailCallback(req.ID, filter, page),
		},
		{
			Text:         "« Back to List",
			CallbackData: EncodeListCallback(filter, page),
		},
	})

	_, _ = b.EditMessageText(ctx, &tgbot.EditMessageTextParams{
		ChatID:    chatID,
		MessageID: messageID,
		ParseMode: models.ParseModeHTML,
		Text:      text,
		LinkPreviewOptions: &models.LinkPreviewOptions{
			IsDisabled: tgbot.True(),
		},
		ReplyMarkup: &models.InlineKeyboardMarkup{InlineKeyboard: rows},
	})
}

func (r *Router) handleDetailActionCallback(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.CallbackQuery == nil {
		return
	}
	sess, ok := sessionFromContext(ctx)
	if !ok {
		return
	}

	actionID, ticketID, filter, page, valid := DecodeActionCallback(update.CallbackQuery.Data)
	if !valid {
		_, _ = b.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
		})
		return
	}

	provider, exists := r.actionProviders[actionID]
	if !exists {
		_, _ = b.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            "This module is not enabled.",
			ShowAlert:       true,
		})
		return
	}

	provider.Handle(ctx, b, update.CallbackQuery, sess.Record, sess.Token, ticketID, filter, page)
}

func (r *Router) handleSDPError(ctx context.Context, b *tgbot.Bot, chatID int64, editMessageID int, userID int64, err error) {
	r.logger.Error("SupportCenter API error", "telegram_user_id", userID, "err", err)

	msg := "⚠️ Failed to fetch data from SupportCenter Plus. Please try again."
	if sdp.IsAuthError(err) {
		msg = "🔒 Your SupportCenter Plus API token was rejected or lacks permission.\n\nPlease re-authenticate with <code>/auth &lt;new_token&gt;</code>."
	}

	if editMessageID > 0 {
		_, _ = b.EditMessageText(ctx, &tgbot.EditMessageTextParams{
			ChatID:    chatID,
			MessageID: editMessageID,
			ParseMode: models.ParseModeHTML,
			Text:      msg,
		})
	} else {
		_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID:    chatID,
			ParseMode: models.ParseModeHTML,
			Text:      msg,
		})
	}
}
