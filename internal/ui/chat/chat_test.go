package chat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
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
	for _, want := range []string{roleYou, "hello there", roleAssistant, "hi back", thinkingLabel, "bash", "echo hello"} {
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
	for _, want := range []string{"bash", "echo hello"} {
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
	for _, want := range []string{roleAssistant, "print(\"Hello, World!\")", "How to run it:", "hello.py"} {
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
	if !strings.Contains(v, "bash") {
		t.Errorf("want tool card bash, got %q", v)
	}
	if status := lastToolStatus(m); status != "denied" {
		t.Errorf("tool status = %q, want denied", status)
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
	if !strings.Contains(v, "bash") {
		t.Errorf("want tool card bash, got %q", v)
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
	if !strings.Contains(v, "question") {
		t.Errorf("want tool card question, got %q", v)
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
	m = p.drainUntil(m, "bash")
	if !m.busy {
		t.Fatal("busy cleared before the turn finished")
	}
	v := stripANSI(viewText(m))
	if strings.Contains(v, "done") && !m.busy {
		t.Fatalf("final text arrived before pending tool card: %q", v)
	}
	m = p.drainIdle(m)
	v = stripANSI(viewText(m))
	if !strings.Contains(v, "bash") || !strings.Contains(v, "hello") {
		t.Errorf("tool card missing: %q", v)
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
	if status := lastToolStatus(m); status != "denied" {
		t.Errorf("confirm esc should deny, status = %q view = %q", status, v)
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

func TestReplayRestoresTokensFromStepFinish(t *testing.T) {
	st := newTestStore(t)
	sess, err := st.CreateSession(context.Background(), db.Session{Title: "t", Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	am, err := st.InsertMessage(context.Background(), db.Message{SessionID: sess.ID, Role: "assistant"})
	if err != nil {
		t.Fatal(err)
	}
	total := int64(4321)
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: am.ID, Type: "step-finish", TokensTotal: &total}); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: t.TempDir(), Session: &sess})
	if m.tokensUsed != 4321 {
		t.Fatalf("tokensUsed = %d, want 4321 after replay", m.tokensUsed)
	}
}

func TestReplayRestoresCacheHitMiss(t *testing.T) {
	st := newTestStore(t)
	sess, err := st.CreateSession(context.Background(), db.Session{Title: "t", Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	am, err := st.InsertMessage(context.Background(), db.Message{SessionID: sess.ID, Role: "assistant"})
	if err != nil {
		t.Fatal(err)
	}
	in, hit := int64(68790), int64(68000)
	if _, err := st.InsertPart(context.Background(), db.Part{
		MessageID: am.ID, Type: "step-finish", TokensInput: &in, TokensCacheRead: &hit, TokensTotal: ptrInt64(69000),
	}); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: t.TempDir(), Session: &sess})
	if m.cacheHit != 68000 || m.cacheMiss != 790 {
		t.Fatalf("cache hit/miss = %d/%d, want 68000/790", m.cacheHit, m.cacheMiss)
	}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 24})
	m = mm.(Model)
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "hit 68k") || !strings.Contains(v, "miss 790") {
		t.Fatalf("footer missing cache stats: %q", v)
	}
}

func TestModelsMsgRestoresMissingSessionCost(t *testing.T) {
	st := newTestStore(t)
	sess, err := st.CreateSession(context.Background(), db.Session{Title: "t", Directory: t.TempDir(), Model: "deepseek-v4-flash"})
	if err != nil {
		t.Fatal(err)
	}
	am, err := st.InsertMessage(context.Background(), db.Message{SessionID: sess.ID, Role: "assistant"})
	if err != nil {
		t.Fatal(err)
	}
	in, out := int64(1_000_000), int64(1_000_000)
	if _, err := st.InsertPart(context.Background(), db.Part{
		MessageID: am.ID, Type: "step-finish", TokensInput: &in, TokensOutput: &out, TokensTotal: ptrInt64(2_000_000),
	}); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: t.TempDir(), Session: &sess})
	if m.sessionCost != 0 {
		t.Fatalf("sessionCost = %v, want 0 before model prices load", m.sessionCost)
	}
	mm, _ := m.Update(modelsMsg{list: []string{"deepseek-v4-flash"}, infos: []modelscache.Info{{
		ID: "deepseek-v4-flash", InputPerM: 0.14, OutputPerM: 0.28,
	}}})
	m = mm.(Model)
	if m.sessionCost < 0.4 || m.sessionCost > 0.45 {
		t.Fatalf("sessionCost = %v, want ~0.42 after models load", m.sessionCost)
	}
	mm, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 24})
	m = mm.(Model)
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "$0.420") && !strings.Contains(v, "$0.42") {
		t.Fatalf("footer missing restored cost: %q", v)
	}
}

