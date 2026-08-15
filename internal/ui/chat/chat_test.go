package chat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
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
	for _, want := range []string{roleYou, "hello there", roleAssistant, "hi back", thinkingLabel, "bash", "completed", "echo hello"} {
		if !strings.Contains(v, want) {
			t.Errorf("View() missing %q: %q", want, v)
		}
	}
	if strings.Contains(v, "user:") || strings.Contains(v, "assistant:") || strings.Contains(v, "reasoning:") {
		t.Errorf("View() still has prefix labels: %q", v)
	}
	if strings.Contains(v, "hmm") {
		t.Errorf("collapsed reasoning leaked body: %q", v)
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
	for _, want := range []string{"bash", "completed", "echo hello"} {
		if !strings.Contains(v, want) {
			t.Errorf("View() missing %q: %q", want, v)
		}
	}
	if strings.Contains(v, "output") {
		t.Errorf("collapsed tool card showed output: %q", v)
	}
	m = m.toggleLastTool()
	v = stripANSI(viewText(m))
	for _, want := range []string{"output", "hello"} {
		if !strings.Contains(v, want) {
			t.Errorf("expanded View() missing %q: %q", want, v)
		}
	}
}

func TestAssistantMarkdownRendered(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	text := "```python\nprint(\"Hello, World!\")\n```\n\n**How to run it:**\n\n1. Save the code in `hello.py`"
	m.applyPart(db.Part{Type: "text", Text: &text})
	v := stripANSI(viewText(m))
	for _, want := range []string{roleAssistant, "print(\"Hello, World!\")", "How to run it:", "hello.py", "╭"} {
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
	if !strings.Contains(v, "hi") || !strings.Contains(v, roleYou) {
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
	if !strings.Contains(v, "bash") || !strings.Contains(v, "denied") {
		t.Errorf("want tool card bash denied, got %q", v)
	}
	if strings.Contains(v, "pending") {
		t.Errorf("pending tool card not replaced: %q", v)
	}
	if !strings.Contains(v, "done") {
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
	if !strings.Contains(v, "bash") || !strings.Contains(v, "completed") {
		t.Errorf("want tool card bash completed, got %q", v)
	}
	if strings.Contains(v, "pending") {
		t.Errorf("tool card still pending: %q", v)
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
	m = p.drainUntil(m, "j/k select")
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "pick one?") || !strings.Contains(v, "choice") || !strings.Contains(v, "alpha") {
		t.Errorf("ask overlay missing question/options: %q", v)
	}
	if !strings.Contains(v, roleYou) {
		t.Errorf("ask overlay wiped the chat: %q", v)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	m = p.drainIdle(m)
	v = stripANSI(viewText(m))
	if !strings.Contains(v, "question") || !strings.Contains(v, "completed") {
		t.Errorf("want tool card question completed, got %q", v)
	}
	if !strings.Contains(v, "done") {
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
	m, cmd := updCmd(m, tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd != nil {
		if msg := cmd(); msg == (tea.QuitMsg{}) {
			t.Fatal("q in the prompt quit the app")
		}
	}
	if got := m.prompt.Value(); got != "q" {
		t.Fatalf("prompt after q = %q, want q", got)
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
	if !strings.Contains(stripANSI(viewText(m)), "first") || !strings.Contains(stripANSI(viewText(m)), roleYou) {
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
	if !strings.Contains(v, "deepseek-v4-flash") {
		t.Errorf("header/status missing current model label: %q", v)
	}
	if strings.Contains(v, "enter to send") || strings.Contains(v, "q to quit") {
		t.Errorf("idle status still dumps key hints: %q", v)
	}
}

func TestTitleStatic(t *testing.T) {
	st := newTestStore(t)
	sess, err := st.CreateSession(context.Background(), db.Session{Title: "hello world go", Directory: t.TempDir()})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: t.TempDir(), Session: &sess})
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "lazykoder") || !strings.Contains(v, "hello world go") {
		t.Fatalf("header missing brand or session title: %q", v)
	}
	mm, _ := m.Update(tea.KeyPressMsg{Code: 'j'})
	m = mm.(Model)
	if !strings.Contains(stripANSI(viewText(m)), "hello world go") {
		t.Errorf("session title missing after key input: %q", stripANSI(viewText(m)))
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
	if !strings.Contains(v, "hello") || !strings.Contains(v, "hi") || !strings.Contains(v, roleYou) {
		t.Fatalf("transcript missing lines: %q", v)
	}
	// No overflow with two lines: no scrollbar cells.
	if strings.Contains(v, "░") || strings.Contains(v, "█") {
		t.Errorf("scrollbar shown without overflow: %q", v)
	}

	// Fill the transcript beyond its default height so it overflows.
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m.items = nil
	for i := 0; i < 40; i++ {
		m.items = append(m.items, transcriptItem{kind: itemNote, text: fmt.Sprintf("line %02d", i)})
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
		m.items = append(m.items, transcriptItem{kind: itemNote, text: fmt.Sprintf("line %02d", i)})
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

func TestQTypesInPrompt(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = typeText(m, "q")
	if m.prompt.Value() != "q" {
		t.Fatalf("prompt = %q, want q", m.prompt.Value())
	}
	m, cmd := updCmd(m, tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd != nil {
		if msg := cmd(); msg == (tea.QuitMsg{}) {
			t.Fatal("second q quit the app")
		}
	}
	if m.prompt.Value() != "qq" {
		t.Fatalf("prompt = %q, want qq", m.prompt.Value())
	}

	m = New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = typeText(m, "hel")
	m, cmd = updCmd(m, tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd != nil {
		if msg := cmd(); msg == (tea.QuitMsg{}) {
			t.Fatal("q after hel quit the app")
		}
	}
	if m.prompt.Value() != "helq" {
		t.Fatalf("prompt = %q, want helq", m.prompt.Value())
	}
}

func TestLiveToolCardBeforeTurnEnds(t *testing.T) {
	fake := newFakeProvider(t, 0,
		respBody("", "tool-calls", []fakeToolCall{{ID: "call_live", Name: "bash", Args: `{"command":"echo hello"}`}}),
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
	m = p.drainUntil(m, "pending")
	if !m.busy {
		t.Fatal("busy cleared before the turn finished")
	}
	v := stripANSI(viewText(m))
	if strings.Contains(v, "done") && !m.busy {
		t.Fatalf("final text arrived before pending tool card: %q", v)
	}
	m = p.drainIdle(m)
	v = stripANSI(viewText(m))
	if !strings.Contains(v, "completed") || !strings.Contains(v, "hello") {
		t.Errorf("completed tool card missing: %q", v)
	}
}

func TestEscCancelsInFlightTurn(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("slow-ok", "stop", nil))
	fake.delay = 2 * time.Second
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = typeText(m, "please wait")
	m, cmd := updCmd(m, enter())
	if cmd == nil {
		t.Fatal("Enter returned nil cmd")
	}
	p.run(cmd)
	if !m.busy {
		t.Fatal("not busy after submit")
	}
	m, cmd = updCmd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd != nil {
		if msg := cmd(); msg == (tea.QuitMsg{}) {
			t.Fatal("esc quit the app")
		}
	}
	if m.busy {
		t.Fatal("still busy after esc")
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "cancelled") {
		t.Fatalf("cancelled note missing: %q", v)
	}
	if strings.Contains(v, "slow-ok") {
		t.Fatalf("late provider result was applied: %q", v)
	}
}

func TestConfirmEscDoesNotCancelTurn(t *testing.T) {
	tmp := t.TempDir()
	tc := fakeToolCall{ID: "call_esc", Name: "bash", Args: fmt.Sprintf(`{"command":"rm -rf %s/lazy-x"}`, tmp)}
	fake := newFakeProvider(t, 0,
		respBody("", "tool-calls", []fakeToolCall{tc}),
		respBody("done", "stop", nil),
	)
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: tmp})
	p := newPump(t)
	p.run(m.Init())
	m = typeText(m, "clean it")
	m, cmd := updCmd(m, enter())
	p.run(cmd)
	m = p.drainUntil(m, "y confirm")
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	m = p.drainIdle(m)
	v := stripANSI(viewText(m))
	if strings.Contains(v, "cancelled") {
		t.Errorf("confirm esc cancelled the turn: %q", v)
	}
	if !strings.Contains(v, "denied") {
		t.Errorf("confirm esc should deny: %q", v)
	}
}

func TestSessionPickerSelectsOlderSession(t *testing.T) {
	dir := t.TempDir()
	st := newTestStore(t)
	older, err := st.CreateSession(context.Background(), db.Session{
		Title: "older-session", Directory: dir, TimeCreated: 1000, TimeUpdated: 1000,
	})
	if err != nil {
		t.Fatalf("create older: %v", err)
	}
	um, err := st.InsertMessage(context.Background(), db.Message{SessionID: older.ID, Role: "user"})
	if err != nil {
		t.Fatalf("insert older user: %v", err)
	}
	oldText := "alpha-old"
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: um.ID, Type: "text", Text: &oldText}); err != nil {
		t.Fatalf("insert older part: %v", err)
	}
	newer, err := st.CreateSession(context.Background(), db.Session{
		Title: "newer-session", Directory: dir, TimeCreated: 2000, TimeUpdated: 2000,
	})
	if err != nil {
		t.Fatalf("create newer: %v", err)
	}
	nm, err := st.InsertMessage(context.Background(), db.Message{SessionID: newer.ID, Role: "user"})
	if err != nil {
		t.Fatalf("insert newer user: %v", err)
	}
	newText := "beta-new"
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: nm.ID, Type: "text", Text: &newText}); err != nil {
		t.Fatalf("insert newer part: %v", err)
	}

	m := New(Options{Store: st, Client: deadClient(), Workdir: dir, Session: &newer})
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "beta-new") {
		t.Fatalf("newer session not replayed: %q", v)
	}
	m = typeText(m, "/sessions")
	m, cmd := updCmd(m, enter())
	if cmd != nil {
		t.Fatalf("opening sessions returned cmd %v", cmd)
	}
	if !m.sessionPickerMode {
		t.Fatal("session picker not open")
	}
	v = stripANSI(viewText(m))
	if !strings.Contains(v, "older-session") || !strings.Contains(v, "newer-session") {
		t.Fatalf("picker missing titles: %q", v)
	}
	m = upd(m, tea.KeyPressMsg{Code: 'j'})
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.sessionPickerMode {
		t.Fatal("picker still open after enter")
	}
	v = stripANSI(viewText(m))
	if !strings.Contains(v, "alpha-old") {
		t.Errorf("older session text missing: %q", v)
	}
	if strings.Contains(v, "beta-new") {
		t.Errorf("newer session text still visible: %q", v)
	}
}

func TestSessionPickerEscKeepsCurrent(t *testing.T) {
	dir := t.TempDir()
	st := newTestStore(t)
	sess, err := st.CreateSession(context.Background(), db.Session{Title: "keep-me", Directory: dir})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	um, err := st.InsertMessage(context.Background(), db.Message{SessionID: sess.ID, Role: "user"})
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	text := "stay-visible"
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: um.ID, Type: "text", Text: &text}); err != nil {
		t.Fatalf("insert part: %v", err)
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: dir, Session: &sess})
	m = typeText(m, "/sessions")
	m = upd(m, enter())
	if !m.sessionPickerMode {
		t.Fatal("session picker not open")
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.sessionPickerMode {
		t.Fatal("picker still open after esc")
	}
	if m.session == nil || m.session.ID != sess.ID {
		t.Fatalf("session changed after esc: %+v", m.session)
	}
	if !strings.Contains(stripANSI(viewText(m)), "stay-visible") {
		t.Errorf("transcript lost after esc: %q", viewText(m))
	}
}

func TestComposerPutsModelOnTheRight(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.model = "deepseek-v4-flash"
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	var footer string
	for _, line := range strings.Split(stripANSI(viewText(m)), "\n") {
		if strings.Contains(line, "deepseek-v4-flash") {
			footer = line
		}
	}
	if footer == "" {
		t.Fatal("model missing from composer")
	}
	idxHint := strings.Index(footer, "enter send")
	idxModel := strings.Index(footer, "deepseek-v4-flash")
	if idxHint < 0 || idxModel < 0 || idxModel < idxHint {
		t.Fatalf("model is not right of the hint: %q", footer)
	}
}

func TestTurnShowsTimestamp(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.items = append(m.items, transcriptItem{kind: itemUser, text: "hello there", when: time.Now().UnixMilli()})
	m.syncTranscript()
	v := stripANSI(viewText(m))
	if !strings.Contains(v, roleYou) || !strings.Contains(v, "just now") {
		t.Fatalf("user turn missing timestamp: %q", v)
	}
}

func TestThinkingFrameUsesBrackets(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.busy = true
	m.pulseOn = true
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "[") || !strings.Contains(v, "]") {
		t.Fatalf("thinking frame missing brackets: %q", v)
	}
	if !strings.Contains(v, "│") {
		t.Fatalf("thinking frame missing vertical bar: %q", v)
	}
}

