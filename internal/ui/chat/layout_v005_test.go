package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

func TestHelpFitsAt80(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m.helpMode = true
	v := stripANSI(viewText(m))
	if strings.Contains(v, "lazykoder╭") {
		t.Fatalf("help collided with header brand: %q", v)
	}
	if !strings.Contains(v, "/settings") || !strings.Contains(v, "[x]") {
		t.Fatalf("help missing settings or [x]: %q", v)
	}
}

func TestOverlayCardsOpaque(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m = mm.(Model)
	m.items = []transcriptItem{{kind: itemUser, text: "secret-bleed-token"}}
	m.syncTranscript()
	m.confirmMode = true
	ask := m
	ask.askMode = true
	ask.askQuestion.Question = "pick one"
	ask.askQuestion.Options = []string{"a", "b"}
	file := m
	file = file.openFilePicker()

	for name, view := range map[string]string{
		"confirm": m.confirmOverlay(),
		"ask":     ask.askOverlay(),
		"at":      file.filePickerOverlay(),
	} {
		if !strings.Contains(view, "\x1b") && !strings.Contains(view, "╭") {
			t.Fatalf("%s overlay empty: %q", name, view)
		}
		// Cards are styled with the theme background so transcript
		// cells cannot show through the fill.
		if name == "at" && !strings.Contains(stripANSI(view), "files") {
			t.Fatalf("at overlay missing files section: %q", view)
		}
	}
	_ = theme.Bg
}

func TestVariantFooterNoRefresh(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m = m.openVariantPicker()
	v := stripANSI(m.pickerView())
	if strings.Contains(v, "r refresh") {
		t.Fatalf("variant footer still advertises refresh: %q", v)
	}
	if !strings.Contains(v, "reasoning_effort") && !strings.Contains(v, "reasoning") {
		t.Fatalf("variant drawer missing reasoning hint: %q", v)
	}
}

func TestCompact80x24KeepsTranscript(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m.todos = []db.Todo{
		{Content: "one", Status: db.TodoInProgress},
		{Content: "two", Status: db.TodoPending},
		{Content: "three", Status: db.TodoPending},
	}
	m.items = []transcriptItem{
		{kind: itemUser, text: "hello from compact"},
		{kind: itemAssistant, text: "line a\nline b\nline c\nline d\nline e\nline f\nline g"},
	}
	m.syncTranscript()
	v := stripANSI(viewText(m))
	if strings.Count(v, "\n") < 10 {
		t.Fatalf("compact view too short: %q", v)
	}
	if !strings.Contains(v, "ask lazykoder") {
		t.Fatalf("composer missing at 80x24: %q", v)
	}
	if strings.Contains(v, "Tip:") && strings.Contains(v, "line g") {
		// tip on alert row should be hidden under 100 cols
		if strings.Contains(v, "▼Tip:") {
			t.Fatalf("tip overwrote transcript: %q", v)
		}
	}
}

func TestSubagentDrawerDoesNotAutoOpen(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.subagentItems = []subagentRow{{ID: "a", Name: "one", Status: "completed"}}
	m = m.openSubagentDrawerIfNew()
	if m.subagentPickerMode {
		t.Fatal("drawer auto-opened")
	}
}

func TestResumeEmptyKeepsFrame(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = mm.(Model)
	m = m.openSessionPicker()
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "no sessions yet") {
		t.Fatalf("empty resume copy: %q", v)
	}
	if strings.Count(v, "│") < 10 {
		t.Fatalf("empty resume card too short: %q", v)
	}
}

func TestHelpOverlayHasNewCommands(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.helpMode = true
	v := stripANSI(m.helpOverlay())
	for _, want := range []string{"/settings", "/new", "/continue", "/refresh", "ctrl+z", "ctrl+e"} {
		if !strings.Contains(v, want) {
			t.Fatalf("help missing %q: %q", want, v)
		}
	}
}

func TestExpandedToolHasOutputSplit(t *testing.T) {
	out := "hello output"
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	body := m.renderTool(agent.Event{Tool: db.ToolCall{
		Tool:   "bash",
		Status: "completed",
		Output: &out,
	}}, false, 0)
	plain := stripANSI(body)
	if !strings.Contains(plain, "output") || !strings.Contains(plain, "hello output") {
		t.Fatalf("expanded tool missing header/body split: %q", plain)
	}
}
