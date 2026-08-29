package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/policy"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/tools/question"
	"github.com/chinmay-sawant/lazykoder/internal/tools/webfetch"
)

func agentWebfetchTransport(srv *httptest.Server) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
		},
	}
}

type wireMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content"`
	ToolCallID string            `json:"tool_call_id"`
	ToolCalls  []json.RawMessage `json:"tool_calls"`
}

type fakeProvider struct {
	mu           sync.Mutex
	responses    []string
	requests     [][]wireMessage
	models       []string
	variants     []string
	paths        []string
	hadTools     []bool
	overflowLeft int
	overflowDone int
	srv          *httptest.Server
}

func newFakeProvider(t *testing.T, responses ...string) *fakeProvider {
	t.Helper()
	f := &fakeProvider{responses: responses}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		var req struct {
			Model           string            `json:"model"`
			ReasoningEffort string            `json:"reasoning_effort"`
			Messages        []wireMessage     `json:"messages"`
			Tools           []json.RawMessage `json:"tools"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		f.mu.Lock()
		idx := len(f.requests)
		f.requests = append(f.requests, req.Messages)
		f.models = append(f.models, req.Model)
		f.variants = append(f.variants, req.ReasoningEffort)
		f.paths = append(f.paths, r.URL.Path)
		f.hadTools = append(f.hadTools, len(req.Tools) > 0)
		if f.overflowLeft > 0 {
			f.overflowLeft--
			f.overflowDone++
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"context_length_exceeded"}}`))
			return
		}
		respIdx := idx - f.overflowDone
		resp := f.responses[len(f.responses)-1]
		if respIdx >= 0 && respIdx < len(f.responses) {
			resp = f.responses[respIdx]
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	f.srv = srv
	return f
}

func (f *fakeProvider) requestModels(idx int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if idx < 0 || idx >= len(f.models) {
		return ""
	}
	return f.models[idx]
}

func (f *fakeProvider) requestVariants(idx int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if idx < 0 || idx >= len(f.variants) {
		return ""
	}
	return f.variants[idx]
}

func (f *fakeProvider) requestPath(idx int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if idx < 0 || idx >= len(f.paths) {
		return ""
	}
	return f.paths[idx]
}

func (f *fakeProvider) requestMessages(idx int) []wireMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[idx]
}

func (f *fakeProvider) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func (f *fakeProvider) requestHadTools(idx int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if idx < 0 || idx >= len(f.hadTools) {
		return false
	}
	return f.hadTools[idx]
}

type fakeToolCall struct {
	ID   string
	Name string
	Args string
}

