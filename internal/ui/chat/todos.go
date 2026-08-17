package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

const (
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
		return m
	}
	items, err := m.store.ListTodos(context.Background(), m.session.ID)
	if err != nil {
		// Non-fatal: keep prior list and surface nothing in the strip.
		return m
	}
	m.todos = items
	return m
}

// applyTodosFromTool updates the checklist when a completed todowrite lands.
func (m Model) applyTodosFromTool(tc db.ToolCall) Model {
	if tc.Tool != "todowrite" {
		return m
	}
	if st := strings.ToLower(tc.Status); st != "" && st != "completed" && st != "success" {
		return m
	}
	// Prefer live re-read so seq/status match the store.
	if m.store != nil && m.session != nil {
		return m.loadTodos()
	}
	// Fallback: parse input JSON if store/session not ready yet.
	var wrap struct {
		Todos []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"todos"`
	}
	if json.Unmarshal([]byte(tc.InputJSON), &wrap) != nil {
		return m
	}
	out := make([]db.Todo, 0, len(wrap.Todos))
	for i, it := range wrap.Todos {
		content := strings.TrimSpace(it.Content)
		if content == "" {
			continue
		}
		st := strings.ToLower(strings.TrimSpace(it.Status))
		switch st {
		case "in_progress", "in-progress", "active", "doing", "running":
			st = db.TodoInProgress
		case "completed", "done", "complete", "finished", "success":
			st = db.TodoCompleted
		case "cancelled", "canceled", "skipped":
			st = db.TodoCancelled
		default:
			st = db.TodoPending
		}
		out = append(out, db.Todo{Seq: i, Content: content, Status: st})
	}
	m.todos = out
	return m
}

// todoPanelView is the tracker strip under the header (checklist style).
// Empty list renders nothing so the transcript is not pushed by a blank hole.
func (m Model) todoPanelView() string {
	if len(m.todos) == 0 {
		return ""
	}
	w := max(minPaneWidth, m.width)
	done, total := 0, len(m.todos)
	for _, t := range m.todos {
		if t.Status == db.TodoCompleted {
			done++
		}
	}
	head := hintStyle.Render("todos  ·  ")
	head += lipgloss.NewStyle().Foreground(theme.ColorText()).Render(fmt.Sprintf("%d/%d", done, total))
	if hasInProgressTodo(m.todos) {
		head += hintStyle.Render("  ·  ")
		head += lipgloss.NewStyle().Foreground(theme.ColorAccent()).Render("in progress")
	}
	head = lipgloss.NewStyle().Width(w).MaxWidth(w).Render(head)
	if !m.todosExpanded {
		return head
	}

	var b strings.Builder
	b.WriteString(head)
	shown := 0
	for _, t := range m.todos {
		if shown >= maxTodoPanelRows {
			rest := total - shown
			b.WriteString("\n")
			b.WriteString(hintStyle.Width(w).Render(fmt.Sprintf("  … %d more", rest)))
			break
		}
		mark, style := todoMarkStyle(t.Status)
		line := mark + " " + t.Content
		b.WriteString("\n")
		b.WriteString(style.Width(w).MaxWidth(w).Render(truncateRunes(line, w)))
		shown++
	}
	return b.String()
}

func (m Model) toggleTodos() Model {
	if len(m.todos) == 0 {
		return m
	}
	m.todosExpanded = !m.todosExpanded
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
