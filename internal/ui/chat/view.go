package chat

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

// View renders the picker card, slash menu, confirm overlay, or the chat layout.
func (m Model) View() tea.View {
	if m.quitConfirm {
		v := tea.NewView(m.quitScreen())
		v.AltScreen = true
		return v
	}
	if m.pickerMode {
		v := tea.NewView(m.pickerScreen())
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}
	if m.sessionPickerMode {
		v := tea.NewView(m.sessionPickerScreen())
		v.AltScreen = true
		v.MouseMode = tea.MouseModeCellMotion
		return v
	}
	screen := m.chatScreen()
	if m.confirmMode {
		screen = overlayOn(screen, m.width, m.height, m.confirmOverlay())
	}
	if m.askMode {
		screen = overlayOn(screen, m.width, m.height, m.askOverlay())
	}
	if m.helpMode {
		screen = overlayOn(screen, m.width, m.height, m.helpOverlay())
	}
	if m.filePickerMode {
		screen = overlayOn(screen, m.width, m.height, m.filePickerOverlay())
	}
	v := tea.NewView(screen)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) chatScreen() string {
	var b strings.Builder
	b.WriteString(m.headerView())
	b.WriteString("\n")
	b.WriteString(m.transcriptView())
	b.WriteString("\n")
	b.WriteString(m.statusLine())
	b.WriteString("\n")
	if m.slashMode {
		b.WriteString(m.slashView())
		b.WriteString("\n")
	}
	b.WriteString(m.promptLine())
	return b.String()
}

func overlayOn(base string, width, height int, card string) string {
	baseLines := strings.Split(lipgloss.NewStyle().Faint(true).Width(max(1, width)).Render(base), "\n")
	for len(baseLines) < height {
		baseLines = append(baseLines, "")
	}
	if height > 0 && len(baseLines) > height {
		baseLines = baseLines[:height]
	}
	cardLines := strings.Split(card, "\n")
	cardH := len(cardLines)
	cardW := 0
	for _, line := range cardLines {
		if w := lipgloss.Width(line); w > cardW {
			cardW = w
		}
	}
	top := max(0, (height-cardH)/centerDiv)
	left := max(0, (width-cardW)/centerDiv)
	for i, line := range cardLines {
		row := top + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		dst := padDisplay(baseLines[row], width)
		baseLines[row] = spliceDisplay(dst, line, left)
	}
	return strings.Join(baseLines, "\n")
}

