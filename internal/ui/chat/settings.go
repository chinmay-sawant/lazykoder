package chat

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
	"github.com/chinmay-sawant/lazykoder/internal/subagent"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

// settingsRow is one focusable row in the project settings card.
const (
	settingsRowModel = iota
	settingsRowVariant
	settingsRowLimit
	settingsRowSteps
	settingsRowAgentsEnabled
	settingsRowAgentsConcurrent
	settingsRowAgentsChildSteps
	settingsRowCount

	// settingsCardChromeRows: top border, header, footer, bottom border.
	settingsCardChromeRows = 4
	// settingsCardHeaderRows: top border + header before the first list row.
	settingsCardHeaderRows = 2
)

// openSettings opens the full-screen settings card (same layout family as
// /resume and /help: centered bordered card over the chat).
func (m Model) openSettings() Model {
	m.settingsMode = true
	m.settingsCursor = settingsRowModel
	m.slashMode = false
	m.slashCursor = 0
	m.pickerMode = false
	m.helpMode = false
	m.filePickerMode = false
	m.sessionPickerMode = false
	m.prompt.SetValue("")
	m.promptUndo = nil
	return m
}

func (m Model) closeSettings() Model {
	m.settingsMode = false
	m.settingsCursor = 0
	m.settingsPickDefault = false
	return m
}

// settingsScreen is the full terminal frame with the settings card centered,
// matching sessionPickerScreen.
func (m Model) settingsScreen() string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.settingsCardView())
}

// settingsCardView is the bordered settings card (resume / help style).
func (m Model) settingsCardView() string {
	cardW := m.overlayWidth()
	innerW := max(minPaneWidth, cardW-cardBorder-2*cardPad)

	title := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("SETTINGS")
	closeBtn := m.settingsCloseLabel()
	gap := max(1, innerW-lipgloss.Width(title)-lipgloss.Width(closeBtn))
	header := title + strings.Repeat(" ", gap) + closeBtn

	sel := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText())
	normal := lipgloss.NewStyle().Foreground(theme.ColorMute())
	var body strings.Builder
	for i, line := range m.settingsRows(innerW) {
		if i > 0 {
			body.WriteString("\n")
		}
		if i == m.settingsCursor {
			body.WriteString(sel.MaxWidth(innerW).Render(line))
		} else {
			body.WriteString(normal.MaxWidth(innerW).Render(line))
		}
	}
	footer := hintStyle.Width(innerW).Render(truncateRunes(
		"j/k move  •  ←/→ adjust  •  enter pick  •  click  •  esc/[x] close",
		innerW,
	))
	content := lipgloss.JoinVertical(lipgloss.Left, header, body.String(), footer)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorBorder()).
		Background(theme.ColorBg()).
		Padding(0, cardPad).
		Width(cardW).
		Render(content)
}

func (m Model) settingsCloseLabel() string {
	return lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("[x]")
}

func (m Model) settingsRows(innerW int) []string {
	modelVal := m.projectSettings.Model.Default
	if modelVal == "" {
		modelVal = settings.DefaultModelID
	}
	variantVal := m.projectSettings.Model.Variant
	if variantVal == "" {
		variantVal = "default"
	}
	limitOn := "off"
	if m.projectSettings.Slot.LimitEnabled {
		limitOn = "on"
	}
	stepsVal := fmt.Sprintf("◂ %d ▸", m.projectSettings.Slot.MaxSteps)
	if !m.projectSettings.Slot.LimitEnabled {
		stepsVal = fmt.Sprintf("%d (off)", m.projectSettings.Slot.MaxSteps)
	}
	agentsOn := "off"
	if m.projectSettings.Agents.Enabled {
		agentsOn = "on"
	}
	concurrentVal := fmt.Sprintf("◂ %d ▸", m.projectSettings.Agents.MaxConcurrent)
	childStepsVal := fmt.Sprintf("◂ %d ▸", m.projectSettings.Agents.ChildMaxSteps)
	return []string{
		settingsKVRow(m.settingsCursor == settingsRowModel, "default model", "◂ "+modelVal+" ▸", innerW),
		settingsKVRow(m.settingsCursor == settingsRowVariant, "default variant", "◂ "+variantVal+" ▸", innerW),
		settingsKVRow(m.settingsCursor == settingsRowLimit, "step limit", "["+limitOn+"]", innerW),
		settingsKVRow(m.settingsCursor == settingsRowSteps, "max steps", stepsVal, innerW),
		settingsKVRow(m.settingsCursor == settingsRowAgentsEnabled, "sub-agents", "["+agentsOn+"]", innerW),
		settingsKVRow(m.settingsCursor == settingsRowAgentsConcurrent, "max concurrent", concurrentVal, innerW),
		settingsKVRow(m.settingsCursor == settingsRowAgentsChildSteps, "child max steps", childStepsVal, innerW),
	}
}

