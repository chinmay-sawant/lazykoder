// Package agent drives provider turns and persists them to the session store.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/policy"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/tools/bash"
	"github.com/chinmay-sawant/lazykoder/internal/tools/edit"
	"github.com/chinmay-sawant/lazykoder/internal/tools/grep"
	"github.com/chinmay-sawant/lazykoder/internal/tools/question"
	"github.com/chinmay-sawant/lazykoder/internal/tools/read"
	"github.com/chinmay-sawant/lazykoder/internal/tools/webfetch"
	"github.com/chinmay-sawant/lazykoder/internal/tools/write"
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
}

// Agent runs user turns against a store and provider client.
type Agent struct {
	store   *db.Store
	client  *opencode.Client
	workdir string
	opts    Options

	sess *db.Session
}

// New returns an Agent for the given store, provider client and workdir.
func New(store *db.Store, client *opencode.Client, workdir string, opts Options) *Agent {
	return &Agent{store: store, client: client, workdir: workdir, opts: opts}
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
	// EventError fires on a fatal turn error.
	EventError
	// EventDone fires after a successful turn, before the channel closes.
	EventDone
)

// Event is one streamed write or error during Send.
type Event struct {
	Kind      EventKind
	SessionID string
	MessageID string
	Role      string
	Part      db.Part
	Tool      db.ToolCall
	Err       error
}

