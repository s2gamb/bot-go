package bot

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	tgbot "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/s2gamb/bot-go/internal/crypto"
	"github.com/s2gamb/bot-go/internal/sdp"
	"github.com/s2gamb/bot-go/internal/store"
)

func TestCallbackEncodingDecoding(t *testing.T) {
	// List callback
	listData := EncodeListCallback(sdp.FilterUnassigned, 3)
	if listData != "l:un:3" {
		t.Fatalf("unexpected listData: %q", listData)
	}
	f, page, ok := DecodeListCallback(listData)
	if !ok || f != sdp.FilterUnassigned || page != 3 {
		t.Fatalf("DecodeListCallback failed: f=%q page=%d ok=%v", f, page, ok)
	}

	// Filter picker callback
	fpData := EncodeFilterPickerCallback(sdp.FilterMyGroups, 2)
	if fpData != "fp:grp:2" {
		t.Fatalf("unexpected fpData: %q", fpData)
	}
	f2, page2, ok := DecodeFilterPickerCallback(fpData)
	if !ok || f2 != sdp.FilterMyGroups || page2 != 2 {
		t.Fatalf("DecodeFilterPickerCallback failed: f=%q page=%d ok=%v", f2, page2, ok)
	}

	// Detail callback
	detData := EncodeDetailCallback("606", sdp.FilterMyOpen, 1)
	if detData != "d:606:my:1" {
		t.Fatalf("unexpected detData: %q", detData)
	}
	tID, f3, page3, ok := DecodeDetailCallback(detData)
	if !ok || tID != "606" || f3 != sdp.FilterMyOpen || page3 != 1 {
		t.Fatalf("DecodeDetailCallback failed: id=%q f=%q page=%d ok=%v", tID, f3, page3, ok)
	}

	// Action callback
	actData := EncodeActionCallback("notes", "606", sdp.FilterMyOpen, 1)
	if actData != "a:notes:606:my:1" {
		t.Fatalf("unexpected actData: %q", actData)
	}
	actID, tID2, f4, page4, ok := DecodeActionCallback(actData)
	if !ok || actID != "notes" || tID2 != "606" || f4 != sdp.FilterMyOpen || page4 != 1 {
		t.Fatalf("DecodeActionCallback failed: act=%q id=%q f=%q page=%d ok=%v", actID, tID2, f4, page4, ok)
	}
}