func respBody(content, reasoning, finishReason string, toolCalls []fakeToolCall, usage map[string]any) string {
	msg := map[string]any{"content": content, "finish_reason": finishReason}
	if reasoning != "" {
		msg["reasoning"] = reasoning
	}
	if len(toolCalls) > 0 {
		var wc []map[string]any
		for _, tc := range toolCalls {
			wc = append(wc, map[string]any{
				"id":   tc.ID,
				"type": "function",
				"function": map[string]any{
					"name":      tc.Name,
					"arguments": tc.Args,
				},
			})
		}
		msg["tool_calls"] = wc
	}
	body := map[string]any{"choices": []any{map[string]any{"message": msg}}}
	if usage != nil {
		body["usage"] = usage
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}

var testUsage = map[string]any{
	"total_tokens":       100,
	"prompt_tokens":      40,
	"completion_tokens":  60,
	"reasoning_tokens":   20,
	"cache_read_tokens":  5,
	"cache_write_tokens": 3,
	"cost":               0.0015,
}

func newTestEnv(t *testing.T) (*db.Store, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	st, err := db.Open(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return st, path
}

func newClient(t *testing.T, srv *httptest.Server) *opencode.Client {
	t.Helper()
	return opencode.NewClient("test-key", opencode.WithBaseURL(srv.URL))
}

func openRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	raw, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return raw
}

func queryCount(t *testing.T, path, query string, args ...any) int {
	t.Helper()
	raw := openRaw(t, path)
	var n int
	if err := raw.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return n
}

type rowTC struct {
	PartID, Tool, CallID, Status string
	Title                        *string
	ExitCode                     *int
	Output                       *string
}

func queryToolCalls(t *testing.T, path string) []rowTC {
	t.Helper()
	raw := openRaw(t, path)
	rows, err := raw.Query(`SELECT part_id, tool, call_id, status, title, exit_code, output FROM tool_calls ORDER BY part_id`)
	if err != nil {
		t.Fatalf("query tool_calls: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []rowTC
	for rows.Next() {
		var r rowTC
		if err := rows.Scan(&r.PartID, &r.Tool, &r.CallID, &r.Status, &r.Title, &r.ExitCode, &r.Output); err != nil {
			t.Fatalf("scan tool_calls: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate tool_calls: %v", err)
	}
	return out
}

func sendAndCollect(t *testing.T, a *Agent, userText string) ([]Event, error) {
	t.Helper()
	events := make(chan Event, 256)
	err := a.Send(context.Background(), userText, events)
	var got []Event
	for ev := range events {
		got = append(got, ev)
	}
	return got, err
}

func continueAndCollect(t *testing.T, a *Agent) ([]Event, error) {
	t.Helper()
	events := make(chan Event, 256)
	err := a.Continue(context.Background(), events)
	var got []Event
	for ev := range events {
		got = append(got, ev)
	}
	return got, err
}

func hasEventKind(events []Event, kind EventKind) bool {
	for _, ev := range events {
		if ev.Kind == kind {
			return true
		}
	}
	return false
}

func sessionIDFromEvents(t *testing.T, events []Event) string {
	t.Helper()
	for _, ev := range events {
		if ev.Kind == EventSessionCreated {
			return ev.SessionID
		}
	}
	t.Fatalf("no EventSessionCreated in %d events", len(events))
	return ""
}

func TestSendToolDispatch(t *testing.T) {
	workdir := t.TempDir()
	fixture := filepath.Join(workdir, "fixture.txt")
	if err := os.WriteFile(fixture, []byte("line one\nline two\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	webSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("web body ok"))
	}))
	t.Cleanup(webSrv.Close)

	readArgs, _ := json.Marshal(map[string]any{"filePath": "fixture.txt"})
	writeArgs, _ := json.Marshal(map[string]any{"filePath": "new.txt", "contents": "hello new"})
	editArgs, _ := json.Marshal(map[string]any{"filePath": "fixture.txt", "oldString": "line one", "newString": "line uno"})
	grepArgs, _ := json.Marshal(map[string]any{"pattern": "line two", "glob": "*.txt"})
	webArgs, _ := json.Marshal(map[string]any{"url": "http://203.0.113.11/", "format": "text"})
	qArgs, _ := json.Marshal(map[string]any{"questions": []any{
		map[string]any{"question": "pick one?", "header": "choice", "options": []string{"alpha", "beta"}},
	}})

	fake := newFakeProvider(t,
		respBody("", "", "tool-calls", []fakeToolCall{
			{ID: "c_read", Name: "read", Args: string(readArgs)},
			{ID: "c_write", Name: "write", Args: string(writeArgs)},
			{ID: "c_edit", Name: "edit", Args: string(editArgs)},
			{ID: "c_grep", Name: "grep", Args: string(grepArgs)},
			{ID: "c_web", Name: "webfetch", Args: string(webArgs)},
			{ID: "c_q", Name: "question", Args: string(qArgs)},
		}, testUsage),
		respBody("all done", "", "stop", nil, nil),
	)
	st, path := newTestEnv(t)
	asks := 0
	a := New(st, newClient(t, fake.srv), workdir, Options{
		Confirm:        func(policy.Decision, string) (bool, error) { return true, nil },
		WebfetchClient: &http.Client{Transport: agentWebfetchTransport(webSrv)},
		Ask: func(q question.Question) (int, error) {
			asks++
			return 1, nil
		},
	})

	if _, err := sendAndCollect(t, a, "do the tools"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	tc := queryToolCalls(t, path)
	if len(tc) != 6 {
		t.Fatalf("tool_calls rows = %d, want 6", len(tc))
	}
	byTool := map[string]rowTC{}
	for _, r := range tc {
		byTool[r.Tool] = r
	}
	if r := byTool["read"]; r.Status != "completed" || r.Output == nil || !strings.Contains(*r.Output, "line two") {
		t.Errorf("read row = %+v, want completed with fixture content", r)
	}
	if r := byTool["write"]; r.Status != "completed" {
		t.Errorf("write row = %+v, want completed", r)
	}
	if r := byTool["edit"]; r.Status != "completed" {
		t.Errorf("edit row = %+v, want completed", r)
	}
	if r := byTool["grep"]; r.Status != "completed" || r.Output == nil || !strings.Contains(*r.Output, "line two") {
		t.Errorf("grep row = %+v, want completed with match", r)
	}
	if r := byTool["webfetch"]; r.Status != "completed" || r.Output == nil || !strings.Contains(*r.Output, "web body ok") {
		t.Errorf("webfetch row = %+v, want completed with body", r)
	}
	if r := byTool["question"]; r.Status != "completed" {
		t.Errorf("question row = %+v, want completed", r)
	}

	written, err := os.ReadFile(filepath.Join(workdir, "new.txt"))
	if err != nil || string(written) != "hello new" {
		t.Errorf("write tool did not create new.txt: %q, %v", string(written), err)
	}
	edited, err := os.ReadFile(fixture)
	if err != nil || !strings.Contains(string(edited), "line uno") {
		t.Errorf("edit tool did not update fixture: %q, %v", string(edited), err)
	}
	if asks != 1 {
		t.Errorf("ask calls = %d, want 1", asks)
	}

	raw := openRaw(t, path)
	var meta string
	if err := raw.QueryRow(`SELECT metadata_json FROM tool_calls WHERE tool = 'question'`).Scan(&meta); err != nil {
		t.Fatalf("read question metadata: %v", err)
	}
	if !strings.Contains(meta, `"answers"`) || !strings.Contains(meta, `"beta"`) {
		t.Errorf("question metadata = %q, want answers with beta", meta)
	}
}

func TestSendWebfetchBrowserMode(t *testing.T) {
	args, _ := json.Marshal(map[string]any{
		"url":  "https://example.com/article",
		"mode": "browser",
	})
	fake := newFakeProvider(t,
		respBody("", "", "tool-calls", []fakeToolCall{
			{ID: "c_browser", Name: "webfetch", Args: string(args)},
		}, testUsage),
		respBody("done", "", "stop", nil, nil),
	)
	reader := &testBrowserReader{result: webfetch.Result{
		Output:   "browser page text",
		Metadata: map[string]any{"title": "Example"},
	}}
	st, path := newTestEnv(t)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		WebfetchBrowser: reader,
	})

	if _, err := sendAndCollect(t, a, "read this page"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if reader.url != "https://example.com/article" {
		t.Errorf("browser URL = %q", reader.url)
	}
	rows := queryToolCalls(t, path)
	if len(rows) != 1 || rows[0].Status != "completed" || rows[0].Output == nil || *rows[0].Output != "browser page text" {
		t.Fatalf("browser tool rows = %+v", rows)
	}
}

func TestSendCancellationPersistsCancelledBrowserTool(t *testing.T) {
	args, _ := json.Marshal(map[string]any{
		"url":  "https://example.com/article",
		"mode": "browser",
	})
	fake := newFakeProvider(t, respBody("", "", "tool-calls", []fakeToolCall{
		{ID: "c_browser_cancel", Name: "webfetch", Args: string(args)},
	}, testUsage))
	reader := &blockingBrowserReader{started: make(chan struct{})}
	st, path := newTestEnv(t)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{WebfetchBrowser: reader})
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan Event, 256)
	result := make(chan error, 1)
	go func() { result <- a.Send(ctx, "read this page", events) }()
	select {
	case <-reader.started:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("browser tool did not start")
	}
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Send error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Send did not stop after cancellation")
	}
	for range events {
	}
	rows := queryToolCalls(t, path)
	if len(rows) != 1 || rows[0].Status != "cancelled" || rows[0].Output == nil || *rows[0].Output != "cancelled" {
		t.Fatalf("cancelled browser tool rows = %+v", rows)
	}
	if fake.requestCount() != 1 {
		t.Fatalf("provider requests = %d, want 1", fake.requestCount())
	}
}

