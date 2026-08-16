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
	"github.com/chinmay-sawant/lazykoder/internal/tips"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

// View renders the picker card, slash menu, confirm overlay, or the chat layout.
func (m Model) View() tea.View {
	if m.sessionPickerMode {
		return m.newView(m.sessionPickerScreen())
	}
	if m.settingsMode {
		return m.newView(m.settingsScreen())
	}
	screen := m.chatScreen()
	overlayH := m.composerTop()
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
	b.WriteString(m.alertRow())
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
	b.WriteString(m.composerBlock())
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

// promptLine renders the prompt inside a subtle translucent-looking panel:
// a dark background with bright text so the input stays clearly readable.
// A bottom margin lifts it one row above the bottom edge.
func (m Model) showLiveStatus() bool {
	return m.busy || strings.TrimSpace(m.activity) != ""
}

func (m Model) liveStatusView() string {
	label := strings.TrimSpace(m.activity)
	if label == "" {
		label = thinkingLabel
	}
	line := m.workRailMark() + " " + hintStyle.Render(label)
	return lipgloss.NewStyle().Width(max(minPaneWidth, m.width)).Render(line)
}

// jumpBarVisible reports whether the transcript is scrolled up so the
// jump-to-latest icon should appear on the alert row above the input box.
// Scrolling up keeps the view pinned until the user clicks the icon, so new
// output never yanks the view to the bottom.
func (m Model) jumpBarVisible() bool {
	if m.pickerMode || m.slashMode || m.confirmMode || m.askMode || m.helpMode || m.settingsMode || m.filePickerMode || m.sessionPickerMode {
		return false
	}
	return !m.transcript.AtBottom()
}

// alertRow is the transparent row above the input box: the jump-to-latest
// icon sits centered, and transient alerts (copy confirmations, the quit
// warning) appear right-aligned.
func (m Model) alertRow() string {
	w := max(minPaneWidth, m.width)
	row := strings.Repeat(" ", w)
	if m.jumpBarVisible() {
		row = spliceDisplay(row, lipgloss.NewStyle().Faint(true).Render(jumpDownArrow), w/2)
	}
	if alert := m.alertText(); alert != "" {
		row = spliceDisplay(row, alert, max(0, w-lipgloss.Width(alert)))
	}
	return row
}

// alertText is the transient message shown right-aligned on the alert row:
// the red quit warning wins, then the green copy confirmation, then the
// rotating idle tip.
func (m Model) alertText() string {
	switch {
	case m.quitConfirm:
		return lipgloss.NewStyle().Foreground(theme.ColorDanger()).Bold(true).Render("ctrl+c again to quit")
	case m.copyNotice != "":
		return lipgloss.NewStyle().Foreground(theme.ColorGood()).Bold(true).Render(m.copyNotice)
	case m.tipsVisible():
		return lipgloss.NewStyle().Bold(true).Foreground(theme.ColorMute()).Render(tipLabel) + " " + hintStyle.Render(tips.At(m.tipsIndex))
	}
	return ""
}

// tipsVisible reports whether the rotating usage tip should show on the
// alert row: only while the user is doing nothing (idle, no overlay).
func (m Model) tipsVisible() bool {
	if m.busy || m.quitConfirm || m.copyNotice != "" {
		return false
	}
	return m.promptEditing()
}

// composerTop is the first screen row of the input box panel: everything
// above it is free for overlays.
func (m Model) composerTop() int {
	top := m.transcriptTop() + m.transcriptRenderHeight() + 1 // alert row
	if m.slashMode {
		top += 1 + lipgloss.Height(m.slashView())
	}
	if m.pickerMode {
		top += 1 + lipgloss.Height(m.pickerView())
	}
	if m.err != "" {
		top += 1 + lipgloss.Height(errStyle.Width(max(minPaneWidth, m.width)).Render(m.err))
	}
	return top
}

func (m Model) composerBlock() string {
	if m.showLiveStatus() {
		return m.liveStatusView() + "\n\n" + m.promptLine()
	}
	return m.promptLine()
}

func (m Model) promptLine() string {
	innerW := max(minPaneWidth, m.width-2)
	p := m.prompt
	h := m.promptHeight()
	p.SetHeight(h)
	p.SetWidth(max(minPaneWidth, innerW-2))
	if m.promptSelectAll {
		st := p.Styles()
		st.Focused.Text = selectionStyle
		st.Focused.CursorLine = selectionStyle
		p.SetStyles(st)
	}
	text := p.View()
	if m.visualPromptLines() > h {
		text = withScrollbar(text, p.Width(), h, p.ScrollPercent(), true)
	}
	body := lipgloss.JoinVertical(lipgloss.Left, text, m.composerFooter(innerW-2))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorBorder()).
		Background(theme.ColorSurface()).
		Width(max(minPaneWidth, m.width)).
		Render(body)
}

func (m Model) composerFooter(width int) string {
	left := m.composerLeft()
	right := m.fitFooterRight(max(4, width-lipgloss.Width(left)-1))
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + hintStyle.Render(right)
}

func (m Model) modelContextLabel() string {
	return joinFooter(m.modelDisplayLabel(), m.footerStats())
}

func (m Model) footerStats() string {
	return strings.Join(m.footerStatParts(), "  ")
}