func TestCacheMissTokens(t *testing.T) {
	if got := cacheMissTokens(68790, 68000); got != 790 {
		t.Fatalf("included cache: miss = %d, want 790", got)
	}
	if got := cacheMissTokens(1000, 1000); got != 0 {
		t.Fatalf("full hit: miss = %d, want 0", got)
	}
	if got := cacheMissTokens(790, 68000); got != 790 {
		t.Fatalf("separate input: miss = %d, want 790", got)
	}
	if got := cacheMissTokens(1000, 0); got != 1000 {
		t.Fatalf("no cache: miss = %d, want 1000", got)
	}
}

func TestReplayEstimatesTokensWithoutUsage(t *testing.T) {
	st := newTestStore(t)
	sess, err := st.CreateSession(context.Background(), db.Session{Title: "t", Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	um, err := st.InsertMessage(context.Background(), db.Message{SessionID: sess.ID, Role: "user"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Repeat("abcd", 50)
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: um.ID, Type: "text", Text: &text}); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: t.TempDir(), Session: &sess})
	if m.tokensUsed <= 0 {
		t.Fatalf("tokensUsed = %d, want an estimate from stored text", m.tokensUsed)
	}
}

func TestRefreshWritesModelsCache(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "models.json")
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: dir, CachePath: cachePath})
	msg := m.refreshModels()
	got, ok := msg.(modelsMsg)
	if !ok {
		t.Fatalf("refreshModels returned %T", msg)
	}
	if got.err != nil {
		t.Fatalf("refreshModels err = %v", got.err)
	}
	infos, _, err := modelscache.Load(cachePath, time.Now(), modelscache.DefaultTTL)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(infos) < 1 {
		t.Fatalf("cache not written: %+v", infos)
	}
	if _, ok := modelscache.InfoOf(infos, "deepseek-v4-flash"); !ok {
		t.Fatalf("cache missing deepseek-v4-flash: %+v", infos)
	}
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"cache_write_per_million"`) {
		t.Fatalf("cache missing cache_write_per_million:\n%s", raw)
	}
	flash, ok := modelscache.InfoOf(infos, "deepseek-v4-flash")
	if !ok || !strings.HasSuffix(flash.Endpoint, "/chat/completions") {
		t.Fatalf("go model endpoint = %+v", flash)
	}
}

func TestRefreshStampsZenEndpoint(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "models.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/zen/go/v1/models"):
			fmt.Fprint(w, `{"data":[{"id":"deepseek-v4-flash"}]}`)
		case strings.HasSuffix(r.URL.Path, "/zen/v1/models"):
			if r.Header.Get("Authorization") == "" {
				t.Error("zen models request missing Authorization")
			}
			fmt.Fprint(w, `{"data":[{"id":"deepseek-v4-flash-free"},{"id":"deepseek-v4-flash"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c := opencode.NewClient("test-key", opencode.WithBaseURL(srv.URL+"/zen/go/v1"))
	m := New(Options{Store: newTestStore(t), Client: c, Workdir: dir, CachePath: cachePath})
	msg := m.refreshModels()
	got, ok := msg.(modelsMsg)
	if !ok {
		t.Fatalf("refreshModels returned %T", msg)
	}
	if got.err != nil {
		t.Fatalf("refreshModels err = %v", got.err)
	}
	infos, _, err := modelscache.Load(cachePath, time.Now(), modelscache.DefaultTTL)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	flash, ok := modelscache.InfoOf(infos, "deepseek-v4-flash")
	if !ok || flash.Endpoint != srv.URL+"/zen/go/v1/chat/completions" {
		t.Fatalf("go model = %+v", flash)
	}
	free, ok := modelscache.InfoOf(infos, "deepseek-v4-flash-free")
	if !ok || free.Endpoint != srv.URL+"/zen/v1/chat/completions" {
		t.Fatalf("zen free model = %+v", free)
	}
}

func TestModelEndpointPrefersCacheThenFreeFallback(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.model = "deepseek-v4-flash-free"
	m.modelInfos = []modelscache.Info{{
		ID:       "deepseek-v4-flash-free",
		Endpoint: "https://opencode.ai/zen/v1/chat/completions",
	}}
	if got := m.modelEndpoint(); got != "https://opencode.ai/zen/v1/chat/completions" {
		t.Fatalf("cached endpoint = %q", got)
	}

	m.modelInfos = nil
	m.client = opencode.NewClient("k", opencode.WithBaseURL("https://opencode.ai/zen/go/v1"))
	if got := m.modelEndpoint(); got != "https://opencode.ai/zen/v1/chat/completions" {
		t.Fatalf("free fallback = %q", got)
	}

	m.model = "deepseek-v4-flash"
	if got := m.modelEndpoint(); got != "https://opencode.ai/zen/go/v1/chat/completions" {
		t.Fatalf("go fallback = %q", got)
	}
}

func TestLiveTextPaintsInstantly(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.busy = true
	full := "abcdefghijklmnopqrstuvwxyz"
	m.applyPart(db.Part{Type: "text", Text: &full})
	if got := m.items[len(m.items)-1].text; got != full {
		t.Fatalf("live text = %q, want instant full text", got)
	}
}

func TestLiveActivitySitsAbovePrompt(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.busy = true
	m.activity = "thinking"
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "thinking") {
		t.Fatalf("live thinking missing above the prompt: %q", v)
	}
	if !strings.Contains(v, "enter send") {
		t.Fatalf("idle hint missing while busy: %q", v)
	}
	thinkAt, promptAt := -1, -1
	for i, line := range strings.Split(v, "\n") {
		if thinkAt < 0 && strings.Contains(line, "thinking") && !strings.Contains(line, "enter") {
			thinkAt = i
		}
		if strings.Contains(line, "ask lazykoder") || strings.Contains(line, "╭") && promptAt < 0 && i > 2 {
			if strings.Contains(line, "╭") {
				promptAt = i
			}
		}
	}
	if thinkAt < 0 || promptAt < 0 || thinkAt >= promptAt {
		t.Fatalf("thinking should sit above the input box: think=%d prompt=%d\n%s", thinkAt, promptAt, v)
	}
	if promptAt-thinkAt < 2 {
		t.Fatalf("need a blank row between thinking and the input box: think=%d prompt=%d\n%s", thinkAt, promptAt, v)
	}
}

func TestTokensDoNotResetOnSmallerUsage(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	high := int64(8000)
	low := int64(12)
	m.applyPart(db.Part{Type: "step-finish", TokensTotal: &high})
	if m.tokensUsed != 8000 {
		t.Fatalf("tokensUsed = %d, want 8000", m.tokensUsed)
	}
	m.applyPart(db.Part{Type: "step-finish", TokensTotal: &low})
	if m.tokensUsed != 8000 {
		t.Fatalf("tokensUsed reset to %d, want to keep 8000", m.tokensUsed)
	}
}

func TestComposerShowsCost(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.model = "deepseek-v4-flash"
	m.modelInfos = []modelscache.Info{{ID: "deepseek-v4-flash", Context: 1000000, InputPerM: 0.14, OutputPerM: 0.28}}
	in, out := int64(1_000_000), int64(1_000_000)
	m.applyPart(db.Part{Type: "step-finish", TokensInput: &in, TokensOutput: &out, TokensTotal: ptrInt64(2_000_000)})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = mm.(Model)
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "$0.420") && !strings.Contains(v, "$0.42") {
		t.Fatalf("footer missing session cost: %q", v)
	}
}

