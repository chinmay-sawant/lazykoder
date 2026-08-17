package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// migrateV11StatusSegments expands the original coarse footer controls into
// the independently toggleable status drawer fields. Existing visibility is
// preserved: model also enables variant, tokens also enables cache, and
// sub-agent counts become visible because they were previously unconditional.
func (s *Store) migrateV11StatusSegments(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin migration %d: %w", migrationStatusV2, err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx, `SELECT id, status_segments FROM sessions`)
	if err != nil {
		return fmt.Errorf("db: read status segments: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return fmt.Errorf("db: scan status segments: %w", err)
		}
		var values []string
		if json.Unmarshal([]byte(raw), &values) != nil {
			values = nil
		}
		legacy := true
		for _, name := range values {
			if name == "variant" || name == "cache" || name == "subs" {
				legacy = false
				break
			}
		}
		if legacy {
			visible := make(map[string]bool, len(values))
			for _, name := range values {
				visible[name] = true
			}
			if visible["model"] {
				values = append(values, "variant")
			}
			if visible["tokens"] {
				values = append(values, "cache")
			}
			values = append(values, "subs")
		}
		values = NormalizeStatusSegments(values)
		encoded, err := json.Marshal(values)
		if err != nil {
			return fmt.Errorf("db: encode status segments: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET status_segments = ? WHERE id = ?`, string(encoded), id); err != nil {
			return fmt.Errorf("db: update status segments: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("db: iterate status segments: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, migrationStatusV2, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("db: record migration %d: %w", migrationStatusV2, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: commit migration %d: %w", migrationStatusV2, err)
	}
	return nil
}