func TestSessionPickerGroupsByAge(t *testing.T) {
	dir := t.TempDir()
	st := newTestStore(t)
	now := time.Now().UnixMilli()
	if _, err := st.CreateSession(context.Background(), db.Session{
		Title: "fresh-run", Directory: dir, TimeCreated: now, TimeUpdated: now,
	}); err != nil {
		t.Fatal(err)
	}
	old := now - 3*24*60*60*1000
	if _, err := st.CreateSession(context.Background(), db.Session{
		Title: "old-run", Directory: dir, TimeCreated: old, TimeUpdated: old,
	}); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: dir})
	m = typeText(m, "/sessions")
	m = upd(m, enter())
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "just now") || !strings.Contains(v, "older") {
		t.Fatalf("session groups missing: %q", v)
	}
	if !strings.Contains(v, "fresh-run") || !strings.Contains(v, "old-run") {
		t.Fatalf("session titles missing: %q", v)
	}
}

func TestHeaderFitsAt80(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: "/tmp/proj"})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	header := m.headerView()
	if lipgloss.Height(header) > 2 {
		t.Fatalf("header is %d rows at width 80: %q", lipgloss.Height(header), header)
	}
	if !strings.Contains(stripANSI(viewText(m)), "ask lazykoder") {
		t.Fatalf("prompt missing at width 80: %q", viewText(m))
	}
}