func TestComposerShowsZeroCostAfterUsage(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.model = "deepseek-v4-flash-free"
	m.modelInfos = []modelscache.Info{{ID: "deepseek-v4-flash-free", Free: true, Context: 200000}}
	in, miss := int64(511), int64(397)
	m.applyPart(db.Part{Type: "step-finish", TokensInput: &in, TokensTotal: &in, TokensCacheRead: ptrInt64(0)})
	m.cacheMiss = miss
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = mm.(Model)
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "hit 0") || !strings.Contains(v, "miss 397") {
		t.Fatalf("footer missing cache counts: %q", v)
	}
	if !strings.Contains(v, "$0.00") {
		t.Fatalf("footer missing zero cost after usage: %q", v)
	}
}

func TestApplyUsageEstimatesWhenStoredCostIsZero(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.model = "deepseek-v4-flash"
	m.modelInfos = []modelscache.Info{{ID: "deepseek-v4-flash", InputPerM: 0.14, OutputPerM: 0.28}}
	zero := 0.0
	in, out := int64(1_000_000), int64(1_000_000)
	m.applyPart(db.Part{Type: "step-finish", TokensInput: &in, TokensOutput: &out, TokensTotal: ptrInt64(2_000_000), Cost: &zero})
	if m.sessionCost < 0.4 || m.sessionCost > 0.45 {
		t.Fatalf("sessionCost = %v, want list-price estimate ~0.42", m.sessionCost)
	}
}

