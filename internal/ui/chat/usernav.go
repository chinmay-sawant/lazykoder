package chat

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

// userNavTipExpireMsg hides the rail label bubble after userNavTipDuration.
type userNavTipExpireMsg struct{ gen uint64 }

const (
	// Full-size circle glyphs (not "·") so the rail stays readable.
	userNavMarkIdle   = "◯"
	userNavMarkActive = "⬤"
	userNavMarkHover  = "◉"
	userNavTooltipMax = 28
	// userNavPadCols is blank columns between timestamps and the rail ticks.
	userNavPadCols = 2
	// userNavRailCols is the far-right column reserved for the nav ticks.
	userNavRailCols = 1
	// userNavHitPad is extra rows above/below the mark cluster that still hit.
	userNavHitPad = 2
	// userNavHitXExtra allows clicks a couple columns left of the rail.
	userNavHitXExtra = 2
	// userNavTipDuration is how long the label bubble stays after show.
	userNavTipDuration = 2 * time.Second
)

// userTurnMark is one progress tick for a user message.
type userTurnMark struct {
	ItemIdx   int    // index in m.items
	ContentY  int    // start row of that item in full transcript content
	Label     string // hover / active preview (first line of user text)
	ScreenRow int    // row within the transcript viewport (0..h-1)
}

// hasUserNav reports whether the right-edge user-turn rail should appear.
func (m Model) hasUserNav() bool {
	for _, it := range m.items {
		if it.kind == itemUser {
			return true
		}
	}
	return false
}

// transcriptChromeCols is columns reserved after the transcript text.
// When the user-nav rail is present it replaces the transcript scrollbar
// (one column). Otherwise one column is reserved for the scrollbar track.
func (m Model) transcriptChromeCols() int {
	if m.hasUserNav() {
		return userNavPadCols + userNavRailCols
	}
	return 1
}

// transcriptContentWidth is the viewport text width (excludes rail or scrollbar).
func (m Model) transcriptContentWidth() int {
	return max(minPaneWidth, m.width-m.transcriptChromeCols())
}

