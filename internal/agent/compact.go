package agent

import (
	"encoding/json"
	"strings"
)

const (
	// EstimateCharsPerToken matches the TUI chars/4 floor.
	EstimateCharsPerToken = 4
	// DefaultCompactPercent is when auto-compact fires (percent of window).
	DefaultCompactPercent = 80
	// DefaultKeepTokens is the recent tail kept beside a summary.
	DefaultKeepTokens = 15_000
	// DefaultSummarizerReserve is the output allowance for a compact call.
	DefaultSummarizerReserve = 4_096
	// PrunedToolPlaceholder replaces old tool bodies in the provider request.
	PrunedToolPlaceholder = "[old tool result cleared]"
	// CompactPartType is parts.type for a durable checkpoint.
	CompactPartType = "compaction"
	// CompactAgentName is messages.agent for a compact turn.
	CompactAgentName = "compaction"
	// CompactReasonAuto is preflight compaction against the live window.
	CompactReasonAuto = "auto"
	// CompactReasonOverflow is a provider context-overflow retry.
	CompactReasonOverflow = "overflow"
	// CompactReasonShrink is a mid-session switch to a smaller window.
	CompactReasonShrink = "model-shrink"
	// CompactReasonManual is an explicit /compact.
	CompactReasonManual = "manual"

	protectUserTurns = 2
	// summarizerSlack is a small reserve besides the output cap.
	summarizerSlack = 256
	// minChunkRunes is the smallest split used when the head is huge.
	minChunkRunes = 256
	// percentScale is the denominator for percent calculations (100%).
	percentScale = 100
)

// CompactEnvelope is stored as JSON in a compaction part's text.
type CompactEnvelope struct {
	Summary            string `json:"summary"`
	TailStartMessageID string `json:"tail_start_message_id,omitempty"`
	FromModel          string `json:"from_model,omitempty"`
	ToModel            string `json:"to_model,omitempty"`
	FromWindow         int64  `json:"from_window,omitempty"`
	ToWindow           int64  `json:"to_window,omitempty"`
	Reason             string `json:"reason,omitempty"`
	// TokensAfter is the estimated model context after this checkpoint
	// (summary + kept tail). The TUI uses it as the live fill meter.
	TokensAfter int64 `json:"tokens_after,omitempty"`
}

// ModelRef is a model id plus its catalog window and endpoint.
type ModelRef struct {
	ID       string
	Context  int64
	Endpoint string
}

// EstimateTokens is chars/4, matching the TUI floor. Empty text is 0.
func EstimateTokens(text string) int64 {
	n := len([]rune(text))
	if n == 0 {
		return 0
	}
	return int64(n) / EstimateCharsPerToken
}

// EstimateMessages sums a conservative token estimate for a request body.
func EstimateMessages(msgs []ChatMessage) int64 {
	var n int64
	for _, msg := range msgs {
		n += messageTokens(msg)
	}
	return n
}

// NeedsCompact reports whether estimate exceeds percent of window.
// percent is 1-100 (0 uses DefaultCompactPercent). A zero or unknown
// window never triggers compaction.
func NeedsCompact(estimate, window int64, percent int) bool {
	if window <= 0 {
		return false
	}
	if percent <= 0 {
		percent = DefaultCompactPercent
	}
	if percent > percentScale {
		percent = percentScale
	}
	limit := window * int64(percent) / percentScale
	return estimate > limit
}

// PickSummarizer chooses the model that can see the history.
// When the incoming window cannot hold estimate+reserve and the outgoing
// window is larger, the outgoing model writes the checkpoint.
func PickSummarizer(outgoing, incoming ModelRef, estimate, reserve int64) ModelRef {
	if reserve < 0 {
		reserve = 0
	}
	fitsIncoming := incoming.Context > 0 && estimate <= incoming.Context-reserve
	if fitsIncoming {
		return incoming
	}
	if outgoing.Context > incoming.Context {
		return outgoing
	}
	if incoming.ID != "" {
		return incoming
	}
	return outgoing
}

// PruneToolOutputs replaces tool bodies older than the last two user turns
// and outside the keep-tokens tail with a short placeholder. Other roles
// are left intact. The slice is copied; callers can reuse the input.
func PruneToolOutputs(msgs []ChatMessage, keepTokens int64) []ChatMessage {
	if len(msgs) == 0 {
		return []ChatMessage{}
	}
	if keepTokens < 0 {
		keepTokens = 0
	}
	protected := make([]bool, len(msgs))
	protectUserTail(msgs, protected)
	protectTokenTail(msgs, keepTokens, protected)
	out := make([]ChatMessage, len(msgs))
	copy(out, msgs)
	for i := range out {
		if protected[i] || out[i].Role != "tool" {
			continue
		}
		out[i].Content = PrunedToolPlaceholder
	}
	return out
}

// EncodeCompactText serializes a checkpoint envelope.
func EncodeCompactText(env CompactEnvelope) string {
	raw, err := json.Marshal(env)
	if err != nil {
		return env.Summary
	}
	return string(raw)
}

// ParseCompactText reads a checkpoint envelope. Plain text is treated as
// a summary-only envelope so older rows stay readable.
func ParseCompactText(text string) CompactEnvelope {
	text = strings.TrimSpace(text)
	if text == "" {
		return CompactEnvelope{}
	}
	var env CompactEnvelope
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		return CompactEnvelope{Summary: text}
	}
	if env.Summary == "" && env.Reason == "" && env.TailStartMessageID == "" {
		return CompactEnvelope{Summary: text}
	}
	return env
}

func messageTokens(msg ChatMessage) int64 {
	n := EstimateTokens(msg.Content) + EstimateTokens(msg.ToolCallID)
	for _, tc := range msg.ToolCalls {
		n += EstimateTokens(tc.ID) + EstimateTokens(tc.Name) + EstimateTokens(tc.Arguments)
	}
	return n
}

func protectUserTail(msgs []ChatMessage, protected []bool) {
	seen := 0
	start := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "user" {
			continue
		}
		seen++
		start = i
		if seen >= protectUserTurns {
			break
		}
	}
	if start < 0 {
		return
	}
	for i := start; i < len(msgs); i++ {
		protected[i] = true
	}
}

func protectTokenTail(msgs []ChatMessage, keepTokens int64, protected []bool) {
	var acc int64
	for i := len(msgs) - 1; i >= 0; i-- {
		size := messageTokens(msgs[i])
		if acc > 0 && acc+size > keepTokens {
			return
		}
		protected[i] = true
		acc += size
	}
}
