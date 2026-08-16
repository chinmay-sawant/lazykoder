package chat

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

func TestTodoPanelRendersUnderHeader(t *testing.T) {
	st := newTestStore(t)
	workdir := t.TempDir()
	sess, err := st.CreateSession(context.Background(), db.Session{Directory: workdir, Title: "main"})
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	if err := st.ReplaceTodos(context.Background(), sess.ID, []db.Todo{
		{Content: "scaffold tracker", Status: db.TodoCompleted},
		{Content: "wire todowrite", Status: db.TodoInProgress},
		{Content: "tests", Status: db.TodoPending},
	}); err != nil {
		t.Fatalf("ReplaceTodos: %v", err)
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: workdir, Session: &sess})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = mm.(Model)
	if len(m.todos) != 3 {
		t.Fatalf("todos loaded = %d, want 3", len(m.todos))
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "todos") {
		t.Fatalf("missing todos header: %q", v)
	}
	if !strings.Contains(v, "1/3") && !strings.Contains(v, "scaffold") {
		// completed count may be 1
		t.Fatalf("missing progress or item: %q", v)
	}
	if !strings.Contains(v, "[x]") || !strings.Contains(v, "[>]") || !strings.Contains(v, "[ ]") {
		t.Fatalf("missing checklist marks: %q", v)
	}
	// Header brand still present above the strip.
	if !strings.Contains(v, "lazykoder") {
		t.Fatalf("missing brand: %q", v)
	}
}

func TestTodoPanelEmptyHides(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	if m.todoPanelView() != "" {
		t.Fatalf("empty panel should hide, got %q", m.todoPanelView())
	}
}