func TestRouter_EndToEndFlow(t *testing.T) {
	ctx := context.Background()

	// 1. Setup mock SupportCenter Plus server
	sdpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("authtoken")
		switch r.URL.Path {
		case "/api/v3/technicians":
			if token != "svc-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{
				"response_status":[{"status_code":2000,"status":"success"}],
				"technicians":[{"id":"42","name":"Alice Tech"}]
			}`))
		case "/api/v3/requests":
			if token != "alice-valid-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"response_status":{"status_code":4000,"status":"failed"}}`))
				return
			}
			_, _ = w.Write([]byte(`{
				"response_status":[{"status_code":2000,"status":"success"}],
				"list_info":{"has_more_rows":true,"page":1,"row_count":8},
				"requests":[{"id":"606","subject":"Printer on 3rd floor is offline"}]
			}`))
		case "/api/v3/requests/606":
			if token != "alice-valid-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = w.Write([]byte(`{
				"response_status":{"status_code":2000,"status":"success"},
				"request":{
					"id":"606",
					"subject":"Printer on 3rd floor is offline",
					"description":"<p>Error code <b>E-42</b> <img src='img.png'/></p>",
					"status":{"id":"1","name":"Open"}
				}
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer sdpSrv.Close()

	// 2. Setup mock Telegram Bot API server
	var (
		mu          sync.Mutex
		tgCalls     []string
		lastSentTxt string
	)
	tgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		bodyStr := string(bodyBytes)

		mu.Lock()
		tgCalls = append(tgCalls, r.URL.Path)
		lastSentTxt = bodyStr
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/getMe") {
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":999,"is_bot":true,"first_name":"SDPBot"}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/deleteMessage") || strings.HasSuffix(r.URL.Path, "/answerCallbackQuery") {
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":10,"date":1700000000,"chat":{"id":777,"type":"private"},"text":"ok"}}`))
	}))
	defer tgSrv.Close()

	// 3. Initialize store, sdp service, router, and bot
	db, err := store.OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite failed: %v", err)
	}
	defer db.Close()

	cipher, _ := crypto.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	st := store.NewTechnicianStore(db, cipher)
	sdpSvc, err := sdp.NewService(sdpSrv.URL, "svc-token", "udf_tg_id", false)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewRouter(st, sdpSvc, 8, logger)

	b, err := tgbot.New("123:TESTTOKEN",
		tgbot.WithServerURL(tgSrv.URL),
		tgbot.WithNotAsyncHandlers(),
		tgbot.WithDefaultHandler(router.DefaultHandler),
	)
	if err != nil {
		t.Fatalf("tgbot.New failed: %v", err)
	}
	router.Register(b)

	// Step A: /start before authenticating -> prompts for /auth
	b.ProcessUpdate(ctx, &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:   100,
			From: &models.User{ID: 777},
			Chat: models.Chat{ID: 777, Type: models.ChatTypePrivate},
			Text: "/start",
			Entities: []models.MessageEntity{
				{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: 6},
			},
		},
	})
	mu.Lock()
	if !strings.Contains(lastSentTxt, "/auth") {
		t.Fatalf("expected /auth prompt before linking, got: %s", lastSentTxt)
	}
	mu.Unlock()

	// Step B: /auth alice-valid-token -> deletes message and stores encrypted token
	b.ProcessUpdate(ctx, &models.Update{
		ID: 2,
		Message: &models.Message{
			ID:   101,
			From: &models.User{ID: 777},
			Chat: models.Chat{ID: 777, Type: models.ChatTypePrivate},
			Text: "/auth alice-valid-token",
			Entities: []models.MessageEntity{
				{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: 5},
			},
		},
	})
	mu.Lock()
	if !strings.Contains(lastSentTxt, "Authenticated as") || !strings.Contains(lastSentTxt, "Alice Tech") {
		t.Fatalf("expected authentication confirmation, got: %s", lastSentTxt)
	}
	mu.Unlock()

	// Step C: /start after authenticating -> renders ticket list with #606
	b.ProcessUpdate(ctx, &models.Update{
		ID: 3,
		Message: &models.Message{
			ID:   102,
			From: &models.User{ID: 777},
			Chat: models.Chat{ID: 777, Type: models.ChatTypePrivate},
			Text: "/start",
			Entities: []models.MessageEntity{
				{Type: models.MessageEntityTypeBotCommand, Offset: 0, Length: 6},
			},
		},
	})
	mu.Lock()
	if !strings.Contains(lastSentTxt, "My Open Tickets") || !strings.Contains(lastSentTxt, "#606") {
		t.Fatalf("expected ticket list with #606, got: %s", lastSentTxt)
	}
	mu.Unlock()

	// Step D: Click ticket #606 detail callback ("d:606:my:1")
	var mim models.MaybeInaccessibleMessage
	_ = json.Unmarshal([]byte(`{"message_id":10,"date":1700000000,"chat":{"id":777,"type":"private"}}`), &mim)

	b.ProcessUpdate(ctx, &models.Update{
		ID: 4,
		CallbackQuery: &models.CallbackQuery{
			ID:      "cb-1",
			From:    models.User{ID: 777},
			Message: mim,
			Data:    "d:606:my:1",
		},
	})
	mu.Lock()
	if !strings.Contains(lastSentTxt, "E-42") || !strings.Contains(lastSentTxt, "[Image]") {
		t.Fatalf("expected ticket detail with sanitized description and [Image], got: %s", lastSentTxt)
	}
	mu.Unlock()
}
