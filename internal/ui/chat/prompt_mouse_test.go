package chat

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

func TestPromptClickMovesCursor(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)

	// Four logical rows in the composer.
	text := "line one alpha\nline two bravo\nline three charlie\nline four delta"
	m.prompt.SetValue(text)
	m.prompt.SetWidth(m.promptContentWidth())
	m.prompt.SetHeight(m.promptHeight())
	m.prompt.MoveToEnd()
	if m.prompt.Line() != 3 {
		t.Fatalf("setup line = %d, want 3", m.prompt.Line())
	}

	left, top, _, h := m.promptBoxMetrics()
	if h < 2 {
		t.Fatalf("prompt height %d too small for multi-line click", h)
	}

	// Click row 1 (second line), near the start of "line two".
	clickY := top + 1
	clickX := left + 2
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: clickX, Y: clickY, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.prompt.Line() != 1 {
		t.Fatalf("after click line = %d, want 1 (second row)", m.prompt.Line())
	}
	// Cursor should not still be at the end of the document.
	if m.prompt.Line() == 3 && m.prompt.Column() > 5 {
		t.Fatal("cursor stayed at end after click")
	}
}

func TestPromptClickColumnMatchesPaintedChar(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	// Unique letters so each column maps to a distinct rune.
	m.prompt.SetValue("ABCDEFGHIJ")
	m.prompt.SetWidth(m.promptContentWidth())
	m.prompt.SetHeight(m.promptHeight())
	m.prompt.MoveToEnd()

	left, top, _, _ := m.promptBoxMetrics()
	// Click the painted column of 'E' (0-based content index 4).
	clickX := left + 4
	off, ok := m.promptOffsetAtScreen(clickX, top)
	if !ok {
		t.Fatal("offset not found")
	}
	runes := []rune(m.prompt.Value())
	if off < 0 || off >= len(runes) {
		t.Fatalf("offset %d out of range", off)
	}
	if runes[off] != 'E' {
		t.Fatalf("click at content col 4 mapped to %q (off=%d), want E", string(runes[off]), off)
	}
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: clickX, Y: top, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.prompt.Line() != 0 || m.prompt.Column() != 4 {
		t.Fatalf("cursor at line=%d col=%d, want 0,4", m.prompt.Line(), m.prompt.Column())
	}
}

func TestPromptDragSelectsRange(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	text := "abcdefghij\nklmnopqrst\nuvwxyz1234"
	m.prompt.SetValue(text)
	m.prompt.SetHeight(m.promptHeight())

	left, top, _, _ := m.promptBoxMetrics()
	// Drag from start of first line into second line.
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: left + 0, Y: top, Button: tea.MouseLeft}))
	m = mm.(Model)
	if !m.promptSel.dragging {
		t.Fatal("click should start prompt drag selection")
	}
	mm, _ = m.Update(tea.MouseMotionMsg(tea.Mouse{X: left + 5, Y: top + 1, Button: tea.MouseLeft}))
	m = mm.(Model)
	mm, _ = m.Update(tea.MouseReleaseMsg(tea.Mouse{X: left + 5, Y: top + 1, Button: tea.MouseLeft}))
	m = mm.(Model)
	if !m.promptSel.hasRange() {
		t.Fatalf("expected a selected range, sel=%+v", m.promptSel)
	}
	got, ok := m.selectedPromptText()
	if !ok || got == "" {
		t.Fatal("selectedPromptText empty after drag")
	}
	if !strings.Contains(got, "abc") {
		t.Fatalf("selection missing start of drag: %q", got)
	}
	// Painted view should include selection styling (or at least keep the text).
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "abcdefghij") {
		t.Fatalf("composer missing after select: %q", v)
	}
}

func TestPromptOffsetMappingRoundTrip(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m = mm.(Model)
	value := "short\nsecond line here\nthird"
	m.prompt.SetValue(value)
	m.prompt.SetWidth(40)
	// Offset of 's' in "second"
	off := utf8.RuneCountInString("short\n") // start of second line
	row, col := promptLogicalAtOffset(value, off+2)
	if row != 1 || col != 2 {
		t.Fatalf("logical = %d,%d want 1,2", row, col)
	}
	m = m.setPromptCursorOffset(off + 2)
	if m.prompt.Line() != 1 {
		t.Fatalf("cursor line = %d want 1", m.prompt.Line())
	}
}

func TestHardWrapRunes(t *testing.T) {
	segs := hardWrapRunes([]rune("abcdefghij"), 4)
	if len(segs) != 3 {
		t.Fatalf("segs = %d want 3: %q", len(segs), segs)
	}
	if string(segs[0]) != "abcd" || string(segs[1]) != "efgh" {
		t.Fatalf("unexpected segs: %q %q", segs[0], segs[1])
	}
}
