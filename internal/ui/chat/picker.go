package chat

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/provider"
	"github.com/chinmay-sawant/lazykoder/internal/skills"
)

// pickerRowMinLeftW is the minimum width left for the model label when a
// provider/status column is shown on the same picker row.
const pickerRowMinLeftW = 4

type pickerLine struct {
	itemIndex int
	provider  string
}

type modelChoice struct {
	id       string
	provider string
	info     modelscache.Info
}

func (m Model) pickerVPHeight() int {
	reserved := lipgloss.Height(m.headerView()) + 1 + lipgloss.Height(m.promptLine()) + pickerDrawerChrome + 1
	if m.err != "" {
		reserved += lipgloss.Height(errStyle.Width(max(minPaneWidth, m.width)).Render(m.err))
	}
	reserved += minPaneHeight
	available := max(minPaneHeight, m.height-reserved)
	return min(max(minPaneHeight, len(m.pickerLines())), min(pickerMaxRows, available))
}

func (m Model) pickerDrawerWidth() int {
	return max(minPaneWidth, m.width-cardBorder)
}

// pickerView renders the model list as a full-width drawer above the prompt,
// matching the slash command menu.
func (m Model) pickerView() string {
	cardW := m.pickerDrawerWidth()
	kind := "models"
	if m.pickerKind == pickerKindVariant {
		kind = "reasoning"
	} else if m.pickerKind == pickerKindSkills {
		kind = "skills"
	} else if m.pickerKind == pickerKindProvider {
		kind = "providers"
	}
	meta := m.pickerSelectedLabel()

	vpH := m.pickerVPHeight()
	body := ""
	if m.modelsErr != "" && m.pickerKind != pickerKindVariant && m.pickerKind != pickerKindSkills && m.pickerKind != pickerKindProvider {
		body = errStyle.Render("models unavailable: " + m.modelsErr)
	} else if len(m.pickerItems) == 0 {
		if m.pickerKind == pickerKindVariant {
			if len(m.pickerSource()) == 0 {
				body = hintStyle.Render("no variants for this model")
			} else {
				body = hintStyle.Render("no variants match \"" + m.pickerFilter + "\"")
			}
		} else if m.pickerKind == pickerKindSkills && len(m.pickerSkillItems) == 0 {
			body = hintStyle.Render("no skills discovered")
		} else if m.pickerKind == pickerKindProvider {
			body = hintStyle.Render("no providers available")
		} else if len(m.models) == 0 {
			body = hintStyle.Render("no models loaded")
		} else {
			body = hintStyle.Render("no models match \"" + m.pickerFilter + "\"")
		}
	} else {
		vpW := m.pickerVp.Width()
		body = withScrollbar(m.pickerVp.View(), vpW, vpH,
			m.pickerVp.ScrollPercent(), m.pickerVp.TotalLineCount() > vpH)
	}

	filter := "filter /  •  r refresh  •  enter select  •  esc cancel"
	switch m.pickerKind {
	case pickerKindVariant:
		filter = "enter select  •  esc cancel  •  sent as reasoning_effort"
	case pickerKindSkills:
		filter = "filter /  •  enter activate  •  esc cancel"
	case pickerKindProvider:
		filter = "filter /  •  enter select or sign in  •  esc cancel"
	}
	if m.pickerFromPrompt {
		filter = "type to search  •  enter select  •  esc cancel"
	} else if m.pickerFiltering {
		filter = "filter: " + m.pickerFilter + "▏"
	} else if m.pickerFilter != "" {
		filter = "filter: " + m.pickerFilter + "  •  enter select"
	}
	return drawerChrome(kind, meta, body, filter, cardW)
}

// pickerContent renders the filtered model list with the cursor marker.
func (m Model) pickerContent(width int) string {
	var b strings.Builder
	for lineNumber, line := range m.pickerLines() {
		if lineNumber > 0 {
			b.WriteString("\n")
		}
		if line.itemIndex < 0 {
			b.WriteString(drawerHeaderTitleStyle.Render(modelProviderLabel(line.provider)))
			continue
		}
		id := m.pickerItems[line.itemIndex]
		selected := line.itemIndex == m.pickerCursor
		row := m.pickerRow(id, m.modelProviderAt(line.itemIndex), selected, width)
		if selected {
			b.WriteString(drawerSelectedStyle.MaxWidth(width).Width(width).Render(row))
			continue
		}
		b.WriteString(drawerNormalStyle.Render(row))
	}
	return b.String()
}