func TestSendCancellationPersistsCancelledBashTool(t *testing.T) {
	args := `{"command":"sleep 10"}`
	fake := newFakeProvider(t, respBody("", "", "tool-calls", []fakeToolCall{
		{ID: "c_bash_cancel", Name: "bash", Args: args},
	}, testUsage))
	st, path := newTestEnv(t)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{DisableStreaming: true})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan Event, 256)
	result := make(chan error, 1)
	go func() { result <- a.Send(ctx, "run the command", events) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if rows := queryToolCalls(t, path); len(rows) == 1 {
			cancel()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Send error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Send did not stop after bash cancellation")
	}
	for range events {
	}
	rows := queryToolCalls(t, path)
	if len(rows) != 1 || rows[0].Status != "cancelled" || rows[0].Output == nil || *rows[0].Output != "cancelled" {
		t.Fatalf("cancelled bash tool rows = %+v", rows)
	}
}

type testBrowserReader struct {
	result webfetch.Result
	url    string
}

func (r *testBrowserReader) Read(_ context.Context, url string) (webfetch.Result, error) {
	r.url = url
	return r.result, nil
}

type blockingBrowserReader struct {
	started chan struct{}
}

func (r *blockingBrowserReader) Read(ctx context.Context, _ string) (webfetch.Result, error) {
	close(r.started)
	<-ctx.Done()
	return webfetch.Result{}, ctx.Err()
}

func TestSendQuestionDeclinedAsk(t *testing.T) {
	qArgs, _ := json.Marshal(map[string]any{"questions": []any{
		map[string]any{"question": "pick one?", "options": []string{"alpha"}},
	}})
	fake := newFakeProvider(t,
		respBody("", "", "tool-calls", []fakeToolCall{
			{ID: "c_q", Name: "question", Args: string(qArgs)},
		}, testUsage),
		respBody("ok", "", "stop", nil, nil),
	)
	st, path := newTestEnv(t)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{})

	if _, err := sendAndCollect(t, a, "ask"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	tc := queryToolCalls(t, path)
	if len(tc) != 1 || tc[0].Tool != "question" || tc[0].Status != "denied" {
		t.Fatalf("question rows = %+v, want one denied", tc)
	}
}