func TestApplyUsagePrefersStoredCost(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.model = "deepseek-v4-flash-free"
	m.modelInfos = []modelscache.Info{{ID: "deepseek-v4-flash-free", Free: true}}
	cost := 0.15
	in := int64(1000)
	m.applyPart(db.Part{Type: "step-finish", TokensInput: &in, TokensTotal: &in, Cost: &cost})
	if m.sessionCost < 0.149 || m.sessionCost > 0.151 {
		t.Fatalf("sessionCost = %v, want stored API cost 0.15", m.sessionCost)
	}
}

func ptrInt64(n int64) *int64 { return &n }

func TestTokensPerSecUsesGeneratedNotSessionTotal(t *testing.T) {
	if got := tokensPerSec(80, time.Second); got != 80 {
		t.Fatalf("tokensPerSec(80, 1s) = %v, want 80", got)
	}
	if got := tokensPerSec(16000, 8*time.Second); got != 2000 {
		t.Fatalf("sanity: 16000/8s = %v, want 2000 (this must not be used for the footer)", got)
	}
	if got := tokensPerSec(80, 20*time.Millisecond); got != 0 {
		t.Fatalf("sub-50ms elapsed should not invent tps, got %v", got)
	}
	if got := tokensPerSec(0, time.Second); got != 0 {
		t.Fatalf("zero generated tokens should not invent tps, got %v", got)
	}

	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.busy = true
	m.turnStarted = time.Now().Add(-time.Second)
	m.tokensUsed = 16000
	out := int64(80)
	m.applyPart(db.Part{Type: "step-finish", TokensOutput: &out, TokensTotal: ptrInt64(16000)})
	if m.turnGenTokens != 80 {
		t.Fatalf("turnGenTokens = %d, want 80", m.turnGenTokens)
	}
	m = m.finishTurn(nil)
	if m.tokensPerSec < 70 || m.tokensPerSec > 90 {
		t.Fatalf("tps = %v, want ~80 from 80 output tokens in 1s, not session 16000", m.tokensPerSec)
	}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = mm.(Model)
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "80 tps") && !strings.Contains(v, "79 tps") && !strings.Contains(v, "81 tps") {
		t.Fatalf("footer missing turn tps: %q", v)
	}
}

func TestComposerShowsContext(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.model = "deepseek-v4-flash"
	m.modelInfos = []modelscache.Info{{ID: "deepseek-v4-flash", Context: 128000}}
	m.tokensUsed = 1200
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "deepseek-v4-flash") || !strings.Contains(v, "1.2k/128k") {
		t.Fatalf("footer missing context: %q", v)
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
	when := time.Date(2026, 8, 16, 15, 32, 5, 0, time.Local).UnixMilli()
	m.items = append(m.items, transcriptItem{kind: itemUser, text: "hello there", when: when})
	m.syncTranscript()
	v := stripANSI(viewText(m))
	stamp := formatClock(when)
	if !strings.Contains(v, roleYou) || !strings.Contains(v, stamp) {
		t.Fatalf("user turn missing timestamp %q: %q", stamp, v)
	}
	if !stampOnRight(v, roleYou, stamp) {
		t.Fatalf("timestamp not right-aligned on the you row: %q", v)
	}
}

func TestThinkingTimestampIsRightAligned(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	when := time.Date(2026, 8, 16, 15, 32, 5, 0, time.Local).UnixMilli()
	m.items = append(m.items, transcriptItem{
		kind: itemReasoning, text: "secret", collapsed: true, when: when,
	})
	m.syncTranscript()
	v := stripANSI(viewText(m))
	if !stampOnRight(v, thinkingLabel, formatClock(when)) {
		t.Fatalf("thinking timestamp not right-aligned: %q", v)
	}
}

