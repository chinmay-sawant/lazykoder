package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/policy"
	"github.com/chinmay-sawant/lazykoder/internal/tools/bash"
	"github.com/chinmay-sawant/lazykoder/internal/tools/edit"
	"github.com/chinmay-sawant/lazykoder/internal/tools/grep"
	"github.com/chinmay-sawant/lazykoder/internal/tools/question"
	"github.com/chinmay-sawant/lazykoder/internal/tools/read"
	"github.com/chinmay-sawant/lazykoder/internal/tools/todo"
	"github.com/chinmay-sawant/lazykoder/internal/tools/webfetch"
	"github.com/chinmay-sawant/lazykoder/internal/tools/write"
)

type bashArgs struct {
	Command string `json:"command"`
	Workdir string `json:"workdir"`
}

// baseToolRunner executes one allowed base tool call (method-expression shape).
type baseToolRunner func(a *Agent, ctx context.Context, events chan<- Event, partID, title string, tc ChatToolCall) (string, error)

// baseToolRunners maps advertised base tool names to their executors.
// Task-family tools are not registered here; they go through SubagentHost.
var baseToolRunners = map[string]baseToolRunner{
	toolBash:      (*Agent).execBash,
	toolRead:      (*Agent).execRead,
	toolGrep:      (*Agent).execGrep,
	toolWrite:     (*Agent).execWrite,
	toolEdit:      (*Agent).execEdit,
	toolWebfetch:  (*Agent).execWebfetch,
	toolQuestion:  (*Agent).execQuestion,
	toolTodowrite: (*Agent).execTodowrite,
}

func splitToolCallArguments(tc ChatToolCall) ([]ChatToolCall, error) {
	dec := json.NewDecoder(strings.NewReader(tc.Arguments))
	var out []ChatToolCall
	for {
		var raw json.RawMessage
		err := dec.Decode(&raw)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(raw) == 0 || string(raw) == "null" {
			return nil, fmt.Errorf("empty tool arguments")
		}
		call := tc
		call.Arguments = string(raw)
		if len(out) > 0 && call.ID != "" {
			call.ID = fmt.Sprintf("%s_%d", tc.ID, len(out)+1)
		}
		out = append(out, call)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty tool arguments")
	}
	return out, nil
}

// runTools executes non-task tools sequentially, then task-family tools in parallel.
func (a *Agent) runTools(ctx context.Context, events chan<- Event, msgID string, toolCalls []ChatToolCall) error {
	var expanded []ChatToolCall
	for _, tc := range toolCalls {
		calls, err := splitToolCallArguments(tc)
		if err != nil {
			// Keep the original call so the normal tool handler records a useful
			// structured error instead of losing the provider response.
			expanded = append(expanded, tc)
			continue
		}
		expanded = append(expanded, calls...)
	}
	toolCalls = expanded
	var sequential, parallel []ChatToolCall
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
		go func(tc ChatToolCall) {
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

func (a *Agent) runTool(ctx context.Context, events chan<- Event, msgID string, tc ChatToolCall) (err error) {
	// Tool calls originate from model output and may execute in parallel. Never
	// allow a malformed payload or an unexpected tool panic to bring down the
	// entire application (a panic in one of the worker goroutines would crash
	// the process). The individual handlers normally return structured errors;
	// this is the last-resort safety net.
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("agent: tool %q failed unexpectedly: %v", tc.Name, recovered)
		}
	}()

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
	a.emit(events, Event{Kind: EventTool, SessionID: a.sessionID(), MessageID: msgID, Part: partDeltaFromDB(part), Tool: toolDeltaFromDB(row)})
	_, err = a.executeTool(ctx, events, part.ID, title, tc)
	return err
}