func settingsKVRow(selected bool, label, value string, width int) string {
	prefix := "  "
	if selected {
		prefix = "▸ "
	}
	left := prefix + label
	right := value
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		left = truncateRunes(left, max(4, width-lipgloss.Width(right)-1))
		gap = width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) updateSettingsKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	if key.Mod.Contains(tea.ModCtrl) && key.Code == 'c' {
		return m.closeDone(), tea.Quit
	}
	switch key.Code {
	case tea.KeyEscape, 'q', 'Q', 'x', 'X':
		return m.closeSettings(), nil
	case 'j', tea.KeyDown:
		if m.settingsCursor < settingsRowCount-1 {
			m.settingsCursor++
		}
		return m, nil
	case 'k', tea.KeyUp:
		if m.settingsCursor > 0 {
			m.settingsCursor--
		}
		return m, nil
	case tea.KeyLeft, 'h', 'H':
		return m.adjustSettings(-1), nil
	case tea.KeyRight, 'l', 'L':
		return m.adjustSettings(1), nil
	case tea.KeySpace:
		return m.toggleSettingsRow(), nil
	case tea.KeyEnter:
		return m.activateSettingsRow()
	}
	return m, nil
}

func (m Model) activateSettingsRow() (Model, tea.Cmd) {
	switch m.settingsCursor {
	case settingsRowModel:
		m.settingsPickDefault = true
		m.settingsMode = false
		return m.openKindPicker(pickerKindModel), nil
	case settingsRowVariant:
		m.settingsPickDefault = true
		m.settingsMode = false
		// Seed live model so the variant list matches the default model.
		if m.projectSettings.Model.Default != "" {
			m.model = m.projectSettings.Model.Default
		}
		return m.openKindPicker(pickerKindVariant), nil
	case settingsRowLimit:
		return m.setLimitEnabled(!m.projectSettings.Slot.LimitEnabled), nil
	case settingsRowSteps:
		if m.projectSettings.Slot.LimitEnabled {
			return m.setMaxSteps(m.projectSettings.Slot.MaxSteps + 1), nil
		}
	case settingsRowAgentsEnabled:
		return m.setAgentsEnabled(!m.projectSettings.Agents.Enabled), nil
	case settingsRowAgentsConcurrent:
		return m.setAgentsConcurrent(m.projectSettings.Agents.MaxConcurrent + 1), nil
	case settingsRowAgentsChildSteps:
		return m.setAgentsChildSteps(m.projectSettings.Agents.ChildMaxSteps + 1), nil
	}
	return m, nil
}

func (m Model) adjustSettings(delta int) Model {
	switch m.settingsCursor {
	case settingsRowModel:
		return m.cycleDefaultModel(delta)
	case settingsRowVariant:
		return m.cycleDefaultVariant(delta)
	case settingsRowLimit:
		if delta != 0 {
			return m.setLimitEnabled(!m.projectSettings.Slot.LimitEnabled)
		}
	case settingsRowSteps:
		return m.setMaxSteps(m.projectSettings.Slot.MaxSteps + delta)
	case settingsRowAgentsEnabled:
		if delta != 0 {
			return m.setAgentsEnabled(!m.projectSettings.Agents.Enabled)
		}
	case settingsRowAgentsConcurrent:
		return m.setAgentsConcurrent(m.projectSettings.Agents.MaxConcurrent + delta)
	case settingsRowAgentsChildSteps:
		return m.setAgentsChildSteps(m.projectSettings.Agents.ChildMaxSteps + delta)
	}
	return m
}

