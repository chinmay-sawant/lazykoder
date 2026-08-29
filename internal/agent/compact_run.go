package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/prompts"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

// Compact runs a tools-off summarizer turn and persists a checkpoint.
// Manual /compact uses this without starting a normal chat turn.
func (a *Agent) Compact(ctx context.Context, events chan<- Event, reason, extra string) (err error) {
	if events != nil {
		defer close(events)
		defer func() {
			if err == nil {
				a.emit(ctx, events, Event{Kind: EventDone, SessionID: a.sessionID()})
			}
		}()
	}
	if a.sess == nil && a.opts.Session != nil {
		a.sess = a.opts.Session
	}
	if a.sessionID() == "" {
		return a.fail(ctx, events, fmt.Errorf("agent: compact requires an existing session"))
	}
	if err = a.runCompact(ctx, events, reason, extra); err != nil {
		return a.fail(ctx, events, err)
	}
	return nil
}

func (a *Agent) maybeCompact(ctx context.Context, events chan<- Event, history []ChatMessage) error {
	reason := a.opts.CompactReason
	gated := a.opts.CompactAuto || reason == CompactReasonShrink
	if !gated {
		return nil
	}
	est := EstimateMessages(history)
	if a.opts.TokensUsed > est {
		est = a.opts.TokensUsed
	}
	percent := a.opts.CompactPercent
	if percent <= 0 {
		percent = DefaultCompactPercent
	}
	if !NeedsCompact(est, a.opts.ContextWindow, percent) {
		return nil
	}
	if reason == "" {
		reason = CompactReasonAuto
	}
	return a.runCompact(ctx, events, reason, a.opts.CompactInstructions)
}

func (a *Agent) runCompact(ctx context.Context, events chan<- Event, reason, extra string) error {
	a.emit(ctx, events, Event{Kind: EventCompacting, SessionID: a.sessionID()})
	entries, byPart, err := a.sessionEntries(ctx)
	if err != nil {
		return err
	}
	work := entriesAfterCheckpoint(entries)
	if len(work) == 0 {
		return fmt.Errorf("agent: nothing to compact")
	}
	keep := a.opts.KeepTokens
	if keep <= 0 {
		keep = DefaultKeepTokens
	}
	tailIdx := selectTailIndex(work, byPart, keep, true)
	if tailIdx <= 0 {
		if reason == CompactReasonManual {
			return fmt.Errorf("agent: nothing to compact")
		}
		return nil
	}
	head := work[:tailIdx]
	tailStart := work[tailIdx].msg.ID
	incoming := ModelRef{ID: a.opts.Model, Context: a.opts.ContextWindow, Endpoint: a.opts.Endpoint}
	outgoing := ModelRef{
		ID:       a.opts.OutgoingModel,
		Context:  a.opts.OutgoingWindow,
		Endpoint: a.opts.OutgoingEndpoint,
	}
	serialized := serializeEntries(a, head, byPart)
	prompt := prompts.Must("compact.md")
	if prev := previousSummary(entries); prev != "" {
		prompt += "\n\nPrevious summary:\n" + prev
	}
	if extra = strings.TrimSpace(extra); extra != "" {
		prompt += "\n\n## Compact instructions\n\n" + extra
	}
	reserve := int64(DefaultSummarizerReserve)
	estimate := EstimateTokens(prompt) + EstimateTokens(serialized)
	if a.opts.TokensUsed > estimate {
		estimate = a.opts.TokensUsed
	}
	summarizer := PickSummarizer(outgoing, incoming, estimate, reserve)
	summary, err := a.summarize(ctx, summarizer, prompt, serialized, reserve)
	if err != nil {
		return err
	}
	env := CompactEnvelope{
		Summary:            summary,
		TailStartMessageID: tailStart,
		FromModel:          outgoing.ID,
		ToModel:            incoming.ID,
		FromWindow:         outgoing.Context,
		ToWindow:           incoming.Context,
		Reason:             reason,
	}
	if env.FromModel == "" {
		env.FromModel = incoming.ID
		env.FromWindow = incoming.Context
	}
	part, err := a.persistCheckpoint(ctx, events, summarizer.ID, env)
	if err != nil {
		return err
	}
	after, err := a.buildHistory(ctx)
	if err != nil {
		return err
	}
	used := EstimateMessages(after)
	env.TokensAfter = used
	text := EncodeCompactText(env)
	if err := a.store.UpdatePartText(ctx, part.ID, text); err != nil {
		return fmt.Errorf("agent: persist compact tokens: %w", err)
	}
	part.Text = &text
	a.opts.TokensUsed = used
	a.opts.CompactReason = ""
	a.emit(ctx, events, Event{
		Kind:       EventCompacted,
		SessionID:  a.sessionID(),
		MessageID:  part.MessageID,
		Part:       partDeltaFromDB(part),
		TokensUsed: used,
	})
	return nil
}

