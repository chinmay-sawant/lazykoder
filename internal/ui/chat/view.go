package chat

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/tips"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

const composerFooterDetailGap = 2

// composerFocusGlow is the static border glow strength while the user is
// editing the draft: a dim step on the same border-to-accent ramp the pulse
// uses, so focus reads as "lit" without competing with the busy throb.
const composerFocusGlow = 0.3

// lazykoderLogo is shown when /new clears the transcript, so a fresh session
// gets the same LazyKoder identity as the quit screen.
const lazykoderLogo = "" +
	"  █    █▀▀█ ▀▀▀█ █  █ █ ▄▀ █▀▀█ █▀▀▄ █▀▀▀ █▀▀█\n" +
	"  █    █▄▄█  ▄▀   ▀▀█ █▀▄  █  █ █  █ █▀▀▀ █▄▄▀\n" +
	"  ▀▀▀▀ ▀  ▀ ▀▀▀▀  ▀▀▀ ▀  ▀ ▀▀▀▀ ▀▀▀▀ ▀▀▀▀ ▀  ▀"

const (
	minWholeTPS = 10
	tpsKilo     = 1000
	tpsTenKilo  = 10_000
)

// View layout quantities. Names follow each use site so semantically distinct
// values that happen to be equal are not collapsed into one const.
const (
	percent         = 100  // percentage scale (100%)
	tipsMinWidth    = 100  // min terminal width to show rotating tips
	twoColMinWidth  = 100  // min terminal width for the two-column keys layout
	minCentDisplay  = 0.01 // smallest USD to print with 4 decimals
	overlayMaxW     = 64   // max inner width of the keys overlay
	overlayColMin   = 28   // min column width in the two-column keys layout
	minAvailW       = 8    // min header space before dropping title onto row 2
	cardHorzPad     = 2    // horizontal card padding
	cardBorderPad   = 4    // two card-border columns on each side
	footerChipMin   = 4    // min budget left before truncating a footer chip
	footerBudgetMin = 4    // min budget for the footer row
	footerStatGap   = 2    // gap between a footer chip and its stats
	layoutHalf      = 2    // halving divisor
	twoColPad       = 2    // space reserved between the two key columns
	keyColPad       = 2    // gap around a key label in the keys overlay
)

// View renders the picker card, slash menu, confirm overlay, or the chat layout.
func (m Model) View() tea.View {
	theme.SetMode(m.projectSettings.EffectiveTheme())
	configureThemeStyles()
	m.prompt.SetStyles(promptStyles())
	m = m.ensureLayout()
	return m.newView(m.frame())
}

// frame is the unpainted screen string. Mouse hit-testing scans this after
// the same Width/Height paint as newView so click rows match the terminal.
func (m Model) frame() string {
	m = m.ensureLayout()
	// Full-screen cards keep the historical paint order (sessions before log
	// before settings...). Overlays stack on chat; key routing uses currentFocus.
	switch {
	case m.sessionPickerMode:
		return m.sessionPickerScreen()
	case m.subagentLogMode:
		if m.memoryHistoryDetailMode {
			return m.memoryHistoryDetailScreen()
		}
		if m.recapDetailMode {
			return m.recapDetailScreen()
		}
		return m.subagentLogScreen()
	case m.settingsMode:
		if m.layout.settingsPaint != "" {
			return m.layout.settingsPaint
		}
		return m.settingsScreen()
	case m.usageMode:
		return m.usageScreen()
	case m.providerDeleteMode:
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.providerDeleteView())
	case m.helpMode:
		return m.helpScreen()
	}
	screen := m.chatScreen()
	overlayH := m.layout.composerTop
	if overlayH == 0 {
		overlayH = m.composerTop()
	}
	if m.confirmMode {
		screen = overlayOn(screen, m.width, overlayH, m.confirmOverlay())
	}
	if m.askMode {
		screen = overlayOn(screen, m.width, overlayH, m.askOverlay())
	}
	if m.filePickerMode {
		screen = overlayOn(screen, m.width, overlayH, m.filePickerOverlay())
	}
	if m.formMode && m.formHost != nil {
		screen = overlayOn(screen, m.width, m.height, m.formOverlay())
	}
	return screen
}

// paintedLines is the alt-screen cell grid (ANSI stripped per line).
func (m Model) paintedLines() []string {
	painted := lipgloss.NewStyle().
		Background(theme.ColorBg()).
		Width(max(1, m.width)).
		Height(max(1, m.height)).
		Render(m.frame())
	return strings.Split(painted, "\n")
}

// newView paints a full-size application background so the host terminal
// profile cannot show through empty cells.
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
	// Request key disambiguation so shift+enter is distinct from enter
	// (Kitty keyboard protocol / Windows console). Harmless when unsupported.
	v.KeyboardEnhancements.ReportAlternateKeys = true
	return v
}

