package chat

import (
	"context"
	"fmt"
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

func TestApplyTodosFromToolExpandsAndCollapsesAgents(t *testing.T) {
	st := newTestStore(t)
	workdir := t.TempDir()
	sess, err := st.CreateSession(context.Background(), db.Session{Directory: workdir, Title: "main"})
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: workdir, Session: &sess})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = mm.(Model)
	// Real child sessions so reloadSubagentRows keeps them after todowrite.
	pid := sess.ID
	for i, name := range []string{"agent-1", "agent-2", "agent-3", "agent-4"} {
		if _, err := st.CreateSession(context.Background(), db.Session{
			Directory: workdir, Title: name, ParentSessionID: &pid, Kind: db.SessionKindSubagent,
			TimeCreated: int64(i + 1), TimeUpdated: int64(i + 1),
		}); err != nil {
			t.Fatalf("child %s: %v", name, err)
		}
	}
	m = m.openSubagentPicker()
	if !m.subagentPickerMode {
		t.Fatal("drawer should be open before todowrite")
	}
	if m.todosExpanded {
		t.Fatal("todos should start collapsed")
	}

	input := `{"todos":[
		{"content":"run agents","status":"completed"},
		{"content":"collect results","status":"completed"},
		{"content":"write report","status":"completed"},
		{"content":"ship","status":"completed"}
	]}`
	if err := st.ReplaceTodos(context.Background(), sess.ID, []db.Todo{
		{Content: "run agents", Status: db.TodoCompleted},
		{Content: "collect results", Status: db.TodoCompleted},
		{Content: "write report", Status: db.TodoCompleted},
		{Content: "ship", Status: db.TodoCompleted},
	}); err != nil {
		t.Fatalf("ReplaceTodos: %v", err)
	}
	m = m.applyTodosFromTool(db.ToolCall{
		Tool: "todowrite", Status: "completed", InputJSON: input,
	})
	if !m.todosExpanded {
		t.Fatal("todowrite should expand the checklist")
	}
	if !m.subagentPickerMode {
		t.Fatal("todowrite should keep the sub-agent drawer visible")
	}
	if !m.subagentDrawerCompact {
		t.Fatal("todowrite should collapse the drawer to the summary strip")
	}
	drawer := stripANSI(m.subagentDrawerView())
	if !strings.Contains(drawer, "sub-agents") || !strings.Contains(drawer, "4") {
		t.Fatalf("compact drawer missing summary: %q", drawer)
	}
	if strings.Contains(drawer, "agent-1") {
		t.Fatalf("compact drawer should not list full rows: %q", drawer)
	}
	panel := stripANSI(m.todoPanelView())
	if !strings.Contains(panel, "4/4") {
		t.Fatalf("missing 4/4 progress: %q", panel)
	}
	if !strings.Contains(panel, "done") {
		t.Fatalf("missing done mark: %q", panel)
	}
	if !strings.Contains(panel, "4 agents") {
		t.Fatalf("missing agent summary: %q", panel)
	}
	if !strings.Contains(panel, "run agents") {
		t.Fatalf("expanded bodies missing: %q", panel)
	}
}

func TestTranscriptFollowsBottomWithSubagentDrawer(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = mm.(Model)
	for i := 0; i < 80; i++ {
		m.items = append(m.items, transcriptItem{kind: itemNote, text: fmt.Sprintf("line %02d", i)})
	}
	m.syncTranscript()
	if !m.transcript.AtBottom() {
		t.Fatal("setup: should start at bottom")
	}
	m.subagentItems = []subagentRow{{ID: "a", Name: "worker", Status: "running", Live: true}}
	m = m.openSubagentPicker()
	if !m.subagentPickerMode {
		t.Fatal("drawer closed")
	}
	if !m.transcript.AtBottom() {
		t.Fatal("opening /agents should keep transcript at bottom when already following")
	}
	m.items = append(m.items, transcriptItem{kind: itemNote, text: "after-drawer-open"})
	m.syncTranscript()
	if !m.transcript.AtBottom() {
		t.Fatal("new content while drawer open should keep following the bottom")
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "after-drawer-open") {
		t.Fatalf("latest line not visible with drawer open: %q", v)
	}
}

func TestLoadTodosVisibleOnSessionOpen(t *testing.T) {
	st := newTestStore(t)
	workdir := t.TempDir()
	sess, err := st.CreateSession(context.Background(), db.Session{Directory: workdir, Title: "main"})
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	if err := st.ReplaceTodos(context.Background(), sess.ID, []db.Todo{
		{Content: "Launch 4 subagent audits", Status: db.TodoInProgress},
		{Content: "Capture screenshots", Status: db.TodoPending},
	}); err != nil {
		t.Fatalf("ReplaceTodos: %v", err)
	}
	m := New(Options{Store: st, Client: deadClient(), Workdir: workdir, Session: &sess})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = mm.(Model)
	if len(m.todos) != 2 {
		t.Fatalf("loaded todos = %d, want 2", len(m.todos))
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "todos") {
		t.Fatalf("view missing todos strip: %q", v)
	}
	// done/total counts only completed rows; 0 completed + in_progress shows 0/2.
	if !strings.Contains(v, "0/2") {
		t.Fatalf("view missing progress: %q", v)
	}
	// collapsed by default still shows the summary line under the header
	if !strings.Contains(v, "in progress") {
		t.Fatalf("view missing in progress: %q", v)
	}
}

func TestApplyTodosFromPendingEventShowsStrip(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 36})
	m = mm.(Model)
	input := `{"todos":[
		{"content":"spawn workers","status":"in_progress"},
		{"content":"collect results","status":"pending"}
	]}`
	// Pending is emitted before ReplaceTodos; UI must still show the strip.
	m = m.applyTodosFromTool(db.ToolCall{
		Tool: "todowrite", Status: "pending", InputJSON: input,
	})
	if len(m.todos) != 2 {
		t.Fatalf("todos = %d, want 2 from pending payload", len(m.todos))
	}
	if !m.todosExpanded {
		t.Fatal("pending todowrite should expand the strip")
	}
	v := stripANSI(viewText(m))
	if !strings.Contains(v, "todos") || !strings.Contains(v, "spawn workers") {
		t.Fatalf("pending todowrite not visible: %q", v)
	}
}
