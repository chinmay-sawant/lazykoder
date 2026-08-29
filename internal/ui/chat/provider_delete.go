package chat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/provider"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

func (m Model) requestProviderDelete() (Model, tea.Cmd) {
	if !m.pickerProviderDeletable() {
		return m, nil
	}
	id := m.pickerItems[m.pickerCursor]
	m.providerDeleteTarget = id
	m = m.setFocus(focusProviderDelete)
	return m, nil
}

func (m Model) confirmProviderDelete() (Model, tea.Cmd) {
	target := strings.TrimSpace(m.providerDeleteTarget)
	m = m.clearFocus(focusProviderDelete)
	if target == "" || target == "__add_new_provider__" {
		return m, nil
	}
	path := filepath.Join(m.workdir, ".lazykoder", "providers.json")
	data, err := os.ReadFile(path)
	if err != nil {
		m.err = "delete failed: " + err.Error()
		return m, nil
	}
	var list []map[string]any
	if err := json.Unmarshal(data, &list); err != nil {
		m.err = "delete failed: providers.json invalid"
		return m, nil
	}
	found := -1
	for i, item := range list {
		if v, ok := item["id"].(string); ok && strings.EqualFold(strings.TrimSpace(v), target) {
			found = i
			break
		}
	}
	if found < 0 {
		m.err = fmt.Sprintf("cannot delete %q: not in providers.json (built-in fallback)", target)
		return m, nil
	}
	// Remove entry
	list = append(list[:found], list[found+1:]...)
	out, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		m.err = "delete failed: " + err.Error()
		return m, nil
	}
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0o600); err != nil {
		m.err = "delete failed: " + err.Error()
		return m, nil
	}
	// Reload registry so picker reflects file truth
	if _, err := provider.LoadProviders(m.workdir); err != nil {
		m.err = "deleted but reload failed: " + err.Error()
	}
	// Refresh auth statuses and picker items
	m.providerAuthStatus = make(map[string]provider.AuthStatus)
	for _, d := range provider.Descriptors() {
		m.providerAuthStatus[d.ID] = provider.InitialAuthStatus(d.ID)
	}
	// If deleted provider was active, fallback to opencode or first available
	active := m.projectSettings.EffectiveProvider()
	if strings.EqualFold(active, target) {
		ids := provider.IDs()
		if len(ids) > 0 {
			if d, ok := provider.DescriptorFor(ids[0]); ok {
				m = m.configureProvider(d)
				m = m.persistSettings()
			}
		}
	}
	m.applyFilter()
	if m.pickerCursor >= len(m.pickerItems) {
		m.pickerCursor = max(0, len(m.pickerItems)-1)
	}
	m.pickerVp.SetContent(m.pickerContent(m.pickerVp.Width()))
	m.copyNotice = "deleted provider: " + target
	return m, clearCopyNotice()
}

func (m Model) cancelProviderDelete() (Model, tea.Cmd) {
	return m.clearFocus(focusProviderDelete), nil
}

func (m Model) providerDeleteView() string {
	target := m.providerDeleteTarget
	if target == "" {
		target = "provider"
	}
	label := target
	if d, ok := provider.DescriptorFor(target); ok {
		label = d.Label + " (" + d.ID + ")"
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorDanger()).Render("Delete provider?")
	body := lipgloss.NewStyle().Foreground(theme.ColorText()).Render(fmt.Sprintf("Remove %s from .lazykoder/providers.json ?", label))
	subject := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render(target)
	hint := hintStyle.Render("y confirm  •  n cancel  •  esc cancel")
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", subject, body, "", hint)
	content = keepBackground(content, theme.ColorSurface())
	cardW := max(minPaneWidth, min(formOverlayMaxWidth, m.overlayWidth()))
	innerW := max(1, cardW-cardBorder-cardBorderPad)
	_ = innerW
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorDanger()).
		BorderBackground(theme.ColorSurface()).
		Background(theme.ColorSurface()).
		Padding(1, cardHorzPad).
		Width(cardW).
		Render(content)
}

func (m Model) providerDeleteCloseRect() (x0, y, x1 int, ok bool) {
	if !m.providerDeleteMode {
		return 0, 0, 0, false
	}
	card := m.providerDeleteView()
	cardW := lipgloss.Width(card)
	cardH := lipgloss.Height(card)
	left := max(0, (m.width-cardW)/centerDiv)
	top := max(0, (m.height-cardH)/centerDiv)
	// Find y of hint line containing "y confirm"
	for i, line := range strings.Split(m.providerDeleteView(), "\n") {
		if strings.Contains(ansi.Strip(line), "y confirm") {
			return left, top + i, left + cardW, true
		}
	}
	return left, top, left + cardW, true
}

func (m Model) providerPickerDeleteRect() (x0, y, x1 int, ok bool) {
	if !m.pickerMode || m.pickerKind != pickerKindProvider || !m.pickerProviderDeletable() {
		return 0, 0, 0, false
	}
	// Header line is first line of pickerView
	top := m.pickerDrawerTop()
	if top < 0 {
		return 0, 0, 0, false
	}
	// Find header line containing "[del]"
	for i, line := range strings.Split(m.pickerView(), "\n") {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "[del]") && strings.Contains(plain, "providers") {
			start, end, found := displaySpan(plain, "[del]")
			if !found {
				continue
			}
			// picker drawer is full-width, left=0, so screen x is start
			return start, top + i, end, true
		}
	}
	return 0, 0, 0, false
}

func (m Model) updateProviderDeleteKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	switch key.Code {
	case tea.KeyEscape:
		return m.cancelProviderDelete()
	}
	switch strings.ToLower(key.Text) {
	case "y":
		return m.confirmProviderDelete()
	case "n":
		return m.cancelProviderDelete()
	}
	return m, nil
}
