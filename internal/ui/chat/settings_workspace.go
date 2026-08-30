package chat

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

const (
	settingsRowFilter = -2

	settingsRailMinWidth       = 13
	settingsRailMaxWidth       = 18
	settingsRailWidthDivisor   = 4
	settingsRailMaxPaneDivisor = 3
	settingsNoMatchQuoteWidth  = 20
)

type settingsSection uint8

const (
	settingsSectionAppearance settingsSection = iota
	settingsSectionModel
	settingsSectionRecaps
	settingsSectionSkills
	settingsSectionAgentLoop
	settingsSectionCompaction
	settingsSectionSubagents
	settingsSectionSafety
	settingsSectionRequestRetries
)

type settingsSectionDefinition struct {
	id          settingsSection
	label       string
	description string
	rows        []int
}

var settingsSectionDefinitions = []settingsSectionDefinition{
	{
		id:          settingsSectionAppearance,
		label:       "appearance",
		description: "Color mode and terminal contrast.",
		rows:        []int{settingsRowTheme},
	},
	{
		id:          settingsSectionModel,
		label:       "model",
		description: "Defaults for new chat sessions.",
		rows:        []int{settingsRowModel, settingsRowVariant},
	},
	{
		id:          settingsSectionRecaps,
		label:       "recaps",
		description: "Automatic session recap behavior.",
		rows:        []int{settingsRowRecapEnabled, settingsRowRecapModel, settingsRowRecapAfterChats},
	},
	{
		id:          settingsSectionSkills,
		label:       "skills",
		description: "Skill discovery and tool availability.",
		rows: []int{
			settingsRowSkillsEnabled,
			settingsRowSkillsAutoDetect,
			settingsRowSkillsLocal,
			settingsRowSkillsGlobal,
			settingsRowSkillsRemember,
			settingsRowSkillsMaxMatches,
			settingsRowTools,
		},
	},
	{
		id:          settingsSectionAgentLoop,
		label:       "agent loop",
		description: "Parent turn limits and safety caps.",
		rows:        []int{settingsRowLimit, settingsRowSteps},
	},
	{
		id:          settingsSectionCompaction,
		label:       "compaction",
		description: "Automatic context compaction.",
		rows:        []int{settingsRowCompactAuto, settingsRowCompactPercent},
	},
	{
		id:          settingsSectionSubagents,
		label:       "sub-agents",
		description: "Configuration for child agents. Live jobs stay in /agents.",
		rows: []int{
			settingsRowAgentsEnabled,
			settingsRowChildModel,
			settingsRowChildVariant,
			settingsRowExploreModel,
			settingsRowAgentsRole,
			settingsRowAgentsConcurrent,
			settingsRowAgentsQueued,
			settingsRowAgentsChildSteps,
			settingsRowAgentsTimeout,
			settingsRowAgentsWriters,
		},
	},
	{
		id:          settingsSectionSafety,
		label:       "safety",
		description: "Command confirmation and the bash allowlist.",
		rows:        []int{settingsRowBashConfirm, settingsRowAllowlistEnabled, settingsRowAllowlist},
	},
	{
		id:          settingsSectionRequestRetries,
		label:       "request retries",
		description: "Transient API retry behavior.",
		rows:        []int{settingsRowRetryMaxRetries, settingsRowRetryDelay},
	},
}

