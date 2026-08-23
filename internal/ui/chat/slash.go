package chat

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

// slashCompactMaxWidth is the compact slash palette card width cap.
const slashCompactMaxWidth = 100

// slashMinNameGap is the minimum gap (columns) between a slash name and its
// description, plus the 2-col space left for the shorthand pad.
const slashMinNameGap = 2

type slashPaletteGroup struct {
	title string
	names []string
}

// slashPaletteGroups is the painted order. Headings are not selectable.
var slashPaletteGroups = []slashPaletteGroup{
	{title: "Session", names: []string{"/new", "/resume", "/continue", "/compact"}},
	{title: "Model", names: []string{"/model", "/variant", "/refresh"}},
	{title: "Project", names: []string{"/agents", "/history", "/spawn", "/settings", "/skills", "/usage"}},
	{title: "Help", names: []string{"/help"}},
}

// slashView renders a grouped command palette above the prompt.
func (m Model) slashView() string {
	cardW := max(minPaneWidth, m.width-cardBorder)
	compact := m.width < slashCompactMaxWidth
	selName := ""
	selDesc := ""
	if m.slashCursor >= 0 && m.slashCursor < len(m.slashItems) {
		selName = m.slashItems[m.slashCursor].name
		selDesc = m.slashItems[m.slashCursor].description
	}

	groupSt := drawerNormalStyle
	nameSt := lipgloss.NewStyle().Foreground(theme.ColorText())
	descSt := drawerNormalStyle

	nameW := 0
	for _, cmd := range m.slashItems {
		if w := lipgloss.Width(cmd.name); w > nameW {
			nameW = w
		}
	}

	var body strings.Builder
	if len(m.slashItems) == 0 {
		body.WriteString(descSt.Render("no matching command"))
	} else {
		byName := make(map[string]slashCmd, len(m.slashItems))
		for _, cmd := range m.slashItems {
			byName[cmd.name] = cmd
		}
		seen := make(map[string]bool, len(m.slashItems))
		for gi, g := range slashPaletteGroups {
			var groupItems []slashCmd
			for _, name := range g.names {
				cmd, ok := byName[name]
				if !ok {
					continue
				}
				groupItems = append(groupItems, cmd)
				seen[name] = true
			}
			if len(groupItems) == 0 {
				continue
			}
			if gi > 0 || body.Len() > 0 {
				body.WriteString("\n")
			}
			if compact && len(groupItems) == 1 {
				cmd := groupItems[0]
				body.WriteString(groupSt.Render(g.title))
				body.WriteString("  ")
				body.WriteString(slashCommandRow(cmd, cmd.name == selName, compact, cardW, nameW, drawerSelectedStyle, nameSt, descSt))
				continue
			}
			body.WriteString(groupSt.Render(g.title))
			for _, cmd := range groupItems {
				body.WriteString("\n")
				body.WriteString(slashCommandRow(cmd, cmd.name == selName, compact, cardW, nameW, drawerSelectedStyle, nameSt, descSt))
			}
		}
		for _, cmd := range m.slashItems {
			if seen[cmd.name] {
				continue
			}
			if body.Len() > 0 {
				body.WriteString("\n")
			}
			body.WriteString(slashCommandRow(cmd, cmd.name == selName, compact, cardW, nameW, drawerSelectedStyle, nameSt, descSt))
		}
	}

	foot := ""
	if compact && selDesc != "" {
		foot = selDesc
	}
	return drawerChrome("commands", selName, body.String(), foot, cardW)
}

func slashCommandRow(cmd slashCmd, selected, compact bool, cardW, nameW int, sel, nameSt, descSt lipgloss.Style) string {
	prefix := "  "
	if selected {
		prefix = "▸ "
	}
	if compact {
		line := prefix + cmd.name
		if lipgloss.Width(line) > cardW {
			line = truncateRunes(line, cardW)
		}
		if selected {
			return drawerSelectedStyle.Width(cardW).MaxWidth(cardW).Render(line)
		}
		return nameSt.Render(line)
	}
	left := prefix + cmd.name
	gap := max(slashMinNameGap, nameW-lipgloss.Width(cmd.name)+slashMinNameGap)
	desc := cmd.description
	avail := cardW - lipgloss.Width(left) - gap
	if avail > 0 && lipgloss.Width(desc) > avail {
		desc = truncateRunes(desc, avail)
	}
	line := left + strings.Repeat(" ", gap) + desc
	if selected {
		return drawerSelectedStyle.Width(cardW).MaxWidth(cardW).Render(line)
	}
	return nameSt.Render(left) + strings.Repeat(" ", gap) + descSt.Render(desc)
}

