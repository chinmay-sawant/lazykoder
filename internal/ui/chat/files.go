package chat

import (
	"io/fs"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

func (m Model) openFilePicker() Model {
	m.filePickerFilter = ""
	m.filePickerCursor = 0
	m.filePickerAt = strings.LastIndex(m.prompt.Value(), "@")
	if m.filePickerAt < 0 {
		m.filePickerAt = len(m.prompt.Value())
	}
	m.filePickerItems = listProjectFiles(m.workdir, "")
	m.filePickerMode = true
	return m
}

func (m Model) closeFilePicker() Model {
	m.filePickerMode = false
	m.filePickerItems = nil
	m.filePickerFilter = ""
	return m
}

func (m Model) filePickerOverlay() string {
	sel := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText())
	dim := lipgloss.NewStyle().Foreground(theme.ColorMute())
	var body strings.Builder
	if len(m.filePickerItems) == 0 {
		body.WriteString(dim.Render("no files match"))
	}
	for i, name := range m.filePickerItems {
		if i > 0 {
			body.WriteString("\n")
		}
		if i == m.filePickerCursor {
			body.WriteString(sel.Render("▸ " + name))
		} else {
			body.WriteString(dim.Render("  " + name))
		}
		if i >= 11 {
			break
		}
	}
	innerW := min(56, max(minPaneWidth, m.width-8))
	head := lipgloss.NewStyle().Bold(true).Render("@ " + m.filePickerFilter)
	foot := hintStyle.Render("↑/↓ select  •  enter insert  •  esc close")
	content := lipgloss.JoinVertical(lipgloss.Left, head, body.String(), foot)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorBorder()).
		Padding(0, 1).
		Width(innerW).
		Render(content)
}

func (m Model) updateFilePickerKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	switch key.Code {
	case tea.KeyEscape:
		return m.closeFilePicker(), nil
	case tea.KeyEnter:
		if m.filePickerCursor >= 0 && m.filePickerCursor < len(m.filePickerItems) {
			path := m.filePickerItems[m.filePickerCursor]
			val := m.prompt.Value()
			prefix := val
			if m.filePickerAt >= 0 && m.filePickerAt <= len(val) {
				prefix = val[:m.filePickerAt]
			}
			m.prompt.SetValue(prefix + "@" + path + " ")
			m = m.closeFilePicker()
			return m, nil
		}
		return m, nil
	case tea.KeyDown:
		if m.filePickerCursor < len(m.filePickerItems)-1 {
			m.filePickerCursor++
		}
		return m, nil
	case tea.KeyUp:
		if m.filePickerCursor > 0 {
			m.filePickerCursor--
		}
		return m, nil
	case tea.KeyBackspace:
		if len(m.filePickerFilter) > 0 {
			m.filePickerFilter = m.filePickerFilter[:len(m.filePickerFilter)-1]
			m.filePickerItems = listProjectFiles(m.workdir, m.filePickerFilter)
			m.filePickerCursor = 0
		}
		return m, nil
	}
	if key.Text != "" {
		m.filePickerFilter += key.Text
		m.filePickerItems = listProjectFiles(m.workdir, m.filePickerFilter)
		m.filePickerCursor = 0
	}
	return m, nil
}

func listProjectFiles(root, filter string) []string {
	if root == "" {
		return nil
	}
	filter = strings.ToLower(filter)
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", ".lazykoder", "bin", "node_modules":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		if filter != "" && !strings.Contains(strings.ToLower(rel), filter) {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		if len(out) >= 80 {
			return fs.SkipAll
		}
		return nil
	})
	return out
}
