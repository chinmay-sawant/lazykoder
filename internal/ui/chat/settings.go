package chat

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/settings"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

// settingsRow is one focusable row in the project settings card.
const (
	settingsRowTheme = iota
	settingsRowModel
	settingsRowVariant
	settingsRowChildModel
	settingsRowExploreModel
	settingsRowRecapEnabled
	settingsRowRecapModel
	settingsRowRecapAfterChats
	settingsRowRetryMaxRetries
	settingsRowRetryDelay
	settingsRowSkillsEnabled
	settingsRowSkillsAutoDetect
	settingsRowSkillsLocal
	settingsRowSkillsGlobal
	settingsRowSkillsRemember
	settingsRowSkillsMaxMatches
	settingsRowLimit
	settingsRowSteps
	settingsRowCompactAuto
	settingsRowCompactPercent
	settingsRowAgentsEnabled
	settingsRowAgentsRole
	settingsRowAgentsConcurrent
	settingsRowAgentsQueued
	settingsRowAgentsChildSteps
	settingsRowAgentsTimeout
	settingsRowAgentsWriters
	settingsRowBashConfirm
	settingsRowAllowlistEnabled
	settingsRowAllowlist
	settingsRowCount

	// settingsCardChromeRows: top border, header, footer, bottom border.
	settingsCardChromeRows = 4
	// settingsCardHeaderRows: top border + header before the first list row.
	settingsCardHeaderRows = 2
	// settingsTimeoutStepSec is the child-timeout stepper increment.
	settingsTimeoutStepSec = 30
	// settingsCompactPercentStep is the auto-compact threshold increment.
	settingsCompactPercentStep = 5
	// settingsMaxQueuedCap matches settings.maxMaxQueued (1-100).
	settingsMaxQueuedCap = 100
	// settingsAllowlistPreviewBudget is how much of innerW names may use
	// before the allowlist collapses to "N allowed".
	settingsAllowlistPreviewBudget = 3
	// settingsCardVertPad is the Padding(1, 2) vertical inset.
	settingsCardVertPad = 1
	// settingsCardHorzPad is the Padding(1, 2) horizontal inset.
	settingsCardHorzPad = 2
	// settingsCardColsPerVertPad is the factor applied to the vertical pad.
	settingsCardColsPerVertPad = 2
	// settingsUsageReserveRows is the headroom added to the paint buffer.
	settingsUsageReserveRows = 8
	// settingsUsageMinHeight keeps the settings card from clipping its footer
	// when the optional usage section would not fit in the terminal.
	settingsUsageMinHeight = 42
	// settingsContentFixedRows are the header, spacer, and footer inside the
	// card. Borders and vertical padding are added separately.
	settingsContentFixedRows = 3
	// settingsUsageMaxCorners is the extra width kept for the error line.
	settingsUsageMaxCorners = 16
	// settingsUsageMinTextW is the floor width for the usage text row.
	settingsUsageMinTextW = 20
	// settingsAllowlistMinBudget is the floor width for an allowlist preview.
	settingsAllowlistMinBudget = 8
	// settingsKVRowMinLeftW is the floor width for a KV row label.
	settingsKVRowMinLeftW = 4
	// settingsTimeoutMinute is seconds per minute.
	settingsTimeoutMinute = 60
)

const (
	settingsLineHeader = iota + 1
	settingsLineHint
	settingsLineRow
)

// openSettings opens the full-screen settings card (same layout family as
// /resume and /help: centered bordered card over the chat). It marks usage as
// loading when the plan usage has not been loaded yet; the caller kicks off
// the fetch via maybeFetchUsage.
func (m Model) openSettings() Model {
	m = m.setFocus(focusSettings)
	m.settingsCursor = settingsRowTheme
	m.settingsHover = -1
	m.settingsEdit = false
	m.settingsEditValue = ""
	m.prompt.SetValue("")
	m.promptUndo = nil
	return m
}

func (m Model) closeSettings() Model {
	m = m.clearFocus(focusSettings)
	m.settingsCursor = 0
	m.settingsHover = -1
	m.settingsPickDefault = false
	m.settingsPickRecap = false
	m.settingsEdit = false
	m.settingsEditValue = ""
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
	innerW := max(minPaneWidth, cardW-cardBorder-2*settingsCardHorzPad)

	title := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorAccent()).Render("SETTINGS")
	closeBtn := m.settingsCloseLabel()
	gap := max(1, innerW-lipgloss.Width(title)-lipgloss.Width(closeBtn))
	header := title + strings.Repeat(" ", gap) + closeBtn

	sel := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Background(theme.ColorBorder())
	hover := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText())
	normal := lipgloss.NewStyle().Foreground(theme.ColorMute())
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorAccent())
	mute := hintStyle
	dim := lipgloss.NewStyle().Foreground(theme.ColorMute()).Faint(true)

	lines := m.visibleSettingsPaintLines(innerW)
	var body strings.Builder
	for i, line := range lines {
		if i > 0 {
			body.WriteString("\n")
		}
		text := line.text
		if line.kind == settingsLineHint || line.kind == settingsLineHeader {
			text = truncateRunes(text, innerW)
		}
		switch {
		case line.kind == settingsLineHeader:
			body.WriteString(headerStyle.MaxWidth(innerW).Render(text))
		case line.kind == settingsLineHint:
			body.WriteString(mute.MaxWidth(innerW).Render(text))
		case line.kind == settingsLineRow && line.row == m.settingsCursor:
			body.WriteString(sel.MaxWidth(innerW).Render(text))
		case line.kind == settingsLineRow && line.row == m.settingsHover:
			body.WriteString(hover.MaxWidth(innerW).Render(text))
		case line.dim:
			body.WriteString(dim.MaxWidth(innerW).Render(text))
		default:
			body.WriteString(normal.MaxWidth(innerW).Render(text))
		}
	}

	foot := "j/k move  •  ←/→ adjust  •  enter pick  •  click  •  esc/[x] close"
	if m.settingsEdit {
		foot = "enter save  •  esc cancel"
	}
	footer := hintStyle.Width(innerW).Render(truncateRunes(foot, innerW))

	// Blank row after SETTINGS, then the sectioned body, then the footer.
	content := lipgloss.JoinVertical(lipgloss.Left, header, "", body.String(), footer)
	minInner := m.settingsCardMinInnerHeight()
	if extra := minInner - lipgloss.Height(content); extra > 0 {
		content = lipgloss.JoinVertical(lipgloss.Left, header, "", body.String(), strings.Repeat("\n", extra-1), footer)
	}
	content = keepBackground(content, theme.ColorSurface())

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorBorder()).
		BorderBackground(theme.ColorSurface()).
		Background(theme.ColorSurface()).
		Padding(settingsCardVertPad, settingsCardHorzPad).
		Width(cardW).
		Render(content)
}

func (m Model) settingsCardMinInnerHeight() int {
	minCard := max(minPaneHeight+settingsCardChromeRows, m.height*sessionCardHeightPct/percentBase)
	inner := minCard - cardBorder - settingsCardColsPerVertPad*settingsCardVertPad
	if inner < minPaneHeight {
		return minPaneHeight
	}
	return inner
}

