package db

import (
	"context"
	"fmt"
)

// SessionEntry is one message plus its parts in seq order.
type SessionEntry struct {
	Message Message
	Parts   []Part
}

// SessionGraph is the deep session load used by history, replay, and
// usage rollups: ordered entries and tool_calls keyed by part id
// (matching agent.sessionEntries / byPart).
type SessionGraph struct {
	Entries         []SessionEntry
	ToolCallsByPart map[string]ToolCall
}

// LoadSessionGraph loads messages, all parts, and tool_calls for a
// session in a fixed number of queries (not per-message ListParts).
func (s *Store) LoadSessionGraph(ctx context.Context, sessionID string) (SessionGraph, error) {
	msgs, err := s.ListMessages(ctx, sessionID)
	if err != nil {
		return SessionGraph{}, err
	}
	partsByMsg, err := s.listPartsBySession(ctx, sessionID)
	if err != nil {
		return SessionGraph{}, err
	}
	tcs, err := s.ListToolCalls(ctx, sessionID)
	if err != nil {
		return SessionGraph{}, err
	}

	byPart := make(map[string]ToolCall, len(tcs))
	for _, tc := range tcs {
		byPart[tc.PartID] = tc
	}

	entries := make([]SessionEntry, 0, len(msgs))
	for _, msg := range msgs {
		entries = append(entries, SessionEntry{
			Message: msg,
			Parts:   partsByMsg[msg.ID],
		})
	}
	return SessionGraph{Entries: entries, ToolCallsByPart: byPart}, nil
}

// listPartsBySession returns parts for every message in the session,
// keyed by message id, each slice ordered by part seq.
func (s *Store) listPartsBySession(ctx context.Context, sessionID string) (map[string][]Part, error) {
	// Column list is a fixed const (same fields as partColumns), not user input.
	const q = `SELECT p.id, p.message_id, p.type, p.time_created, p.seq, p.text, p.time_start, p.time_end, p.finish_reason, p.tokens_total, p.tokens_input, p.tokens_output, p.tokens_reasoning, p.tokens_cache_read, p.tokens_cache_write, p.cost, p.tool_name, p.tool_call_id, p.tool_status
FROM parts p
JOIN messages m ON m.id = p.message_id
WHERE m.session_id = ?
ORDER BY m.seq, p.seq`
	rows, err := s.db.QueryContext(ctx, q, sessionID)
	if err != nil {
		return nil, fmt.Errorf("db: list parts by session: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string][]Part)
	for rows.Next() {
		var p Part
		if err := rows.Scan(&p.ID, &p.MessageID, &p.Type, &p.TimeCreated, &p.Seq, &p.Text, &p.TimeStart,
			&p.TimeEnd, &p.FinishReason, &p.TokensTotal, &p.TokensInput, &p.TokensOutput, &p.TokensReasoning,
			&p.TokensCacheRead, &p.TokensCacheWrite, &p.Cost, &p.ToolName, &p.ToolCallID, &p.ToolStatus); err != nil {
			return nil, fmt.Errorf("db: scan part: %w", err)
		}
		out[p.MessageID] = append(out[p.MessageID], p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list parts by session: %w", err)
	}
	return out, nil
}