// pickerLines adds non-selectable provider headings only to the model picker.
// Keyboard cursors stay item-based so navigation remains stable while mouse
// hit-testing maps visual rows back to the same selectable item index.
func (m Model) pickerLines() []pickerLine {
	if m.pickerKind != pickerKindModel {
		lines := make([]pickerLine, len(m.pickerItems))
		for index := range m.pickerItems {
			lines[index] = pickerLine{itemIndex: index}
		}
		return lines
	}
	byProvider := make(map[string][]int, len(modelPickerProviderIDs))
	var unclassified []int
	for index := range m.pickerItems {
		providerID := m.modelProviderAt(index)
		if providerID == "" {
			unclassified = append(unclassified, index)
			continue
		}
		byProvider[providerID] = append(byProvider[providerID], index)
	}
	lines := make([]pickerLine, 0, len(m.pickerItems)+len(byProvider))
	for _, providerID := range modelPickerProviderIDs {
		items := byProvider[providerID]
		if len(items) == 0 {
			continue
		}
		lines = append(lines, pickerLine{itemIndex: -1, provider: providerID})
		for _, itemIndex := range items {
			lines = append(lines, pickerLine{itemIndex: itemIndex})
		}
	}
	for _, itemIndex := range unclassified {
		lines = append(lines, pickerLine{itemIndex: itemIndex})
	}
	return lines
}

func (m Model) pickerCursorLine() int {
	for lineNumber, line := range m.pickerLines() {
		if line.itemIndex == m.pickerCursor {
			return lineNumber
		}
	}
	return 0
}

func (m Model) modelProviderAt(index int) string {
	if m.pickerKind == pickerKindModel && index >= 0 && index < len(m.pickerProviderIDs) {
		return m.pickerProviderIDs[index]
	}
	if index < 0 || index >= len(m.pickerItems) {
		return ""
	}
	return m.modelProviderID(m.pickerItems[index])
}

func (m Model) modelProviderID(id string) string {
	info, ok := modelscache.InfoOf(m.modelInfos, id)
	if !ok {
		return ""
	}
	return providerIDForModelInfo(info)
}

func (m Model) selectedModelInfo(id string) (modelscache.Info, bool) {
	return m.modelInfoForProvider(id, m.projectSettings.EffectiveProvider())
}

func (m Model) modelContext(id string) int {
	info, ok := m.selectedModelInfo(id)
	if !ok {
		return 0
	}
	return info.Context
}

func (m Model) modelHasVariant(id, variant string) bool {
	if variant == "" {
		return false
	}
	info, ok := m.selectedModelInfo(id)
	if !ok {
		return false
	}
	for _, candidate := range info.Variants {
		if candidate == variant {
			return true
		}
	}
	return false
}

func (m Model) modelChoices() []modelChoice {
	choices := make([]modelChoice, 0, len(m.modelInfos))
	knownIDs := make(map[string]struct{}, len(m.models))
	for _, id := range m.models {
		knownIDs[id] = struct{}{}
	}
	for _, info := range m.modelInfos {
		if _, ok := knownIDs[info.ID]; !ok {
			continue
		}
		marked, ok := modelscache.InfoOf([]modelscache.Info{info}, info.ID)
		if !ok {
			continue
		}
		choices = append(choices, modelChoice{
			id:       info.ID,
			provider: providerIDForModelInfo(marked),
			info:     marked,
		})
	}
	for _, id := range m.models {
		found := false
		for _, choice := range choices {
			if choice.id == id {
				found = true
				break
			}
		}
		if !found {
			choices = append(choices, modelChoice{id: id})
		}
	}
	return choices
}

func (m Model) modelInfoForProvider(id, providerID string) (modelscache.Info, bool) {
	for _, info := range m.modelInfos {
		if info.ID != id {
			continue
		}
		marked, ok := modelscache.InfoOf([]modelscache.Info{info}, id)
		if !ok {
			continue
		}
		rowProvider := providerIDForModelInfo(marked)
		if providerID == "" || rowProvider == providerID || rowProvider == "" {
			return marked, true
		}
	}
	return modelscache.Info{}, false
}