func TestToolHeaderAlignsClockWithoutStatus(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	when := time.Date(2026, 8, 16, 15, 32, 5, 0, time.Local).UnixMilli()
	title := "echo hello"
	m.items = append(m.items, transcriptItem{
		kind: itemTool, collapsed: true, when: when,
		tool: db.ToolCall{Tool: "bash", Status: "completed", Title: &title},
	})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm.(Model)
	stamp := formatClock(when)
	var row string
	for _, line := range strings.Split(stripANSI(viewText(m)), "\n") {
		if strings.Contains(line, "bash") {
			row = strings.TrimRight(line, " ")
			break
		}
	}
	if row == "" {
		t.Fatalf("bash header missing: %q", viewText(m))
	}
	if !stampOnRight(row, "bash", stamp) {
		t.Fatalf("bash clock not right-aligned: %q", row)
	}
	if strings.Contains(row, "completed") || strings.Contains(row, "Aug") || strings.Contains(row, "ago") {
		t.Fatalf("bash header still shows status or date: %q", row)
	}
}

func TestTimestampsRealignOnResize(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	when := time.Date(2026, 8, 16, 15, 32, 5, 0, time.Local).UnixMilli()
	m.items = append(m.items, transcriptItem{kind: itemReasoning, text: "secret", collapsed: true, when: when})
	m.syncTranscript()
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm.(Model)
	if !stampOnRight(stripANSI(viewText(m)), thinkingLabel, formatClock(when)) {
		t.Fatalf("thinking clock not right-aligned after resize: %q", viewText(m))
	}
}

func TestThinkingFrameUsesBrackets(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.busy = true
	m.pulseOn = true
	m.pulse = pulseSteps / 2
	title := "echo hello"
	m.items = append(m.items, transcriptItem{
		kind:      itemTool,
		collapsed: true,
		tool:      db.ToolCall{Tool: "bash", Status: "pending", Title: &title},
	})
	m.syncTranscript()
	raw := viewText(m)
	v := stripANSI(raw)
	if !strings.Contains(v, theme.StatusDiamond) {
		t.Fatalf("in-flight tool card missing status diamond: %q", v)
	}
	if !strings.Contains(v, "bash") {
		t.Fatalf("in-flight tool card missing bash: %q", v)
	}
	var header string
	for _, line := range strings.Split(v, "\n") {
		if strings.Contains(line, "lazykoder") {
			header = strings.TrimSpace(line)
			break
		}
	}
	if header == "" {
		t.Fatal("header missing")
	}
	if strings.HasPrefix(header, "[") || strings.HasSuffix(header, "]") {
		t.Fatalf("header still wrapped in thinking brackets: %q", header)
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
	if !strings.Contains(v, "enter") || !strings.Contains(v, "send") {
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

func TestHelpOverlayBordersAlign(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m.items = []transcriptItem{
		{kind: itemUser, text: "what is love baby don't hurt me 🎵"},
		{kind: itemAssistant, text: "Baby, don't hurt me\n🎶 Da-da-da"},
	}
	m.syncTranscript()
	m.helpMode = true
	v := stripANSI(viewText(m))
	lines := strings.Split(v, "\n")
	start, end := -1, -1
	for i, line := range lines {
		if start < 0 && strings.Contains(line, "╭") && lipgloss.Width(strings.TrimRight(line, " ")) < m.width-4 {
			start = i
		}
		if start >= 0 && strings.Contains(line, "╰") {
			end = i
			break
		}
	}
	if start < 0 || end <= start {
		t.Fatalf("help card borders missing: %q", v)
	}
	var lefts []int
	for _, line := range lines[start : end+1] {
		if i := strings.Index(line, "│"); i >= 0 {
			lefts = append(lefts, lipgloss.Width(line[:i]))
		}
	}
	if len(lefts) < 3 {
		t.Fatalf("not enough help rows to check alignment: %q", v)
	}
	for _, col := range lefts[1:] {
		if col != lefts[0] {
			t.Fatalf("help left border columns = %v\n%s", lefts, v)
		}
	}
	if strings.Contains(strings.Join(lines[start:end+1], "\n"), "ask lazykoder") {
		t.Fatalf("help card overlaps the prompt: %q", v)
	}
	if !strings.Contains(v, "switch model") || !strings.Contains(v, "ctrl+c") {
		t.Fatalf("help rows missing: %q", v)
	}
}

func TestSpliceDisplayUsesCellsNotRunes(t *testing.T) {
	dst := "🎶1234567890 leftover"
	src := "│keys│"
	got := stripANSI(spliceDisplay(dst, src, 10))
	if !strings.Contains(got, "│keys│") {
		t.Fatalf("src missing: %q", got)
	}
	if lipgloss.Width(got) < 10+lipgloss.Width(src) {
		t.Fatalf("spliced width %d too small: %q", lipgloss.Width(got), got)
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
		InputJSON:    `{"filePath":"main.go","oldString":"old","newString":"new line"}`,
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