func (a *Agent) summarize(ctx context.Context, model ModelRef, prompt, conversation string, reserve int64) (string, error) {
	budget := model.Context - reserve - EstimateTokens(prompt) - summarizerSlack
	if model.Context <= 0 {
		budget = EstimateTokens(conversation) + 1
	}
	if budget < summarizerSlack {
		budget = summarizerSlack
	}
	chunks := splitBudget(conversation, budget)
	if len(chunks) == 0 {
		return "", fmt.Errorf("agent: summarizer input is empty")
	}
	if len(chunks) == 1 && model.Context > 0 {
		need := EstimateTokens(prompt) + EstimateTokens(chunks[0])
		if need > model.Context {
			return "", fmt.Errorf("agent: history too large to compact into %s", model.ID)
		}
	}
	var pieces []string
	for i, chunk := range chunks {
		label := chunk
		if len(chunks) > 1 {
			label = fmt.Sprintf("Chunk %d of %d:\n\n%s", i+1, len(chunks), chunk)
		}
		text, err := a.callSummarizer(ctx, model, prompt+"\n\n"+label)
		if err != nil {
			return "", err
		}
		pieces = append(pieces, text)
	}
	if len(pieces) == 1 {
		return pieces[0], nil
	}
	joined := strings.Join(pieces, "\n\n")
	text, err := a.callSummarizer(ctx, model, prompt+"\n\nCombine these chunk summaries into one checkpoint:\n\n"+joined)
	if err != nil {
		return "", err
	}
	return text, nil
}

func (a *Agent) callSummarizer(ctx context.Context, model ModelRef, content string) (string, error) {
	req := opencode.ChatRequest{
		Model:     model.ID,
		Endpoint:  model.Endpoint,
		Messages:  toWireMessages([]ChatMessage{{Role: "user", Content: content}}),
		MaxTokens: int(DefaultSummarizerReserve),
	}
	resp, err := a.client.Chat(ctx, req)
	if err != nil {
		return "", fmt.Errorf("agent: compact: %w", err)
	}
	text := strings.TrimSpace(resp.Content)
	if text == "" {
		return "", fmt.Errorf("agent: compact: empty summary")
	}
	return text, nil
}

func (a *Agent) persistCheckpoint(ctx context.Context, events chan<- Event, modelID string, env CompactEnvelope) (db.Part, error) {
	msg, err := a.store.InsertMessage(ctx, db.Message{
		SessionID: a.sessionID(),
		Role:      "assistant",
		Agent:     CompactAgentName,
		ModelID:   modelID,
	})
	if err != nil {
		return db.Part{}, fmt.Errorf("agent: insert compact message: %w", err)
	}
	a.emit(ctx, events, Event{Kind: EventMessage, SessionID: a.sessionID(), MessageID: msg.ID, Role: "assistant"})
	text := EncodeCompactText(env)
	part, err := a.store.InsertPart(ctx, db.Part{MessageID: msg.ID, Type: CompactPartType, Text: &text})
	if err != nil {
		return db.Part{}, fmt.Errorf("agent: insert compact part: %w", err)
	}
	a.emit(ctx, events, Event{Kind: EventPart, SessionID: a.sessionID(), MessageID: msg.ID, Part: partDeltaFromDB(part)})
	return part, nil
}