func providerIDForModelInfo(info modelscache.Info) string {
	switch info.Provider {
	case provider.IDCodex:
		return provider.IDCodex
	case provider.IDOpenCode, modelscache.ProviderOpenCodeGo, modelscache.ProviderOpenCodeZen:
		return provider.IDOpenCode
	}
	if strings.HasPrefix(info.Endpoint, "cli://"+provider.IDCodex) {
		return provider.IDCodex
	}
	if strings.Contains(info.Endpoint, "opencode.ai") {
		return provider.IDOpenCode
	}
	return ""
}

func modelInfosForProvider(infos []modelscache.Info, providerID string) []modelscache.Info {
	out := make([]modelscache.Info, 0, len(infos))
	for _, info := range infos {
		if providerIDForModelInfo(info) == providerID {
			out = append(out, info)
		}
	}
	return out
}

func modelProviderLabel(id string) string {
	descriptor, ok := provider.DescriptorFor(id)
	if !ok {
		return id
	}
	return descriptor.Label
}

func (m Model) pickerRow(id, providerID string, selected bool, width int) string {
	prefix := "  "
	if selected {
		prefix = "▸ "
	}
	left := prefix + m.pickerItemLabelForProvider(id, providerID)
	if m.pickerKind == pickerKindVariant {
		return left
	}
	if m.pickerKind == pickerKindSkills {
		for _, skill := range m.pickerSkillItems {
			if skill.DescriptorPath != id {
				continue
			}
			return left + "  " + string(skill.Scope) + "  " + skill.DisplayPath
		}
		return left
	}
	if m.pickerKind == pickerKindProvider {
		descriptor, ok := provider.DescriptorFor(id)
		if !ok {
			return left
		}
		return pickerRowWithRight(left, m.providerPickerStatus(descriptor.ID), width)
	}
	right := ""
	if info, ok := m.modelInfoForProvider(id, providerID); ok {
		right = info.Provider
	}
	if right == "" {
		right = modelProviderLabel(providerID)
	}
	if right == "" {
		return left
	}
	return pickerRowWithRight(left, right, width)
}

func pickerRowWithRight(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		left = truncateRunes(left, max(pickerRowMinLeftW, width-lipgloss.Width(right)-1))
		gap = width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) providerPickerStatus(id string) string {
	selection := "not selected"
	if m.projectSettings.EffectiveProvider() == id {
		selection = "selected"
	}
	return selection + " • " + m.providerStatus(id).Label
}

func (m Model) resizePicker() Model {
	vpW := max(pickerVpMinWidth, m.pickerDrawerWidth()-1)
	m.pickerVp.SetWidth(vpW)
	m.pickerVp.SetHeight(m.pickerVPHeight())
	return m
}

func (m Model) updatePickerKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	if key.Mod.Contains(tea.ModCtrl) && key.Code == 'c' {
		return m.closeDone(), tea.Quit
	}
	if delta := pickerVerticalDelta(key); delta != 0 {
		return m.movePickerCursor(delta), nil
	}
	if m.pickerFromPrompt {
		return m.updatePromptPickerKey(key)
	}
	if m.pickerFiltering {
		switch key.Code {
		case tea.KeyEscape, tea.KeyEnter:
			m.pickerFiltering = false
			return m, nil
		case tea.KeyBackspace, tea.KeyDelete:
			if len(m.pickerFilter) > 0 {
				m.pickerFilter = m.pickerFilter[:len(m.pickerFilter)-1]
				m.applyFilter()
			}
			return m, nil
		}
		if key.Text != "" {
			m.pickerFilter += key.Text
			m.applyFilter()
		}
		return m, nil
	}
	switch key.Code {
	case 'q', 'Q', 'x', 'X':
		m = m.closePicker()
		return m, nil
	case 'r', 'R':
		if m.pickerKind == pickerKindProvider {
			return m, nil
		}
		m = m.closePicker()
		return m, m.refreshModels
	case tea.KeyEscape:
		m = m.closePicker()
		return m, nil
	case '/':
		m.pickerFiltering = true
		return m, nil
	case tea.KeyEnter:
		return m.selectPickerItem(m.pickerCursor)
	case 'j':
		m = m.movePickerCursor(1)
		return m, nil
	case 'k':
		m = m.movePickerCursor(-1)
		return m, nil
	case tea.KeyPgDown:
		m.pickerVp.PageDown()
		return m, nil
	case tea.KeyPgUp:
		m.pickerVp.PageUp()
		return m, nil
	}
	return m, nil
}

