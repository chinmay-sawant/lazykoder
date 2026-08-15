package chat

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const slashCardMaxWidth = 60

// slashView renders a compact command list above the prompt.
func (m Model) slashView() string {
	cardW := min(slashCardMaxWidth, max(minPaneWidth, m.width-4))
	sel := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	dim := lipgloss.NewStyle().Faint(true)
	var body strings.Builder
	for i, cmd := range m.slashItems {
		if i > 0 {
			body.WriteString("\n")
		}
		if i == m.slashCursor {
			body.WriteString(sel.Render("▸ " + cmd.name))
		} else {
			body.WriteString(dim.Render("  " + cmd.name))
		}
	}
	if len(m.slashItems) == 0 {
		body.WriteString(dim.Render("no matching command"))
	}
	detail := "no matching command"
	if len(m.slashItems) > 0 && m.slashCursor < len(m.slashItems) {
		detail = m.slashItems[m.slashCursor].description
	}
	footer := hintStyle.Width(cardW - cardBorder).Render(detail)
	content := lipgloss.JoinVertical(lipgloss.Left, body.String(), footer)
	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Width(cardW).
		Render(content)
	return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, card)
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