type bashArgs struct {
	Command string `json:"command"`
	Workdir string `json:"workdir"`
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
		history, err := a.buildHistory(ctx)
		if err != nil {
			return a.fail(events, err)
		}
		req := opencode.ChatRequest{
			Model:           a.opts.Model,
			Endpoint:        a.opts.Endpoint,
			ReasoningEffort: a.opts.Variant,
			Messages:        history,
			Tools:           toolSpecsFor(a.opts.ToolNames, a.opts.Host),
		}
		var resp *opencode.ChatResponse
		if a.opts.DisableStreaming {
			resp, err = a.client.Chat(ctx, req)
			if err != nil {
				return a.fail(events, fmt.Errorf("agent: provider: %w", err))
			}
			if err := a.writeResponse(ctx, events, resp); err != nil {
				return a.fail(events, err)
			}
		} else {
			resp, err = a.streamStep(ctx, events, req)
			if err != nil {
				return a.fail(events, err)
			}
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
	a.emit(events, Event{Kind: EventPart, SessionID: a.sessionID(), MessageID: m.ID, Part: part})
	return nil
}

func (a *Agent) buildHistory(ctx context.Context) ([]opencode.Message, error) {
	msgs, err := a.store.ListMessages(ctx, a.sessionID())
	if err != nil {
		return nil, fmt.Errorf("agent: list messages: %w", err)
	}
	tcs, err := a.store.ListToolCalls(ctx, a.sessionID())
	if err != nil {
		return nil, fmt.Errorf("agent: list tool calls: %w", err)
	}
	byPart := make(map[string]db.ToolCall, len(tcs))
	for _, tc := range tcs {
		byPart[tc.PartID] = tc
	}
	var out []opencode.Message
	for _, m := range msgs {
		parts, err := a.store.ListParts(ctx, m.ID)
		if err != nil {
			return nil, fmt.Errorf("agent: list parts: %w", err)
		}
		switch m.Role {
		case "user":
			out = append(out, opencode.Message{Role: "user", Content: concatText(parts)})
		case "assistant":
			msg := opencode.Message{Role: "assistant", Content: concatText(parts)}
			var toolParts []db.Part
			for _, p := range parts {
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
			out = append(out, msg)
			for _, p := range toolParts {
				out = append(out, opencode.Message{
					Role:       "tool",
					ToolCallID: *p.ToolCallID,
					Content:    a.toolResult(p, byPart[p.ID]),
				})
			}
		}
	}
	return out, nil
}

func (a *Agent) toolResult(p db.Part, tc db.ToolCall) string {
	switch tc.Status {
	case "completed":
		code := 0
		if tc.ExitCode != nil {
			code = *tc.ExitCode
		}
		out := ""
		if tc.Output != nil {
			out = *tc.Output
		}
		return completedJSON(bash.Result{Stdout: out, ExitCode: code})
	case "denied":
		return deniedJSON()
	case "error":
		if tc.Output != nil && *tc.Output != "" {
			return errorJSON(*tc.Output)
		}
		return errorJSON("tool result unavailable")
	}
	if p.ToolStatus != nil {
		switch *p.ToolStatus {
		case "denied":
			return deniedJSON()
		case "error":
			return errorJSON("tool result unavailable")
		}
	}
	return ""
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
	if err := a.runTools(ctx, events, m.ID, resp.ToolCalls); err != nil {
		return err
	}
	return a.writeStepFinish(ctx, events, m.ID, resp)
}

// runTools executes non-task tools sequentially, then task-family tools in parallel.
func (a *Agent) runTools(ctx context.Context, events chan<- Event, msgID string, toolCalls []opencode.ToolCall) error {
	var sequential, parallel []opencode.ToolCall
	for _, tc := range toolCalls {
		if isTaskToolName(tc.Name) {
			parallel = append(parallel, tc)
		} else {
			sequential = append(sequential, tc)
		}
	}
	for _, tc := range sequential {
		if err := a.runTool(ctx, events, msgID, tc); err != nil {
			return err
		}
	}
	if len(parallel) == 0 {
		return nil
	}
	if len(parallel) == 1 {
		return a.runTool(ctx, events, msgID, parallel[0])
	}
	errCh := make(chan error, len(parallel))
	var wg sync.WaitGroup
	for _, tc := range parallel {
		wg.Add(1)
		go func(tc opencode.ToolCall) {
			defer wg.Done()
			if err := a.runTool(ctx, events, msgID, tc); err != nil {
				errCh <- err
			}
		}(tc)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *Agent) runTool(ctx context.Context, events chan<- Event, msgID string, tc opencode.ToolCall) error {
	status := "pending"
	part, err := a.store.InsertPart(ctx, db.Part{
		MessageID:  msgID,
		Type:       "tool",
		ToolName:   &tc.Name,
		ToolCallID: &tc.ID,
		ToolStatus: &status,
	})
	if err != nil {
		return fmt.Errorf("agent: insert tool part: %w", err)
	}
	title := toolTitle(tc)
	row := db.ToolCall{
		PartID:    part.ID,
		Tool:      tc.Name,
		CallID:    tc.ID,
		Status:    "pending",
		Title:     &title,
		InputJSON: tc.Arguments,
	}
	if err := a.store.InsertToolCall(ctx, row); err != nil {
		return fmt.Errorf("agent: insert tool call: %w", err)
	}
	a.emit(events, Event{Kind: EventTool, SessionID: a.sessionID(), MessageID: msgID, Part: part, Tool: row})
	_, err = a.executeTool(ctx, events, part.ID, title, tc)
	return err
}

func toolTitle(tc opencode.ToolCall) string {
	var args map[string]json.RawMessage
	if json.Unmarshal([]byte(tc.Arguments), &args) != nil {
		return tc.Name
	}
	first := func(key string) string {
		var s string
		if raw, ok := args[key]; ok && json.Unmarshal(raw, &s) == nil && s != "" {
			return s
		}
		return ""
	}
	switch tc.Name {
	case "bash":
		if c := first("command"); c != "" {
			return truncateRunes(c, maxToolTitle)
		}
	case "read", "write", "edit":
		if p := first("filePath"); p != "" {
			return p
		}
	case "grep":
		if p := first("pattern"); p != "" {
			if path := first("path"); path != "" {
				return truncateRunes(p+"  "+path, maxToolTitle)
			}
			return truncateRunes(p, maxToolTitle)
		}
	case "webfetch":
		if u := first("url"); u != "" {
			return u
		}
	case "question":
		var qs []question.Question
		if raw, ok := args["questions"]; ok && json.Unmarshal(raw, &qs) == nil && len(qs) > 0 && qs[0].Question != "" {
			return truncateRunes(qs[0].Question, maxToolTitle)
		}
	case toolTask, toolTaskList, toolTaskStatus, toolTaskWait, toolTaskCancel:
		if n := first("name"); n != "" {
			return n
		}
		if p := first("prompt"); p != "" {
			return truncateRunes(p, maxToolTitle)
		}
		if id := first("id"); id != "" {
			return id
		}
	}
	return tc.Name
}

func (a *Agent) executeTool(ctx context.Context, events chan<- Event, partID, title string, tc opencode.ToolCall) (string, error) {
	if isTaskToolName(tc.Name) {
		return a.execTaskTool(ctx, events, partID, title, tc)
	}
	if _, known := allBaseToolSpecs[tc.Name]; known && !toolAllowed(a.opts.ToolNames, tc.Name) {
		out := "tool not allowed: " + tc.Name
		return a.updateTool(ctx, events, partID, title, tc, "denied", &out, deniedJSON(), nil, nil)
	}
	switch tc.Name {
	case toolBash:
		return a.execBash(ctx, events, partID, title, tc)
	case toolRead:
		return a.execRead(ctx, events, partID, title, tc)
	case toolGrep:
		return a.execGrep(ctx, events, partID, title, tc)
	case toolWrite:
		return a.execWrite(ctx, events, partID, title, tc)
	case toolEdit:
		return a.execEdit(ctx, events, partID, title, tc)
	case toolWebfetch:
		return a.execWebfetch(ctx, events, partID, title, tc)
	case toolQuestion:
		return a.execQuestion(ctx, events, partID, title, tc)
	default:
		out := "unknown tool: " + tc.Name
		return a.updateTool(ctx, events, partID, title, tc, "denied", &out, deniedJSON(), nil, nil)
	}
}

func (a *Agent) execTaskTool(ctx context.Context, events chan<- Event, partID, title string, tc opencode.ToolCall) (string, error) {
	if a.opts.Host == nil {
		out := "task tools require a subagent host"
		return a.updateTool(ctx, events, partID, title, tc, "denied", &out, deniedJSON(), nil, nil)
	}
	result, meta, status, err := a.opts.Host.Execute(ctx, a.sessionID(), tc.Name, tc.Arguments, partID)
	if err != nil {
		msg := err.Error()
		return a.updateTool(ctx, events, partID, title, tc, "error", &msg, errorJSON(msg), nil, nil)
	}
	if status == "" {
		status = "completed"
	}
	out := result
	var metaPtr *string
	if meta != "" {
		metaPtr = &meta
	}
	return a.updateTool(ctx, events, partID, title, tc, status, &out, result, nil, metaPtr)
}

func (a *Agent) execBash(ctx context.Context, events chan<- Event, partID, title string, tc opencode.ToolCall) (string, error) {
	var args bashArgs
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		msg := "invalid bash arguments: " + err.Error()
		return a.updateTool(ctx, events, partID, title, tc, "error", &msg, errorJSON(msg), nil, nil)
	}
	dec := policy.ClassifyWithAllowlist(args.Command, a.opts.BashAllowlist, a.opts.BashAllowlistEnabled)
	workdir := args.Workdir
	if workdir == "" {
		workdir = a.workdir
	}
	if !withinWorkspace(a.workdir, workdir) {
		msg := "bash: workdir must remain inside the approved workspace"
		return a.updateTool(ctx, events, partID, title, tc, "denied", &msg, errorJSON(msg), nil, nil)
	}
	deny := func() (string, error) {
		out := deniedJSON()
		return a.updateTool(ctx, events, partID, title, tc, "denied", &out, out, nil, nil)
	}
	confirmed := false
	switch dec.Action {
	case policy.ActionDeny:
		return deny()
	case policy.ActionAsk:
		if a.opts.Confirm == nil {
			return deny()
		}
		ok, err := a.opts.Confirm(dec, args.Command)
		if err != nil {
			return "", fmt.Errorf("agent: confirm: %w", err)
		}
		if !ok {
			return deny()
		}
		confirmed = true
	}
	res, err := bash.Run(ctx, args.Command, workdir, dec, confirmed, &bash.Exec{})
	if errors.Is(err, bash.ErrDenied) {
		return deny()
	}
	if err != nil {
		msg := err.Error()
		return a.updateTool(ctx, events, partID, title, tc, "error", &msg, errorJSON(msg), nil, nil)
	}
	out := res.Stdout + res.Stderr
	code := res.ExitCode
	return a.updateTool(ctx, events, partID, title, tc, "completed", &out, completedJSON(res), &code,
		&res.StartTime, &res.EndTime)
}

func (a *Agent) execRead(ctx context.Context, events chan<- Event, partID, title string, tc opencode.ToolCall) (string, error) {
	var args struct {
		FilePath string `json:"filePath"`
	}
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		msg := "invalid read arguments: " + err.Error()
		return a.updateTool(ctx, events, partID, title, tc, "error", &msg, errorJSON(msg), nil, nil)
	}
	res, err := read.Run(args.FilePath, a.workdir)
	if err != nil {
		return a.updateTool(ctx, events, partID, title, tc, "error", errOut(err), errorJSON(err.Error()), nil, nil)
	}
	out := res.Output
	if len([]rune(out)) > maxToolOutput {
		out = truncateRunes(out, maxToolOutput)
		res.Metadata["truncated"] = true
	}
	meta, _ := json.Marshal(res.Metadata)
	metaJSON := string(meta)
	return a.updateTool(ctx, events, partID, title, tc, "completed", &out, toolOutputJSON(out), nil, &metaJSON)
}

func (a *Agent) execGrep(ctx context.Context, events chan<- Event, partID, title string, tc opencode.ToolCall) (string, error) {
	var args struct {
		Pattern         string `json:"pattern"`
		Path            string `json:"path"`
		Glob            string `json:"glob"`
		CaseInsensitive bool   `json:"caseInsensitive"`
		MaxMatches      int    `json:"maxMatches"`
	}
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		msg := "invalid grep arguments: " + err.Error()
		return a.updateTool(ctx, events, partID, title, tc, "error", &msg, errorJSON(msg), nil, nil)
	}
	res, err := grep.Run(ctx, a.workdir, grep.Options{
		Pattern:         args.Pattern,
		Path:            args.Path,
		Glob:            args.Glob,
		CaseInsensitive: args.CaseInsensitive,
		MaxMatches:      args.MaxMatches,
	}, nil)
	if err != nil {
		return a.updateTool(ctx, events, partID, title, tc, "error", errOut(err), errorJSON(err.Error()), nil, nil)
	}
	out := res.Output
	if len([]rune(out)) > maxToolOutput {
		out = truncateRunes(out, maxToolOutput)
		res.Metadata["truncated"] = true
	}
	meta, _ := json.Marshal(res.Metadata)
	metaJSON := string(meta)
	return a.updateTool(ctx, events, partID, title, tc, "completed", &out, toolOutputJSON(out), nil, &metaJSON)
}