func (m Model) updatePromptPickerKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	switch key.Code {
	case tea.KeyEscape:
		m = m.closePicker()
		m.slashMode = false
		m.slashCursor = 0
		m.prompt.SetValue("/")
		return m, nil
	case tea.KeyEnter:
		return m.selectPickerItem(m.pickerCursor)
	case tea.KeyPgDown:
		m.pickerVp.PageDown()
		return m, nil
	case tea.KeyPgUp:
		m.pickerVp.PageUp()
		return m, nil
	case tea.KeyBackspace, tea.KeyDelete:
		if m.prompt.Value() == "/model " {
			m = m.rememberPrompt()
			m.prompt.SetValue("/mode")
			return m.syncSlash("/mode"), nil
		}
		m = m.rememberPrompt()
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(key)
		return m.syncSlash(m.prompt.Value()), cmd
	}
	if key.Text != "" {
		m = m.rememberPrompt()
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(key)
		return m.syncSlash(m.prompt.Value()), cmd
	}
	return m, nil
}

func pickerVerticalDelta(key tea.KeyPressMsg) int {
	switch key.Code {
	case tea.KeyDown, tea.KeyKpDown:
		return 1
	case tea.KeyUp, tea.KeyKpUp:
		return -1
	default:
		return 0
	}
}

func (m Model) movePickerCursor(delta int) Model {
	if delta == 0 || len(m.pickerItems) == 0 {
		return m
	}
	next := m.pickerCursor + delta
	if next < 0 {
		next = 0
	}
	if next >= len(m.pickerItems) {
		next = len(m.pickerItems) - 1
	}
	if next == m.pickerCursor {
		return m
	}
	m.pickerCursor = next
	return m.refreshPickerCursor()
}

func (m Model) selectPickerItem(idx int) (Model, tea.Cmd) {
	if !m.pickerBuilt || idx < 0 || idx >= len(m.pickerItems) {
		return m, nil
	}
	if m.settingsPickDefault {
		if m.pickerKind == pickerKindVariant {
			m = m.setDefaultVariant(m.pickerItems[idx])
		} else {
			m = m.setDefaultModel(m.pickerItems[idx])
		}
		m = m.finishPickerSelection()
		m.settingsPickDefault = false
		return m.openSettings(), nil
	}
	if m.settingsPickRecap {
		m = m.setRecapModel(m.pickerItems[idx])
		m = m.finishPickerSelection()
		m.settingsPickRecap = false
		return m.openSettings(), nil
	}
	if m.pickerKind == pickerKindSkills {
		for _, skill := range m.pickerSkillItems {
			if skill.DescriptorPath != m.pickerItems[idx] {
				continue
			}
			m.activeSkills = []skills.Skill{skill}
			m.copyNotice = "skill activated: " + skill.Name
			return m.finishPickerSelection(), clearCopyNotice()
		}
		return m.finishPickerSelection(), nil
	}
	if m.pickerKind == pickerKindProvider {
		descriptor, ok := provider.DescriptorFor(m.pickerItems[idx])
		if !ok {
			return m, nil
		}
		if !descriptor.Supported {
			m.err = descriptor.Label + " is not wired yet; choose OpenCode or OpenAI"
			return m.finishPickerSelection(), nil
		}
		return m.selectProvider(descriptor)
	}
	if m.pickerKind == pickerKindVariant {
		m.variant = m.pickerItems[idx]
		m.syncSessionVariant()
		m = m.finishPickerSelection()
		return m, m.persistVariant()
	}
	selected := m.pickerItems[idx]
	prev := m.model
	prevWindow := int64(m.modelContext(prev))
	providerID := m.modelProviderAt(idx)
	providerChanged := providerID != "" && providerID != m.projectSettings.EffectiveProvider()
	if providerChanged {
		descriptor, ok := provider.DescriptorFor(providerID)
		if !ok {
			return m, nil
		}
		m = m.configureProvider(descriptor)
		if m.err != "" {
			return m.finishPickerSelection(), nil
		}
	}
	m.model = selected
	if !m.modelHasVariant(m.model, m.variant) {
		m.variant = ""
	}
	if providerChanged {
		if configured, ok := m.modelDefaults[providerID]; ok && configured.model == m.model &&
			m.modelHasVariant(m.model, configured.variant) {
			m.variant = configured.variant
		}
		m.projectSettings.Model.Default = m.model
		m.projectSettings.Model.Variant = m.variant
		m = m.persistSettings()
	}
	m.syncSessionModel()
	m.syncSessionProvider()
	m.syncSessionVariant()
	m = m.refreshCompactHint(prev, prevWindow)
	m = m.finishPickerSelection()
	return m, m.persistSelection()
}

