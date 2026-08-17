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
	if !strings.Contains(v, "1/3") {
		t.Fatalf("missing progress: %q", v)
	}
	if !strings.Contains(v, "in progress") {
		t.Fatalf("missing in-progress mark: %q", v)
	}
	m = m.toggleTodos()
	v = stripANSI(viewText(m))
	if !strings.Contains(v, "[x]") || !strings.Contains(v, "[>]") || !strings.Contains(v, "[ ]") {
		t.Fatalf("missing checklist marks after expand: %q", v)
	}
	// Header brand still present above the strip.
	if !strings.Contains(v, "lazykoder") {
		t.Fatalf("missing brand: %q", v)
	}
}

func TestTodoPanelCollapsedByDefault(t *testing.T) {
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
	if m.todosExpanded {
		t.Fatal("todosExpanded should default false")
	}
	panel := stripANSI(m.todoPanelView())
	if strings.Contains(panel, "\n") {
		t.Fatalf("collapsed panel should be one line, got %q", panel)
	}
	if !strings.Contains(panel, "todos") || !strings.Contains(panel, "1/3") {
		t.Fatalf("missing summary: %q", panel)
	}
	bodies := 0
	for _, body := range []string{"scaffold tracker", "wire todowrite", "tests"} {
		if strings.Contains(panel, body) {
			bodies++
		}
	}
	if bodies == 3 {
		t.Fatalf("collapsed panel showed all bodies: %q", panel)
	}
	v := stripANSI(viewText(m))
	if strings.Contains(v, "[x]") && strings.Contains(v, "[>]") && strings.Contains(v, "[ ]") {
		t.Fatalf("default View showed all checklist bodies: %q", v)
	}

	m = m.toggleTodos()
	if !m.todosExpanded {
		t.Fatal("toggleTodos did not expand")
	}
	panel = stripANSI(m.todoPanelView())
	if !strings.Contains(panel, "scaffold tracker") || !strings.Contains(panel, "wire todowrite") || !strings.Contains(panel, "tests") {
		t.Fatalf("expanded missing bodies: %q", panel)
	}
	m = m.toggleTodos()
	if m.todosExpanded {
		t.Fatal("toggleTodos did not collapse")
	}
	if strings.Contains(stripANSI(m.todoPanelView()), "\n") {
		t.Fatalf("re-collapsed panel should be one line: %q", m.todoPanelView())
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
