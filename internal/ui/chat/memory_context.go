package chat

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/recap"
)

const memoryContextTimeout = 750 * time.Millisecond
const memoryContextPartCount = 2

// memoryProvider builds the wire-only aggregate plus request-specific recall.
// The aggregate is loaded for every parent turn; grep and model selection stay
// bounded by the existing recall path.
func (m Model) memoryProvider(ctx context.Context, _ string, userText string) (string, error) {
	aggregate, err := recap.LoadMemoryContext(m.workdir)
	if err != nil {
		return "", err
	}
	recall, recallErr := m.recall(ctx, "", userText)
	if recallErr != nil {
		recall = ""
	}
	parts := make([]string, 0, memoryContextPartCount)
	if strings.TrimSpace(aggregate) != "" {
		parts = append(parts, "MEMORIES\n"+aggregate)
	}
	if strings.TrimSpace(recall) != "" {
		parts = append(parts, recall)
	}
	return strings.Join(parts, "\n\n"), nil
}

func (m Model) openMemoryContext(extra string) Model {
	m = m.setFocus(focusSubagents)
	m.memoryContextMode = true
	m.subagentBuilt = true
	m.subagentPickerMode = true
	m.subagentLogMode = false
	m.subagentDrawerCompact = false
	m.memoryContext = ""
	ctx, cancel := context.WithTimeout(context.Background(), memoryContextTimeout)
	defer cancel()
	block, err := m.memoryProvider(ctx, "", strings.TrimSpace(extra))
	if err != nil {
		m.memoryContext = "memory context unavailable: " + err.Error()
	} else if strings.TrimSpace(block) == "" {
		m.memoryContext = "No stored memory or matched recap lines for the next turn."
	} else {
		m.memoryContext = block
	}
	m.memoryContextVp = viewport.New(
		viewport.WithWidth(max(1, m.width-1)),
		viewport.WithHeight(max(1, m.subagentDrawerVPHeight())),
	)
	m.memoryContextVp.SetContent(m.memoryContext)
	m = m.resizeSubagentDrawer()
	return m
}

func (m Model) memoryContextDrawerView() string {
	width := m.pickerDrawerWidth()
	meta := "injection on"
	if !m.memoryInjectionEnabled {
		meta = "injection off"
	}
	footer := "space/t toggle  •  esc close"
	body := withScrollbar(
		m.memoryContextVp.View(),
		max(1, width-1),
		max(1, m.memoryContextVp.Height()),
		m.memoryContextVp.ScrollPercent(),
		m.memoryContextVp.TotalLineCount() > m.memoryContextVp.Height(),
	)
	return drawerChrome("memory context", meta, body, footer, width)
}

func (m Model) memoryContextDrawerContent(width int) string {
	return lipgloss.NewStyle().Width(max(1, width)).MaxWidth(max(1, width)).Render(m.memoryContextVp.View())
}

func (m Model) updateMemoryContextKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	switch key.Code {
	case tea.KeyEscape, 'q', 'Q', 'x', 'X':
		return m.closeSubagentPicker(), nil
	case tea.KeySpace, 't', 'T':
		m.memoryInjectionEnabled = !m.memoryInjectionEnabled
		return m.openMemoryContext(""), nil
	case tea.KeyUp:
		m.memoryContextVp.ScrollUp(1)
	case tea.KeyDown:
		m.memoryContextVp.ScrollDown(1)
	case tea.KeyPgUp:
		m.memoryContextVp.PageUp()
	case tea.KeyPgDown:
		m.memoryContextVp.PageDown()
	}
	return m, nil
}
