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
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
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

func clickModelStatus(t *testing.T, m Model) Model {
	t.Helper()
	left, top, right, bottom, ok := m.modelStatusRect()
	if !ok || right <= left || bottom <= top {
		t.Fatal("model status click target not found")
	}
	mm, _ := m.Update(tea.MouseClickMsg(tea.Mouse{
		X:      right - 1,
		Y:      top,
		Button: tea.MouseLeft,
	}))
	return mm.(Model)
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
	_, cmd := updCmd(m, tea.KeyPressMsg{Code: 'q'})
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
	_, cmd = updCmd(m2, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
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
	if !strings.Contains(v, "model deepseek-v4-flash") {
		t.Errorf("status missing current model label: %q", v)
	}
}

func TestPromptPasteAndDoubleEscape(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})

	m = upd(m, tea.PasteMsg{Content: "pasted prompt"})
	if got := m.prompt.Value(); got != "pasted prompt" {
		t.Fatalf("prompt after paste = %q, want %q", got, "pasted prompt")
	}
	m.prompt.SetValue("")
	m = upd(m, tea.PasteMsg{Content: "/model"})
	if m.slashMode {
		t.Fatal("pasting slash text opened the slash menu")
	}
	m.prompt.SetValue("pasted prompt")

	m, cmd := updCmd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Fatal("first escape returned a command")
	}
	if got := m.prompt.Value(); got != "pasted prompt" {
		t.Fatalf("prompt after first escape = %q, want unchanged text", got)
	}

	m, cmd = updCmd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		t.Fatal("second escape returned a command")
	}
	if got := m.prompt.Value(); got != "" {
		t.Fatalf("prompt after second escape = %q, want empty", got)
	}
}

func TestModelPickerOpensOnlyFromModelClick(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})

	m = upd(m, tea.KeyPressMsg{Code: 'm', Text: "m"})
	if m.pickerMode {
		t.Fatal("pressing m opened the model picker")
	}
	if got := m.prompt.Value(); got != "m" {
		t.Fatalf("prompt after m = %q, want %q", got, "m")
	}

	m = clickModelStatus(t, m)
	if !m.pickerMode {
		t.Fatal("clicking the model status did not open the picker")
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

	m = clickModelStatus(t, m)
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "deepseek-v4-flash") || !strings.Contains(v, "claude-4") {
		t.Fatalf("picker missing models: %q", v)
	}
	if !strings.Contains(v, "filter /") {
		t.Fatalf("picker missing filter prompt: %q", v)
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
	p.drainIdle(m)
	if got := fake.requestModel(0); got != "claude-4" {
		t.Errorf("wire model = %q, want claude-4", got)
	}
}

func TestModelPickerCancel(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m = clickModelStatus(t, m)
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "no models loaded") || !strings.Contains(v, "esc cancel") {
		t.Fatalf("picker not shown: %q", v)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	v = stripANSI(viewText(m))
	if strings.Contains(v, "esc cancel") {
		t.Errorf("picker still shown after esc: %q", v)
	}
	if !strings.Contains(v, "click model to switch") {
		t.Errorf("normal view not restored after esc: %q", v)
	}
	m = clickModelStatus(t, m)
	m = upd(m, tea.KeyPressMsg{Code: 'q'})
	v = stripANSI(viewText(m))
	if strings.Contains(v, "esc cancel") {
		t.Errorf("picker still shown after q: %q", v)
	}
	if !strings.Contains(v, "click model to switch") {
		t.Errorf("normal view not restored after q: %q", v)
	}
}

func TestTitleStatic(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "lazykoder") {
		t.Fatalf("title missing: %q", v)
	}
	mm, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	m = mm.(Model)
	if !strings.Contains(stripANSI(viewText(m)), "lazykoder") {
		t.Errorf("title missing after key input: %q", stripANSI(viewText(m)))
	}
}

func TestTranscriptScrollbarAndFlow(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m = typeText(m, "hello")
	m, cmd := updCmd(m, enter())
	p := newPump(t)
	p.run(cmd)
	m = p.drainIdle(m)

	v := stripANSI(viewText(m))
	if !strings.Contains(v, "user: hello") || !strings.Contains(v, "assistant: hi") {
		t.Fatalf("transcript missing lines: %q", v)
	}
	// No overflow with two lines: no scrollbar cells.
	if strings.Contains(v, "░") || strings.Contains(v, "█") {
		t.Errorf("scrollbar shown without overflow: %q", v)
	}

	// Fill the transcript beyond its default height so it overflows.
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m.lines = nil
	for i := 0; i < 40; i++ {
		m.lines = append(m.lines, fmt.Sprintf("line %02d", i))
	}
	m.syncTranscript()
	v = stripANSI(viewText(m))
	if !strings.Contains(v, "line 39") {
		t.Errorf("newest line not visible at bottom: %q", v)
	}
	if strings.Contains(v, "line 00") {
		t.Errorf("oldest line should have scrolled up: %q", v)
	}
	if !strings.Contains(v, "░") {
		t.Errorf("scrollbar track missing with overflow: %q", v)
	}
}

