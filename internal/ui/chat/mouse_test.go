package chat

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

func TestMouseWheelScrollsTranscript(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	for i := 0; i < 60; i++ {
		m.items = append(m.items, transcriptItem{kind: itemNote, text: fmt.Sprintf("line %02d", i)})
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

func TestTranscriptDragSelectsAndCopiesText(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m.items = append(m.items,
		transcriptItem{kind: itemNote, text: "first message"},
		transcriptItem{kind: itemNote, text: "second message"},
	)
	m.syncTranscript()

	top := lipgloss.Height(m.headerView()) + 1
	mm, _ := m.Update(tea.MouseClickMsg(tea.Mouse{
		X:      0,
		Y:      top,
		Button: tea.MouseLeft,
	}))
	m = mm.(Model)
	mm, _ = m.Update(tea.MouseMotionMsg(tea.Mouse{
		X:      6,
		Y:      top + 1,
		Button: tea.MouseLeft,
	}))
	m = mm.(Model)
	mm, cmd := m.Update(tea.MouseReleaseMsg(tea.Mouse{
		X:      6,
		Y:      top + 1,
		Button: tea.MouseLeft,
	}))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("mouse release returned nil clipboard command")
	}
	if m.copyNotice != "text copied" {
		t.Fatalf("copy notice = %q, want %q", m.copyNotice, "text copied")
	}
	if !strings.Contains(stripANSI(viewText(m)), "text copied") {
		t.Fatalf("View() missing copy notice: %q", viewText(m))
	}

	got, ok := m.selectedText()
	if !ok {
		t.Fatal("drag did not create a text selection")
	}
	if want := "first message\nsecond"; got != want {
		t.Errorf("selected text = %q, want %q", got, want)
	}
}

func TestScrollbarClickJumpAndDrag(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	for i := 0; i < 80; i++ {
		m.items = append(m.items, transcriptItem{kind: itemNote, text: fmt.Sprintf("line %02d", i)})
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

func TestClickTogglesToolCard(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	title := "echo hi"
	m.items = append(m.items, transcriptItem{
		kind:      itemTool,
		collapsed: true,
		tool:      db.ToolCall{Tool: "bash", Status: "completed", Title: &title},
	})
	m.syncTranscript()
	if !m.items[0].collapsed {
		t.Fatal("tool should start collapsed")
	}

	top := lipgloss.Height(m.headerView()) + 1
	y := -1
	for row := top; row < top+m.transcriptRenderHeight(); row++ {
		if idx, ok := m.itemIndexAtScreenY(row); ok && idx == 0 {
			y = row
			break
		}
	}
	if y < 0 {
		t.Fatal("could not map a screen row to the tool card")
	}

	mm, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: 2, Y: y, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.items[0].collapsed {
		t.Fatal("click did not expand the tool card")
	}
	if m.selection.active {
		t.Fatal("click started a text selection")
	}
	if m.selectedItem != 0 {
		t.Fatalf("selectedItem = %d, want 0", m.selectedItem)
	}

	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: 2, Y: y, Button: tea.MouseLeft}))
	m = mm.(Model)
	if !m.items[0].collapsed {
		t.Fatal("second click did not collapse the tool card")
	}
}

func TestClickRunsSlashCommand(t *testing.T) {
	fake := newFakeProvider(t, 0, respBody("hi", "stop", nil))
	m := New(Options{Store: newTestStore(t), Client: newClient(fake.srv), Workdir: t.TempDir()})
	m = typeRune(m, '/')
	if !m.slashMode {
		t.Fatal("slash menu did not open")
	}

	y := -1
	for row := 0; row < m.height; row++ {
		idx, ok := m.slashIndexAtScreenY(row)
		if ok && idx < len(m.slashItems) && m.slashItems[idx].name == "/model" {
			y = row
			break
		}
	}
	if y < 0 {
		t.Fatal("could not map a screen row to /model")
	}

	mm, _ := m.Update(tea.MouseClickMsg(tea.Mouse{X: 2, Y: y, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.slashMode {
		t.Fatal("slash menu still open after click")
	}
	if !m.pickerMode {
		t.Fatal("click on /model did not open the picker")
	}
}
