package agent

import (
	"context"
	"fmt"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

func (a *Agent) streamStep(ctx context.Context, events chan<- Event, req opencode.ChatRequest) (*opencode.ChatResponse, error) {
	m, err := a.beginAssistant(ctx, events)
	if err != nil {
		return nil, err
	}
	var reasonPart, textPart *db.Part
	resp, err := a.client.ChatStream(ctx, req, func(d opencode.Delta) error {
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
	if err := a.runTools(ctx, events, m.ID, resp.ToolCalls); err != nil {
		return nil, err
	}
	if err := a.writeStepFinish(ctx, events, m.ID, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (a *Agent) beginAssistant(ctx context.Context, events chan<- Event) (db.Message, error) {
	m, err := a.store.InsertMessage(ctx, db.Message{
		SessionID: a.sessionID(),
		Role:      "assistant",
		Agent:     a.opts.AgentName,
		ModelID:   a.opts.Model,
		Variant:   strPtr(a.opts.Variant),
	})
	if err != nil {
		return db.Message{}, fmt.Errorf("agent: insert assistant message: %w", err)
	}
	a.emit(events, Event{Kind: EventMessage, SessionID: a.sessionID(), MessageID: m.ID, Role: "assistant"})
	part, err := a.store.InsertPart(ctx, db.Part{MessageID: m.ID, Type: "step-start"})
	if err != nil {
		return db.Message{}, fmt.Errorf("agent: insert step-start: %w", err)
	}
	a.emit(events, Event{Kind: EventPart, SessionID: a.sessionID(), MessageID: m.ID, Part: part})
	return m, nil
}

func (a *Agent) writeTextPart(ctx context.Context, events chan<- Event, msgID, typ, text string) error {
	part, err := a.store.InsertPart(ctx, db.Part{MessageID: msgID, Type: typ, Text: &text})
	if err != nil {
		return fmt.Errorf("agent: insert %s: %w", typ, err)
	}
	a.emit(events, Event{Kind: EventPart, SessionID: a.sessionID(), MessageID: msgID, Part: part})
	return nil
}

func (a *Agent) writeStepFinish(ctx context.Context, events chan<- Event, msgID string, resp *opencode.ChatResponse) error {
	if resp == nil || resp.Usage == nil {
		return nil
	}
	u := resp.Usage
	part, err := a.store.InsertPart(ctx, db.Part{
		MessageID:        msgID,
		Type:             "step-finish",
		FinishReason:     strPtr(resp.FinishReason),
		TokensTotal:      &u.TokensTotal,
		TokensInput:      &u.TokensInput,
		TokensOutput:     &u.TokensOutput,
		TokensReasoning:  &u.TokensReasoning,
		TokensCacheRead:  &u.TokensCacheRead,
		TokensCacheWrite: &u.TokensCacheWrite,
		Cost:             &u.Cost,
	})
	if err != nil {
		return fmt.Errorf("agent: insert step-finish: %w", err)
	}
	a.emit(events, Event{Kind: EventPart, SessionID: a.sessionID(), MessageID: msgID, Part: part})
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
		a.emit(events, Event{Kind: EventPart, SessionID: a.sessionID(), MessageID: msgID, Part: part})
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
	a.emit(events, Event{Kind: EventPart, SessionID: a.sessionID(), MessageID: msgID, Part: **slot})
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
	a.emit(events, Event{Kind: EventPart, SessionID: a.sessionID(), MessageID: msgID, Part: **slot})
	return nil
}
