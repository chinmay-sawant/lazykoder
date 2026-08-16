package chat

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/settings"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

// settingsRow is one focusable row in the Slot settings drawer.
const (
	settingsRowLimit = iota
	settingsRowSteps
	settingsRowCount

	// settingsHeaderLines is header + description before the first control row.
	settingsHeaderLines = 2
)

// openSettings opens the Slot settings drawer above the prompt.
func (m Model) openSettings() Model {
	m.settingsMode = true
	m.settingsCursor = settingsRowLimit
	m.slashMode = false
	m.slashCursor = 0
	m.pickerMode = false
	m.helpMode = false
	m.filePickerMode = false
	m.prompt.SetValue("")
	m.promptUndo = nil
	return m
}

func (m Model) closeSettings() Model {
	m.settingsMode = false
	m.settingsCursor = 0
	return m
}

// settingsView renders the Slot settings drawer (model-picker placement)
// with a help-style header and a top-right clickable x.
func (m Model) settingsView() string {
	cardW := max(minPaneWidth, m.width-cardBorder)
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("slot settings")
	closeBtn := m.settingsCloseLabel()
	gap := max(1, cardW-lipgloss.Width(title)-lipgloss.Width(closeBtn))
	header := title + strings.Repeat(" ", gap) + closeBtn

	sel := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	dim := hintStyle
	var body strings.Builder
	body.WriteString(header)
	body.WriteString("\n")
	body.WriteString(dim.Render(truncateRunes("agent loop budget for each user turn", cardW)))
	for i, line := range m.settingsRows(cardW) {
		body.WriteString("\n")
		if i == m.settingsCursor {
			body.WriteString(sel.Render(line))
		} else {
			body.WriteString(dim.Render(line))
		}
	}
	body.WriteString("\n")
	body.WriteString(dim.Render(truncateRunes(
		"j/k move  •  left/right or click adjust  •  space toggle  •  x close",
		cardW,
	)))
	return lipgloss.NewStyle().Width(cardW).Render(body.String())
}

func (m Model) settingsCloseLabel() string {
	return lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("[x]")
}

// settingsDrawerTop is the first screen row of the settings drawer, matching
// chatScreen (after the alert row, under any slash/picker drawers).
// Do not derive this from composerTop minus height: composerTop adds an
// extra row for the newline after the drawer, which shifts every hit target
// one cell down.
func (m Model) settingsDrawerTop() int {
	if !m.settingsMode {
		return 0
	}
	top := m.transcriptTop() + m.transcriptRenderHeight() + 1 // alert row
	if m.slashMode {
		top += 1 + lipgloss.Height(m.slashView())
	}
	if m.pickerMode {
		top += 1 + lipgloss.Height(m.pickerView())
	}
	return top
}

func (m Model) settingsRows(cardW int) []string {
	limitOn := "off"
	if m.slotSettings.LimitEnabled {
		limitOn = "on"
	}
	limitLine := fmt.Sprintf("%s step limit          [%s]",
		settingsPrefix(m.settingsCursor == settingsRowLimit), limitOn)
	stepsLine := fmt.Sprintf("%s max steps           ◂ %d ▸",
		settingsPrefix(m.settingsCursor == settingsRowSteps), m.slotSettings.MaxSteps)
	if !m.slotSettings.LimitEnabled {
		stepsLine = fmt.Sprintf("%s max steps           %d (disabled)",
			settingsPrefix(m.settingsCursor == settingsRowSteps), m.slotSettings.MaxSteps)
	}
	return []string{
		truncateRunes(limitLine, cardW),
		truncateRunes(stepsLine, cardW),
	}
}

func settingsPrefix(selected bool) string {
	if selected {
		return "▸"
	}
	return " "
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
	case tea.KeySpace, tea.KeyEnter:
		return m.toggleSettingsRow(), nil
	}
	return m, nil
}

func (m Model) adjustSettings(delta int) Model {
	switch m.settingsCursor {
	case settingsRowLimit:
		if delta != 0 {
			return m.setLimitEnabled(!m.slotSettings.LimitEnabled)
		}
	case settingsRowSteps:
		return m.setMaxSteps(m.slotSettings.MaxSteps + delta)
	}
	return m
}

