package chat

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

const (
	commitDrawerMaxRows      = 8
	commitDrawerLeftPad      = 0
	commitDrawerTitle        = "Diff"
	commitDrawerPreviewRows  = 6
	commitDiffDetailMinRows  = 3
	commitDiffDetailChrome   = 11
	commitDiffDetailBodyTop  = 3
	commitDiffHunkRowSpan    = 2
	commitDiffScrollbarWidth = 1
)

func (m Model) commitDrawerVisible() bool { return m.commitPushVisible() }

func (m Model) commitDrawerFiles() []WorktreeFile {
	if len(m.commitFiles) > 0 {
		return m.commitFiles
	}
	return nil
}

func (m Model) commitDrawerView(width int) string {
	files := m.commitDrawerFiles()
	cardW := max(minPaneWidth, width)
	if m.pickerDrawerWidth() > 0 {
		cardW = m.pickerDrawerWidth()
	}
	cardW = max(minPaneWidth, min(cardW, width))

	branch := strings.TrimSpace(m.commitBranch)
	headerTitle := commitDrawerTitle
	if branch != "" {
		headerTitle = commitDrawerTitle + "  ·  " + branch
	}
	meta := ""
	if len(files) == 0 {
		meta = "no changes"
	} else if len(files) == 1 {
		meta = "1 file"
	} else {
		meta = fmt.Sprintf("%d files", len(files))
	}

	if len(files) == 0 {
		actionLabel := "[ commit and push ]"
		actionStyle := lipgloss.NewStyle().Foreground(theme.ColorText()).Bold(true).Background(theme.ColorBorder()).Padding(0, 1)
		if m.commitDrawerActionFocused {
			actionStyle = actionStyle.Underline(true)
		}
		action := actionStyle.Render(actionLabel)
		actionLine := lipgloss.NewStyle().Background(theme.ColorSurface()).Width(cardW).Align(lipgloss.Center).Render(action)
		body := hintStyle.Render("nothing to commit") + "\n" + actionLine
		hint := "esc collapse  •  [x] close"
		return m.commitDrawerChrome(headerTitle, meta, body, hint, cardW)
	}

	selected := m.commitDrawerSelected
	if selected < 0 {
		selected = 0
	}
	if selected >= len(files) {
		selected = len(files) - 1
	}

	visibleRows := min(len(files), commitDrawerMaxRows)
	start := 0
	if selected >= visibleRows {
		start = selected - visibleRows + 1
	}
	if start < 0 {
		start = 0
	}
	var rows []string
	for i := start; i < start+visibleRows && i < len(files); i++ {
		f := files[i]
		right := commitFileCounts(f)
		rows = append(rows, drawerRowLine(f.Path, right, i == selected, cardW, commitDrawerLeftPad))
	}
	if len(files) > visibleRows {
		more := fmt.Sprintf("… %d more", len(files)-visibleRows)
		if start > 0 {
			more = fmt.Sprintf("↑ %d  •  ↓ %d", start, len(files)-start-visibleRows)
		}
		rows = append(rows, hintStyle.Width(cardW).Render(truncateRunes(more, cardW)))
	}

	sel := files[selected]
	preview := m.commitDiffPreviewFor(sel.Path)
	if preview == "" {
		if sel.Binary {
			preview = hintStyle.Render("binary file  •  no preview")
		} else if sel.Added == 0 && sel.Removed == 0 {
			preview = hintStyle.Render("no line counts  •  run git diff for details")
		} else {
			preview = fmt.Sprintf("diff -- %s  (%s)", sel.Path, commitFileCounts(sel))
		}
		if preview != "" {
			preview = lipgloss.NewStyle().Background(theme.ColorSurface()).Foreground(theme.ColorMute()).Width(cardW).Render(truncateRunes(preview, cardW))
		}
	} else {
		preview = m.renderCommitPreview(preview, cardW)
	}

	actionLabel := "[ commit and push ]"
	actionStyle := lipgloss.NewStyle().Foreground(theme.ColorText()).Bold(true).Background(theme.ColorBorder()).Padding(0, 1)
	if m.commitDrawerActionFocused {
		actionStyle = actionStyle.Underline(true)
	}
	action := actionStyle.Render(actionLabel)
	actionLine := lipgloss.NewStyle().Background(theme.ColorSurface()).Width(cardW).Align(lipgloss.Center).Render(action)

	body := strings.Join(rows, "\n") + "\n" + preview + "\n" + actionLine
	hint := "↑/↓ select  •  click file  •  enter view diff  •  esc or [x] collapse  •  scroll"
	return m.commitDrawerChrome(headerTitle, meta, body, hint, cardW)
}

