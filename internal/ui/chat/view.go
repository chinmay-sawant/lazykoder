package chat

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// View renders the picker card, slash menu, confirm view, or the chat layout.
func (m Model) View() tea.View {
	if m.quitConfirm {
		v := tea.NewView(m.quitScreen())
		v.AltScreen = true
		return v
	}
	if m.confirmMode {
		v := tea.NewView(m.confirm.View())
		v.AltScreen = true
		return v
	}
	if m.pickerMode {
		v := tea.NewView(m.pickerScreen())
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}
	var b strings.Builder
	b.WriteString(m.titleLine())
	b.WriteString("\n\n")
	b.WriteString(m.transcriptView())
	b.WriteString("\n")
	b.WriteString(m.statusLine())
	b.WriteString("\n")
	if m.slashMode {
		b.WriteString(m.slashView())
		b.WriteString("\n")
	}
	b.WriteString(m.promptLine())
	v := tea.NewView(b.String())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) quitScreen() string {
	card := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("3")).
		Padding(1, cardBorder).
		Render("Press Ctrl+C again to close lazyKoder\nEsc cancel")
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}

// promptLine renders the prompt inside a subtle translucent-looking panel:
// a dark background with bright text so the input stays clearly readable.
// A bottom margin lifts it one row above the bottom edge.
func (m Model) promptLine() string {
	return lipgloss.NewStyle().
		Background(lipgloss.Color("#262626")).
		Padding(0, 1).
		MarginBottom(1).
		Width(m.width).
		Render(m.prompt.View())
}

// transcriptRenderHeight returns the transcript height for rendering, shrinking
// it when the slash popover needs space above the prompt.
func (m Model) transcriptRenderHeight() int {
	fixedRows := titleBlockRows + lipgloss.Height(m.statusLine()) + lipgloss.Height(m.promptLine())
	if m.slashMode {
		fixedRows += 1 + lipgloss.Height(m.slashView())
	}
	return max(minPaneHeight, m.height-fixedRows)
}

// transcriptView renders the transcript viewport with a right-edge scrollbar.
func (m Model) transcriptView() string {
	atBottom := m.transcript.AtBottom()
	vp := m.transcript
	h := m.transcriptRenderHeight()
	vp.SetHeight(h)
	if atBottom {
		vp.GotoBottom()
	}
	width := vp.Width()
	return withScrollbar(vp.View(), width, h, vp.ScrollPercent(), vp.TotalLineCount() > h)
}

// titleLine renders the static white app title at the top of the chat view.
func (m Model) titleLine() string {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Render("lazykoder")
}

// scrollbarRect returns the screen rectangle (top row, bottom row, column)
// of a rendered scrollbar column for the given target (0 = transcript,
// 1 = picker). ok is false when no scrollbar is shown.
func (m Model) scrollbarRect(target int) (top, bottom, col int, ok bool) {
	if target == 0 {
		if m.pickerMode {
			return 0, 0, 0, false
		}
		h := m.transcriptRenderHeight()
		if m.transcript.TotalLineCount() <= h {
			return 0, 0, 0, false
		}
		return titleBlockRows, titleBlockRows + h, m.width - 1, true
	}
	vpH := m.pickerVPHeight()
	if len(m.pickerItems) <= vpH {
		return 0, 0, 0, false
	}
	card := m.pickerView()
	cardTop := max(0, (m.height-lipgloss.Height(card))/centerDiv)
	cardLeft := max(0, (m.width-lipgloss.Width(card))/centerDiv)
	innerW := max(minPaneWidth, m.overlayWidth()-cardBorder)
	leftW, _ := splitPaneWidths(innerW)
	listTop := cardTop + listInsetRows
	listCol := cardLeft + 1 + leftW + paneDivider + m.pickerVp.Width()
	return listTop, listTop + vpH, listCol, true
}

// withScrollbar appends a scrollbar column at the right edge of a rendered
// viewport when its content overflows. The thumb tracks the scroll percent.
func withScrollbar(v string, width, height int, percent float64, overflow bool) string {
	if !overflow {
		return v
	}
	lines := strings.Split(v, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	thumb := int(percent * float64(height-1))
	track := lipgloss.NewStyle().Faint(true).Render("░")
	thumbCell := lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Render("█")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		if w := width - lipgloss.Width(line); w > 0 {
			b.WriteString(line + strings.Repeat(" ", w))
		} else {
			b.WriteString(line)
		}
		if i == thumb {
			b.WriteString(thumbCell)
		} else {
			b.WriteString(track)
		}
	}
	return b.String()
}

func (m Model) overlayWidth() int {
	available := max(minPaneWidth, m.width-cardBorder)
	desired := max(minPaneWidth, m.width*cardWidthPct/percentBase)
	return min(available, desired)
}

func splitPaneWidths(total int) (left, right int) {
	left = max(minLeftPane, min(maxLeftPane, total/paneCount))
	right = total - left - paneDivider
	if right < minRightPane {
		right = minRightPane
		left = max(minLeftPane, total-right-paneDivider)
	}
	return left, right
}

func (m Model) statusLine() string {
	var line string
	switch {
	case m.err != "":
		line = m.wrapStatus(errStyle.Render(m.err))
	case m.busy:
		line = m.wrapStatus(busyStyle.Render(busyHint))
	default:
		label := m.model
		if label == "" && m.client != nil {
			label = m.client.Model()
		}
		if label == "" {
			label = "default"
		}
		var b strings.Builder
		b.WriteString(hintStyle.Render(strings.Join([]string{
			"model " + label,
			"click model to switch",
			"/ commands",
			"enter to send",
			"q to quit",
		}, "  •  ")))
		if _, ok := m.selectedHistoryItem(); ok {
			b.WriteString("\n")
			b.WriteString(hintStyle.Render("history: ↑/↓ previous/next  •  c copy  •  d delete"))
		}
		if m.transcript.TotalLineCount() > m.transcript.Height() {
			b.WriteString("\n")
			b.WriteString(hintStyle.Render("scroll: ↑/↓, page up/down or mouse wheel"))
		}
		if m.modelsErr != "" {
			b.WriteString("\n")
			b.WriteString(errStyle.Render("models: " + m.modelsErr))
		} else if len(m.models) > 0 {
			b.WriteString("\n")
			count := fmt.Sprintf("models: %d available", len(m.models))
			if m.modelsCached {
				count += " (cached)"
			}
			b.WriteString(hintStyle.Render(count))
		}
		line = m.wrapStatus(b.String())
	}
	if m.copyNotice == "" {
		return line
	}
	notice := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Bold(true).
		Width(m.width).
		Align(lipgloss.Right).
		Render(m.copyNotice)
	return line + "\n" + notice
}

func (m Model) modelStatusRect() (left, top, right, bottom int, ok bool) {
	if m.busy || m.err != "" || m.pickerMode || m.slashMode {
		return 0, 0, 0, 0, false
	}
	label := m.model
	if label == "" && m.client != nil {
		label = m.client.Model()
	}
	if label == "" {
		label = "default"
	}
	statusTop := titleBlockRows + m.transcriptRenderHeight()
	return 0, statusTop, min(m.width, lipgloss.Width("model "+label)), statusTop + 1, true
}

func (m Model) wrapStatus(status string) string {
	return lipgloss.NewStyle().Width(max(minPaneWidth, m.width)).Render(status)
}
