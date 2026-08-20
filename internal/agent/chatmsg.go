package agent

import "github.com/chinmay-sawant/lazykoder/internal/provider/opencode"

// ChatMessage is the agent-owned turn message. Wire mapping to
// opencode.Message happens only at the Client call boundary.
type ChatMessage struct {
	Role       string
	Content    string
	ToolCallID string
	ToolCalls  []ChatToolCall
}

// ChatToolCall is one model-requested tool invocation inside the agent loop.
type ChatToolCall struct {
	ID        string
	Name      string
	Arguments string
}

func toWireMessages(msgs []ChatMessage) []opencode.Message {
	out := make([]opencode.Message, len(msgs))
	for i, m := range msgs {
		out[i] = opencode.Message{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
			ToolCalls:  toWireToolCalls(m.ToolCalls),
		}
	}
	return out
}

func fromWireToolCalls(tcs []opencode.ToolCall) []ChatToolCall {
	if len(tcs) == 0 {
		return nil
	}
	out := make([]ChatToolCall, len(tcs))
	for i, tc := range tcs {
		out[i] = ChatToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments}
	}
	return out
}

func toWireToolCalls(tcs []ChatToolCall) []opencode.ToolCall {
	if len(tcs) == 0 {
		return nil
	}
	out := make([]opencode.ToolCall, len(tcs))
	for i, tc := range tcs {
		out[i] = opencode.ToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments}
	}
	return out
}
