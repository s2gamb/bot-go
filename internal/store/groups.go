package store

import (
	"context"
	"database/sql"
	"fmt"
)

// TechnicianGroup represents a technician's membership in an SDP support group.
type TechnicianGroup struct {
	SDPTechnicianID  string
	SupportGroupID   string
	SupportGroupName string
}

// GroupStore manages persistence of technician-to-support-group mappings in SQLite.
type GroupStore struct {
	db *sql.DB
}

// NewGroupStore creates a new GroupStore backed by db.
func NewGroupStore(db *sql.DB) *GroupStore {
	return &GroupStore{db: db}
}

// ReplaceAll atomically replaces all technician group memberships in SQLite.
func (s *GroupStore) ReplaceAll(ctx context.Context, mappings []TechnicianGroup) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, `DELETE FROM technician_groups;`); err != nil {
		return fmt.Errorf("store: clear technician_groups: %w", err)
	}

	if len(mappings) > 0 {
		stmt, err := tx.PrepareContext(ctx, `
INSERT INTO technician_groups (sdp_technician_id, support_group_id, support_group_name, updated_at)
VALUES (?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(sdp_technician_id, support_group_id) DO UPDATE SET
    support_group_name = excluded.support_group_name,
    updated_at         = CURRENT_TIMESTAMP;
`)
		if err != nil {
			return fmt.Errorf("store: prepare insert technician_groups: %w", err)
		}
		defer func() {
			_ = stmt.Close()
		}()

		for _, m := range mappings {
			if m.SDPTechnicianID == "" || m.SupportGroupID == "" {
				continue
			}
			if _, err := stmt.ExecContext(ctx, m.SDPTechnicianID, m.SupportGroupID, m.SupportGroupName); err != nil {
				return fmt.Errorf("store: insert technician_group (%s, %s): %w", m.SDPTechnicianID, m.SupportGroupID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit technician_groups: %w", err)
	}
	return nil
}

// GetGroupIDsByTechnician returns all support group IDs that sdpTechID belongs to.
func (s *GroupStore) GetGroupIDsByTechnician(ctx context.Context, sdpTechID string) ([]string, error) {
	if sdpTechID == "" {
		return nil, nil
	}

	const q = `
SELECT support_group_id
FROM technician_groups
WHERE sdp_technician_id = ?
ORDER BY support_group_id;
`
	rows, err := s.db.QueryContext(ctx, q, sdpTechID)
	if err != nil {
		return nil, fmt.Errorf("store: query technician_groups for %s: %w", sdpTechID, err)
	}
	defer func() {
		_ = rows.Close()
	}()

	var groupIDs []string
	for rows.Next() {
		var gid string
		if err := rows.Scan(&gid); err != nil {
			return nil, fmt.Errorf("store: scan support_group_id: %w", err)
		}
		groupIDs = append(groupIDs, gid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate technician_groups: %w", err)
	}
	return groupIDs, nil
}

// GetGroupIDsByTechnician returns all support group IDs that sdpTechID belongs to.
func (s *TechnicianStore) GetGroupIDsByTechnician(ctx context.Context, sdpTechID string) ([]string, error) {
	return NewGroupStore(s.db).GetGroupIDsByTechnician(ctx, sdpTechID)
}