func commitFileCounts(f WorktreeFile) string {
	if f.Binary {
		return "binary"
	}
	if f.Added == 0 && f.Removed == 0 {
		return "changed"
	}
	return fmt.Sprintf("+%d -%d", f.Added, f.Removed)
}

func (m Model) commitDiffPreviewFor(path string) string {
	if m.commitDiffPreview == nil {
		return ""
	}
	if v, ok := m.commitDiffPreview[path]; ok {
		return v
	}
	return ""
}

func (m Model) renderCommitPreview(raw string, width int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	if len(lines) > commitDrawerPreviewRows {
		lines = lines[:commitDrawerPreviewRows]
	}
	for i, l := range lines {
		if lipgloss.Width(l) > width {
			lines[i] = truncateRunes(l, width)
		}
		if strings.HasPrefix(strings.TrimSpace(l), "+") && !strings.HasPrefix(strings.TrimSpace(l), "+++") {
			lines[i] = diffAddStyle.Render(lines[i])
		} else if strings.HasPrefix(strings.TrimSpace(l), "-") && !strings.HasPrefix(strings.TrimSpace(l), "---") {
			lines[i] = diffDelStyle.Render(lines[i])
		} else {
			lines[i] = diffCtxStyle.Render(lines[i])
		}
	}
	return strings.Join(lines, "\n")
}

func splitCommitDiffHunks(raw string) []string {
	raw = strings.TrimRight(raw, "\n")
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	firstHunk := -1
	for index, line := range lines {
		if strings.HasPrefix(line, "@@") {
			firstHunk = index
			break
		}
	}
	if firstHunk < 0 {
		return []string{raw}
	}

	hunks := make([]string, 0, 1)
	current := append([]string{}, lines[:firstHunk]...)
	flush := func() {
		if len(current) == 0 {
			return
		}
		hunks = append(hunks, strings.Join(current, "\n"))
		current = nil
	}
	for index, line := range lines[firstHunk:] {
		if index > 0 && strings.HasPrefix(line, "@@") {
			flush()
		}
		current = append(current, line)
	}
	flush()
	return hunks
}

