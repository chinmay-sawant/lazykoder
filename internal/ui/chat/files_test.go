package chat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestAtPickerFilesReachable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: dir})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = mm.(Model)
	for i := 0; i < 15; i++ {
		m.subagentItems = append(m.subagentItems, subagentRow{
			ID:     fmt.Sprintf("agent-%02d", i),
			Name:   fmt.Sprintf("agent-%02d", i),
			Status: "completed",
		})
	}
	m = m.openFilePicker()
	if !m.filePickerMode {
		t.Fatal("picker did not open")
	}
	if n := len(m.filePickerItems); n < 16 {
		t.Fatalf("items = %d, want agents + hello.go", n)
	}
	if m.filePickerCursor < 0 || m.filePickerCursor >= len(m.filePickerItems) {
		t.Fatalf("cursor %d outside items 0..%d", m.filePickerCursor, len(m.filePickerItems)-1)
	}

	overlay := stripANSI(m.filePickerOverlay())
	if !strings.Contains(overlay, "sub-agents") {
		t.Fatalf("missing sub-agents section: %q", overlay)
	}
	if !cursorRowPainted(overlay, m) {
		t.Fatalf("cursor %d not painted: %q", m.filePickerCursor, overlay)
	}

	fileIdx := -1
	for i, it := range m.filePickerItems {
		if it.Kind == atKindFile && strings.Contains(it.Label, "hello.go") {
			fileIdx = i
			break
		}
	}
	if fileIdx < 0 {
		t.Fatalf("hello.go not in items: %+v", m.filePickerItems)
	}
	if strings.Contains(overlay, "hello.go") {
		if !strings.Contains(overlay, "files") {
			t.Fatalf("hello.go visible without files section: %q", overlay)
		}
	} else {
		for m.filePickerCursor < fileIdx {
			m = upd(m, tea.KeyPressMsg{Code: tea.KeyDown})
		}
		overlay = stripANSI(m.filePickerOverlay())
		if !strings.Contains(overlay, "hello.go") {
			t.Fatalf("hello.go still hidden after moving to file row %d: %q", fileIdx, overlay)
		}
		if !strings.Contains(overlay, "files") {
			t.Fatalf("missing files section at file row: %q", overlay)
		}
	}
	if m.filePickerCursor != fileIdx {
		t.Fatalf("cursor = %d, want file row %d", m.filePickerCursor, fileIdx)
	}
	if !cursorRowPainted(overlay, m) {
		t.Fatalf("file cursor %d not painted: %q", m.filePickerCursor, overlay)
	}
	start, end := atPickerWindow(len(m.filePickerItems), m.filePickerCursor, maxAtPickerVisible)
	if end-start > maxAtPickerVisible {
		t.Fatalf("painted item window %d..%d exceeds %d", start, end, maxAtPickerVisible)
	}
	if m.filePickerCursor < start || m.filePickerCursor >= end {
		t.Fatalf("cursor %d outside painted window %d..%d", m.filePickerCursor, start, end)
	}

	last := len(m.filePickerItems) - 1
	for i := 0; i < last+8; i++ {
		m = upd(m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if m.filePickerCursor != last {
		t.Fatalf("cursor = %d after extra downs, want last %d", m.filePickerCursor, last)
	}
	start, end = atPickerWindow(len(m.filePickerItems), m.filePickerCursor, maxAtPickerVisible)
	if m.filePickerCursor < start || m.filePickerCursor >= end {
		t.Fatalf("last cursor %d outside painted window %d..%d", m.filePickerCursor, start, end)
	}
}

func TestFilePickerOverlaySections(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: dir})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = mm.(Model)
	m.subagentItems = []subagentRow{{ID: "one", Name: "lint-fix", Status: "completed"}}
	m = m.openFilePicker()
	overlay := stripANSI(m.filePickerOverlay())
	if !strings.Contains(overlay, "sub-agents") || !strings.Contains(overlay, "files") {
		t.Fatalf("missing sections: %q", overlay)
	}
	if !strings.Contains(overlay, "hello.go") {
		t.Fatalf("overlay missing hello.go: %q", overlay)
	}
	if !strings.Contains(overlay, "lint-fix") {
		t.Fatalf("overlay missing agent: %q", overlay)
	}
	if !strings.Contains(overlay, "@ files & sub-agents") {
		t.Fatalf("missing header: %q", overlay)
	}
	if !strings.Contains(overlay, "enter insert") {
		t.Fatalf("missing footer: %q", overlay)
	}
	// Agent is one row: name and status share the line.
	var agentLine string
	for _, line := range strings.Split(overlay, "\n") {
		if strings.Contains(line, "lint-fix") {
			agentLine = line
			break
		}
	}
	if agentLine == "" || !strings.Contains(agentLine, "agent") {
		t.Fatalf("agent row missing label: %q", overlay)
	}
	if !strings.Contains(agentLine, "completed") && !strings.Contains(agentLine, "◆") {
		t.Fatalf("agent status not on the same row: %q", agentLine)
	}
}

func cursorRowPainted(overlay string, m Model) bool {
	if m.filePickerCursor < 0 || m.filePickerCursor >= len(m.filePickerItems) {
		return false
	}
	it := m.filePickerItems[m.filePickerCursor]
	needle := it.Label
	if needle == "" {
		return false
	}
	start, end := atPickerWindow(len(m.filePickerItems), m.filePickerCursor, maxAtPickerVisible)
	if m.filePickerCursor < start || m.filePickerCursor >= end {
		return false
	}
	return strings.Contains(overlay, needle)
}
