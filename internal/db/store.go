package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store wraps the sqlite connection pool.
type Store struct {
	db *sql.DB
}

// busyTimeoutMS is how long a connection waits for a write lock before
// returning SQLITE_BUSY. Sub-agents share one Store and need headroom.
const busyTimeoutMS = 30_000

// Open opens (creating if missing) the sqlite file and configures it for
// concurrent agent/sub-agent use. SQLite allows only one writer: MaxOpenConns(1)
// serializes all access through a single connection so parent + child agents
// never hit "database is locked" under parallel task tools. WAL + busy_timeout
// remain for robustness if the pool setting is ever relaxed.
func Open(path string) (*Store, error) {
	// _pragma values apply to each new connection from the pool.
	dsn := fmt.Sprintf(
		"file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(%d)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)",
		path, busyTimeoutMS,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open %s: %w", path, err)
	}
	// Most important for concurrent sub-agents: one connection, no multi-conn lock fights.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	// Re-assert pragmas on the live connection (covers DSN variants).
	if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeoutMS)); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db: busy_timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db: foreign_keys: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode = WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db: enable WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA synchronous = NORMAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db: synchronous: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the underlying connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

var schemaMigrations = [][]string{
	{
		`CREATE TABLE sessions (
  id           TEXT PRIMARY KEY,
  title        TEXT    NOT NULL DEFAULT '',
  directory    TEXT    NOT NULL,
  provider     TEXT    NOT NULL DEFAULT 'opencode-go',
  model        TEXT    NOT NULL DEFAULT 'deepseek-v4-flash',
  variant      TEXT,
  time_created INTEGER NOT NULL,
  time_updated INTEGER NOT NULL,
  status       TEXT    NOT NULL DEFAULT 'active'
)`,
		`CREATE TABLE messages (
  id           TEXT PRIMARY KEY,
  session_id   TEXT    NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  role         TEXT    NOT NULL,
  agent        TEXT,
  provider_id  TEXT,
  model_id     TEXT,
  variant      TEXT,
  time_created INTEGER NOT NULL,
  seq          INTEGER NOT NULL
)`,
		`CREATE TABLE parts (
  id                  TEXT PRIMARY KEY,
  message_id          TEXT    NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  type                TEXT    NOT NULL,
  time_created        INTEGER NOT NULL,
  seq                 INTEGER NOT NULL,
  text                TEXT,
  time_start          INTEGER,
  time_end            INTEGER,
  finish_reason       TEXT,
  tokens_total        INTEGER,
  tokens_input        INTEGER,
  tokens_output       INTEGER,
  tokens_reasoning    INTEGER,
  tokens_cache_read   INTEGER,
  tokens_cache_write  INTEGER,
  cost                REAL,
  tool_name           TEXT,
  tool_call_id        TEXT,
  tool_status         TEXT
)`,
		`CREATE TABLE tool_calls (
  part_id       TEXT PRIMARY KEY REFERENCES parts(id) ON DELETE CASCADE,
  tool          TEXT NOT NULL,
  call_id       TEXT NOT NULL,
  status        TEXT NOT NULL,
  title         TEXT,
  time_start    INTEGER,
  time_end      INTEGER,
  exit_code     INTEGER,
  input_json    TEXT NOT NULL,
  output        TEXT,
  metadata_json TEXT
)`,
		`CREATE INDEX idx_messages_session_seq ON messages(session_id, seq)`,
		`CREATE INDEX idx_parts_message_seq    ON parts(message_id, seq)`,
		`CREATE INDEX idx_parts_type           ON parts(type)`,
		`CREATE INDEX idx_parts_tool           ON parts(tool_name) WHERE tool_name IS NOT NULL`,
		`CREATE INDEX idx_tool_calls_tool      ON tool_calls(tool)`,
		`CREATE INDEX idx_tool_calls_status    ON tool_calls(status)`,
		`CREATE INDEX idx_sessions_updated     ON sessions(time_updated DESC)`,
	},
	{
		`ALTER TABLE messages ADD COLUMN visible INTEGER NOT NULL DEFAULT 1`,
	},
	{
		// Sessions created before findings 1.2 stored <project>/.lazykoder
		// as directory. Strip that suffix so ListSessionsByDir(project) finds them.
		`UPDATE sessions SET directory = substr(directory, 1, length(directory) - 11) WHERE directory LIKE '%/.lazykoder'`,
	},
	{
		// Sub-agent child sessions: parent link + kind (main|subagent).
		`ALTER TABLE sessions ADD COLUMN parent_session_id TEXT`,
		`ALTER TABLE sessions ADD COLUMN kind TEXT NOT NULL DEFAULT 'main'`,
		`CREATE INDEX idx_sessions_parent ON sessions(parent_session_id) WHERE parent_session_id IS NOT NULL`,
		`CREATE INDEX idx_sessions_kind ON sessions(kind)`,
	},
}

// Migrate runs numbered migrations. schema_migrations records the applied
// versions after first open. Idempotent: a second call is a no-op.
func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
  version    INTEGER PRIMARY KEY,
  applied_at INTEGER NOT NULL
)`); err != nil {
		return fmt.Errorf("db: create schema_migrations: %w", err)
	}
	var current int
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("db: read schema version: %w", err)
	}
	for i := current + 1; i <= len(schemaMigrations); i++ {
		if err := s.applyMigration(ctx, i, schemaMigrations[i-1]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) applyMigration(ctx context.Context, version int, statements []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin migration %d: %w", version, err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("db: apply migration %d: %w", version, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`, version, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("db: record migration %d: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: commit migration %d: %w", version, err)
	}
	return nil
}