func (m Model) toggleSettingsRow() Model {
	switch m.settingsCursor {
	case settingsRowLimit:
		return m.setLimitEnabled(!m.projectSettings.Slot.LimitEnabled)
	case settingsRowSteps:
		if m.projectSettings.Slot.LimitEnabled {
			return m.setMaxSteps(m.projectSettings.Slot.MaxSteps + 1)
		}
	case settingsRowAgentsEnabled:
		return m.setAgentsEnabled(!m.projectSettings.Agents.Enabled)
	case settingsRowAgentsConcurrent:
		return m.setAgentsConcurrent(m.projectSettings.Agents.MaxConcurrent + 1)
	case settingsRowAgentsChildSteps:
		return m.setAgentsChildSteps(m.projectSettings.Agents.ChildMaxSteps + 1)
	case settingsRowModel:
		return m.cycleDefaultModel(1)
	case settingsRowVariant:
		return m.cycleDefaultVariant(1)
	}
	return m
}

func (m Model) cycleDefaultModel(delta int) Model {
	list := m.models
	if len(list) == 0 {
		list = []string{settings.DefaultModelID}
	}
	cur := m.projectSettings.Model.Default
	if cur == "" {
		cur = settings.DefaultModelID
	}
	idx := indexOfString(list, cur)
	if idx < 0 {
		idx = 0
	}
	idx = (idx + delta) % len(list)
	if idx < 0 {
		idx += len(list)
	}
	return m.setDefaultModel(list[idx])
}

func (m Model) cycleDefaultVariant(delta int) Model {
	list := m.defaultVariantChoices()
	cur := m.projectSettings.Model.Variant
	idx := indexOfString(list, cur)
	if idx < 0 {
		idx = 0
	}
	idx = (idx + delta) % len(list)
	if idx < 0 {
		idx += len(list)
	}
	return m.setDefaultVariant(list[idx])
}