func commitDiffHunkHeader(hunk string) string {
	for _, line := range strings.Split(hunk, "\n") {
		if strings.HasPrefix(line, "@@") {
			return strings.TrimSpace(line)
		}
	}
	for _, line := range strings.Split(hunk, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return "change"
}

func (m Model) commitDiffHunkListContent(width int) string {
	if len(m.commitDiffHunks) == 0 {
		return drawerNormalStyle.Width(width).MaxWidth(width).Render("no diff sections")
	}
	separatorStyle := lipgloss.NewStyle().Foreground(theme.ColorBorder()).Width(width).MaxWidth(width)
	rows := make([]string, 0, len(m.commitDiffHunks)*commitDiffHunkRowSpan-1)
	for index, hunk := range m.commitDiffHunks {
		right := fmt.Sprintf("%d/%d", index+1, len(m.commitDiffHunks))
		rows = append(rows, drawerRowLine(commitDiffHunkHeader(hunk), right, index == m.commitDiffHunkSelected, width, 0))
		if index < len(m.commitDiffHunks)-1 {
			rows = append(rows, separatorStyle.Render(strings.Repeat("─", max(1, width))))
		}
	}
	return strings.Join(rows, "\n")
}

func renderCommitDiffHunks(hunks []string, width int) string {
	if len(hunks) == 0 {
		return ""
	}
	separator := lipgloss.NewStyle().Foreground(theme.ColorBorder()).Render(strings.Repeat("─", width))
	sections := make([]string, 0, len(hunks)*2-1)
	for index, hunk := range hunks {
		if index > 0 {
			sections = append(sections, separator)
		}
		sections = append(sections, renderDiff(hunk, width))
	}
	return strings.Join(sections, "\n")
}

func commitDiffHunkStartLine(hunks []string, selected, width int) int {
	selected = min(max(0, selected), len(hunks))
	line := 0
	for index := 0; index < selected; index++ {
		line += lipgloss.Height(renderDiff(hunks[index], width)) + 1
	}
	return line
}

func (m Model) commitDiffDetailViewportSize() (int, int) {
	innerW := m.commitDiffDetailContentWidth()
	viewportHeight := max(commitDiffDetailMinRows, m.height-commitDiffDetailChrome)
	return max(1, innerW-commitDiffScrollbarWidth), viewportHeight
}

func (m Model) commitDiffDetailContentWidth() int {
	cardW := m.commitDiffDetailWidth()
	return max(minPaneWidth+commitDiffScrollbarWidth, cardW-cardBorder-2*settingsCardHorzPad)
}

func (m Model) refreshCommitDiffHunkList() Model {
	innerW, viewportHeight := m.commitDiffDetailViewportSize()
	if len(m.commitDiffHunks) == 0 {
		m.commitDiffHunkSelected = 0
	} else {
		m.commitDiffHunkSelected = min(m.commitDiffHunkSelected, len(m.commitDiffHunks)-1)
		m.commitDiffHunkSelected = max(0, m.commitDiffHunkSelected)
	}
	m.commitDiffHunkVp.SetWidth(innerW)
	m.commitDiffHunkVp.SetHeight(viewportHeight)
	m.commitDiffHunkVp.SetContent(m.commitDiffHunkListContent(innerW))
	row := m.commitDiffHunkSelected * commitDiffHunkRowSpan
	offset := m.commitDiffHunkVp.YOffset()
	if row < offset {
		offset = row
	}
	if row >= offset+viewportHeight {
		offset = row - viewportHeight + 1
	}
	m.commitDiffHunkVp.SetYOffset(max(0, offset))
	return m
}

func (m Model) openCommitDiffHunkContext() Model {
	if m.commitDiffHunkSelected < 0 || m.commitDiffHunkSelected >= len(m.commitDiffHunks) {
		return m
	}
	innerW, viewportHeight := m.commitDiffDetailViewportSize()
	m.commitDiffDetailVp.SetWidth(innerW)
	m.commitDiffDetailVp.SetHeight(viewportHeight)
	m.commitDiffDetailVp.SetContent(renderCommitDiffHunks(m.commitDiffHunks, innerW))
	m.commitDiffDetailVp.GotoTop()
	selectedStart := commitDiffHunkStartLine(m.commitDiffHunks, m.commitDiffHunkSelected, innerW)
	if selectedStart >= viewportHeight {
		m.commitDiffDetailVp.SetYOffset(selectedStart)
	}
	m.commitDiffHunkContextMode = true
	return m
}

func (m Model) openCommitDiffDetail() Model {
	files := m.commitDrawerFiles()
	if m.commitDrawerSelected < 0 || m.commitDrawerSelected >= len(files) {
		return m
	}
	file := files[m.commitDrawerSelected]
	m.commitDiffDetailPath = file.Path
	m.commitDiffHunks = splitCommitDiffHunks(m.commitDiffRaw(file))
	m.commitDiffHunkSelected = 0
	m.commitDiffHunkContextMode = false
	m.commitDiffDetailMode = true
	m.commitDrawerActionFocused = false
	m = m.refreshCommitDiffHunkList()
	if len(m.commitDiffHunks) > 0 {
		m = m.openCommitDiffHunkContext()
	}
	return m
}

func (m Model) commitDiffRaw(file WorktreeFile) string {
	raw := m.commitDiffPreviewFor(file.Path)
	if strings.TrimSpace(raw) != "" {
		return raw
	}
	if file.Binary {
		return fmt.Sprintf("diff -- %s\nBinary file changed\n", file.Path)
	}
	return fmt.Sprintf("diff -- %s\n(no diff payload available)\n", file.Path)
}

func (m Model) resizeCommitDiffDetail() Model {
	if !m.commitDiffDetailMode {
		return m
	}
	if m.commitDiffHunkContextMode {
		offset := m.commitDiffDetailVp.YOffset()
		atBottom := m.commitDiffDetailVp.TotalLineCount() == 0 || m.commitDiffDetailVp.AtBottom()
		innerW, viewportHeight := m.commitDiffDetailViewportSize()
		m.commitDiffDetailVp.SetWidth(innerW)
		m.commitDiffDetailVp.SetHeight(viewportHeight)
		m.commitDiffDetailVp.SetContent(renderCommitDiffHunks(m.commitDiffHunks, innerW))
		if atBottom {
			m.commitDiffDetailVp.GotoBottom()
		} else {
			m.commitDiffDetailVp.SetYOffset(offset)
		}
		return m
	}
	return m.refreshCommitDiffHunkList()
}

func (m Model) closeCommitDiffDetail() Model {
	m.commitDiffDetailMode = false
	m.commitDiffDetailPath = ""
	m.commitDiffHunks = nil
	m.commitDiffHunkSelected = 0
	m.commitDiffHunkContextMode = false
	m.layout = layoutSnap{}
	return m
}

func (m Model) commitDiffDetailWidth() int {
	return max(minPaneWidth, min(m.width-cardBorder, m.overlayWidth()))
}

func (m Model) commitDiffDetailScreen() string {
	cardW := m.commitDiffDetailWidth()
	innerW := m.commitDiffDetailContentWidth()
	title := "Diff"
	if m.commitDiffDetailPath != "" {
		title += "  ·  " + m.commitDiffDetailPath
	}
	close := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("[x]")
	header := drawerHeaderTitleStyle.Render(truncateRunes(title, max(1, innerW-lipgloss.Width(close)-1)))
	header += strings.Repeat(" ", max(1, innerW-lipgloss.Width(header)-lipgloss.Width(close))) + close

	meta := ""
	for index, file := range m.commitDrawerFiles() {
		if file.Path == m.commitDiffDetailPath {
			meta = fmt.Sprintf("%s  ·  %d of %d files  ·  change %d of %d", commitFileCounts(file), index+1, len(m.commitDrawerFiles()), m.commitDiffHunkSelected+1, len(m.commitDiffHunks))
			break
		}
	}
	if meta == "" {
		meta = "diff details"
	}
	body := ""
	footerText := ""
	if m.commitDiffHunkContextMode {
		body = m.commitDiffDetailVp.View()
		body = withScrollbar(body, m.commitDiffDetailVp.Width(), m.commitDiffDetailVp.Height(), m.commitDiffDetailVp.ScrollPercent(), m.commitDiffDetailVp.TotalLineCount() > m.commitDiffDetailVp.Height())
		body = fitCommitDiffViewport(body, innerW)
		footerText = "↑/↓ scroll code  •  wheel scroll  •  ←/→ file  •  enter/esc back"
	} else {
		body = m.commitDiffHunkVp.View()
		body = withScrollbar(body, m.commitDiffHunkVp.Width(), m.commitDiffHunkVp.Height(), m.commitDiffHunkVp.ScrollPercent(), m.commitDiffHunkVp.TotalLineCount() > m.commitDiffHunkVp.Height())
		body = fitCommitDiffViewport(body, innerW)
		footerText = "↑/↓ select change  •  enter expand  •  wheel select  •  ←/→ file"
	}
	navigation := m.commitDiffDetailNavigation(innerW)
	footer := hintStyle.Width(innerW).Render(truncateRunes(footerText, innerW))
	content := lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		drawerHeaderMetaStyle.Render(meta),
		"",
		body,
		"",
		navigation,
		footer,
	)
	content = keepBackground(content, theme.ColorSurface())
	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.ColorBorder()).
			BorderBackground(theme.ColorSurface()).
			Background(theme.ColorSurface()).
			Padding(settingsCardVertPad, settingsCardHorzPad).
			Width(cardW).
			Render(content),
	)
}