// IsContextOverflow reports a provider error that means the request was too large.
func IsContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, n := range []string{
		"context_length_exceeded",
		"maximum context",
		"prompt is too long",
		"context window",
		"prompt too long",
	} {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

func entriesAfterCheckpoint(entries []histEntry) []histEntry {
	start := 0
	for i, entry := range entries {
		if _, ok := compactionOf(entry); ok {
			start = i + 1
		}
	}
	return entries[start:]
}

func selectTailIndex(entries []histEntry, byPart map[string]db.ToolCall, keepTokens int64, forceHead bool) int {
	if len(entries) == 0 {
		return 0
	}
	lastUser := lastUserIndex(entries)
	start := len(entries)
	var acc int64
	for i := len(entries) - 1; i >= 0; i-- {
		acc += entryEstimate(entries[i], byPart)
		start = i
		if acc >= keepTokens && (lastUser < 0 || start <= lastUser) {
			break
		}
	}
	if forceHead && start == 0 && lastUser > 0 {
		return lastUser
	}
	return start
}

func lastUserIndex(entries []histEntry) int {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].msg.Role == "user" {
			return i
		}
	}
	return -1
}

func previousSummary(entries []histEntry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		if env, ok := compactionOf(entries[i]); ok && strings.TrimSpace(env.Summary) != "" {
			return env.Summary
		}
	}
	return ""
}

func serializeEntries(a *Agent, entries []histEntry, byPart map[string]db.ToolCall) string {
	keep := a.opts.KeepTokens
	if keep <= 0 {
		keep = DefaultKeepTokens
	}
	pruned := PruneToolOutputs(flattenEntries(a, entries, byPart), keep)
	var b strings.Builder
	for _, msg := range pruned {
		switch msg.Role {
		case "user":
			fmt.Fprintf(&b, "[User]: %s\n\n", msg.Content)
		case "assistant":
			fmt.Fprintf(&b, "[Assistant]: %s\n", msg.Content)
			for _, tc := range msg.ToolCalls {
				fmt.Fprintf(&b, "[Assistant tool call]: %s(%s)\n", tc.Name, tc.Arguments)
			}
			b.WriteString("\n")
		case "tool":
			fmt.Fprintf(&b, "[Tool result]: %s\n\n", msg.Content)
		}
	}
	return strings.TrimSpace(b.String())
}

func flattenEntries(a *Agent, entries []histEntry, byPart map[string]db.ToolCall) []ChatMessage {
	out := make([]ChatMessage, 0, len(entries))
	for _, entry := range entries {
		out = append(out, a.entryMessages(entry, byPart)...)
	}
	return out
}

func entryEstimate(entry histEntry, byPart map[string]db.ToolCall) int64 {
	n := EstimateTokens(concatText(entry.parts))
	for _, p := range entry.parts {
		if p.Type != "tool" {
			continue
		}
		if tc, ok := byPart[p.ID]; ok && tc.Output != nil {
			n += EstimateTokens(*tc.Output)
		}
	}
	return n
}

func splitBudget(text string, budget int64) []string {
	if budget <= 0 || EstimateTokens(text) <= budget {
		if strings.TrimSpace(text) == "" {
			return nil
		}
		return []string{text}
	}
	runes := []rune(text)
	// 4 chars per token; keep a little slack.
	width := int(budget * EstimateCharsPerToken)
	if width < minChunkRunes {
		width = minChunkRunes
	}
	var chunks []string
	for len(runes) > 0 {
		n := width
		if n > len(runes) {
			n = len(runes)
		}
		chunks = append(chunks, string(runes[:n]))
		runes = runes[n:]
	}
	return chunks
}
