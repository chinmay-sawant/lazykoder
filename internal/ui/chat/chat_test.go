package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
)

func viewText(m Model) string {
	return m.View().Content
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			for i += 2; i < len(s); i++ {
				if s[i] >= 0x40 && s[i] <= 0x7e {
					i++
					break
				}
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func newTestStore(t *testing.T) *db.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := db.Open(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return st
}

func deadClient() *opencode.Client {
	return opencode.NewClient("test-key", opencode.WithBaseURL("http://127.0.0.1:1"))
}

type fakeToolCall struct {
	ID   string
	Name string
	Args string
}

func respBody(content, finishReason string, toolCalls []fakeToolCall) string {
	msg := map[string]any{"content": content, "finish_reason": finishReason}
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
	raw, _ := json.Marshal(body)
	return string(raw)
}

type fakeProvider struct {
	mu        sync.Mutex
	responses []string
	requests  int
	models    []string
	modelList []string
	srv       *httptest.Server
}

func newFakeProvider(t *testing.T, status int, responses ...string) *fakeProvider {
	t.Helper()
	f := &fakeProvider{responses: responses}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			raw, _ := json.Marshal(map[string]any{"data": []any{
				map[string]any{"id": "deepseek-v4-flash"},
				map[string]any{"id": "claude-4"},
			}})
			_, _ = w.Write(raw)
			return
		}
		f.mu.Lock()
		idx := f.requests
		f.requests++
		var req struct {
			Model string `json:"model"`
		}
		if raw, err := io.ReadAll(r.Body); err == nil {
			_ = json.Unmarshal(raw, &req)
		}
		f.modelList = append(f.modelList, req.Model)
		resp := responses[len(responses)-1]
		if idx < len(responses) {
			resp = responses[idx]
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	f.srv = srv
	return f
}

func (f *fakeProvider) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func (f *fakeProvider) requestModel(idx int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if idx < 0 || idx >= len(f.modelList) {
		return ""
	}
	return f.modelList[idx]
}

func newClient(srv *httptest.Server) *opencode.Client {
	return opencode.NewClient("test-key", opencode.WithBaseURL(srv.URL))
}

type pump struct {
	t  *testing.T
	ch chan tea.Msg
}

func newPump(t *testing.T) *pump {
	return &pump{t: t, ch: make(chan tea.Msg, 512)}
}

func (p *pump) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				p.run(c)
			}
			return
		}
		p.ch <- msg
	}()
}

func (p *pump) next() tea.Msg {
	select {
	case m := <-p.ch:
		return m
	case <-time.After(15 * time.Second):
		p.t.Fatal("timed out waiting for message")
		return nil
	}
}

func (p *pump) runStep(m Model, msg tea.Msg) Model {
	mm, cmd := m.Update(msg)
	p.run(cmd)
	return mm.(Model)
}

func (p *pump) drainUntil(m Model, want string) Model {
	for i := 0; i < 300; i++ {
		if strings.Contains(stripANSI(viewText(m)), want) {
			return m
		}
		m = upd(m, p.next())
	}
	p.t.Fatalf("never saw %q in view", want)
	return m
}

func (p *pump) drainIdle(m Model) Model {
	for i := 0; i < 300; i++ {
		if !strings.Contains(stripANSI(viewText(m)), "sending...") {
			return m
		}
		m = upd(m, p.next())
	}
	p.t.Fatalf("model still busy after draining")
	return m
}

func upd(m Model, msg tea.Msg) Model {
	nm, _ := m.Update(msg)
	return nm.(Model)
}

func updCmd(m Model, msg tea.Msg) (Model, tea.Cmd) {
	nm, cmd := m.Update(msg)
	return nm.(Model), cmd
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m = upd(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func enter() tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: tea.KeyEnter}
}