func fitCommitDiffViewport(view string, width int) string {
	lines := strings.Split(view, "\n")
	for index, line := range lines {
		lineWidth := lipgloss.Width(line)
		if lineWidth > width {
			lines[index] = ansi.Cut(line, 0, width)
			continue
		}
		if lineWidth < width {
			lines[index] = line + strings.Repeat(" ", width-lineWidth)
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) commitDiffDetailNavigation(width int) string {
	files := m.commitDrawerFiles()
	hasPrevious := m.commitDrawerSelected > 0
	hasNext := m.commitDrawerSelected >= 0 && m.commitDrawerSelected < len(files)-1
	previous := hintStyle.Render("[← prev]")
	next := hintStyle.Render("[next →]")
	if hasPrevious {
		previous = drawerHeaderTitleStyle.Render("[← prev]")
	}
	if hasNext {
		next = drawerHeaderTitleStyle.Render("[next →]")
	}
	gap := max(1, width-lipgloss.Width(previous)-lipgloss.Width(next))
	return previous + strings.Repeat(" ", gap) + next
}

func (m Model) commitDiffDetailCloseRect() (x0, y, x1 int, ok bool) {
	if !m.commitDiffDetailMode {
		return 0, 0, 0, false
	}
	for i, line := range strings.Split(m.commitDiffDetailScreen(), "\n") {
		plain := ansi.Strip(line)
		if !strings.Contains(plain, "Diff") || !strings.Contains(plain, "[x]") {
			continue
		}
		if start, end, found := displaySpan(plain, "[x]"); found {
			return max(0, start-1), i, end + 1, true
		}
	}
	return 0, 0, 0, false
}

func (m Model) commitDiffDetailNavRect(next bool) (x0, y, x1 int, ok bool) {
	if !m.commitDiffDetailMode {
		return 0, 0, 0, false
	}
	label := "[← prev]"
	if next {
		label = "[next →]"
	}
	for i, line := range strings.Split(m.commitDiffDetailScreen(), "\n") {
		plain := ansi.Strip(line)
		if start, end, found := displaySpan(plain, label); found {
			return start, i, end, true
		}
	}
	return 0, 0, 0, false
}

func (m Model) commitDiffHunkIndexAtScreenY(y int) (int, bool) {
	if !m.commitDiffDetailMode || m.commitDiffHunkContextMode {
		return -1, false
	}
	_, headerY, _, ok := m.commitDiffDetailCloseRect()
	if !ok {
		return -1, false
	}
	row := y - headerY - commitDiffDetailBodyTop + m.commitDiffHunkVp.YOffset()
	if row < 0 || row%commitDiffHunkRowSpan != 0 {
		return -1, false
	}
	index := row / commitDiffHunkRowSpan
	if index < 0 || index >= len(m.commitDiffHunks) {
		return -1, false
	}
	return index, true
}

func (m Model) navigateCommitDiffHunk(delta int) (Model, tea.Cmd) {
	next := m.commitDiffHunkSelected + delta
	if next < 0 || next >= len(m.commitDiffHunks) {
		return m.resetCommitDrawerTimer()
	}
	m.commitDiffHunkSelected = next
	m = m.refreshCommitDiffHunkList()
	return m.resetCommitDrawerTimer()
}

func (m Model) navigateCommitDiffDetail(delta int) (Model, tea.Cmd) {
	files := m.commitDrawerFiles()
	next := m.commitDrawerSelected + delta
	if next < 0 || next >= len(files) {
		return m.resetCommitDrawerTimer()
	}
	m.commitDrawerSelected = next
	m = m.openCommitDiffDetail()
	if !m.commitDiffDetailMode {
		return m.resetCommitDrawerTimer()
	}
	return m.resetCommitDrawerTimer()
}

func (m Model) commitDiffDetailHit(x, y int, button tea.MouseButton) (Model, tea.Cmd, bool) {
	if !m.commitDiffDetailMode || (button != tea.MouseLeft && button != tea.MouseRight) {
		return m, nil, false
	}
	if x0, cy, x1, ok := m.commitDiffDetailCloseRect(); ok && y == cy && x >= x0 && x < x1 {
		m = m.closeCommitDiffDetail()
		nm, cmd := m.resetCommitDrawerTimer()
		return nm, cmd, true
	}
	if x0, cy, x1, ok := m.commitDiffDetailNavRect(false); ok && y == cy && x >= x0 && x < x1 {
		m, cmd := m.navigateCommitDiffDetail(-1)
		return m, cmd, true
	}
	if x0, cy, x1, ok := m.commitDiffDetailNavRect(true); ok && y == cy && x >= x0 && x < x1 {
		m, cmd := m.navigateCommitDiffDetail(1)
		return m, cmd, true
	}
	if index, ok := m.commitDiffHunkIndexAtScreenY(y); ok {
		m.commitDiffHunkSelected = index
		m = m.openCommitDiffHunkContext()
		m, cmd := m.resetCommitDrawerTimer()
		return m, cmd, true
	}
	if y >= 0 && y < m.height {
		nm, cmd := m.resetCommitDrawerTimer()
		return nm, cmd, true
	}
	return m, nil, false
}

func (m Model) handleCommitDiffDetailKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	if !m.commitDiffDetailMode {
		return m, nil
	}
	switch key.Code {
	case tea.KeyLeft:
		return m.navigateCommitDiffDetail(-1)
	case tea.KeyRight:
		return m.navigateCommitDiffDetail(1)
	}
	if m.commitDiffHunkContextMode {
		switch key.Code {
		case tea.KeyEscape, tea.KeyEnter:
			m.commitDiffHunkContextMode = false
			m = m.refreshCommitDiffHunkList()
			return m.resetCommitDrawerTimer()
		case 'q', 'Q', 'x', 'X':
			m = m.closeCommitDiffDetail()
			return m.resetCommitDrawerTimer()
		default:
			vp, viewportCmd := m.commitDiffDetailVp.Update(key)
			m.commitDiffDetailVp = vp
			m, timerCmd := m.resetCommitDrawerTimer()
			return m, tea.Batch(viewportCmd, timerCmd)
		}
	}
	switch key.Code {
	case tea.KeyEscape, 'q', 'Q', 'x', 'X':
		m = m.closeCommitDiffDetail()
		return m.resetCommitDrawerTimer()
	case tea.KeyEnter:
		m = m.openCommitDiffHunkContext()
		return m.resetCommitDrawerTimer()
	case tea.KeyUp, 'k':
		return m.navigateCommitDiffHunk(-1)
	case tea.KeyDown, 'j':
		return m.navigateCommitDiffHunk(1)
	default:
		vp, viewportCmd := m.commitDiffHunkVp.Update(key)
		m.commitDiffHunkVp = vp
		m, timerCmd := m.resetCommitDrawerTimer()
		return m, tea.Batch(viewportCmd, timerCmd)
	}
}

func (m Model) commitDrawerChrome(title, meta, body, hint string, width int) string {
	leftHead := ""
	if title != "" {
		leftHead = drawerHeaderTitleStyle.Render(title)
	}
	if title != "" && meta != "" {
		leftHead += hintStyle.Render("  ·  ")
	}
	if meta != "" {
		leftHead += drawerHeaderMetaStyle.Render(meta)
	}
	rightHead := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("[x]")
	gap := width - lipgloss.Width(leftHead) - lipgloss.Width(rightHead)
	if gap < 1 {
		leftHead = truncateRunes(leftHead, max(1, width-lipgloss.Width(rightHead)-1))
		gap = width - lipgloss.Width(leftHead) - lipgloss.Width(rightHead)
		if gap < 1 {
			gap = 1
		}
	}
	head := leftHead + strings.Repeat(" ", gap) + rightHead
	if lipgloss.Width(head) > width {
		head = truncateRunes(head, width)
	}
	var parts []string
	parts = append(parts, head)
	if body != "" {
		parts = append(parts, body)
	}
	if hint != "" {
		foot := hintStyle.Width(width).Render(truncateRunes(hint, width))
		parts = append(parts, foot)
	}
	content := keepBackground(strings.Join(parts, "\n"), theme.ColorSurface())
	return lipgloss.NewStyle().
		Background(theme.ColorSurface()).
		Width(width).
		MaxWidth(width).
		Render(content)
}

func (m Model) commitDrawerTop() int {
	if !m.commitDrawerVisible() {
		return 0
	}
	drawerH := lipgloss.Height(m.commitDrawerView(m.width))
	bot := lipgloss.Height(m.composerBlockSansCommitDrawer())
	if m.err != "" {
		bot += 1 + lipgloss.Height(errStyle.Width(max(minPaneWidth, m.width)).Render(m.err))
	}
	return max(0, m.height-bot-drawerH-1)
}

func (m Model) commitDrawerHeight() int {
	if !m.commitDrawerVisible() {
		return 0
	}
	return lipgloss.Height(m.commitDrawerView(m.width))
}

func (m Model) composerBlockSansCommitDrawer() string {
	var parts []string
	if m.compactHint != "" && !m.busy {
		parts = append(parts, hintStyle.Render(truncateRunes(m.compactHint, max(minPaneWidth, m.width))))
	}
	if m.showLiveStatus() && !m.liveStatusInSubagentDrawer() {
		parts = append(parts, m.liveStatusView())
	}
	parts = append(parts, m.promptLine())
	if len(parts) == 1 {
		return parts[0]
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m Model) commitDrawerCloseRect() (x0, y, x1 int, ok bool) {
	if !m.commitDrawerVisible() {
		return 0, 0, 0, false
	}
	paint := m.commitDrawerView(m.width)
	for i, line := range strings.Split(m.chatScreen(), "\n") {
		plain := ansi.Strip(line)
		if !strings.Contains(plain, commitDrawerTitle) || !strings.Contains(plain, "[x]") {
			continue
		}
		if drawerH := lipgloss.Height(paint); i < m.commitDrawerTop() || i >= m.commitDrawerTop()+drawerH {
			continue
		}
		start, end, found := displaySpan(plain, "[x]")
		if !found {
			continue
		}
		return max(0, start-1), i, end + 1, true
	}
	for i, line := range strings.Split(paint, "\n") {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "[x]") {
			if start, end, found := displaySpan(plain, "[x]"); found {
				return max(0, start-1), m.commitDrawerTop() + i, end + 1, true
			}
		}
	}
	return 0, 0, 0, false
}

func (m Model) commitDrawerActionRect() (x0, y, x1 int, ok bool) {
	if !m.commitDrawerVisible() {
		return 0, 0, 0, false
	}
	label := "commit and push"
	for i, line := range strings.Split(m.chatScreen(), "\n") {
		plain := strings.ToLower(ansi.Strip(line))
		if !strings.Contains(plain, label) {
			continue
		}
		if plain == label || strings.Contains(plain, "[ commit and push ]") {
			start, end, found := displaySpan(plain, "commit and push")
			if found {
				return max(0, start-1), i, end + 1, true
			}
			w := lipgloss.Width(line)
			return 0, i, w, true
		}
	}
	return 0, 0, 0, false
}

func (m Model) commitDrawerIndexAtScreenY(y int) (int, bool) {
	if !m.commitDrawerVisible() {
		return -1, false
	}
	top := m.commitDrawerTop()
	files := m.commitDrawerFiles()
	if len(files) == 0 {
		return -1, false
	}
	rel := y - top - 1
	if rel < 0 {
		return -1, false
	}
	visibleRows := min(len(files), commitDrawerMaxRows)
	if rel >= visibleRows {
		return -1, false
	}
	selected := m.commitDrawerSelected
	start := 0
	if selected >= visibleRows {
		start = selected - visibleRows + 1
	}
	idx := start + rel
	if idx < 0 || idx >= len(files) {
		return -1, false
	}
	return idx, true
}

func (m Model) handleCommitDrawerKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if !m.commitDrawerVisible() {
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.pushPromptUntil = time.Time{}
		m.layout = layoutSnap{}
		return m, nil
	case "up", "k":
		if m.commitDrawerActionFocused {
			m.commitDrawerActionFocused = false
			return m.resetCommitDrawerTimer()
		}
		if m.commitDrawerSelected > 0 {
			m.commitDrawerSelected--
			return m.resetCommitDrawerTimer()
		}
		return m.resetCommitDrawerTimer()
	case "down", "j":
		files := m.commitDrawerFiles()
		if m.commitDrawerSelected < len(files)-1 {
			m.commitDrawerSelected++
			m.commitDrawerActionFocused = false
			return m.resetCommitDrawerTimer()
		}
		if len(files) > 0 {
			m.commitDrawerActionFocused = true
			return m.resetCommitDrawerTimer()
		}
		return m.resetCommitDrawerTimer()
	case "enter":
		if m.commitDrawerActionFocused {
			return m.activateCommitPush()
		}
		if m.commitDrawerSelected >= 0 && m.commitDrawerSelected < len(m.commitDrawerFiles()) {
			m = m.openCommitDiffDetail()
			if m.commitDiffDetailMode {
				return m.resetCommitDrawerTimer()
			}
		}
		return m.resetCommitDrawerTimer()
	}
	return m, nil
}