func TestSendPhase1Gate(t *testing.T) {
	fake := newFakeProvider(t, respBody("hello", "", "stop", nil, nil))
	st, path := newTestEnv(t)
	workdir := t.TempDir()
	a := New(st, newClient(t, fake.srv), workdir, Options{})

	events, err := sendAndCollect(t, a, "hello world")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	sid := sessionIDFromEvents(t, events)

	raw := openRaw(t, path)
	var directory, title string
	if err := raw.QueryRow(`SELECT directory, title FROM sessions WHERE id = ?`, sid).Scan(&directory, &title); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if directory != workdir {
		t.Errorf("session directory = %q, want %q", directory, workdir)
	}
	if title != "hello world" {
		t.Errorf("session title = %q, want %q", title, "hello world")
	}
	if n := queryCount(t, path, `SELECT count(*) FROM sessions`); n != 1 {
		t.Errorf("sessions = %d, want 1", n)
	}
	if n := queryCount(t, path, `SELECT count(*) FROM messages`); n != 2 {
		t.Errorf("messages = %d, want 2", n)
	}
	if n := queryCount(t, path, `SELECT count(*) FROM parts WHERE type = 'text'`); n != 2 {
		t.Errorf("text parts = %d, want 2", n)
	}

	msgs, err := st.ListMessages(context.Background(), sid)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Role != "user" || msgs[1].Role != "assistant" {
		t.Errorf("roles = %v, want [user assistant]", []string{msgs[0].Role, msgs[1].Role})
	}
	userParts, _ := st.ListParts(context.Background(), msgs[0].ID)
	if len(userParts) != 1 || userParts[0].Text == nil || *userParts[0].Text != "hello world" {
		t.Errorf("user parts = %v, want one text part with %q", userParts, "hello world")
	}
	assistantParts, _ := st.ListParts(context.Background(), msgs[1].ID)
	if len(assistantParts) != 2 || assistantParts[1].Type != "text" || assistantParts[1].Text == nil || *assistantParts[1].Text != "hello" {
		t.Errorf("assistant parts = %v, want step-start + text hello", assistantParts)
	}

	if n := fake.requestCount(); n != 1 {
		t.Errorf("provider calls = %d, want 1", n)
	}
	first := fake.requestMessages(0)
	if len(first) != 1 || first[0].Role != "user" || first[0].Content != "hello world" {
		t.Errorf("first request = %+v, want one user message", first)
	}

	if !hasEventKind(events, EventSessionCreated) || !hasEventKind(events, EventMessage) ||
		!hasEventKind(events, EventPart) || !hasEventKind(events, EventDone) {
		t.Errorf("events missing expected kinds: %+v", events)
	}
	if hasEventKind(events, EventError) {
		t.Errorf("unexpected EventError in events")
	}
}