func TestTranscriptScrollKeys(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	for i := 0; i < 60; i++ {
		m.lines = append(m.lines, fmt.Sprintf("line %02d", i))
	}
	m.syncTranscript()

	mm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = mm.(Model)
	if m.transcript.AtBottom() {
		t.Errorf("viewport still at bottom after one Up scroll")
	}
	mm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	m = mm.(Model)
	if !m.transcript.AtTop() {
		t.Errorf("Home did not jump to top")
	}
	mm, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	m = mm.(Model)
	if !m.transcript.AtBottom() {
		t.Errorf("End did not jump to bottom")
	}
}

func TestInputHistoryCopyDeleteAndVisibility(t *testing.T) {
	tmp := t.TempDir()
	st := newTestStore(t)
	sess, err := st.CreateSession(context.Background(), db.Session{Title: "history", Directory: tmp})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil), respBody("hello", "stop", nil))
	m := New(Options{Store: st, Client: newClient(fake.srv), Workdir: tmp, Session: &sess})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	m = typeText(m, "first prompt")
	m, cmd := updCmd(m, enter())
	p.run(cmd)
	m = p.drainIdle(m)
	m = typeText(m, "second prompt")
	m, cmd = updCmd(m, enter())
	p.run(cmd)
	m = p.drainIdle(m)

	if len(m.inputHistory) != 2 || m.inputHistory[0].messageID == "" || m.inputHistory[1].messageID == "" {
		t.Fatalf("input history missing persisted message IDs: %+v", m.inputHistory)
	}
	if m.lines[0] == "user: first prompt" {
		t.Fatal("user message was not highlighted")
	}

	m = upd(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.prompt.Value(); got != "second prompt" {
		t.Fatalf("first Up prompt = %q, want second prompt", got)
	}
	if !strings.Contains(stripANSI(viewText(m)), "history: ↑/↓ previous/next") {
		t.Fatal("history actions are not shown for the selected message")
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.prompt.Value(); got != "first prompt" {
		t.Fatalf("second Up prompt = %q, want first prompt", got)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.prompt.Value(); got != "second prompt" {
		t.Fatalf("Down prompt = %q, want second prompt", got)
	}

	m, cmd = updCmd(m, tea.KeyPressMsg{Code: 'c'})
	if cmd == nil || fmt.Sprint(cmd()) != "second prompt" {
		t.Fatalf("copy command did not contain selected text: %v", cmd)
	}
	messageID := m.inputHistory[m.historyCursor].messageID
	m, cmd = updCmd(m, tea.KeyPressMsg{Code: 'd'})
	if cmd == nil {
		t.Fatal("delete did not return a persistence command")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("delete command returned %v", msg)
	}
	if strings.Contains(stripANSI(viewText(m)), "user: second prompt") {
		t.Fatal("deleted user message remains visible in the transcript")
	}
	messages, err := st.ListMessages(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("list messages after delete: %v", err)
	}
	foundHidden := false
	for _, msg := range messages {
		if msg.ID == messageID {
			if msg.Visible {
				t.Fatal("deleted user message is still visible in the database")
			}
			foundHidden = true
		}
	}
	if !foundHidden {
		t.Fatalf("deleted message %q not found in database", messageID)
	}
	replayed := New(Options{Store: st, Client: newClient(fake.srv), Workdir: tmp, Session: &sess})
	if strings.Contains(stripANSI(viewText(replayed)), "user: second prompt") {
		t.Fatal("soft-deleted user message returned in the UI replay")
	}
}

func TestModelPickerFilter(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	m = clickModelStatus(t, m)
	m = upd(m, tea.KeyPressMsg{Code: '/'})
	for _, r := range "claude" {
		m = upd(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	v := stripANSI(viewText(m))
	card := v[strings.Index(v, "╭"):]
	if len(m.pickerItems) != 1 || m.pickerItems[0] != "claude-4" {
		t.Errorf("filtered picker items = %v, want [claude-4]", m.pickerItems)
	}
	if !strings.Contains(card, "AVAILABLE MODELS") || !strings.Contains(card, "claude-4") {
		t.Errorf("matching model missing: %q", card)
	}
	if !strings.Contains(v, "filter: claude") {
		t.Errorf("filter prompt missing query: %q", v)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	v = stripANSI(viewText(m))
	if !strings.Contains(v, "claude-4") {
		t.Errorf("filter exit lost the list: %q", v)
	}
	if !strings.Contains(v, "filter: claude") {
		t.Errorf("active filter query not shown after exit: %q", v)
	}
}

func typeRune(m Model, r rune) Model {
	return upd(m, tea.KeyPressMsg{Code: r, Text: string(r)})
}

func TestSlashMenuOpensAndDivides(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	if !strings.Contains(stripANSI(viewText(m)), "ask lazykoder... (type / for commands)") {
		t.Fatalf("prompt placeholder missing: %q", stripANSI(viewText(m)))
	}
	m = typeRune(m, '/')
	if !m.slashMode {
		t.Fatal("slash mode not opened on /")
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "/new") || !strings.Contains(v, "/model") || !strings.Contains(v, "/help") {
		t.Errorf("slash menu missing commands: %q", v)
	}
	if !strings.Contains(v, "start a new session") {
		t.Errorf("slash menu missing detail pane: %q", v)
	}
	if !strings.Contains(v, "│") {
		t.Errorf("slash menu missing vertical divider: %q", v)
	}
}

func TestSlashMenuAnchorsAbovePrompt(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m = typeRune(m, '/')

	lines := strings.Split(stripANSI(viewText(m)), "\n")
	top := -1
	bottom := -1
	prompt := -1
	for i, line := range lines {
		if strings.Contains(line, "╭") && top == -1 {
			top = i
		}
		if strings.Contains(line, "╰") {
			bottom = i
		}
		if strings.Contains(line, "▏/") {
			prompt = i
		}
	}
	if top < 0 || bottom < 0 || prompt < 0 {
		t.Fatalf("slash card or prompt missing: %q", lines)
	}
	if bottom >= prompt {
		t.Errorf("slash card bottom row %d is not above prompt row %d", bottom, prompt)
	}
	if len(lines) > m.height {
		t.Errorf("slash view has %d rows for a %d-row terminal", len(lines), m.height)
	}
	if !strings.Contains(stripANSI(viewText(m)), "/▏") {
		t.Errorf("slash query input missing: %q", lines)
	}
}

func TestSlashMenuFilterAndRunNew(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m = typeRune(m, '/')
	m = typeRune(m, 'm')
	if len(m.slashItems) != 1 || m.slashItems[0].name != "/model" {
		t.Fatalf("filtered items = %+v, want only /model", m.slashItems)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.slashMode {
		t.Fatal("slash mode still open after esc")
	}
	if got := m.prompt.Value(); got != "/" {
		t.Fatalf("prompt after esc = %q, want /", got)
	}

	m = typeRune(m, 'm')
	if !m.slashMode || len(m.slashItems) != 1 || m.slashItems[0].name != "/model" {
		t.Fatalf("menu not reopened with /model filter: %+v", m.slashItems)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.slashMode {
		t.Fatal("slash mode still open after enter")
	}
	if !m.pickerMode {
		t.Fatal("enter on /model did not open the picker")
	}

	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m.lines = append(m.lines, "old line")
	m = typeRune(m, '/')
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.slashMode {
		t.Fatal("slash mode still open after /new")
	}
	if len(m.lines) != 0 {
		t.Errorf("transcript not cleared by /new: %d lines", len(m.lines))
	}
	if m.session != nil {
		t.Errorf("/new should drop the session for a fresh one")
	}
}

func TestSlashMenuEscapeLeavesSlash(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m = typeRune(m, '/')
	m = typeRune(m, 'h')
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.slashMode {
		t.Fatal("slash mode still open")
	}
	if got := m.prompt.Value(); got != "/" {
		t.Errorf("prompt after esc = %q, want /", got)
	}
}

func TestMouseWheelScrollsTranscript(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	for i := 0; i < 60; i++ {
		m.lines = append(m.lines, fmt.Sprintf("line %02d", i))
	}
	m.syncTranscript()
	if !m.transcript.AtBottom() {
		t.Fatal("expected viewport at bottom after sync")
	}
	mm, _ := m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp}))
	m = mm.(Model)
	if m.transcript.AtBottom() {
		t.Error("wheel up did not scroll the transcript")
	}
	mm, _ = m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	m = mm.(Model)
	if !m.transcript.AtBottom() {
		t.Error("wheel down did not return to bottom")
	}
}

func TestScrollbarClickJumpAndDrag(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	for i := 0; i < 80; i++ {
		m.lines = append(m.lines, fmt.Sprintf("line %02d", i))
	}
	m.syncTranscript()
	if !m.transcript.AtBottom() {
		t.Fatal("expected at bottom")
	}

	col := m.width - 1
	mm, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: col, Y: 4, Button: tea.MouseLeft}))
	m = mm.(Model)
	if !m.dragOn {
		t.Fatal("click on scrollbar did not start a drag")
	}
	if m.transcript.AtBottom() {
		t.Error("click-jump did not scroll up")
	}

	mm, _ = m.Update(tea.MouseMotionMsg(tea.Mouse{X: col, Y: 2, Button: tea.MouseLeft}))
	m = mm.(Model)
	topPct := m.transcript.ScrollPercent()
	if !m.transcript.AtTop() {
		t.Errorf("drag to top row did not reach top (pct %.2f)", topPct)
	}

	mm, _ = m.Update(tea.MouseReleaseMsg(tea.Mouse{X: col, Y: 2}))
	m = mm.(Model)
	if m.dragOn {
		t.Error("release did not end the drag")
	}
	mm, _ = m.Update(tea.MouseMotionMsg(tea.Mouse{X: col, Y: 2, Button: tea.MouseLeft}))
	m = mm.(Model)
	if !m.transcript.AtTop() {
		t.Error("drag continued after release")
	}
}

func TestScrollbarClickIgnoredWithoutOverflow(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	col := m.width - 1
	mm, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: col, Y: 3, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.dragOn {
		t.Error("drag started without overflow")
	}
}

func TestPickerCardCenteredSettingsLayout(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	m = clickModelStatus(t, m)
	v := stripANSI(viewText(m))
	card := stripANSI(m.pickerView())
	if !strings.Contains(v, "SETTINGS  /  MODEL") || !strings.Contains(v, "AVAILABLE MODELS") {
		t.Fatalf("settings card labels missing: %q", v)
	}
	if got, want := lipgloss.Width(card), m.width*cardWidthPct/100; got != want {
		t.Errorf("card width = %d, want %d (%d%% of screen)", got, want, cardWidthPct)
	}
	cardLine := strings.Index(strings.Split(v, "\n")[0], "╭")
	if cardLine >= 0 {
		t.Fatalf("card unexpectedly starts on the first screen row: %q", v)
	}
	for _, line := range strings.Split(v, "\n") {
		if strings.Contains(line, "╭") {
			wantLeft := (m.width - lipgloss.Width(card)) / 2
			if got := strings.Index(line, "╭"); got != wantLeft {
				t.Errorf("card left offset = %d, want %d: %q", got, wantLeft, line)
			}
			return
		}
	}
	t.Fatalf("card border missing: %q", v)
}

func TestPickerCloseButton(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	m = clickModelStatus(t, m)
	v := stripANSI(viewText(m))
	headerFound := false
	for _, line := range strings.Split(v, "\n") {
		if strings.Contains(line, "SETTINGS  /  MODEL") {
			headerFound = true
			if !strings.HasSuffix(strings.TrimSpace(line), "X│") {
				t.Fatalf("settings card close button is not at the header edge: %q", line)
			}
		}
	}
	if !headerFound {
		t.Fatalf("settings card header missing: %q", v)
	}
	x, y, ok := m.pickerCloseRect()
	if !ok {
		t.Fatal("picker close button rectangle not found")
	}
	mm, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.pickerMode {
		t.Fatal("clicking the picker close button did not close the card")
	}

	m = clickModelStatus(t, m)
	m = upd(m, tea.KeyPressMsg{Code: 'x'})
	if m.pickerMode {
		t.Fatal("x did not close the picker card")
	}
}

func TestPickerArrowKeysRefreshSelectionAndScroll(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())
	m.models = make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		m.models = append(m.models, fmt.Sprintf("model-%02d", i))
	}

	m = clickModelStatus(t, m)
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if m.pickerCursor != 1 {
		t.Fatalf("down cursor = %d, want 1", m.pickerCursor)
	}
	v := stripANSI(m.pickerView())
	if !strings.Contains(v, "▸ model-01") || strings.Contains(v, "▸ model-00") {
		t.Fatalf("down did not refresh the visible selection: %q", v)
	}

	m = upd(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if m.pickerCursor != 0 {
		t.Fatalf("up cursor = %d, want 0", m.pickerCursor)
	}
	v = stripANSI(m.pickerView())
	if !strings.Contains(v, "▸ model-00") || strings.Contains(v, "▸ model-01") {
		t.Fatalf("up did not refresh the visible selection: %q", v)
	}

	for i := 0; i < 12; i++ {
		m = upd(m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.pickerCursor != 12 {
		t.Fatalf("scrolled cursor = %d, want 12", m.pickerCursor)
	}
	v = stripANSI(m.pickerView())
	if !strings.Contains(v, "▸ model-12") {
		t.Fatalf("scrolled view did not show the selected model: %q", v)
	}
}

func TestPickerCardFitsAndScrollbarDrags(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mm.(Model)

	for i := 0; i < 30; i++ {
		m.models = append(m.models, fmt.Sprintf("model-%02d", i))
	}
	m = clickModelStatus(t, m)

	v := stripANSI(viewText(m))
	lines := strings.Split(v, "\n")
	bottomBorder := -1
	for i, line := range lines {
		if strings.Contains(line, "╰") {
			bottomBorder = i
		}
	}
	if bottomBorder < 0 {
		t.Fatalf("card bottom border missing: %q", v)
	}
	if bottomBorder >= 30 {
		t.Errorf("card bottom (%d) below the 30-row screen", bottomBorder)
	}

	top, _, col, ok := m.scrollbarRect(1)
	if !ok {
		t.Fatal("picker scrollbar rect not found")
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: col, Y: top, Button: tea.MouseLeft}))
	m = mm.(Model)
	if !m.dragOn {
		t.Fatal("click on picker scrollbar did not start a drag")
	}
	if !m.pickerVp.AtTop() {
		t.Error("click at the top of the track should stay at top")
	}
	mm, _ = m.Update(tea.MouseMotionMsg(tea.Mouse{X: col, Y: top + 4, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.pickerVp.AtTop() {
		t.Error("drag did not scroll the picker")
	}
	mm, _ = m.Update(tea.MouseReleaseMsg(tea.Mouse{X: col, Y: top + 4}))
	m = mm.(Model)
	if m.dragOn {
		t.Error("release did not end picker drag")
	}
}

func TestModelsLoadedFromCacheSkipsAPI(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "models.json")
	if err := modelscache.Save(cachePath, []string{"deepseek-v4-flash", "claude-4"}, time.Now()); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: dir, CachePath: cachePath})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	v := stripANSI(viewText(m))
	if !strings.Contains(v, "models: 2 available (cached)") {
		t.Errorf("status missing cached label: %q", v)
	}
	if !m.modelsCached {
		t.Error("modelsCached = false, want true for fresh cache")
	}
}