func (m Model) chatScreen() string {
	var top strings.Builder
	top.WriteString(m.headerView())
	top.WriteString("\n")
	// Tracker strip: checklist under brand/title, above the transcript.
	if panel := m.todoPanelView(); panel != "" {
		top.WriteString(panel)
		top.WriteString("\n")
	}
	top.WriteString(m.transcriptView())
	top.WriteString("\n")
	top.WriteString(m.alertRow())

	var bottom strings.Builder
	if m.slashMode {
		if m.width >= slashCompactMaxWidth {
			bottom.WriteString("\n")
		}
		bottom.WriteString(m.slashView())
	}
	if m.pickerMode {
		bottom.WriteString("\n")
		bottom.WriteString(m.pickerView())
	}
	// Sub-agent drawer sits above the prompt like the /model list.
	if m.subagentPickerMode && !m.subagentLogMode {
		bottom.WriteString("\n")
		bottom.WriteString(m.subagentDrawerView())
	}
	if m.statusMode {
		bottom.WriteString("\n")
		bottom.WriteString(m.statusDrawerView())
	}
	if m.err != "" {
		bottom.WriteString("\n")
		bottom.WriteString(errStyle.Width(max(minPaneWidth, m.width)).Render(m.err))
	}
	bottom.WriteString("\n")
	bottom.WriteString(m.composerBlock())

	// Pin the composer to the bottom of the terminal: pad between alert row
	// and the lower chrome (drawers / prompt) when content is short.
	topS := top.String()
	if m.hasUserNav() && len(m.items) > 0 {
		topS = m.applyUserNavRail(topS)
	}
	botS := bottom.String()
	topH := lipgloss.Height(topS)
	botH := lipgloss.Height(botS)
	pad := m.height - topH - botH
	if pad > 0 {
		return topS + strings.Repeat("\n", pad) + botS
	}
	if pad == 0 {
		return topS + botS
	}
	// Chrome grew past the terminal (for example a taller slash palette).
	// Keep the composer and drop extra transcript rows.
	lines := strings.Split(topS, "\n")
	keep := len(lines) + pad
	if keep < 1 {
		keep = 1
	}
	if keep > len(lines) {
		keep = len(lines)
	}
	return strings.Join(lines[:keep], "\n") + botS
}

func overlayOn(base string, width, height int, card string) string {
	dimBg := lipgloss.Color("#121212")
	if theme.CurrentMode() == theme.ModeLight {
		dimBg = lipgloss.Color("#d0d0d0")
	}
	baseLines := strings.Split(lipgloss.NewStyle().Background(dimBg).Foreground(theme.ColorMute()).Faint(true).Width(max(1, width)).Render(base), "\n")
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
	return m.busy || m.recallScanning || m.skillsScanning || m.memoryScanJobs > 0 || strings.TrimSpace(m.activity) != ""
}

func (m Model) liveStatusView() string {
	w := max(minPaneWidth, m.width)
	label := strings.TrimSpace(m.activity)
	mark := m.workRailMark()
	if m.recallScanning {
		label = "scanning memory patterns"
		mark = m.memoryScanMark()
	} else if m.skillsScanning {
		label = "scanning approved skills"
		mark = m.memoryScanMark()
	} else if !m.busy && m.memoryScanJobs > 0 {
		label = "updating memories.md"
		mark = m.memoryScanMark()
	}
	if label == "" {
		label = thinkingLabel
	}
	label = truncateRunes(label, max(1, w/layoutHalf))
	// Primary line: work rail + live activity so the user can see progress.
	status := mark + " " + busyStyle.Render("working") + "  " + hintStyle.Render(label)
	status = lipgloss.NewStyle().Width(w).MaxWidth(w).Render(ansi.Truncate(status, w, "…"))
	if !m.busy {
		return "\n" + status
	}
	// Busy action strip (Grok Build-style): cancel / send now / edit draft.
	draft := strings.TrimSpace(m.prompt.Value())
	actions := "esc cancel  •  type to edit draft"
	if draft != "" {
		actions = "enter send now  •  esc cancel  •  type to edit draft"
	}
	actionLine := hintStyle.Width(w).Render(truncateRunes(actions, w))
	return "\n" + status + "\n" + actionLine
}

func (m Model) memoryScanMark() string {
	frames := []rune(memoryScanFrames)
	if len(frames) == 0 {
		return "⌕"
	}
	frame := frames[m.pulse%len(frames)]
	style := lipgloss.NewStyle().Foreground(theme.PulseAccent(m.pulseT()))
	return style.Render(string(frame))
}

// jumpBarVisible reports whether the transcript is scrolled up so the
// jump-to-latest icon should appear on the alert row above the input box.
// Scrolling up keeps the view pinned until the user clicks the icon, so new
// output never yanks the view to the bottom.
func (m Model) jumpBarVisible() bool {
	// Keep the jump-to-latest affordance available while the sub-agent drawer
	// is open so a stuck mid-scroll background can still be recovered. Full-
	// screen overlays still hide it.
	if m.pickerMode || m.slashMode || m.confirmMode || m.askMode || m.helpMode || m.usageMode || m.settingsMode || m.filePickerMode || m.sessionPickerMode {
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
		row = spliceDisplay(row, lipgloss.NewStyle().Faint(true).Render(jumpDownArrow), w/layoutHalf)
	}
	if alert := m.alertText(); alert != "" {
		row = spliceDisplay(row, alert, max(0, w-lipgloss.Width(alert)))
	}
	return row
}

// alertText is the transient message shown right-aligned on the alert row:
// the red quit warning wins, then the green copy confirmation, then the
// AGENTS.md loaded notice, then the rotating idle tip.
func (m Model) alertText() string {
	switch {
	case m.quitConfirm:
		return lipgloss.NewStyle().Foreground(theme.ColorDanger()).Bold(true).Render("ctrl+c again to quit")
	case m.copyNotice != "":
		return lipgloss.NewStyle().Foreground(theme.ColorGood()).Bold(true).Render(m.copyNotice)
	case m.projectInstructionsNotice != "":
		return lipgloss.NewStyle().Foreground(theme.ColorGood()).Render(m.projectInstructionsNotice)
	case m.tipsVisible():
		return lipgloss.NewStyle().Bold(true).Foreground(theme.ColorMute()).Render(tipLabel) + " " + hintStyle.Render(tips.At(m.tipsIndex))
	}
	return ""
}

