package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
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
	if !strings.Contains(v, "/new") || !strings.Contains(v, "/model") || !strings.Contains(v, "/variant") || !strings.Contains(v, "/help") || !strings.Contains(v, "/sessions") {
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
	cmdRow := -1
	promptRow := -1
	for i, line := range lines {
		if cmdRow < 0 && strings.Contains(line, "/new") {
			cmdRow = i
			continue
		}
		if cmdRow >= 0 && promptRow < 0 && strings.Contains(line, "╭") {
			promptRow = i
		}
	}
	if cmdRow < 0 {
		t.Fatalf("slash commands missing: %q", lines)
	}
	if promptRow < 0 {
		t.Fatalf("prompt row missing: %q", lines)
	}
	if cmdRow >= promptRow {
		t.Errorf("slash list row %d is not above prompt row %d: %q", cmdRow, promptRow, lines)
	}
	if strings.Contains(stripANSI(m.slashView()), "╭") || strings.Contains(stripANSI(m.slashView()), "╰") {
		t.Errorf("slash list should not have a border: %q", stripANSI(m.slashView()))
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

func TestSlashMenuFullWidthLeftAligned(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m = typeRune(m, '/')
	if !m.slashMode {
		t.Fatal("slash mode not opened on /")
	}

	var modelLine, newLine string
	var widest int
	for _, line := range strings.Split(stripANSI(m.slashView()), "\n") {
		if w := lipgloss.Width(line); w > widest {
			widest = w
		}
		if strings.Contains(line, "/model") && modelLine == "" {
			modelLine = line
		}
		if strings.Contains(line, "/new") && newLine == "" {
			newLine = line
		}
	}
	if widest < m.width-4 {
		t.Errorf("slash card width = %d, want near %d", widest, m.width)
	}
	if modelLine == "" {
		t.Fatal("/model row missing")
	}
	nameAt := strings.Index(modelLine, "/model")
	descAt := strings.Index(modelLine, "switch the chat model")
	if nameAt < 0 || descAt < 0 || nameAt > descAt {
		t.Fatalf("/model should sit left of its description on the same line: %q", modelLine)
	}
	if !strings.Contains(newLine, "start a new session") {
		t.Errorf("/new line missing its description: %q", newLine)
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

func TestSlashVariantOpensPicker(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m.model = "deepseek-v4-flash"
	m.modelInfos = []modelscache.Info{{ID: "deepseek-v4-flash", Variants: []string{"low", "medium", "high"}}}
	m = typeRune(m, '/')
	m = typeRune(m, 'v')
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !m.pickerMode || m.pickerKind != pickerKindVariant {
		t.Fatalf("pickerMode=%v kind=%q, want variant picker", m.pickerMode, m.pickerKind)
	}
	v := stripANSI(m.pickerView())
	if !strings.Contains(v, "low") || !strings.Contains(v, "medium") || !strings.Contains(v, "high") {
		t.Fatalf("variant picker missing options: %q", v)
	}
	m.pickerCursor = 2
	m = upd(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.pickerMode {
		t.Fatal("picker still open after select")
	}
	if m.variant != "high" {
		t.Fatalf("variant = %q, want high", m.variant)
	}
}
