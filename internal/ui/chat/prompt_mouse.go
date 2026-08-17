package chat

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mattn/go-runewidth"

	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

// promptSelection is a mouse-driven range inside the composer textarea.
// Offsets are rune indexes into prompt.Value() (newlines count as one rune).
type promptSelection struct {
	active   bool
	dragging bool
	anchor   int
	focus    int
	// Hit-test geometry captured on press so drag motion stays cheap and stable.
	hitLeft, hitTop, hitW, hitH int
	hitOK                       bool
}

func (s promptSelection) hasRange() bool {
	return s.active && s.anchor != s.focus
}

func (s promptSelection) bounds() (start, end int) {
	if s.anchor <= s.focus {
		return s.anchor, s.focus
	}
	return s.focus, s.anchor
}

// promptContentWidth is the editable column count used by paint and hit-testing.
func (m Model) promptContentWidth() int {
	innerW := max(minPaneWidth, m.width-2)
	return max(minPaneWidth, innerW-2)
}

// promptBoxMetrics returns the editable text rectangle (inside border, above footer).
// Y is anchored to the painted composer footer row ("enter send …") so it stays
// aligned with the real box regardless of pad/trailing-newline quirks.
func (m Model) promptBoxMetrics() (left, top, width, height int) {
	w := m.promptContentWidth()
	h := m.promptHeight()
	if h < 1 {
		h = 1
	}
	left = 1
	// Footer is the row immediately under the last text row.
	if _, footerY, ok := m.composerFooterPlainLine(); ok {
		top = footerY - h
		if top < 0 {
			top = 0
		}
		return left, top, w, h
	}
	// Fallback without a paint pass: last h+2 rows above the bottom edge
	// (text + footer + bottom border), text starts one row under the top border.
	// height layout: …, topBorder, text…, footer, bottomBorder [, pad]
	top = max(0, m.height-2-h-1) // leave bottom border + footer under text
	return left, top, w, h
}

func (m Model) pointerInPromptText(x, y int) bool {
	left, top, w, h := m.promptHitRect()
	return y >= top && y < top+h && x >= left && x < left+w
}

// promptHitRect uses drag-captured geometry when available (stable + fast).
func (m Model) promptHitRect() (left, top, w, h int) {
	if m.promptSel.hitOK && (m.promptSel.dragging || m.promptSel.active) {
		return m.promptSel.hitLeft, m.promptSel.hitTop, m.promptSel.hitW, m.promptSel.hitH
	}
	return m.promptBoxMetrics()
}

type promptVisLine struct {
	runes []rune
	start int
}

func (m Model) promptVisualLines() []promptVisLine {
	w := m.promptContentWidth()
	if w < 1 {
		w = 1
	}
	value := m.prompt.Value()
	if value == "" {
		return []promptVisLine{{runes: nil, start: 0}}
	}
	var out []promptVisLine
	off := 0
	parts := strings.Split(value, "\n")
	for i, logical := range parts {
		runes := []rune(logical)
		segs := hardWrapRunes(runes, w)
		if len(segs) == 0 {
			segs = [][]rune{{}}
		}
		segOff := 0
		for _, seg := range segs {
			out = append(out, promptVisLine{runes: seg, start: off + segOff})
			segOff += len(seg)
		}
		off += len(runes)
		if i < len(parts)-1 {
			off++
		}
	}
	if len(out) == 0 {
		return []promptVisLine{{runes: nil, start: 0}}
	}
	return out
}

func (m Model) promptOffsetAtScreen(x, y int) (int, bool) {
	left, top, w, h := m.promptHitRect()
	valueLen := utf8.RuneCountInString(m.prompt.Value())
	dragging := m.promptSel.dragging

	if y < top {
		if dragging {
			return 0, true
		}
		return 0, false
	}
	if y >= top+h {
		if dragging {
			return valueLen, true
		}
		return 0, false
	}

	viewRow := y - top
	viewCol := x - left
	if viewCol < 0 {
		if dragging {
			viewCol = 0
		} else if x < left-1 {
			return 0, false
		} else {
			viewCol = 0
		}
	}
	if viewCol > w {
		viewCol = w
	}

	vis := m.promptVisualLines()
	yOff := m.promptScrollOffset(len(vis), h)
	idx := yOff + viewRow
	if idx < 0 {
		idx = 0
	}
	if idx >= len(vis) {
		return valueLen, true
	}
	line := vis[idx]
	col := displayColToRunes(line.runes, viewCol)
	if col > len(line.runes) {
		col = len(line.runes)
	}
	return line.start + col, true
}

func (m Model) promptScrollOffset(total, viewH int) int {
	if total <= viewH {
		return 0
	}
	off := m.prompt.ScrollYOffset()
	if off < 0 {
		off = 0
	}
	if maxOff := total - viewH; off > maxOff {
		off = maxOff
	}
	return off
}

