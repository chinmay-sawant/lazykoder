package chat

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

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
	callID := "call_replay"
	status := "completed"
	toolPart, err := st.InsertPart(context.Background(), db.Part{
		MessageID:  am.ID,
		Type:       "tool",
		ToolName:   &name,
		ToolCallID: &callID,
		ToolStatus: &status,
	})
	if err != nil {
		t.Fatalf("insert tool part: %v", err)
	}
	title := "echo hello"
	inputJSON := `{"command":"echo hello"}`
	output := "hello\n"
	exitCode := 0
	if err := st.InsertToolCall(context.Background(), db.ToolCall{
		PartID:    toolPart.ID,
		Tool:      name,
		CallID:    callID,
		Status:    status,
		Title:     &title,
		InputJSON: inputJSON,
		Output:    &output,
		ExitCode:  &exitCode,
	}); err != nil {
		t.Fatalf("insert tool call: %v", err)
	}

	m := New(Options{Store: st, Client: deadClient(), Workdir: t.TempDir(), Session: &sess})
	v := stripANSI(viewText(m))
	for _, want := range []string{"user: hello there", "assistant: hi back", "reasoning: hmm", "bash: completed", "echo hello", "hello"} {
		if !strings.Contains(v, want) {
			t.Errorf("View() missing %q: %q", want, v)
		}
	}
}

func TestBashCommandAndOutputRendered(t *testing.T) {
	fake := newFakeProvider(t, 0,
		respBody("", "tool-calls", []fakeToolCall{{ID: "call_output", Name: "bash", Args: `{"command":"echo hello"}`}}),
		respBody("done", "stop", nil),
	)
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = typeText(m, "run a command")
	m, cmd := updCmd(m, enter())
	if cmd == nil {
		t.Fatal("Enter returned nil cmd")
	}
	p.run(cmd)
	m = p.drainIdle(m)
	v := stripANSI(viewText(m))
	for _, want := range []string{"bash: completed", "echo hello", "output", "hello"} {
		if !strings.Contains(v, want) {
			t.Errorf("View() missing %q: %q", want, v)
		}
	}
}

func TestAssistantMarkdownRendered(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	text := "```python\nprint(\"Hello, World!\")\n```\n\n**How to run it:**\n\n1. Save the code in `hello.py`"
	m.applyPart(db.Part{Type: "text", Text: &text})
	v := stripANSI(viewText(m))
	for _, want := range []string{"assistant:", "print(\"Hello, World!\")", "How to run it:", "hello.py", "╭"} {
		if !strings.Contains(v, want) {
			t.Errorf("View() missing Markdown output %q: %q", want, v)
		}
	}
	if strings.Contains(v, "```") {
		t.Errorf("View() left Markdown fences in output: %q", v)
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
	m2, cmd = updCmd(m2, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatalf("first ctrl+c in confirm mode returned command %v, want nil", cmd)
	}
	if !m2.quitConfirm {
		t.Fatal("first ctrl+c did not show quit confirmation")
	}
	m2, cmd = updCmd(m2, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("second ctrl+c in confirm mode returned nil cmd")
	}
	if msg := cmd(); msg != (tea.QuitMsg{}) {
		t.Errorf("second ctrl+c in confirm mode: cmd() = %#v, want tea.QuitMsg", msg)
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
