package chat

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// slashView renders the slash command menu as a prompt-anchored two-pane card:
// the query is shown in an input-like row, followed by commands and details.
func (m Model) slashView() string {
	cardW := m.overlayWidth()
	innerW := max(minPaneWidth, cardW-cardBorder)

	sel := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	dim := lipgloss.NewStyle().Faint(true)
	leftW, rightW := splitPaneWidths(innerW)

	var leftB strings.Builder
	for i, cmd := range m.slashItems {
		if i > 0 {
			leftB.WriteString("\n")
		}
		if i == m.slashCursor {
			leftB.WriteString(sel.Render("▸ " + cmd.name))
		} else {
			leftB.WriteString(dim.Render("  " + cmd.name))
		}
	}
	left := lipgloss.NewStyle().Width(leftW).Render(leftB.String())

	detail := "no matching command"
	if len(m.slashItems) > 0 && m.slashCursor < len(m.slashItems) {
		detail = m.slashItems[m.slashCursor].description
	}
	right := lipgloss.NewStyle().Faint(true).Width(rightW).Render(detail)

	divider := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(" │ ")
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)
	query := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("8")).
		Width(max(slashQueryMinWidth, innerW)).
		Render(m.prompt.Value() + "▏")
	footer := hintStyle.Width(innerW).Render("↑/↓ select  •  enter run  •  esc close")
	content := lipgloss.JoinVertical(lipgloss.Left, query, body, footer)
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
	case 'j', tea.KeyDown:
		if m.slashCursor < len(m.slashItems)-1 {
			m.slashCursor++
		}
		return m, nil
	case 'k', tea.KeyUp:
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
		m.lines = nil
		m.lastTool = -1
		m.session = nil
		m.pendingUser = ""
		m.inputHistory = nil
		m.historyCursor = -1
		m.historyDraft = ""
		m.pendingHistoryIndex = -1
		m.promptUndo = nil
		m.slashFromPaste = false
		m.syncTranscript()
	case "/model":
		return m.openPicker(), nil
	case "/refresh":
		return m, m.refreshModels
	case "/help":
		m.lines = append(m.lines,
			"help: enter send  •  click model  •  / slash commands  •  ↑/↓ history  •  q quit")
		m.syncTranscript()
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