func promptLogicalAtOffset(value string, offset int) (row, col int) {
	if offset < 0 {
		offset = 0
	}
	runes := []rune(value)
	if offset > len(runes) {
		offset = len(runes)
	}
	for i := 0; i < offset; i++ {
		if runes[i] == '\n' {
			row++
			col = 0
			continue
		}
		col++
	}
	return row, col
}

func (m Model) setPromptCursorOffset(offset int) Model {
	w := m.promptContentWidth()
	if m.prompt.Width() != w {
		m.prompt.SetWidth(w)
	}
	value := m.prompt.Value()
	row, col := promptLogicalAtOffset(value, offset)
	nLines := m.prompt.LineCount()
	if nLines <= 0 {
		return m
	}
	if row >= nLines {
		row = nLines - 1
	}
	if row < 0 {
		row = 0
	}
	m.prompt.MoveToBegin()
	guard := 0
	for m.prompt.Line() < row && guard < 10000 {
		before := m.prompt.Line()
		m.prompt.CursorEnd()
		m.prompt.CursorDown()
		if m.prompt.Line() <= before {
			break
		}
		guard++
	}
	for m.prompt.Line() > row && guard < 20000 {
		m.prompt.CursorUp()
		guard++
	}
	m.prompt.SetCursorColumn(col)
	return m
}

func hardWrapRunes(runes []rune, width int) [][]rune {
	if width < 1 {
		width = 1
	}
	if len(runes) == 0 {
		return [][]rune{{}}
	}
	var lines [][]rune
	var cur []rune
	curW := 0
	for _, r := range runes {
		rw := runewidth.RuneWidth(r)
		if rw < 1 {
			rw = 1
		}
		if curW+rw > width && len(cur) > 0 {
			lines = append(lines, cur)
			cur = nil
			curW = 0
		}
		if rw > width && len(cur) == 0 {
			lines = append(lines, []rune{r})
			continue
		}
		cur = append(cur, r)
		curW += rw
	}
	if len(cur) > 0 || len(lines) == 0 {
		lines = append(lines, cur)
	}
	return lines
}

func displayColToRunes(seg []rune, displayCol int) int {
	if displayCol <= 0 {
		return 0
	}
	w := 0
	for i, r := range seg {
		rw := runewidth.RuneWidth(r)
		if rw < 1 {
			rw = 1
		}
		if w+rw > displayCol {
			return i
		}
		w += rw
		if w == displayCol {
			return i + 1
		}
	}
	return len(seg)
}

func (m Model) mousePressPrompt(mu tea.Mouse) (Model, tea.Cmd, bool) {
	if mu.Button != tea.MouseLeft {
		return m, nil, false
	}
	if m.confirmMode || m.askMode || m.helpMode || m.usageMode || m.settingsMode || m.filePickerMode ||
		m.pickerMode || m.sessionPickerMode || m.subagentLogMode {
		return m, nil, false
	}
	w := m.promptContentWidth()
	if m.prompt.Width() != w {
		m.prompt.SetWidth(w)
		m.prompt.SetHeight(m.promptHeight())
	}
	// Capture geometry once for the whole press/drag.
	left, top, pw, h := m.promptBoxMetrics()
	if !(mu.Y >= top && mu.Y < top+h && mu.X >= left && mu.X < left+pw) {
		return m, nil, false
	}
	m.promptSel.hitLeft, m.promptSel.hitTop = left, top
	m.promptSel.hitW, m.promptSel.hitH = pw, h
	m.promptSel.hitOK = true

	off, ok := m.promptOffsetAtScreen(mu.X, mu.Y)
	if !ok {
		m.promptSel.hitOK = false
		return m, nil, false
	}
	m = m.clearTextSelection()
	m.promptSelectAll = false
	m = m.setPromptCursorOffset(off)
	m.promptSel.active = true
	m.promptSel.dragging = true
	m.promptSel.anchor = off
	m.promptSel.focus = off
	_ = m.prompt.Focus()
	return m, nil, true
}

func (m Model) updatePromptSelection(mu tea.Mouse) Model {
	if !m.promptSel.dragging {
		return m
	}
	// Fast path: only update focus offset. No caret moves, no full layout.
	off, ok := m.promptOffsetAtScreen(mu.X, mu.Y)
	if !ok {
		return m
	}
	m.promptSel.focus = off
	m.promptSel.active = true
	return m
}

func (m Model) clearPromptSelection() Model {
	if !m.promptSel.active && !m.promptSel.dragging && !m.promptSel.hitOK {
		return m
	}
	m.promptSel = promptSelection{}
	return m
}

