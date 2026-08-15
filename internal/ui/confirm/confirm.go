// Package confirm renders a full-view y/n confirm for destructive actions.
package confirm

import (
	"charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

var (
	errStyle   = lipgloss.NewStyle().Foreground(theme.ColorDanger())
	focusStyle = lipgloss.NewStyle().Foreground(theme.ColorAccent()).Bold(true)
	hintStyle  = lipgloss.NewStyle().Foreground(theme.ColorMute())
)

// Model is a full-view confirm (not an overlay).
type Model struct {
	subject   string
	qualifier string
	resolved  bool
	allow     bool
	result    *ResultMsg
}

// ResultMsg carries the human's decision to the caller.
type ResultMsg struct {
	Allow bool
}

// New returns an unresolved confirm for the given subject and qualifier.
func New(subject, qualifier string) Model {
	return Model{subject: subject, qualifier: qualifier}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update resolves the confirm on y/Y/n/N/esc/q, quits on ctrl+c, and ignores all other keys.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if m.resolved {
		return m, nil
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	if key.Mod.Contains(tea.ModCtrl) {
		if key.Code == 'c' {
			return m, tea.Quit
		}
		return m, nil
	}
	switch key.Code {
	case 'y', 'Y':
		return m.resolve(true), resultCmd(true)
	case 'n', 'N', 'q', 'Q', tea.KeyEsc:
		return m.resolve(false), resultCmd(false)
	default:
		return m, nil
	}
}

// View renders the three-line confirm layout.
func (m Model) View() string {
	return errStyle.Render("Delete ") +
		focusStyle.Render(m.subject) +
		errStyle.Render(" ("+m.qualifier+")?") +
		"\n\n" +
		hintStyle.Render("y confirm  •  n cancel")
}

// Result returns the pending result (nil until a confirm key resolves it).
func (m Model) Result() *ResultMsg {
	return m.result
}

func (m Model) resolve(allow bool) Model {
	m.resolved = true
	m.allow = allow
	m.result = &ResultMsg{Allow: allow}
	return m
}

func resultCmd(allow bool) tea.Cmd {
	return func() tea.Msg { return ResultMsg{Allow: allow} }
}