var settingsRowDescriptions = [...]string{
	"Switch between dark and light color modes.",
	"Choose the model used when a new chat starts.",
	"Choose the reasoning variant for new chats.",
	"Create recap records after completed chats.",
	"Choose the model used for recap summaries.",
	"Set how many successful chats trigger a recap.",
	"Retry transient API failures before reporting an error.",
	"Set the pause between transient API retries.",
	"Turn automatic skill selection on or off.",
	"Let matching request text choose relevant skills.",
	"Include skills declared in the current project.",
	"Include skills installed in the global catalog.",
	"Keep selected skill references for later turns.",
	"Limit automatic skills added to one request.",
	"Choose the tools available to new chats.",
	"Turn the parent-step cap on or off.",
	"Set the maximum parent-agent steps per turn.",
	"Turn automatic context compaction on or off.",
	"Choose the context percentage that starts compaction.",
	"Turn child-agent execution on or off.",
	"Override the default model for child agents.",
	"Choose the reasoning variant for child agents.",
	"Override the model used by the explore role.",
	"Choose the role assigned to new child agents.",
	"Set how many child agents can run together.",
	"Set the maximum waiting child-agent backlog.",
	"Set the maximum steps for each child agent.",
	"Set the default child-agent execution timeout.",
	"Allow child agents to edit in parallel.",
	"Choose how child-agent bash requests are handled.",
	"Turn the parent bash allowlist on or off.",
	"Edit executable prefixes that bypass a bash confirmation.",
}

func settingsSectionForRow(row int) (settingsSection, bool) {
	for _, definition := range settingsSectionDefinitions {
		if containsInt(definition.rows, row) {
			return definition.id, true
		}
	}
	return settingsSectionAppearance, false
}

func settingsSectionDefinitionFor(section settingsSection) settingsSectionDefinition {
	for _, definition := range settingsSectionDefinitions {
		if definition.id == section {
			return definition
		}
	}
	return settingsSectionDefinitions[0]
}

func settingsRowDescription(row int) string {
	if row < 0 || row >= len(settingsRowDescriptions) {
		return ""
	}
	return settingsRowDescriptions[row]
}

func (m Model) settingsFilterActive() bool {
	return m.settingsEdit && m.settingsCursor == settingsRowFilter
}

func (m Model) settingsAllowlistEditing() bool {
	return m.settingsEdit && !m.settingsFilterActive()
}

func (m Model) selectedSettingsRow() int {
	if m.settingsFilterActive() {
		return m.settingsHover
	}
	return m.settingsCursor
}

func (m Model) settingsCurrentSection() settingsSection {
	section, ok := settingsSectionForRow(m.selectedSettingsRow())
	if !ok {
		return settingsSectionAppearance
	}
	return section
}

func (m Model) startSettingsFilter() Model {
	row := m.selectedSettingsRow()
	if _, ok := settingsSectionForRow(row); !ok {
		row = settingsRowTheme
	}
	m.settingsCursor = settingsRowFilter
	m.settingsHover = row
	m.settingsEdit = true
	m.settingsEditValue = ""
	return m.ensureSettingsFilterSelection()
}

func (m Model) clearSettingsFilter() Model {
	row := m.selectedSettingsRow()
	if _, ok := settingsSectionForRow(row); !ok {
		row = settingsRowTheme
	}
	m.settingsCursor = row
	m.settingsHover = -1
	m.settingsEdit = false
	m.settingsEditValue = ""
	return m
}

func (m Model) ensureSettingsFilterSelection() Model {
	if !m.settingsFilterActive() {
		return m
	}
	rows := m.settingsMatchingRows()
	if len(rows) == 0 {
		m.settingsHover = -1
		return m
	}
	if !containsInt(rows, m.settingsHover) {
		m.settingsHover = rows[0]
	}
	return m
}

func (m Model) settingsMatchingRows() []int {
	term := strings.ToLower(strings.TrimSpace(m.settingsEditValue))
	rows := make([]int, 0, settingsRowCount)
	seen := make(map[int]bool, settingsRowCount)
	for _, line := range m.settingsPaintLines(m.settingsContentWidth()) {
		if line.kind != settingsLineRow || line.row < 0 || seen[line.row] {
			continue
		}
		if term != "" && !settingsRowMatches(line.row, term) {
			continue
		}
		seen[line.row] = true
		rows = append(rows, line.row)
	}
	return rows
}

func settingsRowMatches(row int, term string) bool {
	text := strings.ToLower(settingsRowLabel(row) + " " + settingsRowDescription(row))
	return strings.Contains(text, term)
}