func (m Model) endPromptSelectionDrag() (Model, tea.Cmd) {
	if !m.promptSel.dragging {
		return m, nil
	}
	m.promptSel.dragging = false
	m = m.setPromptCursorOffset(m.promptSel.focus)
	if !m.promptSel.hasRange() {
		m.promptSel = promptSelection{}
		return m, nil
	}
	// Keep hitOK false after release so later geometry refreshes.
	m.promptSel.hitOK = false
	text, ok := m.selectedPromptText()
	if !ok {
		return m, nil
	}
	m.copyNotice = "Text copied"
	return m, tea.Batch(tea.SetClipboard(text), clearCopyNotice())
}

func (m Model) selectedPromptText() (string, bool) {
	if m.promptSelectAll {
		s := m.prompt.Value()
		if s == "" {
			return "", false
		}
		return s, true
	}
	if !m.promptSel.hasRange() {
		return "", false
	}
	start, end := m.promptSel.bounds()
	runes := []rune(m.prompt.Value())
	if start < 0 {
		start = 0
	}
	if end > len(runes) {
		end = len(runes)
	}
	if start >= end {
		return "", false
	}
	return string(runes[start:end]), true
}

// promptBodyPaint draws editable lines with the same hard-wrap as hit-testing.
func (m Model) promptBodyPaint(width, height int) string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	// Placeholder when empty (matches prior textarea View behavior).
	if strings.TrimSpace(m.prompt.Value()) == "" && !m.promptSel.dragging && !m.promptSel.hasRange() && !m.promptSelectAll {
		if m.prompt.LineCount() <= 1 && m.prompt.Column() == 0 {
			ph := m.prompt.Placeholder
			if ph == "" {
				ph = "ask lazykoder"
			}
			mute := lipgloss.NewStyle().Background(theme.ColorBg()).Foreground(theme.ColorMute())
			plain := lipgloss.NewStyle().Background(theme.ColorBg()).Foreground(theme.ColorText())
			// Show placeholder on first row; caret at col 0.
			line := selectionStyle.Render(" ") + mute.Render(ph)
			pad := width - 1 - lipgloss.Width(ph)
			if pad > 0 {
				line += plain.Render(strings.Repeat(" ", pad))
			}
			var b strings.Builder
			b.WriteString(line)
			for row := 1; row < height; row++ {
				b.WriteByte('\n')
				b.WriteString(plain.Render(strings.Repeat(" ", width)))
			}
			return b.String()
		}
	}

	vis := m.promptVisualLines()
	yOff := m.promptScrollOffset(len(vis), height)

	selStart, selEnd := -1, -1
	if m.promptSel.hasRange() || (m.promptSel.dragging && m.promptSel.anchor != m.promptSel.focus) {
		selStart, selEnd = m.promptSel.bounds()
	} else if m.promptSelectAll {
		selStart, selEnd = 0, utf8.RuneCountInString(m.prompt.Value())
	}

	caret := -1
	if m.promptSel.dragging {
		caret = m.promptSel.focus
	} else {
		caret = promptOffsetFromCursor(m)
	}

	plain := lipgloss.NewStyle().Background(theme.ColorBg()).Foreground(theme.ColorText())
	var b strings.Builder
	for row := 0; row < height; row++ {
		if row > 0 {
			b.WriteByte('\n')
		}
		idx := yOff + row
		if idx < 0 || idx >= len(vis) {
			b.WriteString(plain.Render(strings.Repeat(" ", width)))
			continue
		}
		b.WriteString(paintPromptVisLine(vis[idx], width, selStart, selEnd, caret, plain))
	}
	return b.String()
}

func promptOffsetFromCursor(m Model) int {
	value := m.prompt.Value()
	row := m.prompt.Line()
	col := m.prompt.Column()
	parts := strings.Split(value, "\n")
	if len(parts) == 0 {
		return 0
	}
	if row < 0 {
		row = 0
	}
	if row >= len(parts) {
		return utf8.RuneCountInString(value)
	}
	off := 0
	for i := 0; i < row; i++ {
		off += utf8.RuneCountInString(parts[i]) + 1
	}
	lineRunes := utf8.RuneCountInString(parts[row])
	if col < 0 {
		col = 0
	}
	if col > lineRunes {
		col = lineRunes
	}
	return off + col
}

func paintPromptVisLine(line promptVisLine, width, selStart, selEnd, caret int, plain lipgloss.Style) string {
	runes := line.runes
	var b strings.Builder
	drawnW := 0
	for i, r := range runes {
		at := line.start + i
		rw := runewidth.RuneWidth(r)
		if rw < 1 {
			rw = 1
		}
		style := plain
		if selStart >= 0 && at >= selStart && at < selEnd {
			style = selectionStyle
		}
		if caret == at {
			style = selectionStyle
		}
		b.WriteString(style.Render(string(r)))
		drawnW += rw
	}
	if caret == line.start+len(runes) {
		b.WriteString(selectionStyle.Render(" "))
		drawnW++
	}
	if pad := width - drawnW; pad > 0 {
		b.WriteString(plain.Render(strings.Repeat(" ", pad)))
	}
	return b.String()
}
