package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/s2gamb/bot-go/internal/crypto"
)

// ErrNotFound is returned when a technician record is not found in the local database.
var ErrNotFound = errors.New("store: technician not found")

// TechnicianRecord represents a linked Telegram user and SupportCenter technician.
type TechnicianRecord struct {
	TelegramUserID    int64
	SDPTechnicianID   string
	SDPTechnicianName string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// TechnicianStore manages persistence of technician links and encrypted tokens in SQLite.
type TechnicianStore struct {
	db     *sql.DB
	cipher *crypto.Cipher
}

// NewTechnicianStore creates a new TechnicianStore backed by db and encrypted with cipher.
func NewTechnicianStore(db *sql.DB, cipher *crypto.Cipher) *TechnicianStore {
	return &TechnicianStore{
		db:     db,
		cipher: cipher,
	}
}

// Upsert stores or updates a technician record and encrypts their rawToken at rest.
func (s *TechnicianStore) Upsert(ctx context.Context, rec TechnicianRecord, rawToken string) error {
	if rec.TelegramUserID == 0 {
		return errors.New("store: telegram_user_id is required")
	}
	if rec.SDPTechnicianID == "" {
		return errors.New("store: sdp_technician_id is required")
	}
	if rawToken == "" {
		return errors.New("store: rawToken is required")
	}

	ciphertext, nonce, err := s.cipher.Encrypt([]byte(rawToken))
	if err != nil {
		return fmt.Errorf("store: encrypt token: %w", err)
	}

	const q = `
INSERT INTO technicians (
    telegram_user_id, sdp_technician_id, sdp_technician_name, encrypted_token, nonce, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
ON CONFLICT(telegram_user_id) DO UPDATE SET
    sdp_technician_id   = excluded.sdp_technician_id,
    sdp_technician_name = excluded.sdp_technician_name,
    encrypted_token     = excluded.encrypted_token,
    nonce               = excluded.nonce,
    updated_at          = CURRENT_TIMESTAMP;
`
	if _, err := s.db.ExecContext(ctx, q,
		rec.TelegramUserID,
		rec.SDPTechnicianID,
		rec.SDPTechnicianName,
		ciphertext,
		nonce,
	); err != nil {
		return fmt.Errorf("store: upsert technician %d: %w", rec.TelegramUserID, err)
	}

	return nil
}

// GetWithToken retrieves a TechnicianRecord and decrypts their stored SupportCenter authtoken.
func (s *TechnicianStore) GetWithToken(ctx context.Context, telegramUserID int64) (*TechnicianRecord, string, error) {
	const q = `
SELECT telegram_user_id, sdp_technician_id, sdp_technician_name, encrypted_token, nonce, created_at, updated_at
FROM technicians
WHERE telegram_user_id = ?;
`
	var (
		rec        TechnicianRecord
		ciphertext []byte
		nonce      []byte
	)

	err := s.db.QueryRowContext(ctx, q, telegramUserID).Scan(
		&rec.TelegramUserID,
		&rec.SDPTechnicianID,
		&rec.SDPTechnicianName,
		&ciphertext,
		&nonce,
		&rec.CreatedAt,
		&rec.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("store: query technician %d: %w", telegramUserID, err)
	}

	plaintext, err := s.cipher.Decrypt(ciphertext, nonce)
	if err != nil {
		return nil, "", fmt.Errorf("store: decrypt token for %d: %w", telegramUserID, err)
	}

	return &rec, string(plaintext), nil
}

// Delete removes a technician record and their encrypted token from the database.
func (s *TechnicianStore) Delete(ctx context.Context, telegramUserID int64) error {
	const q = `DELETE FROM technicians WHERE telegram_user_id = ?;`
	res, err := s.db.ExecContext(ctx, q, telegramUserID)
	if err != nil {
		return fmt.Errorf("store: delete technician %d: %w", telegramUserID, err)
	}
	affected, err := res.RowsAffected()
	if err == nil && affected == 0 {
		return ErrNotFound
	}
	return nil
}