func (m Model) toggleSettingsRow() Model {
	switch m.settingsCursor {
	case settingsRowLimit:
		return m.setLimitEnabled(!m.slotSettings.LimitEnabled)
	case settingsRowSteps:
		// enter on steps bumps by 1 when enabled
		if m.slotSettings.LimitEnabled {
			return m.setMaxSteps(m.slotSettings.MaxSteps + 1)
		}
	}
	return m
}

func (m Model) setLimitEnabled(on bool) Model {
	m.slotSettings.LimitEnabled = on
	m.maxSteps = m.effectiveMaxSteps()
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
	m.slotSettings.MaxSteps = n
	m.maxSteps = m.effectiveMaxSteps()
	m = m.persistSettings()
	return m
}

func (m Model) effectiveMaxSteps() int {
	return settings.Settings{Slot: m.slotSettings}.EffectiveMaxSteps()
}

func (m Model) persistSettings() Model {
	if m.settingsPath == "" {
		return m
	}
	if err := settings.Save(m.settingsPath, settings.Settings{Slot: m.slotSettings}); err != nil {
		m.err = err.Error()
	}
	return m
}

// settingsCloseRect is the screen rectangle of the [x] close control,
// located by scanning the rendered header so it matches what the user sees.
func (m Model) settingsCloseRect() (x0, y, x1 int, ok bool) {
	if !m.settingsMode {
		return 0, 0, 0, false
	}
	top := m.settingsDrawerTop()
	line := plainLine(m.settingsView(), 0)
	x0, x1, ok = displaySpan(line, "[x]")
	if !ok {
		return 0, 0, 0, false
	}
	// Pad one cell on each side so the target is easier to hit.
	return max(0, x0-1), top, x1 + 1, true
}

// settingsRowAtScreenY maps a click row to a settings control row.
// Layout lines: 0 header, 1 description, 2.. row controls, then footer.
func (m Model) settingsRowAtScreenY(y int) (row int, ok bool) {
	if !m.settingsMode {
		return 0, false
	}
	rowTop := m.settingsDrawerTop() + settingsHeaderLines
	if y < rowTop || y >= rowTop+settingsRowCount {
		return 0, false
	}
	return y - rowTop, true
}

// settingsHit handles a mouse press inside the settings drawer.
// Controls are hit-tested against the rendered glyphs ([x], [on]/[off],
// ◂ / ▸), not crude left/right halves of the row.
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
		drawerTop := m.settingsDrawerTop()
		if y >= drawerTop && y < m.composerTop() {
			return m, nil, true
		}
		return m, nil, false
	}
	m.settingsCursor = row
	line := plainLine(m.settingsView(), settingsHeaderLines+row)
	switch row {
	case settingsRowLimit:
		// Whole row is the toggle; prefer the [on]/[off] hit box when present.
		if hitToggle(line, x) || button == tea.MouseLeft || button == tea.MouseRight {
			return m.setLimitEnabled(!m.slotSettings.LimitEnabled), nil, true
		}
		return m, nil, true
	case settingsRowSteps:
		if !m.slotSettings.LimitEnabled {
			return m, nil, true
		}
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.setMaxSteps(m.slotSettings.MaxSteps - 1), nil, true
		} else if inc {
			return m.setMaxSteps(m.slotSettings.MaxSteps + 1), nil, true
		}
		// Click on the number: left decreases, right increases.
		if x0, x1, ok := displaySpan(line, fmt.Sprintf("%d", m.slotSettings.MaxSteps)); ok && x >= x0-1 && x < x1+1 {
			if button == tea.MouseRight {
				return m.setMaxSteps(m.slotSettings.MaxSteps + 1), nil, true
			}
			return m.setMaxSteps(m.slotSettings.MaxSteps - 1), nil, true
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
// control of a max-steps row.
func hitStepChevrons(line string, x int) (dec, inc bool) {
	if x0, x1, ok := displaySpan(line, "◂"); ok && x >= x0-1 && x < x1+1 {
		return true, false
	}
	// Prefer the rightmost ▸ so a selected-row "▸ " prefix is not treated
	// as increase when the real chevron is further right.
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
