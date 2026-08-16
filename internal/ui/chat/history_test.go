package chat

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/chinmay-sawant/lazykoder/internal/db"
)

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
	m = upd(m, tea.KeyPressMsg{Code: ' ', Text: " "})
	m = upd(m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.slashMode {
		t.Fatal("typing after pasted slash text reopened the slash menu")
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

func TestPromptUndo(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = upd(m, tea.PasteMsg{Content: "hello"})
	m = upd(m, tea.KeyPressMsg{Code: '!', Text: "!"})
	m, cmd := updCmd(m, tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatalf("ctrl+z returned command %v, want nil", cmd)
	}
	if got := m.prompt.Value(); got != "hello" {
		t.Fatalf("prompt after ctrl+z = %q, want %q", got, "hello")
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
	if len(m.items) == 0 || m.items[0].text != "first prompt" {
		t.Fatal("first user message missing from transcript")
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
	if strings.Contains(stripANSI(viewText(m)), "second prompt") {
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
	if strings.Contains(stripANSI(viewText(replayed)), "second prompt") {
		t.Fatal("soft-deleted user message returned in the UI replay")
	}
}

func TestHistoryDownRestoresCurrentDraft(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.inputHistory = []inputHistoryItem{{text: "older"}, {text: "newer"}}
	m.prompt.SetValue("draft I am typing")
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.prompt.Value(); got != "newer" {
		t.Fatalf("up = %q, want newer", got)
	}
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyDown})
	if got := m.prompt.Value(); got != "draft I am typing" {
		t.Fatalf("down should restore the current draft, got %q", got)
	}
	if m.historyCursor != -1 {
		t.Fatalf("historyCursor = %d, want -1 after restoring draft", m.historyCursor)
	}
}

func TestUpStaysInMultilinePrompt(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.inputHistory = []inputHistoryItem{{text: "previous message"}}
	m.prompt.SetValue("line one\nline two\nline three\nline four")
	m.prompt.SetHeight(m.promptHeight())
	if !m.promptHasMultipleLines() {
		t.Fatal("precondition: prompt should be multi-line")
	}
	start := m.prompt.Line()
	if start < 1 {
		t.Fatalf("precondition: cursor should start on a lower line, line=%d", start)
	}
	for i := 0; i < start+3; i++ {
		m = upd(m, tea.KeyPressMsg{Code: tea.KeyUp})
	}
	if got := m.prompt.Value(); got != "line one\nline two\nline three\nline four" {
		t.Fatalf("up from the first line jumped to history: %q", got)
	}
	if m.historyCursor != -1 {
		t.Fatalf("historyCursor = %d, want -1", m.historyCursor)
	}
}
