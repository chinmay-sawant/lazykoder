// Package agent drives provider turns and persists them to the session store.
package agent

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/policy"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/tools/question"
)

const (
	// maxSessionTitle caps the session title derived from the first prompt.
	maxSessionTitle = 60
	// maxToolTitle caps the one-line tool title shown in the transcript.
	maxToolTitle = 80
	// maxToolOutput caps the output returned to the model for a tool call.
	maxToolOutput = 8000
	// defaultMaxSteps bounds tool-calling turns when no limit is configured.
	defaultMaxSteps = 16
)

// Options configures an Agent.
type Options struct {
	Session  *db.Session
	MaxSteps int
	// Model overrides the provider default for new sessions and every
	// chat request. When empty the client default applies.
	Model string
	// Endpoint is the chat-completions URL for Model, from models.json.
	// Empty uses the client default base.
	Endpoint string
	// Variant is the selected reasoning effort (low, medium, high).
	// Empty omits the field so the provider default applies.
	Variant string
	// Confirm is invoked when a tool call needs a human decision (policy.ActionAsk).
	Confirm func(dec policy.Decision, subject string) (bool, error)
	// Ask is invoked when the model calls the question tool; it must return
	// the chosen option index. When nil, question calls fail as denied.
	Ask func(q question.Question) (int, error)
	// DisableStreaming forces the non-streaming Chat path. Streaming is
	// the default so reasoning and text can paint as they arrive.
	DisableStreaming bool
	// Host enables task-family tools for parent agents. Nil denies them.
	Host SubagentHost
	// ToolNames is the base-tool allowlist (bash/read/...). Empty uses
	// DefaultParentTools. Task tools are never granted via this list alone.
	ToolNames []string
	// AgentName is written to messages.agent (empty = main parent agent).
	AgentName string
	// BashAllowlist controls optional strict command allowlisting.
	BashAllowlist        []string
	BashAllowlistEnabled bool
	// WebfetchClient is an explicit egress client, primarily for injected tests.
	// Production leaves it nil, so webfetch uses its validated default client.
	WebfetchClient *http.Client
	// ContextWindow is the live model's catalog context (0 = unknown).
	ContextWindow int64
	// TokensUsed is the last known fill, used as a floor on the estimate.
	TokensUsed int64
	// OutgoingModel / OutgoingWindow / OutgoingEndpoint describe the model
	// that produced the current history, used when shrinking the window.
	OutgoingModel    string
	OutgoingWindow   int64
	OutgoingEndpoint string
	// CompactAuto runs the preflight size check. Manual /compact and one
	// overflow retry stay available when this is false.
	CompactAuto bool
	// CompactPercent is the fill of ContextWindow that triggers auto-compact.
	// 0 uses DefaultCompactPercent (80).
	CompactPercent int
	// KeepTokens is the recent tail kept beside a summary. 0 uses the default.
	KeepTokens int64
	// CompactReason is an explicit preflight reason (model-shrink).
	CompactReason string
	// CompactInstructions are extra /compact notes appended to the prompt.
	CompactInstructions string
}

// Agent runs user turns against a store and provider client.
type Agent struct {
	store   *db.Store
	client  *opencode.Client
	workdir string
	opts    Options

	sess *db.Session

	// projectInstructions caches workdir AGENTS.md after the first load.
	projectInstructions     string
	projectInstructionsPath string
	projectInstructionsDone bool
}

// New returns an Agent for the given store, provider client and workdir.
func New(store *db.Store, client *opencode.Client, workdir string, opts Options) *Agent {
	return &Agent{store: store, client: client, workdir: workdir, opts: opts}
}

// ensureProjectInstructions loads AGENTS.md once per Agent.
func (a *Agent) ensureProjectInstructions() {
	if a.projectInstructionsDone {
		return
	}
	a.projectInstructionsDone = true
	content, path, ok := LoadProjectInstructions(a.workdir)
	if !ok {
		return
	}
	a.projectInstructions = content
	a.projectInstructionsPath = path
}

