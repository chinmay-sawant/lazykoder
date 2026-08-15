package chat

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

	mm, _ := m.Update(tea.MouseClickMsg(tea.Mouse{
		X:      0,
		Y:      titleBlockRows,
		Button: tea.MouseLeft,
	}))
	m = mm.(Model)
	mm, _ = m.Update(tea.MouseMotionMsg(tea.Mouse{
		X:      6,
		Y:      titleBlockRows + 1,
		Button: tea.MouseLeft,
	}))
	m = mm.(Model)
	mm, cmd := m.Update(tea.MouseReleaseMsg(tea.Mouse{
		X:      6,
		Y:      titleBlockRows + 1,
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