func (m Model) settingsBodyMaxRows() int {
	chrome := cardBorder + settingsCardColsPerVertPad*settingsCardVertPad + settingsContentFixedRows
	return max(1, m.height-chrome)
}

func (m Model) visibleSettingsPaintLines(innerW int) []settingsPaintLine {
	lines := m.settingsPaintLines(innerW)
	maxRows := m.settingsBodyMaxRows()
	if len(lines) <= maxRows {
		return lines
	}

	selected := 0
	for i, line := range lines {
		if line.kind == settingsLineRow && line.row == m.settingsCursor {
			selected = i
			break
		}
	}
	start := max(0, selected-maxRows+1)
	for i := selected; i >= 0; i-- {
		if lines[i].kind != settingsLineHeader || selected-i >= maxRows {
			continue
		}
		start = i
		break
	}
	end := min(len(lines), start+maxRows)
	if end-start < maxRows {
		start = max(0, end-maxRows)
	}
	return lines[start:end]
}

func (m Model) settingsCloseLabel() string {
	return lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("[x]")
}

type settingsPaintLine struct {
	kind int
	row  int
	text string
	dim  bool
}

func (m Model) settingsPaintLines(innerW int) []settingsPaintLine {
	modelVal := m.projectSettings.Model.Default
	if modelVal == "" {
		modelVal = settings.DefaultModelID
	}
	variantVal := m.projectSettings.Model.Variant
	if variantVal == "" {
		variantVal = "default"
	}
	childVal := m.projectSettings.Agents.ModelOverride
	if childVal == "" {
		childVal = "inherit"
	}
	exploreVal := m.projectSettings.Agents.ExploreModel
	if exploreVal == "" {
		exploreVal = "inherit"
	}
	recap := m.projectSettings.EffectiveRecap()
	skillCfg := m.projectSettings.EffectiveSkills()
	recapModelVal := recap.Model
	if recapModelVal == "" {
		recapModelVal = settings.DefaultModelID
	}
	limitOn := "off"
	if m.projectSettings.Slot.LimitEnabled {
		limitOn = "on"
	}
	stepsVal := fmt.Sprintf("◂ %d ▸", m.projectSettings.Slot.MaxSteps)
	stepsDim := false
	if !m.projectSettings.Slot.LimitEnabled {
		stepsVal = "limit off (safety cap still applies)"
		stepsDim = true
	}
	agentsOn := "off"
	if m.projectSettings.Agents.Enabled {
		agentsOn = "on"
	}
	roleVal := m.projectSettings.Agents.DefaultRole
	if roleVal == "" {
		roleVal = "explore"
	}
	confirmVal := "ask parent"
	if m.projectSettings.Agents.BashConfirm == "deny" {
		confirmVal = "deny"
	}
	allowlistVal := formatAllowlistValue(m.projectSettings.Agents.BashAllowlist, innerW)
	if m.settingsEdit {
		allowlistVal = m.settingsEditValue
	}

	out := make([]settingsPaintLine, 0, settingsRowCount+settingsUsageReserveRows)
	out = append(out, settingsPaintLine{kind: settingsLineHeader, row: -1, text: "appearance"})
	out = append(out,
		m.settingsPaintRow(settingsRowTheme, "◂ "+m.projectSettings.EffectiveTheme()+" ▸", innerW, false),
		settingsPaintLine{kind: settingsLineHeader, row: -1, text: "model"},
	)
	out = append(out,
		m.settingsPaintRow(settingsRowModel, "◂ "+modelVal+" ▸", innerW, false),
		m.settingsPaintRow(settingsRowVariant, "◂ "+variantVal+" ▸", innerW, false),
		m.settingsPaintRow(settingsRowChildModel, "◂ "+childVal+" ▸", innerW, false),
		m.settingsPaintRow(settingsRowExploreModel, "◂ "+exploreVal+" ▸", innerW, false),
		settingsPaintLine{kind: settingsLineHint, row: -1, text: "live /model and /variant do not change these defaults"},
		settingsPaintLine{kind: settingsLineHeader, row: -1, text: "recaps"},
		m.settingsPaintRow(settingsRowRecapEnabled, "["+boolOn(recap.Enabled)+"]", innerW, false),
		m.settingsPaintRow(settingsRowRecapModel, "◂ "+recapModelVal+" ▸", innerW, false),
		m.settingsPaintRow(settingsRowRecapAfterChats, fmt.Sprintf("◂ %d ▸", recap.AfterChats), innerW, false),
		settingsPaintLine{kind: settingsLineHeader, row: -1, text: "agent loop"},
		m.settingsPaintRow(settingsRowLimit, "["+limitOn+"]", innerW, false),
		m.settingsPaintRow(settingsRowSteps, stepsVal, innerW, stepsDim),
		settingsPaintLine{kind: settingsLineHeader, row: -1, text: "compaction"},
		m.settingsPaintRow(settingsRowCompactAuto, "["+boolOn(m.projectSettings.Compaction.Auto)+"]", innerW, false),
		m.settingsPaintRow(settingsRowCompactPercent, fmt.Sprintf("◂ %d%% ▸", m.projectSettings.EffectiveCompaction().Percent), innerW, !m.projectSettings.Compaction.Auto),
		settingsPaintLine{kind: settingsLineHint, row: -1, text: "auto-compact when used tokens exceed this % of the model window"},
		settingsPaintLine{kind: settingsLineHeader, row: -1, text: "sub-agents"},
		m.settingsPaintRow(settingsRowAgentsEnabled, "["+agentsOn+"]", innerW, false),
		m.settingsPaintRow(settingsRowAgentsRole, "◂ "+roleVal+" ▸", innerW, false),
		m.settingsPaintRow(settingsRowAgentsConcurrent, fmt.Sprintf("◂ %d ▸", m.projectSettings.Agents.MaxConcurrent), innerW, false),
		m.settingsPaintRow(settingsRowAgentsQueued, fmt.Sprintf("◂ %d ▸", m.projectSettings.Agents.MaxQueued), innerW, false),
		m.settingsPaintRow(settingsRowAgentsChildSteps, fmt.Sprintf("◂ %d ▸", m.projectSettings.Agents.ChildMaxSteps), innerW, false),
		m.settingsPaintRow(settingsRowAgentsTimeout, "◂ "+formatSettingsTimeout(m.projectSettings.Agents.DefaultTimeoutSec)+" ▸", innerW, false),
		m.settingsPaintRow(settingsRowAgentsWriters, "["+boolOn(m.projectSettings.Agents.AllowParallelWriters)+"]", innerW, false),
		settingsPaintLine{kind: settingsLineHeader, row: -1, text: "safety"},
		m.settingsPaintRow(settingsRowBashConfirm, "◂ "+confirmVal+" ▸", innerW, false),
		m.settingsPaintRow(settingsRowAllowlistEnabled, "["+boolOn(m.projectSettings.Agents.BashAllowlistEnabled)+"]", innerW, false),
		m.settingsPaintRow(settingsRowAllowlist, allowlistVal, innerW, false),
		settingsPaintLine{kind: settingsLineHeader, row: -1, text: "request retries"},
		m.settingsPaintRow(settingsRowRetryMaxRetries, fmt.Sprintf("◂ %d ▸", m.projectSettings.EffectiveRetry().MaxRetries), innerW, false),
		m.settingsPaintRow(settingsRowRetryDelay, fmt.Sprintf("◂ %ds ▸", m.projectSettings.EffectiveRetry().DelaySeconds), innerW, false),
	)
	if m.height < settingsUsageMinHeight || m.usageLoaded {
		// Keep compact and usage cards stable. The master switch remains
		// visible; the detailed skill controls appear when the full card has
		// room for them.
		insertAt := 0
		for index, line := range out {
			if line.kind == settingsLineHeader && line.text == "agent loop" {
				insertAt = index
				break
			}
		}
		out = append(out, settingsPaintLine{})
		copy(out[insertAt+1:], out[insertAt:])
		out[insertAt] = m.settingsPaintRow(settingsRowSkillsEnabled, "["+boolOn(skillCfg.Enabled)+"]", innerW, false)
	} else {
		insertAt := 0
		for index, line := range out {
			if line.kind == settingsLineHeader && line.text == "agent loop" {
				insertAt = index
				break
			}
		}
		skillLines := []settingsPaintLine{
			{kind: settingsLineHeader, row: -1, text: "skills"},
			m.settingsPaintRow(settingsRowSkillsEnabled, "["+boolOn(skillCfg.Enabled)+"]", innerW, false),
			m.settingsPaintRow(settingsRowSkillsAutoDetect, "["+boolOn(skillCfg.AutoDetect)+"]", innerW, false),
			m.settingsPaintRow(settingsRowSkillsLocal, "["+boolOn(skillCfg.IncludeLocal)+"]", innerW, false),
			m.settingsPaintRow(settingsRowSkillsGlobal, "["+boolOn(skillCfg.IncludeGlobal)+"]", innerW, false),
			m.settingsPaintRow(settingsRowSkillsRemember, "["+boolOn(skillCfg.Remember)+"]", innerW, false),
			m.settingsPaintRow(settingsRowSkillsMaxMatches, fmt.Sprintf("◂ %d ▸", skillCfg.MaxAutoMatches), innerW, false),
		}
		out = append(out, make([]settingsPaintLine, len(skillLines))...)
		copy(out[insertAt+len(skillLines):], out[insertAt:])
		copy(out[insertAt:], skillLines)
	}
	if m.height >= settingsUsageMinHeight && m.usageLoaded {
		out = append(out, settingsPaintLine{kind: settingsLineHeader, row: -1, text: "opencode usage"})
		out = append(out,
			m.settingsUsageRow("rolling", m.usage.Rolling, innerW),
			m.settingsUsageRow("weekly", m.usage.Weekly, innerW),
			m.settingsUsageRow("monthly", m.usage.Monthly, innerW),
		)
	} else if m.height >= settingsUsageMinHeight && m.usageErr != "" {
		out = append(out, settingsPaintLine{kind: settingsLineHint, row: -1, text: "opencode usage: " + truncateRunes(m.usageErr, max(settingsUsageMinTextW, innerW-settingsUsageMaxCorners))})
	} else if m.height >= settingsUsageMinHeight {
		out = append(out, settingsPaintLine{kind: settingsLineHint, row: -1, text: "opencode usage: run /usage to load plan usage"})
	}
	return out
}