func (m Model) finishPickerSelection() Model {
	if m.pickerFromPrompt {
		m.prompt.SetValue("")
		m.promptUndo = nil
		m.slashFromPaste = false
	}
	return m.closePicker()
}

func (m Model) closePicker() Model {
	reopenSettings := m.settingsPickDefault || m.settingsPickRecap
	m = m.clearFocus(focusPicker)
	m.pickerKind = pickerKindModel
	m.pickerSkillItems = nil
	m.dragOn = false
	m.settingsPickDefault = false
	m.settingsPickRecap = false
	if reopenSettings {
		return m.openSettings()
	}
	return m
}

func (m Model) refreshPickerCursor() Model {
	m.pickerVp.SetContent(m.pickerContent(m.pickerVp.Width()))
	if m.pickerCursor == 0 {
		m.pickerVp.GotoTop()
	} else {
		m.pickerVp.EnsureVisible(m.pickerCursorLine(), 0, 1)
	}
	return m
}

func (m *Model) applyFilter() {
	m.pickerItems = nil
	m.pickerProviderIDs = nil
	needle := strings.ToLower(m.pickerFilter)
	if m.pickerKind == pickerKindSkills {
		for _, skill := range m.pickerSkillItems {
			haystack := strings.ToLower(skill.Name + " " + skill.Description + " " + strings.Join(skill.Triggers, " ") + " " + skill.DisplayPath)
			if modelMatchesFilter(haystack, "", needle) {
				m.pickerItems = append(m.pickerItems, skill.DescriptorPath)
			}
		}
		if m.pickerCursor >= len(m.pickerItems) {
			m.pickerCursor = max(0, len(m.pickerItems)-1)
		}
		m.pickerVp.SetHeight(m.pickerVPHeight())
		m.pickerVp.SetContent(m.pickerContent(m.pickerVp.Width()))
		return
	}
	if m.pickerKind == pickerKindModel {
		for _, choice := range m.modelChoices() {
			providerLabel := choice.info.Provider
			if providerLabel == "" {
				providerLabel = modelProviderLabel(choice.provider)
			}
			if !modelMatchesFilter(choice.id, providerLabel, needle) {
				continue
			}
			m.pickerItems = append(m.pickerItems, choice.id)
			m.pickerProviderIDs = append(m.pickerProviderIDs, choice.provider)
		}
	} else {
		for _, id := range m.pickerSource() {
			if m.pickerKind == pickerKindProvider {
				descriptor, ok := provider.DescriptorFor(id)
				if !ok || !modelMatchesFilter(id, descriptor.Label+" "+descriptor.EnvKey, needle) {
					continue
				}
				m.pickerItems = append(m.pickerItems, id)
				continue
			}
			if modelMatchesFilter(id, modelscache.ProviderOf(m.modelInfos, id), needle) {
				m.pickerItems = append(m.pickerItems, id)
			}
		}
	}
	if m.pickerCursor >= len(m.pickerItems) {
		m.pickerCursor = max(0, len(m.pickerItems)-1)
	}
	m.pickerVp.SetHeight(m.pickerVPHeight())
	m.pickerVp.SetContent(m.pickerContent(m.pickerVp.Width()))
	if m.pickerCursor == 0 {
		m.pickerVp.GotoTop()
	} else {
		m.pickerVp.EnsureVisible(m.pickerCursorLine(), 0, 1)
	}
}

