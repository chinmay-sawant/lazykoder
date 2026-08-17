package chat

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

const statusPickerReserve = 2

// statusSegmentEnabled reports whether a named footer segment is visible.
func (m Model) statusSegmentEnabled(name string) bool {
	if len(m.statusSegments) == 0 {
		return true
	}
	for _, visible := range m.statusSegments {
		if visible == name {
			return true
		}
	}
	return false
}

func (m Model) statusPickerView(width int) string {
	parts := make([]string, 0, len(db.StatusSegmentNames)+statusPickerReserve)
	parts = append(parts, hintStyle.Render("status"))
	for i, name := range db.StatusSegmentNames {
		label := name + ":off"
		if m.statusSegmentEnabled(name) {
			label = name + ":on"
		}
		style := hintStyle
		if i == m.statusCursor {
			style = lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Background(theme.ColorBorder())
		}
		parts = append(parts, style.Render(" "+label+" "))
	}
	parts = append(parts, hintStyle.Render("←/→ move  enter toggle  esc close"))
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(truncateRunes(strings.Join(parts, "  "), width))
}

func (m Model) updateStatusKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	switch key.Code {
	case tea.KeyEscape, 'q', 'Q', 's', 'S':
		m.statusMode = false
		return m, nil
	case tea.KeyLeft, tea.KeyUp:
		if m.statusCursor > 0 {
			m.statusCursor--
		}
		return m, nil
	case tea.KeyRight, tea.KeyDown:
		if m.statusCursor < len(db.StatusSegmentNames)-1 {
			m.statusCursor++
		}
		return m, nil
	case tea.KeyEnter:
		if m.statusCursor < 0 || m.statusCursor >= len(db.StatusSegmentNames) {
			return m, nil
		}
		return m.toggleStatusSegment(db.StatusSegmentNames[m.statusCursor])
	}
	return m, nil
}

func (m Model) toggleStatusSegment(name string) (Model, tea.Cmd) {
	visible := make(map[string]bool, len(m.statusSegments))
	for _, segment := range m.statusSegments {
		visible[segment] = true
	}
	visible[name] = !visible[name]
	segments := make([]string, 0, len(m.statusSegments))
	for _, segment := range db.StatusSegmentNames {
		if visible[segment] {
			segments = append(segments, segment)
		}
	}
	if len(segments) == 0 {
		segments = db.DefaultStatusSegments()
	}
	m.statusSegments = segments
	if m.session == nil || m.store == nil {
		return m, nil
	}
	sessionID := m.session.ID
	saved := append([]string(nil), segments...)
	m.session.StatusSegments = append([]string(nil), saved...)
	return m, func() tea.Msg {
		if err := m.store.UpdateSessionSegments(context.Background(), sessionID, saved); err != nil {
			return errMsg{err: err}
		}
		return nil
	}
}