// settingsUsageRow builds a compact usage line for the settings card.
func (m Model) settingsUsageRow(label string, w opencode.BillingWindow, innerW int) settingsPaintLine {
	stat := "ok"
	if w.RateLimited {
		stat = "limited"
	}
	val := fmt.Sprintf("%d%%  %s", w.Percent, stat)
	if !w.ResetsAt.IsZero() {
		val += "  ·  " + w.ResetsAt.Local().Format("Jan 2 15:04")
	}
	return settingsPaintLine{kind: settingsLineRow, row: -1, text: settingsKVRow(false, false, label, val, innerW), dim: w.RateLimited}
}

func (m Model) settingsPaintRow(row int, value string, innerW int, dim bool) settingsPaintLine {
	return settingsPaintLine{
		kind: settingsLineRow,
		row:  row,
		text: settingsKVRow(m.settingsCursor == row, m.settingsHover == row, settingsRowLabel(row), value, innerW),
		dim:  dim,
	}
}

func settingsRowLabel(row int) string {
	switch row {
	case settingsRowTheme:
		return "theme"
	case settingsRowModel:
		return "new-session model"
	case settingsRowVariant:
		return "new-session variant"
	case settingsRowChildModel:
		return "child model override"
	case settingsRowExploreModel:
		return "explore model"
	case settingsRowRecapEnabled:
		return "recaps enabled"
	case settingsRowRecapModel:
		return "recap model"
	case settingsRowRecapAfterChats:
		return "recap after chats"
	case settingsRowRetryMaxRetries:
		return "api retries"
	case settingsRowRetryDelay:
		return "retry delay"
	case settingsRowSkillsEnabled:
		return "skills enabled"
	case settingsRowSkillsAutoDetect:
		return "skills auto-detect"
	case settingsRowSkillsLocal:
		return "skills local source"
	case settingsRowSkillsGlobal:
		return "skills global source"
	case settingsRowSkillsRemember:
		return "remember skill references"
	case settingsRowSkillsMaxMatches:
		return "skill auto matches"
	case settingsRowLimit:
		return "step limit"
	case settingsRowSteps:
		return "parent max steps"
	case settingsRowCompactAuto:
		return "auto-compact"
	case settingsRowCompactPercent:
		return "compact at"
	case settingsRowAgentsEnabled:
		return "sub-agents"
	case settingsRowAgentsRole:
		return "default role"
	case settingsRowAgentsConcurrent:
		return "max concurrent"
	case settingsRowAgentsQueued:
		return "max queued"
	case settingsRowAgentsChildSteps:
		return "child max steps"
	case settingsRowAgentsTimeout:
		return "child timeout"
	case settingsRowAgentsWriters:
		return "parallel writers"
	case settingsRowBashConfirm:
		return "child bash confirms"
	case settingsRowAllowlistEnabled:
		return "parent bash allowlist"
	case settingsRowAllowlist:
		return "allowed executables"
	default:
		return ""
	}
}

func formatSettingsTimeout(sec int) string {
	if sec <= 0 {
		return "off"
	}
	if sec%settingsTimeoutMinute == 0 {
		return fmt.Sprintf("%dm", sec/settingsTimeoutMinute)
	}
	if sec < settingsTimeoutMinute {
		return fmt.Sprintf("%ds", sec)
	}
	return fmt.Sprintf("%dm%ds", sec/settingsTimeoutMinute, sec%settingsTimeoutMinute)
}