func (m Model) defaultVariantChoices() []string {
	// Empty string first = provider default.
	out := []string{""}
	seen := map[string]bool{"": true}
	modelID := m.projectSettings.Model.Default
	if modelID == "" {
		modelID = settings.DefaultModelID
	}
	if info, ok := modelscache.InfoOf(m.modelInfos, modelID); ok {
		for _, v := range info.Variants {
			v = strings.TrimSpace(v)
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			out = append(out, v)
		}
	}
	for _, v := range []string{"low", "medium", "high", "max"} {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func indexOfString(list []string, want string) int {
	for i, s := range list {
		if s == want {
			return i
		}
	}
	return -1
}

func (m Model) setDefaultModel(id string) Model {
	id = strings.TrimSpace(id)
	if id == "" {
		id = settings.DefaultModelID
	}
	m.projectSettings.Model.Default = id
	if !modelscache.HasVariant(m.modelInfos, id, m.projectSettings.Model.Variant) {
		m.projectSettings.Model.Variant = ""
	}
	// Apply to the live chat when there is no session yet, or the live
	// model still matches the previous default path (empty / default).
	if m.session == nil || m.model == "" || m.model == settings.DefaultModelID {
		m.model = id
	}
	m = m.persistSettings()
	return m
}

func (m Model) setDefaultVariant(v string) Model {
	m.projectSettings.Model.Variant = strings.TrimSpace(v)
	if m.session == nil || m.variant == "" {
		m.variant = m.projectSettings.Model.Variant
	}
	m = m.persistSettings()
	return m
}

func (m Model) setLimitEnabled(on bool) Model {
	m.projectSettings.Slot.LimitEnabled = on
	m.maxSteps = m.projectSettings.EffectiveMaxSteps()
	m = m.persistSettings()
	return m
}

func (m Model) setMaxSteps(n int) Model {
	if n < settings.MinMaxSteps {
		n = settings.MinMaxSteps
	}
	if n > settings.MaxMaxSteps {
		n = settings.MaxMaxSteps
	}
	m.projectSettings.Slot.MaxSteps = n
	m.maxSteps = m.projectSettings.EffectiveMaxSteps()
	m = m.persistSettings()
	return m
}

func (m Model) setAgentsEnabled(on bool) Model {
	m.projectSettings.Agents.Enabled = on
	m = m.rebuildSubMgr()
	m = m.persistSettings()
	return m
}

func (m Model) setAgentsConcurrent(n int) Model {
	if n < settings.MinMaxConcurrent {
		n = settings.MinMaxConcurrent
	}
	if n > settings.MaxMaxConcurrent {
		n = settings.MaxMaxConcurrent
	}
	m.projectSettings.Agents.MaxConcurrent = n
	m = m.rebuildSubMgr()
	m = m.persistSettings()
	return m
}

func (m Model) setAgentsChildSteps(n int) Model {
	if n < settings.MinMaxSteps {
		n = settings.MinMaxSteps
	}
	if n > settings.MaxMaxSteps {
		n = settings.MaxMaxSteps
	}
	m.projectSettings.Agents.ChildMaxSteps = n
	m = m.rebuildSubMgr()
	m = m.persistSettings()
	return m
}

func (m Model) rebuildSubMgr() Model {
	if m.store == nil || m.client == nil {
		return m
	}
	m.subMgr = subagent.NewManager(subagent.ConfigFromSettings(m.projectSettings), subagent.AgentRunner{
		Store:  m.store,
		Client: m.client,
	})
	return m
}

func (m Model) persistSettings() Model {
	if m.settingsPath == "" {
		return m
	}
	if err := settings.Save(m.settingsPath, m.projectSettings); err != nil {
		m.err = err.Error()
	}
	return m
}

// settingsCloseRect is the screen rectangle of the [x] close control on the
// centered card header.
func (m Model) settingsCloseRect() (x0, y, x1 int, ok bool) {
	if !m.settingsMode {
		return 0, 0, 0, false
	}
	for i, line := range strings.Split(m.settingsScreen(), "\n") {
		plain := ansi.Strip(line)
		if !strings.Contains(plain, "SETTINGS") || !strings.Contains(plain, "[x]") {
			continue
		}
		start, end, found := displaySpan(plain, "[x]")
		if !found {
			continue
		}
		return max(0, start-1), i, end + 1, true
	}
	return 0, 0, 0, false
}

// settingsRowAtScreenY maps a click row to a settings control row by
// scanning the painted full-screen card (same approach as the resume list).
func (m Model) settingsRowAtScreenY(y int) (row int, ok bool) {
	if !m.settingsMode {
		return 0, false
	}
	listTop := m.settingsListScreenTop()
	if y < listTop || y >= listTop+settingsRowCount {
		return 0, false
	}
	return y - listTop, true
}

// settingsListScreenTop is the first settings control row on the painted
// screen: the line after the SETTINGS header.
func (m Model) settingsListScreenTop() int {
	for i, line := range strings.Split(m.settingsScreen(), "\n") {
		if strings.Contains(ansi.Strip(line), "SETTINGS") {
			return i + 1
		}
	}
	// Fallback: centered card formula (border + header).
	cardH := settingsCardChromeRows + settingsRowCount
	return max(0, (m.height-cardH)/centerDiv) + settingsCardHeaderRows
}

// settingsHit handles a mouse press on the settings card.
func (m Model) settingsHit(x, y int, button tea.MouseButton) (Model, tea.Cmd, bool) {
	if !m.settingsMode {
		return m, nil, false
	}
	if button != tea.MouseLeft && button != tea.MouseRight {
		return m, nil, false
	}
	if x0, cy, x1, ok := m.settingsCloseRect(); ok && y == cy && x >= x0 && x < x1 {
		return m.closeSettings(), nil, true
	}
	row, ok := m.settingsRowAtScreenY(y)
	if !ok {
		// Click outside the list but on the card: still consume so the
		// dimmed chat underneath is not selected.
		return m, nil, true
	}
	m.settingsCursor = row
	line := ""
	if rows := m.settingsRows(max(minPaneWidth, m.overlayWidth()-cardBorder-2*cardPad)); row >= 0 && row < len(rows) {
		line = rows[row]
	}
	// Align control X to the painted card: find the row text on the full
	// screen and use that line for glyph spans.
	for _, screenLine := range strings.Split(m.settingsScreen(), "\n") {
		plain := ansi.Strip(screenLine)
		if strings.Contains(plain, strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "▸ "), "  "))) ||
			(row == settingsRowModel && strings.Contains(plain, "default model")) ||
			(row == settingsRowVariant && strings.Contains(plain, "default variant")) ||
			(row == settingsRowLimit && strings.Contains(plain, "step limit")) ||
			(row == settingsRowSteps && strings.Contains(plain, "max steps")) {
			// Prefer the painted line that has the row label.
			if (row == settingsRowModel && strings.Contains(plain, "default model")) ||
				(row == settingsRowVariant && strings.Contains(plain, "default variant")) ||
				(row == settingsRowLimit && strings.Contains(plain, "step limit")) ||
				(row == settingsRowSteps && strings.Contains(plain, "max steps")) {
				line = plain
				break
			}
		}
	}
	switch row {
	case settingsRowModel:
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.cycleDefaultModel(-1), nil, true
		} else if inc {
			return m.cycleDefaultModel(1), nil, true
		}
		if button == tea.MouseLeft {
			next, cmd := m.activateSettingsRow()
			return next, cmd, true
		}
		return m.cycleDefaultModel(1), nil, true
	case settingsRowVariant:
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.cycleDefaultVariant(-1), nil, true
		} else if inc {
			return m.cycleDefaultVariant(1), nil, true
		}
		if button == tea.MouseLeft {
			next, cmd := m.activateSettingsRow()
			return next, cmd, true
		}
		return m.cycleDefaultVariant(1), nil, true
	case settingsRowLimit:
		return m.setLimitEnabled(!m.projectSettings.Slot.LimitEnabled), nil, true
	case settingsRowSteps:
		if !m.projectSettings.Slot.LimitEnabled {
			return m, nil, true
		}
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.setMaxSteps(m.projectSettings.Slot.MaxSteps - 1), nil, true
		} else if inc {
			return m.setMaxSteps(m.projectSettings.Slot.MaxSteps + 1), nil, true
		}
		if x0, x1, ok := displaySpan(line, fmt.Sprintf("%d", m.projectSettings.Slot.MaxSteps)); ok && x >= x0-1 && x < x1+1 {
			if button == tea.MouseRight {
				return m.setMaxSteps(m.projectSettings.Slot.MaxSteps + 1), nil, true
			}
			return m.setMaxSteps(m.projectSettings.Slot.MaxSteps - 1), nil, true
		}
		return m, nil, true
	}
	return m, nil, true
}

