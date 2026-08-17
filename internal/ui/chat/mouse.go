package chat

import (
	"math"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// jumpBarRow is the screen row of the jump-to-latest icon above the input
// box: the row right after the transcript lines.
func (m Model) jumpBarRow() int {
	return m.transcriptTop() + m.transcriptRenderHeight()
}

// mousePress starts a scrollbar drag when the click lands on a scrollbar
// column, and jumps the viewport to the clicked position.
func (m Model) mousePress(msg tea.MouseClickMsg) (Model, tea.Cmd) {
	mu := msg.Mouse()
	m.copyNotice = ""
	if m.settingsMode {
		if next, cmd, hit := m.settingsHit(mu.X, mu.Y, mu.Button); hit {
			return next, cmd
		}
	}
	// Full-screen sub-agent log card: [x] closes back to the drawer list.
	if m.subagentLogMode {
		if next, cmd, hit := m.subagentLogHit(mu.X, mu.Y, mu.Button); hit {
			return next, cmd
		}
	}
	if mu.Button == tea.MouseLeft && m.jumpBarVisible() && mu.Y == m.jumpBarRow() {
		m = m.clearTextSelection()
		m.transcript.GotoBottom()
		return m, nil
	}
	// Medium-style user-turn rail: jump to that "you" message.
	if mu.Button == tea.MouseLeft {
		if idx, ok := m.userNavIndexAtScreen(mu.X, mu.Y); ok {
			m = m.clearTextSelection()
			return m.jumpToUserTurn(idx)
		}
	}
	if mu.Button == tea.MouseLeft && m.sessionPickerMode {
		if x0, y, x1, ok := m.sessionCloseRect(); ok && mu.Y == y && mu.X >= x0 && mu.X < x1 {
			return m.closeSessionPicker(), nil
		}
		if idx, ok := m.sessionIndexAtScreenY(mu.Y); ok {
			sess := m.sessionItems[idx]
			m = m.closeSessionPicker()
			return m.loadSession(&sess), nil
		}
		return m, nil
	}
	if mu.Button == tea.MouseLeft && m.subagentPickerMode && !m.subagentLogMode {
		if idx, ok := m.subagentIndexAtScreenY(mu.Y); ok {
			m.subagentCursor = idx
			return m.openSelectedSubagentLog()
		}
	}
	if !m.pickerMode && !m.slashMode && !m.subagentPickerMode {
		if left, top, right, bottom, ok := m.subsStatusRect(); ok && mu.X >= left && mu.X < right && mu.Y >= top && mu.Y < bottom {
			m = m.clearTextSelection()
			return m.openSubagentPicker(), nil
		}
		if left, top, right, bottom, ok := m.variantStatusRect(); ok && mu.X >= left && mu.X < right && mu.Y >= top && mu.Y < bottom {
			m = m.clearTextSelection()
			return m.openVariantPicker(), nil
		}
		if left, top, right, bottom, ok := m.modelStatusRect(); ok && mu.X >= left && mu.X < right && mu.Y >= top && mu.Y < bottom {
			m = m.clearTextSelection()
			return m.openPicker(), nil
		}
		if m.helpMode {
			if x0, y, x1, ok := m.helpCloseRect(); ok && mu.Y == y && mu.X >= x0 && mu.X < x1 {
				m.helpMode = false
				return m, nil
			}
		}
		if panel := m.todoPanelView(); panel != "" && mu.Y >= lipgloss.Height(m.headerView()) && mu.Y < m.transcriptTop() {
			return m.toggleTodos(), nil
		}
	}
	for _, target := range []int{0, 1} {
		if m.dragTarget == 1 && target == 0 {
			continue
		}
		if !m.pickerMode && target == 1 {
			continue
		}
		top, bottom, col, ok := m.scrollbarRect(target)
		if !ok || mu.X != col || mu.Y < top || mu.Y >= bottom {
			continue
		}
		m = m.clearTextSelection()
		m = m.applyJump(target, mu.Y)
		m.dragTarget = target
		m.dragOn = true
		return m, nil
	}
	if mu.Button == tea.MouseLeft && m.pickerMode {
		if idx, ok := m.pickerIndexAtScreenY(mu.Y); ok {
			return m.selectPickerItem(idx)
		}
	}
	if mu.Button == tea.MouseLeft && m.slashMode {
		if idx, ok := m.slashIndexAtScreenY(mu.Y); ok {
			return m.activateSlashItem(idx)
		}
	}
	if mu.Button == tea.MouseLeft {
		if idx, ok := m.itemIndexAtScreenY(mu.Y); ok {
			kind := m.items[idx].kind
			if kind == itemTool || kind == itemReasoning {
				m = m.clearTextSelection()
				m.selectedItem = idx
				if kind == itemTool {
					m.lastTool = idx
				}
				return m.toggleSelectedMeta(), nil
			}
		}
		if pos, ok := m.transcriptPosition(mu); ok {
			m.selection = textSelection{
				anchor:   pos,
				focus:    pos,
				active:   true,
				dragging: true,
			}
			m.syncTranscript()
		}
	}
	return m, nil
}

// activateSlashItem runs the slash command at idx, same as pressing enter.
func (m Model) activateSlashItem(idx int) (Model, tea.Cmd) {
	if idx < 0 || idx >= len(m.slashItems) {
		return m, nil
	}
	name := m.slashItems[idx].name
	m.slashMode = false
	m.slashCursor = 0
	m.slashFromPaste = false
	m.prompt.SetValue("")
	m.promptUndo = nil
	return m.runSlash(name)
}

// mouseDrag keeps the viewport following the pointer while a scrollbar
// drag is in progress.
func (m Model) mouseDrag(msg tea.MouseMotionMsg) Model {
	mu := msg.Mouse()
	top, bottom, col, ok := m.scrollbarRect(m.dragTarget)
	if !ok || mu.X != col {
		return m
	}
	if mu.Y < top {
		mu.Y = top
	}
	if mu.Y >= bottom {
		mu.Y = bottom - 1
	}
	return m.applyJump(m.dragTarget, mu.Y)
}

// applyJump scrolls the target viewport so the scrollbar position matches
// the given pointer row within the scrollbar column.
func (m Model) applyJump(target, y int) Model {
	var vp *viewport.Model
	if target == 0 {
		vp = &m.transcript
	} else {
		vp = &m.pickerVp
	}
	top, _, _, _ := m.scrollbarRect(target)
	height := vp.Height()
	maxY := vp.TotalLineCount() - height
	if maxY <= 0 {
		return m
	}
	frac := float64(y-top) / float64(max(1, height-1))
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	vp.SetYOffset(int(math.Round(frac * float64(maxY))))
	return m
}

func (m Model) transcriptTop() int {
	// headerView has no trailing newline; the \n after it in chatScreen
	// only ends that row, it does not insert a blank line.
	top := lipgloss.Height(m.headerView())
	if panel := m.todoPanelView(); panel != "" {
		top += 1 + lipgloss.Height(panel)
	}
	return top
}

func (m Model) transcriptPosition(mu tea.Mouse) (textPosition, bool) {
	if m.pickerMode || m.slashMode {
		return textPosition{}, false
	}
	// Drawer is a strip above the prompt; transcript above it stays interactive.
	if m.subagentPickerMode && !m.subagentLogMode && m.pointerInSubagentDrawer(mu.Y) {
		return textPosition{}, false
	}
	top := m.transcriptTop()
	height := m.transcriptRenderHeight()
	if mu.Y < top || mu.Y >= top+height || mu.X < 0 || mu.X >= m.transcript.Width() {
		return textPosition{}, false
	}
	rows := m.plainTranscriptRows()
	vp := m.paintedTranscript()
	row := mu.Y - top + vp.YOffset()
	if row < 0 || row >= len(rows) {
		return textPosition{}, false
	}
	col := mu.X + m.transcript.XOffset()
	return textPosition{row: row, col: min(col, lipgloss.Width(rows[row]))}, true
}

// itemIndexAtScreenY maps a screen row to a transcript item, walking
// rendered item heights from the header offset and viewport YOffset.
func (m Model) itemIndexAtScreenY(y int) (int, bool) {
	if m.pickerMode || m.slashMode {
		return -1, false
	}
	top := m.transcriptTop()
	height := m.transcriptRenderHeight()
	if y < top || y >= top+height {
		return -1, false
	}
	vp := m.paintedTranscript()
	target := y - top + vp.YOffset()
	if target < 0 {
		return -1, false
	}
	rendered := m.renderedItems()
	row := 0
	ri := 0
	for i, it := range m.items {
		if i > 0 && (it.kind == itemUser || it.kind == itemAssistant) {
			if ri < len(rendered) && isTurnGap(rendered[ri]) {
				if target == row {
					return -1, false
				}
				row++
				ri++
			}
		}
		if ri >= len(rendered) {
			break
		}
		h := lipgloss.Height(rendered[ri])
		if h < 1 {
			h = 1
		}
		if target >= row && target < row+h {
			return i, true
		}
		row += h
		ri++
	}
	return -1, false
}

func isTurnGap(s string) bool {
	t := strings.TrimSpace(ansi.Strip(s))
	return t == "" || t == workBracket || t == workRail
}

// slashIndexAtScreenY maps a screen row to a visible slash command.
// The menu is full width; only Y is used. The top border is skipped.
func (m Model) slashIndexAtScreenY(y int) (int, bool) {
	if !m.slashMode || len(m.slashItems) == 0 {
		return -1, false
	}
	top := m.transcriptTop() + m.transcriptRenderHeight() + 1
	inner := y - top - 1
	if inner < 0 || inner >= len(m.slashItems) {
		return -1, false
	}
	return inner, true
}

// pickerIndexAtScreenY maps a screen row to a visible model in the drawer.
func (m Model) pickerIndexAtScreenY(y int) (int, bool) {
	if !m.pickerMode || len(m.pickerItems) == 0 {
		return -1, false
	}
	top := m.pickerDrawerTop() + 1
	visible := y - top
	if visible < 0 || visible >= m.pickerVPHeight() {
		return -1, false
	}
	idx := visible + m.pickerVp.YOffset()
	if idx < 0 || idx >= len(m.pickerItems) {
		return -1, false
	}
	return idx, true
}

// sessionIndexAtScreenY maps a screen row to a visible session in the
// centered resume card, skipping group headers and the viewport offset.
func (m Model) sessionIndexAtScreenY(y int) (int, bool) {
	if !m.sessionPickerMode || len(m.sessionItems) == 0 {
		return -1, false
	}
	inner := y - m.sessionListScreenTop()
	if inner < 0 || inner >= m.sessionVPHeight() {
		return -1, false
	}
	return m.sessionIndexAtDisplayRow(inner + m.sessionVp.YOffset())
}

// sessionListScreenTop is the first session-list row on the painted
// screen: the line after the RUNS header. Falling back to the centered
// card formula keeps hit-testing usable if the header is off-screen.
func (m Model) sessionListScreenTop() int {
	for i, line := range strings.Split(m.sessionPickerScreen(), "\n") {
		if strings.Contains(ansi.Strip(line), "RUNS") {
			// Header is followed by a blank row, then the list.
			return i + 2
		}
	}
	vpH := m.sessionVPHeight()
	return max(0, (m.height-(vpH+sessionCardChromeRows))/centerDiv) + sessionCardHeaderRows
}

// sessionIndexAtDisplayRow maps a content row (group headers occupy rows
// too) back to a session index. A group header row reports not-found.
func (m Model) sessionIndexAtDisplayRow(row int) (int, bool) {
	r := 0
	last := ""
	for i, sess := range m.sessionItems {
		g := sessionAgeGroup(sess.TimeUpdated)
		if g != last {
			last = g
			if r == row {
				return -1, false
			}
			r++
		}
		if r == row {
			return i, true
		}
		r++
	}
	return -1, false
}

func (m Model) updateTextSelection(msg tea.MouseMotionMsg) Model {
	if pos, ok := m.transcriptPosition(msg.Mouse()); ok {
		m.selection.focus = pos
		m.syncTranscript()
	}
	return m
}

func (m Model) clearTextSelection() Model {
	if !m.selection.active {
		return m
	}
	m.selection = textSelection{}
	m.syncTranscript()
	return m
}

func clearCopyNotice() tea.Cmd {
	return tea.Tick(copyNoticeDuration, func(time.Time) tea.Msg {
		return copyNoticeMsg{}
	})
}

func (s textSelection) hasRange() bool {
	if !s.active {
		return false
	}
	start, end := s.bounds()
	return start != end
}

func (s textSelection) bounds() (textPosition, textPosition) {
	if s.anchor.row < s.focus.row || (s.anchor.row == s.focus.row && s.anchor.col <= s.focus.col) {
		return s.anchor, s.focus
	}
	return s.focus, s.anchor
}

func (m Model) selectedText() (string, bool) {
	if !m.selection.hasRange() {
		return "", false
	}
	rows := m.plainTranscriptRows()
	start, end := m.selection.bounds()
	if start.row < 0 || end.row >= len(rows) {
		return "", false
	}
	selected := make([]string, 0, end.row-start.row+1)
	for row := start.row; row <= end.row; row++ {
		from := 0
		to := lipgloss.Width(rows[row])
		if row == start.row {
			from = start.col
		}
		if row == end.row {
			to = end.col
		}
		// Keep on-screen rails/brackets for layout; strip them from the
		// clipboard so drag-copy yields plain message text.
		selected = append(selected, stripTranscriptChrome(ansi.Cut(rows[row], from, to)))
	}
	return strings.Join(selected, "\n"), true
}

// stripTranscriptChrome removes left layout markers (work rail, user frame
// curls) from a copied transcript slice. The markers stay visible in the TUI.
func stripTranscriptChrome(line string) string {
	for _, p := range []string{workRail + " ", "╭ ", "╰ "} {
		if strings.HasPrefix(line, p) {
			return strings.TrimPrefix(line, p)
		}
	}
	if line == workRail {
		return ""
	}
	return line
}
