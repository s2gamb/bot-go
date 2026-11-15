package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS technicians (
    telegram_user_id    INTEGER PRIMARY KEY,
    sdp_technician_id   TEXT NOT NULL,
    sdp_technician_name TEXT NOT NULL,
    encrypted_token     BLOB NOT NULL,
    nonce               BLOB NOT NULL,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS technician_groups (
    sdp_technician_id  TEXT NOT NULL,
    support_group_id   TEXT NOT NULL,
    support_group_name TEXT NOT NULL DEFAULT '',
    updated_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (sdp_technician_id, support_group_id)
);

CREATE INDEX IF NOT EXISTS idx_technician_groups_tech_id ON technician_groups(sdp_technician_id);
`

// OpenSQLite opens and initializes a SQLite database at dbPath and applies migrations.
func OpenSQLite(ctx context.Context, dbPath string) (*sql.DB, error) {
	if dbPath != ":memory:" && !strings.HasPrefix(dbPath, "file:") {
		dir := filepath.Dir(dbPath)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o750); err != nil {
				return nil, fmt.Errorf("store: create db directory %q: %w", dir, err)
			}
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("store: open sqlite %q: %w", dbPath, err)
	}

	// SQLite works best with a single writer connection or WAL mode.
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: set sqlite pragmas: %w", err)
	}

	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: migrate schema: %w", err)
	}

	return db, nil
}