func (a *Agent) execWrite(ctx context.Context, events chan<- Event, partID, title string, tc opencode.ToolCall) (string, error) {
	var args struct {
		FilePath string `json:"filePath"`
		Contents string `json:"contents"`
	}
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		msg := "invalid write arguments: " + err.Error()
		return a.updateTool(ctx, events, partID, title, tc, "error", &msg, errorJSON(msg), nil, nil)
	}
	res, err := write.Run(args.FilePath, args.Contents, a.workdir)
	if err != nil {
		return a.updateTool(ctx, events, partID, title, tc, "error", errOut(err), errorJSON(err.Error()), nil, nil)
	}
	meta, _ := json.Marshal(res.Metadata)
	metaJSON := string(meta)
	return a.updateTool(ctx, events, partID, title, tc, "completed", &res.Output, toolOutputJSON(res.Output), nil, &metaJSON)
}

func (a *Agent) execEdit(ctx context.Context, events chan<- Event, partID, title string, tc opencode.ToolCall) (string, error) {
	var args struct {
		FilePath  string `json:"filePath"`
		OldString string `json:"oldString"`
		NewString string `json:"newString"`
	}
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		msg := "invalid edit arguments: " + err.Error()
		return a.updateTool(ctx, events, partID, title, tc, "error", &msg, errorJSON(msg), nil, nil)
	}
	res, err := edit.Run(args.FilePath, args.OldString, args.NewString, a.workdir)
	if err != nil {
		return a.updateTool(ctx, events, partID, title, tc, "error", errOut(err), errorJSON(err.Error()), nil, nil)
	}
	meta, _ := json.Marshal(res.Metadata)
	metaJSON := string(meta)
	return a.updateTool(ctx, events, partID, title, tc, "completed", &res.Output, toolOutputJSON(res.Output), nil, &metaJSON)
}