// plainLine returns display line i of view with ANSI stripped.
func plainLine(view string, i int) string {
	lines := strings.Split(view, "\n")
	if i < 0 || i >= len(lines) {
		return ""
	}
	return stripANSIString(lines[i])
}

// displaySpan returns the [start, end) display columns of needle in line.
func displaySpan(line, needle string) (start, end int, ok bool) {
	if needle == "" {
		return 0, 0, false
	}
	idx := strings.Index(line, needle)
	if idx < 0 {
		return 0, 0, false
	}
	start = lipgloss.Width(line[:idx])
	end = start + lipgloss.Width(needle)
	return start, end, true
}

func hitToggle(line string, x int) bool {
	for _, token := range []string{"[on]", "[off]"} {
		x0, x1, ok := displaySpan(line, token)
		if !ok {
			continue
		}
		if x >= x0-1 && x < x1+1 {
			return true
		}
	}
	return false
}

// hitStepChevrons reports whether x is on the decrease (◂) or increase (▸)
// control. The last ▸ wins so a row selection marker is not treated as +.
func hitStepChevrons(line string, x int) (dec, inc bool) {
	if x0, x1, ok := displaySpan(line, "◂"); ok && x >= x0-1 && x < x1+1 {
		return true, false
	}
	if x0, x1, ok := displaySpanLast(line, "▸"); ok && x >= x0-1 && x < x1+1 {
		return false, true
	}
	return false, false
}

// displaySpanLast is displaySpan for the last occurrence of needle.
func displaySpanLast(line, needle string) (start, end int, ok bool) {
	if needle == "" {
		return 0, 0, false
	}
	idx := strings.LastIndex(line, needle)
	if idx < 0 {
		return 0, 0, false
	}
	start = lipgloss.Width(line[:idx])
	end = start + lipgloss.Width(needle)
	return start, end, true
}

// stripANSIString removes CSI sequences so hit-testing matches screen cells.
func stripANSIString(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			for i += 2; i < len(s); i++ {
				if s[i] >= 0x40 && s[i] <= 0x7e {
					i++
					break
				}
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