// updateSlash handles keys while the slash menu is open.
func (m Model) updateSlashKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	m = m.clearTextSelection()
	if isUndoKey(key) {
		return m.undoPrompt(), nil
	}
	switch key.Code {
	case tea.KeyEscape:
		m.slashMode = false
		m.slashCursor = 0
		m.prompt.SetValue("/")
		return m, nil
	case tea.KeyEnter:
		if m.slashCursor >= 0 && m.slashCursor < len(m.slashItems) {
			name := m.slashItems[m.slashCursor].name
			extra := slashArg(m.prompt.Value(), name)
			m.slashMode = false
			m.slashCursor = 0
			m.slashFromPaste = false
			if name == "/model" {
				return m.openModelSearch(), nil
			}
			m.prompt.SetValue("")
			m.promptUndo = nil
			return m.runSlashArg(name, extra)
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
	return m.runSlashArg(name, "")
}

func (m Model) runSlashArg(name, extra string) (Model, tea.Cmd) {
	switch name {
	case "/new":
		return m.loadSession(nil), nil
	case "/resume", "/sessions", "/session":
		return m.openSessionPicker(), nil
	case "/model":
		return m.openModelSearch(), nil
	case "/variant":
		return m.openVariantPicker(), nil
	case "/refresh":
		return m, m.refreshModels
	case "/usage":
		return m.openUsageModal(), m.fetchUsage()
	case "/status":
		return m.openStatusDrawer(), nil
	case "/skills":
		if !m.projectSettings.EffectiveSkills().Enabled {
			m.copyNotice = "skills disabled in settings"
			return m, clearCopyNotice()
		}
		return m.openSkillsPicker(extra)
	case "/settings", "/slot":
		return m.openSettings(), m.maybeFetchUsage()
	case "/agents", "/subs", "/subagents":
		return m.openSubagentPicker(), nil
	case "/history":
		return m.openMemoryHistory(), nil
	case "/spawn", "/agent":
		return m.openSubagentSpawnForm()
	case "/continue":
		return m.runContinue()
	case "/compact":
		return m.runCompact(extra)
	case "/help", "/keys":
		m = m.setFocus(focusHelp)
	}
	return m, nil
}

func slashArg(prompt, name string) string {
	trimmed := strings.TrimSpace(prompt)
	if !strings.HasPrefix(trimmed, name) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, name))
}

// syncSlash recomputes the slash menu from the prompt text. The menu opens
// when the prompt starts with "/" and closes when it no longer does.
func (m Model) syncSlash(value string) Model {
	if !strings.HasPrefix(value, "/") {
		if m.pickerFromPrompt {
			m = m.closePicker()
		}
		m.slashMode = false
		m.slashCursor = 0
		m.slashFromPaste = false
		m.syncTranscript()
		return m
	}
	if query, ok := modelSearchQuery(value); ok {
		if strings.EqualFold(value, "/model") {
			m.prompt.SetValue("/model ")
			query = ""
		}
		m.slashMode = false
		m.slashCursor = 0
		if !m.pickerMode || m.pickerKind != pickerKindModel {
			m = m.openKindPicker(pickerKindModel)
		}
		m.pickerFromPrompt = true
		m.pickerFilter = query
		m.pickerFiltering = false
		m.applyFilter()
		if query != "" && len(m.pickerItems) == 0 {
			m = m.closePicker()
			m.slashMode = false
		}
		return m
	}
	if m.pickerFromPrompt {
		m = m.closePicker()
	}
	partial := strings.ToLower(strings.TrimPrefix(value, "/"))
	m.slashItems = filterSlashItems(partial)
	if m.slashCursor >= len(m.slashItems) {
		m.slashCursor = max(0, len(m.slashItems)-1)
	}
	m.slashMode = true
	m.syncTranscript()
	return m
}

// filterSlashItems returns matching commands in palette-group order.
func filterSlashItems(partial string) []slashCmd {
	byName := make(map[string]slashCmd, len(slashCommands))
	for _, cmd := range slashCommands {
		byName[cmd.name] = cmd
	}
	var out []slashCmd
	seen := make(map[string]bool, len(slashCommands))
	for _, g := range slashPaletteGroups {
		for _, name := range g.names {
			cmd, ok := byName[name]
			if !ok || !matchesSlashPartial(cmd, partial) {
				continue
			}
			out = append(out, cmd)
			seen[name] = true
		}
	}
	for _, cmd := range slashCommands {
		if cmd.name == "/status" && partial == "" {
			continue
		}
		if seen[cmd.name] || !matchesSlashPartial(cmd, partial) {
			continue
		}
		out = append(out, cmd)
	}
	return out
}

// matchesSlashPartial reports whether a command matches a typed partial
// by its own name or by one of its aliases.
func matchesSlashPartial(cmd slashCmd, partial string) bool {
	if strings.HasPrefix(strings.TrimPrefix(cmd.name, "/"), partial) {
		return true
	}
	for _, alias := range cmd.aliases {
		if strings.HasPrefix(alias, partial) {
			return true
		}
	}
	return false
}

// modelSearchQuery reports whether value is `/model` plus an optional
// search string. The search looks through model names and providers.
func modelSearchQuery(value string) (string, bool) {
	if !strings.HasPrefix(value, "/") {
		return "", false
	}
	rest := strings.ToLower(strings.TrimPrefix(value, "/"))
	if rest == "model" {
		return "", true
	}
	if strings.HasPrefix(rest, "model ") {
		return strings.TrimSpace(rest[len("model"):]), true
	}
	return "", false
}

func (m Model) openModelSearch() Model {
	m.prompt.SetValue("/model ")
	m.promptUndo = nil
	return m.syncSlash("/model ")
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