func (a *Agent) execWebfetch(ctx context.Context, events chan<- Event, partID, title string, tc opencode.ToolCall) (string, error) {
	var args struct {
		URL    string `json:"url"`
		Format string `json:"format"`
	}
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		msg := "invalid webfetch arguments: " + err.Error()
		return a.updateTool(ctx, events, partID, title, tc, "error", &msg, errorJSON(msg), nil, nil)
	}
	res, err := webfetch.Run(ctx, args.URL, args.Format, &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if policy.PrivateOrLoopback(req.URL.Hostname()) {
			return fmt.Errorf("webfetch: redirect to local or private host is not allowed")
		}
		return nil
	}})
	if err != nil {
		return a.updateTool(ctx, events, partID, title, tc, "error", errOut(err), errorJSON(err.Error()), nil, nil)
	}
	out := res.Output
	truncated := false
	if len([]rune(out)) > maxToolOutput {
		out = truncateRunes(out, maxToolOutput)
		truncated = true
	}
	res.Metadata["truncated"] = truncated
	meta, _ := json.Marshal(res.Metadata)
	metaJSON := string(meta)
	return a.updateTool(ctx, events, partID, title, tc, "completed", &out, toolOutputJSON(out), nil, &metaJSON)
}

func (a *Agent) execQuestion(ctx context.Context, events chan<- Event, partID, title string, tc opencode.ToolCall) (string, error) {
	var args struct {
		Questions []question.Question `json:"questions"`
	}
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		msg := "invalid question arguments: " + err.Error()
		return a.updateTool(ctx, events, partID, title, tc, "error", &msg, errorJSON(msg), nil, nil)
	}
	if a.opts.Ask == nil {
		out := "question tool requires UI support"
		return a.updateTool(ctx, events, partID, title, tc, "denied", &out, deniedJSON(), nil, nil)
	}
	ask := func(q question.Question) (int, error) { return a.opts.Ask(q) }
	res, err := question.Run(args.Questions, ask)
	if err != nil {
		return a.updateTool(ctx, events, partID, title, tc, "error", errOut(err), errorJSON(err.Error()), nil, nil)
	}
	meta, _ := json.Marshal(res.Metadata)
	metaJSON := string(meta)
	return a.updateTool(ctx, events, partID, title, tc, "completed", &res.Output, toolOutputJSON(res.Output), nil, &metaJSON)
}