func TestSendInjectsProjectInstructions(t *testing.T) {
	fake := newFakeProvider(t, respBody("ok", "", "stop", nil, testUsage))
	st, _ := newTestEnv(t)
	workdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workdir, "AGENTS.md"), []byte("never invent APIs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := New(st, newClient(t, fake.srv), workdir, Options{DisableStreaming: true})
	if _, err := sendAndCollect(t, a, "hi"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if fake.requestCount() < 1 {
		t.Fatal("no provider request")
	}
	msgs := fake.requestMessages(0)
	if len(msgs) < 2 {
		t.Fatalf("messages = %d, want at least system+user", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Fatalf("first role = %q, want system", msgs[0].Role)
	}
	if !strings.Contains(msgs[0].Content, "never invent APIs") {
		t.Fatalf("system content missing AGENTS.md: %q", msgs[0].Content)
	}
	if !strings.Contains(msgs[0].Content, projectInstructionsHeader) {
		t.Fatalf("system content missing header: %q", msgs[0].Content)
	}
	foundUser := false
	for _, m := range msgs[1:] {
		if m.Role == "user" && strings.Contains(m.Content, "hi") {
			foundUser = true
			break
		}
	}
	if !foundUser {
		t.Fatalf("user turn missing after system: %+v", msgs)
	}
	// System primer is wire-only; SQLite should still be user+assistant only.
	sid := a.sessionID()
	dbMsgs, err := st.ListMessages(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range dbMsgs {
		if m.Role == "system" {
			t.Fatal("system message must not be persisted")
		}
	}
}

func TestSendWithoutProjectInstructions(t *testing.T) {
	fake := newFakeProvider(t, respBody("ok", "", "stop", nil, testUsage))
	st, _ := newTestEnv(t)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{DisableStreaming: true})
	if _, err := sendAndCollect(t, a, "hi"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	msgs := fake.requestMessages(0)
	if len(msgs) == 0 {
		t.Fatal("no messages")
	}
	if msgs[0].Role == "system" {
		t.Fatal("unexpected system message without AGENTS.md")
	}
}

func TestSendUsageAndReasoning(t *testing.T) {
	fake := newFakeProvider(t, respBody("visible text", "think step by step", "stop", nil, testUsage))
	st, path := newTestEnv(t)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{})

	events, err := sendAndCollect(t, a, "hi")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	sid := sessionIDFromEvents(t, events)
	msgs, err := st.ListMessages(context.Background(), sid)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2", len(msgs))
	}
	parts, err := st.ListParts(context.Background(), msgs[1].ID)
	if err != nil {
		t.Fatalf("list parts: %v", err)
	}
	wantTypes := []string{"step-start", "reasoning", "text", "step-finish"}
	if len(parts) != len(wantTypes) {
		t.Fatalf("assistant parts = %d, want %d", len(parts), len(wantTypes))
	}
	for i, want := range wantTypes {
		if parts[i].Type != want {
			t.Errorf("part %d type = %q, want %q", i, parts[i].Type, want)
		}
	}
	if parts[1].Text == nil || *parts[1].Text != "think step by step" {
		t.Errorf("reasoning text = %v, want %q", parts[1].Text, "think step by step")
	}
	if parts[2].Text == nil || *parts[2].Text != "visible text" {
		t.Errorf("text = %v, want %q", parts[2].Text, "visible text")
	}
	sf := parts[3]
	if sf.TimeStart == nil || sf.TimeEnd == nil || *sf.TimeEnd < *sf.TimeStart {
		t.Fatalf("step-finish timestamps = start %v end %v", sf.TimeStart, sf.TimeEnd)
	}
	if sf.FinishReason == nil || *sf.FinishReason != "stop" {
		t.Errorf("finish_reason = %v, want stop", sf.FinishReason)
	}
	if sf.TokensTotal == nil || *sf.TokensTotal != 100 {
		t.Errorf("tokens_total = %v, want 100", sf.TokensTotal)
	}
	if sf.TokensInput == nil || *sf.TokensInput != 40 {
		t.Errorf("tokens_input = %v, want 40", sf.TokensInput)
	}
	if sf.TokensOutput == nil || *sf.TokensOutput != 60 {
		t.Errorf("tokens_output = %v, want 60", sf.TokensOutput)
	}
	if sf.TokensReasoning == nil || *sf.TokensReasoning != 20 {
		t.Errorf("tokens_reasoning = %v, want 20", sf.TokensReasoning)
	}
	if sf.TokensCacheRead == nil || *sf.TokensCacheRead != 5 {
		t.Errorf("tokens_cache_read = %v, want 5", sf.TokensCacheRead)
	}
	if sf.TokensCacheWrite == nil || *sf.TokensCacheWrite != 3 {
		t.Errorf("tokens_cache_write = %v, want 3", sf.TokensCacheWrite)
	}
	if sf.Cost == nil || *sf.Cost != 0.0015 {
		t.Errorf("cost = %v, want 0.0015", sf.Cost)
	}
	if n := queryCount(t, path, `SELECT count(*) FROM parts WHERE type = 'step-start'`); n != 1 {
		t.Errorf("step-start parts = %d, want 1", n)
	}
	if n := queryCount(t, path, `SELECT count(*) FROM parts WHERE type = 'step-finish'`); n != 1 {
		t.Errorf("step-finish parts = %d, want 1", n)
	}
}

func TestSendDeniedBash(t *testing.T) {
	tc := fakeToolCall{ID: "call_1", Name: "bash", Args: `{"command":"rm -rf /tmp/lazy-x"}`}
	fake := newFakeProvider(t,
		respBody("", "", "tool-calls", []fakeToolCall{tc}, testUsage),
		respBody("ok, skipped", "", "stop", nil, testUsage),
	)
	st, path := newTestEnv(t)
	var confirmed []string
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		Confirm: func(dec policy.Decision, subject string) (bool, error) {
			confirmed = append(confirmed, subject)
			return false, nil
		},
	})

	events, err := sendAndCollect(t, a, "delete it")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	rows := queryToolCalls(t, path)
	if len(rows) != 1 {
		t.Fatalf("tool_calls rows = %d, want 1", len(rows))
	}
	if rows[0].Status != "denied" {
		t.Errorf("status = %q, want denied", rows[0].Status)
	}
	if rows[0].Tool != "bash" || rows[0].CallID != "call_1" {
		t.Errorf("tool = %q call = %q, want bash call_1", rows[0].Tool, rows[0].CallID)
	}
	if rows[0].ExitCode != nil {
		t.Errorf("exit_code = %v, want nil for denied", *rows[0].ExitCode)
	}
	if rows[0].Output == nil || !strings.Contains(*rows[0].Output, "user denied the command") {
		t.Errorf("output = %v, want denial json", rows[0].Output)
	}
	if len(confirmed) != 1 || confirmed[0] != "rm -rf /tmp/lazy-x" {
		t.Errorf("confirm calls = %v, want [rm -rf /tmp/lazy-x]", confirmed)
	}

	if n := fake.requestCount(); n != 2 {
		t.Fatalf("provider calls = %d, want 2", n)
	}
	second := fake.requestMessages(1)
	var found bool
	for _, m := range second {
		if m.Role == "tool" && strings.Contains(m.Content, `"user denied the command"`) {
			found = true
		}
	}
	if !found {
		t.Errorf("second request has no tool message with denial content: %+v", second)
	}

	sid := sessionIDFromEvents(t, events)
	msgs, _ := st.ListMessages(context.Background(), sid)
	var partTypes []string
	for _, m := range msgs {
		parts, _ := st.ListParts(context.Background(), m.ID)
		for _, p := range parts {
			partTypes = append(partTypes, p.Type)
		}
	}
	for _, want := range []string{"step-start", "tool", "step-finish"} {
		if !contains(partTypes, want) {
			t.Errorf("part types %v missing %q", partTypes, want)
		}
	}
}

