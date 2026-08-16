package chat

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
)

func (m Model) pickerVPHeight() int {
	reserved := lipgloss.Height(m.headerView()) + 1 + lipgloss.Height(m.promptLine()) + pickerDrawerChrome + 1
	if m.err != "" {
		reserved += lipgloss.Height(errStyle.Width(max(minPaneWidth, m.width)).Render(m.err))
	}
	reserved += minPaneHeight
	available := max(minPaneHeight, m.height-reserved)
	return min(max(minPaneHeight, len(m.pickerItems)), min(pickerMaxRows, available))
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
		kind = "variants"
	}
	header := hintStyle.Render(kind+"  ·  ") + m.pickerSelectedLabel()
	if lipgloss.Width(header) > cardW {
		header = truncateRunes(header, cardW)
	}

	vpH := m.pickerVPHeight()
	body := ""
	if m.modelsErr != "" && m.pickerKind != pickerKindVariant {
		body = errStyle.Render("models unavailable: " + m.modelsErr)
	} else if len(m.pickerItems) == 0 {
		if m.pickerKind == pickerKindVariant {
			if len(m.pickerSource()) == 0 {
				body = hintStyle.Render("no variants for this model")
			} else {
				body = hintStyle.Render("no variants match \"" + m.pickerFilter + "\"")
			}
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
	if m.pickerFromPrompt {
		filter = "type to search  •  enter select  •  esc cancel"
	} else if m.pickerFiltering {
		filter = "filter: " + m.pickerFilter + "▏"
	} else if m.pickerFilter != "" {
		filter = "filter: " + m.pickerFilter + "  •  enter select"
	}
	footer := hintStyle.Width(cardW).Render(filter)
	return lipgloss.NewStyle().Width(cardW).Render(
		lipgloss.JoinVertical(lipgloss.Left, header, body, footer),
	)
}

// pickerContent renders the filtered model list with the cursor marker.
func (m Model) pickerContent(width int) string {
	sel := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	normal := lipgloss.NewStyle().Faint(true)
	var b strings.Builder
	for i, id := range m.pickerItems {
		if i > 0 {
			b.WriteString("\n")
		}
		line := m.pickerRow(id, i == m.pickerCursor, width)
		if i == m.pickerCursor {
			b.WriteString(sel.Render(line))
			continue
		}
		b.WriteString(normal.Render(line))
	}
	return b.String()
}

func (m Model) pickerRow(id string, selected bool, width int) string {
	prefix := "  "
	if selected {
		prefix = "▸ "
	}
	left := prefix + m.pickerItemLabel(id)
	if m.pickerKind == pickerKindVariant {
		return left
	}
	right := modelscache.ProviderOf(m.modelInfos, id)
	if right == "" {
		return left
	}
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
	case 'j', tea.KeyDown:
		if m.pickerCursor < len(m.pickerItems)-1 {
			m.pickerCursor++
			m = m.refreshPickerCursor()
		}
		return m, nil
	case 'k', tea.KeyUp:
		if m.pickerCursor > 0 {
			m.pickerCursor--
			m = m.refreshPickerCursor()
		}
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
	case tea.KeyDown:
		if m.pickerCursor < len(m.pickerItems)-1 {
			m.pickerCursor++
			m = m.refreshPickerCursor()
		}
		return m, nil
	case tea.KeyUp:
		if m.pickerCursor > 0 {
			m.pickerCursor--
			m = m.refreshPickerCursor()
		}
		return m, nil
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
	if m.pickerKind == pickerKindVariant {
		m.variant = m.pickerItems[idx]
		m.syncSessionVariant()
		m = m.finishPickerSelection()
		return m, m.persistVariant()
	}
	m.model = m.pickerItems[idx]
	if !modelscache.HasVariant(m.modelInfos, m.model, m.variant) {
		m.variant = ""
	}
	m.syncSessionVariant()
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
	reopenSettings := m.settingsPickDefault
	m.pickerMode = false
	m.pickerFiltering = false
	m.pickerFromPrompt = false
	m.pickerKind = pickerKindModel
	m.dragOn = false
	m.settingsPickDefault = false
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
		m.pickerVp.EnsureVisible(m.pickerCursor, 0, 1)
	}
	return m
}

func (m *Model) applyFilter() {
	m.pickerItems = nil
	needle := strings.ToLower(m.pickerFilter)
	for _, id := range m.pickerSource() {
		if modelMatchesFilter(id, modelscache.ProviderOf(m.modelInfos, id), needle) {
			m.pickerItems = append(m.pickerItems, id)
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
		m.pickerVp.EnsureVisible(m.pickerCursor, 0, 1)
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
	for i, id := range m.pickerItems {
		if id == current {
			m.pickerCursor = i
			break
		}
	}
	m.pickerVp.SetContent(m.pickerContent(m.pickerVp.Width()))
	if m.pickerCursor == 0 {
		m.pickerVp.GotoTop()
	} else {
		m.pickerVp.EnsureVisible(m.pickerCursor, 0, 1)
	}
	m.pickerMode = true
	return m
}

func (m Model) pickerSource() []string {
	if m.pickerKind == pickerKindVariant {
		info, ok := modelscache.InfoOf(m.modelInfos, m.modelLabel())
		if !ok {
			return nil
		}
		return info.Variants
	}
	return m.models
}

func (m Model) pickerSelectedValue() string {
	if m.pickerKind == pickerKindVariant {
		return m.variant
	}
	current := m.model
	if current == "" && m.client != nil {
		current = m.client.Model()
	}
	return current
}

func (m Model) pickerSelectedLabel() string {
	if m.pickerKind == pickerKindVariant {
		if m.variant != "" {
			return m.variant
		}
		return "provider default"
	}
	current := m.model
	if current == "" && m.client != nil {
		current = m.client.Model()
	}
	if current == "" {
		return "provider default"
	}
	return current
}

func (m Model) pickerItemLabel(id string) string {
	if m.pickerKind == pickerKindVariant {
		return id
	}
	info, ok := modelscache.InfoOf(m.modelInfos, id)
	if ok && modelscache.IsFree(info) {
		return id + "  free"
	}
	if modelscache.IsFree(modelscache.Info{ID: id}) {
		return id + "  free"
	}
	return id
}

func (m Model) persistSelection() tea.Cmd {
	if m.session == nil || m.store == nil {
		return nil
	}
	sid, model, variant := m.session.ID, m.model, m.variant
	return func() tea.Msg {
		if err := m.store.UpdateSessionModel(context.Background(), sid, model); err != nil {
			return errMsg{err: err}
		}
		if err := m.store.UpdateSessionVariant(context.Background(), sid, variant); err != nil {
			return errMsg{err: err}
		}
		return nil
	}
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
