package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

// LastAssistantText returns the concatenated text parts of the last assistant
// message in the session, truncated to maxToolOutput runes.
func LastAssistantText(ctx context.Context, store *db.Store, sessionID string) (string, error) {
	if store == nil || sessionID == "" {
		return "", nil
	}
	graph, err := store.LoadSessionGraph(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("agent: load session graph: %w", err)
	}
	for i := len(graph.Entries) - 1; i >= 0; i-- {
		e := graph.Entries[i]
		if e.Message.Role != "assistant" {
			continue
		}
		var b strings.Builder
		for _, p := range e.Parts {
			if p.Type == "text" && p.Text != nil {
				b.WriteString(*p.Text)
			}
		}
		return truncateRunes(b.String(), maxToolOutput), nil
	}
	return "", nil
}
