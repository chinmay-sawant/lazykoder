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

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/provider"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/tips"
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
	if !strings.Contains(v, "hello there") || !strings.Contains(v, "╭") {
		t.Fatalf("resumed session lost the user prompt box: %q", v)
	}
	if !lineHasPrefix(v, roleAssistant, workRail) && !lineHasPrefix(v, thinkingLabel, workRail) {
		t.Fatalf("resumed session lost the reply rail: %q", v)
	}
}

func lineHasPrefix(view, needle, prefix string) bool {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, needle) && strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return true
		}
	}
	return false
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
	m = m.toggleAllTools()
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
	if id := m2.SessionID(); id == "" {
		t.Fatal("second ctrl+c quit with empty SessionID after a started turn")
	} else if !strings.HasPrefix(id, "ses_") {
		t.Fatalf("SessionID = %q, want ses_ prefix", id)
	}
	deadline := time.Now().Add(5 * time.Second)
	for fake.requestCount() < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := fake.requestCount(); n != 2 {
		t.Errorf("agent did not finish after quit, provider calls = %d, want 2", n)
	}
}

func TestPromptCtrlCAndCtrlA(t *testing.T) {
	st := newTestStore(t)
	m := New(Options{Store: st, Client: deadClient(), Workdir: t.TempDir()})
	m = typeText(m, "hello world")

	// ctrl+c with unselected text in the input box clears the prompt (no copy).
	m, cmd := updCmd(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatalf("ctrl+c on unselected text returned a command: %v", cmd)
	}
	if got := m.prompt.Value(); got != "" {
		t.Fatalf("ctrl+c did not clear the prompt: %q", got)
	}
	if m.copyNotice != "" {
		t.Fatalf("copyNotice = %q, want empty", m.copyNotice)
	}
	if m.quitConfirm {
		t.Fatal("ctrl+c with text in the input box triggered quit confirmation")
	}

	// Re-enter text and select all with ctrl+a.
	m = typeText(m, "hello world")
	m, cmd = updCmd(m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatal("ctrl+a returned a command")
	}
	if !m.promptSelectAll {
		t.Fatal("ctrl+a did not select all")
	}
	if got := m.prompt.Value(); got != "hello world" {
		t.Fatalf("prompt value after ctrl+a = %q", got)
	}
	if !strings.Contains(viewText(m), "48;2;163;177;138") {
		t.Fatalf("select-all highlight missing from view: %q", viewText(m))
	}

	// ctrl+c after ctrl+a copies the prompt and keeps the text.
	m, cmd = updCmd(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !copyCmdContains(cmd, "hello world") {
		t.Fatal("ctrl+c after ctrl+a did not copy the prompt")
	}
	if m.copyNotice != "Text copied" {
		t.Fatalf("copyNotice = %q, want %q", m.copyNotice, "Text copied")
	}
	if got := m.prompt.Value(); got != "hello world" {
		t.Fatalf("ctrl+c after ctrl+a mutated prompt value = %q", got)
	}

	// Select-all then type replaces the whole draft.
	m, _ = updCmd(m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	m = upd(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.promptSelectAll {
		t.Fatal("typing did not clear the select-all state")
	}
	if got := m.prompt.Value(); got != "x" {
		t.Fatalf("select-all + type = %q, want %q", got, "x")
	}

	m.prompt.SetValue("hello world")
	m, _ = updCmd(m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := m.prompt.Value(); got != "" {
		t.Fatalf("select-all + backspace = %q, want empty", got)
	}
	if m.promptSelectAll {
		t.Fatal("select-all should clear after backspace")
	}

	m.prompt.SetValue("again")
	m = upd(m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if got := m.prompt.Value(); got != "" {
		t.Fatalf("prompt after ctrl+u = %q, want empty", got)
	}
	m, cmd = updCmd(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatalf("ctrl+c with an empty input box returned a command %v", cmd)
	}
	if !m.quitConfirm {
		t.Fatal("ctrl+c with an empty input box did not show quit confirmation")
	}
}

func copyCmdContains(cmd tea.Cmd, want string) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, sub := range batch {
			if fmt.Sprint(sub()) == want {
				return true
			}
		}
		return false
	}
	return fmt.Sprint(msg) == want
}

func TestJumpBarShowsWhenScrolledUp(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	for i := 0; i < 60; i++ {
		m.items = append(m.items, transcriptItem{kind: itemNote, text: fmt.Sprintf("line %02d", i)})
	}
	m.syncTranscript()
	if m.jumpBarVisible() {
		t.Fatal("jump bar visible while at the bottom")
	}
	mm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = mm.(Model)
	if !m.jumpBarVisible() {
		t.Fatal("jump bar not visible after scrolling up")
	}
	v := viewText(m)
	if !strings.Contains(v, jumpDownArrow) {
		t.Fatalf("jump bar arrow missing from view: %q", v)
	}
	row := viewLineIndex(m, jumpDownArrow)
	if row != m.jumpBarRow() {
		t.Errorf("jump bar at row %d, want %d", row, m.jumpBarRow())
	}
}

func TestJumpBarHiddenWithoutOverflow(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.items = append(m.items, transcriptItem{kind: itemNote, text: "short"})
	m.syncTranscript()
	if m.jumpBarVisible() {
		t.Fatal("jump bar visible without transcript overflow")
	}
	if strings.Contains(viewText(m), jumpDownArrow) {
		t.Fatal("jump bar arrow rendered without overflow")
	}
}

func TestScrollPositionPreservedOnNewContent(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	for i := 0; i < 60; i++ {
		m.items = append(m.items, transcriptItem{kind: itemNote, text: fmt.Sprintf("line %02d", i)})
	}
	m.syncTranscript()
	m.transcript.SetYOffset(10)
	if m.transcript.AtBottom() {
		t.Fatal("test setup: viewport should be scrolled up")
	}
	m.items = append(m.items, transcriptItem{kind: itemNote, text: "newest"})
	m.syncTranscript()
	if m.transcript.AtBottom() {
		t.Fatal("new content yanked the scrolled-up view to the bottom")
	}
	if got := m.transcript.YOffset(); got != 10 {
		t.Errorf("offset after new content = %d, want 10", got)
	}
	if !m.jumpBarVisible() {
		t.Fatal("jump bar should offer the way down after new content while scrolled up")
	}
}

func TestAlertRowCopyNotice(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = typeText(m, "hello world")
	m, _ = updCmd(m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	m, cmd := updCmd(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !copyCmdContains(cmd, "hello world") {
		t.Fatalf("ctrl+c after ctrl+a did not copy the prompt: %v", cmd)
	}
	v := viewText(m)
	if !strings.Contains(v, "38;2;143;191;143") {
		t.Fatalf("copy alert is not green: %q", v)
	}
	if !strings.Contains(stripANSI(v), "Text copied") {
		t.Fatalf("copy alert missing from view: %q", v)
	}
	alertRow := viewLineIndex(m, "Text copied")
	wantRow := m.transcriptTop() + m.transcriptRenderHeight()
	if alertRow != wantRow {
		t.Errorf("copy alert on row %d, want the alert row %d", alertRow, wantRow)
	}
	if strings.Contains(stripANSI(m.promptLine()), "Text copied") {
		t.Fatal("copy alert still rendered inside the input box")
	}
}

func TestTipsShowWhenIdle(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm.(Model)
	v := stripANSI(viewText(m))
	if !strings.Contains(v, tips.At(0)) {
		t.Fatalf("idle tip missing from view: %q", v)
	}
	if !strings.Contains(v, tipLabel) {
		t.Fatalf("bold TIP label missing from view: %q", v)
	}
	row := viewLineIndex(m, tips.At(0))
	wantRow := m.transcriptTop() + m.transcriptRenderHeight()
	if row != wantRow {
		t.Errorf("tip on row %d, want the alert row %d", row, wantRow)
	}
	if row != viewLineIndex(m, tipLabel) {
		t.Errorf("TIP label and tip text are not on the same alert row")
	}

	m.busy = true
	if strings.Contains(stripANSI(viewText(m)), tips.At(0)) {
		t.Fatal("tip still shown while busy")
	}
	m.busy = false

	m = typeText(m, "hello world")
	m, _ = updCmd(m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	m, cmd := updCmd(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !copyCmdContains(cmd, "hello world") {
		t.Fatal("copy failed")
	}
	if strings.Contains(stripANSI(viewText(m)), tips.At(0)) {
		t.Fatal("tip still shown while the copy alert is up")
	}
}

func TestTipsRotateOnTick(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm0, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm0.(Model)
	if got := m.tipsIndex; got != 0 {
		t.Fatalf("tipsIndex = %d, want 0", got)
	}
	mm, cmd := m.Update(tipsTickMsg{})
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("tips tick did not re-arm")
	}
	if got := m.tipsIndex; got != 1 {
		t.Fatalf("tipsIndex after tick = %d, want 1", got)
	}
	if !strings.Contains(stripANSI(viewText(m)), tips.At(1)) {
		t.Fatalf("next tip missing after tick: %q", viewText(m))
	}
	for i := 0; i < len(tips.All)*3; i++ {
		mm, _ = m.Update(tipsTickMsg{})
		m = mm.(Model)
	}
	if !strings.Contains(stripANSI(viewText(m)), tips.At(m.tipsIndex)) {
		t.Fatalf("tip after cycling does not match tipsIndex %d", m.tipsIndex)
	}
	if want := (1 + len(tips.All)*3) % len(tips.All); m.tipsIndex%len(tips.All) != want {
		t.Errorf("tipsIndex %d does not wrap to %d", m.tipsIndex, want)
	}
}

func TestAlertRowQuitWarning(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m, cmd := updCmd(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatalf("first ctrl+c returned a command %v", cmd)
	}
	if !m.quitConfirm {
		t.Fatal("first ctrl+c did not arm the quit confirmation")
	}
	v := viewText(m)
	if strings.Contains(stripANSI(v), "close lazykoder") {
		t.Fatalf("full-screen quit card still rendered: %q", v)
	}
	if !strings.Contains(stripANSI(v), "ctrl+c again to quit") {
		t.Fatalf("quit warning missing from view: %q", v)
	}
	if !strings.Contains(v, "38;2;209;122;122") {
		t.Fatalf("quit warning is not red: %q", v)
	}
	alertRow := viewLineIndex(m, "ctrl+c again to quit")
	wantRow := m.transcriptTop() + m.transcriptRenderHeight()
	if alertRow != wantRow {
		t.Errorf("quit warning on row %d, want the alert row %d", alertRow, wantRow)
	}
	m, cmd = updCmd(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil || cmd() != (tea.QuitMsg{}) {
		t.Fatalf("second ctrl+c did not quit: %v", cmd)
	}
}

func TestPromptCtrlArrowWordMovement(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = typeText(m, "hello world foo")

	m = upd(m, tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl})
	m = typeText(m, "X")
	if got := m.prompt.Value(); got != "hello world Xfoo" {
		t.Fatalf("ctrl+left then X = %q, want %q", got, "hello world Xfoo")
	}

	m = upd(m, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl})
	m = typeText(m, "Y")
	if got := m.prompt.Value(); got != "hello world XfooY" {
		t.Fatalf("ctrl+right then Y = %q, want %q", got, "hello world XfooY")
	}
}

func TestPromptCtrlUpDownLineMovement(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = typeText(m, "line one")
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	m = typeText(m, "line two")
	if m.prompt.LineCount() != 2 {
		t.Fatalf("prompt lines = %d, want 2", m.prompt.LineCount())
	}

	m = upd(m, tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModCtrl})
	m = typeText(m, "X")
	if got := m.prompt.Value(); got != "line oneX\nline two" {
		t.Fatalf("ctrl+up then X = %q, want %q", got, "line oneX\nline two")
	}

	m = upd(m, tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModCtrl})
	m = typeText(m, "Y")
	if got := m.prompt.Value(); got != "line oneX\nline twoY" {
		t.Fatalf("ctrl+down then Y = %q, want %q", got, "line oneX\nline twoY")
	}
}

func TestPromptCtrlHomeEndNav(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = typeText(m, "hello world")

	m = upd(m, tea.KeyPressMsg{Code: tea.KeyHome, Mod: tea.ModCtrl})
	m = typeText(m, "X")
	if got := m.prompt.Value(); got != "Xhello world" {
		t.Fatalf("ctrl+home then X = %q, want %q", got, "Xhello world")
	}

	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	m = typeText(m, "Y")
	if got := m.prompt.Value(); got != "Xhello worldY" {
		t.Fatalf("ctrl+end then Y = %q, want %q", got, "Xhello worldY")
	}
}

func TestPromptHomeEndNav(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = typeText(m, "hello")

	m = upd(m, tea.KeyPressMsg{Code: tea.KeyHome})
	m = typeText(m, "X")
	if got := m.prompt.Value(); got != "Xhello" {
		t.Fatalf("home then X = %q, want %q", got, "Xhello")
	}

	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnd})
	m = typeText(m, "Y")
	if got := m.prompt.Value(); got != "XhelloY" {
		t.Fatalf("end then Y = %q, want %q", got, "XhelloY")
	}
}

func TestPromptHomeEndScrollTranscriptWhenEmpty(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	for i := 0; i < 60; i++ {
		m.items = append(m.items, transcriptItem{kind: itemNote, text: fmt.Sprintf("line %02d", i)})
	}
	m.syncTranscript()
	m.transcript.SetYOffset(10)

	m = upd(m, tea.KeyPressMsg{Code: tea.KeyHome})
	if !m.transcript.AtTop() {
		t.Fatal("home with an empty prompt did not scroll the transcript to top")
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnd})
	if !m.transcript.AtBottom() {
		t.Fatal("end with an empty prompt did not scroll the transcript to bottom")
	}
}