func formatAllowlistValue(names []string, innerW int) string {
	n := len(names)
	if n == 0 {
		return "0 allowed"
	}
	joined := strings.Join(names, ", ")
	budget := max(settingsAllowlistMinBudget, innerW/settingsAllowlistPreviewBudget)
	if lipgloss.Width(joined) <= budget {
		return joined
	}
	return fmt.Sprintf("%d allowed", n)
}

func boolOn(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func settingsKVRow(selected, hovered bool, label, value string, width int) string {
	prefix := "  "
	if selected {
		prefix = "▸ "
	} else if hovered {
		prefix = "• "
	}
	left := prefix + label
	right := value
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		left = truncateRunes(left, max(settingsKVRowMinLeftW, width-lipgloss.Width(right)-1))
		gap = width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) updateSettingsKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.settingsEdit {
		if key.Code == tea.KeyEscape {
			m.settingsEdit = false
			return m, nil
		}
		if key.Code == tea.KeyEnter {
			m.settingsEdit = false
			m.projectSettings.Agents.BashAllowlist = strings.Split(m.settingsEditValue, ",")
			return m.persistSettings(), nil
		}
		if key.Code == tea.KeyBackspace {
			if len(m.settingsEditValue) > 0 {
				m.settingsEditValue = m.settingsEditValue[:len(m.settingsEditValue)-1]
			}
			return m, nil
		}
		if key.Text != "" {
			m.settingsEditValue += key.Text
		}
		return m, nil
	}
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
	case settingsRowTheme:
		return m.cycleTheme(1), nil
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
	case settingsRowChildModel:
		return m.cycleChildModel(1), nil
	case settingsRowExploreModel:
		return m.cycleExploreModel(1), nil
	case settingsRowRecapEnabled:
		return m.setRecapEnabled(!m.projectSettings.EffectiveRecap().Enabled), nil
	case settingsRowRecapModel:
		m.settingsPickDefault = false
		m.settingsPickRecap = true
		m.settingsMode = false
		return m.openKindPicker(pickerKindModel), nil
	case settingsRowRecapAfterChats:
		return m.openSettingInputForm("Recap after chats", "Successful chats before a recap", strconv.Itoa(m.projectSettings.EffectiveRecap().AfterChats), validateRecapAfterChats, func(mod Model, val string) (Model, tea.Cmd) {
			v, _ := strconv.Atoi(val)
			return mod.setRecapAfterChats(v), nil
		})
	case settingsRowRetryMaxRetries:
		return m.openSettingInputForm("API retries", "Retries after a transient 500 or 503", strconv.Itoa(m.projectSettings.EffectiveRetry().MaxRetries), validateRetryMaxRetries, func(mod Model, val string) (Model, tea.Cmd) {
			v, _ := strconv.Atoi(val)
			return mod.setRetryMaxRetries(v), nil
		})
	case settingsRowRetryDelay:
		return m.openSettingInputForm("Retry delay (sec)", "Wait between transient API attempts", strconv.Itoa(m.projectSettings.EffectiveRetry().DelaySeconds), validateRetryDelaySeconds, func(mod Model, val string) (Model, tea.Cmd) {
			v, _ := strconv.Atoi(val)
			return mod.setRetryDelaySeconds(v), nil
		})
	case settingsRowSkillsEnabled:
		return m.setSkillsEnabled(!m.projectSettings.EffectiveSkills().Enabled), nil
	case settingsRowSkillsAutoDetect:
		return m.setSkillsAutoDetect(!m.projectSettings.EffectiveSkills().AutoDetect), nil
	case settingsRowSkillsLocal:
		return m.setSkillsIncludeLocal(!m.projectSettings.EffectiveSkills().IncludeLocal), nil
	case settingsRowSkillsGlobal:
		return m.setSkillsIncludeGlobal(!m.projectSettings.EffectiveSkills().IncludeGlobal), nil
	case settingsRowSkillsRemember:
		return m.setSkillsRemember(!m.projectSettings.EffectiveSkills().Remember), nil
	case settingsRowSkillsMaxMatches:
		return m.openSettingInputForm("Skill auto matches", "Automatic skills per request", strconv.Itoa(m.projectSettings.EffectiveSkills().MaxAutoMatches), validateSkillMaxMatches, func(mod Model, val string) (Model, tea.Cmd) {
			v, _ := strconv.Atoi(val)
			return mod.setSkillsMaxMatches(v), nil
		})
	case settingsRowLimit:
		return m.setLimitEnabled(!m.projectSettings.Slot.LimitEnabled), nil
	case settingsRowSteps:
		if m.projectSettings.Slot.LimitEnabled {
			return m.openSettingInputForm("Max Steps", "Maximum steps per prompt turn", strconv.Itoa(m.projectSettings.Slot.MaxSteps), validateIntSetting, func(mod Model, val string) (Model, tea.Cmd) {
				v, _ := strconv.Atoi(val)
				return mod.setMaxSteps(v), nil
			})
		}
	case settingsRowCompactAuto:
		return m.setCompactAuto(!m.projectSettings.Compaction.Auto), nil
	case settingsRowCompactPercent:
		return m.openSettingInputForm("Compaction Context %", "Percentage of context window (10-90)", strconv.Itoa(m.projectSettings.EffectiveCompaction().Percent), validatePercentSetting, func(mod Model, val string) (Model, tea.Cmd) {
			v, _ := strconv.Atoi(val)
			return mod.setCompactPercent(v), nil
		})
	case settingsRowAgentsEnabled:
		return m.setAgentsEnabled(!m.projectSettings.Agents.Enabled), nil
	case settingsRowAgentsRole:
		return m.cycleAgentsRole(1), nil
	case settingsRowAgentsConcurrent:
		return m.openSettingInputForm("Max Concurrent Agents", "Max parallel sub-agents", strconv.Itoa(m.projectSettings.Agents.MaxConcurrent), validateIntSetting, func(mod Model, val string) (Model, tea.Cmd) {
			v, _ := strconv.Atoi(val)
			return mod.setAgentsConcurrent(v), nil
		})
	case settingsRowAgentsQueued:
		return m.openSettingInputForm("Max Queued Agents", "Max queued sub-agent backlog", strconv.Itoa(m.projectSettings.Agents.MaxQueued), validateIntSetting, func(mod Model, val string) (Model, tea.Cmd) {
			v, _ := strconv.Atoi(val)
			return mod.setAgentsQueued(v), nil
		})
	case settingsRowAgentsChildSteps:
		return m.openSettingInputForm("Child Agent Steps", "Maximum steps for each child agent", strconv.Itoa(m.projectSettings.Agents.ChildMaxSteps), validateIntSetting, func(mod Model, val string) (Model, tea.Cmd) {
			v, _ := strconv.Atoi(val)
			return mod.setAgentsChildSteps(v), nil
		})
	case settingsRowAgentsTimeout:
		return m.openSettingInputForm("Agent Timeout (sec)", "Child agent execution timeout in seconds", strconv.Itoa(m.projectSettings.Agents.DefaultTimeoutSec), validateIntSetting, func(mod Model, val string) (Model, tea.Cmd) {
			v, _ := strconv.Atoi(val)
			return mod.setAgentsTimeout(v), nil
		})
	case settingsRowAgentsWriters:
		return m.setAgentsWriters(!m.projectSettings.Agents.AllowParallelWriters), nil
	case settingsRowBashConfirm:
		return m.cycleBashConfirm(1), nil
	case settingsRowAllowlistEnabled:
		return m.setAllowlistEnabled(!m.projectSettings.Agents.BashAllowlistEnabled), nil
	case settingsRowAllowlist:
		return m.openSettingInputForm("Bash Allowlist", "Comma-separated allowlisted command prefixes", strings.Join(m.projectSettings.Agents.BashAllowlist, ", "), nil, func(mod Model, val string) (Model, tea.Cmd) {
			var list []string
			for _, p := range strings.Split(val, ",") {
				if s := strings.TrimSpace(p); s != "" {
					list = append(list, s)
				}
			}
			mod.projectSettings.Agents.BashAllowlist = list
			return mod.persistSettings(), nil
		})
	}
	return m, nil
}

