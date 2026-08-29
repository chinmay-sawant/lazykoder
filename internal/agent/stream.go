package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

const tokenRunesPerUnit = 4

func (a *Agent) streamStep(ctx context.Context, events chan<- Event, req opencode.ChatRequest) (*opencode.ChatResponse, error) {
	started := time.Now()
	m, err := a.beginAssistant(ctx, events)
	if err != nil {
		return nil, err
	}
	var reasonPart, textPart *db.Part
	resp, err := a.client.ChatStream(ctx, req, func(d opencode.Delta) error {
		chunk := estimateTokenDelta(d.Reasoning + d.Content)
		if chunk > 0 {
			a.emit(ctx, events, Event{Kind: EventTokenDelta, SessionID: a.sessionID(), TokenDelta: chunk,
				ElapsedMS: time.Since(started).Milliseconds()})
		}
		if err := a.appendPartDelta(ctx, events, m.ID, "reasoning", d.Reasoning, &reasonPart); err != nil {
			return err
		}
		return a.appendPartDelta(ctx, events, m.ID, "text", d.Content, &textPart)
	})
	if err != nil {
		return nil, fmt.Errorf("agent: provider: %w", err)
	}
	if err := a.ensurePartText(ctx, events, m.ID, "reasoning", resp.Reasoning, &reasonPart); err != nil {
		return nil, err
	}
	if err := a.ensurePartText(ctx, events, m.ID, "text", resp.Content, &textPart); err != nil {
		return nil, err
	}
	if err := a.runTools(ctx, events, m.ID, fromWireToolCalls(resp.ToolCalls)); err != nil {
		return nil, err
	}
	if err := a.writeStepFinish(ctx, events, m.ID, resp, started); err != nil {
		return nil, err
	}
	return resp, nil
}

func estimateTokenDelta(text string) int64 {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	runes := int64(len([]rune(text)))
	return max(1, (runes+tokenRunesPerUnit-1)/tokenRunesPerUnit)
}

// FormatTokensPerSecond formats a completed step rate for diagnostics and
// tests. A missing or zero-duration sample is intentionally represented as
// "-" instead of a misleading infinity or NaN.
func FormatTokensPerSecond(tokens int64, elapsed time.Duration) string {
	if tokens <= 0 || elapsed <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", float64(tokens)/elapsed.Seconds())
}

func (a *Agent) beginAssistant(ctx context.Context, events chan<- Event) (db.Message, error) {
	m, err := a.store.InsertMessage(ctx, db.Message{
		SessionID:  a.sessionID(),
		Role:       "assistant",
		Agent:      a.opts.AgentName,
		ProviderID: a.opts.Provider,
		ModelID:    a.opts.Model,
		Variant:    strPtr(a.opts.Variant),
	})
	if err != nil {
		return db.Message{}, fmt.Errorf("agent: insert assistant message: %w", err)
	}
	a.emit(ctx, events, Event{Kind: EventMessage, SessionID: a.sessionID(), MessageID: m.ID, Role: "assistant"})
	part, err := a.store.InsertPart(ctx, db.Part{MessageID: m.ID, Type: "step-start"})
	if err != nil {
		return db.Message{}, fmt.Errorf("agent: insert step-start: %w", err)
	}
	a.emit(ctx, events, Event{Kind: EventPart, SessionID: a.sessionID(), MessageID: m.ID, Part: partDeltaFromDB(part)})
	return m, nil
}

func (a *Agent) writeTextPart(ctx context.Context, events chan<- Event, msgID, typ, text string) error {
	part, err := a.store.InsertPart(ctx, db.Part{MessageID: msgID, Type: typ, Text: &text})
	if err != nil {
		return fmt.Errorf("agent: insert %s: %w", typ, err)
	}
	a.emit(ctx, events, Event{Kind: EventPart, SessionID: a.sessionID(), MessageID: msgID, Part: partDeltaFromDB(part)})
	return nil
}

func (a *Agent) writeStepFinish(ctx context.Context, events chan<- Event, msgID string, resp *opencode.ChatResponse, started time.Time) error {
	if resp == nil || resp.Usage == nil {
		return nil
	}
	part := db.Part{
		MessageID:    msgID,
		Type:         "step-finish",
		FinishReason: strPtr(resp.FinishReason),
	}
	startMS := started.UnixMilli()
	endMS := time.Now().UnixMilli()
	part.TimeStart = &startMS
	part.TimeEnd = &endMS
	u := resp.Usage
	part.TokensTotal = &u.TokensTotal
	part.TokensInput = &u.TokensInput
	part.TokensOutput = &u.TokensOutput
	part.TokensReasoning = &u.TokensReasoning
	part.TokensCacheRead = &u.TokensCacheRead
	part.TokensCacheWrite = &u.TokensCacheWrite
	part.Cost = &u.Cost
	inserted, err := a.store.InsertPart(ctx, part)
	if err != nil {
		return fmt.Errorf("agent: insert step-finish: %w", err)
	}
	a.emit(ctx, events, Event{Kind: EventPart, SessionID: a.sessionID(), MessageID: msgID, Part: partDeltaFromDB(inserted)})
	var output int64
	output = resp.Usage.TokensOutput
	a.emit(ctx, events, Event{Kind: EventStepMetrics, SessionID: a.sessionID(), MessageID: msgID,
		TokensOutput: output, ElapsedMS: endMS - startMS})
	return nil
}

func (a *Agent) appendPartDelta(ctx context.Context, events chan<- Event, msgID, typ, chunk string, slot **db.Part) error {
	if chunk == "" {
		return nil
	}
	if *slot == nil {
		text := chunk
		part, err := a.store.InsertPart(ctx, db.Part{MessageID: msgID, Type: typ, Text: &text})
		if err != nil {
			return fmt.Errorf("agent: insert %s: %w", typ, err)
		}
		*slot = &part
		a.emit(ctx, events, Event{Kind: EventPart, SessionID: a.sessionID(), MessageID: msgID, Part: partDeltaFromDB(part)})
		return nil
	}
	text := chunk
	if (*slot).Text != nil {
		text = *(*slot).Text + chunk
	}
	if err := a.store.UpdatePartText(ctx, (*slot).ID, text); err != nil {
		return err
	}
	(*slot).Text = &text
	a.emit(ctx, events, Event{Kind: EventPart, SessionID: a.sessionID(), MessageID: msgID, Part: partDeltaFromDB(**slot)})
	return nil
}

func (a *Agent) ensurePartText(ctx context.Context, events chan<- Event, msgID, typ, want string, slot **db.Part) error {
	if want == "" {
		return nil
	}
	if *slot == nil {
		return a.appendPartDelta(ctx, events, msgID, typ, want, slot)
	}
	if (*slot).Text != nil && *(*slot).Text == want {
		return nil
	}
	if err := a.store.UpdatePartText(ctx, (*slot).ID, want); err != nil {
		return err
	}
	(*slot).Text = &want
	a.emit(ctx, events, Event{Kind: EventPart, SessionID: a.sessionID(), MessageID: msgID, Part: partDeltaFromDB(**slot)})
	return nil
}