// userTurnMarks builds right-rail ticks for every user message.
// Screen rows are even-spaced across the viewport so expanding a tool or
// growing a reply does not move the dots. ContentY is still measured from
// the live transcript so click/scroll land on the matching "you" section.
func (m Model) userTurnMarks() []userTurnMark {
	if len(m.items) == 0 {
		return nil
	}
	rendered := m.renderedItems()
	var marks []userTurnMark
	row := 0
	ri := 0
	for i, it := range m.items {
		if i > 0 && (it.kind == itemUser || it.kind == itemAssistant) {
			if ri < len(rendered) && isTurnGap(rendered[ri]) {
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
		if it.kind == itemUser {
			label := strings.TrimSpace(strings.Split(it.text, "\n")[0])
			label = strings.Join(strings.Fields(label), " ")
			if label == "" {
				label = "you"
			}
			marks = append(marks, userTurnMark{
				ItemIdx:  i,
				ContentY: row,
				Label:    label,
			})
		}
		row += h
		ri++
	}
	if len(marks) == 0 {
		return nil
	}
	vh := m.userNavStableHeight()
	if vh < 1 {
		vh = 1
	}
	placeUserNavMarks(marks, vh)
	return marks
}

// userNavStableHeight is the transcript height with the todo list collapsed.
// Expanding the checklist must not respread the rail ticks.
func (m Model) userNavStableHeight() int {
	m.todosExpanded = false
	return m.transcriptRenderHeight()
}

// userNavRailScreenTop is the first screen row of the rail: the transcript
// top while the todo list is collapsed. Extra checklist rows sit inside
// this span so ticks stay on the same screen rows.
func (m Model) userNavRailScreenTop() int {
	m.todosExpanded = false
	return m.transcriptTop()
}

// placeUserNavMarks spreads ticks evenly down the rail. Positions depend
// only on mark count and viewport height, so they stay put when content
// height changes (expand/collapse).
func placeUserNavMarks(marks []userTurnMark, viewH int) {
	n := len(marks)
	if n == 0 || viewH < 1 {
		return
	}
	if n == 1 {
		marks[0].ScreenRow = viewH / 2
		return
	}
	maxR := viewH - 1
	if maxR < 1 {
		maxR = 0
	}
	if n > viewH {
		for i := range marks {
			if i > maxR {
				marks[i].ScreenRow = maxR
			} else {
				marks[i].ScreenRow = i
			}
		}
		return
	}
	for i := range marks {
		marks[i].ScreenRow = i * maxR / (n - 1)
	}
}

func (m Model) activeUserTurnIdx(marks []userTurnMark) int {
	if len(marks) == 0 {
		return -1
	}
	vp := m.paintedTranscript()
	yOff := vp.YOffset()
	h := m.transcriptRenderHeight()
	if h < 1 {
		h = 1
	}
	if vp.AtBottom() {
		return len(marks) - 1
	}
	for i, mk := range marks {
		if mk.ContentY >= yOff && mk.ContentY < yOff+h {
			return i
		}
	}
	active := 0
	for i, mk := range marks {
		if mk.ContentY <= yOff {
			active = i
		}
	}
	return active
}

// overlayUserNavRail paints even-spaced ticks in the far-right column.
// The highlighted tick (hover, else the active section) also paints a
// label bubble so scroll and click show the same preview as hover.
func (m Model) overlayUserNavRail(view string, contentW, height int) string {
	marks := m.userTurnMarks()
	if len(marks) == 0 || height < 1 {
		return view
	}
	active := m.activeUserTurnIdx(marks)
	hover := m.userNavHover
	tipIdx := hover
	if tipIdx < 0 {
		tipIdx = m.userNavTip
	}

	lines := strings.Split(view, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}

	byRow := map[int]int{}
	for i, mk := range marks {
		byRow[mk.ScreenRow] = i
	}

	idle := lipgloss.NewStyle().Foreground(theme.ColorMute())
	hot := lipgloss.NewStyle().Foreground(theme.ColorAccent()).Bold(true)
	hoverSt := lipgloss.NewStyle().Foreground(theme.ColorText()).Bold(true)

	for y, line := range lines {
		if lipgloss.Width(line) < contentW {
			line += strings.Repeat(" ", contentW-lipgloss.Width(line))
		} else if lipgloss.Width(line) > contentW {
			line = ansi.Cut(line, 0, contentW)
			if lipgloss.Width(line) < contentW {
				line += strings.Repeat(" ", contentW-lipgloss.Width(line))
			}
		}
		mi, isMark := byRow[y]
		pad := strings.Repeat(" ", userNavPadCols)
		if !isMark {
			lines[y] = line + pad + " "
			continue
		}
		var mark string
		switch {
		case mi == hover:
			mark = hoverSt.Render(userNavMarkHover)
		case mi == active:
			mark = hot.Render(userNavMarkActive)
		default:
			mark = idle.Render(userNavMarkIdle)
		}
		lines[y] = line + pad + mark
	}

	if tipIdx >= 0 && tipIdx < len(marks) {
		mk := marks[tipIdx]
		y := mk.ScreenRow
		if y >= 0 && y < len(lines) {
			tip := truncateRunes(mk.Label, userNavTooltipMax)
			tipStyled := lipgloss.NewStyle().
				Foreground(theme.ColorBg()).
				Background(theme.ColorText()).
				Bold(true).
				Render(" " + tip + " ")
			tipW := lipgloss.Width(tipStyled)
			avail := max(0, contentW-tipW)
			left := ansi.Cut(lines[y], 0, avail)
			if lipgloss.Width(left) < avail {
				left += strings.Repeat(" ", avail-lipgloss.Width(left))
			}
			rail := hot.Render(userNavMarkActive)
			if tipIdx == hover {
				rail = hoverSt.Render(userNavMarkHover)
				if hover == active {
					rail = hot.Render(userNavMarkHover)
				}
			}
			lines[y] = left + tipStyled + strings.Repeat(" ", userNavPadCols) + rail
		}
	}
	return strings.Join(lines, "\n")
}

// userNavIndexAtScreen maps a click/hover to a user-turn mark index.
func (m Model) userNavIndexAtScreen(x, y int) (int, bool) {
	marks := m.userTurnMarks()
	if len(marks) == 0 {
		return -1, false
	}
	top := m.userNavRailScreenTop()
	h := m.userNavStableHeight()
	if y < top || y >= top+h {
		return -1, false
	}
	railCol := m.userNavRailColumn()
	if x < railCol-userNavHitXExtra || x > railCol {
		return -1, false
	}
	rel := y - top
	return userNavHitIndex(marks, rel)
}

// userNavHitIndex maps a transcript-relative row to a mark using exclusive
// midpoint bands so adjacent ticks do not steal each other's clicks.
func userNavHitIndex(marks []userTurnMark, rel int) (int, bool) {
	if len(marks) == 0 {
		return -1, false
	}
	first := marks[0].ScreenRow
	last := marks[len(marks)-1].ScreenRow
	if rel < first-userNavHitPad || rel > last+userNavHitPad {
		return -1, false
	}
	if len(marks) == 1 {
		return 0, true
	}
	for i := 0; i < len(marks)-1; i++ {
		mid := (marks[i].ScreenRow + marks[i+1].ScreenRow) / 2
		if rel <= mid {
			return i, true
		}
	}
	return len(marks) - 1, true
}

func (m Model) userNavRailColumn() int {
	return max(0, m.width-1)
}

// applyUserNavRail paints ticks onto a full-width screen starting at
// userNavRailScreenTop. Used from chatScreen so an open todo list does
// not shrink the rail or move the dots.
func (m Model) applyUserNavRail(screen string) string {
	if !m.hasUserNav() || len(m.items) == 0 {
		return screen
	}
	top := m.userNavRailScreenTop()
	h := m.userNavStableHeight()
	if h < 1 {
		return screen
	}
	lines := strings.Split(screen, "\n")
	for len(lines) < top+h {
		lines = append(lines, "")
	}
	block := strings.Join(lines[top:top+h], "\n")
	painted := m.overlayUserNavRail(block, m.transcriptContentWidth(), h)
	part := strings.Split(painted, "\n")
	copy(lines[top:], part)
	return strings.Join(lines, "\n")
}

// jumpToUserTurn scrolls the transcript so the given user item is at the top
// and keeps that mark selected so its label bubble stays visible.
func (m Model) jumpToUserTurn(markIdx int) (Model, tea.Cmd) {
	marks := m.userTurnMarks()
	if markIdx < 0 || markIdx >= len(marks) {
		return m, nil
	}
	m.transcript.SetWidth(m.transcriptContentWidth())
	m.transcript.SetHeight(max(minPaneHeight, m.transcriptRenderHeight()))
	m.userNavHover = markIdx
	m.transcript.SetYOffset(marks[markIdx].ContentY)
	m.selectedItem = marks[markIdx].ItemIdx
	return m.showUserNavTip(markIdx)
}

// showUserNavTip paints the label bubble for idx and starts the 10s hide timer.
func (m Model) showUserNavTip(idx int) (Model, tea.Cmd) {
	if idx < 0 {
		return m, nil
	}
	m.userNavTip = idx
	m.userNavTipGen++
	gen := m.userNavTipGen
	return m, tea.Tick(userNavTipDuration, func(time.Time) tea.Msg {
		return userNavTipExpireMsg{gen: gen}
	})
}

// showActiveUserNavTip shows the bubble for the section now in view.
func (m Model) showActiveUserNavTip() (Model, tea.Cmd) {
	return m.showUserNavTip(m.activeUserTurnIdx(m.userTurnMarks()))
}
