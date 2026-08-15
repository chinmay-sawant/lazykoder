package db

import (
	"context"
	"fmt"
	"time"
)

const (
	sessionColumns = `id, title, directory, provider, model, variant, time_created, time_updated, status`
	messageColumns = `id, session_id, role, agent, provider_id, model_id, variant, time_created, seq`
	partColumns    = `id, message_id, type, time_created, seq, text, time_start, time_end, finish_reason, ` +
		`tokens_total, tokens_input, tokens_output, tokens_reasoning, tokens_cache_read, tokens_cache_write, ` +
		`cost, tool_name, tool_call_id, tool_status`
	toolCallColumns = `part_id, tool, call_id, status, title, time_start, time_end, exit_code, input_json, output, metadata_json`
)

// CreateSession inserts a session, filling the ID and defaults for
// Provider, Model, Status and timestamps when zero.
func (s *Store) CreateSession(ctx context.Context, sess Session) (Session, error) {
	if sess.ID == "" {
		sess.ID = NewID("ses_")
	}
	if sess.Provider == "" {
		sess.Provider = "opencode-go"
	}
	if sess.Model == "" {
		sess.Model = "deepseek-v4-flash"
	}
	if sess.Status == "" {
		sess.Status = "active"
	}
	if sess.TimeCreated == 0 {
		sess.TimeCreated = time.Now().UnixMilli()
	}
	if sess.TimeUpdated == 0 {
		sess.TimeUpdated = sess.TimeCreated
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (`+sessionColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.Title, sess.Directory, sess.Provider, sess.Model, sess.Variant,
		sess.TimeCreated, sess.TimeUpdated, sess.Status)
	if err != nil {
		return Session{}, fmt.Errorf("db: create session: %w", err)
	}
	return sess, nil
}

// InsertMessage inserts a message, filling the ID, TimeCreated and the
// per-session seq as MAX(seq)+1.
func (s *Store) InsertMessage(ctx context.Context, m Message) (Message, error) {
	if m.ID == "" {
		m.ID = NewID("msg_")
	}
	if m.TimeCreated == 0 {
		m.TimeCreated = time.Now().UnixMilli()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Message{}, fmt.Errorf("db: begin message insert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM messages WHERE session_id = ?`, m.SessionID).Scan(&m.Seq); err != nil {
		return Message{}, fmt.Errorf("db: next message seq: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO messages (`+messageColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.SessionID, m.Role, m.Agent, m.ProviderID, m.ModelID, m.Variant, m.TimeCreated, m.Seq)
	if err != nil {
		return Message{}, fmt.Errorf("db: insert message: %w", err)
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
	if err := tx.Commit(); err != nil {
		return Part{}, fmt.Errorf("db: commit part insert: %w", err)
	}
	return p, nil
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
			&m.Variant, &m.TimeCreated, &m.Seq); err != nil {
			return nil, fmt.Errorf("db: scan message: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list messages: %w", err)
	}
	return out, nil
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

// ListSessionsByDir returns the sessions of a directory ordered by
// time_updated DESC.
func (s *Store) ListSessionsByDir(ctx context.Context, directory string) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE directory = ? ORDER BY time_updated DESC`, directory)
	if err != nil {
		return nil, fmt.Errorf("db: list sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.Title, &sess.Directory, &sess.Provider, &sess.Model,
			&sess.Variant, &sess.TimeCreated, &sess.TimeUpdated, &sess.Status); err != nil {
			return nil, fmt.Errorf("db: scan session: %w", err)
		}
		out = append(out, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list sessions: %w", err)
	}
	return out, nil
}

// DeleteSession removes a session; messages and parts cascade via
// foreign_keys=ON.
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id); err != nil {
		return fmt.Errorf("db: delete session: %w", err)
	}
	return nil
}
