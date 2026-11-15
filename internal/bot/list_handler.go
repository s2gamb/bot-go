package bot

import (
	"context"
	"fmt"
	stdhtml "html"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/s2gamb/bot-go/internal/format"
	"github.com/s2gamb/bot-go/internal/sdp"
)

func (r *Router) handleStartCommand(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	sess, ok := sessionFromContext(ctx)
	if !ok {
		return
	}

	r.renderList(ctx, b, update.Message.Chat.ID, 0, sess, sdp.FilterMyOpen, 1)
}

func (r *Router) handleListCallback(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.CallbackQuery == nil {
		return
	}
	sess, ok := sessionFromContext(ctx)
	if !ok {
		return
	}

	filter, page, valid := DecodeListCallback(update.CallbackQuery.Data)
	if !valid {
		_, _ = b.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
			Text:            "Invalid list request.",
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

	r.renderList(ctx, b, chatID, messageID, sess, filter, page)
}

func (r *Router) handleFilterPickerCallback(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.CallbackQuery == nil {
		return
	}
	currentFilter, page, valid := DecodeFilterPickerCallback(update.CallbackQuery.Data)
	if !valid {
		_, _ = b.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
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

	var rows [][]models.InlineKeyboardButton
	for _, f := range sdp.AllFilters() {
		label := f.Label()
		if f == currentFilter {
			label = "✓ " + label
		}
		rows = append(rows, []models.InlineKeyboardButton{
			{
				Text:         label,
				CallbackData: EncodeListCallback(f, 1),
			},
		})
	}

	rows = append(rows, []models.InlineKeyboardButton{
		{
			Text:         "« Cancel",
			CallbackData: EncodeListCallback(currentFilter, page),
		},
	})

	text := fmt.Sprintf("🔍 <b>Select Ticket Filter</b>\n\nCurrent filter: <b>%s</b>", stdhtml.EscapeString(currentFilter.Label()))
	_, _ = b.EditMessageText(ctx, &tgbot.EditMessageTextParams{
		ChatID:      chatID,
		MessageID:   messageID,
		ParseMode:   models.ParseModeHTML,
		Text:        text,
		ReplyMarkup: &models.InlineKeyboardMarkup{InlineKeyboard: rows},
	})
}

func (r *Router) renderList(
	ctx context.Context,
	b *tgbot.Bot,
	chatID int64,
	editMessageID int,
	sess *authSession,
	filter sdp.FilterKind,
	page int,
) {
	var groupIDs []string
	if filter == sdp.FilterMyGroups {
		var err error
		groupIDs, err = r.store.GetGroupIDsByTechnician(ctx, sess.Record.SDPTechnicianID)
		if err != nil {
			r.logger.Warn("failed to load technician groups from sqlite",
				"sdp_technician_id", sess.Record.SDPTechnicianID,
				"err", err,
			)
		}
	}

	resp, err := r.sdp.ListRequests(ctx, sess.Token, filter, sess.Record.SDPTechnicianID, groupIDs, page, r.requestsPerPage)
	if err != nil {
		r.handleSDPError(ctx, b, chatID, editMessageID, sess.Record.TelegramUserID, err)
		return
	}

	var rows [][]models.InlineKeyboardButton

	// 1. Upper row: Filter Picker button
	rows = append(rows, []models.InlineKeyboardButton{
		{
			Text:         fmt.Sprintf("🔍 Filter: %s ▾", filter.Label()),
			CallbackData: EncodeFilterPickerCallback(filter, page),
		},
	})

	// 2. Middle rows: 1 inline button per request
	for _, req := range resp.Requests {
		rows = append(rows, []models.InlineKeyboardButton{
			{
				Text:         format.FormatButtonLabel(req),
				CallbackData: EncodeDetailCallback(req.ID, filter, page),
			},
		})
	}

	// 3. Bottom row: Simple paginator
	var paginator []models.InlineKeyboardButton
	if page > 1 {
		paginator = append(paginator, models.InlineKeyboardButton{
			Text:         "« Prev",
			CallbackData: EncodeListCallback(filter, page-1),
		})
	}
	paginator = append(paginator, models.InlineKeyboardButton{
		Text:         fmt.Sprintf("Page %d", page),
		CallbackData: CallbackNoop,
	})
	if resp.ListInfo.HasMoreRows {
		paginator = append(paginator, models.InlineKeyboardButton{
			Text:         "Next »",
			CallbackData: EncodeListCallback(filter, page+1),
		})
	}
	rows = append(rows, paginator)

	text := format.FormatListHeader(filter.Label(), page, len(resp.Requests))
	markup := &models.InlineKeyboardMarkup{InlineKeyboard: rows}

	if editMessageID > 0 {
		_, _ = b.EditMessageText(ctx, &tgbot.EditMessageTextParams{
			ChatID:      chatID,
			MessageID:   editMessageID,
			ParseMode:   models.ParseModeHTML,
			Text:        text,
			ReplyMarkup: markup,
		})
	} else {
		_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
			ChatID:      chatID,
			ParseMode:   models.ParseModeHTML,
			Text:        text,
			ReplyMarkup: markup,
		})
	}
}