func TestAlertRowHoldsJumpBarAndAlert(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	for i := 0; i < 60; i++ {
		m.items = append(m.items, transcriptItem{kind: itemNote, text: fmt.Sprintf("line %02d", i)})
	}
	m.syncTranscript()
	mm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = mm.(Model)
	m = typeText(m, "hello")
	m, _ = updCmd(m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	m, _ = updCmd(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !m.jumpBarVisible() {
		t.Fatal("jump bar should still be visible while scrolled up")
	}
	row := m.transcriptTop() + m.transcriptRenderHeight()
	lines := strings.Split(stripANSI(viewText(m)), "\n")
	if row >= len(lines) {
		t.Fatalf("alert row %d out of range", row)
	}
	if !strings.Contains(lines[row], jumpDownArrow) {
		t.Errorf("jump arrow missing from alert row %q", lines[row])
	}
	if !strings.Contains(lines[row], "Text copied") {
		t.Errorf("copy alert missing from alert row %q", lines[row])
	}
}

func TestBusyEnterForceSends(t *testing.T) {
	// While a turn is in flight, enter with a draft interrupts and sends.
	fake := newFakeProvider(t, 0,
		respBody("ok", "stop", nil),
		respBody("ok2", "stop", nil),
	)
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
	// Still busy (or drain until busy): type a second message and force-send.
	if !m.busy {
		// First turn may have finished already; re-busy for the force-send path.
		m.busy = true
		m.turnCancel = func() {}
		m.activity = "thinking"
	}
	m = typeText(m, "second")
	m, cmd2 := updCmd(m, enter())
	if cmd2 == nil {
		t.Fatal("Enter while busy with draft should force send")
	}
	p.run(cmd2)
	m = p.drainIdle(m)
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "second") {
		t.Errorf("force-sent message missing from view: %q", v)
	}
	if !strings.Contains(v, "interrupted") && !strings.Contains(v, "second") {
		t.Errorf("view after force send: %q", v)
	}
}

func TestModelsFetchedOnStartup(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	p := newPump(t)
	p.run(m.Init())

	m = p.runStep(m, p.next())
	v := statusDrawerText(m)
	if !strings.Contains(v, "deepseek-v4-flash") {
		t.Errorf("status drawer missing current model label: %q", v)
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
	// InsertMessage bumps the conversation timestamp; build older first, then
	// newer, so the resume list keeps newer on top.
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
	time.Sleep(2 * time.Millisecond)
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

func TestSessionPickerCardAlignsAndShowsScrollbar(t *testing.T) {
	dir := t.TempDir()
	st := newTestStore(t)
	for i := 0; i < 25; i++ {
		sess, err := st.CreateSession(context.Background(), db.Session{
			Title: fmt.Sprintf("session-%02d-%s", i, strings.Repeat("x", 40)), Directory: dir,
			TimeCreated: int64(i), TimeUpdated: int64(i),
		})
		if err != nil {
			t.Fatalf("create session %d: %v", i, err)
		}
		um, err := st.InsertMessage(context.Background(), db.Message{SessionID: sess.ID, Role: "user"})
		if err != nil {
			t.Fatalf("insert message %d: %v", i, err)
		}
		text := fmt.Sprintf("msg-%02d", i)
		if _, err := st.InsertPart(context.Background(), db.Part{MessageID: um.ID, Type: "text", Text: &text}); err != nil {
			t.Fatalf("insert part %d: %v", i, err)
		}
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: dir})
	m = typeText(m, "/resume")
	m = upd(m, enter())
	if !m.sessionPickerMode {
		t.Fatal("resume picker not open")
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "RUNS") {
		t.Fatalf("picker title missing: %q", v)
	}
	lines := strings.Split(v, "\n")
	start, end := -1, -1
	for i, line := range lines {
		if start < 0 && strings.Contains(line, "╭") {
			start = i
		}
		if start >= 0 && strings.Contains(line, "╰") {
			end = i
			break
		}
	}
	if start < 0 || end <= start {
		t.Fatalf("picker card borders missing: %q", v)
	}
	cardW := lipgloss.Width(strings.TrimRight(lines[start], " "))
	for _, line := range lines[start : end+1] {
		if w := lipgloss.Width(strings.TrimRight(line, " ")); w != cardW {
			t.Errorf("picker card row width %d, want %d: %q", w, cardW, line)
		}
	}
	if !strings.Contains(v, "░") && !strings.Contains(v, "█") {
		t.Errorf("picker scrollbar missing with overflow: %q", v)
	}
	if !strings.Contains(v, "…") {
		t.Errorf("long session titles should truncate to one line with an ellipsis: %q", v)
	}
}

func TestSessionPickerCardUsesMostOfTheScreen(t *testing.T) {
	dir := t.TempDir()
	st := newTestStore(t)
	for i := 0; i < 40; i++ {
		if _, err := st.CreateSession(context.Background(), db.Session{
			Title: fmt.Sprintf("tall-%02d", i), Directory: dir,
			TimeCreated: int64(i), TimeUpdated: int64(i),
		}); err != nil {
			t.Fatalf("create session %d: %v", i, err)
		}
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: dir})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 50})
	m = mm.(Model)
	m = typeText(m, "/resume")
	m = upd(m, enter())
	if !m.sessionPickerMode {
		t.Fatal("resume picker not open")
	}
	v := stripANSI(viewText(m))
	start, end := -1, -1
	for i, line := range strings.Split(v, "\n") {
		if start < 0 && strings.Contains(line, "╭") {
			start = i
		}
		if start >= 0 && strings.Contains(line, "╰") {
			end = i
			break
		}
	}
	if start < 0 || end <= start {
		t.Fatalf("picker card borders missing: %q", v)
	}
	cardH := end - start + 1
	want := 50 * sessionCardHeightPct / percentBase
	if cardH < want-1 {
		t.Fatalf("resume card height %d, want about %d (80%% of 50): %q", cardH, want, v)
	}
	if m.sessionVPHeight() <= 20 {
		t.Fatalf("list height %d still looks capped at 20", m.sessionVPHeight())
	}
}