func validateIntSetting(s string) error {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v < 1 {
		return fmt.Errorf("must be a positive integer")
	}
	return nil
}

func validatePercentSetting(s string) error {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v < 10 || v > 90 {
		return fmt.Errorf("must be between 10 and 90")
	}
	return nil
}

func validateRecapAfterChats(s string) error {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v < settings.MinRecapAfterChats || v > settings.MaxRecapAfterChats {
		return fmt.Errorf("must be between %d and %d", settings.MinRecapAfterChats, settings.MaxRecapAfterChats)
	}
	return nil
}

func validateSkillMaxMatches(s string) error {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v < settings.MinSkillMaxAutoMatches || v > settings.MaxSkillMaxAutoMatches {
		return fmt.Errorf("must be between %d and %d", settings.MinSkillMaxAutoMatches, settings.MaxSkillMaxAutoMatches)
	}
	return nil
}

func validateRetryMaxRetries(s string) error {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v < settings.MinRetryMaxRetries || v > settings.MaxRetryMaxRetries {
		return fmt.Errorf("must be between %d and %d", settings.MinRetryMaxRetries, settings.MaxRetryMaxRetries)
	}
	return nil
}

func validateRetryDelaySeconds(s string) error {
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || v < settings.MinRetryDelaySeconds || v > settings.MaxRetryDelaySeconds {
		return fmt.Errorf("must be between %d and %d", settings.MinRetryDelaySeconds, settings.MaxRetryDelaySeconds)
	}
	return nil
}

func (m Model) setAllowlistEnabled(on bool) Model {
	m.projectSettings.Agents.BashAllowlistEnabled = on
	return m.persistSettings()
}

func (m Model) adjustSettings(delta int) Model {
	switch m.settingsCursor {
	case settingsRowTheme:
		return m.cycleTheme(delta)
	case settingsRowModel:
		return m.cycleDefaultModel(delta)
	case settingsRowVariant:
		return m.cycleDefaultVariant(delta)
	case settingsRowChildModel:
		return m.cycleChildModel(delta)
	case settingsRowExploreModel:
		return m.cycleExploreModel(delta)
	case settingsRowRecapEnabled:
		if delta > 0 {
			return m.setRecapEnabled(true)
		} else if delta < 0 {
			return m.setRecapEnabled(false)
		}
	case settingsRowRecapModel:
		return m.cycleRecapModel(delta)
	case settingsRowRecapAfterChats:
		return m.setRecapAfterChats(m.projectSettings.EffectiveRecap().AfterChats + delta)
	case settingsRowRetryMaxRetries:
		return m.setRetryMaxRetries(m.projectSettings.EffectiveRetry().MaxRetries + delta)
	case settingsRowRetryDelay:
		return m.setRetryDelaySeconds(m.projectSettings.EffectiveRetry().DelaySeconds + delta)
	case settingsRowSkillsEnabled:
		if delta > 0 {
			return m.setSkillsEnabled(true)
		}
		if delta < 0 {
			return m.setSkillsEnabled(false)
		}
	case settingsRowSkillsAutoDetect:
		if delta != 0 {
			return m.setSkillsAutoDetect(delta > 0)
		}
	case settingsRowSkillsLocal:
		if delta != 0 {
			return m.setSkillsIncludeLocal(delta > 0)
		}
	case settingsRowSkillsGlobal:
		if delta != 0 {
			return m.setSkillsIncludeGlobal(delta > 0)
		}
	case settingsRowSkillsRemember:
		if delta != 0 {
			return m.setSkillsRemember(delta > 0)
		}
	case settingsRowSkillsMaxMatches:
		return m.setSkillsMaxMatches(m.projectSettings.EffectiveSkills().MaxAutoMatches + delta)
	case settingsRowLimit:
		if delta > 0 {
			return m.setLimitEnabled(true)
		} else if delta < 0 {
			return m.setLimitEnabled(false)
		}
	case settingsRowSteps:
		if m.projectSettings.Slot.LimitEnabled {
			return m.setMaxSteps(m.projectSettings.Slot.MaxSteps + delta)
		}
	case settingsRowCompactAuto:
		if delta > 0 {
			return m.setCompactAuto(true)
		} else if delta < 0 {
			return m.setCompactAuto(false)
		}
	case settingsRowCompactPercent:
		return m.setCompactPercent(m.projectSettings.EffectiveCompaction().Percent + delta*settingsCompactPercentStep)
	case settingsRowAgentsEnabled:
		if delta > 0 {
			return m.setAgentsEnabled(true)
		} else if delta < 0 {
			return m.setAgentsEnabled(false)
		}
	case settingsRowAgentsRole:
		return m.cycleAgentsRole(delta)
	case settingsRowAgentsConcurrent:
		return m.setAgentsConcurrent(m.projectSettings.Agents.MaxConcurrent + delta)
	case settingsRowAgentsQueued:
		return m.setAgentsQueued(m.projectSettings.Agents.MaxQueued + delta)
	case settingsRowAgentsChildSteps:
		return m.setAgentsChildSteps(m.projectSettings.Agents.ChildMaxSteps + delta*10)
	case settingsRowAgentsTimeout:
		return m.setAgentsTimeout(m.projectSettings.Agents.DefaultTimeoutSec + delta*settingsTimeoutStepSec)
	case settingsRowAgentsWriters:
		if delta > 0 {
			return m.setAgentsWriters(true)
		} else if delta < 0 {
			return m.setAgentsWriters(false)
		}
	case settingsRowBashConfirm:
		return m.cycleBashConfirm(delta)
	case settingsRowAllowlistEnabled:
		if delta > 0 {
			return m.setAllowlistEnabled(true)
		} else if delta < 0 {
			return m.setAllowlistEnabled(false)
		}
	case settingsRowAllowlist:
		if delta != 0 {
			m.settingsEdit = true
			m.settingsEditValue = strings.Join(m.projectSettings.Agents.BashAllowlist, ", ")
		}
	}
	return m
}

