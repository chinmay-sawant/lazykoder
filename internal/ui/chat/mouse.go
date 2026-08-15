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

// mousePress starts a scrollbar drag when the click lands on a scrollbar
// column, and jumps the viewport to the clicked position.
func (m Model) mousePress(msg tea.MouseClickMsg) Model {
	mu := msg.Mouse()
	m.copyNotice = ""
	if !m.pickerMode && !m.slashMode {
		if _, top, right, bottom, ok := m.modelStatusRect(); ok && mu.X < right && mu.Y >= top && mu.Y < bottom {
			m = m.clearTextSelection()
			return m.openPicker()
		}
	}
	if m.pickerMode {
		if x, y, ok := m.pickerCloseRect(); ok && mu.X == x && mu.Y == y {
			return m.closePicker()
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
		return m
	}
	if mu.Button == tea.MouseLeft {
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
	return m
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

func (m Model) transcriptPosition(mu tea.Mouse) (textPosition, bool) {
	if m.pickerMode || m.slashMode {
		return textPosition{}, false
	}
	top := titleBlockRows
	height := m.transcriptRenderHeight()
	if mu.Y < top || mu.Y >= top+height || mu.X < 0 || mu.X >= m.transcript.Width() {
		return textPosition{}, false
	}
	rows := m.plainTranscriptRows()
	row := mu.Y - top + m.transcript.YOffset()
	if row < 0 || row >= len(rows) {
		return textPosition{}, false
	}
	col := mu.X + m.transcript.XOffset()
	return textPosition{row: row, col: min(col, lipgloss.Width(rows[row]))}, true
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
		selected = append(selected, ansi.Cut(rows[row], from, to))
	}
	return strings.Join(selected, "\n"), true
}
