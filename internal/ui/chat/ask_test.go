package chat

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestAskDialogWrapsQuestionAndOptions(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m.askMode = true
	m.askQuestion.Question = "This is a deliberately long question that must wrap inside the dialog instead of running into the right terminal edge."
	m.askQuestion.Header = "Choose one"
	m.askQuestion.Options = []string{
		"The first option also has enough text to require a wrapped continuation line.",
		"The second option remains selectable after the first option wraps.",
	}

	card := stripANSI(m.askOverlay())
	for _, line := range strings.Split(card, "\n") {
		if lipgloss.Width(line) > m.overlayWidth() {
			t.Fatalf("dialog line exceeds card width %d: width=%d line=%q", m.overlayWidth(), lipgloss.Width(line), line)
		}
	}
	for _, text := range []string{"deliberately long question", "wrapped continuation", "second option"} {
		if !strings.Contains(card, text) {
			t.Fatalf("wrapped dialog missing %q: %q", text, card)
		}
	}
	_, _, lines, spans := m.askOverlayLines()
	if len(spans) != 3 || spans[0].end-spans[0].start < 2 {
		t.Fatalf("option wrapping spans = %#v, lines=%d (expected 2 options + custom)", spans, len(lines))
	}
}

func TestAskDialogMouseSelectsWrappedOption(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	m.askMode = true
	m.askQuestion.Question = "Pick the option that should be selected."
	m.askQuestion.Options = []string{
		"A long first option that wraps over multiple rows so the second row is still part of option one.",
		"The target option",
	}
	left, top, _, _, ok := m.askOverlayRect()
	if !ok {
		t.Fatal("ask dialog rectangle unavailable")
	}
	_, _, _, spans := m.askOverlayLines()
	secondY := top + 2 + spans[1].start

	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: left + 3, Y: secondY, Button: tea.MouseLeft}))
	m = mm.(Model)
	if m.askMode {
		t.Fatal("clicking a wrapped option did not close the question")
	}

	m.askMode = true
	mm, _ = m.Update(tea.MouseClickMsg(tea.Mouse{X: 0, Y: 0, Button: tea.MouseLeft}))
	m = mm.(Model)
	if !m.askMode {
		t.Fatal("click outside the dialog reached the underlying chat")
	}
}