func padDisplay(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func spliceDisplay(dst, src string, left int) string {
	if left <= 0 {
		return src
	}
	prefix := dst
	// Keep the left columns of dst; lipgloss cells are not split here
	// because overlay cards are placed on a padded row.
	runes := []rune(dst)
	if left < len(runes) {
		prefix = string(runes[:left])
	}
	return prefix + src
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
	p := m.prompt
	p.SetHeight(m.promptHeight())
	p.SetWidth(max(minPaneWidth, m.width-2))
	return lipgloss.NewStyle().
		Background(lipgloss.Color("#262626")).
		Padding(0, 1).
		MarginBottom(1).
		Width(m.width).
		Render(p.View())
}

func (m Model) transcriptRenderHeight() int {
	fixedRows := lipgloss.Height(m.headerView()) + 1 + lipgloss.Height(m.statusLine()) + lipgloss.Height(m.promptLine())
	if m.slashMode {
		fixedRows += 1 + lipgloss.Height(m.slashView())
	}
	return max(minPaneHeight, m.height-fixedRows)
}

// transcriptView renders the transcript viewport with a right-edge scrollbar.
func (m Model) transcriptView() string {
	h := m.transcriptRenderHeight()
	if len(m.items) == 0 {
		empty := strings.Join([]string{
			hintStyle.Render("lazykoder is a local OpenCode chat."),
			hintStyle.Render("try: list the files in this project"),
			hintStyle.Render("type / for commands"),
		}, "\n")
		return lipgloss.NewStyle().Width(max(minPaneWidth, m.width)).Height(h).Render(empty)
	}
	atBottom := m.transcript.AtBottom()
	vp := m.transcript
	vp.SetHeight(h)
	if atBottom {
		vp.GotoBottom()
	}
	width := vp.Width()
	return withScrollbar(vp.View(), width, h, vp.ScrollPercent(), vp.TotalLineCount() > h)
}

func (m Model) helpOverlay() string {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Render("keys"),
		"enter send  •  shift+enter newline",
		"/ commands  •  /sessions or ctrl+s",
		"@ mention a project file",
		"click model to switch",
		"t thinking  •  e expand last tool",
		"esc cancel turn  •  ctrl+c quit",
		"scroll ↑/↓ page mouse",
		hintStyle.Render("esc or ? close"),
	}
	innerW := min(56, max(minPaneWidth, m.width-8))
	body := hintStyle.Width(innerW).Render(strings.Join(lines, "\n"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorBorder()).
		Padding(1, 2).
		Width(innerW + 6).
		Render(body)
}

func (m Model) headerView() string {
	brand := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Render("lazykoder")
	title := m.sessionTitle()
	model := m.modelLabel()
	cwd := filepath.Base(m.workdir)
	if cwd == "" || cwd == "." {
		cwd = m.workdir
	}
	sep := hintStyle.Render("  ·  ")
	w := max(minPaneWidth, m.width)
	right := hintStyle.Render(model + "  " + cwd)
	avail := w - lipgloss.Width(brand) - lipgloss.Width(sep) - lipgloss.Width(right) - lipgloss.Width(sep)
	if avail < 8 {
		row1 := truncateRunes(brand+"  "+title, w)
		row2 := truncateRunes(model+"  "+cwd, w)
		return row1 + "\n" + hintStyle.Render(row2)
	}
	title = truncateRunes(title, avail)
	return brand + sep + title + sep + right
}

func (m Model) confirmOverlay() string {
	inner := m.confirm.View()
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("1")).
		Padding(1, 2).
		Render(inner)
}

func (m Model) askOverlay() string {
	q := m.askQuestion
	header := q.Header
	if header == "" {
		header = "question"
	}
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(q.Question))
	b.WriteString("\n")
	b.WriteString(hintStyle.Render(header))
	b.WriteString("\n")
	for i, opt := range q.Options {
		prefix := "  "
		style := hintStyle
		if i == m.askCursor {
			prefix = "▸ "
			style = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
		}
		b.WriteString(style.Render(fmt.Sprintf("%s%d. %s", prefix, i+1, opt)))
		b.WriteString("\n")
	}
	b.WriteString(hintStyle.Render("j/k select  •  enter confirm  •  esc cancel"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("8")).
		Padding(1, 2).
		Render(strings.TrimRight(b.String(), "\n"))
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
		headerH := lipgloss.Height(m.headerView()) + 1
		return headerH, headerH + h, m.width - 1, true
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
		busy := "thinking"
		if tool := m.currentToolName(); tool != "" {
			busy = tool
		}
		line = m.wrapStatus(busyStyle.Render(busy))
	default:
		line = m.wrapStatus(hintStyle.Render(m.modelLabel()))
		if _, ok := m.selectedHistoryItem(); ok {
			line += "\n" + hintStyle.Render("history: ↑/↓ previous/next  •  c copy  •  d delete")
		}
		if m.transcript.TotalLineCount() > m.transcript.Height() {
			line += "\n" + hintStyle.Render("scroll: ↑/↓, page up/down or mouse wheel")
		}
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
	label := m.modelLabel()
	statusTop := lipgloss.Height(m.headerView()) + 1 + m.transcriptRenderHeight()
	return 0, statusTop, min(m.width, lipgloss.Width(label)), statusTop + 1, true
}

func (m Model) wrapStatus(status string) string {
	return lipgloss.NewStyle().Width(max(minPaneWidth, m.width)).Render(status)
}