func (m Model) moveSettingsSection(delta int) Model {
	if delta == 0 {
		return m
	}
	index := 0
	current := m.settingsCurrentSection()
	for i, definition := range settingsSectionDefinitions {
		if definition.id == current {
			index = i
			break
		}
	}
	index += delta
	if index < 0 {
		index = len(settingsSectionDefinitions) - 1
	}
	if index >= len(settingsSectionDefinitions) {
		index = 0
	}
	return m.selectSettingsSection(settingsSectionDefinitions[index].id)
}

func (m Model) selectSettingsSection(section settingsSection) Model {
	definition := settingsSectionDefinitionFor(section)
	if len(definition.rows) == 0 {
		return m
	}
	m.settingsCursor = definition.rows[0]
	m.settingsHover = -1
	m.settingsEdit = false
	m.settingsEditValue = ""
	return m
}

func (m Model) reopenSettingsFromPicker() Model {
	row := m.settingsCursor
	section, ok := settingsSectionForRow(row)
	if !ok {
		section = settingsSectionAppearance
		row = settingsRowTheme
	}
	return m.openSettingsAt(section, row)
}

func (m Model) settingsWorkspaceWidths() (railW, paneW int) {
	innerW := m.settingsInnerWidth()
	railW = min(settingsRailMaxWidth, max(settingsRailMinWidth, innerW/settingsRailWidthDivisor))
	railW = min(railW, max(1, innerW/settingsRailMaxPaneDivisor))
	paneW = max(1, innerW-railW-1)
	return railW, paneW
}

func (m Model) settingsContentWidth() int {
	_, paneW := m.settingsWorkspaceWidths()
	return paneW
}

func (m Model) settingsWorkspacePaintLines(innerW int) []settingsPaintLine {
	selected := m.selectedSettingsRow()
	term := strings.ToLower(strings.TrimSpace(m.settingsEditValue))
	if m.settingsFilterActive() {
		return m.filteredSettingsPaintLines(innerW, selected, term)
	}

	section := m.settingsCurrentSection()
	definition := settingsSectionDefinitionFor(section)
	lines := []settingsPaintLine{
		{kind: settingsLineHeader, row: -1, text: definition.label},
		{kind: settingsLineHint, row: -1, text: definition.description},
	}
	showUsage := section == settingsSectionModel
	usageLines := false
	for _, line := range m.settingsPaintLines(innerW) {
		if line.kind == settingsLineHeader && line.text == "opencode usage" {
			usageLines = showUsage
			if usageLines {
				lines = append(lines, line)
			}
			continue
		}
		if showUsage && line.kind == settingsLineHint && strings.HasPrefix(line.text, "opencode usage:") {
			lines = append(lines, line)
			continue
		}
		if usageLines && line.kind == settingsLineRow && line.row < 0 {
			lines = append(lines, line)
			continue
		}
		if line.kind != settingsLineRow || line.row < 0 {
			continue
		}
		rowSection, ok := settingsSectionForRow(line.row)
		if !ok || rowSection != section {
			continue
		}
		lines = append(lines, line)
		if line.row == selected {
			lines = append(lines, settingsPaintLine{kind: settingsLineHint, row: -1, text: settingsRowDescription(line.row)})
		}
	}
	return lines
}

func (m Model) filteredSettingsPaintLines(innerW, selected int, term string) []settingsPaintLine {
	lines := make([]settingsPaintLine, 0, settingsRowCount)
	lastSection := settingsSectionRequestRetries + 1
	for _, line := range m.settingsPaintLines(innerW) {
		if line.kind != settingsLineRow || line.row < 0 || !settingsRowMatches(line.row, term) {
			continue
		}
		section, ok := settingsSectionForRow(line.row)
		if !ok {
			continue
		}
		if section != lastSection {
			lines = append(lines, settingsPaintLine{kind: settingsLineHeader, row: -1, text: settingsSectionDefinitionFor(section).label})
			lastSection = section
		}
		lines = append(lines, line)
		if line.row == selected {
			lines = append(lines, settingsPaintLine{kind: settingsLineHint, row: -1, text: settingsRowDescription(line.row)})
		}
	}
	if len(lines) == 0 {
		return []settingsPaintLine{{kind: settingsLineHint, row: -1, text: "no settings match \"" + truncateRunes(term, max(1, innerW-settingsNoMatchQuoteWidth)) + "\""}}
	}
	return lines
}