// tipsVisible reports whether the rotating usage tip should show.
// Compact terminals keep tips out of the transcript gutter (they collide).
func (m Model) tipsVisible() bool {
	if m.busy || m.quitConfirm || m.copyNotice != "" || m.projectInstructionsNotice != "" {
		return false
	}
	if m.width < tipsMinWidth {
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
	if m.subagentPickerMode && !m.subagentLogMode {
		top += 1 + lipgloss.Height(m.subagentDrawerView())
	}
	if m.statusMode {
		top += 1 + lipgloss.Height(m.statusDrawerView())
	}
	if m.err != "" {
		top += 1 + lipgloss.Height(errStyle.Width(max(minPaneWidth, m.width)).Render(m.err))
	}
	return top
}

func (m Model) composerBlock() string {
	var parts []string
	if m.compactHint != "" && !m.busy {
		parts = append(parts, hintStyle.Render(truncateRunes(m.compactHint, max(minPaneWidth, m.width))))
	}
	if m.showLiveStatus() && !m.liveStatusInSubagentDrawer() {
		parts = append(parts, m.liveStatusView())
	}
	if button := m.commitPushRow(); button != "" {
		parts = append(parts, button)
	}
	parts = append(parts, m.promptLine())
	if len(parts) == 1 {
		return parts[0]
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m Model) promptLine() string {
	contentW := m.promptContentWidth()
	// Keep live prompt width identical to paint/hit-test wrap width.
	m.prompt.SetWidth(contentW)
	h := m.promptHeight()
	m.prompt.SetHeight(h)
	// Always paint with hard-wrap body so mouse columns match the pixels
	// (bubbles soft-wrap View was skewing clicks to the right on lower rows).
	text := m.promptBodyPaint(contentW, h)
	if len(m.promptVisualLines()) > h {
		// Scrollbar uses the same scroll offset as promptBodyPaint.
		visN := len(m.promptVisualLines())
		yOff := m.promptScrollOffset(visN, h)
		percent := 0.0
		if visN > h {
			percent = float64(yOff) / float64(visN-h)
		}
		text = withScrollbar(text, contentW, h, percent, true)
	}
	text = keepBackground(text, theme.ColorComposer())
	// The composer uses a dedicated input surface above the neutral black
	// canvas. Its border carries state: it throbs with the shared pulse while
	// the agent works, holds a dim accent glow while the user edits, and stays
	// muted idle.
	var border color.Color = theme.ColorBorder()
	switch {
	case m.busy && m.pulseOn:
		border = theme.PulseAccent(m.pulseT())
	case m.promptEditing():
		border = theme.PulseAccent(composerFocusGlow)
	}
	boxed := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		BorderBackground(theme.ColorComposer()).
		Background(theme.ColorComposer()).
		Width(max(minPaneWidth, m.width)).
		Render(text)
	return embedComposerBorderLabels(boxed, m.composerFooter(contentW), border)
}

func embedComposerBorderLabels(boxed, footer string, border color.Color) string {
	lines := strings.Split(boxed, "\n")
	if len(lines) == 0 {
		return boxed
	}
	plain := ansi.Strip(footer)
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return boxed
	}
	// Render label with transparent/muted look on the border: faint + muted fg, border bg.
	label := lipgloss.NewStyle().Foreground(theme.ColorMute()).Faint(true).Background(theme.ColorComposer()).Render(" "+plain+" ")
	labelPlain := " "+plain+" "
	labelW := lipgloss.Width(labelPlain)
	idx := len(lines) - 1
	bottom := lines[idx]
	bw := lipgloss.Width(ansi.Strip(bottom))

	left := 1
	if left+labelW > bw-1 {
		left = max(1, bw-labelW-1)
	}
	// Splice display-aware: replace runes in the stripped bottom, then re-apply border color.
	stripped := ansi.Strip(bottom)
	runes := []rune(stripped)
	// Find display columns: for rounded border, width == rune count (all single width).
	// Replace slice [left : left+labelW] with labelPlain runes
	for i, r := range []rune(labelPlain) {
		pos := left + i
		if pos >= 0 && pos < len(runes) {
			runes[pos] = r
		}
	}
	_ = string(runes)
	// Re-style bottom border with border color, but keep label faint/muted via embedded ANSI.
	// Build colored bottom: border color for border chars, label already has its own ANSI.
	borderStyle := lipgloss.NewStyle().Foreground(border).Background(theme.ColorComposer())
	// Split newBottomPlain around label to color border parts separately
	before := string(runes[:left])
	after := string(runes[left+labelW:])
	lines[idx] = borderStyle.Render(before) + label + borderStyle.Render(after)
	return strings.Join(lines, "\n")
}

func (m Model) composerFooter(width int) string {
	left := m.composerLeft()
	status := m.statusChipLabel()
	detailBudget := width - lipgloss.Width(left) - lipgloss.Width(status) - composerFooterDetailGap
	details := ""
	if detailBudget > 0 {
		details = m.fitFooterRight(detailBudget)
	}
	right := status
	if details != "" {
		right = details + "  " + status
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + hintStyle.Render(right)
}

func (m Model) footerPieces() (tokens, cache, cost, tps string) {
	window := m.modelContext(m.modelLabel())
	if m.statusSegmentEnabled("tokens") {
		switch {
		case m.tokensUsed > 0 && window > 0:
			tokens = formatTokens(m.tokensUsed) + "/" + formatTokens(int64(window))
		case m.tokensUsed > 0:
			tokens = formatTokens(m.tokensUsed)
		case window > 0:
			tokens = "0/" + formatTokens(int64(window))
		}
	}
	hit, miss := m.cacheTotals()
	if m.statusSegmentEnabled("cache") && (hit > 0 || miss > 0) {
		cache = formatCache(hit, miss)
	}
	_, subs, total := m.costTotals()
	if m.statusSegmentEnabled("cost") && (total > 0 || m.tokensUsed > 0 || hit > 0 || miss > 0) {
		cost = formatCost(total)
		if subs > 0 {
			cost += " +" + formatCost(subs)
		}
	}
	if m.statusSegmentEnabled("tps") {
		tps = m.tpsDisplayLabel()
	}
	return tokens, cache, cost, tps
}

func (m Model) footerExtraSegments() []string {
	var parts []string
	if m.statusSegmentEnabled("models") && len(m.models) > 0 {
		parts = append(parts, fmt.Sprintf("models:%d", len(m.models)))
	}
	if m.statusSegmentEnabled("scroll") && m.transcript.TotalLineCount() > m.transcript.Height() {
		parts = append(parts, "scroll ↑↓")
	}
	return parts
}

// subsStatusLabel is a persistent footer chip for this session's sub-agents.
// Shows live/total when any exist (including completed children from the store).
func (m Model) subsStatusLabel() string {
	if m.session == nil {
		return ""
	}
	live, total := m.subagentCounts()
	if total <= 0 {
		return ""
	}
	if live > 0 {
		return fmt.Sprintf("subs:%d/%d", live, total)
	}
	return fmt.Sprintf("subs:%d", total)
}

func (m Model) fitFooterRight(budget int) string {
	model := ""
	if m.statusSegmentEnabled("model") {
		model = strings.TrimSpace(m.modelChipLabel() + "  " + m.variantChipLabel())
	}
	tokens, cache, cost, tps := m.footerPieces()
	subs := ""
	if m.statusSegmentEnabled("subs") {
		subs = m.subsStatusLabel()
	}
	extras := m.footerExtraSegments()
	chips := model
	try := func(lead string, bits ...string) string {
		parts := make([]string, 0, len(bits))
		for _, b := range bits {
			if b != "" {
				parts = append(parts, b)
			}
		}
		stats := strings.Join(parts, "  ")
		if lead != "" {
			right := joinFooter(lead, stats)
			if lipgloss.Width(right) <= budget {
				return right
			}
			if stats != "" {
				chipBudget := budget - lipgloss.Width(stats) - footerStatGap
				if chipBudget >= footerChipMin {
					return truncateRunes(lead, chipBudget) + "  " + stats
				}
			}
			return ""
		}
		if stats != "" && lipgloss.Width(stats) <= budget {
			return stats
		}
		return ""
	}
	withExtras := func(bits ...string) []string { return append(bits, extras...) }
	if s := try(chips, withExtras(tokens, cache, cost, tps, subs)...); s != "" {
		return s
	}
	if s := try(model, withExtras(tokens, cache, cost, tps, subs)...); s != "" {
		return s
	}
	if s := try(chips, withExtras(tokens, cache, cost, subs)...); s != "" {
		return s
	}
	if s := try(model, withExtras(tokens, cache, cost, subs)...); s != "" {
		return s
	}
	if s := try(model, withExtras(tokens, cache, subs)...); s != "" {
		return s
	}
	if s := try(model, withExtras(cache, subs)...); s != "" {
		return s
	}
	if s := try(model, withExtras(subs)...); s != "" {
		return s
	}
	if s := try("", withExtras(tokens, cache, cost, tps)...); s != "" {
		return s
	}
	if s := try("", cache); s != "" {
		return s
	}
	if cache != "" {
		return truncateRunes(cache, budget)
	}
	return truncateRunes(chips, budget)
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
		return percent, 0
	}
	if hit <= 0 {
		return 0, percent
	}
	hitPct = int((hit*100 + total/2) / total)
	if hitPct > percent {
		hitPct = percent
	}
	return hitPct, percent - hitPct
}

func formatPercent(n int) string {
	if n < 0 {
		n = 0
	}
	if n > percent {
		n = percent
	}
	return fmt.Sprintf("%d%%", n)
}

func (m Model) cacheTotals() (hit, miss int64) {
	u := m.rolledSubagentUsage()
	return m.cacheHit + u.CacheHit, m.cacheMiss + u.CacheMiss
}

func (m Model) costTotals() (parent, subs, total float64) {
	parent = m.sessionCost
	subs = m.rolledSubagentUsage().Cost
	return parent, subs, parent + subs
}

func formatCost(usd float64) string {
	if usd <= 0 {
		return "$0.00"
	}
	if usd < minCentDisplay {
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.3f", usd)
}

func formatTPS(n float64) string {
	if n >= tpsKilo {
		if n >= tpsTenKilo {
			return fmt.Sprintf("%.0fk tps", n/tpsKilo)
		}
		return fmt.Sprintf("%.1fk tps", n/tpsKilo)
	}
	if n >= minWholeTPS {
		return fmt.Sprintf("%.0f tps", n)
	}
	return fmt.Sprintf("%.1f tps", n)
}

func (m Model) tpsDisplayLabel() string {
	n := m.displayTPS()
	if n <= 0 {
		if m.busy {
			return "measuring"
		}
		return ""
	}
	label := formatTPS(n)
	estimated := m.tpsEstimated
	if m.busy && !m.stepMetrics && (len(m.tpsSamples) > 0 || m.generatedThisTurn() > 0) {
		estimated = true
	}
	if estimated {
		return "~" + label
	}
	return label
}

func (m Model) composerLeft() string {
	if !m.statusSegmentEnabled("prompt") {
		return ""
	}
	switch {
	case m.err != "":
		return errStyle.Render("error")
	case m.busy:
		if strings.TrimSpace(m.prompt.Value()) != "" {
			return busyStyle.Render("enter send now")
		}
		return hintStyle.Render("edit")
	default:
		if _, ok := m.selectedHistoryItem(); ok {
			return hintStyle.Render("history: ↑/↓ previous/next")
		}
		return hintStyle.Render("enter send")
	}
}

func (m Model) transcriptRenderHeight() int {
	// Reserve every row that sits outside the transcript so the composer
	// never gets pushed off-screen by drawers (model, sub-agents, slash).
	fixedRows := lipgloss.Height(m.headerView()) + 1 + 1 + 1 + lipgloss.Height(m.composerBlock())
	if panel := m.todoPanelView(); panel != "" {
		fixedRows += lipgloss.Height(panel)
	}
	if m.slashMode {
		fixedRows += 1 + lipgloss.Height(m.slashView())
	}
	if m.pickerMode {
		fixedRows += 1 + lipgloss.Height(m.pickerView())
	}
	if m.subagentPickerMode && !m.subagentLogMode {
		fixedRows += 1 + lipgloss.Height(m.subagentDrawerView())
	}
	if m.statusMode {
		fixedRows += 1 + lipgloss.Height(m.statusDrawerView())
	}
	if m.err != "" {
		fixedRows += lipgloss.Height(errStyle.Width(max(minPaneWidth, m.width)).Render(m.err))
	}
	avail := m.height - fixedRows
	if avail < minPaneHeight {
		return max(1, avail)
	}
	return avail
}

// paintedTranscript is the viewport the user sees: current width/height and
// pinned to the bottom when the model thinks it is at the bottom. Click
// mapping must use this offset, not the stale model YOffset from the
// default 80x24 size used at replay.
func (m Model) paintedTranscript() viewport.Model {
	h := m.transcriptRenderHeight()
	atBottom := m.transcript.AtBottom()
	vp := m.transcript
	vp.SetWidth(m.transcriptContentWidth())
	vp.SetHeight(h)
	if atBottom {
		vp.GotoBottom()
	}
	return vp
}

// transcriptView renders the transcript viewport. When user-turn ticks exist
// they occupy the far-right column; otherwise a scrollbar is used on overflow.
func (m Model) transcriptView() string {
	h := m.transcriptRenderHeight()
	w := max(minPaneWidth, m.width)
	if len(m.items) == 0 {
		lines := []string{
			lazykoderLogo,
			lipgloss.NewStyle().Foreground(theme.ColorText()).Bold(true).Render("new session"),
			hintStyle.Render("ask anything about this project"),
			hintStyle.Render("/ commands   @ files   ? help   /settings"),
		}
		if m.projectInstructionsNotice != "" {
			lines = append(lines, hintStyle.Render(m.projectInstructionsNotice))
		}
		empty := strings.Join(lines, "\n")
		return lipgloss.NewStyle().Width(w).Height(h).Align(lipgloss.Center, lipgloss.Center).Render(empty)
	}
	vp := m.paintedTranscript()
	view := vp.View()
	if m.selection.active && m.selection.hasRange() {
		view = m.highlightTranscriptSelection(view, vp.YOffset())
	}
	contentW := vp.Width()
	// User-nav rail is painted in chatScreen over a stable span so an
	// open todo list does not move the ticks. Still skip the scrollbar.
	if m.hasUserNav() {
		return view
	}
	overflow := vp.TotalLineCount() > h
	return withScrollbar(view, contentW, h, vp.ScrollPercent(), overflow)
}

func (m Model) helpScreen() string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.helpOverlay())
}

