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
	available := max(minPaneHeight, m.height*70/percentBase-pickerFixedRows)
	return min(max(minPaneHeight, len(m.pickerItems)), min(pickerMaxRows, available))
}

// pickerView renders the model settings card with a label rail on the left,
// the selectable model list on the right, and a filter prompt at the bottom.
func (m Model) pickerView() string {
	cardW := m.overlayWidth()
	innerW := max(minPaneWidth, cardW-cardBorder)
	leftW, rightW := splitPaneWidths(innerW)

	current := m.pickerSelectedLabel()
	kindLabel := "MODEL"
	rightTitle := "AVAILABLE MODELS"
	if m.pickerKind == pickerKindVariant {
		kindLabel = "VARIANT"
		rightTitle = "AVAILABLE VARIANTS"
	}
	left := lipgloss.NewStyle().Width(leftW).Render(strings.Join([]string{
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Render(kindLabel),
		hintStyle.Render("Selected"),
		current,
	}, "\n"))

	vpH := m.pickerVPHeight()
	rightHeader := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Render(rightTitle)
	rightBody := ""
	if m.modelsErr != "" {
		rightBody = errStyle.Render("models unavailable: " + m.modelsErr)
	} else if len(m.pickerItems) == 0 {
		if m.pickerKind == pickerKindVariant {
			if len(m.pickerSource()) == 0 {
				rightBody = hintStyle.Render("no variants for this model")
			} else {
				rightBody = hintStyle.Render("no variants match \"" + m.pickerFilter + "\"")
			}
		} else if len(m.models) == 0 {
			rightBody = hintStyle.Render("no models loaded")
		} else {
			rightBody = hintStyle.Render("no models match \"" + m.pickerFilter + "\"")
		}
	} else {
		vpW := m.pickerVp.Width()
		rightBody = withScrollbar(m.pickerVp.View(), vpW, vpH,
			m.pickerVp.ScrollPercent(), m.pickerVp.TotalLineCount() > vpH)
	}
	right := lipgloss.NewStyle().Width(rightW).Render(
		lipgloss.JoinVertical(lipgloss.Left, rightHeader, rightBody),
	)
	divider := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(" │ ")
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, divider, right)

	filter := "filter /  •  r refresh  •  enter select  •  esc cancel"
	if m.pickerFiltering {
		filter = "filter: " + m.pickerFilter + "▏"
	} else if m.pickerFilter != "" {
		filter = "filter: " + m.pickerFilter + "  •  enter select"
	}
	footer := hintStyle.Width(innerW).Render(filter)
	content := lipgloss.JoinVertical(lipgloss.Left,
		pickerHeaderFor(m.pickerKind, innerW),
		body,
		footer,
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Width(cardW).
		Render(content)
}

func pickerHeaderFor(kind string, width int) string {
	title := " SETTINGS  /  MODEL"
	if kind == pickerKindVariant {
		title = " SETTINGS  /  VARIANT"
	}
	return pickerHeaderTitle(width, title)
}

func pickerHeaderTitle(width int, title string) string {
	if lipgloss.Width(title)+1 > width {
		if strings.Contains(title, "VARIANT") {
			title = " SETTINGS / VARIANT"
		} else {
			title = " SETTINGS / MODEL"
		}
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Width(width).Render(
		title + strings.Repeat(" ", max(0, width-lipgloss.Width(title)-1)) + "X",
	)
}

func (m Model) pickerCloseRect() (x, y int, ok bool) {
	if !m.pickerMode {
		return 0, 0, false
	}
	card := m.pickerView()
	cardW, cardH := lipgloss.Width(card), lipgloss.Height(card)
	left := max(0, (m.width-cardW)/centerDiv)
	top := max(0, (m.height-cardH)/centerDiv)
	// The close marker sits one row below the top border and two columns
	// from the right border.
	return left + cardW - cardBorder, top + 1, true
}

func (m Model) pickerScreen() string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.pickerView())
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
		label := m.pickerItemLabel(id)
		line := "  " + label
		if i == m.pickerCursor {
			b.WriteString(sel.Render("▸ " + label))
			continue
		}
		b.WriteString(normal.Render(line))
	}
	return b.String()
}

func (m Model) resizePicker() Model {
	innerW := max(minPaneWidth, m.overlayWidth()-cardBorder)
	_, rightW := splitPaneWidths(innerW)
	vpW := max(pickerVpMinWidth, rightW-1)
	m.pickerVp.SetWidth(vpW)
	m.pickerVp.SetHeight(m.pickerVPHeight())
	return m
}

func (m Model) updatePickerKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	if key.Mod.Contains(tea.ModCtrl) && key.Code == 'c' {
		return m.closeDone(), tea.Quit
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
		if m.pickerBuilt && len(m.pickerItems) > 0 && m.pickerCursor < len(m.pickerItems) {
			if m.pickerKind == pickerKindVariant {
				m.variant = m.pickerItems[m.pickerCursor]
				m.syncSessionVariant()
				m.pickerMode = false
				return m, m.persistVariant()
			}
			m.model = m.pickerItems[m.pickerCursor]
			if !modelscache.HasVariant(m.modelInfos, m.model, m.variant) {
				m.variant = ""
			}
			m.syncSessionVariant()
			m.pickerMode = false
			return m, m.persistSelection()
		}
		return m, nil
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

func (m Model) closePicker() Model {
	m.pickerMode = false
	m.pickerFiltering = false
	m.pickerKind = pickerKindModel
	m.dragOn = false
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
		if needle == "" || strings.Contains(strings.ToLower(id), needle) ||
			(needle == "free" && modelscache.IsFree(modelscache.Info{ID: id})) {
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