func TestModelsCacheRefreshedWhenStale(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "models.json")
	stale := time.Now().Add(-modelscache.DefaultTTL - time.Minute)
	if err := modelscache.Save(cachePath, []string{"stale-model"}, stale); err != nil {
		t.Fatalf("seed stale cache: %v", err)
	}
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: dir, CachePath: cachePath})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	v := stripANSI(viewText(m))
	if !strings.Contains(v, "models: 2 available") {
		t.Errorf("status missing refreshed count: %q", v)
	}
	if strings.Contains(v, "(cached)") {
		t.Errorf("status shows cached label after live refresh: %q", v)
	}
	if m.modelsCached {
		t.Error("modelsCached = true, want false after live refresh")
	}
	models, fresh, err := modelscache.Load(cachePath, time.Now(), modelscache.DefaultTTL)
	if err != nil {
		t.Fatalf("reload cache: %v", err)
	}
	if len(models) != 2 || models[0] == "stale-model" {
		t.Errorf("cache not rewritten: %v", models)
	}
	if !fresh {
		t.Error("cache still stale after refresh")
	}
}

func TestModelsRefreshKeyReloadsFromAPI(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "models.json")
	if err := modelscache.Save(cachePath, []string{"deepseek-v4-flash"}, time.Now()); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: dir, CachePath: cachePath})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())

	if !m.modelsCached {
		t.Fatal("precondition: models should come from cache")
	}
	m = clickModelStatus(t, m)
	if !m.pickerMode {
		t.Fatal("precondition: picker did not open")
	}
	mm, cmd := m.Update(tea.KeyPressMsg{Code: 'r'})
	m = mm.(Model)
	if m.pickerMode {
		t.Error("picker still open after refresh key")
	}
	p.run(cmd)
	m = p.runStep(m, p.next())

	v := stripANSI(viewText(m))
	if !strings.Contains(v, "models: 2 available") {
		t.Errorf("status missing refreshed count: %q", v)
	}
	if strings.Contains(v, "(cached)") {
		t.Errorf("status shows cached label after manual refresh: %q", v)
	}
	if m.modelsCached {
		t.Error("modelsCached = true, want false after manual refresh")
	}
}
