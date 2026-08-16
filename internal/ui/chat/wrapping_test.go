package chat

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

const stamp = "03:43:20"

func bodyLines(m Model) []string {
	return strings.Split(stripANSI(strings.Join(m.renderedItems(), "\n")), "\n")
}

func clockColumn(lines []string) int {
	for _, line := range lines {
		if idx := strings.Index(line, stamp); idx >= 0 {
			return idx
		}
	}
	return -1
}

func TestAssistantTextStaysOutOfClockZone(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.items = []transcriptItem{
		{kind: itemUser, text: "hello", when: 1700000000000},
		{kind: itemAssistant, text: strings.Repeat("wordy ", 40), when: 1700000000000},
	}
	m.syncTranscript()
	lines := bodyLines(m)
	clockAt := clockColumn(lines)
	if clockAt < 0 {
		t.Fatalf("clock stamp missing from role line: %q", lines)
	}
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " ")
		if !strings.Contains(trimmed, "wordy") {
			continue
		}
		if w := lipgloss.Width(trimmed); w >= clockAt {
			t.Errorf("assistant text reaches the clock zone: width=%d clock at %d: %q", w, clockAt, trimmed)
		}
	}
}

func TestReasoningStaysOutOfClockZone(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	thought := strings.Repeat("planning ", 40)
	m.items = []transcriptItem{
		{kind: itemUser, text: "hello", when: 1700000000000},
		{kind: itemReasoning, text: thought, collapsed: false, when: 1700000000000},
	}
	m.syncTranscript()
	lines := bodyLines(m)
	clockAt := clockColumn(lines)
	if clockAt < 0 {
		t.Fatalf("clock stamp missing from role line: %q", lines)
	}
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " ")
		if !strings.Contains(trimmed, "planning") {
			continue
		}
		if w := lipgloss.Width(trimmed); w >= clockAt {
			t.Errorf("reasoning text reaches the clock zone: width=%d clock at %d: %q", w, clockAt, trimmed)
		}
	}
}

func TestUserFrameNeverSpillsPastPane(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.items = []transcriptItem{{kind: itemUser, text: strings.Repeat("a", 200), when: 1700000000000}}
	m.syncTranscript()
	for _, line := range bodyLines(m) {
		trimmed := strings.TrimRight(line, " ")
		if !strings.Contains(trimmed, "╭") && !strings.Contains(trimmed, "╰") {
			continue
		}
		if w := lipgloss.Width(trimmed); w > m.width {
			t.Errorf("user frame spills past the pane: width=%d pane=%d: %q", w, m.width, trimmed)
		}
	}
}

func TestAssistantTextWithoutStampWrapsFullPane(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.items = []transcriptItem{
		{kind: itemUser, text: "hello"},
		{kind: itemAssistant, text: strings.Repeat("wordy ", 40)},
	}
	m.syncTranscript()
	lines := bodyLines(m)
	maxContent := 0
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " ")
		if strings.Contains(trimmed, "wordy") {
			if w := lipgloss.Width(trimmed); w > maxContent {
				maxContent = w
			}
		}
	}
	want := 2 + max(minPaneWidth, m.width-1-2)
	if maxContent > want {
		t.Errorf("stamp-less assistant content too wide: %d > %d", maxContent, want)
	}
}

func TestLiveStreamedTextStaysOutOfClockZone(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	m.width = 80
	m.busy = true
	m.items = []transcriptItem{{kind: itemUser, text: "hello", when: 1700000000000}}
	long := strings.Repeat("streaming ", 40)
	m.applyPart(db.Part{ID: "prt_live", Type: "text", Text: &long})
	m.syncTranscript()
	lines := bodyLines(m)
	clockAt := clockColumn(lines)
	if clockAt < 0 {
		t.Fatalf("clock stamp missing from role line: %q", lines)
	}
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " ")
		if !strings.Contains(trimmed, "streaming") {
			continue
		}
		if w := lipgloss.Width(trimmed); w >= clockAt {
			t.Errorf("streamed text reaches the clock zone: width=%d clock at %d: %q", w, clockAt, trimmed)
		}
	}
}
