package agent

import (
	"context"
	"fmt"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/prompts"
)

type histEntry struct {
	msg   db.Message
	parts []db.Part
}

func (a *Agent) buildHistory(ctx context.Context) ([]ChatMessage, error) {
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

func (a *Agent) loadHistory(ctx context.Context) ([]ChatMessage, error) {
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
	out := make([]ChatMessage, 0, len(entries)+1)
	if checkpoint != nil {
		out = append(out, ChatMessage{
			Role:    "user",
			Content: prompts.New(a.workdir).Must("agent/compact-checkpoint-lead.md") + "\n" + checkpoint.Summary,
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
	graph, err := a.store.LoadSessionGraph(ctx, a.sessionID())
	if err != nil {
		return nil, nil, fmt.Errorf("agent: load session graph: %w", err)
	}
	entries := make([]histEntry, 0, len(graph.Entries))
	for _, e := range graph.Entries {
		entries = append(entries, histEntry{msg: e.Message, parts: e.Parts})
	}
	return entries, graph.ToolCallsByPart, nil
}

func (a *Agent) entryMessages(entry histEntry, byPart map[string]db.ToolCall) []ChatMessage {
	switch entry.msg.Role {
	case "user":
		return []ChatMessage{{Role: "user", Content: concatText(entry.parts)}}
	case "assistant":
		msg := ChatMessage{Role: "assistant", Content: concatText(entry.parts)}
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
			msg.ToolCalls = append(msg.ToolCalls, ChatToolCall{
				ID:        *p.ToolCallID,
				Name:      name,
				Arguments: args,
			})
		}
		out := []ChatMessage{msg}
		for _, p := range toolParts {
			out = append(out, ChatMessage{
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