func TestSessionPickerFlattensMultilineTitles(t *testing.T) {
	dir := t.TempDir()
	st := newTestStore(t)
	if _, err := st.CreateSession(context.Background(), db.Session{
		Title: "line one\n| Path | Purpose |\nline three", Directory: dir,
		TimeCreated: 1, TimeUpdated: 1,
	}); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: dir})
	m = typeText(m, "/resume")
	m = upd(m, enter())
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "line one | Path") {
		t.Fatalf("flattened title missing from picker: %q", v)
	}
	for _, line := range strings.Split(v, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "| Path | Purpose |" || trimmed == "line three" {
			t.Fatalf("multiline title leaked onto its own row: %q", v)
		}
	}
	if m.sessionContentRows() != 2 {
		t.Fatalf("content rows = %d, want 2 (one group + one session)", m.sessionContentRows())
	}
}

func TestSessionPickerClickOpensSession(t *testing.T) {
	dir := t.TempDir()
	st := newTestStore(t)
	newer, err := st.CreateSession(context.Background(), db.Session{
		Title: "click-newer", Directory: dir, TimeCreated: 2000, TimeUpdated: time.Now().UnixMilli(),
	})
	if err != nil {
		t.Fatalf("create newer: %v", err)
	}
	nm, err := st.InsertMessage(context.Background(), db.Message{SessionID: newer.ID, Role: "user"})
	if err != nil {
		t.Fatalf("insert newer user: %v", err)
	}
	newText := "newer-click-text"
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: nm.ID, Type: "text", Text: &newText}); err != nil {
		t.Fatalf("insert newer part: %v", err)
	}
	older, err := st.CreateSession(context.Background(), db.Session{
		Title: "click-older", Directory: dir, TimeCreated: 1000, TimeUpdated: 1000,
	})
	if err != nil {
		t.Fatalf("create older: %v", err)
	}
	om, err := st.InsertMessage(context.Background(), db.Message{SessionID: older.ID, Role: "user"})
	if err != nil {
		t.Fatalf("insert older user: %v", err)
	}
	oldText := "older-click-text"
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: om.ID, Type: "text", Text: &oldText}); err != nil {
		t.Fatalf("insert older part: %v", err)
	}

	m := New(Options{Store: st, Client: deadClient(), Workdir: dir})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m = typeText(m, "/resume")
	m = upd(m, enter())
	if !m.sessionPickerMode {
		t.Fatal("resume picker not open")
	}
	v := stripANSI(viewText(m))
	olderRow := -1
	for i, line := range strings.Split(v, "\n") {
		if strings.Contains(line, "click-older") {
			olderRow = i
			break
		}
	}
	if olderRow < 0 {
		t.Fatalf("older session row not rendered: %q", v)
	}

	m = upd(m, tea.MouseClickMsg(tea.Mouse{X: 40, Y: olderRow, Button: tea.MouseLeft}))
	if m.sessionPickerMode {
		t.Fatal("click on a session row did not close the picker")
	}
	v = stripANSI(viewText(m))
	if !strings.Contains(v, oldText) {
		t.Errorf("clicked older session not loaded: %q", v)
	}
	if strings.Contains(v, newText) {
		t.Errorf("clicked older session kept newer transcript: %q", v)
	}
}

