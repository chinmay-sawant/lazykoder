package db

import (
	"context"
	"fmt"
	"time"
)

// migrateV7SessionsParentFK rebuilds sessions so parent_session_id is a real
// self-FK with ON DELETE CASCADE. SQLite cannot ADD CONSTRAINT via ALTER.
func (s *Store) migrateV7SessionsParentFK(ctx context.Context) error {
	// PRAGMA foreign_keys cannot change inside a transaction; turn off for swap.
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("db: migration 7 foreign_keys off: %w", err)
	}
	defer func() { _, _ = s.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`) }()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin migration 7: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Drop orphan children whose parent row is missing (CASCADE intent).
	if _, err := tx.ExecContext(ctx, `
DELETE FROM sessions
WHERE id IN (
  SELECT c.id FROM sessions c
  LEFT JOIN sessions p ON p.id = c.parent_session_id
  WHERE c.parent_session_id IS NOT NULL AND c.parent_session_id != '' AND p.id IS NULL
)`); err != nil {
		return fmt.Errorf("db: migration 7 orphan cleanup: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
CREATE TABLE sessions_new (
  id                TEXT PRIMARY KEY,
  title             TEXT    NOT NULL DEFAULT '',
  directory         TEXT    NOT NULL,
  provider          TEXT    NOT NULL DEFAULT 'opencode-go',
  model             TEXT    NOT NULL DEFAULT 'deepseek-v4-flash',
  variant           TEXT,
  time_created      INTEGER NOT NULL,
  time_updated      INTEGER NOT NULL,
  status            TEXT    NOT NULL DEFAULT 'active',
  parent_session_id TEXT    REFERENCES sessions_new(id) ON DELETE CASCADE,
  kind              TEXT    NOT NULL DEFAULT 'main'
)`); err != nil {
		return fmt.Errorf("db: migration 7 create sessions_new: %w", err)
	}

	// Parents first, then children (self-FK safe even if FK is re-enabled later).
	copySQL := `
INSERT INTO sessions_new (
  id, title, directory, provider, model, variant,
  time_created, time_updated, status, parent_session_id, kind
)
SELECT
  id, title, directory, provider, model, variant,
  time_created, time_updated, status,
  NULLIF(parent_session_id, ''),
  COALESCE(NULLIF(kind, ''), 'main')
FROM sessions
WHERE parent_session_id IS NULL OR parent_session_id = ''
`
	if _, err := tx.ExecContext(ctx, copySQL); err != nil {
		return fmt.Errorf("db: migration 7 copy parent sessions: %w", err)
	}
	copyChildren := `
INSERT INTO sessions_new (
  id, title, directory, provider, model, variant,
  time_created, time_updated, status, parent_session_id, kind
)
SELECT
  id, title, directory, provider, model, variant,
  time_created, time_updated, status,
  NULLIF(parent_session_id, ''),
  COALESCE(NULLIF(kind, ''), 'main')
FROM sessions
WHERE parent_session_id IS NOT NULL AND parent_session_id != ''
`
	if _, err := tx.ExecContext(ctx, copyChildren); err != nil {
		return fmt.Errorf("db: migration 7 copy child sessions: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DROP TABLE sessions`); err != nil {
		return fmt.Errorf("db: migration 7 drop sessions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE sessions_new RENAME TO sessions`); err != nil {
		return fmt.Errorf("db: migration 7 rename sessions: %w", err)
	}

	// Recreate indexes that lived on sessions (including migration 6 composites).
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_sessions_updated ON sessions(time_updated DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_parent ON sessions(parent_session_id) WHERE parent_session_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_kind ON sessions(kind)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_dir_kind_updated ON sessions(directory, kind, time_updated DESC, time_created DESC, id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_parent_kind_updated ON sessions(parent_session_id, kind, time_updated DESC, time_created DESC)`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("db: migration 7 recreate index: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (7, ?)`, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("db: record migration 7: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: commit migration 7: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("db: migration 7 foreign_keys on: %w", err)
	}
	if err := s.foreignKeyCheck(ctx); err != nil {
		return fmt.Errorf("db: migration 7 foreign_key_check: %w", err)
	}
	return nil
}

// migrateV8SubagentJobsFK rebuilds subagent_jobs with FKs for child session
// and parent part (SET NULL on delete).
func (s *Store) migrateV8SubagentJobsFK(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("db: migration 8 foreign_keys off: %w", err)
	}
	defer func() { _, _ = s.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`) }()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin migration 8: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Null dangling optional refs before enforcing FKs.
	if _, err := tx.ExecContext(ctx, `
UPDATE subagent_jobs SET child_session_id = NULL
WHERE child_session_id IS NOT NULL AND child_session_id != ''
  AND NOT EXISTS (SELECT 1 FROM sessions s WHERE s.id = subagent_jobs.child_session_id)`); err != nil {
		return fmt.Errorf("db: migration 8 null bad child_session_id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE subagent_jobs SET parent_part_id = NULL
WHERE parent_part_id IS NOT NULL AND parent_part_id != ''
  AND NOT EXISTS (SELECT 1 FROM parts p WHERE p.id = subagent_jobs.parent_part_id)`); err != nil {
		return fmt.Errorf("db: migration 8 null bad parent_part_id: %w", err)
	}
	// Drop jobs whose parent session is gone (should be rare; CASCADE intent).
	if _, err := tx.ExecContext(ctx, `
DELETE FROM subagent_jobs
WHERE NOT EXISTS (SELECT 1 FROM sessions s WHERE s.id = subagent_jobs.parent_session_id)`); err != nil {
		return fmt.Errorf("db: migration 8 drop orphan jobs: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
CREATE TABLE subagent_jobs_new (
  id                TEXT PRIMARY KEY,
  parent_session_id TEXT    NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  parent_part_id    TEXT    REFERENCES parts(id) ON DELETE SET NULL,
  child_session_id  TEXT    REFERENCES sessions(id) ON DELETE SET NULL,
  name              TEXT    NOT NULL DEFAULT '',
  role              TEXT    NOT NULL DEFAULT 'explore',
  status            TEXT    NOT NULL,
  prompt            TEXT    NOT NULL DEFAULT '',
  description       TEXT    NOT NULL DEFAULT '',
  model             TEXT,
  variant           TEXT,
  max_steps         INTEGER NOT NULL DEFAULT 0,
  timeout_ms        INTEGER NOT NULL DEFAULT 0,
  summary           TEXT,
  error             TEXT,
  time_created      INTEGER NOT NULL,
  time_updated      INTEGER NOT NULL,
  time_started      INTEGER,
  time_finished     INTEGER
)`); err != nil {
		return fmt.Errorf("db: migration 8 create subagent_jobs_new: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO subagent_jobs_new (
  id, parent_session_id, parent_part_id, child_session_id, name, role, status,
  prompt, description, model, variant, max_steps, timeout_ms, summary, error,
  time_created, time_updated, time_started, time_finished
)
SELECT
  id, parent_session_id,
  NULLIF(parent_part_id, ''),
  NULLIF(child_session_id, ''),
  name, role, status, prompt, description, model, variant,
  max_steps, timeout_ms, summary, error,
  time_created, time_updated, time_started, time_finished
FROM subagent_jobs`); err != nil {
		return fmt.Errorf("db: migration 8 copy jobs: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DROP TABLE subagent_jobs`); err != nil {
		return fmt.Errorf("db: migration 8 drop subagent_jobs: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE subagent_jobs_new RENAME TO subagent_jobs`); err != nil {
		return fmt.Errorf("db: migration 8 rename: %w", err)
	}

	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_subagent_jobs_parent ON subagent_jobs(parent_session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_subagent_jobs_status ON subagent_jobs(status)`,
		`CREATE INDEX IF NOT EXISTS idx_subagent_jobs_parent_started ON subagent_jobs(parent_session_id, time_started, time_created, id)`,
		`CREATE INDEX IF NOT EXISTS idx_subagent_jobs_open ON subagent_jobs(status, time_created, id) WHERE status IN ('queued', 'running')`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("db: migration 8 recreate index: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (8, ?)`, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("db: record migration 8: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: commit migration 8: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("db: migration 8 foreign_keys on: %w", err)
	}
	if err := s.foreignKeyCheck(ctx); err != nil {
		return fmt.Errorf("db: migration 8 foreign_key_check: %w", err)
	}
	return nil
}

func (s *Store) foreignKeyCheck(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	var violations []string
	for rows.Next() {
		var table, rowid, parent, fkid string
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			return err
		}
		violations = append(violations, fmt.Sprintf("%s row=%s parent=%s fk=%s", table, rowid, parent, fkid))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(violations) > 0 {
		return fmt.Errorf("violations: %v", violations)
	}
	return nil
}