func (m Model) settingsRailView(width int) string {
	current := m.settingsCurrentSection()
	header := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorAccent()).Render("categories")
	lines := make([]string, 0, len(settingsSectionDefinitions)+1)
	lines = append(lines, header)
	for _, definition := range settingsSectionDefinitions {
		prefix := "· "
		style := lipgloss.NewStyle().Foreground(theme.ColorMute())
		if definition.id == current {
			prefix = "▸ "
			style = lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText())
		}
		lines = append(lines, style.Render(truncateRunes(prefix+definition.label, width)))
	}
	return strings.Join(lines, "\n")
}

func (m Model) settingsContentView(width int) string {
	sel := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Background(theme.ColorBorder())
	hover := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText())
	normal := lipgloss.NewStyle().Foreground(theme.ColorMute())
	header := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorAccent())
	dim := lipgloss.NewStyle().Foreground(theme.ColorMute()).Faint(true)
	selected := m.selectedSettingsRow()

	lines := m.visibleSettingsPaintLines(width)
	var body strings.Builder
	for i, line := range lines {
		if i > 0 {
			body.WriteString("\n")
		}
		text := line.text
		if line.kind == settingsLineHint || line.kind == settingsLineHeader {
			text = truncateRunes(text, width)
		}
		switch {
		case line.kind == settingsLineHeader:
			body.WriteString(header.MaxWidth(width).Render(text))
		case line.kind == settingsLineHint:
			body.WriteString(hintStyle.MaxWidth(width).Render(text))
		case line.kind == settingsLineRow && line.row == selected:
			body.WriteString(sel.MaxWidth(width).Render(text))
		case line.kind == settingsLineRow && line.row == m.settingsHover && !m.settingsFilterActive():
			body.WriteString(hover.MaxWidth(width).Render(text))
		case line.dim:
			body.WriteString(dim.MaxWidth(width).Render(text))
		default:
			body.WriteString(normal.MaxWidth(width).Render(text))
		}
	}
	return body.String()
}

func (m Model) settingsWorkspaceView() string {
	railW, paneW := m.settingsWorkspaceWidths()
	rail := m.settingsRailView(railW)
	pane := m.settingsContentView(paneW)
	height := max(lipgloss.Height(rail), lipgloss.Height(pane))
	rail = lipgloss.NewStyle().Width(railW).Height(height).Render(rail)
	pane = lipgloss.NewStyle().Width(paneW).Height(height).Render(pane)
	divider := lipgloss.NewStyle().Foreground(theme.ColorBorder()).Render(strings.Repeat("│\n", max(0, height-1)) + "│")
	return lipgloss.JoinHorizontal(lipgloss.Top, rail, divider, pane)
}

func (m Model) settingsFilterLine(width int) string {
	prefix := "  "
	value := "type to narrow settings"
	style := hintStyle
	if m.settingsFilterActive() {
		prefix = "▸ "
		value = m.settingsEditValue
		if value == "" {
			value = "type to narrow settings"
		}
		style = lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText())
	}
	return style.Width(width).MaxWidth(width).Render(truncateRunes(prefix+"filter settings [/]: "+value, width))
}

func settingsSectionFromPaintedLine(plain string) (settingsSection, bool) {
	for _, definition := range settingsSectionDefinitions {
		if strings.Contains(plain, "· "+definition.label) || strings.Contains(plain, "▸ "+definition.label) {
			return definition.id, true
		}
	}
	return settingsSectionAppearance, false
}

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
