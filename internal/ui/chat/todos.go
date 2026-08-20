package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/subagent"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

const (
	// maxTodoPanelRows is the visible body height; longer checklists scroll.
	maxTodoPanelRows = 6
	todoMarkPending  = "[ ]"
	todoMarkActive   = "[>]"
	todoMarkDone     = "[x]"
	todoMarkCancel   = "[-]"
)

// loadTodos refreshes the in-memory checklist from SQLite for the open session.
func (m Model) loadTodos() Model {
	if m.store == nil || m.session == nil {
		m.todos = nil
		m.todosExpanded = false
		m.todoVp.GotoTop()
		return m
	}
	items, err := m.store.ListTodos(context.Background(), m.session.ID)
	if err != nil {
		// Non-fatal: keep prior list and surface nothing in the strip.
		return m
	}
	m.todos = items
	m.todosExpanded = len(items) > 0
	m = m.resizeTodoPanel()
	return m.focusTodoViewport()
}

// applyTodosFromTool updates the checklist when a todowrite tool event lands.
// Applies on pending and completed so the strip appears as soon as the model
// emits the payload (pending is emitted before the DB write finishes).
// Expands the strip and collapses the /agents drawer so the checklist is not
// hidden under a tall sub-agent list.
func (m Model) applyTodosFromTool(tc db.ToolCall) Model {
	if tc.Tool != "todowrite" {
		return m
	}
	st := strings.ToLower(strings.TrimSpace(tc.Status))
	switch st {
	case "error", "denied", "cancelled", "canceled":
		return m
	}

	// Prefer the tool payload first: pending events fire before ReplaceTodos,
	// and completed events always carry the list the model just wrote.
	if items := parseTodosInputJSON(tc.InputJSON); len(items) > 0 {
		m.todos = items
	}
	// After a successful write, re-read SQLite so seq/status match storage.
	if (st == "completed" || st == "success" || st == "") && m.store != nil && m.session != nil {
		prev := m.todos
		m = m.loadTodos()
		if len(m.todos) == 0 && len(prev) > 0 {
			// Store lag or session not adopted yet: keep the payload list.
			m.todos = prev
		}
	}
	if len(m.todos) == 0 {
		return m
	}

	m = m.resizeTodoPanel()
	m = m.focusTodoViewport()
	// Model-driven checklist updates should surface the bodies immediately.
	m.todosExpanded = true
	// Keep agents visible as a compact summary strip under the checklist
	// (full list steals too many rows while todos are expanded).
	m = m.reloadSubagentRows()
	if len(m.subagentItems) > 0 {
		m = m.collapseSubagentDrawerToSummary()
	} else {
		follow := m.transcript.AtBottom()
		m.syncTranscript()
		if follow {
			m.transcript.GotoBottom()
		}
	}
	return m
}

