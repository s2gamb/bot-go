package store

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/s2gamb/bot-go/internal/crypto"
)

func TestTechnicianStore_CRUDAndEncryptionAtRest(t *testing.T) {
	ctx := context.Background()
	db, err := OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatalf("OpenSQLite failed: %v", err)
	}
	defer db.Close()

	cipher, err := crypto.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewCipher failed: %v", err)
	}

	st := NewTechnicianStore(db, cipher)

	// NotFound initially
	if _, _, err := st.GetWithToken(ctx, 1001); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	// Upsert
	rawToken := "sdp-secret-token-xyz-987"
	err = st.Upsert(ctx, TechnicianRecord{
		TelegramUserID:    1001,
		SDPTechnicianID:   "42",
		SDPTechnicianName: "Alice Tech",
	}, rawToken)
	if err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Verify raw token is encrypted at rest in SQLite
	var rawBlob []byte
	if err := db.QueryRowContext(ctx, "SELECT encrypted_token FROM technicians WHERE telegram_user_id = 1001").Scan(&rawBlob); err != nil {
		t.Fatalf("query raw blob failed: %v", err)
	}
	if bytes.Contains(rawBlob, []byte(rawToken)) {
		t.Fatal("raw token found in plaintext inside SQLite encrypted_token column!")
	}

	// GetWithToken
	rec, gotToken, err := st.GetWithToken(ctx, 1001)
	if err != nil {
		t.Fatalf("GetWithToken failed: %v", err)
	}
	if rec.SDPTechnicianID != "42" || rec.SDPTechnicianName != "Alice Tech" || gotToken != rawToken {
		t.Fatalf("unexpected record/token: %+v, token=%q", rec, gotToken)
	}

	// Upsert overwrite (re-auth)
	newToken := "sdp-rotated-token-111"
	err = st.Upsert(ctx, TechnicianRecord{
		TelegramUserID:    1001,
		SDPTechnicianID:   "42",
		SDPTechnicianName: "Alice Senior Tech",
	}, newToken)
	if err != nil {
		t.Fatalf("Upsert update failed: %v", err)
	}

	rec2, gotToken2, err := st.GetWithToken(ctx, 1001)
	if err != nil {
		t.Fatalf("GetWithToken after update failed: %v", err)
	}
	if rec2.SDPTechnicianName != "Alice Senior Tech" || gotToken2 != newToken {
		t.Fatalf("unexpected updated record/token: %+v, token=%q", rec2, gotToken2)
	}

	// Delete
	if err := st.Delete(ctx, 1001); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if _, _, err := st.GetWithToken(ctx, 1001); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if err := st.Delete(ctx, 1001); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on second delete, got %v", err)
	}
}