func TestSendTodowriteReplacesAndPersistsRows(t *testing.T) {
	input := `{"todos":[{"content":"first","status":"in_progress"},{"content":"second","status":"unknown"}]}`
	tc := fakeToolCall{ID: "todo_1", Name: "todowrite", Args: input}
	fake := newFakeProvider(t,
		respBody("", "", "tool-calls", []fakeToolCall{tc}, testUsage),
		respBody("done", "", "stop", nil, testUsage),
	)
	st, _ := newTestEnv(t)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{DisableStreaming: true})
	events, err := sendAndCollect(t, a, "track this")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	sid := sessionIDFromEvents(t, events)
	items, err := st.ListTodos(context.Background(), sid)
	if err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if len(items) != 2 || items[0].Status != db.TodoInProgress || items[1].Status != db.TodoPending {
		t.Fatalf("todos = %+v", items)
	}
	calls, err := st.ListToolCalls(context.Background(), sid)
	if err != nil {
		t.Fatalf("ListToolCalls: %v", err)
	}
	found := false
	for _, call := range calls {
		if call.Tool == "todowrite" {
			found = true
			if call.Status != "completed" || call.InputJSON != input || call.Output == nil || *call.Output != "todos updated: 2" {
				t.Fatalf("todowrite call = %+v", call)
			}
		}
	}
	if !found {
		t.Fatalf("todowrite call missing: %+v", calls)
	}
}

func TestSendAllowedBash(t *testing.T) {
	tc := fakeToolCall{ID: "call_2", Name: "bash", Args: `{"command":"echo hi"}`}
	fake := newFakeProvider(t,
		respBody("", "", "tool-calls", []fakeToolCall{tc}, testUsage),
		respBody("done", "", "stop", nil, testUsage),
	)
	st, path := newTestEnv(t)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{})

	if _, err := sendAndCollect(t, a, "say hi"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	rows := queryToolCalls(t, path)
	if len(rows) != 1 {
		t.Fatalf("tool_calls rows = %d, want 1", len(rows))
	}
	if rows[0].Status != "completed" {
		t.Errorf("status = %q, want completed", rows[0].Status)
	}
	if rows[0].ExitCode == nil || *rows[0].ExitCode != 0 {
		t.Errorf("exit_code = %v, want 0", rows[0].ExitCode)
	}
	if rows[0].Output == nil || !strings.Contains(*rows[0].Output, "hi") {
		t.Errorf("output = %v, want combined output with hi", rows[0].Output)
	}
	if rows[0].Title == nil || *rows[0].Title != "echo hi" {
		t.Errorf("title = %v, want echo hi", rows[0].Title)
	}

	second := fake.requestMessages(1)
	var found bool
	for _, m := range second {
		if m.Role == "tool" && strings.Contains(m.Content, `"exit_code":0`) {
			found = true
		}
	}
	if !found {
		t.Errorf("second request has no tool message with exit_code 0: %+v", second)
	}
}

func TestSendUnknownTool(t *testing.T) {
	tc := fakeToolCall{ID: "call_3", Name: "frobnicate", Args: `{}`}
	fake := newFakeProvider(t,
		respBody("", "", "tool-calls", []fakeToolCall{tc}, testUsage),
		respBody("continuing", "", "stop", nil, testUsage),
	)
	st, path := newTestEnv(t)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{})

	if _, err := sendAndCollect(t, a, "frobnicate the data"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	rows := queryToolCalls(t, path)
	if len(rows) != 1 {
		t.Fatalf("tool_calls rows = %d, want 1", len(rows))
	}
	if rows[0].Tool != "frobnicate" || rows[0].Status != "denied" {
		t.Errorf("tool = %q status = %q, want frobnicate denied", rows[0].Tool, rows[0].Status)
	}
	if rows[0].Output == nil || !strings.Contains(*rows[0].Output, "unknown tool: frobnicate") {
		t.Errorf("output = %v, want unknown tool: frobnicate", rows[0].Output)
	}
	if n := fake.requestCount(); n != 2 {
		t.Errorf("provider calls = %d, want 2 (loop continued)", n)
	}
}

