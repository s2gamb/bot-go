package bot

import (
	"context"
	"errors"
	"log/slog"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/s2gamb/bot-go/internal/sdp"
	"github.com/s2gamb/bot-go/internal/store"
)

type authSession struct {
	Record *store.TechnicianRecord
	Token  string
}

type authCtxKey struct{}

// Router coordinates Telegram command and callback handlers, DM enforcement, and auth middleware.
type Router struct {
	store           *store.TechnicianStore
	sdp             *sdp.Service
	requestsPerPage int
	logger          *slog.Logger
	actionProviders map[string]DetailActionProvider
	actionOrder     []DetailActionProvider
}

// NewRouter creates a new Router and registers all handlers onto the provided *tgbot.Bot.
func NewRouter(
	st *store.TechnicianStore,
	sdpSvc *sdp.Service,
	requestsPerPage int,
	logger *slog.Logger,
	providers ...DetailActionProvider,
) *Router {
	if requestsPerPage < 1 {
		requestsPerPage = 8
	}
	if logger == nil {
		logger = slog.Default()
	}

	r := &Router{
		store:           st,
		sdp:             sdpSvc,
		requestsPerPage: requestsPerPage,
		logger:          logger,
		actionProviders: make(map[string]DetailActionProvider, len(providers)),
		actionOrder:     providers,
	}
	for _, p := range providers {
		if p != nil && p.ID() != "" {
			r.actionProviders[p.ID()] = p
		}
	}
	return r
}

// Register binds all command and callback handlers on b.
func (r *Router) Register(b *tgbot.Bot) {
	// Public DM commands
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "auth", tgbot.MatchTypeCommand, r.dmOnly(r.handleAuthCommand))
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "logout", tgbot.MatchTypeCommand, r.dmOnly(r.handleLogoutCommand))
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "help", tgbot.MatchTypeCommand, r.dmOnly(r.handleHelpCommand))

	// Protected DM commands
	b.RegisterHandler(tgbot.HandlerTypeMessageText, "start", tgbot.MatchTypeCommand, r.dmOnly(r.requireAuth(r.handleStartCommand)))

	// Callback queries
	b.RegisterHandler(tgbot.HandlerTypeCallbackQueryData, CallbackNoop, tgbot.MatchTypeExact, r.dmOnly(r.handleNoopCallback))
	b.RegisterHandler(tgbot.HandlerTypeCallbackQueryData, PrefixList, tgbot.MatchTypePrefix, r.dmOnly(r.requireAuth(r.handleListCallback)))
	b.RegisterHandler(tgbot.HandlerTypeCallbackQueryData, PrefixFilterPicker, tgbot.MatchTypePrefix, r.dmOnly(r.requireAuth(r.handleFilterPickerCallback)))
	b.RegisterHandler(tgbot.HandlerTypeCallbackQueryData, PrefixDetail, tgbot.MatchTypePrefix, r.dmOnly(r.requireAuth(r.handleDetailCallback)))
	b.RegisterHandler(tgbot.HandlerTypeCallbackQueryData, PrefixDetailAction, tgbot.MatchTypePrefix, r.dmOnly(r.requireAuth(r.handleDetailActionCallback)))
}

// DefaultHandler handles unmatched updates gracefully in private DMs.
func (r *Router) DefaultHandler(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if !isPrivateUpdate(update) {
		return
	}
	if update.CallbackQuery != nil {
		_, _ = b.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
		})
	}
}

// dmOnly ensures the handler only runs for private 1-on-1 DMs.
func (r *Router) dmOnly(next tgbot.HandlerFunc) tgbot.HandlerFunc {
	return func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
		if !isPrivateUpdate(update) {
			if update.CallbackQuery != nil {
				_, _ = b.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
					CallbackQueryID: update.CallbackQuery.ID,
					Text:            "This bot only operates in private DMs.",
					ShowAlert:       true,
				})
			}
			return
		}
		next(ctx, b, update)
	}
}

// requireAuth verifies that the user has a stored encrypted token in SQLite before running next.
func (r *Router) requireAuth(next tgbot.HandlerFunc) tgbot.HandlerFunc {
	return func(ctx context.Context, b *tgbot.Bot, update *models.Update) {
		userID, chatID, ok := extractUserAndChatID(update)
		if !ok {
			return
		}

		rec, token, err := r.store.GetWithToken(ctx, userID)
		if err != nil {
			if update.CallbackQuery != nil {
				_, _ = b.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
					CallbackQueryID: update.CallbackQuery.ID,
					Text:            "Authentication required. Use /auth <token>.",
					ShowAlert:       true,
				})
			}
			if errors.Is(err, store.ErrNotFound) {
				r.sendAuthPrompt(ctx, b, chatID, userID)
				return
			}
			r.logger.Error("failed to load technician token from store", "telegram_user_id", userID, "err", err)
			_, _ = b.SendMessage(ctx, &tgbot.SendMessageParams{
				ChatID:    chatID,
				ParseMode: models.ParseModeHTML,
				Text:      "⚠️ Failed to read your stored credentials. Please re-authenticate using <code>/auth &lt;your_api_token&gt;</code>.",
			})
			return
		}

		ctxWithSession := context.WithValue(ctx, authCtxKey{}, &authSession{
			Record: rec,
			Token:  token,
		})
		next(ctxWithSession, b, update)
	}
}

func sessionFromContext(ctx context.Context) (*authSession, bool) {
	sess, ok := ctx.Value(authCtxKey{}).(*authSession)
	return sess, ok && sess != nil
}

func isPrivateUpdate(update *models.Update) bool {
	if update == nil {
		return false
	}
	if update.Message != nil {
		return update.Message.Chat.Type == models.ChatTypePrivate && update.Message.From != nil
	}
	if update.CallbackQuery != nil {
		chat, ok := extractCallbackChat(update.CallbackQuery.Message)
		return ok && chat.Type == models.ChatTypePrivate
	}
	return false
}

func extractUserAndChatID(update *models.Update) (userID int64, chatID int64, ok bool) {
	if update.Message != nil && update.Message.From != nil {
		return update.Message.From.ID, update.Message.Chat.ID, true
	}
	if update.CallbackQuery != nil {
		chat, chatOK := extractCallbackChat(update.CallbackQuery.Message)
		if !chatOK {
			return 0, 0, false
		}
		return update.CallbackQuery.From.ID, chat.ID, true
	}
	return 0, 0, false
}

func extractCallbackChat(mim models.MaybeInaccessibleMessage) (models.Chat, bool) {
	if mim.Message != nil {
		return mim.Message.Chat, true
	}
	if mim.InaccessibleMessage != nil {
		return mim.InaccessibleMessage.Chat, true
	}
	return models.Chat{}, false
}

func extractCallbackChatAndMessageID(mim models.MaybeInaccessibleMessage) (chatID int64, messageID int, ok bool) {
	if mim.Message != nil {
		return mim.Message.Chat.ID, mim.Message.ID, true
	}
	if mim.InaccessibleMessage != nil {
		return mim.InaccessibleMessage.Chat.ID, mim.InaccessibleMessage.MessageID, true
	}
	return 0, 0, false
}

func (r *Router) handleNoopCallback(ctx context.Context, b *tgbot.Bot, update *models.Update) {
	if update.CallbackQuery != nil {
		_, _ = b.AnswerCallbackQuery(ctx, &tgbot.AnswerCallbackQueryParams{
			CallbackQueryID: update.CallbackQuery.ID,
		})
	}
}
