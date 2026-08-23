package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

// sessionCardChromeRows are the resume-card rows outside the session list:
// top border, header, footer, bottom border.
const sessionCardChromeRows = 4

// sessionCardHeaderRows are the rows between the card top edge and the
// first session list row: the top border and the header.
const sessionCardHeaderRows = 2

// sessionCardHeightPct is the share of the terminal height the resume
// card itself occupies (borders and chrome included).
const sessionCardHeightPct = 80

// sessionPad is the horizontal padding reserved on each side of a
// session picker row.
const sessionPad = 2

// hoursPerDay is the number of hours in a day for the age label.
const hoursPerDay = 24

func (m Model) openSessionPicker() Model {
	if m.store == nil {
		m.sessionItems = nil
		m.sessionCursor = 0
		m.sessionHover = -1
		m = m.setFocus(focusSessions)
		return m.refreshSessionPicker()
	}
	sessions, err := m.store.ListSessionsByDir(context.Background(), m.workdir)
	if err != nil {
		m.err = "sessions: " + err.Error()
		return m
	}
	m.sessionItems = sessions
	m.sessionCursor = 0
	m.sessionHover = -1
	for i, sess := range sessions {
		if m.session != nil && sess.ID == m.session.ID {
			m.sessionCursor = i
			break
		}
	}
	if !m.sessionBuilt {
		m.sessionVp = viewport.New(viewport.WithWidth(pickerVpDefaultW), viewport.WithHeight(pickerVpDefaultH))
		m.sessionVp.FillHeight = true
		m.sessionBuilt = true
	}
	m = m.setFocus(focusSessions)
	return m.resizeSessionPicker().refreshSessionPicker()
}

func (m Model) closeSessionPicker() Model {
	m = m.clearFocus(focusSessions)
	m.sessionHover = -1
	return m
}

func (m Model) sessionPickerScreen() string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, m.sessionPickerView())
}

func (m Model) sessionCloseRect() (x0, y, x1 int, ok bool) {
	if !m.sessionPickerMode {
		return 0, 0, 0, false
	}
	for i, line := range strings.Split(m.sessionPickerScreen(), "\n") {
		plain := stripANSIString(line)
		if !strings.Contains(plain, "RUNS") || !strings.Contains(plain, "[x]") {
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

func (m Model) sessionPickerView() string {
	cardW := m.overlayWidth()
	innerW := max(minPaneWidth, cardW-cardBorder-2*cardPad)
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("RUNS")
	closeBtn := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("[x]")
	gap := max(1, innerW-lipgloss.Width(title)-lipgloss.Width(closeBtn))
	header := title + strings.Repeat(" ", gap) + closeBtn
	body := hintStyle.Width(innerW).Align(lipgloss.Center).Render("no sessions yet  ·  esc back")
	if len(m.sessionItems) > 0 {
		vpH := m.sessionVPHeight()
		body = withScrollbar(m.sessionVp.View(), m.sessionVp.Width(), vpH,
			m.sessionVp.ScrollPercent(), m.sessionVp.TotalLineCount() > vpH)
	}
	footer := hintStyle.Width(innerW).Render("j/k select  •  enter open  •  esc/[x] cancel")
	content := lipgloss.JoinVertical(lipgloss.Left, header, "", body, footer)
	content = keepBackground(content, theme.ColorSurface())
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorBorder()).
		BorderBackground(theme.ColorSurface()).
		Background(theme.ColorSurface()).
		Padding(1, cardPad).
		Width(cardW).
		Render(content)
}

// sessionPickerContent renders the session list with the cursor marker.
// Every session line is truncated to the viewport width so each entry
// stays on exactly one line and click targets map 1:1 to rows. The row
// under the mouse is filled with the hover highlight.
func (m Model) sessionPickerContent(width int) string {
	sel := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Background(theme.ColorBorder())
	normal := lipgloss.NewStyle().Foreground(theme.ColorMute())
	group := lipgloss.NewStyle().Foreground(theme.ColorAccent()).Bold(true)
	hover := lipgloss.NewStyle().Background(theme.ColorBorder()).Foreground(theme.ColorText()).Inline(true)
	var b strings.Builder
	lastGroup := ""
	for i, sess := range m.sessionItems {
		name := sessionAgeGroup(sess.TimeUpdated)
		if name != lastGroup {
			if lastGroup != "" {
				b.WriteString("\n")
			}
			b.WriteString(group.Render(name))
			b.WriteString("\n")
			lastGroup = name
		} else if i > 0 {
			b.WriteString("\n")
		}
		line := sessionPickerLine(sess, max(1, width-sessionPad))
		prefix := "  "
		if i == m.sessionCursor {
			prefix = "▸ "
		}
		switch {
		case i == m.sessionHover && i != m.sessionCursor:
			b.WriteString(hover.MaxWidth(width).Width(width).Render(prefix + line))
		case i == m.sessionCursor:
			b.WriteString(sel.MaxWidth(width).Width(width).Render(prefix + line))
		default:
			b.WriteString(normal.MaxWidth(width).Render(prefix + line))
		}
	}
	return b.String()
}

func sessionAgeGroup(ms int64) string {
	if ms <= 0 {
		return "older"
	}
	d := time.Since(time.UnixMilli(ms))
	switch {
	case d < time.Hour:
		return "just now"
	case d < 24*time.Hour:
		return "recently"
	default:
		return "older"
	}
}

// sessionPickerTitle is the one-line label for a session in the resume
// list. Newlines and other interior whitespace collapse so each entry
// stays on a single row and click targets stay aligned.
func sessionPickerTitle(sess db.Session) string {
	title := strings.Join(strings.Fields(sess.Title), " ")
	if title == "" {
		return "untitled"
	}
	return title
}

// sessionPickerLine renders one session row: the title on the left, and
// the model and age right-aligned. Long titles truncate with an ellipsis
// so the right side always fits.
func sessionPickerLine(sess db.Session, width int) string {
	title := sessionPickerTitle(sess)
	model := sess.Model
	if model == "" {
		model = "default"
	}
	right := model
	if age := formatSessionAge(sess.TimeUpdated); age != "" {
		right = model + "  ·  " + age
	}
	if rightW := lipgloss.Width(right); rightW > width {
		right = truncateRunes(right, width)
	}
	left := title
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < sessionPad {
		left = truncateRunes(left, max(1, width-lipgloss.Width(right)-sessionPad))
		gap = width - lipgloss.Width(left) - lipgloss.Width(right)
		if gap < 1 {
			gap = 1
		}
	}
	return left + strings.Repeat(" ", gap) + right
}

func formatSessionAge(ms int64) string {
	if ms <= 0 {
		return ""
	}
	d := time.Since(time.UnixMilli(ms))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/hoursPerDay))
	}
}

