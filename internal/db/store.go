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
	{
		// Durable sub-agent job registry for task_list/wait after restart.
		`CREATE TABLE subagent_jobs (
  id                TEXT PRIMARY KEY,
  parent_session_id TEXT    NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  parent_part_id    TEXT,
  child_session_id  TEXT,
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
)`,
		`CREATE INDEX idx_subagent_jobs_parent ON subagent_jobs(parent_session_id)`,
		`CREATE INDEX idx_subagent_jobs_status ON subagent_jobs(status)`,
	},
	{
		// v0.0.4: unique seq + query-shaped indexes (no table rebuild).
		// Replace non-unique seq indexes with UNIQUE covering the same paths.
		`DROP INDEX IF EXISTS idx_messages_session_seq`,
		`CREATE UNIQUE INDEX idx_messages_session_seq ON messages(session_id, seq)`,
		`DROP INDEX IF EXISTS idx_parts_message_seq`,
		`CREATE UNIQUE INDEX idx_parts_message_seq ON parts(message_id, seq)`,
		// Resume list: filter by project directory first.
		`CREATE INDEX IF NOT EXISTS idx_sessions_dir_kind_updated ON sessions(directory, kind, time_updated DESC, time_created DESC, id)`,
		// Child drawer: parent + kind + recency.
		`CREATE INDEX IF NOT EXISTS idx_sessions_parent_kind_updated ON sessions(parent_session_id, kind, time_updated DESC, time_created DESC)`,
		// Durable jobs: list by parent and open-job recover.
		`CREATE INDEX IF NOT EXISTS idx_subagent_jobs_parent_started ON subagent_jobs(parent_session_id, time_started, time_created, id)`,
		`CREATE INDEX IF NOT EXISTS idx_subagent_jobs_open ON subagent_jobs(status, time_created, id) WHERE status IN ('queued', 'running')`,
	},
}

// schemaVersion is the highest applied migration number (includes rebuilds
// implemented as Go steps rather than pure SQL slices).
const (
	migrationSessionsFK = 7
	migrationJobsFK     = 8
	migrationTodos      = 9
	migrationSegments   = 10
	migrationStatusV2   = 11
	migrationRecaps     = 12
	schemaVersion       = migrationRecaps
)

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
	for i := current + 1; i <= schemaVersion; i++ {
		var err error
		switch i {
		case migrationSessionsFK:
			err = s.migrateV7SessionsParentFK(ctx)
		case migrationJobsFK:
			err = s.migrateV8SubagentJobsFK(ctx)
		case migrationTodos:
			// Model-driven todos (phase 4.5): full-list replace per session.
			err = s.applyMigration(ctx, migrationTodos, []string{
				`CREATE TABLE todos (
  session_id   TEXT    NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  seq          INTEGER NOT NULL,
  content      TEXT    NOT NULL DEFAULT '',
  status       TEXT    NOT NULL DEFAULT 'pending',
  time_updated INTEGER NOT NULL,
  PRIMARY KEY (session_id, seq)
)`,
				`CREATE INDEX idx_todos_session ON todos(session_id, seq)`,
			})
		case migrationSegments:
			// Footer visibility is session state so resume restores the same
			// status-line layout. JSON keeps the column additive and extensible.
			err = s.applyMigration(ctx, migrationSegments, []string{
				`ALTER TABLE sessions ADD COLUMN status_segments TEXT NOT NULL DEFAULT '["model","variant","tokens","cache","cost","tps","subs","models","scroll","prompt"]'`,
			})
		case migrationStatusV2:
			err = s.migrateV11StatusSegments(ctx)
		case migrationRecaps:
			err = s.applyMigration(ctx, migrationRecaps, []string{
				`CREATE TABLE IF NOT EXISTS recap_records (
  id                    TEXT PRIMARY KEY,
  session_id            TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  source_start_seq      INTEGER NOT NULL,
  source_end_seq        INTEGER NOT NULL,
  source_start_time     INTEGER NOT NULL,
  source_end_time       INTEGER NOT NULL,
  source_end_message_id TEXT NOT NULL,
  model                 TEXT NOT NULL,
  artifacts_json        TEXT NOT NULL DEFAULT '{}',
  status                TEXT NOT NULL,
  attempts              INTEGER NOT NULL DEFAULT 0,
  error                 TEXT,
  time_created          INTEGER NOT NULL,
  time_started          INTEGER,
  time_finished         INTEGER
)`,
				`CREATE UNIQUE INDEX IF NOT EXISTS idx_recap_records_session_end ON recap_records(session_id, source_end_message_id)`,
				`CREATE INDEX IF NOT EXISTS idx_recap_records_open ON recap_records(status, time_created, id) WHERE status IN ('queued', 'running')`,
				`CREATE INDEX IF NOT EXISTS idx_recap_records_session_seq ON recap_records(session_id, source_end_seq, id)`,
			})
		default:
			if i < 1 || i > len(schemaMigrations) {
				return fmt.Errorf("db: missing migration statements for version %d", i)
			}
			err = s.applyMigration(ctx, i, schemaMigrations[i-1])
		}
		if err != nil {
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
