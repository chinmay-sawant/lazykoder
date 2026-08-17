package agent

import (
	"context"
	"fmt"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

type histEntry struct {
	msg   db.Message
	parts []db.Part
}

func (a *Agent) buildHistory(ctx context.Context) ([]opencode.Message, error) {
	raw, err := a.loadHistory(ctx)
	if err != nil {
		return nil, err
	}
	keep := a.opts.KeepTokens
	if keep <= 0 {
		keep = DefaultKeepTokens
	}
	return PruneToolOutputs(raw, keep), nil
}

func (a *Agent) loadHistory(ctx context.Context) ([]opencode.Message, error) {
	entries, byPart, err := a.sessionEntries(ctx)
	if err != nil {
		return nil, err
	}
	start := 0
	var checkpoint *CompactEnvelope
	for i, entry := range entries {
		env, ok := compactionOf(entry)
		if !ok {
			continue
		}
		copied := env
		checkpoint = &copied
		start = i + 1
		if env.TailStartMessageID == "" {
			continue
		}
		if idx := indexMessageID(entries, env.TailStartMessageID); idx >= 0 {
			start = idx
		}
	}
	out := make([]opencode.Message, 0, len(entries)+1)
	if checkpoint != nil {
		out = append(out, opencode.Message{
			Role:    "user",
			Content: compactCheckpointLead + checkpoint.Summary,
		})
	}
	for _, entry := range entries[start:] {
		if _, ok := compactionOf(entry); ok {
			continue
		}
		out = append(out, a.entryMessages(entry, byPart)...)
	}
	return out, nil
}

func (a *Agent) sessionEntries(ctx context.Context) ([]histEntry, map[string]db.ToolCall, error) {
	msgs, err := a.store.ListMessages(ctx, a.sessionID())
	if err != nil {
		return nil, nil, fmt.Errorf("agent: list messages: %w", err)
	}
	tcs, err := a.store.ListToolCalls(ctx, a.sessionID())
	if err != nil {
		return nil, nil, fmt.Errorf("agent: list tool calls: %w", err)
	}
	byPart := make(map[string]db.ToolCall, len(tcs))
	for _, tc := range tcs {
		byPart[tc.PartID] = tc
	}
	entries := make([]histEntry, 0, len(msgs))
	for _, msg := range msgs {
		parts, err := a.store.ListParts(ctx, msg.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("agent: list parts: %w", err)
		}
		entries = append(entries, histEntry{msg: msg, parts: parts})
	}
	return entries, byPart, nil
}

func (a *Agent) entryMessages(entry histEntry, byPart map[string]db.ToolCall) []opencode.Message {
	switch entry.msg.Role {
	case "user":
		return []opencode.Message{{Role: "user", Content: concatText(entry.parts)}}
	case "assistant":
		msg := opencode.Message{Role: "assistant", Content: concatText(entry.parts)}
		var toolParts []db.Part
		for _, p := range entry.parts {
			if p.Type != "tool" || p.ToolCallID == nil {
				continue
			}
			toolParts = append(toolParts, p)
			name := ""
			if p.ToolName != nil {
				name = *p.ToolName
			}
			args := ""
			if tc, ok := byPart[p.ID]; ok {
				args = tc.InputJSON
			}
			msg.ToolCalls = append(msg.ToolCalls, opencode.ToolCall{
				ID:        *p.ToolCallID,
				Name:      name,
				Arguments: args,
			})
		}
		out := []opencode.Message{msg}
		for _, p := range toolParts {
			out = append(out, opencode.Message{
				Role:       "tool",
				ToolCallID: *p.ToolCallID,
				Content:    a.toolResult(p, byPart[p.ID]),
			})
		}
		return out
	default:
		return nil
	}
}

func compactionOf(entry histEntry) (CompactEnvelope, bool) {
	if entry.msg.Agent == CompactAgentName {
		for _, p := range entry.parts {
			if p.Type == CompactPartType && p.Text != nil {
				return ParseCompactText(*p.Text), true
			}
		}
	}
	for _, p := range entry.parts {
		if p.Type == CompactPartType && p.Text != nil {
			return ParseCompactText(*p.Text), true
		}
	}
	return CompactEnvelope{}, false
}

func indexMessageID(entries []histEntry, id string) int {
	for i, entry := range entries {
		if entry.msg.ID == id {
			return i
		}
	}
	return -1
}