func (m Model) footerPieces() (tokens, cache, cost, tps string) {
	window := modelscache.ContextOf(m.modelInfos, m.modelLabel())
	switch {
	case m.tokensUsed > 0 && window > 0:
		tokens = formatTokens(m.tokensUsed) + "/" + formatTokens(int64(window))
	case m.tokensUsed > 0:
		tokens = formatTokens(m.tokensUsed)
	case window > 0:
		tokens = "0/" + formatTokens(int64(window))
	}
	if m.cacheHit > 0 || m.cacheMiss > 0 {
		cache = formatCache(m.cacheHit, m.cacheMiss)
	}
	if m.tokensUsed > 0 || m.cacheHit > 0 || m.cacheMiss > 0 || m.sessionCost > 0 {
		cost = formatCost(m.sessionCost)
	}
	if n := m.displayTPS(); n > 0 {
		tps = formatTPS(n)
	}
	return tokens, cache, cost, tps
}

func (m Model) footerStatParts() []string {
	tokens, cache, cost, tps := m.footerPieces()
	parts := make([]string, 0, 4)
	for _, p := range []string{tokens, cache, cost, tps} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func (m Model) fitFooterRight(budget int) string {
	model := m.modelDisplayLabel()
	tokens, cache, cost, tps := m.footerPieces()
	try := func(includeModel bool, bits ...string) string {
		parts := make([]string, 0, len(bits))
		for _, b := range bits {
			if b != "" {
				parts = append(parts, b)
			}
		}
		stats := strings.Join(parts, "  ")
		if includeModel {
			right := joinFooter(model, stats)
			if lipgloss.Width(right) <= budget {
				return right
			}
			if stats != "" {
				modelBudget := budget - lipgloss.Width(stats) - 2
				if modelBudget >= 4 {
					return truncateRunes(model, modelBudget) + "  " + stats
				}
			}
			return ""
		}
		if stats != "" && lipgloss.Width(stats) <= budget {
			return stats
		}
		return ""
	}
	if s := try(true, tokens, cache, cost, tps); s != "" {
		return s
	}
	if s := try(true, tokens, cache, cost); s != "" {
		return s
	}
	if s := try(true, tokens, cache); s != "" {
		return s
	}
	if s := try(true, cache); s != "" {
		return s
	}
	if s := try(false, tokens, cache, cost, tps); s != "" {
		return s
	}
	if s := try(false, cache); s != "" {
		return s
	}
	if cache != "" {
		return truncateRunes(cache, budget)
	}
	return truncateRunes(model, budget)
}

func joinFooter(model, stats string) string {
	switch {
	case model != "" && stats != "":
		return model + "  " + stats
	case stats != "":
		return stats
	default:
		return model
	}
}

func formatCache(hit, miss int64) string {
	hitPct, missPct := cachePercents(hit, miss)
	return "hit " + formatTokens(hit) + " " + formatPercent(hitPct) +
		"  miss " + formatTokens(miss) + " " + formatPercent(missPct)
}

func cacheHitPercent(hit, miss int64) int {
	hitPct, _ := cachePercents(hit, miss)
	return hitPct
}

func cachePercents(hit, miss int64) (hitPct, missPct int) {
	total := hit + miss
	if total <= 0 {
		return 0, 0
	}
	if miss <= 0 {
		return 100, 0
	}
	if hit <= 0 {
		return 0, 100
	}
	hitPct = int((hit*100 + total/2) / total)
	if hitPct > 100 {
		hitPct = 100
	}
	return hitPct, 100 - hitPct
}

func formatPercent(n int) string {
	if n < 0 {
		n = 0
	}
	if n > 100 {
		n = 100
	}
	return fmt.Sprintf("%d%%", n)
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
	fixedRows := lipgloss.Height(m.headerView()) + 1 + 1 + 1 + lipgloss.Height(m.composerBlock())
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
	view := vp.View()
	if m.selection.active && m.selection.hasRange() {
		view = m.highlightTranscriptSelection(view, vp.YOffset())
	}
	return withScrollbar(view, vp.Width(), h, vp.ScrollPercent(), vp.TotalLineCount() > h)
}

func (m Model) helpOverlay() string {
	rows := [][2]string{
		{"enter", "send"},
		{"shift+enter", "newline"},
		{"/", "commands"},
		{"ctrl+s", "resume"},
		{"/model", "switch model"},
		{"/variant", "reasoning effort"},
		{"@", "mention a file"},
		{"click model", "switch"},
		{"t / e", "thinking / expand tool"},
		{"esc", "cancel turn"},
		{"ctrl+a", "select all"},
		{"ctrl+←/→", "jump words"},
		{"ctrl+home/end", "input start/end"},
		{"ctrl+c", "copy / quit"},
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
	if len(lines) > height {
		lines = lines[:height]
	}
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
	if m.busy || m.pickerMode || m.sessionPickerMode || m.settingsMode {
		return 0, 0, 0, 0, false
	}
	leftW := lipgloss.Width(m.composerLeft())
	label := m.fitFooterRight(max(4, max(minPaneWidth, m.width)-2-leftW-1))
	top = lipgloss.Height(m.headerView()) + 1 + m.transcriptRenderHeight() + 1 + 1
	if m.slashMode {
		top += 1 + lipgloss.Height(m.slashView())
	}
	if m.err != "" {
		top += lipgloss.Height(errStyle.Width(max(minPaneWidth, m.width)).Render(m.err)) + 1
	}
	if m.showLiveStatus() {
		top += 2
	}
	// Footer sits inside the composer: top border + prompt rows.
	top += 1 + m.promptHeight()
	right = m.width - 1
	left = max(0, right-lipgloss.Width(label))
	return left, top, right, top + 1, true
}