func TestIdleStatusIsOneFactRow(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	v := stripANSI(viewText(m))
	if strings.Contains(v, "enter to send") || strings.Contains(v, "q to quit") || strings.Contains(v, "models: ") {
		t.Fatalf("idle view still dumps hints: %q", v)
	}
}

func TestReasoningToggle(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	text := "secret thought"
	m.items = append(m.items,
		transcriptItem{kind: itemUser, text: "hi"},
		transcriptItem{kind: itemReasoning, text: text, collapsed: true},
		transcriptItem{kind: itemAssistant, text: "hello"},
	)
	m.syncTranscript()
	v := stripANSI(viewText(m))
	if strings.Contains(v, text) {
		t.Fatalf("collapsed reasoning visible: %q", v)
	}
	if !strings.Contains(v, thinkingLabel) {
		t.Fatalf("thinking marker missing: %q", v)
	}
	m = m.toggleReasoning()
	v = stripANSI(viewText(m))
	if !strings.Contains(v, text) {
		t.Fatalf("expanded reasoning missing: %q", v)
	}
}

func TestShiftEnterDoesNotSubmit(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = typeText(m, "hello")
	m, cmd := updCmd(m, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	if cmd != nil {
		t.Fatalf("shift+enter submitted: %v", cmd)
	}
	if !strings.Contains(m.prompt.Value(), "\n") {
		t.Fatalf("shift+enter did not insert newline: %q", m.prompt.Value())
	}
	if m.busy {
		t.Fatal("shift+enter started a turn")
	}
}

func TestSlashDescriptionNotOnNextCommand(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m = typeRune(m, '/')
	var modelLine string
	for _, line := range strings.Split(stripANSI(viewText(m)), "\n") {
		if strings.Contains(line, "/model") {
			modelLine = line
			break
		}
	}
	if modelLine == "" {
		t.Fatal("/model row missing")
	}
	if strings.Contains(modelLine, "transcript") || strings.Contains(modelLine, "start a new") {
		t.Fatalf("description collided with /model row: %q", modelLine)
	}
}

func TestPickerHasNoOrphanFor(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())
	m = p.runStep(m, p.next())
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm.(Model)
	m = clickModelStatus(t, m)
	for _, line := range strings.Split(stripANSI(m.pickerView()), "\n") {
		if strings.TrimSpace(line) == "for" {
			t.Fatalf("picker left rail orphaned %q", line)
		}
	}
}

