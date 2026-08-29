package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	sessionColumns       = `id, title, directory, provider, model, variant, time_created, time_updated, time_active, status, parent_session_id, kind, status_segments`
	messageColumns       = `id, session_id, role, agent, provider_id, model_id, variant, time_created, seq, visible`
	messageInsertColumns = `id, session_id, role, agent, provider_id, model_id, variant, time_created, seq`
	partColumns          = `id, message_id, type, time_created, seq, text, time_start, time_end, finish_reason, ` +
		`tokens_total, tokens_input, tokens_output, tokens_reasoning, tokens_cache_read, tokens_cache_write, ` +
		`cost, tool_name, tool_call_id, tool_status`
	toolCallColumns = `part_id, tool, call_id, status, title, time_start, time_end, exit_code, input_json, output, metadata_json`
)

// CreateSession inserts a session, filling the ID and storage defaults for
// Provider, Status, and timestamps when zero. Callers choose the model.
func (s *Store) CreateSession(ctx context.Context, sess Session) (Session, error) {
	if sess.ID == "" {
		sess.ID = NewID("ses_")
	}
	if sess.Provider == "" {
		sess.Provider = "opencode-go"
	}
	if sess.Status == "" {
		sess.Status = "active"
	}
	if sess.Kind == "" {
		if sess.ParentSessionID != nil && *sess.ParentSessionID != "" {
			sess.Kind = SessionKindSubagent
		} else {
			sess.Kind = SessionKindMain
		}
	}
	sess.StatusSegments = NormalizeStatusSegments(sess.StatusSegments)
	segmentsJSON, err := json.Marshal(sess.StatusSegments)
	if err != nil {
		return Session{}, fmt.Errorf("db: encode status segments: %w", err)
	}
	if sess.TimeCreated == 0 {
		sess.TimeCreated = time.Now().UnixMilli()
	}
	if sess.TimeUpdated == 0 {
		sess.TimeUpdated = sess.TimeCreated
	}
	if sess.TimeActive == 0 {
		sess.TimeActive = sess.TimeUpdated
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO sessions (`+sessionColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.Title, sess.Directory, sess.Provider, sess.Model, sess.Variant,
		sess.TimeCreated, sess.TimeUpdated, sess.TimeActive, sess.Status, sess.ParentSessionID, sess.Kind, string(segmentsJSON))
	if err != nil {
		return Session{}, fmt.Errorf("db: create session: %w", err)
	}
	return sess, nil
}

// InsertMessage inserts a message, filling the ID, TimeCreated and the
// per-session seq as MAX(seq)+1. Also bumps the parent session's
// time_updated so resume lists and age labels stay current.
func (s *Store) InsertMessage(ctx context.Context, m Message) (Message, error) {
	if m.ID == "" {
		m.ID = NewID("msg_")
	}
	if m.TimeCreated == 0 {
		m.TimeCreated = time.Now().UnixMilli()
	}
	m.Visible = true
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, fmt.Errorf("db: begin message insert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM messages WHERE session_id = ?`, m.SessionID).Scan(&m.Seq); err != nil {
		return Message{}, fmt.Errorf("db: next message seq: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO messages (`+messageInsertColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.SessionID, m.Role, m.Agent, m.ProviderID, m.ModelID, m.Variant, m.TimeCreated, m.Seq)
	if err != nil {
		return Message{}, fmt.Errorf("db: insert message: %w", err)
	}
	if err := touchConversationTx(ctx, tx, m.SessionID, m.TimeCreated); err != nil {
		return Message{}, err
	}
	if err := tx.Commit(); err != nil {
		return Message{}, fmt.Errorf("db: commit message insert: %w", err)
	}
	return m, nil
}

// InsertPart inserts a part, filling the ID, TimeCreated and the
// per-message seq as MAX(seq)+1.
func (s *Store) InsertPart(ctx context.Context, p Part) (Part, error) {
	if p.ID == "" {
		p.ID = NewID("prt_")
	}
	if p.TimeCreated == 0 {
		p.TimeCreated = time.Now().UnixMilli()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Part{}, fmt.Errorf("db: begin part insert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM parts WHERE message_id = ?`, p.MessageID).Scan(&p.Seq); err != nil {
		return Part{}, fmt.Errorf("db: next part seq: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO parts (`+partColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.MessageID, p.Type, p.TimeCreated, p.Seq, p.Text, p.TimeStart, p.TimeEnd, p.FinishReason,
		p.TokensTotal, p.TokensInput, p.TokensOutput, p.TokensReasoning, p.TokensCacheRead, p.TokensCacheWrite,
		p.Cost, p.ToolName, p.ToolCallID, p.ToolStatus)
	if err != nil {
		return Part{}, fmt.Errorf("db: insert part: %w", err)
	}
	if err := touchConversationForMessageTx(ctx, tx, p.MessageID, p.TimeCreated); err != nil {
		return Part{}, err
	}
	if err := tx.Commit(); err != nil {
		return Part{}, fmt.Errorf("db: commit part insert: %w", err)
	}
	return p, nil
}

// UpdatePartText replaces the text of an existing part. Used to grow
// streamed reasoning and assistant text in place.
func (s *Store) UpdatePartText(ctx context.Context, id, text string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE parts SET text = ? WHERE id = ?`, text, id)
	if err != nil {
		return fmt.Errorf("db: update part text: %w", err)
	}
	return nil
}

// InsertToolCall inserts a tool_calls row for an existing tool part.
func (s *Store) InsertToolCall(ctx context.Context, tc ToolCall) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO tool_calls (`+toolCallColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tc.PartID, tc.Tool, tc.CallID, tc.Status, tc.Title, tc.TimeStart, tc.TimeEnd, tc.ExitCode,
		tc.InputJSON, tc.Output, tc.MetadataJSON)
	if err != nil {
		return fmt.Errorf("db: insert tool call: %w", err)
	}
	return nil
}

// UpdateToolCall upserts a tool_calls row by PartID and sets the owning
// part's tool_status to tc.Status.
func (s *Store) UpdateToolCall(ctx context.Context, tc ToolCall) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin tool call update: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `INSERT INTO tool_calls (`+toolCallColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(part_id) DO UPDATE SET tool = excluded.tool, call_id = excluded.call_id, status = excluded.status,
title = excluded.title, time_start = excluded.time_start, time_end = excluded.time_end,
exit_code = excluded.exit_code, input_json = excluded.input_json, output = excluded.output,
metadata_json = excluded.metadata_json`,
		tc.PartID, tc.Tool, tc.CallID, tc.Status, tc.Title, tc.TimeStart, tc.TimeEnd, tc.ExitCode,
		tc.InputJSON, tc.Output, tc.MetadataJSON)
	if err != nil {
		return fmt.Errorf("db: upsert tool call: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE parts SET tool_status = ? WHERE id = ?`, tc.Status, tc.PartID); err != nil {
		return fmt.Errorf("db: update part tool_status: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: commit tool call update: %w", err)
	}
	return nil
}

// ListMessages returns the messages of a session ordered by seq.
func (s *Store) ListMessages(ctx context.Context, sessionID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+messageColumns+` FROM messages WHERE session_id = ? ORDER BY seq`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("db: list messages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Agent, &m.ProviderID, &m.ModelID,
			&m.Variant, &m.TimeCreated, &m.Seq, &m.Visible); err != nil {
			return nil, fmt.Errorf("db: scan message: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list messages: %w", err)
	}
	return out, nil
}

// SetMessageVisibility soft-hides or restores a message without deleting it.
// Bumps the owning session's time_updated.
func (s *Store) SetMessageVisibility(ctx context.Context, messageID string, visible bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin set message visibility: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE messages SET visible = ? WHERE id = ?`, visible, messageID); err != nil {
		return fmt.Errorf("db: set message visibility: %w", err)
	}
	var sessionID string
	if err := tx.QueryRowContext(ctx, `SELECT session_id FROM messages WHERE id = ?`, messageID).Scan(&sessionID); err != nil {
		return fmt.Errorf("db: message session for visibility: %w", err)
	}
	if err := touchSession(ctx, tx, sessionID, 0); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: commit set message visibility: %w", err)
	}
	return nil
}

// TouchSession sets sessions.time_updated to now (or when > 0).
// It records general activity without changing conversation ordering.
func (s *Store) TouchSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("db: touch session: empty id")
	}
	return touchSession(ctx, s.db, sessionID, 0)
}

type execContext interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func touchSession(ctx context.Context, db execContext, sessionID string, when int64) error {
	if when <= 0 {
		when = time.Now().UnixMilli()
	}
	if _, err := db.ExecContext(ctx, `UPDATE sessions SET time_updated = ? WHERE id = ?`, when, sessionID); err != nil {
		return fmt.Errorf("db: touch session: %w", err)
	}
	return nil
}

func touchConversationTx(ctx context.Context, tx *sql.Tx, sessionID string, when int64) error {
	if when <= 0 {
		when = time.Now().UnixMilli()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET time_updated = ?, time_active = ? WHERE id = ?`, when, when, sessionID); err != nil {
		return fmt.Errorf("db: touch conversation: %w", err)
	}
	return nil
}

func touchConversationForMessageTx(ctx context.Context, tx *sql.Tx, messageID string, when int64) error {
	if when <= 0 {
		when = time.Now().UnixMilli()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET time_active = ? WHERE id = (SELECT session_id FROM messages WHERE id = ?)`, when, messageID); err != nil {
		return fmt.Errorf("db: touch conversation for message: %w", err)
	}
	return nil
}

// ListParts returns the parts of a message ordered by seq.
func (s *Store) ListParts(ctx context.Context, messageID string) ([]Part, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+partColumns+` FROM parts WHERE message_id = ? ORDER BY seq`, messageID)
	if err != nil {
		return nil, fmt.Errorf("db: list parts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Part
	for rows.Next() {
		var p Part
		if err := rows.Scan(&p.ID, &p.MessageID, &p.Type, &p.TimeCreated, &p.Seq, &p.Text, &p.TimeStart,
			&p.TimeEnd, &p.FinishReason, &p.TokensTotal, &p.TokensInput, &p.TokensOutput, &p.TokensReasoning,
			&p.TokensCacheRead, &p.TokensCacheWrite, &p.Cost, &p.ToolName, &p.ToolCallID, &p.ToolStatus); err != nil {
			return nil, fmt.Errorf("db: scan part: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list parts: %w", err)
	}
	return out, nil
}

// ListToolCalls returns the tool_calls rows of a session joined with their
// parts, ordered by part seq then part id.
func (s *Store) ListToolCalls(ctx context.Context, sessionID string) ([]ToolCall, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tc.part_id, tc.tool, tc.call_id, tc.status, tc.title,
tc.time_start, tc.time_end, tc.exit_code, tc.input_json, tc.output, tc.metadata_json
FROM tool_calls tc
JOIN parts p ON p.id = tc.part_id
JOIN messages m ON m.id = p.message_id
WHERE m.session_id = ? ORDER BY p.seq, tc.part_id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("db: list tool calls: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ToolCall
	for rows.Next() {
		var tc ToolCall
		if err := rows.Scan(&tc.PartID, &tc.Tool, &tc.CallID, &tc.Status, &tc.Title, &tc.TimeStart,
			&tc.TimeEnd, &tc.ExitCode, &tc.InputJSON, &tc.Output, &tc.MetadataJSON); err != nil {
			return nil, fmt.Errorf("db: scan tool call: %w", err)
		}
		out = append(out, tc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list tool calls: %w", err)
	}
	return out, nil
}

// UpdateSessionModel sets the model of a session and bumps time_updated.
func (s *Store) UpdateSessionModel(ctx context.Context, sessionID, model string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET model = ?, time_updated = ? WHERE id = ?`,
		model, time.Now().UnixMilli(), sessionID); err != nil {
		return fmt.Errorf("db: update session model: %w", err)
	}
	return nil
}

// UpdateSessionProvider sets the provider route of a session and bumps
// time_updated.
func (s *Store) UpdateSessionProvider(ctx context.Context, sessionID, provider string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET provider = ?, time_updated = ? WHERE id = ?`,
		provider, time.Now().UnixMilli(), sessionID); err != nil {
		return fmt.Errorf("db: update session provider: %w", err)
	}
	return nil
}

// UpdateSessionVariant sets the reasoning variant of a session and bumps
// time_updated. An empty variant clears the column.
func (s *Store) UpdateSessionVariant(ctx context.Context, sessionID, variant string) error {
	var value any
	if variant != "" {
		value = variant
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET variant = ?, time_updated = ? WHERE id = ?`,
		value, time.Now().UnixMilli(), sessionID); err != nil {
		return fmt.Errorf("db: update session variant: %w", err)
	}
	return nil
}