// withProjectInstructions prepends a system message when AGENTS.md is present.
func (a *Agent) withProjectInstructions(history []ChatMessage) []ChatMessage {
	a.ensureProjectInstructions()
	body := FormatProjectInstructionsMessage(a.projectInstructions)
	if body == "" {
		return history
	}
	out := make([]ChatMessage, 0, len(history)+1)
	out = append(out, ChatMessage{Role: "system", Content: body})
	out = append(out, history...)
	return out
}

// EventKind classifies streamed events.
type EventKind int

const (
	// EventSessionCreated fires when a new session row is written.
	EventSessionCreated EventKind = iota
	// EventMessage fires when a message row is written.
	EventMessage
	// EventPart fires when a part row is written.
	EventPart
	// EventTool fires when a tool_calls row is written or updated.
	EventTool
	// EventTokenDelta fires for one streamed text/reasoning delta.
	EventTokenDelta
	// EventStepMetrics fires after a step-finish row is persisted.
	EventStepMetrics
	// EventError fires on a fatal turn error.
	EventError
	// EventDone fires after a successful turn, before the channel closes.
	EventDone
	// EventCompacting fires when a summarizer call starts.
	EventCompacting
	// EventCompacted fires after a checkpoint is persisted.
	EventCompacted
)

// Event is one streamed write or error during Send.
// Part/Tool are UI deltas; db rows stay inside agent persistence code.
type Event struct {
	Kind         EventKind
	SessionID    string
	MessageID    string
	Role         string
	Part         PartDelta
	Tool         ToolDelta
	TokenDelta   int64
	TokensOutput int64
	ElapsedMS    int64
	TokensUsed   int64
	Err          error
}

// Send runs one user turn, closing events when done.
func (a *Agent) Send(ctx context.Context, userText string, events chan<- Event) (err error) {
	if events != nil {
		defer close(events)
		defer func() {
			if err == nil {
				a.emit(events, Event{Kind: EventDone, SessionID: a.sessionID()})
			}
		}()
	}
	if err = a.ensureSession(ctx, userText, events); err != nil {
		return err
	}
	if err = a.writeUserTurn(ctx, userText, events); err != nil {
		return err
	}
	return a.runSteps(ctx, events)
}

// Continue resumes the agent loop on the current session history without
// writing a new user message. Used after a step-limit stop so the model
// can keep working with another MaxSteps budget.
func (a *Agent) Continue(ctx context.Context, events chan<- Event) (err error) {
	if events != nil {
		defer close(events)
		defer func() {
			if err == nil {
				a.emit(events, Event{Kind: EventDone, SessionID: a.sessionID()})
			}
		}()
	}
	if a.sess == nil && a.opts.Session != nil {
		a.sess = a.opts.Session
	}
	if a.sessionID() == "" {
		return a.fail(events, fmt.Errorf("agent: continue requires an existing session"))
	}
	return a.runSteps(ctx, events)
}

func (a *Agent) runSteps(ctx context.Context, events chan<- Event) error {
	maxSteps := a.opts.MaxSteps
	if maxSteps <= 0 {
		maxSteps = defaultMaxSteps
	}
	for step := 0; step < maxSteps; step++ {
		resp, err := a.stepOnce(ctx, events)
		if err != nil {
			return a.fail(events, err)
		}
		if resp.FinishReason != "tool-calls" && len(resp.ToolCalls) == 0 {
			break
		}
		if step == maxSteps-1 {
			return a.fail(events, fmt.Errorf("agent: step limit reached (max %d)", maxSteps))
		}
	}
	return nil
}