func TestEmptyStateShown(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "new run") || !strings.Contains(v, "/ commands") {
		t.Fatalf("empty state missing: %q", v)
	}
	m.items = append(m.items, transcriptItem{kind: itemUser, text: "hi"})
	m.syncTranscript()
	if strings.Contains(stripANSI(viewText(m)), "new run") {
		t.Fatal("empty state still shown after a line")
	}
}

func TestHelpOverlayDoesNotGrowTranscript(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	before := len(m.items)
	m = upd(m, tea.KeyPressMsg{Code: '?'})
	if !m.helpMode {
		t.Fatal("? did not open help")
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "enter send") {
		t.Fatalf("help overlay missing: %q", v)
	}
	if len(m.items) != before {
		t.Fatalf("help appended transcript items: %d -> %d", before, len(m.items))
	}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	if strings.Contains(stripANSI(viewText(m)), "q qui") {
		t.Fatalf("help clipped: %q", viewText(m))
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.helpMode {
		t.Fatal("esc did not close help")
	}
}

func TestEditDiffCard(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	diff := "@@ -1,1 +1,1 @@\n-old\n+new line"
	meta := `{"diff":` + "`" + diff + "`" + `}`
	// valid JSON
	meta = `{"diff":"@@ -1,1 +1,1 @@\n-old\n+new line"}`
	path := "main.go"
	tc := db.ToolCall{
		Tool: "edit", Status: "completed", Title: &path,
		InputJSON: `{"filePath":"main.go","oldString":"old","newString":"new line"}`,
		MetadataJSON: &meta,
	}
	m.items = append(m.items, transcriptItem{kind: itemTool, collapsed: true, tool: tc})
	m.syncTranscript()
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "main.go") {
		t.Fatalf("collapsed edit missing path: %q", v)
	}
	if strings.Contains(v, "@@") {
		t.Fatalf("collapsed edit showed diff: %q", v)
	}
	m = m.toggleLastTool()
	v = stripANSI(viewText(m))
	if !strings.Contains(v, "@@") || !strings.Contains(v, "new line") {
		t.Fatalf("expanded edit missing diff: %q", v)
	}
}