func (m Model) helpOverlay() string {
	rows := [][2]string{
		{"enter", "send / send now if busy"},
		{"shift+enter", "newline"},
		{"/", "commands"},
		{"/new", "new session"},
		{"/resume", "past sessions (ctrl+s)"},
		{"/continue", "after a step-limit stop"},
		{"/compact", "summarize older context"},
		{"/settings", "project defaults"},
		{"/agents", "sub-agents + logs"},
		{"/status", "status details and visibility"},
		{"/provider", "switch chat provider"},
		{"/model", "switch live model"},
		{"/variant", "reasoning effort"},
		{"/refresh", "reload models.json"},
		{"@", "mention a file"},
		{"click model", "switch model"},
		{"ctrl+e", "expand / collapse all tools"},
		{"ctrl+p", "expand / collapse all thinking"},
		{"esc", "cancel turn; twice clears"},
		{"ctrl+a", "select all"},
		{"ctrl+z", "undo prompt"},
		{"c", "copy selection"},
		{"ctrl+c", "copy selected / clear / quit"},
	}
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText())
	actStyle := hintStyle
	keyW := 0
	for _, row := range rows {
		if w := lipgloss.Width(row[0]); w > keyW {
			keyW = w
		}
	}
	innerW := min(overlayMaxW, max(minPaneWidth, m.overlayWidth()-cardBorder-cardBorderPad))
	twoCol := m.width >= twoColMinWidth
	colW := innerW
	if twoCol {
		colW = max(overlayColMin, (innerW-twoColPad)/layoutHalf)
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("keys")
	closeBtn := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("[x]")
	gap := max(1, innerW-lipgloss.Width(title)-lipgloss.Width(closeBtn))
	header := title + strings.Repeat(" ", gap) + closeBtn

	format := func(row [2]string, width int) string {
		line := keyStyle.Render(row[0]) + strings.Repeat(" ", max(keyColPad, keyW-lipgloss.Width(row[0])+keyColPad)) + actStyle.Render(row[1])
		if lipgloss.Width(line) > width {
			return truncateRunes(row[0]+"  "+row[1], width)
		}
		return line
	}
	var body strings.Builder
	if twoCol {
		mid := (len(rows) + 1) / layoutHalf
		for i := 0; i < mid; i++ {
			if i > 0 {
				body.WriteString("\n")
			}
			left := format(rows[i], colW)
			right := ""
			if i+mid < len(rows) {
				right = format(rows[i+mid], colW)
			}
			pad := max(1, innerW-lipgloss.Width(left)-lipgloss.Width(right))
			body.WriteString(left)
			body.WriteString(strings.Repeat(" ", pad))
			body.WriteString(right)
		}
	} else {
		for i, row := range rows {
			if i > 0 {
				body.WriteString("\n")
			}
			body.WriteString(format(row, innerW))
		}
	}
	body.WriteString("\n")
	body.WriteString(hintStyle.Width(innerW).Render("esc or ?  close"))
	content := keepBackground(lipgloss.JoinVertical(lipgloss.Left, header, body.String()), theme.ColorSurface())
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorBorder()).
		BorderBackground(theme.ColorSurface()).
		Background(theme.ColorSurface()).
		Padding(1, cardHorzPad).
		Width(min(m.overlayWidth(), innerW+cardBorder+cardBorderPad)).
		Render(content)
}