func (a *Agent) stepOnce(ctx context.Context, events chan<- Event) (*opencode.ChatResponse, error) {
	history, err := a.buildHistory(ctx)
	if err != nil {
		return nil, err
	}
	if err := a.maybeCompact(ctx, events, history); err != nil {
		return nil, err
	}
	history, err = a.buildHistory(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := a.callModel(ctx, events, history)
	if err == nil {
		return resp, nil
	}
	if !IsContextOverflow(err) {
		return nil, fmt.Errorf("agent: provider: %w", err)
	}
	if cerr := a.runCompact(ctx, events, CompactReasonOverflow, ""); cerr != nil {
		return nil, fmt.Errorf("agent: compact after overflow: %w", cerr)
	}
	history, err = a.buildHistory(ctx)
	if err != nil {
		return nil, err
	}
	resp, err = a.callModel(ctx, events, history)
	if err != nil {
		return nil, fmt.Errorf("agent: provider: %w", err)
	}
	return resp, nil
}

func (a *Agent) callModel(ctx context.Context, events chan<- Event, history []ChatMessage) (*opencode.ChatResponse, error) {
	req := opencode.ChatRequest{
		Model:           a.opts.Model,
		Endpoint:        a.opts.Endpoint,
		ReasoningEffort: a.opts.Variant,
		Messages:        toWireMessages(a.withProjectInstructions(history)),
		Tools:           toolSpecsFor(a.opts.ToolNames, a.opts.Host),
	}
	if a.opts.DisableStreaming {
		resp, err := a.client.Chat(ctx, req)
		if err != nil {
			return nil, err
		}
		if err := a.writeResponse(ctx, events, resp); err != nil {
			return nil, err
		}
		return resp, nil
	}
	return a.streamStep(ctx, events, req)
}

func (a *Agent) fail(events chan<- Event, err error) error {
	a.emit(events, Event{Kind: EventError, SessionID: a.sessionID(), Err: err})
	return err
}

func (a *Agent) sessionID() string {
	if a.sess == nil {
		return ""
	}
	return a.sess.ID
}

func (a *Agent) emit(events chan<- Event, ev Event) {
	if events != nil {
		events <- ev
	}
}

func (a *Agent) ensureSession(ctx context.Context, userText string, events chan<- Event) error {
	if a.sess != nil {
		return nil
	}
	if a.opts.Session != nil {
		a.sess = a.opts.Session
		return nil
	}
	sess, err := a.store.CreateSession(ctx, db.Session{
		Title:     truncateRunes(strings.TrimSpace(userText), maxSessionTitle),
		Directory: a.workdir,
		Model:     a.opts.Model,
		Variant:   strPtr(a.opts.Variant),
	})
	if err != nil {
		return a.fail(events, fmt.Errorf("agent: create session: %w", err))
	}
	a.sess = &sess
	a.emit(events, Event{Kind: EventSessionCreated, SessionID: sess.ID})
	return nil
}

func (a *Agent) writeUserTurn(ctx context.Context, userText string, events chan<- Event) error {
	m, err := a.store.InsertMessage(ctx, db.Message{
		SessionID: a.sessionID(),
		Role:      "user",
		Agent:     a.opts.AgentName,
	})
	if err != nil {
		return fmt.Errorf("agent: insert user message: %w", err)
	}
	a.emit(events, Event{Kind: EventMessage, SessionID: a.sessionID(), MessageID: m.ID, Role: "user"})
	text := userText
	part, err := a.store.InsertPart(ctx, db.Part{MessageID: m.ID, Type: "text", Text: &text})
	if err != nil {
		return fmt.Errorf("agent: insert user part: %w", err)
	}
	a.emit(events, Event{Kind: EventPart, SessionID: a.sessionID(), MessageID: m.ID, Part: partDeltaFromDB(part)})
	return nil
}

func concatText(parts []db.Part) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" && p.Text != nil {
			b.WriteString(*p.Text)
		}
	}
	return b.String()
}

func (a *Agent) writeResponse(ctx context.Context, events chan<- Event, resp *opencode.ChatResponse) error {
	started := time.Now()
	m, err := a.beginAssistant(ctx, events)
	if err != nil {
		return err
	}
	if resp.Reasoning != "" {
		if err := a.writeTextPart(ctx, events, m.ID, "reasoning", resp.Reasoning); err != nil {
			return err
		}
	}
	if resp.Content != "" {
		if err := a.writeTextPart(ctx, events, m.ID, "text", resp.Content); err != nil {
			return err
		}
	}
	if err := a.runTools(ctx, events, m.ID, fromWireToolCalls(resp.ToolCalls)); err != nil {
		return err
	}
	return a.writeStepFinish(ctx, events, m.ID, resp, started)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
