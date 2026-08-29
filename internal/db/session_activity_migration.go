package db

import (
	"context"
	"database/sql"
	"fmt"
)

func (s *Store) migrateV15SessionActivity(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(sessions)`)
	if err != nil {
		return fmt.Errorf("db: inspect session columns: %w", err)
	}
	defer func() { _ = rows.Close() }()

	hasColumn := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return fmt.Errorf("db: inspect session columns: %w", err)
		}
		if name == "time_active" {
			hasColumn = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("db: inspect session columns: %w", err)
	}

	index := `CREATE INDEX IF NOT EXISTS idx_sessions_dir_kind_active ON sessions(directory, kind, time_active DESC, time_created DESC, id)`
	if hasColumn {
		return s.applyMigration(ctx, migrationSessionActive, []string{
			`UPDATE sessions SET time_active = time_updated WHERE time_active = 0`,
			index,
		})
	}
	return s.applyMigration(ctx, migrationSessionActive, []string{
		`ALTER TABLE sessions ADD COLUMN time_active INTEGER NOT NULL DEFAULT 0`,
		`UPDATE sessions SET time_active = time_updated`,
		index,
	})
}
