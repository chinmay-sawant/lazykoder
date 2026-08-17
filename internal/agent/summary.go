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
	msgs, err := store.ListMessages(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("agent: list messages: %w", err)
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "assistant" {
			continue
		}
		parts, err := store.ListParts(ctx, msgs[i].ID)
		if err != nil {
			return "", fmt.Errorf("agent: list parts: %w", err)
		}
		var b strings.Builder
		for _, p := range parts {
			if p.Type == "text" && p.Text != nil {
				b.WriteString(*p.Text)
			}
		}
		return truncateRunes(b.String(), maxToolOutput), nil
	}
	return "", nil
}