func TestReplayNoNetwork(t *testing.T) {
	st := newTestStore(t)
	sess, err := st.CreateSession(context.Background(), db.Session{Title: "t", Directory: "/tmp/x"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	um, err := st.InsertMessage(context.Background(), db.Message{SessionID: sess.ID, Role: "user"})
	if err != nil {
		t.Fatalf("insert user message: %v", err)
	}
	userText := "hello there"
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: um.ID, Type: "text", Text: &userText}); err != nil {
		t.Fatalf("insert user part: %v", err)
	}
	am, err := st.InsertMessage(context.Background(), db.Message{SessionID: sess.ID, Role: "assistant"})
	if err != nil {
		t.Fatalf("insert assistant message: %v", err)
	}
	asstText := "hi back"
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: am.ID, Type: "text", Text: &asstText}); err != nil {
		t.Fatalf("insert assistant text part: %v", err)
	}
	reason := "hmm"
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: am.ID, Type: "reasoning", Text: &reason}); err != nil {
		t.Fatalf("insert reasoning part: %v", err)
	}
	name := "bash"
	status := "completed"
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: am.ID, Type: "tool", ToolName: &name, ToolStatus: &status}); err != nil {
		t.Fatalf("insert tool part: %v", err)
	}

	m := New(Options{Store: st, Client: deadClient(), Workdir: t.TempDir(), Session: &sess})
	v := stripANSI(viewText(m))
	for _, want := range []string{"user: hello there", "assistant: hi back", "reasoning: (collapsed)", "bash: completed"} {
		if !strings.Contains(v, want) {
			t.Errorf("View() missing %q: %q", want, v)
		}
	}
}

func TestEmptyEnterIgnored(t *testing.T) {
	st := newTestStore(t)
	m := New(Options{Store: st, Client: deadClient(), Workdir: t.TempDir()})
	before := viewText(m)
	m, cmd := updCmd(m, enter())
	if cmd != nil {
		t.Fatalf("empty Enter returned cmd %v, want nil", cmd)
	}
	if v := viewText(m); v != before {
		t.Errorf("empty Enter changed view:\nbefore: %q\nafter:  %q", before, v)
	}
}

func TestInitialErrShownAndPromptUsable(t *testing.T) {
	st := newTestStore(t)
	const errText = "opencode: OPENCODE_API_KEY (or OPENCODE_ZEN_API_KEY) is not set"
	m := New(Options{Store: st, Client: deadClient(), Workdir: t.TempDir(), InitialErr: errText})
	if !strings.Contains(stripANSI(viewText(m)), errText) {
		t.Fatalf("View() missing initial error %q: %q", errText, viewText(m))
	}
	m = typeText(m, "hello")
	if got := m.prompt.Value(); got != "hello" {
		t.Errorf("prompt value = %q, want %q", got, "hello")
	}
	if !strings.Contains(stripANSI(viewText(m)), errText) {
		t.Errorf("error disappeared while typing: %q", viewText(m))
	}
}

func TestErrorTurnPreservesUserText(t *testing.T) {
	fake := newFakeProvider(t, 500, "boom")
	st := newTestStore(t)
	m := New(Options{Store: st, Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = typeText(m, "hi")
	m, cmd := updCmd(m, enter())
	if cmd == nil {
		t.Fatal("Enter returned nil cmd")
	}
	p.run(cmd)
	m = p.drainIdle(m)
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "user: hi") {
		t.Errorf("user text lost on error: %q", v)
	}
	if !strings.Contains(v, "500") {
		t.Errorf("no error text in view: %q", v)
	}
	if strings.Contains(v, "sending...") {
		t.Errorf("status still busy: %q", v)
	}
}

func TestConfirmDeny(t *testing.T) {
	tmp := t.TempDir()
	tc := fakeToolCall{ID: "call_1", Name: "bash", Args: fmt.Sprintf(`{"command":"rm -rf %s/lazy-x"}`, tmp)}
	fake := newFakeProvider(t, 0,
		respBody("", "tool-calls", []fakeToolCall{tc}),
		respBody("done", "stop", nil),
	)
	st := newTestStore(t)
	m := New(Options{Store: st, Client: newClient(fake.srv), Workdir: tmp})
	p := newPump(t)
	p.run(m.Init())
	m = typeText(m, "please clean")
	m, cmd := updCmd(m, enter())
	if cmd == nil {
		t.Fatal("Enter returned nil cmd")
	}
	p.run(cmd)
	m = p.drainUntil(m, "y confirm")
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "Delete ") {
		t.Errorf("confirm view missing Delete: %q", v)
	}
	m = upd(m, tea.KeyPressMsg{Code: 'n'})
	m = p.drainIdle(m)
	v = stripANSI(viewText(m))
	if !strings.Contains(v, "bash: denied") {
		t.Errorf("want tool card bash: denied, got %q", v)
	}
	if strings.Contains(v, "bash: pending") {
		t.Errorf("pending tool card not replaced: %q", v)
	}
	if !strings.Contains(v, "assistant: done") {
		t.Errorf("final reply missing: %q", v)
	}
	if n := fake.requestCount(); n != 2 {
		t.Errorf("provider calls = %d, want 2", n)
	}
}