func (m Model) helpCloseRect() (x0, y, x1 int, ok bool) {
	if !m.helpMode {
		return 0, 0, 0, false
	}
	for i, line := range strings.Split(m.helpScreen(), "\n") {
		plain := ansi.Strip(line)
		if !strings.Contains(plain, "keys") || !strings.Contains(plain, "[x]") {
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

func (m Model) headerView() string {
	brand := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorAccent()).Render("lazykoder")
	title := m.sessionTitle()
	cwd := filepath.Base(m.workdir)
	if cwd == "" || cwd == "." {
		cwd = m.workdir
	}
	sep := hintStyle.Render("  ·  ")
	w := max(minPaneWidth, m.width)
	showCwd := cwd != "" && cwd != "lazykoder" && cwd != "."
	right := ""
	if showCwd {
		right = hintStyle.Render(cwd)
	}
	rightW := 0
	if right != "" {
		rightW = lipgloss.Width(sep) + lipgloss.Width(right)
	}
	avail := w - lipgloss.Width(brand) - lipgloss.Width(sep) - rightW
	if avail < minAvailW {
		row1 := truncateRunes(brand+"  "+title, w)
		if showCwd {
			return row1 + "\n" + hintStyle.Render(truncateRunes(cwd, w))
		}
		return row1
	}
	title = truncateRunes(title, avail)
	line := brand + sep + lipgloss.NewStyle().Foreground(theme.ColorText()).Render(title)
	if right != "" {
		line += sep + right
	}
	return line
}

func (m Model) confirmOverlay() string {
	innerW := max(minPaneWidth, m.overlayWidth()-cardBorder-cardBorderPad)
	inner := keepBackground(lipgloss.NewStyle().Width(innerW).Render(m.confirm.View()), theme.ColorSurface())
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorDanger()).
		BorderBackground(theme.ColorSurface()).
		Background(theme.ColorSurface()).
		Padding(1, cardHorzPad).
		Width(min(m.overlayWidth(), innerW+cardBorder+cardBorderPad)).
		Render(inner)
}

func (m Model) askOverlay() string {
	_, cardW, lines, _ := m.askOverlayLines()
	content := keepBackground(strings.Join(lines, "\n"), theme.ColorDialog())
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorAccent()).
		BorderBackground(theme.ColorDialog()).
		Background(theme.ColorDialog()).
		Padding(1, cardHorzPad).
		Width(cardW).
		Render(content)
}