func TestSendMaxSteps(t *testing.T) {
	tc := fakeToolCall{ID: "call_9", Name: "bash", Args: `{"command":"echo loop"}`}
	fake := newFakeProvider(t, respBody("", "", "tool-calls", []fakeToolCall{tc}, testUsage))
	st, path := newTestEnv(t)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{MaxSteps: 3})

	events, err := sendAndCollect(t, a, "loop")
	if err == nil {
		t.Fatalf("Send succeeded, want step limit error")
	}
	if !strings.Contains(err.Error(), "agent: step limit reached (max 3)") {
		t.Errorf("error = %q, want step limit reached (max 3)", err)
	}
	if !errors.Is(err, ErrStepLimit) {
		t.Errorf("errors.Is(%v, ErrStepLimit) = false", err)
	}
	if !hasEventKind(events, EventError) {
		t.Errorf("events missing EventError: %+v", events)
	}
	if n := fake.requestCount(); n != 3 {
		t.Errorf("provider calls = %d, want 3", n)
	}
	if n := queryCount(t, path, `SELECT count(*) FROM tool_calls`); n != 3 {
		t.Errorf("tool_calls rows = %d, want 3", n)
	}
}

func TestContinueAfterStepLimit(t *testing.T) {
	tc := fakeToolCall{ID: "call_c", Name: "bash", Args: `{"command":"echo loop"}`}
	// Three tool-call steps then a final text reply for Continue.
	fake := newFakeProvider(t,
		respBody("", "", "tool-calls", []fakeToolCall{tc}, testUsage),
		respBody("", "", "tool-calls", []fakeToolCall{tc}, testUsage),
		respBody("", "", "tool-calls", []fakeToolCall{tc}, testUsage),
		respBody("done", "stop", "", nil, testUsage),
	)
	st, path := newTestEnv(t)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{MaxSteps: 3})

	if _, err := sendAndCollect(t, a, "loop"); err == nil {
		t.Fatal("Send succeeded, want step limit error")
	}
	beforeUsers := queryCount(t, path, `SELECT count(*) FROM messages WHERE role='user'`)
	events, err := continueAndCollect(t, a)
	if err != nil {
		t.Fatalf("Continue: %v", err)
	}
	if !hasEventKind(events, EventDone) {
		t.Errorf("events missing EventDone: %+v", events)
	}
	afterUsers := queryCount(t, path, `SELECT count(*) FROM messages WHERE role='user'`)
	if afterUsers != beforeUsers {
		t.Errorf("Continue wrote a user message: users %d -> %d", beforeUsers, afterUsers)
	}
	if n := fake.requestCount(); n != 4 {
		t.Errorf("provider calls = %d, want 4 (3 limit + 1 continue)", n)
	}
}

