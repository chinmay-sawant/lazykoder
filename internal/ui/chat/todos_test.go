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
	v = stripANSI(viewText(m))
	if !strings.Contains(v, "[x]") || !strings.Contains(v, "[>]") || !strings.Contains(v, "[ ]") {
		t.Fatalf("missing checklist marks after expand: %q", v)
	}
	// Header brand still present above the strip.
	if !strings.Contains(v, "lazykoder") {
		t.Fatalf("missing brand: %q", v)
	}
}

func TestTodoPanelExpandedWhenSessionResumed(t *testing.T) {
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
	if !m.todosExpanded {
		t.Fatal("stored todos should be expanded when the session is resumed")
	}
	panel := stripANSI(m.todoPanelView())
	if !strings.Contains(panel, "todos") || !strings.Contains(panel, "1/3") {
		t.Fatalf("missing progress: %q", panel)
	}
	for _, body := range []string{"scaffold tracker", "wire todowrite", "tests"} {
		if !strings.Contains(panel, body) {
			t.Fatalf("resumed panel missing todo %q: %q", body, panel)
		}
	}
	m = m.toggleTodos()
	if m.todosExpanded {
		t.Fatal("toggleTodos did not collapse the resumed checklist")
	}
	panel = stripANSI(m.todoPanelView())
	if strings.Contains(panel, "scaffold tracker") || strings.Contains(panel, "wire todowrite") || strings.Contains(panel, "tests") {
		t.Fatalf("collapsed panel still showed checklist bodies: %q", panel)
	}
	m = m.toggleTodos()
	if !m.todosExpanded {
		t.Fatal("toggleTodos did not re-expand the resumed checklist")
	}
	if !strings.Contains(stripANSI(m.todoPanelView()), "scaffold tracker") {
		t.Fatal("re-expanded panel missing checklist body")
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

func TestTodoPanelScrollsLongChecklist(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	for i := 0; i < maxTodoPanelRows+3; i++ {
		m.todos = append(m.todos, db.Todo{Content: fmt.Sprintf("todo-%d", i+1), Status: db.TodoPending})
	}
	m.todosExpanded = true
	m = m.resizeTodoPanel()
	panel := stripANSI(m.todoPanelView())
	if strings.Contains(panel, "more") || strings.Contains(panel, "…") {
		t.Fatalf("long checklist still uses a summary row: %q", panel)
	}
	if !strings.Contains(panel, "todo-1") || strings.Contains(panel, "todo-9") {
		t.Fatalf("initial checklist viewport is wrong: %q", panel)
	}
	if m.todoVp.TotalLineCount() <= m.todoVp.Height() {
		t.Fatalf("todo viewport did not overflow: lines=%d height=%d", m.todoVp.TotalLineCount(), m.todoVp.Height())
	}

	y := m.todoPanelTop() + 1
	mm, _ = m.Update(tea.MouseWheelMsg(tea.Mouse{X: 2, Y: y, Button: tea.MouseWheelDown}))
	m = mm.(Model)
	if m.todoVp.YOffset() == 0 {
		t.Fatal("todo wheel did not scroll its viewport")
	}
	panel = stripANSI(m.todoPanelView())
	if !strings.Contains(panel, "todo-9") {
		t.Fatalf("scrolled checklist did not reveal the final todo: %q", panel)
	}
}

func TestTodoPanelFollowsActiveTodo(t *testing.T) {
	m := New(Options{Store: newTestStore(t), Client: deadClient(), Workdir: t.TempDir()})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	for i := 0; i < maxTodoPanelRows+3; i++ {
		status := db.TodoPending
		if i == maxTodoPanelRows {
			status = db.TodoInProgress
		}
		m.todos = append(m.todos, db.Todo{Content: fmt.Sprintf("todo-%d", i+1), Status: status})
	}
	m.todosExpanded = true
	m = m.resizeTodoPanel()
	m = m.focusTodoViewport()

	if got, want := m.todoVp.YOffset(), 1; got != want {
		t.Fatalf("active todo offset = %d, want %d", got, want)
	}
	panel := stripANSI(m.todoPanelView())
	if strings.Contains(panel, "todo-1") {
		t.Fatalf("viewport stayed at the first todo: %q", panel)
	}
	if !strings.Contains(panel, "[>] todo-7") {
		t.Fatalf("active seventh todo is not visible or highlighted: %q", panel)
	}
}

func TestTodoPanelResumeStartsAtNewestRows(t *testing.T) {
	st := newTestStore(t)
	workdir := t.TempDir()
	sess, err := st.CreateSession(context.Background(), db.Session{Directory: workdir, Title: "main"})
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	items := make([]db.Todo, 0, maxTodoPanelRows+4)
	for i := 0; i < cap(items); i++ {
		items = append(items, db.Todo{Content: fmt.Sprintf("todo-%d", i+1), Status: db.TodoCompleted})
	}
	if err := st.ReplaceTodos(context.Background(), sess.ID, items); err != nil {
		t.Fatalf("ReplaceTodos: %v", err)
	}

	m := New(Options{Store: st, Client: deadClient(), Workdir: workdir, Session: &sess})
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = mm.(Model)
	if got, want := m.todoVp.YOffset(), len(items)-m.todoVp.Height(); got != want {
		t.Fatalf("resumed todo offset = %d, want newest-page offset %d", got, want)
	}
	panel := stripANSI(m.todoPanelView())
	if strings.Contains(panel, "[x] todo-1 ") || !strings.Contains(panel, "[x] todo-10") {
		t.Fatalf("resumed panel did not show newest todos: %q", panel)
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
	// Resumed sessions show the checklist body immediately.
	if !strings.Contains(v, "in progress") {
		t.Fatalf("view missing in progress: %q", v)
	}
	if !strings.Contains(v, "Launch 4 subagent audits") || !strings.Contains(v, "Capture screenshots") {
		t.Fatalf("view missing resumed checklist bodies: %q", v)
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