// parseTodosInputJSON reads a todowrite arguments object into checklist rows.
func parseTodosInputJSON(raw string) []db.Todo {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var wrap struct {
		Todos []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"todos"`
	}
	if json.Unmarshal([]byte(raw), &wrap) != nil {
		return nil
	}
	out := make([]db.Todo, 0, len(wrap.Todos))
	for i, it := range wrap.Todos {
		content := strings.TrimSpace(it.Content)
		if content == "" {
			continue
		}
		out = append(out, db.Todo{
			Seq:     i,
			Content: content,
			Status:  normalizeTodoStatus(it.Status),
		})
	}
	return out
}

func normalizeTodoStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "in_progress", "in-progress", "active", "doing", "running":
		return db.TodoInProgress
	case "completed", "done", "complete", "finished", "success":
		return db.TodoCompleted
	case "cancelled", "canceled", "skipped":
		return db.TodoCancelled
	default:
		return db.TodoPending
	}
}

// todoPanelView is the tracker strip under the header (checklist style).
// Empty list renders nothing so the transcript is not pushed by a blank hole.
func (m Model) todoPanelView() string {
	if len(m.todos) == 0 {
		return ""
	}
	w := max(minPaneWidth, m.width)
	done, total, cancelled := 0, len(m.todos), 0
	for _, t := range m.todos {
		switch t.Status {
		case db.TodoCompleted:
			done++
		case db.TodoCancelled:
			cancelled++
		}
	}
	head := m.todoPanelHead(w, done, total, cancelled)
	if !m.todosExpanded {
		return head
	}

	m = m.resizeTodoPanel()
	body := withScrollbar(m.todoVp.View(), m.todoVp.Width(), m.todoVp.Height(),
		m.todoVp.ScrollPercent(), m.todoVp.TotalLineCount() > m.todoVp.Height())
	return lipgloss.JoinVertical(lipgloss.Left, head, body)
}

func (m Model) todoPanelBodyHeight() int {
	return min(maxTodoPanelRows, max(1, len(m.todos)))
}

func (m Model) todoPanelContent(width int) string {
	var b strings.Builder
	for i, t := range m.todos {
		if i > 0 {
			b.WriteString("\n")
		}
		mark, style := todoMarkStyle(t.Status)
		line := mark + " " + t.Content
		b.WriteString(style.Width(width).MaxWidth(width).Render(truncateRunes(line, width)))
	}
	return b.String()
}

func (m Model) resizeTodoPanel() Model {
	if len(m.todos) == 0 {
		m.todoVp.SetContent("")
		return m
	}
	width := max(minPaneWidth, m.width)
	m.todoVp.SetWidth(max(1, width-1))
	m.todoVp.SetHeight(m.todoPanelBodyHeight())
	m.todoVp.SetContent(m.todoPanelContent(m.todoVp.Width()))
	return m
}

// focusTodoViewport keeps the active row visible while a checklist is being
// updated. A completed or idle checklist opens at its newest rows instead of
// forcing a resumed session back to the first page.
func (m Model) focusTodoViewport() Model {
	if len(m.todos) == 0 {
		m.todoVp.GotoTop()
		return m
	}
	height := max(1, m.todoVp.Height())
	target := max(0, len(m.todos)-height)
	for i, todo := range m.todos {
		if todo.Status == db.TodoInProgress {
			target = max(0, i-height+1)
			break
		}
	}
	maxOffset := max(0, m.todoVp.TotalLineCount()-height)
	m.todoVp.SetYOffset(min(target, maxOffset))
	return m
}

func (m Model) todoPanelTop() int {
	return lipgloss.Height(m.headerView()) + 1
}

func (m Model) todoPanelHeaderAt(y int) bool {
	return len(m.todos) > 0 && y == m.todoPanelTop()
}

func (m Model) todoPanelBodyAt(y int) bool {
	if !m.todosExpanded || len(m.todos) == 0 {
		return false
	}
	top := m.todoPanelTop() + 1
	return y >= top && y < top+m.todoPanelBodyHeight()
}

func (m Model) todoPanelHead(w, done, total, cancelled int) string {
	head := hintStyle.Render("todos  ·  ")
	head += lipgloss.NewStyle().Foreground(theme.ColorText()).Render(fmt.Sprintf("%d/%d", done, total))
	if hasInProgressTodo(m.todos) {
		head += hintStyle.Render("  ·  ")
		head += lipgloss.NewStyle().Foreground(theme.ColorAccent()).Render("in progress")
	} else if done+cancelled >= total && total > 0 {
		head += hintStyle.Render("  ·  ")
		head += lipgloss.NewStyle().Foreground(theme.ColorGood()).Render("done")
		if summary := m.todoAgentSummary(); summary != "" {
			head += hintStyle.Render("  ·  " + summary)
		}
	}
	return lipgloss.NewStyle().Width(w).MaxWidth(w).Render(head)
}

// todoAgentSummary is a compact sub-agent result line for the completed
// checklist header, e.g. "4 agents · 4 ok · 0 failed".
func (m Model) todoAgentSummary() string {
	rows := m.subagentItems
	if len(rows) == 0 {
		rows = m.collectSubagentRows()
	}
	if len(rows) == 0 {
		return ""
	}
	ok, failed, live := 0, 0, 0
	for _, r := range rows {
		switch {
		case r.Live:
			live++
		case isFailedSubStatus(r.Status):
			failed++
		case subagent.IsTerminalStatus(r.Status):
			ok++
		default:
			// Still queued/running without Live set, or unknown in-flight.
			live++
		}
	}
	total := len(rows)
	if live > 0 {
		return fmt.Sprintf("%d agents · %d live · %d ok · %d failed", total, live, ok, failed)
	}
	return fmt.Sprintf("%d agents · %d ok · %d failed", total, ok, failed)
}

func (m Model) toggleTodos() Model {
	if len(m.todos) == 0 {
		return m
	}
	m.todosExpanded = !m.todosExpanded
	if !m.todosExpanded {
		m.todoVp.GotoTop()
	} else {
		m = m.focusTodoViewport()
	}
	m = m.resizeTodoPanel()
	follow := m.transcript.AtBottom()
	m.syncTranscript()
	if follow {
		m.transcript.GotoBottom()
	}
	return m
}

func hasInProgressTodo(items []db.Todo) bool {
	for _, t := range items {
		if t.Status == db.TodoInProgress {
			return true
		}
	}
	return false
}

func todoMarkStyle(status string) (string, lipgloss.Style) {
	mute := lipgloss.NewStyle().Foreground(theme.ColorMute())
	text := lipgloss.NewStyle().Foreground(theme.ColorText())
	good := lipgloss.NewStyle().Foreground(theme.ColorGood())
	accent := lipgloss.NewStyle().Foreground(theme.ColorAccent())
	switch status {
	case db.TodoInProgress:
		return todoMarkActive, accent
	case db.TodoCompleted:
		return todoMarkDone, good
	case db.TodoCancelled:
		return todoMarkCancel, mute
	default:
		return todoMarkPending, text
	}
}