func TestSendResume(t *testing.T) {
	st, path := newTestEnv(t)
	sess, err := st.CreateSession(context.Background(), db.Session{Title: "existing", Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	fake := newFakeProvider(t,
		respBody("hello", "", "stop", nil, nil),
		respBody("world", "", "stop", nil, nil),
	)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{Session: &sess})

	events1, err := sendAndCollect(t, a, "first turn")
	if err != nil {
		t.Fatalf("first Send: %v", err)
	}
	events2, err := sendAndCollect(t, a, "second turn")
	if err != nil {
		t.Fatalf("second Send: %v", err)
	}
	if hasEventKind(events1, EventSessionCreated) || hasEventKind(events2, EventSessionCreated) {
		t.Errorf("resume emitted EventSessionCreated")
	}
	if n := queryCount(t, path, `SELECT count(*) FROM sessions`); n != 1 {
		t.Errorf("sessions = %d, want 1", n)
	}
	if n := queryCount(t, path, `SELECT count(*) FROM messages`); n != 4 {
		t.Errorf("messages = %d, want 4", n)
	}

	if n := fake.requestCount(); n != 2 {
		t.Fatalf("provider calls = %d, want 2", n)
	}
	second := fake.requestMessages(1)
	if len(second) != 3 {
		t.Fatalf("second request messages = %d, want 3 (history includes first turn)", len(second))
	}
	if second[0].Role != "user" || second[0].Content != "first turn" {
		t.Errorf("msg 0 = %+v, want user first turn", second[0])
	}
	if second[1].Role != "assistant" || second[1].Content != "hello" {
		t.Errorf("msg 1 = %+v, want assistant hello", second[1])
	}
	if second[2].Role != "user" || second[2].Content != "second turn" {
		t.Errorf("msg 2 = %+v, want user second turn", second[2])
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestSendFixturePartTypes(t *testing.T) {
	fake := newFakeProvider(t,
		respBody("visible text", "think step by step", "tool-calls", []fakeToolCall{
			{ID: "call_1", Name: "bash", Args: `{"command":"echo hi"}`},
			{ID: "call_2", Name: "frobnicate", Args: `{}`},
		}, testUsage),
		respBody("done", "", "stop", nil, nil),
	)
	st, path := newTestEnv(t)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{Confirm: func(policy.Decision, string) (bool, error) {
		return true, nil
	}})

	if _, err := sendAndCollect(t, a, "fixture turn"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	expected := map[string]int{
		"step-start":  2,
		"reasoning":   1,
		"text":        3,
		"step-finish": 1,
		"tool":        2,
	}
	raw := openRaw(t, path)
	rows, err := raw.Query(`SELECT type, count(*) FROM parts GROUP BY type`)
	if err != nil {
		t.Fatalf("group parts: %v", err)
	}
	defer func() { _ = rows.Close() }()
	got := map[string]int{}
	for rows.Next() {
		var typ string
		var n int
		if err := rows.Scan(&typ, &n); err != nil {
			t.Fatalf("scan part group: %v", err)
		}
		got[typ] = n
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate part groups: %v", err)
	}
	for typ, want := range expected {
		if got[typ] != want {
			t.Errorf("parts type %q = %d, want %d (got %v)", typ, got[typ], want, got)
		}
	}
	for typ := range got {
		if _, ok := expected[typ]; !ok {
			t.Errorf("unexpected part type %q", typ)
		}
	}

	tc := queryToolCalls(t, path)
	if len(tc) != 2 {
		t.Fatalf("tool_calls rows = %d, want 2", len(tc))
	}
	byTool := map[string]rowTC{}
	for _, r := range tc {
		byTool[r.Tool] = r
	}
	if r, ok := byTool["bash"]; !ok || r.Status != "completed" || r.ExitCode == nil || *r.ExitCode != 0 {
		t.Errorf("bash row = %+v, want completed exit 0", byTool["bash"])
	}
	if r, ok := byTool["frobnicate"]; !ok || r.Status != "denied" {
		t.Errorf("unknown tool row = %+v, want denied", byTool["frobnicate"])
	}

	if n := queryCount(t, path, `SELECT count(*) FROM messages`); n != 3 {
		t.Errorf("messages = %d, want 3", n)
	}
}

func TestSendModelOption(t *testing.T) {
	fake := newFakeProvider(t, respBody("hello", "", "stop", nil, nil))
	st, path := newTestEnv(t)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{Model: "picked-model"})

	events, err := sendAndCollect(t, a, "hi")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	sid := sessionIDFromEvents(t, events)

	if got := fake.requestModels(0); got != "picked-model" {
		t.Errorf("wire model = %q, want picked-model", got)
	}
	raw := openRaw(t, path)
	var model string
	if err := raw.QueryRow(`SELECT model FROM sessions WHERE id = ?`, sid).Scan(&model); err != nil {
		t.Fatalf("read session model: %v", err)
	}
	if model != "picked-model" {
		t.Errorf("session model = %q, want picked-model", model)
	}
}

func TestSendEndpointOption(t *testing.T) {
	fake := newFakeProvider(t, respBody("hello", "", "stop", nil, nil))
	st, _ := newTestEnv(t)
	endpoint := fake.srv.URL + "/zen/v1/chat/completions"
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{
		Model:    "deepseek-v4-flash-free",
		Endpoint: endpoint,
	})

	if _, err := sendAndCollect(t, a, "hi"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := fake.requestModels(0); got != "deepseek-v4-flash-free" {
		t.Errorf("wire model = %q", got)
	}
	if got := fake.requestPath(0); got != "/zen/v1/chat/completions" {
		t.Errorf("path = %q, want /zen/v1/chat/completions", got)
	}
}

func TestSendVariantOption(t *testing.T) {
	fake := newFakeProvider(t, respBody("hello", "", "stop", nil, nil))
	st, path := newTestEnv(t)
	a := New(st, newClient(t, fake.srv), t.TempDir(), Options{Model: "grok-4.5", Variant: "high"})

	events, err := sendAndCollect(t, a, "hi")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	sid := sessionIDFromEvents(t, events)

	if got := fake.requestVariants(0); got != "high" {
		t.Errorf("wire reasoning_effort = %q, want high", got)
	}
	var variant sql.NullString
	if err := openRaw(t, path).QueryRow(`SELECT variant FROM sessions WHERE id = ?`, sid).Scan(&variant); err != nil {
		t.Fatalf("read session variant: %v", err)
	}
	if !variant.Valid || variant.String != "high" {
		t.Errorf("session variant = %v, want high", variant)
	}
}