type askOptionSpan struct {
	index int
	start int
	end   int
}

// askOverlayLines builds the dialog content and the option row spans from the
// same wrapped lines used by askOverlay, keeping pointer hit-testing aligned.
func (m Model) askOverlayLines() (innerW, cardW int, lines []string, spans []askOptionSpan) {
	cardW = max(minPaneWidth, m.overlayWidth())
	innerW = max(1, cardW-cardBorder-cardBorderPad)
	appendText := func(text string, style lipgloss.Style) {
		for _, line := range wrapAskText(text, innerW) {
			lines = append(lines, style.Width(innerW).MaxWidth(innerW).Render(line))
		}
	}

	appendText(m.askQuestion.Question, lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()))
	if strings.TrimSpace(m.askQuestion.Header) != "" {
		appendText(m.askQuestion.Header, hintStyle)
	}
	lines = append(lines, strings.Repeat(" ", innerW))
	for i, opt := range m.askQuestion.Options {
		start := len(lines)
		prefix := fmt.Sprintf("  %d. ", i+1)
		style := hintStyle
		if i == m.askCursor {
			prefix = fmt.Sprintf("▸ %d. ", i+1)
			style = lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText())
		}
		continuation := strings.Repeat(" ", lipgloss.Width(prefix))
		textW := max(1, innerW-lipgloss.Width(prefix))
		wrapped := wrapAskText(opt, textW)
		for j, line := range wrapped {
			if j == 0 {
				line = prefix + line
			} else {
				line = continuation + line
			}
			lines = append(lines, style.Width(innerW).MaxWidth(innerW).Render(line))
		}
		spans = append(spans, askOptionSpan{index: i, start: start, end: len(lines)})
	}
	// Hard-coded custom answer affordance so the user is never trapped by the
	// LLM's enumerated choices. Selecting it opens a free-form input.
	customIdx := len(m.askQuestion.Options)
	customLabel := "✏️  Type your own answer..."
	start := len(lines)
	prefix := fmt.Sprintf("  %d. ", customIdx+1)
	style := hintStyle
	if customIdx == m.askCursor {
		prefix = fmt.Sprintf("▸ %d. ", customIdx+1)
		style = lipgloss.NewStyle().Bold(true).Foreground(theme.ColorAccent())
	}
	continuation := strings.Repeat(" ", lipgloss.Width(prefix))
	textW := max(1, innerW-lipgloss.Width(prefix))
	wrapped := wrapAskText(customLabel, textW)
	for j, line := range wrapped {
		if j == 0 {
			line = prefix + line
		} else {
			line = continuation + line
		}
		lines = append(lines, style.Width(innerW).MaxWidth(innerW).Render(line))
	}
	spans = append(spans, askOptionSpan{index: customIdx, start: start, end: len(lines)})
	appendText("j/k select  •  enter confirm  •  esc cancel  •  custom writes to LLM", hintStyle)
	return innerW, cardW, lines, spans
}