// formatClock is the transcript stamp: 24-hour time with seconds, no date.
func formatClock(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).Format("15:04:05")
}

func (m Model) sessionVPHeight() int {
	cardH := max(minPaneHeight+sessionCardChromeRows, m.height*sessionCardHeightPct/percentBase)
	return max(minPaneHeight, cardH-sessionCardChromeRows)
}

func (m Model) sessionContentRows() int {
	n := len(m.sessionItems)
	seen := map[string]struct{}{}
	for _, sess := range m.sessionItems {
		seen[sessionAgeGroup(sess.TimeUpdated)] = struct{}{}
	}
	return n + len(seen)
}

func (m Model) sessionDisplayRow(idx int) int {
	row := 0
	last := ""
	for i, sess := range m.sessionItems {
		g := sessionAgeGroup(sess.TimeUpdated)
		if g != last {
			last = g
			row++
		}
		if i == idx {
			return row
		}
		row++
	}
	return idx
}

func (m Model) resizeSessionPicker() Model {
	if !m.sessionBuilt {
		return m
	}
	innerW := max(minPaneWidth, m.overlayWidth()-cardBorder-2*cardPad)
	m.sessionVp.SetWidth(max(pickerVpMinWidth, innerW-1))
	m.sessionVp.SetHeight(m.sessionVPHeight())
	return m
}

func (m Model) refreshSessionPicker() Model {
	if !m.sessionBuilt {
		return m
	}
	m.sessionVp.SetHeight(m.sessionVPHeight())
	m.sessionVp.SetContent(m.sessionPickerContent(m.sessionVp.Width()))
	if len(m.sessionItems) > 0 {
		m.sessionVp.EnsureVisible(m.sessionDisplayRow(m.sessionCursor), 0, 1)
	}
	return m
}

// refreshSessionHover repaints the list content after the hovered row
// changes, without moving the viewport.
func (m Model) refreshSessionHover() Model {
	if !m.sessionBuilt {
		return m
	}
	m.sessionVp.SetContent(m.sessionPickerContent(m.sessionVp.Width()))
	return m
}

func (m Model) updateSessionPickerKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	if key.Mod.Contains(tea.ModCtrl) && key.Code == 'c' {
		return m.closeDone(), tea.Quit
	}
	switch key.Code {
	case 'q', 'Q', 'x', 'X', tea.KeyEscape:
		return m.closeSessionPicker(), nil
	case tea.KeyEnter:
		if m.sessionCursor >= 0 && m.sessionCursor < len(m.sessionItems) {
			sess := m.sessionItems[m.sessionCursor]
			m = m.closeSessionPicker()
			return m.loadSession(&sess), nil
		}
		return m, nil
	case 'j', tea.KeyDown:
		if m.sessionCursor < len(m.sessionItems)-1 {
			m.sessionCursor++
			m = m.refreshSessionPicker()
		}
		return m, nil
	case 'k', tea.KeyUp:
		if m.sessionCursor > 0 {
			m.sessionCursor--
			m = m.refreshSessionPicker()
		}
		return m, nil
	case tea.KeyPgDown:
		m.sessionVp.PageDown()
		return m, nil
	case tea.KeyPgUp:
		m.sessionVp.PageUp()
		return m, nil
	}
	return m, nil
}

func (m Model) loadSession(sess *db.Session) Model {
	m.items = nil
	m.selectedItem = -1
	m.lastTool = -1
	m.pendingUser = ""
	m.turnHasNewUser = false
	m.successfulRecapChats = 0
	m.inputHistory = nil
	m.historyCursor = -1
	m.historyDraft = ""
	m.pendingHistoryIndex = -1
	m.promptUndo = nil
	m.slashFromPaste = false
	m.err = ""
	m.tokensUsed = 0
	m.compacting = false
	m.compactHint = ""
	m.pendingCompactReason = ""
	m.prevModel = ""
	m.prevWindow = 0
	m.sessionCost = 0
	m.tokensPerSec = 0
	m.tpsEstimated = false
	m.cacheHit = 0
	m.cacheMiss = 0
	m.turnGenTokens = 0
	m.turnItemFrom = 0
	m.turnStarted = time.Time{}
	m.statusMode = false
	m.statusCursor = 0
	m.statusSegments = db.DefaultStatusSegments()
	m.prompt.SetValue("")
	m.session = sess
	if sess != nil {
		m.model = sess.Model
		m.variant = ""
		if sess.Variant != nil {
			m.variant = *sess.Variant
		}
		if m.store != nil {
			m.replay(sess.ID)
			m = m.loadTodos()
			m.statusSegments = db.NormalizeStatusSegments(sess.StatusSegments)
		}
	} else {
		m.todos = nil
		m.syncTranscript()
	}
	return m
}
