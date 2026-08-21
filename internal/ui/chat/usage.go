package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/provider/opencode"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

// usageTimeout bounds the /usage API call.
const usageTimeout = 10 * time.Second

// Usage bar layout quantities.
const (
	usageBarMinW       = 2
	usagePctBase       = 100
	usageLabelW        = 8
	usagePaddingAndPct = 7 // width not counted by label labelW (gap, % text)
)

// usageMsg carries the result of a GET /usage fetch.
type usageMsg struct {
	usage opencode.BillingUsage
	err   error
}

// usageBars renders a proportional bar for a usage window.
func usageBar(percent int, width int, limited bool) string {
	if width < usageBarMinW {
		width = usageBarMinW
	}
	clamped := percent
	if clamped < 0 {
		clamped = 0
	}
	if clamped > usagePctBase {
		clamped = usagePctBase
	}
	fill := clamped * width / usagePctBase
	bar := strings.Repeat("█", fill) + strings.Repeat("░", width-fill)
	style := lipgloss.NewStyle().Foreground(theme.ColorAccent())
	if limited {
		style = lipgloss.NewStyle().Foreground(theme.ColorDanger())
	}
	return style.Render(bar)
}

// openUsageModal opens the usage card. The caller decides whether to fetch:
// /usage always refreshes; settings only fetches when not yet loaded.
func (m Model) openUsageModal() Model {
	m = m.setFocus(focusUsage)
	m.prompt.SetValue("")
	m.promptUndo = nil
	return m
}

// closeUsageModal closes the usage card.
func (m Model) closeUsageModal() Model {
	return m.clearFocus(focusUsage)
}

// fetchUsage returns a tea.Cmd that loads plan usage from the API.
func (m Model) fetchUsage() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return usageMsg{err: fmt.Errorf("opencode client is not configured")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), usageTimeout)
		defer cancel()
		u, err := m.client.Usage(ctx)
		return usageMsg{usage: u, err: err}
	}
}

// maybeFetchUsage returns the usage fetch only when the card has not loaded
// usage yet. Settings can reopen repeatedly without re-hitting the API.
func (m Model) maybeFetchUsage() tea.Cmd {
	if m.usageLoaded || m.usageLoading {
		return nil
	}
	return m.fetchUsage()
}

// updateUsageKey handles keys while the usage card is open.
func (m Model) updateUsageKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	if key.Mod.Contains(tea.ModCtrl) && key.Code == 'c' {
		return m.closeDone(), tea.Quit
	}
	if key.Code == tea.KeyEscape || key.Code == 'q' || key.Code == 'Q' || key.Code == 'x' || key.Code == 'X' {
		return m.closeUsageModal(), nil
	}
	if key.Code == 'r' || key.Code == 'R' {
		m.usageLoading = true
		return m, m.fetchUsage()
	}
	return m, nil
}

// usageScreen is the full terminal frame with the usage card centered.
func (m Model) usageScreen() string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.usageCardView())
}

// usageCardView renders the bordered usage card (same family as settings/help).
func (m Model) usageCardView() string {
	cardW := m.overlayWidth()
	innerW := max(minPaneWidth, cardW-cardBorder-2*settingsCardHorzPad)

	title := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("USAGE")
	closeBtn := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("[x]")
	gap := max(1, innerW-lipgloss.Width(title)-lipgloss.Width(closeBtn))
	header := title + strings.Repeat(" ", gap) + closeBtn

	var body strings.Builder
	switch {
	case m.usageLoading && !m.usageLoaded:
		body.WriteString(hintStyle.Render("loading usage..."))
	case m.usageErr != "" && !m.usageLoaded:
		body.WriteString(errStyle.Render(m.usageErr))
	default:
		body.WriteString(m.usageBody(innerW))
	}

	foot := "r refresh  •  esc close"
	footer := hintStyle.Width(innerW).Render(truncateRunes(foot, innerW))

	content := lipgloss.JoinVertical(lipgloss.Left, header, "", body.String(), "", footer)
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

// usageBody renders the three usage windows with bars, percents, and reset
// times. It is also reused by the settings card.
func (m Model) usageBody(innerW int) string {
	labelW := usageLabelW
	barW := max(usageLabelW, innerW-labelW-usagePaddingAndPct)
	var sb strings.Builder
	sb.WriteString(m.usageWindowLine("rolling", m.usage.Rolling, barW))
	sb.WriteString("\n")
	sb.WriteString(m.usageWindowLine("weekly", m.usage.Weekly, barW))
	sb.WriteString("\n")
	sb.WriteString(m.usageWindowLine("monthly", m.usage.Monthly, barW))
	return strings.TrimSuffix(sb.String(), "\n")
}

func (m Model) usageWindowLine(label string, w opencode.BillingWindow, barW int) string {
	labelSt := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText())
	line := labelSt.Render(fmt.Sprintf("%-8s", label)) + " " + usageBar(w.Percent, barW, w.RateLimited) + fmt.Sprintf(" %d%%", w.Percent)
	if w.RateLimited {
		line += " " + errStyle.Render("limited")
	}
	return line
}

// usageCloseRect is the [x] close control rect on the usage card header.
func (m Model) usageCloseRect() (x0, y, x1 int, ok bool) {
	if !m.usageMode {
		return 0, 0, 0, false
	}
	for i, line := range strings.Split(m.usageScreen(), "\n") {
		plain := ansi.Strip(line)
		if !strings.Contains(plain, "USAGE") || !strings.Contains(plain, "[x]") {
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
