package chat

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// slashView renders a full-width command list above the prompt. Each row
// is the command name on the left and its description after it.
func (m Model) slashView() string {
	cardW := max(minPaneWidth, m.width-cardBorder)
	sel := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	dim := lipgloss.NewStyle().Faint(true)
	nameW := 0
	for _, cmd := range m.slashItems {
		if w := lipgloss.Width(cmd.name); w > nameW {
			nameW = w
		}
	}
	var body strings.Builder
	if len(m.slashItems) == 0 {
		body.WriteString(dim.Render("no matching command"))
	} else {
		for i, cmd := range m.slashItems {
			if i > 0 {
				body.WriteString("\n")
			}
			prefix := "  "
			if i == m.slashCursor {
				prefix = "▸ "
			}
			gap := max(2, nameW-lipgloss.Width(cmd.name)+2)
			line := prefix + cmd.name + strings.Repeat(" ", gap) + cmd.description
			if lipgloss.Width(line) > cardW {
				line = truncateRunes(line, cardW)
			}
			if i == m.slashCursor {
				body.WriteString(sel.Render(line))
			} else {
				body.WriteString(dim.Render(line))
			}
		}
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Width(cardW).
		Render(body.String())
}

// updateSlash handles keys while the slash menu is open.
func (m Model) updateSlashKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	m = m.clearTextSelection()
	switch key.Code {
	case tea.KeyEscape:
		m.slashMode = false
		m.slashCursor = 0
		m.prompt.SetValue("/")
		return m, nil
	case tea.KeyEnter:
		if m.slashCursor >= 0 && m.slashCursor < len(m.slashItems) {
			name := m.slashItems[m.slashCursor].name
			m.slashMode = false
			m.slashCursor = 0
			m.slashFromPaste = false
			m.prompt.SetValue("")
			m.promptUndo = nil
			return m.runSlash(name)
		}
		return m, nil
	case tea.KeyBackspace:
		m = m.rememberPrompt()
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(key)
		return m.syncSlash(m.prompt.Value()), cmd
	case tea.KeyDown:
		if m.slashCursor < len(m.slashItems)-1 {
			m.slashCursor++
		}
		return m, nil
	case tea.KeyUp:
		if m.slashCursor > 0 {
			m.slashCursor--
		}
		return m, nil
	}
	if key.Text != "" {
		m = m.rememberPrompt()
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(key)
		return m.syncSlash(m.prompt.Value()), cmd
	}
	return m, nil
}

// runSlash executes a chosen slash command.
func (m Model) runSlash(name string) (Model, tea.Cmd) {
	switch name {
	case "/new":
		return m.loadSession(nil), nil
	case "/sessions":
		return m.openSessionPicker(), nil
	case "/model":
		return m.openPicker(), nil
	case "/refresh":
		return m, m.refreshModels
	case "/help":
		m.helpMode = true
	}
	return m, nil
}

// syncSlash recomputes the slash menu from the prompt text. The menu opens
// when the prompt starts with "/" and closes when it no longer does.
func (m Model) syncSlash(value string) Model {
	if !strings.HasPrefix(value, "/") {
		m.slashMode = false
		m.slashCursor = 0
		m.slashFromPaste = false
		return m
	}
	partial := strings.ToLower(strings.TrimPrefix(value, "/"))
	m.slashItems = nil
	for _, cmd := range slashCommands {
		if strings.HasPrefix(strings.TrimPrefix(cmd.name, "/"), partial) {
			m.slashItems = append(m.slashItems, cmd)
		}
	}
	if m.slashCursor >= len(m.slashItems) {
		m.slashCursor = max(0, len(m.slashItems)-1)
	}
	m.slashMode = true
	return m
}

func (m Model) syncPromptSlash() Model {
	if m.slashFromPaste {
		if !strings.HasPrefix(m.prompt.Value(), "/") {
			m.slashFromPaste = false
		}
		m.slashMode = false
		m.slashCursor = 0
		return m
	}
	return m.syncSlash(m.prompt.Value())
}