func wrapAskText(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	var lines []string
	for _, part := range strings.Split(text, "\n") {
		lines = append(lines, wrapAskLine(part, width)...)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func wrapAskLine(line string, width int) []string {
	words := strings.Fields(line)
	if len(words) == 0 {
		return []string{""}
	}
	lines := make([]string, 0, len(words))
	current := ""
	flush := func() {
		if current != "" {
			lines = append(lines, current)
			current = ""
		}
	}
	for _, word := range words {
		if lipgloss.Width(word) > width {
			flush()
			chunks := hardWrapRunes([]rune(word), width)
			for i, chunk := range chunks {
				if i == len(chunks)-1 {
					current = string(chunk)
				} else {
					lines = append(lines, string(chunk))
				}
			}
			continue
		}
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if lipgloss.Width(candidate) > width {
			flush()
		}
		if current == "" {
			current = word
		} else if lipgloss.Width(current)+1+lipgloss.Width(word) <= width {
			current += " " + word
		}
	}
	flush()
	return lines
}

func (m Model) askOverlayRect() (left, top, width, height int, ok bool) {
	card := m.askOverlay()
	width = lipgloss.Width(card)
	height = lipgloss.Height(card)
	regionH := m.composerTop()
	if !m.askMode || width < 1 || height < 1 || regionH < 1 {
		return 0, 0, 0, 0, false
	}
	regionH = min(regionH, max(1, m.height))
	top = max(0, (regionH-height)/centerDiv)
	if top+height > regionH {
		top = max(0, regionH-height)
	}
	left = max(0, (m.width-width)/centerDiv)
	return left, top, width, height, true
}

func (m Model) askIndexAtScreen(x, y int) (int, bool) {
	left, top, width, _, ok := m.askOverlayRect()
	if !ok || x < left || x >= left+width {
		return -1, false
	}
	_, _, lines, spans := m.askOverlayLines()
	contentTop := top + askCardContentTop
	row := y - contentTop
	if row < 0 || row >= len(lines) {
		return -1, false
	}
	for _, span := range spans {
		if row >= span.start && row < span.end {
			return span.index, true
		}
	}
	return -1, false
}

// scrollbarRect returns the screen rectangle (top row, bottom row, column)
// of a rendered scrollbar column for the given target (0 = transcript,
// 1 = picker). ok is false when no scrollbar is shown.
func (m Model) scrollbarRect(target int) (top, bottom, col int, ok bool) {
	if target == 0 {
		if m.pickerMode || m.hasUserNav() {
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
	if y, ok := m.pickerHeaderScreenY(); ok {
		return y
	}
	top := m.transcriptTop() + m.transcriptRenderHeight() + 1
	if m.slashMode {
		top += 1 + lipgloss.Height(m.slashView())
	}
	return top
}

func (m Model) pickerHeaderScreenY() (int, bool) {
	if !m.pickerMode {
		return 0, false
	}
	for i, line := range m.paintedLines() {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "models  ·") ||
			strings.Contains(plain, "reasoning  ·") ||
			strings.Contains(plain, "variants  ·") {
			return i, true
		}
	}
	return 0, false
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
	thumbCell := lipgloss.NewStyle().Foreground(theme.ColorText()).Render("█")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		if w := width - lipgloss.Width(line); w > 0 {
			b.WriteString(line)
			b.WriteString(strings.Repeat(" ", w))
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

// footerChipHit reports which footer chip contains (x,y). If both model
// and variant boxes overlap, the closer center wins.
func (m Model) footerChipHit(x, y int) (hit bool, which string) {
	if left, top, right, bottom, ok := m.statusChipRect(); ok &&
		x >= left && x < right && y >= top && y < bottom {
		return true, "status"
	}
	ml, mt, mr, mb, mok := m.modelStatusRect()
	vl, vt, vr, vb, vok := m.variantStatusRect()
	in := func(l, t, r, b int) bool {
		return x >= l && x < r && y >= t && y < b
	}
	inM := mok && in(ml, mt, mr, mb)
	inV := vok && in(vl, vt, vr, vb)
	switch {
	case inM && inV:
		mc := (ml + mr) / layoutHalf
		vc := (vl + vr) / layoutHalf
		if absInt(x-vc) < absInt(x-mc) {
			return true, "variant"
		}
		return true, "model"
	case inV:
		return true, "variant"
	case inM:
		return true, "model"
	default:
		return false, ""
	}
}

func (m Model) statusChipRect() (left, top, right, bottom int, ok bool) {
	if m.pickerMode || m.sessionPickerMode || m.usageMode || m.settingsMode || m.subagentLogMode || m.statusMode {
		return 0, 0, 0, 0, false
	}
	plain, y, found := m.composerFooterPlainLine()
	if !found {
		return 0, 0, 0, 0, false
	}
	start, end, hit := displaySpan(plain, m.statusChipLabel())
	if !hit {
		return 0, 0, 0, 0, false
	}
	return max(0, start-1), y, end, y + 1, true
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (m Model) modelStatusRect() (left, top, right, bottom int, ok bool) {
	return m.footerChipRect(m.modelChipLabel())
}

func (m Model) variantStatusRect() (left, top, right, bottom int, ok bool) {
	return m.footerChipRect(m.variantChipLabel())
}

func (m Model) footerChipRect(chip string) (left, top, right, bottom int, ok bool) {
	// Allow hits while the sub-agent drawer or slash menu is open; only
	// suppress when another full-screen/modal chrome owns the mouse. A live
	// turn does not own the model or variant controls: changes apply to the
	// next prompt and should remain available while the current one runs.
	if chip == "" || m.pickerMode || m.sessionPickerMode || m.usageMode || m.settingsMode || m.subagentLogMode || m.statusMode {
		return 0, 0, 0, 0, false
	}
	plain, y, found := m.composerFooterPlainLine()
	if !found {
		return 0, 0, 0, 0, false
	}
	needles := []string{chip}
	base := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(chip), "▾"))
	if base != "" && base != chip {
		needles = append(needles, base+" ▾", base+"▾", base)
	}
	for _, n := range needles {
		if n == "" {
			continue
		}
		if start, end, hit := displaySpan(plain, n); hit {
			// One column of slack on the left. Do not pad right into
			// the next chip (model ▾ sits beside variant ▾).
			return max(0, start-1), y, end, y + 1, true
		}
	}
	// Footer may truncate long model ids; use chevron order on the row.
	// modelStatusRect / variantStatusRect pass labels that identify the slot.
	idx := 0
	if footerChipIsVariantLabel(chip) {
		idx = 1
	}
	if start, end, hit := footerChevronChipSpan(plain, idx); hit {
		return max(0, start-1), y, end, y + 1, true
	}
	return 0, 0, 0, 0, false
}

func footerChipIsVariantLabel(chip string) bool {
	base := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(chip), "▾")))
	switch base {
	case "default", "none", "low", "medium", "high", "xhigh", "max", "minimal":
		return true
	default:
		return false
	}
}

// footerChevronChipSpan finds the idx-th "token ▾" chip on the footer row
// (0 = model, 1 = variant) by scanning painted chevrons right-to-left tokens.
func footerChevronChipSpan(plain string, idx int) (start, end int, ok bool) {
	plain = ansi.Strip(plain)
	runes := []rune(plain)
	cols := make([]int, len(runes)+1)
	for i, r := range runes {
		cols[i+1] = cols[i] + max(1, lipgloss.Width(string(r)))
	}
	var chips [][2]int
	for i, r := range runes {
		if r != '▾' && r != '▼' {
			continue
		}
		endCol := cols[i+1]
		j := i - 1
		if j >= 0 && runes[j] == ' ' {
			j--
		}
		for j >= 0 && runes[j] != ' ' && runes[j] != '│' && runes[j] != '|' {
			j--
		}
		startCol := cols[j+1]
		chips = append(chips, [2]int{startCol, endCol})
	}
	if idx < 0 || idx >= len(chips) {
		return 0, 0, false
	}
	return chips[idx][0], chips[idx][1], true
}

// composerFooterPlainLine is the painted composer footer row (enter send + chips).
func (m Model) composerFooterPlainLine() (plain string, y int, ok bool) {
	for i, line := range m.paintedLines() {
		plain = ansi.Strip(line)
		if !composerFooterLine(plain) {
			continue
		}
		return plain, i, true
	}
	return "", 0, false
}

func composerFooterLine(plain string) bool {
	// The rotating tip "enter sends the prompt" must not match "enter send".
	if strings.Contains(plain, "enter send now") {
		return true
	}
	if strings.Contains(plain, "enter send") && !strings.Contains(plain, "enter sends") {
		return true
	}
	if strings.Contains(plain, "history:") {
		return true
	}
	if strings.Contains(plain, "working") && strings.Contains(plain, "esc cancel") {
		return true
	}
	return strings.Contains(plain, "status ▾")
}

// subsStatusRect is the click target for the persistent "subs:N" footer chip.
func (m Model) subsStatusRect() (left, top, right, bottom int, ok bool) {
	if m.busy || m.pickerMode || m.sessionPickerMode || m.subagentPickerMode || m.usageMode || m.settingsMode {
		return 0, 0, 0, 0, false
	}
	subs := m.subsStatusLabel()
	if subs == "" {
		return 0, 0, 0, 0, false
	}
	leftW := lipgloss.Width(m.composerLeft())
	budget := max(footerBudgetMin, max(minPaneWidth, m.width)-2-leftW-1)
	footer := m.fitFooterRight(budget)
	if !strings.Contains(footer, "subs:") {
		return 0, 0, 0, 0, false
	}
	plain, y, found := m.composerFooterPlainLine()
	if !found {
		return 0, 0, 0, 0, false
	}
	start, end, hit := displaySpan(plain, subs)
	if !hit {
		start, end, hit = displaySpan(plain, "subs:")
	}
	if !hit {
		return 0, 0, 0, 0, false
	}
	return start, y, end, y + 1, true
}