func TestConfirmAllow(t *testing.T) {
	tmp := t.TempDir()
	tc := fakeToolCall{ID: "call_2", Name: "bash", Args: fmt.Sprintf(`{"command":"rm -rf %s/lazy-x"}`, tmp)}
	fake := newFakeProvider(t, 0,
		respBody("", "tool-calls", []fakeToolCall{tc}),
		respBody("done", "stop", nil),
	)
	st := newTestStore(t)
	m := New(Options{Store: st, Client: newClient(fake.srv), Workdir: tmp})
	p := newPump(t)
	p.run(m.Init())
	m = typeText(m, "clean it")
	m, cmd := updCmd(m, enter())
	if cmd == nil {
		t.Fatal("Enter returned nil cmd")
	}
	p.run(cmd)
	m = p.drainUntil(m, "y confirm")
	m = upd(m, tea.KeyPressMsg{Code: 'y'})
	m = p.drainIdle(m)
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "bash: completed") {
		t.Errorf("want tool card bash: completed, got %q", v)
	}
	if !strings.Contains(v, "bash: denied") {
		if strings.Contains(v, "bash: pending") {
			t.Errorf("tool card still pending: %q", v)
		}
	}
}

func TestAskQuestion(t *testing.T) {
	qArgs := `{"questions":[{"question":"pick one?","header":"choice","options":["alpha","beta"]}]}`
	tc := fakeToolCall{ID: "call_q", Name: "question", Args: qArgs}
	fake := newFakeProvider(t, 0,
		respBody("", "tool-calls", []fakeToolCall{tc}),
		respBody("done", "stop", nil),
	)
	st := newTestStore(t)
	m := New(Options{Store: st, Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = typeText(m, "what should i pick")
	m, cmd := updCmd(m, enter())
	if cmd == nil {
		t.Fatal("Enter returned nil cmd")
	}
	p.run(cmd)
	m = p.drainUntil(m, "y confirm")
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "pick one?") || !strings.Contains(v, "choice") {
		t.Errorf("ask confirm view missing question/header: %q", v)
	}
	m = upd(m, tea.KeyPressMsg{Code: 'y'})
	m = p.drainIdle(m)
	v = stripANSI(viewText(m))
	if !strings.Contains(v, "question: completed") {
		t.Errorf("want tool card question: completed, got %q", v)
	}
	if !strings.Contains(v, "assistant: done") {
		t.Errorf("final reply missing: %q", v)
	}
}

func TestConfirmModeKeyIsolation(t *testing.T) {
	tmp := t.TempDir()
	tc := fakeToolCall{ID: "call_3", Name: "bash", Args: fmt.Sprintf(`{"command":"rm -rf %s/lazy-x"}`, tmp)}
	fake := newFakeProvider(t, 0,
		respBody("", "tool-calls", []fakeToolCall{tc}),
		respBody("done", "stop", nil),
	)
	st := newTestStore(t)
	m := New(Options{Store: st, Client: newClient(fake.srv), Workdir: tmp})
	p := newPump(t)
	p.run(m.Init())
	m = typeText(m, "clean it")
	m, cmd := updCmd(m, enter())
	p.run(cmd)
	m = p.drainUntil(m, "y confirm")
	m = typeText(m, "abc")
	v := stripANSI(viewText(m))
	if strings.Contains(v, "abc") {
		t.Errorf("keys leaked into confirm view: %q", v)
	}
	m = upd(m, tea.KeyPressMsg{Code: 'n'})
	m = p.drainIdle(m)
	if got := m.prompt.Value(); got != "" {
		t.Errorf("prompt value = %q, want empty after confirm keys", got)
	}
	if strings.Contains(stripANSI(viewText(m)), "abc") {
		t.Errorf("keys leaked into chat view: %q", viewText(m))
	}
}

func TestQuitKeys(t *testing.T) {
	st := newTestStore(t)
	m := New(Options{Store: st, Client: deadClient(), Workdir: t.TempDir()})
	m, cmd := updCmd(m, tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Fatal("q in normal mode returned nil cmd")
	}
	if msg := cmd(); msg != (tea.QuitMsg{}) {
		t.Errorf("q in normal mode: cmd() = %#v, want tea.QuitMsg", msg)
	}

	tmp := t.TempDir()
	tc := fakeToolCall{ID: "call_4", Name: "bash", Args: fmt.Sprintf(`{"command":"rm -rf %s/lazy-x"}`, tmp)}
	fake := newFakeProvider(t, 0,
		respBody("", "tool-calls", []fakeToolCall{tc}),
		respBody("done", "stop", nil),
	)
	m2 := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: tmp})
	p := newPump(t)
	p.run(m2.Init())
	m2 = typeText(m2, "clean it")
	m2, cmd = updCmd(m2, enter())
	p.run(cmd)
	m2 = p.drainUntil(m2, "y confirm")
	m2, cmd = updCmd(m2, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+c in confirm mode returned nil cmd")
	}
	if msg := cmd(); msg != (tea.QuitMsg{}) {
		t.Errorf("ctrl+c in confirm mode: cmd() = %#v, want tea.QuitMsg", msg)
	}
	deadline := time.Now().Add(5 * time.Second)
	for fake.requestCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := fake.requestCount(); n != 2 {
		t.Errorf("agent did not finish after quit, provider calls = %d, want 2", n)
	}
}

