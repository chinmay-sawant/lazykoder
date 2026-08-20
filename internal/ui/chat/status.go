package chat

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

type statusDrawerRow struct {
	name  string
	label string
	value string
}

const (
	statusDrawerExtraRows = 2
	statusDrawerLeftPad   = 3
)

// statusSegmentEnabled reports whether a named status detail is visible in
// the status drawer. An empty in-memory value means the default-on layout.
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

func (m Model) statusDrawerRows() []statusDrawerRow {
	rows := make([]statusDrawerRow, 0, len(db.StatusSegmentNames))
	for _, name := range db.StatusSegmentNames {
		rows = append(rows, statusDrawerRow{
			name:  name,
			label: statusSegmentLabel(name),
			value: m.statusSegmentValue(name),
		})
	}
	return rows
}

func statusSegmentLabel(name string) string {
	switch name {
	case "tps":
		return "tokens/sec"
	case "subs":
		return "sub-agents"
	default:
		return name
	}
}

func (m Model) statusSegmentValue(name string) string {
	switch name {
	case "model":
		if value := m.modelLabel(); value != "" {
			return value
		}
	case "variant":
		if m.variant != "" {
			return m.variant
		}
		return "default"
	case "tokens":
		return m.statusTokensValue()
	case "cache":
		hit, miss := m.cacheTotals()
		if hit > 0 || miss > 0 {
			return formatCache(hit, miss)
		}
	case "cost":
		_, subs, total := m.costTotals()
		if total > 0 || m.tokensUsed > 0 {
			if subs > 0 {
				return formatCost(total) + "  ·  subs " + formatCost(subs)
			}
			return formatCost(total)
		}
	case "tps":
		if value := m.tpsDisplayLabel(); value != "" {
			return value
		}
	case "subs":
		if value := m.subsStatusLabel(); value != "" {
			return value
		}
	case "models":
		if len(m.models) > 0 {
			return fmt.Sprintf("models:%d", len(m.models))
		}
	case "scroll":
		if m.transcript.TotalLineCount() > m.transcript.Height() {
			return "scroll ↑↓"
		}
		return "not needed"
	case "prompt":
		return m.promptStatusValue()
	}
	return "not available"
}

func (m Model) statusTokensValue() string {
	window := modelscache.ContextOf(m.modelInfos, m.modelLabel())
	switch {
	case m.tokensUsed > 0 && window > 0:
		return formatTokens(m.tokensUsed) + "/" + formatTokens(int64(window))
	case m.tokensUsed > 0:
		return formatTokens(m.tokensUsed)
	case window > 0:
		return "0/" + formatTokens(int64(window))
	default:
		return "not available"
	}
}

func (m Model) promptStatusValue() string {
	switch {
	case m.err != "":
		return "error"
	case m.compacting:
		return "compacting"
	case m.busy:
		return "working"
	default:
		return "enter send"
	}
}

func (m Model) statusDrawerView() string {
	width := m.pickerDrawerWidth()
	rows := m.statusDrawerRows()
	enabled := 0
	for _, row := range rows {
		if m.statusSegmentEnabled(row.name) {
			enabled++
		}
	}
	header := hintStyle.Render("status  ·  ") +
		lipgloss.NewStyle().Foreground(theme.ColorText()).Render(fmt.Sprintf("%d/%d enabled", enabled, len(rows)))
	if lipgloss.Width(header) > width {
		header = truncateRunes(header, width)
	}

	lines := make([]string, 0, len(rows)+statusDrawerExtraRows)
	lines = append(lines, header)
	for i, row := range rows {
		state := "off"
		if m.statusSegmentEnabled(row.name) {
			state = "on"
		}
		prefix := "  "
		if i == m.statusCursor {
			prefix = "▸ "
		}
		right := row.value + "  " + state
		leftWidth := max(1, width-lipgloss.Width(right)-statusDrawerLeftPad)
		left := truncateRunes(prefix+row.label, leftWidth)
		gap := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
		line := left + strings.Repeat(" ", gap) + right
		if i == m.statusCursor {
			line = lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Background(theme.ColorBorder()).Width(width).Render(line)
		} else {
			line = hintStyle.Width(width).Render(line)
		}
		lines = append(lines, line)
	}
	lines = append(lines, hintStyle.Width(width).Render(
		truncateRunes("↑/↓ select  •  enter toggle  •  ←/esc close", width),
	))
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(strings.Join(lines, "\n"))
}

func (m Model) statusDrawerTop() int {
	if !m.statusMode {
		return 0
	}
	drawerH := lipgloss.Height(m.statusDrawerView())
	bot := lipgloss.Height(m.composerBlock())
	if m.err != "" {
		bot += 1 + lipgloss.Height(errStyle.Width(max(minPaneWidth, m.width)).Render(m.err))
	}
	return max(0, m.height-bot-drawerH-1)
}

func (m Model) statusIndexAtScreenY(y int) (int, bool) {
	if !m.statusMode {
		return -1, false
	}
	idx := y - m.statusDrawerTop() - 1
	if idx < 0 || idx >= len(db.StatusSegmentNames) {
		return -1, false
	}
	return idx, true
}

func (m Model) statusChipLabel() string {
	return "status ▾"
}

func (m Model) openStatusDrawer() Model {
	m = m.setFocus(focusStatus)
	m.statusCursor = 0
	return m
}

func (m Model) updateStatusKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	switch key.Code {
	case tea.KeyEscape, tea.KeyLeft, 'q', 'Q', 's', 'S':
		return m.clearFocus(focusStatus), nil
	case tea.KeyUp:
		if m.statusCursor > 0 {
			m.statusCursor--
		}
		return m, nil
	case tea.KeyDown:
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