func toolTitle(tc ChatToolCall) string {
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
	case toolBash:
		if c := first("command"); c != "" {
			return truncateRunes(c, maxToolTitle)
		}
	case toolTodowrite:
		var wrap struct {
			Todos []struct {
				Content string `json:"content"`
				Status  string `json:"status"`
			} `json:"todos"`
		}
		if json.Unmarshal([]byte(tc.Arguments), &wrap) == nil {
			return fmt.Sprintf("todos (%d)", len(wrap.Todos))
		}
		return "todos"
	case toolRead, toolWrite, toolEdit:
		if p := first("filePath"); p != "" {
			return p
		}
	case toolGrep:
		if p := first("pattern"); p != "" {
			if path := first("path"); path != "" {
				return truncateRunes(p+"  "+path, maxToolTitle)
			}
			return truncateRunes(p, maxToolTitle)
		}
	case toolWebfetch:
		if u := first("url"); u != "" {
			return u
		}
	case toolQuestion:
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

func (a *Agent) executeTool(ctx context.Context, events chan<- Event, partID, title string, tc ChatToolCall) (string, error) {
	if isTaskToolName(tc.Name) {
		return a.execTaskTool(ctx, events, partID, title, tc)
	}
	if _, known := allBaseToolSpecs[tc.Name]; known && !toolAllowed(a.opts.ToolNames, tc.Name) {
		out := "tool not allowed: " + tc.Name
		return a.updateTool(ctx, events, partID, title, tc, "denied", &out, deniedJSON(), nil, nil)
	}
	if run, ok := baseToolRunners[tc.Name]; ok {
		return run(a, ctx, events, partID, title, tc)
	}
	out := "unknown tool: " + tc.Name
	return a.updateTool(ctx, events, partID, title, tc, "denied", &out, deniedJSON(), nil, nil)
}

func (a *Agent) execTaskTool(ctx context.Context, events chan<- Event, partID, title string, tc ChatToolCall) (string, error) {
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

func (a *Agent) execBash(ctx context.Context, events chan<- Event, partID, title string, tc ChatToolCall) (string, error) {
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

func (a *Agent) execRead(ctx context.Context, events chan<- Event, partID, title string, tc ChatToolCall) (string, error) {
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

func (a *Agent) execGrep(ctx context.Context, events chan<- Event, partID, title string, tc ChatToolCall) (string, error) {
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

func (a *Agent) execWrite(ctx context.Context, events chan<- Event, partID, title string, tc ChatToolCall) (string, error) {
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

func (a *Agent) execEdit(ctx context.Context, events chan<- Event, partID, title string, tc ChatToolCall) (string, error) {
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

func (a *Agent) execWebfetch(ctx context.Context, events chan<- Event, partID, title string, tc ChatToolCall) (string, error) {
	var args struct {
		URL    string `json:"url"`
		Format string `json:"format"`
		Mode   string `json:"mode"`
	}
	if err := json.Unmarshal([]byte(tc.Arguments), &args); err != nil {
		msg := "invalid webfetch arguments: " + err.Error()
		return a.updateTool(ctx, events, partID, title, tc, "error", &msg, errorJSON(msg), nil, nil)
	}
	client := a.opts.WebfetchClient
	res, err := webfetch.RunWithOptions(ctx, webfetch.Options{
		URL:     args.URL,
		Format:  args.Format,
		Mode:    webfetch.Mode(args.Mode),
		Client:  client,
		Browser: a.opts.WebfetchBrowser,
	})
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

func (a *Agent) execQuestion(ctx context.Context, events chan<- Event, partID, title string, tc ChatToolCall) (string, error) {
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

func (a *Agent) execTodowrite(ctx context.Context, events chan<- Event, partID, title string, tc ChatToolCall) (string, error) {
	res, err := todo.Run(tc.Arguments)
	if err != nil {
		msg := err.Error()
		return a.updateTool(ctx, events, partID, title, tc, "error", &msg, errorJSON(msg), nil, nil)
	}
	sid := a.sessionID()
	if sid == "" {
		msg := "todowrite requires an active session"
		return a.updateTool(ctx, events, partID, title, tc, "error", &msg, errorJSON(msg), nil, nil)
	}
	items := make([]db.Todo, 0, len(res.Items))
	for i, it := range res.Items {
		items = append(items, db.Todo{
			SessionID: sid,
			Seq:       i,
			Content:   it.Content,
			Status:    it.Status,
		})
	}
	if err := a.store.ReplaceTodos(ctx, sid, items); err != nil {
		msg := err.Error()
		return a.updateTool(ctx, events, partID, title, tc, "error", &msg, errorJSON(msg), nil, nil)
	}
	out := res.Output
	return a.updateTool(ctx, events, partID, title, tc, "completed", &out, toolOutputJSON(out), nil, nil)
}

func (a *Agent) updateTool(ctx context.Context, events chan<- Event, partID, title string, tc ChatToolCall,
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
	a.emit(events, Event{Kind: EventTool, SessionID: a.sessionID(), Tool: toolDeltaFromDB(row)})
	return result, nil
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
