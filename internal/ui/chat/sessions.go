package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
)

func (m Model) openSessionPicker() Model {
	if m.store == nil {
		m.sessionItems = nil
		m.sessionCursor = 0
		m.sessionPickerMode = true
		return m.refreshSessionPicker()
	}
	sessions, err := m.store.ListSessionsByDir(context.Background(), m.workdir)
	if err != nil {
		m.err = "sessions: " + err.Error()
		return m
	}
	m.sessionItems = sessions
	m.sessionCursor = 0
	for i, sess := range sessions {
		if m.session != nil && sess.ID == m.session.ID {
			m.sessionCursor = i
			break
		}
	}
	if !m.sessionBuilt {
		m.sessionVp = viewport.New(viewport.WithWidth(pickerVpDefaultW), viewport.WithHeight(pickerVpDefaultH))
		m.sessionBuilt = true
	}
	m.sessionPickerMode = true
	return m.resizeSessionPicker().refreshSessionPicker()
}

func (m Model) closeSessionPicker() Model {
	m.sessionPickerMode = false
	return m
}

func (m Model) sessionPickerScreen() string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.sessionPickerView())
}

func (m Model) sessionPickerView() string {
	cardW := m.overlayWidth()
	innerW := max(minPaneWidth, cardW-cardBorder)
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Width(innerW).Render(" SESSIONS")
	body := hintStyle.Render("no sessions")
	if len(m.sessionItems) > 0 {
		vpH := m.sessionVPHeight()
		body = withScrollbar(m.sessionVp.View(), m.sessionVp.Width(), vpH,
			m.sessionVp.ScrollPercent(), m.sessionVp.TotalLineCount() > vpH)
	}
	footer := hintStyle.Width(innerW).Render("j/k select  •  enter open  •  esc cancel")
	content := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Width(cardW).
		Render(content)
}

func (m Model) sessionPickerContent() string {
	sel := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	normal := lipgloss.NewStyle().Faint(true)
	var b strings.Builder
	for i, sess := range m.sessionItems {
		if i > 0 {
			b.WriteString("\n")
		}
		line := sessionPickerLine(sess)
		if i == m.sessionCursor {
			b.WriteString(sel.Render("▸ " + line))
			continue
		}
		b.WriteString(normal.Render("  " + line))
	}
	return b.String()
}

func sessionPickerLine(sess db.Session) string {
	title := strings.TrimSpace(sess.Title)
	if title == "" {
		title = "untitled"
	}
	model := sess.Model
	if model == "" {
		model = "default"
	}
	return fmt.Sprintf("%s  %s  %s", title, formatSessionAge(sess.TimeUpdated), model)
}

func formatSessionAge(ms int64) string {
	if ms <= 0 {
		return ""
	}
	d := time.Since(time.UnixMilli(ms))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func (m Model) sessionVPHeight() int {
	available := max(minPaneHeight, m.height*70/percentBase-pickerFixedRows)
	return min(max(minPaneHeight, len(m.sessionItems)), min(pickerMaxRows, available))
}

func (m Model) resizeSessionPicker() Model {
	if !m.sessionBuilt {
		return m
	}
	innerW := max(minPaneWidth, m.overlayWidth()-cardBorder)
	m.sessionVp.SetWidth(max(pickerVpMinWidth, innerW))
	m.sessionVp.SetHeight(m.sessionVPHeight())
	return m
}

func (m Model) refreshSessionPicker() Model {
	if !m.sessionBuilt {
		return m
	}
	m.sessionVp.SetHeight(m.sessionVPHeight())
	m.sessionVp.SetContent(m.sessionPickerContent())
	if len(m.sessionItems) > 0 {
		m.sessionVp.EnsureVisible(m.sessionCursor, 0, 1)
	}
	return m
}

func (m Model) updateSessionPickerKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	if key.Mod.Contains(tea.ModCtrl) && key.Code == 'c' {
		return m.closeDone(), tea.Quit
	}
	switch key.Code {
	case 'q', 'Q', tea.KeyEscape:
		return m.closeSessionPicker(), nil
	case tea.KeyEnter:
		if m.sessionCursor >= 0 && m.sessionCursor < len(m.sessionItems) {
			sess := m.sessionItems[m.sessionCursor]
			m = m.closeSessionPicker()
			return m.loadSession(&sess), nil
		}
		return m, nil
	case 'j', tea.KeyDown:
		if m.sessionCursor < len(m.sessionItems)-1 {
			m.sessionCursor++
			m = m.refreshSessionPicker()
		}
		return m, nil
	case 'k', tea.KeyUp:
		if m.sessionCursor > 0 {
			m.sessionCursor--
			m = m.refreshSessionPicker()
		}
		return m, nil
	case tea.KeyPgDown:
		m.sessionVp.PageDown()
		return m, nil
	case tea.KeyPgUp:
		m.sessionVp.PageUp()
		return m, nil
	}
	return m, nil
}

func (m Model) loadSession(sess *db.Session) Model {
	m.items = nil
	m.selectedItem = -1
	m.lastTool = -1
	m.pendingUser = ""
	m.inputHistory = nil
	m.historyCursor = -1
	m.historyDraft = ""
	m.pendingHistoryIndex = -1
	m.promptUndo = nil
	m.slashFromPaste = false
	m.err = ""
	m.prompt.SetValue("")
	m.session = sess
	if sess != nil {
		m.model = sess.Model
		if m.store != nil {
			m.replay(sess.ID)
		}
	} else {
		m.syncTranscript()
	}
	return m
}
