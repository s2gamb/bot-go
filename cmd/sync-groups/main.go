package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/s2gamb/bot-go/internal/sdp"
	"github.com/s2gamb/bot-go/internal/store"
)

func main() {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("SDP_BASE_URL")), "/")
	serviceToken := strings.TrimSpace(os.Getenv("SDP_SERVICE_TOKEN"))
	dbPath := strings.TrimSpace(os.Getenv("DB_PATH"))
	if dbPath == "" {
		dbPath = "./data/bot.db"
	}

	var insecureTLS bool
	if raw := strings.TrimSpace(os.Getenv("SDP_INSECURE_TLS")); raw != "" {
		v, err := strconv.ParseBool(raw)
		if err != nil {
			log.Fatalf("invalid SDP_INSECURE_TLS %q: %v", raw, err)
		}
		insecureTLS = v
	}

	if baseURL == "" {
		log.Fatal("SDP_BASE_URL is required")
	}
	if serviceToken == "" {
		log.Fatal("SDP_SERVICE_TOKEN is required")
	}

	telegramField := strings.TrimSpace(os.Getenv("SDP_TELEGRAM_FIELD"))
	if telegramField == "" {
		telegramField = "udf_tg_id"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sdpSvc, err := sdp.NewService(baseURL, serviceToken, telegramField, insecureTLS)
	if err != nil {
		log.Fatalf("init sdp service: %v", err)
	}

	mappings, err := sdpSvc.FetchTechnicianGroups(ctx)
	if err != nil {
		log.Fatalf("fetch technician groups: %v", err)
	}

	db, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		log.Fatalf("open sqlite %q: %v", dbPath, err)
	}
	defer func() {
		_ = db.Close()
	}()

	rows := make([]store.TechnicianGroup, 0, len(mappings))
	groupsSeen := make(map[string]struct{})
	techsSeen := make(map[string]struct{})
	for _, m := range mappings {
		rows = append(rows, store.TechnicianGroup{
			SDPTechnicianID:  m.SDPTechnicianID,
			SupportGroupID:   m.SupportGroupID,
			SupportGroupName: m.SupportGroupName,
		})
		groupsSeen[m.SupportGroupID] = struct{}{}
		techsSeen[m.SDPTechnicianID] = struct{}{}
	}

	if err := store.NewGroupStore(db).ReplaceAll(ctx, rows); err != nil {
		log.Fatalf("save technician groups: %v", err)
	}

	fmt.Printf("Synced %d technician-group memberships (%d technicians across %d support groups) into %s\n",
		len(rows), len(techsSeen), len(groupsSeen), dbPath)
}