func TestSessionPickerHoverHighlightsRow(t *testing.T) {
	dir := t.TempDir()
	st := newTestStore(t)
	for i := 0; i < 6; i++ {
		sess, err := st.CreateSession(context.Background(), db.Session{
			Title: fmt.Sprintf("hover-session-%d", i), Directory: dir,
			TimeCreated: int64(i), TimeUpdated: int64(i),
		})
		if err != nil {
			t.Fatalf("create session %d: %v", i, err)
		}
		um, err := st.InsertMessage(context.Background(), db.Message{SessionID: sess.ID, Role: "user"})
		if err != nil {
			t.Fatalf("insert message %d: %v", i, err)
		}
		text := fmt.Sprintf("hover-msg-%d", i)
		if _, err := st.InsertPart(context.Background(), db.Part{MessageID: um.ID, Type: "text", Text: &text}); err != nil {
			t.Fatalf("insert part %d: %v", i, err)
		}
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: dir})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m = typeText(m, "/resume")
	m = upd(m, enter())
	if !m.sessionPickerMode {
		t.Fatal("resume picker not open")
	}
	target := -1
	for i, line := range strings.Split(stripANSI(viewText(m)), "\n") {
		if strings.Contains(line, "hover-session-3") {
			target = i
			break
		}
	}
	if target < 0 {
		t.Fatalf("target session row not rendered: %q", stripANSI(viewText(m)))
	}
	m = upd(m, tea.MouseMotionMsg(tea.Mouse{X: 40, Y: target, Button: tea.MouseLeft}))
	want := -1
	for i, sess := range m.sessionItems {
		if strings.Contains(sess.Title, "hover-session-3") {
			want = i
			break
		}
	}
	if m.sessionHover != want {
		t.Fatalf("sessionHover = %d, want %d (hover-session-3)", m.sessionHover, want)
	}
	if !strings.Contains(viewText(m), "48;2;48;48;46") {
		t.Errorf("hovered row missing background highlight: %q", viewText(m))
	}
	m = upd(m, tea.MouseMotionMsg(tea.Mouse{X: 1, Y: 0, Button: tea.MouseLeft}))
	if m.sessionHover != -1 {
		t.Fatalf("sessionHover = %d after leaving the list, want -1", m.sessionHover)
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
	v := statusDrawerText(m)
	if !strings.Contains(v, "hit 68k") || !strings.Contains(v, "miss 790") {
		t.Fatalf("status drawer missing cache stats: %q", v)
	}
	if !strings.Contains(v, "99%") {
		t.Fatalf("status drawer missing cache hit percent: %q", v)
	}
	if !strings.Contains(v, "1%") {
		t.Fatalf("status drawer missing cache miss percent: %q", v)
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
	v := statusDrawerText(m)
	if !strings.Contains(v, "$0.420") && !strings.Contains(v, "$0.42") {
		t.Fatalf("status drawer missing restored cost: %q", v)
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

func TestCodexLiveCatalogClearsRemovedSavedModel(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.projectSettings.Provider.Active = provider.IDCodex
	m.projectSettings.Model.Default = "removed-codex-model"
	m.model = "removed-codex-model"

	next, _ := m.Update(modelsMsg{infos: []modelscache.Info{{
		ID:       "gpt-account-default",
		Provider: provider.IDCodex,
	}}})
	m = next.(Model)
	if m.projectSettings.Model.Default != "" {
		t.Fatalf("saved Codex model = %q, want live default", m.projectSettings.Model.Default)
	}
	if m.model != "" {
		t.Fatalf("live Codex model = %q, want provider default", m.model)
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

func TestModelEndpointRefreshesStaleResponsesRoute(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: opencode.NewClient("k", opencode.WithBaseURL("https://opencode.ai/zen/go/v1")), Workdir: t.TempDir()})
	m.model = "gpt-5.6-luna"
	m.modelInfos = []modelscache.Info{{
		ID: "gpt-5.6-luna", Provider: modelscache.ProviderOpenCodeGo,
		Endpoint: "https://opencode.ai/zen/go/v1/chat/completions",
	}}
	if got := m.modelEndpoint(); got != "https://opencode.ai/zen/go/v1/responses" {
		t.Fatalf("stale cached endpoint = %q", got)
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
	if !strings.Contains(v, "working") {
		t.Fatalf("live working status missing above the prompt: %q", v)
	}
	if !strings.Contains(v, "esc cancel") {
		t.Fatalf("busy cancel hint missing: %q", v)
	}
	if !strings.Contains(v, "think") {
		t.Fatalf("live thinking activity missing: %q", v)
	}
	thinkAt, promptAt := -1, -1
	for i, line := range strings.Split(v, "\n") {
		if thinkAt < 0 && strings.Contains(line, "working") {
			thinkAt = i
		}
		if strings.Contains(line, "ask lazykoder") || strings.Contains(line, "╭") && promptAt < 0 && i > 2 {
			if strings.Contains(line, "╭") {
				promptAt = i
			}
		}
	}
	m.pulseOn = true
	m.pulse = 0
	dimPlasma := m.plasmaBlob()
	dim := m.liveStatusView()
	m.pulse = pulseSteps / 2
	litPlasma := m.plasmaBlob()
	lit := m.liveStatusView()
	if !strings.Contains(dim, dimPlasma) || !strings.Contains(lit, litPlasma) || dimPlasma == litPlasma {
		t.Fatalf("plasma blob should animate the working status: dim=%q lit=%q", dim, lit)
	}
	if thinkAt < 0 || promptAt < 0 || thinkAt >= promptAt {
		t.Fatalf("thinking should sit above the input box: think=%d prompt=%d\n%s", thinkAt, promptAt, v)
	}
	if promptAt-thinkAt < 2 {
		t.Fatalf("need a blank row between thinking and the input box: think=%d prompt=%d\n%s", thinkAt, promptAt, v)
	}
}

func TestWorkBracketStaysAfterTurnEnds(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.items = []transcriptItem{
		{kind: itemUser, text: "hello"},
		{kind: itemAssistant, text: "hi back"},
	}
	m.busy = false
	body := stripANSI(strings.Join(m.renderedItems(), "\n"))
	if !strings.Contains(body, "╭ hello") || !strings.Contains(body, "hello") {
		t.Fatalf("static user bracket missing after the turn finished: %q", body)
	}
	if !lineHasPrefix(body, roleAssistant, workRail) || !lineHasPrefix(body, "hi back", workRail) {
		t.Fatalf("static reply rail missing after the turn finished: %q", body)
	}
	if !strings.Contains(body, "hello") || !strings.Contains(body, "hi back") {
		t.Fatalf("turn text missing: %q", body)
	}
}

func TestUserPromptBigBracketAndReplyUsesRail(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.items = []transcriptItem{
		{kind: itemUser, text: "Tell me something long\nand more"},
		{kind: itemReasoning, text: "planning", collapsed: true},
		{kind: itemAssistant, text: "Here is a long reply\nwith two lines"},
	}
	body := stripANSI(strings.Join(m.renderedItems(), "\n"))
	if !strings.Contains(body, "╭ Tell me something long") {
		t.Fatalf("first user line should open with the top curl: %q", body)
	}
	var lastMore string
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "and more") {
			lastMore = line
		}
	}
	if !strings.HasPrefix(strings.TrimSpace(lastMore), "╰") {
		t.Fatalf("last user line should open with the bottom curl: %q", lastMore)
	}
	if strings.Contains(body, "╮") || strings.Contains(body, "╯") {
		t.Fatalf("right-side curls should be gone: %q", body)
	}
	if strings.Contains(body, workBracket) || strings.Contains(body, "─") {
		t.Fatalf("user prompt should not use per-line brackets or horizontal borders: %q", body)
	}
	var userLines int
	for _, line := range strings.Split(body, "\n") {
		plain := strings.TrimSpace(line)
		switch {
		case strings.Contains(line, "Tell me something long"), strings.Contains(line, "and more"):
			userLines++
			if strings.HasPrefix(plain, workRail) && strings.HasSuffix(plain, workRail) {
				t.Fatalf("prompt edge lines should use corner curls, not rails: %q", line)
			}
		case strings.Contains(line, thinkingLabel) || strings.Contains(line, roleAssistant) || strings.Contains(line, "Here is a long reply") || strings.Contains(line, "with two lines"):
			if !strings.HasPrefix(plain, workRail) {
				t.Fatalf("thinking and assistant lines should start with the rail: %q", line)
			}
		case strings.Contains(line, roleYou):
			if strings.Contains(plain, workBracket) || strings.HasPrefix(plain, workRail) {
				t.Fatalf("you label should stay unmarked: %q", line)
			}
		}
	}
	if userLines != 2 {
		t.Fatalf("want two bracketed user lines, got %d\n%s", userLines, body)
	}
}

func TestTokensFollowLatestRequestSize(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	high := int64(8000)
	low := int64(2100)
	m.applyPart(db.Part{Type: "step-finish", TokensTotal: &high})
	if m.tokensUsed != 8000 {
		t.Fatalf("tokensUsed = %d, want 8000", m.tokensUsed)
	}
	m.applyPart(db.Part{Type: "step-finish", TokensTotal: &low})
	if m.tokensUsed != 2100 {
		t.Fatalf("tokensUsed = %d, want latest request 2100", m.tokensUsed)
	}
	empty := int64(0)
	m.applyPart(db.Part{Type: "step-finish", TokensTotal: &empty})
	if m.tokensUsed != 2100 {
		t.Fatalf("empty usage wiped meter: %d", m.tokensUsed)
	}
}

func TestCompactEventResetsTokensUsed(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	high := int64(8000)
	m.applyPart(db.Part{Type: "step-finish", TokensTotal: &high})
	m.cacheHit = 68000
	m.cacheMiss = 790
	env := agent.EncodeCompactText(agent.CompactEnvelope{
		Summary:     "handoff",
		FromWindow:  1_000_000,
		ToWindow:    256_000,
		Reason:      agent.CompactReasonShrink,
		TokensAfter: 1200,
	})
	m = m.applyEvent(agent.Event{
		Kind:       agent.EventCompacted,
		TokensUsed: 1200,
		Part:       agent.PartDelta{ID: "p_c", Kind: agent.PartDeltaCompaction, Text: env},
	})
	if m.tokensUsed != 1200 {
		t.Fatalf("tokensUsed = %d, want 1200 after compact", m.tokensUsed)
	}
	if m.cacheHit != 0 || m.cacheMiss != 0 {
		t.Fatalf("cache after compact event = %d/%d, want 0/0", m.cacheHit, m.cacheMiss)
	}
	m.bumpTokenFloor()
	if m.tokensUsed != 1200 {
		t.Fatalf("bumpTokenFloor restored peak: %d", m.tokensUsed)
	}
	v := strings.Join(func() []string {
		var texts []string
		for _, it := range m.items {
			texts = append(texts, it.text)
		}
		return texts
	}(), "\n")
	if !strings.Contains(v, "context compacted (1000k -> 256k)") {
		t.Fatalf("missing compact notice: %q", v)
	}
}

func TestReplayAfterCompactUsesCompactFill(t *testing.T) {
	st := newTestStore(t)
	sess, err := st.CreateSession(context.Background(), db.Session{Title: "t", Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	am, err := st.InsertMessage(ctx, db.Message{SessionID: sess.ID, Role: "assistant"})
	if err != nil {
		t.Fatal(err)
	}
	peak := int64(144635)
	if _, err := st.InsertPart(ctx, db.Part{MessageID: am.ID, Type: "step-finish", TokensInput: &peak, TokensTotal: &peak}); err != nil {
		t.Fatal(err)
	}
	cm, err := st.InsertMessage(ctx, db.Message{SessionID: sess.ID, Role: "assistant", Agent: agent.CompactAgentName})
	if err != nil {
		t.Fatal(err)
	}
	env := agent.EncodeCompactText(agent.CompactEnvelope{
		Summary:     "handoff",
		TokensAfter: 20880,
		Reason:      agent.CompactReasonManual,
	})
	if _, err := st.InsertPart(ctx, db.Part{MessageID: cm.ID, Type: agent.CompactPartType, Text: &env}); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: t.TempDir(), Session: &sess})
	if m.tokensUsed != 20880 {
		t.Fatalf("tokensUsed = %d, want compact fill 20880 not peak 144635", m.tokensUsed)
	}
	if m.cacheHit != 0 || m.cacheMiss != 0 {
		t.Fatalf("cache after compact = %d/%d, want 0/0", m.cacheHit, m.cacheMiss)
	}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m = mm.(Model)
	if got := m.statusTokensValue(); got != "20k" {
		t.Fatalf("tokens meter = %q, want 20k (compact fill), not the 144k peak", got)
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
	v := statusDrawerText(m)
	if !strings.Contains(v, "$0.420") && !strings.Contains(v, "$0.42") {
		t.Fatalf("status drawer missing session cost: %q", v)
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
	v := statusDrawerText(m)
	if !strings.Contains(v, "hit 0") || !strings.Contains(v, "miss 397") {
		t.Fatalf("status drawer missing cache counts: %q", v)
	}
	if !strings.Contains(v, "$0.00") {
		t.Fatalf("status drawer missing zero cost after usage: %q", v)
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

func TestSessionCostUsesMessageModelNotLive(t *testing.T) {
	st := newTestStore(t)
	sess, err := st.CreateSession(context.Background(), db.Session{Title: "t", Directory: t.TempDir(), Model: "cheap"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	am, err := st.InsertMessage(ctx, db.Message{SessionID: sess.ID, Role: "assistant", ModelID: "pricey"})
	if err != nil {
		t.Fatal(err)
	}
	in, out := int64(1_000_000), int64(0)
	if _, err := st.InsertPart(ctx, db.Part{MessageID: am.ID, Type: "step-finish", TokensInput: &in, TokensOutput: &out, TokensTotal: &in}); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: t.TempDir(), Session: &sess})
	m.model = "cheap"
	m.modelInfos = []modelscache.Info{
		{ID: "cheap", InputPerM: 0.10, OutputPerM: 0.10},
		{ID: "pricey", InputPerM: 3.00, OutputPerM: 15.00},
	}
	m.recomputeSessionCost()
	// 1M input on pricey = $3.00, not $0.10 from the live cheap model.
	if m.sessionCost < 2.9 || m.sessionCost > 3.1 {
		t.Fatalf("sessionCost = %v, want ~3.00 from pricey step", m.sessionCost)
	}
}

func TestChildSessionCostAndCacheRollUp(t *testing.T) {
	st := newTestStore(t)
	parent, err := st.CreateSession(context.Background(), db.Session{Title: "p", Directory: t.TempDir(), Model: "parent-model"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pid := parent.ID
	child, err := st.CreateSession(ctx, db.Session{
		Title: "explore", Directory: t.TempDir(), Model: "child-model",
		ParentSessionID: &pid, Kind: db.SessionKindSubagent,
	})
	if err != nil {
		t.Fatal(err)
	}
	am, err := st.InsertMessage(ctx, db.Message{SessionID: child.ID, Role: "assistant", ModelID: "child-model"})
	if err != nil {
		t.Fatal(err)
	}
	in, hit, out := int64(1_000_000), int64(800_000), int64(0)
	if _, err := st.InsertPart(ctx, db.Part{
		MessageID: am.ID, Type: "step-finish",
		TokensInput: &in, TokensOutput: &out, TokensTotal: &in, TokensCacheRead: &hit,
	}); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: t.TempDir(), Session: &parent})
	m.modelInfos = []modelscache.Info{
		{ID: "parent-model", InputPerM: 0.14, OutputPerM: 0.28, CacheReadPerM: 0.014},
		{ID: "child-model", InputPerM: 1.00, OutputPerM: 2.00, CacheReadPerM: 0.10},
	}
	m = m.reloadSubagentRows()
	if len(m.subagentItems) != 1 {
		t.Fatalf("rows = %d", len(m.subagentItems))
	}
	row := m.subagentItems[0]
	// miss 200k * $1/M + hit 800k * $0.10/M = 0.20 + 0.08 = 0.28
	if row.Cost < 0.27 || row.Cost > 0.29 {
		t.Fatalf("child cost = %v, want ~0.28", row.Cost)
	}
	if row.CacheHit != 800_000 || row.CacheMiss != 200_000 {
		t.Fatalf("child cache = %d/%d", row.CacheHit, row.CacheMiss)
	}
	_, subs, total := m.costTotals()
	if subs < 0.27 || subs > 0.29 || total < 0.27 || total > 0.29 {
		t.Fatalf("rolled cost parent/subs/total = %v %v %v", m.sessionCost, subs, total)
	}
	ch, cm := m.cacheTotals()
	if ch != 800_000 || cm != 200_000 {
		t.Fatalf("rolled cache = %d/%d", ch, cm)
	}
	got := m.statusSegmentValue("cost")
	if !strings.Contains(got, "subs") || !strings.Contains(got, "$0.28") && !strings.Contains(got, "$0.280") {
		t.Fatalf("status cost = %q", got)
	}
	right := m.subagentRowRight(row, 200)
	if !strings.Contains(right, "$0.28") && !strings.Contains(right, "$0.280") {
		t.Fatalf("drawer right missing child cost: %q", right)
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
	v := statusDrawerText(m)
	if !strings.Contains(v, "80 tps") && !strings.Contains(v, "79 tps") && !strings.Contains(v, "81 tps") {
		t.Fatalf("status drawer missing turn tps: %q", v)
	}
}

func TestRollingTPSUsesRecentSamples(t *testing.T) {
	now := time.Unix(100, 0)
	samples := []tpsSample{
		{at: now.Add(-1500 * time.Millisecond), tokens: 20},
		{at: now.Add(-500 * time.Millisecond), tokens: 10},
	}
	if got := rollingTPS(samples, now); got != 20 {
		t.Fatalf("rollingTPS = %v, want 20", got)
	}
	burst := []tpsSample{
		{at: now.Add(-10 * time.Millisecond), tokens: 10},
		{at: now.Add(-5 * time.Millisecond), tokens: 10},
	}
	if got := rollingTPS(burst, now); got != 0 {
		t.Fatalf("short burst rollingTPS = %v, want 0 until the minimum interval", got)
	}
	if got := formatTPS(120); got != "120 tps" {
		t.Fatalf("formatTPS(120) = %q, want 120 tps", got)
	}
}

func TestDisplayTPSSurvivesSilentBusyGap(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.busy = true
	m.turnStarted = time.Now().Add(-time.Second)
	m.tokensPerSec = 80
	if got := m.displayTPS(); got != 80 {
		t.Fatalf("silent busy displayTPS = %v, want last known 80", got)
	}

	m.tokensPerSec = 0
	if got := m.statusSegmentValue("tps"); got != "measuring" {
		t.Fatalf("unmeasured busy TPS label = %q, want measuring", got)
	}
}

func TestFooterShowsLiveTPSWhileBusy(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.busy = true
	m.turnStarted = time.Now().Add(-time.Second)
	m.turnGenTokens = 80
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = mm.(Model)
	v := statusDrawerText(m)
	if !strings.Contains(v, "80 tps") && !strings.Contains(v, "79 tps") && !strings.Contains(v, "81 tps") && !strings.Contains(v, "~80 tps") {
		t.Fatalf("busy status drawer missing live tps: %q", v)
	}
}

func TestFooterShowsNumericHighTPS(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.model = "fast-model"
	m.tokensPerSec = 120
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = mm.(Model)
	v := stripANSI(viewText(m))
	if strings.Contains(v, ">99.9") || !strings.Contains(v, "120 tps") {
		t.Fatalf("footer distorted high TPS: %q", v)
	}
}

func TestFormatCacheIncludesHitPercent(t *testing.T) {
	if got := formatCache(80, 20); got != "hit 80 80%  miss 20 20%" {
		t.Fatalf("formatCache(80, 20) = %q", got)
	}
	if got := cacheHitPercent(68000, 790); got != 99 {
		t.Fatalf("cacheHitPercent(68000, 790) = %d, want 99", got)
	}
	if got := cacheHitPercent(0, 397); got != 0 {
		t.Fatalf("cacheHitPercent(0, 397) = %d, want 0", got)
	}
	if got := formatCache(0, 397); got != "hit 0 0%  miss 397 100%" {
		t.Fatalf("formatCache(0, 397) = %q", got)
	}
	if got := formatCache(1400, 100); got != "hit 1.4k 93%  miss 100 7%" {
		t.Fatalf("formatCache(1400, 100) = %q", got)
	}
}

func TestFooterShowsHitAndMissPercentsAt80(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.model = "deepseek-v4-flash"
	m.modelInfos = []modelscache.Info{{ID: "deepseek-v4-flash", Context: 100000}}
	m.tokensUsed = 1500
	m.cacheHit = 1400
	m.cacheMiss = 100
	for _, width := range []int{80, 100, 120} {
		mm, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		got := mm.(Model)
		v := statusDrawerText(got)
		if !strings.Contains(v, "1.5k/100k") {
			t.Fatalf("width %d status drawer missing token window: %q", width, v)
		}
		if !strings.Contains(v, "hit 1.4k") || !strings.Contains(v, "93%") {
			t.Fatalf("width %d status drawer missing hit percent: %q", width, v)
		}
		if !strings.Contains(v, "miss 100") || !strings.Contains(v, "7%") {
			t.Fatalf("width %d status drawer missing miss percent: %q", width, v)
		}
	}
}

func TestComposerShowsContext(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.model = "deepseek-v4-flash"
	m.modelInfos = []modelscache.Info{{ID: "deepseek-v4-flash", Context: 128000}}
	m.tokensUsed = 1200
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	v := statusDrawerText(m)
	if !strings.Contains(v, "deepseek-v4-flash") || !strings.Contains(v, "1.2k/128k") {
		t.Fatalf("status drawer missing context: %q", v)
	}
}

func TestComposerPutsStatusOnTheRight(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	var footer string
	for _, line := range strings.Split(stripANSI(viewText(m)), "\n") {
		if strings.Contains(line, "status ▾") {
			footer = line
		}
	}
	if footer == "" {
		t.Fatal("status control missing from composer")
	}
	idxHint := strings.Index(footer, "enter send")
	idxStatus := strings.Index(footer, "status ▾")
	if idxHint < 0 || idxStatus < 0 || idxStatus < idxHint {
		t.Fatalf("status control is not right of the hint: %q", footer)
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
	if !strings.Contains(v, theme.StatusBatonFrame(m.pulse)) {
		t.Fatalf("in-flight tool card missing baton spinner: %q", v)
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
	m = m.toggleAllReasoning()
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

func TestAltEnterAndCtrlJInsertNewline(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = typeText(m, "a")
	m, cmd := updCmd(m, tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	if cmd != nil {
		t.Fatalf("alt+enter submitted: %v", cmd)
	}
	if got := m.prompt.Value(); got != "a\n" {
		t.Fatalf("alt+enter = %q, want %q", got, "a\n")
	}
	m, cmd = updCmd(m, tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatalf("ctrl+j submitted: %v", cmd)
	}
	if got := m.prompt.Value(); got != "a\n\n" {
		t.Fatalf("ctrl+j = %q, want %q", got, "a\n\n")
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
	if !strings.Contains(v, "new session") || !strings.Contains(v, "/ commands") || !strings.Contains(v, "/settings") {
		t.Fatalf("empty state missing: %q", v)
	}
	m.items = append(m.items, transcriptItem{kind: itemUser, text: "hi"})
	m.syncTranscript()
	if strings.Contains(stripANSI(viewText(m)), "ask anything about this project") {
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
	if !strings.Contains(v, "enter") || !strings.Contains(v, "send") || !strings.Contains(v, "/settings") || !strings.Contains(v, "/continue") {
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
	cardLeft := strings.Index(lines[start], "╭")
	if cardLeft < 0 {
		t.Fatalf("help card top border missing: %q", lines[start])
	}
	minCol := lipgloss.Width(lines[start][:cardLeft])
	var lefts []int
	for _, line := range lines[start : end+1] {
		if col := indexAtDisplayCol(line, "│", minCol); col >= 0 {
			lefts = append(lefts, col)
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

func indexAtDisplayCol(s, sep string, minCol int) int {
	col := 0
	for s != "" {
		i := strings.Index(s, sep)
		if i < 0 {
			return -1
		}
		col += lipgloss.Width(s[:i])
		if col >= minCol {
			return col
		}
		col += lipgloss.Width(sep)
		s = s[i+len(sep):]
	}
	return -1
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
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = mm.(Model)
	meta := `{"diff":"@@ -1,1 +1,1 @@\n-old\n+new line"}`
	path := "main.go"
	tc := db.ToolCall{
		Tool: "edit", Status: "completed", Title: &path,
		InputJSON:    `{"filePath":"main.go","oldString":"old","newString":"new line"}`,
		MetadataJSON: &meta,
	}
	// Open by default: full soft-tinted diff panel.
	m.items = append(m.items, transcriptItem{kind: itemTool, collapsed: false, tool: tc})
	m.syncTranscript()
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "main.go") {
		t.Fatalf("edit missing path: %q", v)
	}
	if !strings.Contains(v, "+1") || !strings.Contains(v, "-1") {
		t.Fatalf("edit missing diff stats: %q", v)
	}
	if !strings.Contains(v, "@@") || !strings.Contains(v, "new line") {
		t.Fatalf("open edit missing full diff: %q", v)
	}
	// Diff lines paint full card width.
	var diffLine string
	for _, line := range strings.Split(viewText(m), "\n") {
		if strings.Contains(stripANSI(line), "+new line") {
			diffLine = line
			break
		}
	}
	if diffLine == "" {
		t.Fatal("missing +new line row")
	}
	wantW := m.toolCardWidth()
	if got := lipgloss.Width(diffLine); got < wantW {
		t.Fatalf("diff line width %d < tool card width %d", got, wantW)
	}
	// Collapsed: header + stats only, no body.
	m.items[0].collapsed = true
	m.syncTranscript()
	v = stripANSI(viewText(m))
	if strings.Contains(v, "@@") || strings.Contains(v, "new line") {
		t.Fatalf("collapsed edit still shows body: %q", v)
	}
	if !strings.Contains(v, "+1") || !strings.Contains(v, "-1") {
		t.Fatalf("collapsed edit should keep stats: %q", v)
	}
}

func TestEditOpenByDefaultAndToggle(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = mm.(Model)
	path := "main.go"
	meta := `{"diff":"@@ -1,1 +1,1 @@\n-old\n+new"}`
	m.applyTool(agent.Event{Tool: agent.ToolDelta{
		Name: "edit", Status: "pending", Title: path, PartID: "p1", CallID: "c1",
		InputJSON: `{"filePath":"main.go","oldString":"old","newString":"new"}`,
	}})
	if idx := m.lastTool; idx < 0 || m.items[idx].collapsed {
		t.Fatal("pending edit should start open")
	}
	// User collapses all tools with ctrl+e.
	m = m.toggleAllTools()
	if !m.items[m.lastTool].collapsed {
		t.Fatal("toggle should collapse edit")
	}
	// Status update must keep the user's collapsed choice.
	m.applyTool(agent.Event{Tool: agent.ToolDelta{
		Name: "edit", Status: "completed", Title: path, PartID: "p1", CallID: "c1",
		InputJSON:    `{"filePath":"main.go","oldString":"old","newString":"new"}`,
		MetadataJSON: meta,
	}})
	if !m.items[m.lastTool].collapsed {
		t.Fatal("completed edit must stay collapsed after user closed it")
	}
	// Re-open.
	m = m.toggleAllTools()
	if m.items[m.lastTool].collapsed {
		t.Fatal("toggle should re-open edit")
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "+new") || !strings.Contains(v, "-old") {
		t.Fatalf("re-opened edit missing changes: %q", v)
	}
}

func TestEditCtrlEToggles(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	path := "main.go"
	meta := `{"diff":"@@ -1,1 +1,1 @@\n-a\n+b"}`
	m.applyTool(agent.Event{Tool: agent.ToolDelta{
		Name: "edit", Status: "completed", Title: path, PartID: "p1",
		InputJSON: `{"filePath":"main.go","oldString":"a","newString":"b"}`, MetadataJSON: meta,
	}})
	if m.items[m.lastTool].collapsed {
		t.Fatal("edit should start open")
	}
	// ctrl+e works even when the prompt has text.
	m.prompt.SetValue("draft")
	mm, _ = m.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	m = mm.(Model)
	if !m.items[m.lastTool].collapsed {
		t.Fatal("ctrl+e should collapse the edit card")
	}
	mm, _ = m.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
	m = mm.(Model)
	if m.items[m.lastTool].collapsed {
		t.Fatal("ctrl+e should re-open the edit card")
	}
}

func TestEditFallbackDiffFromArgs(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	path := "x.go"
	tc := db.ToolCall{
		Tool: "edit", Status: "completed", Title: &path,
		InputJSON: `{"filePath":"x.go","oldString":"aaa","newString":"bbb"}`,
		// No MetadataJSON: UI should still paint a synthetic diff.
	}
	m.items = append(m.items, transcriptItem{kind: itemTool, collapsed: false, tool: tc})
	m.syncTranscript()
	v := stripANSI(viewText(m))
	// Body marker is after the line-number gutter: "… │ -aaa"
	if !strings.Contains(v, "-aaa") || !strings.Contains(v, "+bbb") {
		t.Fatalf("synthetic edit diff missing: %q", v)
	}
}

func TestEditDiffShowsLineNumbersAndRelPath(t *testing.T) {
	dir := t.TempDir()
	// Build a deep file so recompute can prove real line numbers (not 1..n).
	var body strings.Builder
	for i := 1; i <= 80; i++ {
		fmt.Fprintf(&body, "pad-%d\n", i)
	}
	body.WriteString("## Project Snapshot\n\n")
	body.WriteString("- Example workspace size: 128\n")
	body.WriteString("- Review sample: 731\n\n")
	body.WriteString("## License\n\n")
	body.WriteString("MIT, see [LICENSE](LICENSE).\n")
	rel := "README.md"
	abs := filepath.Join(dir, rel)
	if err := os.WriteFile(abs, []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: dir})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm.(Model)

	// Historical row with the OLD bug: stored @@ -1 even though edit is deep.
	oldS := "- Example workspace size: 128\n- Review sample: 731\n\n"
	newS := "- Example workspace size: 128\n- Review sample: 731\n\n## License\n\nMIT, see [LICENSE](LICENSE).\n"
	badMeta := `{"diff":"@@ -1,4 +1,8 @@\n - Example workspace size: 128\n - Review sample: 731\n \n+## License\n \n+MIT, see [LICENSE](LICENSE).\n+\n"}`
	title := abs
	tc := db.ToolCall{
		Tool: "edit", Status: "completed", Title: &title,
		InputJSON: fmt.Sprintf(
			`{"filePath":%q,"oldString":%q,"newString":%q}`, abs, oldS, newS,
		),
		MetadataJSON: &badMeta,
	}
	m.items = append(m.items, transcriptItem{kind: itemTool, collapsed: false, tool: tc})
	m.syncTranscript()
	v := stripANSI(viewText(m))
	if !strings.Contains(v, rel) {
		t.Fatalf("header missing relative path %q in: %q", rel, v)
	}
	if strings.Contains(v, abs) {
		t.Fatalf("header still shows absolute path: %q", v)
	}
	// Must NOT show a deep edit as lines 1-4 only; recompute from disk.
	if strings.Contains(v, "@@ -1,") {
		t.Fatalf("still showing bogus @@ -1 hunk after recompute: %q", v)
	}
	// Line numbers should be in the 80s.
	foundDeep := false
	for _, n := range []string{"80", "81", "82", "83", "84", "85"} {
		if strings.Contains(v, n+" ") || strings.Contains(v, " "+n) {
			foundDeep = true
			break
		}
	}
	if !foundDeep {
		t.Fatalf("expected deep line numbers (~80+), got: %q", v)
	}
	if !strings.Contains(v, "│") {
		t.Fatalf("diff missing line-number gutter: %q", v)
	}
	if !strings.Contains(v, "License") {
		t.Fatalf("diff body missing: %q", v)
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

func TestAtPickerListsSubagentsWithStatus(t *testing.T) {
	st := newTestStore(t)
	dir := t.TempDir()
	parent, err := st.CreateSession(context.Background(), db.Session{Directory: dir, Title: "main"})
	if err != nil {
		t.Fatalf("parent: %v", err)
	}
	pid := parent.ID
	child, err := st.CreateSession(context.Background(), db.Session{
		Directory: dir, Title: "lint-fix", ParentSessionID: &pid, Kind: db.SessionKindSubagent,
	})
	if err != nil {
		t.Fatalf("child: %v", err)
	}
	um, err := st.InsertMessage(context.Background(), db.Message{SessionID: child.ID, Role: "user"})
	if err != nil {
		t.Fatal(err)
	}
	task := "fix the linter"
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: um.ID, Type: "text", Text: &task}); err != nil {
		t.Fatal(err)
	}
	am, err := st.InsertMessage(context.Background(), db.Message{SessionID: child.ID, Role: "assistant"})
	if err != nil {
		t.Fatal(err)
	}
	reply := "all clean"
	if _, err := st.InsertPart(context.Background(), db.Part{MessageID: am.ID, Type: "text", Text: &reply}); err != nil {
		t.Fatal(err)
	}

	m := New(Options{Store: st, Client: deadClient(), Workdir: dir, Session: &parent})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = mm.(Model)
	m = typeText(m, "@lint")
	if !m.filePickerMode {
		t.Fatal("@ should open picker")
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "lint-fix") {
		t.Fatalf("picker missing sub-agent name: %q", v)
	}
	if !strings.Contains(v, "agent") {
		t.Fatalf("picker missing agent label: %q", v)
	}
	if !strings.Contains(v, "completed") && !strings.Contains(v, theme.StatusBatonFrame(0)) {
		t.Fatalf("picker missing baton status mark: %q", v)
	}
	m = upd(m, enter())
	if m.filePickerMode {
		t.Fatal("picker should close")
	}
	if !strings.Contains(m.prompt.Value(), "@agent:lint-fix") {
		t.Fatalf("prompt missing agent mention: %q", m.prompt.Value())
	}
	expanded := m.withMentionContext(m.prompt.Value() + " please continue")
	if !strings.Contains(expanded, "Sub-agent context: lint-fix") {
		t.Fatalf("expanded missing context header: %q", expanded)
	}
	if !strings.Contains(expanded, "fix the linter") {
		t.Fatalf("expanded missing child task: %q", expanded)
	}
	if !strings.Contains(expanded, "all clean") {
		t.Fatalf("expanded missing child reply: %q", expanded)
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

func TestCtrlCPastedTextClearedUnlessSelected(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})

	// Simulate pasted text
	mm, _ := m.Update(tea.PasteMsg{Content: "pasted multi-line\ncode snippet"})
	m = mm.(Model)
	if got := m.prompt.Value(); got != "pasted multi-line\ncode snippet" {
		t.Fatalf("pasted text = %q", got)
	}

	// Pressing Ctrl+C without selection should clear the text and NOT copy to clipboard
	m, cmd := updCmd(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatalf("ctrl+c without selection returned command: %v", cmd)
	}
	if got := m.prompt.Value(); got != "" {
		t.Fatalf("ctrl+c did not clear pasted text, got %q", got)
	}
	if m.copyNotice != "" {
		t.Fatalf("copyNotice = %q, want empty", m.copyNotice)
	}
	if m.quitConfirm {
		t.Fatal("first ctrl+c with text should not arm quit confirm")
	}

	// Now that prompt is empty, next Ctrl+C arms quit confirmation
	m, cmd = updCmd(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatalf("ctrl+c on empty prompt returned command: %v", cmd)
	}
	if !m.quitConfirm {
		t.Fatal("ctrl+c on empty prompt should arm quit confirm")
	}

	// Subsequent Ctrl+C quits
	m, cmd = updCmd(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil || cmd() != (tea.QuitMsg{}) {
		t.Fatalf("ctrl+c when armed did not quit: %v", cmd)
	}

	// Test Ctrl+A selection enables Ctrl+C copying
	m = New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ = m.Update(tea.PasteMsg{Content: "pasted code"})
	m = mm.(Model)

	m, _ = updCmd(m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	if !m.promptSelectAll {
		t.Fatal("ctrl+a did not set promptSelectAll")
	}

	m, cmd = updCmd(m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !copyCmdContains(cmd, "pasted code") {
		t.Fatalf("ctrl+c with ctrl+a did not copy text: %v", cmd)
	}
	if m.copyNotice != "Text copied" {
		t.Fatalf("copyNotice = %q, want %q", m.copyNotice, "Text copied")
	}
	if got := m.prompt.Value(); got != "pasted code" {
		t.Fatalf("ctrl+c with ctrl+a should preserve text, got %q", got)
	}
}

func TestCtrlZUndoesLastKeys(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})

	m = typeText(m, "hello")
	if got := m.prompt.Value(); got != "hello" {
		t.Fatalf("prompt value = %q, want %q", got, "hello")
	}

	// First Ctrl+Z undoes last typed character 'o' -> "hell"
	m, _ = updCmd(m, tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if got := m.prompt.Value(); got != "hell" {
		t.Fatalf("after first ctrl+z = %q, want %q", got, "hell")
	}

	// Second Ctrl+Z undoes 'l' -> "hel"
	m, _ = updCmd(m, tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if got := m.prompt.Value(); got != "hel" {
		t.Fatalf("after second ctrl+z = %q, want %q", got, "hel")
	}

	// Typing a new character 'p' after undo appends at cursor end -> "help"
	m = typeText(m, "p")
	if got := m.prompt.Value(); got != "help" {
		t.Fatalf("typing after undo = %q, want %q", got, "help")
	}

	// Ctrl+Z undoes 'p' -> "hel"
	m, _ = updCmd(m, tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if got := m.prompt.Value(); got != "hel" {
		t.Fatalf("ctrl+z after typing = %q, want %q", got, "hel")
	}

	// Test undo in slash mode
	m2 := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m2 = typeRune(m2, '/')
	m2 = typeText(m2, "sett")
	if got := m2.prompt.Value(); got != "/sett" {
		t.Fatalf("prompt value = %q, want %q", got, "/sett")
	}
	m2, _ = updCmd(m2, tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if got := m2.prompt.Value(); got != "/set" {
		t.Fatalf("ctrl+z in slash mode = %q, want %q", got, "/set")
	}
}