func (a *Agent) updateTool(ctx context.Context, events chan<- Event, partID, title string, tc opencode.ToolCall,
	status string, out *string, result string, exit *int, timesAndMeta ...any,
) (string, error) {
	row := db.ToolCall{
		PartID:    partID,
		Tool:      tc.Name,
		CallID:    tc.ID,
		Status:    status,
		Title:     &title,
		InputJSON: tc.Arguments,
		Output:    out,
		ExitCode:  exit,
	}
	for _, tm := range timesAndMeta {
		switch v := tm.(type) {
		case *string:
			if row.MetadataJSON == nil && v != nil {
				row.MetadataJSON = v
			}
		case *int64:
			if row.TimeStart == nil {
				row.TimeStart = v
			} else if row.TimeEnd == nil {
				row.TimeEnd = v
			}
		}
	}
	if row.TimeStart == nil && row.TimeEnd == nil {
		now := time.Now().UnixMilli()
		row.TimeStart = &now
		row.TimeEnd = &now
	}
	if err := a.store.UpdateToolCall(ctx, row); err != nil {
		return "", fmt.Errorf("agent: update tool call: %w", err)
	}
	a.emit(events, Event{Kind: EventTool, SessionID: a.sessionID(), Tool: row})
	return result, nil
}

func errOut(err error) *string {
	s := err.Error()
	return &s
}

type toolResultJSON struct {
	ExitCode  int    `json:"exit_code"`
	Output    string `json:"output"`
	Truncated bool   `json:"truncated,omitempty"`
}

func completedJSON(res bash.Result) string {
	raw := res.Stdout + res.Stderr
	truncated := false
	r := []rune(raw)
	if len(r) > maxToolOutput {
		raw = string(r[:maxToolOutput])
		truncated = true
	}
	out, _ := json.Marshal(toolResultJSON{ExitCode: res.ExitCode, Output: raw, Truncated: truncated})
	return string(out)
}

func deniedJSON() string {
	out, _ := json.Marshal(struct {
		Denied string `json:"denied"`
	}{Denied: "user denied the command"})
	return string(out)
}

func toolOutputJSON(output string) string {
	out, _ := json.Marshal(struct {
		Output string `json:"output"`
	}{Output: output})
	return string(out)
}

func errorJSON(msg string) string {
	out, _ := json.Marshal(struct {
		Error string `json:"error"`
	}{Error: msg})
	return string(out)
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

func withinWorkspace(root, candidate string) bool {
	r, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	c, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	r, err = filepath.EvalSymlinks(r)
	if err != nil {
		return false
	}
	c, err = filepath.EvalSymlinks(c)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(r, c)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