func TestWriteCardHidesFullBody(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	body := strings.Repeat("hello world ", 80)
	path := "note.txt"
	tc := db.ToolCall{
		Tool: "write", Status: "completed", Title: &path,
		InputJSON: `{"filePath":"note.txt","contents":"` + body + `"}`,
		Output:    &body,
	}
	m.items = append(m.items, transcriptItem{kind: itemTool, collapsed: true, tool: tc})
	m.syncTranscript()
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "note.txt") {
		t.Fatalf("collapsed write missing path: %q", v)
	}
	if strings.Contains(v, body) {
		t.Fatalf("collapsed write showed full contents")
	}
}

func TestFilePickerInsertsPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: dir})
	m = typeText(m, "@hel")
	if !m.filePickerMode {
		t.Fatal("@ did not open file picker")
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "hello.go") {
		t.Fatalf("picker missing hello.go: %q", v)
	}
	m = upd(m, enter())
	if m.filePickerMode {
		t.Fatal("picker still open after enter")
	}
	if !strings.Contains(m.prompt.Value(), "hello.go") {
		t.Fatalf("prompt missing path: %q", m.prompt.Value())
	}
}

func TestFilePickerEsc(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: dir})
	m = typeText(m, "@")
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.filePickerMode {
		t.Fatal("esc did not close file picker")
	}
	if m.busy {
		t.Fatal("esc submitted")
	}
}

func TestPaletteUsedForUserText(t *testing.T) {
	if theme.Accent == "" || userStyle.GetForeground() == nil {
		t.Fatal("theme accent or user style foreground is unset")
	}
}

func TestSessionPickerEmptyList(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = typeText(m, "/sessions")
	m = upd(m, enter())
	if !m.sessionPickerMode {
		t.Fatal("session picker not open")
	}
	if !strings.Contains(stripANSI(viewText(m)), "no sessions") {
		t.Errorf("empty picker missing no sessions: %q", viewText(m))
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.sessionPickerMode {
		t.Fatal("empty picker not dismissible")
	}
}