func (m Model) toggleSettingsRow() Model {
	switch m.settingsCursor {
	case settingsRowTheme:
		return m.cycleTheme(1)
	case settingsRowLimit:
		return m.setLimitEnabled(!m.projectSettings.Slot.LimitEnabled)
	case settingsRowSteps:
		if m.projectSettings.Slot.LimitEnabled {
			return m.setMaxSteps(m.projectSettings.Slot.MaxSteps + 1)
		}
	case settingsRowCompactAuto:
		return m.setCompactAuto(!m.projectSettings.Compaction.Auto)
	case settingsRowCompactPercent:
		return m.setCompactPercent(m.projectSettings.EffectiveCompaction().Percent + settingsCompactPercentStep)
	case settingsRowAgentsEnabled:
		return m.setAgentsEnabled(!m.projectSettings.Agents.Enabled)
	case settingsRowAgentsRole:
		return m.cycleAgentsRole(1)
	case settingsRowAgentsConcurrent:
		return m.setAgentsConcurrent(m.projectSettings.Agents.MaxConcurrent + 1)
	case settingsRowAgentsQueued:
		return m.setAgentsQueued(m.projectSettings.Agents.MaxQueued + 1)
	case settingsRowAgentsChildSteps:
		return m.setAgentsChildSteps(m.projectSettings.Agents.ChildMaxSteps + 1)
	case settingsRowAgentsTimeout:
		return m.setAgentsTimeout(m.projectSettings.Agents.DefaultTimeoutSec + settingsTimeoutStepSec)
	case settingsRowAgentsWriters:
		return m.setAgentsWriters(!m.projectSettings.Agents.AllowParallelWriters)
	case settingsRowBashConfirm:
		return m.cycleBashConfirm(1)
	case settingsRowAllowlistEnabled:
		return m.setAllowlistEnabled(!m.projectSettings.Agents.BashAllowlistEnabled)
	case settingsRowAllowlist:
		m.settingsEdit = true
		m.settingsEditValue = strings.Join(m.projectSettings.Agents.BashAllowlist, ", ")
	case settingsRowModel:
		return m.cycleDefaultModel(1)
	case settingsRowVariant:
		return m.cycleDefaultVariant(1)
	case settingsRowChildModel:
		return m.cycleChildModel(1)
	case settingsRowExploreModel:
		return m.cycleExploreModel(1)
	case settingsRowRecapEnabled:
		return m.setRecapEnabled(!m.projectSettings.EffectiveRecap().Enabled)
	case settingsRowRecapModel:
		return m.cycleRecapModel(1)
	case settingsRowRetryMaxRetries:
		return m.setRetryMaxRetries(m.projectSettings.EffectiveRetry().MaxRetries + 1)
	case settingsRowRetryDelay:
		return m.setRetryDelaySeconds(m.projectSettings.EffectiveRetry().DelaySeconds + 1)
	case settingsRowSkillsEnabled:
		return m.setSkillsEnabled(!m.projectSettings.EffectiveSkills().Enabled)
	case settingsRowSkillsAutoDetect:
		return m.setSkillsAutoDetect(!m.projectSettings.EffectiveSkills().AutoDetect)
	case settingsRowSkillsLocal:
		return m.setSkillsIncludeLocal(!m.projectSettings.EffectiveSkills().IncludeLocal)
	case settingsRowSkillsGlobal:
		return m.setSkillsIncludeGlobal(!m.projectSettings.EffectiveSkills().IncludeGlobal)
	case settingsRowSkillsRemember:
		return m.setSkillsRemember(!m.projectSettings.EffectiveSkills().Remember)
	case settingsRowSkillsMaxMatches:
		return m.setSkillsMaxMatches(m.projectSettings.EffectiveSkills().MaxAutoMatches + 1)
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

func (m Model) cycleRecapModel(delta int) Model {
	list := m.models
	if len(list) == 0 {
		list = []string{settings.DefaultModelID}
	}
	cur := m.projectSettings.EffectiveRecap().Model
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
	return m.setRecapModel(list[idx])
}

func (m Model) inheritModelChoices() []string {
	out := []string{""}
	seen := map[string]bool{"": true}
	for _, id := range m.models {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func (m Model) cycleChildModel(delta int) Model {
	list := m.inheritModelChoices()
	idx := indexOfString(list, m.projectSettings.Agents.ModelOverride)
	if idx < 0 {
		idx = 0
	}
	idx = (idx + delta) % len(list)
	if idx < 0 {
		idx += len(list)
	}
	m.projectSettings.Agents.ModelOverride = list[idx]
	return m.rebuildSubMgr().persistSettings()
}

func (m Model) cycleExploreModel(delta int) Model {
	list := m.inheritModelChoices()
	idx := indexOfString(list, m.projectSettings.Agents.ExploreModel)
	if idx < 0 {
		idx = 0
	}
	idx = (idx + delta) % len(list)
	if idx < 0 {
		idx += len(list)
	}
	m.projectSettings.Agents.ExploreModel = list[idx]
	return m.rebuildSubMgr().persistSettings()
}

func (m Model) cycleAgentsRole(delta int) Model {
	list := []string{"explore", "plan", "general"}
	cur := m.projectSettings.Agents.DefaultRole
	idx := indexOfString(list, cur)
	if idx < 0 {
		idx = 0
	}
	idx = (idx + delta) % len(list)
	if idx < 0 {
		idx += len(list)
	}
	m.projectSettings.Agents.DefaultRole = list[idx]
	return m.rebuildSubMgr().persistSettings()
}

func (m Model) cycleBashConfirm(delta int) Model {
	list := []string{"parent", "deny"}
	cur := m.projectSettings.Agents.BashConfirm
	idx := indexOfString(list, cur)
	if idx < 0 {
		idx = 0
	}
	idx = (idx + delta) % len(list)
	if idx < 0 {
		idx += len(list)
	}
	m.projectSettings.Agents.BashConfirm = list[idx]
	return m.rebuildSubMgr().persistSettings()
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

func (m Model) cycleTheme(delta int) Model {
	if delta == 0 {
		return m
	}
	next := settings.DefaultTheme
	if m.projectSettings.EffectiveTheme() == "dark" {
		next = "light"
	}
	return m.setTheme(next)
}

func (m Model) setTheme(value string) Model {
	m.projectSettings.Appearance.Theme = settings.NormalizeTheme(value)
	theme.SetMode(m.projectSettings.EffectiveTheme())
	configureThemeStyles()
	m.prompt.SetStyles(promptStyles())
	m.layout = layoutSnap{}
	m.syncTranscript()
	return m.persistSettings()
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

func (m Model) setRecapEnabled(on bool) Model {
	m.projectSettings.Recap.Enabled = on
	if !on {
		m.successfulRecapChats = 0
	}
	return m.persistSettings()
}

func (m Model) setRecapModel(id string) Model {
	id = strings.TrimSpace(id)
	if id == "" {
		id = settings.DefaultModelID
	}
	m.projectSettings.Recap.Model = id
	return m.persistSettings()
}

func (m Model) setRecapAfterChats(value int) Model {
	if value < settings.MinRecapAfterChats {
		value = settings.MinRecapAfterChats
	}
	if value > settings.MaxRecapAfterChats {
		value = settings.MaxRecapAfterChats
	}
	m.projectSettings.Recap.AfterChats = value
	return m.persistSettings()
}

func (m Model) setRetryMaxRetries(value int) Model {
	if value < settings.MinRetryMaxRetries {
		value = settings.MinRetryMaxRetries
	}
	if value > settings.MaxRetryMaxRetries {
		value = settings.MaxRetryMaxRetries
	}
	m.projectSettings.Retry.MaxRetries = value
	return m.persistSettings()
}

func (m Model) setRetryDelaySeconds(value int) Model {
	if value < settings.MinRetryDelaySeconds {
		value = settings.MinRetryDelaySeconds
	}
	if value > settings.MaxRetryDelaySeconds {
		value = settings.MaxRetryDelaySeconds
	}
	m.projectSettings.Retry.DelaySeconds = value
	return m.persistSettings()
}

func (m Model) setSkillsEnabled(on bool) Model {
	m.projectSettings.Skills.Enabled = on
	if !on {
		m.activeSkills = nil
	}
	return m.persistSettings()
}

func (m Model) setSkillsAutoDetect(on bool) Model {
	m.projectSettings.Skills.AutoDetect = on
	return m.persistSettings()
}

func (m Model) setSkillsIncludeLocal(on bool) Model {
	m.projectSettings.Skills.IncludeLocal = on
	return m.persistSettings()
}

func (m Model) setSkillsIncludeGlobal(on bool) Model {
	m.projectSettings.Skills.IncludeGlobal = on
	return m.persistSettings()
}

func (m Model) setSkillsRemember(on bool) Model {
	m.projectSettings.Skills.Remember = on
	return m.persistSettings()
}

func (m Model) setSkillsMaxMatches(value int) Model {
	if value < settings.MinSkillMaxAutoMatches {
		value = settings.MinSkillMaxAutoMatches
	}
	if value > settings.MaxSkillMaxAutoMatches {
		value = settings.MaxSkillMaxAutoMatches
	}
	m.projectSettings.Skills.MaxAutoMatches = value
	return m.persistSettings()
}

func (m Model) setLimitEnabled(on bool) Model {
	m.projectSettings.Slot.LimitEnabled = on
	m.maxSteps = m.projectSettings.EffectiveMaxSteps()
	m = m.persistSettings()
	return m
}

func (m Model) setCompactAuto(on bool) Model {
	m.projectSettings.Compaction.Auto = on
	return m.persistSettings()
}

func (m Model) setCompactPercent(n int) Model {
	if n < settings.MinCompactPercent {
		n = settings.MinCompactPercent
	}
	if n > settings.MaxCompactPercent {
		n = settings.MaxCompactPercent
	}
	m.projectSettings.Compaction.Percent = n
	return m.persistSettings()
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
	if m.projectSettings.Agents.MaxQueued < n {
		m.projectSettings.Agents.MaxQueued = n
	}
	m = m.rebuildSubMgr()
	m = m.persistSettings()
	return m
}

func (m Model) setAgentsQueued(n int) Model {
	n = clampSettingsQueued(n, m.projectSettings.Agents.MaxConcurrent)
	m.projectSettings.Agents.MaxQueued = n
	m = m.rebuildSubMgr()
	m = m.persistSettings()
	return m
}

func clampSettingsQueued(n, concurrent int) int {
	if n < 1 {
		n = 1
	}
	if n > settingsMaxQueuedCap {
		n = settingsMaxQueuedCap
	}
	if n < concurrent {
		n = concurrent
	}
	return n
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

func (m Model) setAgentsTimeout(n int) Model {
	if n < 0 {
		n = 0
	}
	m.projectSettings.Agents.DefaultTimeoutSec = n
	m = m.rebuildSubMgr()
	m = m.persistSettings()
	return m
}

func (m Model) setAgentsWriters(on bool) Model {
	m.projectSettings.Agents.AllowParallelWriters = on
	m = m.rebuildSubMgr()
	m = m.persistSettings()
	return m
}

func (m Model) persistSettings() Model {
	retry := m.projectSettings.EffectiveRetry()
	if m.client != nil {
		m.client.SetRetryPolicy(opencode.RetryPolicy{
			MaxRetries: retry.MaxRetries,
			Delay:      time.Duration(retry.DelaySeconds) * time.Second,
		})
	}
	if m.settingsPath == "" {
		return m
	}
	if err := settings.Save(m.settingsPath, m.projectSettings); err != nil {
		m.err = err.Error()
	}
	return m
}

// settingsCloseRect is the screen rectangle of the [x] close control on the
// centered card header. Prefers the layout snapshot built with the same paint.
func (m Model) settingsCloseRect() (x0, y, x1 int, ok bool) {
	if !m.settingsMode {
		return 0, 0, 0, false
	}
	m = m.ensureLayout()
	if m.layout.settingsCloseOK {
		return m.layout.settingsCloseX0, m.layout.settingsCloseY, m.layout.settingsCloseX1, true
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
// scanning the painted full-screen card for the row label.
func (m Model) settingsRowAtScreenY(y int) (row int, ok bool) {
	if !m.settingsMode {
		return 0, false
	}
	m = m.ensureLayout()
	if m.layout.settingsRowByY != nil {
		if row, ok = m.layout.settingsRowByY[y]; ok {
			return row, true
		}
	}
	paint := m.layout.settingsPaint
	if paint == "" {
		paint = m.settingsScreen()
	}
	return settingsRowFromPaintedLine(plainLine(paint, y))
}

func settingsRowFromPaintedLine(plain string) (row int, ok bool) {
	for r := 0; r < settingsRowCount; r++ {
		if settingsLineHasLabel(plain, settingsRowLabel(r)) {
			return r, true
		}
	}
	return 0, false
}

// settingsLineHasLabel reports whether plain is a KV control row for label,
// not a lone section header such as "sub-agents".
func settingsLineHasLabel(plain, label string) bool {
	if label == "" {
		return false
	}
	return strings.Contains(plain, "▸ "+label) || strings.Contains(plain, "  "+label)
}

// settingsListScreenTop is the first settings control row on the painted
// screen (new-session model).
func (m Model) settingsListScreenTop() int {
	m = m.ensureLayout()
	label := settingsRowLabel(settingsRowModel)
	if m.layout.settingsRowByY != nil {
		best := -1
		for y, row := range m.layout.settingsRowByY {
			if row == settingsRowModel && (best < 0 || y < best) {
				best = y
			}
		}
		if best >= 0 {
			return best
		}
	}
	paint := m.layout.settingsPaint
	if paint == "" {
		paint = m.settingsScreen()
	}
	for i, line := range strings.Split(paint, "\n") {
		if settingsLineHasLabel(ansi.Strip(line), label) {
			return i
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
	m = m.ensureLayout()
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
	paint := m.layout.settingsPaint
	if paint == "" {
		paint = m.settingsScreen()
	}
	line := plainLine(paint, y)
	switch row {
	case settingsRowTheme:
		if dec, inc := hitStepChevrons(line, x); dec || inc {
			return m.cycleTheme(1), nil, true
		}
		return m.cycleTheme(1), nil, true
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
	case settingsRowChildModel:
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.cycleChildModel(-1), nil, true
		} else if inc {
			return m.cycleChildModel(1), nil, true
		}
		return m.cycleChildModel(1), nil, true
	case settingsRowExploreModel:
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.cycleExploreModel(-1), nil, true
		} else if inc {
			return m.cycleExploreModel(1), nil, true
		}
		return m.cycleExploreModel(1), nil, true
	case settingsRowRecapEnabled:
		return m.setRecapEnabled(!m.projectSettings.EffectiveRecap().Enabled), nil, true
	case settingsRowRecapModel:
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.cycleRecapModel(-1), nil, true
		} else if inc {
			return m.cycleRecapModel(1), nil, true
		}
		if button == tea.MouseLeft {
			next, cmd := m.activateSettingsRow()
			return next, cmd, true
		}
		return m.cycleRecapModel(1), nil, true
	case settingsRowRecapAfterChats:
		current := m.projectSettings.EffectiveRecap().AfterChats
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.setRecapAfterChats(current - 1), nil, true
		} else if inc {
			return m.setRecapAfterChats(current + 1), nil, true
		}
		if button == tea.MouseLeft {
			next, cmd := m.activateSettingsRow()
			return next, cmd, true
		}
		return m.setRecapAfterChats(current + 1), nil, true
	case settingsRowRetryMaxRetries:
		current := m.projectSettings.EffectiveRetry().MaxRetries
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.setRetryMaxRetries(current - 1), nil, true
		} else if inc {
			return m.setRetryMaxRetries(current + 1), nil, true
		}
		if button == tea.MouseLeft {
			next, cmd := m.activateSettingsRow()
			return next, cmd, true
		}
		return m.setRetryMaxRetries(current + 1), nil, true
	case settingsRowRetryDelay:
		current := m.projectSettings.EffectiveRetry().DelaySeconds
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.setRetryDelaySeconds(current - 1), nil, true
		} else if inc {
			return m.setRetryDelaySeconds(current + 1), nil, true
		}
		if button == tea.MouseLeft {
			next, cmd := m.activateSettingsRow()
			return next, cmd, true
		}
		return m.setRetryDelaySeconds(current + 1), nil, true
	case settingsRowSkillsEnabled:
		return m.setSkillsEnabled(!m.projectSettings.EffectiveSkills().Enabled), nil, true
	case settingsRowSkillsAutoDetect:
		return m.setSkillsAutoDetect(!m.projectSettings.EffectiveSkills().AutoDetect), nil, true
	case settingsRowSkillsLocal:
		return m.setSkillsIncludeLocal(!m.projectSettings.EffectiveSkills().IncludeLocal), nil, true
	case settingsRowSkillsGlobal:
		return m.setSkillsIncludeGlobal(!m.projectSettings.EffectiveSkills().IncludeGlobal), nil, true
	case settingsRowSkillsRemember:
		return m.setSkillsRemember(!m.projectSettings.EffectiveSkills().Remember), nil, true
	case settingsRowSkillsMaxMatches:
		current := m.projectSettings.EffectiveSkills().MaxAutoMatches
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.setSkillsMaxMatches(current - 1), nil, true
		} else if inc {
			return m.setSkillsMaxMatches(current + 1), nil, true
		}
		return m.setSkillsMaxMatches(current + 1), nil, true
	case settingsRowLimit:
		return m.setLimitEnabled(!m.projectSettings.Slot.LimitEnabled), nil, true
	case settingsRowCompactAuto:
		return m.setCompactAuto(!m.projectSettings.Compaction.Auto), nil, true
	case settingsRowCompactPercent:
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.setCompactPercent(m.projectSettings.EffectiveCompaction().Percent - settingsCompactPercentStep), nil, true
		} else if inc {
			return m.setCompactPercent(m.projectSettings.EffectiveCompaction().Percent + settingsCompactPercentStep), nil, true
		}
		return m.setCompactPercent(m.projectSettings.EffectiveCompaction().Percent + settingsCompactPercentStep), nil, true
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
	case settingsRowAgentsEnabled:
		return m.setAgentsEnabled(!m.projectSettings.Agents.Enabled), nil, true
	case settingsRowAgentsRole:
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.cycleAgentsRole(-1), nil, true
		} else if inc {
			return m.cycleAgentsRole(1), nil, true
		}
		return m.cycleAgentsRole(1), nil, true
	case settingsRowAgentsConcurrent:
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.setAgentsConcurrent(m.projectSettings.Agents.MaxConcurrent - 1), nil, true
		} else if inc {
			return m.setAgentsConcurrent(m.projectSettings.Agents.MaxConcurrent + 1), nil, true
		}
		return m.setAgentsConcurrent(m.projectSettings.Agents.MaxConcurrent + 1), nil, true
	case settingsRowAgentsQueued:
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.setAgentsQueued(m.projectSettings.Agents.MaxQueued - 1), nil, true
		} else if inc {
			return m.setAgentsQueued(m.projectSettings.Agents.MaxQueued + 1), nil, true
		}
		return m.setAgentsQueued(m.projectSettings.Agents.MaxQueued + 1), nil, true
	case settingsRowAgentsChildSteps:
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.setAgentsChildSteps(m.projectSettings.Agents.ChildMaxSteps - 1), nil, true
		} else if inc {
			return m.setAgentsChildSteps(m.projectSettings.Agents.ChildMaxSteps + 1), nil, true
		}
		return m.setAgentsChildSteps(m.projectSettings.Agents.ChildMaxSteps + 1), nil, true
	case settingsRowAgentsTimeout:
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.setAgentsTimeout(m.projectSettings.Agents.DefaultTimeoutSec - settingsTimeoutStepSec), nil, true
		} else if inc {
			return m.setAgentsTimeout(m.projectSettings.Agents.DefaultTimeoutSec + settingsTimeoutStepSec), nil, true
		}
		return m.setAgentsTimeout(m.projectSettings.Agents.DefaultTimeoutSec + settingsTimeoutStepSec), nil, true
	case settingsRowAgentsWriters:
		return m.setAgentsWriters(!m.projectSettings.Agents.AllowParallelWriters), nil, true
	case settingsRowBashConfirm:
		if dec, inc := hitStepChevrons(line, x); dec {
			return m.cycleBashConfirm(-1), nil, true
		} else if inc {
			return m.cycleBashConfirm(1), nil, true
		}
		return m.cycleBashConfirm(1), nil, true
	case settingsRowAllowlistEnabled:
		return m.setAllowlistEnabled(!m.projectSettings.Agents.BashAllowlistEnabled), nil, true
	case settingsRowAllowlist:
		m.settingsEdit = true
		m.settingsEditValue = strings.Join(m.projectSettings.Agents.BashAllowlist, ", ")
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