// UpdateSessionSegments persists the visible footer segment identifiers.
func (s *Store) UpdateSessionSegments(ctx context.Context, sessionID string, segments []string) error {
	segments = NormalizeStatusSegments(segments)
	raw, err := json.Marshal(segments)
	if err != nil {
		return fmt.Errorf("db: encode status segments: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE sessions SET status_segments = ?, time_updated = ? WHERE id = ?`,
		string(raw), time.Now().UnixMilli(), sessionID); err != nil {
		return fmt.Errorf("db: update session segments: %w", err)
	}
	return nil
}

func decodeStatusSegments(raw string) []string {
	var values []string
	if json.Unmarshal([]byte(raw), &values) != nil {
		return DefaultStatusSegments()
	}
	return NormalizeStatusSegments(values)
}

// lazykoderDirSuffix is the workspace folder incorrectly stored as
// sessions.directory before findings 1.2.
const lazykoderDirSuffix = "/.lazykoder"

// RepairSessionDirectories rewrites session rows whose directory is the
// .lazykoder workspace folder so they point at the project root. Idempotent.
func (s *Store) RepairSessionDirectories(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE sessions
SET directory = substr(directory, 1, length(directory) - ?)
WHERE directory LIKE '%`+lazykoderDirSuffix+`'`, len(lazykoderDirSuffix))
	if err != nil {
		return fmt.Errorf("db: repair session directories: %w", err)
	}
	return nil
}

// GetSession returns one session by id.
func (s *Store) GetSession(ctx context.Context, id string) (Session, error) {
	var sess Session
	var statusSegments string
	err := s.db.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id).
		Scan(&sess.ID, &sess.Title, &sess.Directory, &sess.Provider, &sess.Model,
			&sess.Variant, &sess.TimeCreated, &sess.TimeUpdated, &sess.TimeActive, &sess.Status,
			&sess.ParentSessionID, &sess.Kind, &statusSegments)
	if err != nil {
		return Session{}, fmt.Errorf("db: get session: %w", err)
	}
	sess.StatusSegments = decodeStatusSegments(statusSegments)
	return sess, nil
}

// ListChildSessions returns sub-agent sessions spawned under parentID,
// most recently updated first. Used by the TUI sub-agent picker/log view.
func (s *Store) ListChildSessions(ctx context.Context, parentID string) ([]Session, error) {
	if parentID == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+sessionColumns+` FROM sessions
WHERE parent_session_id = ? AND kind = 'subagent'
ORDER BY time_updated DESC, time_created DESC`, parentID)
	if err != nil {
		return nil, fmt.Errorf("db: list child sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Session
	for rows.Next() {
		var sess Session
		var statusSegments string
		if err := rows.Scan(&sess.ID, &sess.Title, &sess.Directory, &sess.Provider, &sess.Model,
			&sess.Variant, &sess.TimeCreated, &sess.TimeUpdated, &sess.TimeActive, &sess.Status,
			&sess.ParentSessionID, &sess.Kind, &statusSegments); err != nil {
			return nil, fmt.Errorf("db: scan child session: %w", err)
		}
		sess.StatusSegments = decodeStatusSegments(statusSegments)
		out = append(out, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list child sessions: %w", err)
	}
	return out, nil
}

// ListSessionsByDir returns main sessions of a directory ordered by
// conversation activity (stable ties via time_created, id). Sub-agent child
// sessions are omitted from resume lists.
func (s *Store) ListSessionsByDir(ctx context.Context, directory string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sessionColumns+` FROM sessions
WHERE directory = ? AND (kind = 'main' OR kind = '' OR kind IS NULL)
ORDER BY time_active DESC, time_created DESC, id DESC`, directory)
	if err != nil {
		return nil, fmt.Errorf("db: list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Session
	for rows.Next() {
		var sess Session
		var statusSegments string
		if err := rows.Scan(&sess.ID, &sess.Title, &sess.Directory, &sess.Provider, &sess.Model,
			&sess.Variant, &sess.TimeCreated, &sess.TimeUpdated, &sess.TimeActive, &sess.Status,
			&sess.ParentSessionID, &sess.Kind, &statusSegments); err != nil {
			return nil, fmt.Errorf("db: scan session: %w", err)
		}
		sess.StatusSegments = decodeStatusSegments(statusSegments)
		out = append(out, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list sessions: %w", err)
	}
	return out, nil
}

// DeleteSession removes a session. Child sub-agent sessions cascade via
// sessions.parent_session_id ON DELETE CASCADE; messages/parts/tool_calls and
// subagent_jobs cascade from their session FKs (foreign_keys=ON).
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("db: delete session: %w", err)
	}
	return nil
}

// ReplaceTodos replaces the full todo list for a session (todowrite contract).
// items may be empty to clear the list. Seq is assigned 0..n-1 in order.
func (s *Store) ReplaceTodos(ctx context.Context, sessionID string, items []Todo) error {
	if sessionID == "" {
		return fmt.Errorf("db: replace todos: empty session id")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("db: begin replace todos: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM todos WHERE session_id = ?`, sessionID); err != nil {
		return fmt.Errorf("db: clear todos: %w", err)
	}
	now := time.Now().UnixMilli()
	for i, it := range items {
		st := it.Status
		if st == "" {
			st = TodoPending
		}
		content := strings.TrimSpace(it.Content)
		if _, err := tx.ExecContext(ctx, `INSERT INTO todos (session_id, seq, content, status, time_updated)
VALUES (?, ?, ?, ?, ?)`, sessionID, i, content, st, now); err != nil {
			return fmt.Errorf("db: insert todo %d: %w", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("db: commit replace todos: %w", err)
	}
	return nil
}

// ListTodos returns todos for a session ordered by seq ascending.
func (s *Store) ListTodos(ctx context.Context, sessionID string) ([]Todo, error) {
	if sessionID == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT session_id, seq, content, status, time_updated
FROM todos WHERE session_id = ? ORDER BY seq ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("db: list todos: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Todo
	for rows.Next() {
		var t Todo
		if err := rows.Scan(&t.SessionID, &t.Seq, &t.Content, &t.Status, &t.TimeUpdated); err != nil {
			return nil, fmt.Errorf("db: scan todo: %w", err)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list todos: %w", err)
	}
	return out, nil
}