func modelMatchesFilter(id, provider, needle string) bool {
	if needle == "" {
		return true
	}
	haystack := strings.ToLower(id + " " + provider)
	if modelscache.IsFree(modelscache.Info{ID: id}) {
		haystack += " free"
	}
	if strings.Contains(haystack, needle) {
		return true
	}
	for _, tok := range strings.Fields(needle) {
		if !strings.Contains(haystack, tok) {
			return false
		}
	}
	return len(strings.Fields(needle)) > 0
}

func (m Model) openPicker() Model {
	return m.openKindPicker(pickerKindModel)
}

func (m Model) openVariantPicker() Model {
	return m.openKindPicker(pickerKindVariant)
}

func (m Model) openProviderPicker() Model {
	return m.openKindPicker(pickerKindProvider)
}

func (m Model) openSkillsPicker(query string) (Model, tea.Cmd) {
	m = m.openKindPicker(pickerKindSkills)
	m.pickerFilter = strings.TrimSpace(query)
	if m.pickerFilter != "" {
		m.applyFilter()
	}
	m.skillsScanning = true
	m.activity = "scanning approved skills"
	return m, m.scanSkills
}

func (m Model) scanSkills() tea.Msg {
	cfg := m.projectSettings.EffectiveSkills()
	opts := skills.DefaultOptions(m.workdir)
	opts.IncludeLocal = cfg.IncludeLocal
	opts.IncludeGlobal = cfg.IncludeGlobal
	opts.MaxAutoMatches = cfg.MaxAutoMatches
	opts.MaxBody = cfg.MaxBodyBytes
	catalog, err := skills.Discover(context.Background(), opts)
	return skillsMsg{catalog: catalog, err: err}
}

func (m Model) openKindPicker(kind string) Model {
	if !m.pickerBuilt {
		m.pickerVp = viewport.New(viewport.WithWidth(pickerVpDefaultW), viewport.WithHeight(pickerVpDefaultH))
		m.pickerBuilt = true
		m = m.resizePicker()
	}
	m.pickerKind = kind
	m.pickerFilter = ""
	m.pickerFiltering = false
	m.pickerCursor = 0
	m.applyFilter()
	current := m.pickerSelectedValue()
	activeProvider := m.projectSettings.EffectiveProvider()
	for i, id := range m.pickerItems {
		if id != current {
			continue
		}
		if kind == pickerKindModel && m.modelProviderAt(i) != activeProvider {
			continue
		}
		m.pickerCursor = i
		break
	}
	m.pickerVp.SetContent(m.pickerContent(m.pickerVp.Width()))
	if m.pickerCursor == 0 {
		m.pickerVp.GotoTop()
	} else {
		m.pickerVp.EnsureVisible(m.pickerCursor, 0, 1)
	}
	m = m.setFocus(focusPicker)
	return m
}

func (m Model) pickerSource() []string {
	if m.pickerKind == pickerKindSkills {
		out := make([]string, 0, len(m.pickerSkillItems))
		for _, skill := range m.pickerSkillItems {
			out = append(out, skill.DescriptorPath)
		}
		return out
	}
	if m.pickerKind == pickerKindVariant {
		info, ok := m.modelInfoForProvider(m.modelLabel(), m.projectSettings.EffectiveProvider())
		if !ok {
			return nil
		}
		return info.Variants
	}
	if m.pickerKind == pickerKindProvider {
		return provider.IDs()
	}
	return m.models
}

func (m Model) pickerSelectedValue() string {
	if m.pickerKind == pickerKindSkills {
		if len(m.activeSkills) > 0 {
			return m.activeSkills[0].DescriptorPath
		}
		return ""
	}
	if m.pickerKind == pickerKindVariant {
		return m.variant
	}
	if m.pickerKind == pickerKindProvider {
		return m.projectSettings.EffectiveProvider()
	}
	if m.settingsPickRecap {
		return m.projectSettings.EffectiveRecap().Model
	}
	current := m.model
	if current == "" && m.client != nil {
		current = m.client.Model()
	}
	return current
}

