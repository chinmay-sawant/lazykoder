package chat

import (
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

// View renders the picker card, slash menu, confirm overlay, or the chat layout.
func (m Model) View() tea.View {
	if m.quitConfirm {
		return m.newView(m.quitScreen())
	}
	if m.sessionPickerMode {
		return m.newView(m.sessionPickerScreen())
	}
	screen := m.chatScreen()
	overlayH := m.height
	if ph := lipgloss.Height(m.promptLine()); ph > 0 && overlayH > ph {
		overlayH -= ph
	}
	if m.confirmMode {
		screen = overlayOn(screen, m.width, overlayH, m.confirmOverlay())
	}
	if m.askMode {
		screen = overlayOn(screen, m.width, overlayH, m.askOverlay())
	}
	if m.helpMode {
		screen = overlayOn(screen, m.width, overlayH, m.helpOverlay())
	}
	if m.filePickerMode {
		screen = overlayOn(screen, m.width, overlayH, m.filePickerOverlay())
	}
	return m.newView(screen)
}

// newView paints a full-size solid black layer so the host terminal
// background never shows through empty cells.
func (m Model) newView(content string) tea.View {
	painted := lipgloss.NewStyle().
		Background(theme.ColorBg()).
		Width(max(1, m.width)).
		Height(max(1, m.height)).
		Render(content)
	v := tea.NewView(painted)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.BackgroundColor = theme.ColorBg()
	return v
}

func (m Model) chatScreen() string {
	var b strings.Builder
	b.WriteString(m.headerView())
	b.WriteString("\n")
	b.WriteString(m.transcriptView())
	b.WriteString("\n")
	if m.slashMode {
		b.WriteString(m.slashView())
		b.WriteString("\n")
	}
	if m.pickerMode {
		b.WriteString(m.pickerView())
		b.WriteString("\n")
	}
	if m.err != "" {
		b.WriteString(errStyle.Width(max(minPaneWidth, m.width)).Render(m.err))
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
	cardLines := strings.Split(card, "\n")
	cardH := len(cardLines)
	cardW := 0
	for _, line := range cardLines {
		if w := lipgloss.Width(line); w > cardW {
			cardW = w
		}
	}
	regionH := height
	if regionH <= 0 || regionH > len(baseLines) {
		regionH = len(baseLines)
	}
	top := max(0, (regionH-cardH)/centerDiv)
	if top+cardH > regionH {
		top = max(0, regionH-cardH)
	}
	left := max(0, (width-cardW)/centerDiv)
	for i, line := range cardLines {
		row := top + i
		if row < 0 || row >= len(baseLines) {
			continue
		}
		dst := padDisplay(baseLines[row], width)
		baseLines[row] = spliceDisplay(dst, padDisplay(line, cardW), left)
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
	if left < 0 {
		left = 0
	}
	srcW := lipgloss.Width(src)
	dst = padDisplay(dst, left+srcW)
	prefix := ansi.Cut(dst, 0, left)
	if w := lipgloss.Width(prefix); w < left {
		prefix += strings.Repeat(" ", left-w)
	}
	end := left + srcW
	dstW := lipgloss.Width(dst)
	suffix := ""
	if end < dstW {
		suffix = ansi.Cut(dst, end, dstW)
	}
	return prefix + src + suffix
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
	innerW := max(minPaneWidth, m.width-2)
	p := m.prompt
	p.SetHeight(m.promptHeight())
	p.SetWidth(max(minPaneWidth, innerW-2))
	body := lipgloss.JoinVertical(lipgloss.Left, p.View(), m.composerFooter(innerW-2))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorBorder()).
		Background(theme.ColorSurface()).
		Width(max(minPaneWidth, m.width)).
		Render(body)
}

func (m Model) composerFooter(width int) string {
	left := m.composerLeft()
	right := m.modelContextLabel()
	if m.copyNotice != "" {
		left = lipgloss.NewStyle().Foreground(theme.ColorGood()).Bold(true).Render(m.copyNotice)
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
		right = truncateRunes(right, max(4, width-lipgloss.Width(left)-1))
	}
	return left + strings.Repeat(" ", gap) + hintStyle.Render(right)
}

func (m Model) modelContextLabel() string {
	label := m.modelDisplayLabel()
	window := modelscache.ContextOf(m.modelInfos, m.modelLabel())
	if m.tokensUsed > 0 && window > 0 {
		label = label + "  " + formatTokens(m.tokensUsed) + "/" + formatTokens(int64(window))
	} else if m.tokensUsed > 0 {
		label = label + "  " + formatTokens(m.tokensUsed)
	} else if window > 0 {
		label = label + "  0/" + formatTokens(int64(window))
	}
	if m.cacheHit > 0 || m.cacheMiss > 0 {
		label = label + "  " + formatCache(m.cacheHit, m.cacheMiss)
	}
	if m.tokensUsed > 0 || m.cacheHit > 0 || m.cacheMiss > 0 || m.sessionCost > 0 {
		label = label + "  " + formatCost(m.sessionCost)
	}
	if m.tokensPerSec > 0 {
		label = label + "  " + formatTPS(m.tokensPerSec)
	}
	return label
}

func formatCache(hit, miss int64) string {
	return "hit " + formatTokens(hit) + "  miss " + formatTokens(miss)
}

func formatCost(usd float64) string {
	if usd <= 0 {
		return "$0.00"
	}
	if usd < 0.01 {
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.3f", usd)
}

func formatTPS(n float64) string {
	if n >= 10 {
		return fmt.Sprintf("%.0f tps", n)
	}
	return fmt.Sprintf("%.1f tps", n)
}

func (m Model) composerLeft() string {
	switch {
	case m.err != "":
		return errStyle.Render("error")
	default:
		if _, ok := m.selectedHistoryItem(); ok {
			return hintStyle.Render("history: ↑/↓ previous/next")
		}
		return hintStyle.Render("enter send")
	}
}

func (m Model) transcriptRenderHeight() int {
	fixedRows := lipgloss.Height(m.headerView()) + 1 + lipgloss.Height(m.promptLine())
	if m.slashMode {
		fixedRows += 1 + lipgloss.Height(m.slashView())
	}
	if m.pickerMode {
		fixedRows += 1 + lipgloss.Height(m.pickerView())
	}
	if m.err != "" {
		fixedRows += lipgloss.Height(errStyle.Width(max(minPaneWidth, m.width)).Render(m.err))
	}
	return max(minPaneHeight, m.height-fixedRows)
}

// paintedTranscript is the viewport the user sees: current width/height and
// pinned to the bottom when the model thinks it is at the bottom. Click
// mapping must use this offset, not the stale model YOffset from the
// default 80x24 size used at replay.
func (m Model) paintedTranscript() viewport.Model {
	h := m.transcriptRenderHeight()
	atBottom := m.transcript.AtBottom()
	vp := m.transcript
	vp.SetWidth(max(minPaneWidth, m.width-1))
	vp.SetHeight(h)
	if atBottom {
		vp.GotoBottom()
	}
	return vp
}

// transcriptView renders the transcript viewport with a right-edge scrollbar.
func (m Model) transcriptView() string {
	h := m.transcriptRenderHeight()
	if len(m.items) == 0 {
		empty := strings.Join([]string{
			lipgloss.NewStyle().Foreground(theme.ColorText()).Bold(true).Render("new run"),
			hintStyle.Render("ask anything about this project"),
			hintStyle.Render("/ commands   @ files   ctrl+s history"),
		}, "\n")
		return lipgloss.NewStyle().Width(max(minPaneWidth, m.width)).Height(h).Render(empty)
	}
	vp := m.paintedTranscript()
	return withScrollbar(vp.View(), vp.Width(), h, vp.ScrollPercent(), vp.TotalLineCount() > h)
}

func (m Model) helpOverlay() string {
	rows := [][2]string{
		{"enter", "send"},
		{"shift+enter", "newline"},
		{"/", "commands"},
		{"ctrl+s", "sessions"},
		{"/model", "switch model"},
		{"/variant", "reasoning effort"},
		{"@", "mention a file"},
		{"click model", "switch"},
		{"t", "thinking"},
		{"e", "expand last tool"},
		{"esc", "cancel turn"},
		{"ctrl+c", "quit"},
		{"↑/↓  page", "scroll"},
	}
	keyW := 0
	for _, row := range rows {
		if w := lipgloss.Width(row[0]); w > keyW {
			keyW = w
		}
	}
	innerW := min(56, max(minPaneWidth, m.width-12))
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Width(innerW).Render("keys")
	var body strings.Builder
	body.WriteString(title)
	for _, row := range rows {
		gap := max(2, keyW-lipgloss.Width(row[0])+2)
		line := row[0] + strings.Repeat(" ", gap) + row[1]
		if lipgloss.Width(line) > innerW {
			line = truncateRunes(line, innerW)
		}
		body.WriteString("\n")
		body.WriteString(hintStyle.Width(innerW).Render(line))
	}
	body.WriteString("\n")
	closeGap := max(2, keyW-lipgloss.Width("esc or ?")+2)
	body.WriteString(hintStyle.Width(innerW).Render("esc or ?" + strings.Repeat(" ", closeGap) + "close"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorBorder()).
		Background(theme.ColorBg()).
		Padding(0, 2).
		Render(body.String())
}

func (m Model) headerView() string {
	brand := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorAccent()).Render("lazykoder")
	title := m.sessionTitle()
	cwd := filepath.Base(m.workdir)
	if cwd == "" || cwd == "." {
		cwd = m.workdir
	}
	sep := hintStyle.Render("  ·  ")
	w := max(minPaneWidth, m.width)
	right := hintStyle.Render(cwd)
	avail := w - lipgloss.Width(brand) - lipgloss.Width(sep) - lipgloss.Width(right) - lipgloss.Width(sep)
	if avail < 8 {
		row1 := truncateRunes(brand+"  "+title, w)
		row2 := truncateRunes(cwd, w)
		return row1 + "\n" + hintStyle.Render(row2)
	}
	title = truncateRunes(title, avail)
	return brand + sep + lipgloss.NewStyle().Foreground(theme.ColorText()).Render(title) + sep + right
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
		headerH := m.transcriptTop()
		return headerH, headerH + h, m.width - 1, true
	}
	vpH := m.pickerVPHeight()
	if len(m.pickerItems) <= vpH {
		return 0, 0, 0, false
	}
	listTop := m.pickerDrawerTop() + 1
	listCol := min(m.width-1, m.pickerDrawerWidth())
	return listTop, listTop + vpH, listCol, true
}

func (m Model) pickerDrawerTop() int {
	top := m.transcriptTop() + m.transcriptRenderHeight() + 1
	if m.slashMode {
		top += 1 + lipgloss.Height(m.slashView())
	}
	return top
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

func (m Model) modelStatusRect() (left, top, right, bottom int, ok bool) {
	if m.busy || m.pickerMode || m.sessionPickerMode {
		return 0, 0, 0, 0, false
	}
	label := m.modelContextLabel()
	top = lipgloss.Height(m.headerView()) + 1 + m.transcriptRenderHeight() + 1
	if m.slashMode {
		top += 1 + lipgloss.Height(m.slashView())
	}
	if m.err != "" {
		top += lipgloss.Height(errStyle.Width(max(minPaneWidth, m.width)).Render(m.err)) + 1
	}
	// Footer sits inside the composer: top border + prompt rows.
	top += 1 + m.promptHeight()
	right = m.width - 1
	left = max(0, right-lipgloss.Width(label))
	return left, top, right, top + 1, true
}