func TestBusyIgnoresEnter(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("ok", "stop", nil))
	st := newTestStore(t)
	m := New(Options{Store: st, Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = typeText(m, "first")
	m, cmd := updCmd(m, enter())
	if cmd == nil {
		t.Fatal("Enter returned nil cmd")
	}
	p.run(cmd)
	m = typeText(m, "second")
	m, cmd2 := updCmd(m, enter())
	if cmd2 != nil {
		t.Fatal("Enter while busy returned a cmd")
	}
	m = p.drainIdle(m)
	if n := fake.requestCount(); n != 1 {
		t.Errorf("provider calls = %d, want 1", n)
	}
	if !strings.Contains(stripANSI(viewText(m)), "user: first") {
		t.Errorf("first turn missing from view: %q", viewText(m))
	}
}

func TestModelsFetchedOnStartup(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())

	m = p.runStep(m, p.next())
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "models: 2 available") {
		t.Errorf("status missing models count: %q", v)
	}
	if !strings.Contains(v, "model default") {
		t.Errorf("status missing current model label: %q", v)
	}
}

func TestModelPickerSwitchAndPersist(t *testing.T) {
	tmp := t.TempDir()
	st := newTestStore(t)
	sess, err := st.CreateSession(context.Background(), db.Session{Title: "s", Directory: tmp})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	fake := newFakeProvider(t, 0, respBody("hello", "stop", nil))
	m := New(Options{Store: st, Client: newClient(fake.srv), Workdir: tmp, Session: &sess})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	m = upd(m, tea.KeyPressMsg{Code: 'm'})
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "Models") || !strings.Contains(v, "2 items") {
		t.Fatalf("picker missing model list: %q", v)
	}
	m = upd(m, tea.KeyPressMsg{Code: 'j'})
	m, cmd := updCmd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter returned nil cmd")
	}
	p.run(cmd)
	m = p.runStep(m, p.next())

	msgs, err := st.ListSessionsByDir(context.Background(), tmp)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Model != "claude-4" {
		t.Errorf("session model = %+v, want claude-4", msgs)
	}

	m = typeText(m, "hi there")
	m, cmd = updCmd(m, enter())
	if cmd == nil {
		t.Fatal("Enter returned nil cmd")
	}
	p.run(cmd)
	m = p.drainIdle(m)
	if got := fake.requestModel(0); got != "claude-4" {
		t.Errorf("wire model = %q, want claude-4", got)
	}
}

func TestModelPickerCancel(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m = upd(m, tea.KeyPressMsg{Code: 'm'})
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "no models loaded") || !strings.Contains(v, "esc cancel") {
		t.Fatalf("picker not shown: %q", v)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	v = stripANSI(viewText(m))
	if strings.Contains(v, "esc cancel") {
		t.Errorf("picker still shown after esc: %q", v)
	}
	if !strings.Contains(v, "m switch") {
		t.Errorf("normal view not restored after esc: %q", v)
	}
	m = upd(m, tea.KeyPressMsg{Code: 'm'})
	m = upd(m, tea.KeyPressMsg{Code: 'q'})
	v = stripANSI(viewText(m))
	if strings.Contains(v, "esc cancel") {
		t.Errorf("picker still shown after q: %q", v)
	}
	if !strings.Contains(v, "m switch") {
		t.Errorf("normal view not restored after q: %q", v)
	}
}