func (m Model) resetCommitDrawerTimer() (Model, tea.Cmd) {
	if !m.commitDrawerVisible() {
		return m, nil
	}
	m.pushPromptUntil = time.Now().Add(commitActionLifetime)
	return m, m.scheduleCommitPushExpiry()
}

func (m Model) commitDrawerHit(x, y int, button tea.MouseButton) (Model, tea.Cmd, bool) {
	if !m.commitDrawerVisible() {
		return m, nil, false
	}
	if button != tea.MouseLeft && button != tea.MouseRight {
		return m, nil, false
	}
	if x0, cy, x1, ok := m.commitDrawerCloseRect(); ok && y == cy && x >= x0 && x < x1 {
		m.pushPromptUntil = time.Time{}
		m.layout = layoutSnap{}
		return m, nil, true
	}
	if x0, cy, x1, ok := m.commitDrawerActionRect(); ok && y == cy && x >= x0 && x < x1 {
		nm, cmd := m.activateCommitPush()
		return nm, cmd, true
	}
	if idx, ok := m.commitDrawerIndexAtScreenY(y); ok {
		m.commitDrawerSelected = idx
		m.commitDrawerActionFocused = false
		m = m.openCommitDiffDetail()
		nm, cmd := m.resetCommitDrawerTimer()
		m = nm
		return m, cmd, true
	}
	top := m.commitDrawerTop()
	h := m.commitDrawerHeight()
	if y >= top && y < top+h {
		nm, cmd := m.resetCommitDrawerTimer()
		return nm, cmd, true
	}
	return m, nil, false
}

func (m Model) pointerInCommitDrawer(y int) bool {
	if !m.commitDrawerVisible() {
		return false
	}
	top := m.commitDrawerTop()
	h := m.commitDrawerHeight()
	return y >= top && y < top+h
}
