package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSlashMenuOpensAndDivides(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	if !strings.Contains(stripANSI(viewText(m)), "ask lazykoder") {
		t.Fatalf("prompt placeholder missing: %q", stripANSI(viewText(m)))
	}
	m = typeRune(m, '/')
	if !m.slashMode {
		t.Fatal("slash mode not opened on /")
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "/new") || !strings.Contains(v, "/model") || !strings.Contains(v, "/help") || !strings.Contains(v, "/sessions") {
		t.Errorf("slash menu missing commands: %q", v)
	}
	if !strings.Contains(v, "start a new session") {
		t.Errorf("slash menu missing selected description: %q", v)
	}
}

func TestSlashMenuAnchorsAbovePrompt(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m = typeRune(m, '/')

	lines := strings.Split(stripANSI(viewText(m)), "\n")
	top := -1
	bottom := -1
	for i, line := range lines {
		if strings.Contains(line, "╭") && top == -1 {
			top = i
		}
		if strings.Contains(line, "╰") {
			bottom = i
		}
	}
	if top < 0 || bottom < 0 {
		t.Fatalf("slash card missing: %q", lines)
	}
	if !strings.Contains(stripANSI(viewText(m)), "/new") {
		t.Errorf("slash commands missing: %q", lines)
	}
	if got := m.prompt.Value(); got != "/" {
		t.Errorf("prompt = %q, want /", got)
	}
	if len(lines) > m.height {
		t.Errorf("slash view has %d rows for a %d-row terminal", len(lines), m.height)
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
	m.items = append(m.items, transcriptItem{kind: itemNote, text: "old line"})
	m = typeRune(m, '/')
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.slashMode {
		t.Fatal("slash mode still open after /new")
	}
	if len(m.items) != 0 {
		t.Errorf("transcript not cleared by /new: %d items", len(m.items))
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