func (m Model) pickerSelectedLabel() string {
	if m.pickerKind == pickerKindSkills {
		if len(m.activeSkills) > 0 {
			return m.activeSkills[0].Name
		}
		return "none"
	}
	if m.pickerKind == pickerKindVariant {
		if m.variant != "" {
			return m.variant
		}
		return "default"
	}
	if m.pickerKind == pickerKindProvider {
		if descriptor, ok := provider.DescriptorFor(m.projectSettings.EffectiveProvider()); ok {
			return descriptor.Label
		}
		return m.projectSettings.EffectiveProvider()
	}
	current := m.model
	if current == "" && m.client != nil {
		current = m.client.Model()
	}
	if current == "" {
		return "default"
	}
	return current
}

func (m Model) pickerItemLabelForProvider(id, providerID string) string {
	if m.pickerKind == pickerKindSkills {
		for _, skill := range m.pickerSkillItems {
			if skill.DescriptorPath == id {
				return skill.Name
			}
		}
		return id
	}
	if m.pickerKind == pickerKindVariant {
		if id == "" {
			return "default"
		}
		return id
	}
	if m.pickerKind == pickerKindProvider {
		if descriptor, ok := provider.DescriptorFor(id); ok {
			return descriptor.Label
		}
		return id
	}
	label := id
	info, ok := m.modelInfoForProvider(id, providerID)
	if ok && modelscache.IsFree(info) {
		label += "  free"
	} else if modelscache.IsFree(modelscache.Info{ID: id}) {
		label += "  free"
	}
	if ok {
		var bits []string
		if info.Context > 0 {
			bits = append(bits, formatTokens(int64(info.Context)))
		}
		if info.InputPerM > 0 {
			bits = append(bits, fmt.Sprintf("$%.2f/M", info.InputPerM))
		}
		if len(bits) > 0 {
			label += "  " + strings.Join(bits, " · ")
		}
	}
	return label
}

func (m Model) persistSelection() tea.Cmd {
	if m.session == nil || m.store == nil {
		return nil
	}
	sid, providerID, model, variant := m.session.ID, m.projectSettings.EffectiveProvider(), m.model, m.variant
	return func() tea.Msg {
		if err := m.store.UpdateSessionProvider(context.Background(), sid, providerID); err != nil {
			return errMsg{err: err}
		}
		if err := m.store.UpdateSessionModel(context.Background(), sid, model); err != nil {
			return errMsg{err: err}
		}
		if err := m.store.UpdateSessionVariant(context.Background(), sid, variant); err != nil {
			return errMsg{err: err}
		}
		return nil
	}
}

func (m *Model) syncSessionModel() {
	if m.session == nil {
		return
	}
	m.session.Model = m.model
}

func (m *Model) syncSessionProvider() {
	if m.session == nil {
		return
	}
	m.session.Provider = m.projectSettings.EffectiveProvider()
}

func (m Model) refreshCompactHint(prev string, prevWindow int64) Model {
	window := int64(m.modelContext(m.modelLabel()))
	pct := m.projectSettings.EffectiveCompaction().Percent
	if agent.NeedsCompact(m.tokensUsed, window, pct) {
		m.pendingCompactReason = agent.CompactReasonShrink
		m.prevModel = prev
		m.prevWindow = prevWindow
		m.compactHint = fmt.Sprintf(
			"next send will compact (window %s -> %s)",
			formatTokens(prevWindow),
			formatTokens(window),
		)
		return m
	}
	m.pendingCompactReason = ""
	m.compactHint = ""
	if window <= 0 || (prevWindow > 0 && window >= prevWindow) {
		m.prevModel = ""
		m.prevWindow = 0
	}
	return m
}

func (m *Model) syncSessionVariant() {
	if m.session == nil {
		return
	}
	if m.variant == "" {
		m.session.Variant = nil
		return
	}
	v := m.variant
	m.session.Variant = &v
}

func (m Model) persistVariant() tea.Cmd {
	if m.session == nil || m.store == nil {
		return nil
	}
	sid, variant := m.session.ID, m.variant
	return func() tea.Msg {
		if err := m.store.UpdateSessionVariant(context.Background(), sid, variant); err != nil {
			return errMsg{err: err}
		}
		return nil
	}
}
