package chat

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/recap"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

const memoryHistoryPageSize = 20

type memoryHistoryItem struct {
	category string
	entry    recap.MemoryEntry
}

// openMemoryHistory opens the shared drawer shell with facts sourced only
// from the active parent session.
func (m Model) openMemoryHistory() Model {
	follow := m.transcript.AtBottom()
	m = m.setFocus(focusSubagents)
	m.memoryHistoryMode = true
	m.memoryHistoryDetailMode = false
	m.memoryHistorySelected = memoryHistoryItem{}
	m.memoryHistoryPage = 0
	m.subagentDrawerCompact = false
	m.subagentHover = -1
	m.recapSelected = false
	m.prompt.SetValue("")
	m.promptUndo = nil
	m = m.ensureSubagentBuilt()
	m = m.reloadMemoryHistory()
	m = m.resizeSubagentDrawer()
	m.syncTranscript()
	if follow {
		m.transcript.GotoBottom()
	}
	return m
}

func (m Model) reloadMemoryHistory() Model {
	items, err := m.readMemoryHistory()
	if err != nil {
		m.memoryHistoryAll = []memoryHistoryItem{}
		m.memoryHistoryItems = []memoryHistoryItem{}
		m.memoryHistoryPage = 0
		m.err = appendChatError(m.err, "memory history failed: "+err.Error())
		return m
	}
	m.memoryHistoryAll = items
	m.memoryHistoryPage = min(m.memoryHistoryPage, m.memoryHistoryPageCount()-1)
	m.memoryHistoryItems = m.memoryHistoryPageItems()
	if m.subagentCursor >= len(m.memoryHistoryItems) {
		m.subagentCursor = max(0, len(m.memoryHistoryItems)-1)
	}
	return m
}

func (m Model) readMemoryHistory() ([]memoryHistoryItem, error) {
	if m.store == nil || m.session == nil || strings.TrimSpace(m.workdir) == "" {
		return []memoryHistoryItem{}, nil
	}
	messages, err := m.store.ListMessages(context.Background(), m.session.ID)
	if err != nil {
		return nil, fmt.Errorf("list session messages: %w", err)
	}
	messageIDs := make(map[string]struct{}, len(messages))
	for _, message := range messages {
		messageIDs[message.ID] = struct{}{}
	}
	document, err := recap.ReadMemoryDocument(m.workdir)
	if err != nil {
		return nil, err
	}
	sections := []struct {
		name    string
		entries []recap.MemoryEntry
	}{
		{name: "preferences", entries: document.Preferences},
		{name: "decisions", entries: document.Decisions},
		{name: "things to avoid", entries: document.ThingsToAvoid},
		{name: "questions", entries: document.Questions},
		{name: "recent context", entries: document.RecentContext},
	}
	items := make([]memoryHistoryItem, 0)
	for _, section := range sections {
		for _, entry := range section.entries {
			if !memoryEntryBelongsToSession(entry, messageIDs) {
				continue
			}
			items = append(items, memoryHistoryItem{category: section.name, entry: entry})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		left := memoryHistoryTime(items[i].entry.LastSeenUTC)
		right := memoryHistoryTime(items[j].entry.LastSeenUTC)
		if !left.Equal(right) {
			return left.After(right)
		}
		if items[i].entry.LastSeenUTC != items[j].entry.LastSeenUTC {
			return items[i].entry.LastSeenUTC > items[j].entry.LastSeenUTC
		}
		return items[i].entry.ID < items[j].entry.ID
	})
	return items, nil
}

func memoryEntryBelongsToSession(entry recap.MemoryEntry, messageIDs map[string]struct{}) bool {
	for _, messageID := range entry.SourceMessageIDs {
		if _, ok := messageIDs[messageID]; ok {
			return true
		}
	}
	return false
}

func memoryHistoryTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func formatMemoryHistoryTime(value string) string {
	parsed := memoryHistoryTime(value)
	if parsed.IsZero() {
		return truncateRunes(singleLine(value), 18)
	}
	return parsed.Local().Format("2006-01-02 15:04")
}

func (m Model) memoryHistoryPageCount() int {
	if len(m.memoryHistoryAll) == 0 {
		return 1
	}
	return (len(m.memoryHistoryAll) + memoryHistoryPageSize - 1) / memoryHistoryPageSize
}

func (m Model) memoryHistoryPageItems() []memoryHistoryItem {
	start := m.memoryHistoryPage * memoryHistoryPageSize
	if start >= len(m.memoryHistoryAll) {
		return []memoryHistoryItem{}
	}
	end := min(len(m.memoryHistoryAll), start+memoryHistoryPageSize)
	return append([]memoryHistoryItem{}, m.memoryHistoryAll[start:end]...)
}

func (m Model) setMemoryHistoryPage(page int) Model {
	page = max(0, min(page, m.memoryHistoryPageCount()-1))
	if page == m.memoryHistoryPage {
		return m
	}
	m.memoryHistoryPage = page
	m.memoryHistoryItems = m.memoryHistoryPageItems()
	m.subagentCursor = 0
	m.subagentHover = -1
	m.subagentVp.GotoTop()
	return m.resizeSubagentDrawer()
}

func (m Model) collapseMemoryHistoryDrawer() Model {
	follow := m.transcript.AtBottom()
	m = m.setFocus(focusSubagents)
	m.memoryHistoryMode = true
	m.subagentDrawerCompact = true
	m.subagentHover = -1
	m = m.resizeSubagentDrawer()
	m.syncTranscript()
	if follow {
		m.transcript.GotoBottom()
	}
	return m
}

func (m Model) expandMemoryHistoryDrawer() Model {
	if !m.memoryHistoryMode {
		return m.openMemoryHistory()
	}
	follow := m.transcript.AtBottom()
	m = m.setFocus(focusSubagents)
	m.memoryHistoryMode = true
	m.subagentDrawerCompact = false
	m = m.reloadMemoryHistory()
	m = m.resizeSubagentDrawer()
	m.syncTranscript()
	if follow {
		m.transcript.GotoBottom()
	}
	return m
}

func (m Model) memoryHistoryDrawerView() string {
	width := m.pickerDrawerWidth()
	pageCount := m.memoryHistoryPageCount()
	meta := fmt.Sprintf("page %d/%d  ·  %d total", m.memoryHistoryPage+1, pageCount, len(m.memoryHistoryAll))
	if m.subagentDrawerCompact {
		return drawerChrome("memory history", meta, "", "enter/click expand  ·  esc close", width)
	}
	body := hintStyle.Render("no memories from this chat")
	if len(m.memoryHistoryItems) > 0 {
		body = withScrollbar(m.subagentVp.View(), m.subagentVp.Width(), m.subagentVp.Height(),
			m.subagentVp.ScrollPercent(), m.subagentVp.TotalLineCount() > m.subagentVp.Height())
	}
	footer := "↑/↓ select  ·  → details  ·  pgup/pgdn page  ·  esc close"
	return drawerChrome("memory history", meta, body, footer, width)
}

func (m Model) memoryHistoryDrawerContent(width int) string {
	var b strings.Builder
	for i, item := range m.memoryHistoryItems {
		if i > 0 {
			b.WriteString("\n")
		}
		left := item.category + "  " + singleLine(item.entry.Text)
		if item.entry.State != "active" {
			left += "  ·  " + item.entry.State
		}
		b.WriteString(drawerRowLine(left, formatMemoryHistoryTime(item.entry.LastSeenUTC), i == m.subagentCursor, width, 2))
	}
	return b.String()
}

func (m Model) updateMemoryHistoryPickerKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	empty := strings.TrimSpace(m.prompt.Value()) == ""
	if m.subagentDrawerCompact {
		switch key.Code {
		case tea.KeyEscape, 'q', 'Q', 'x', 'X':
			if empty || key.Code == tea.KeyEscape {
				return m.closeSubagentPicker(), nil
			}
		case tea.KeyEnter, tea.KeyRight:
			if empty {
				return m.expandMemoryHistoryDrawer(), nil
			}
		case tea.KeyDown, tea.KeyUp, 'j', 'k':
			if empty {
				return m.expandMemoryHistoryDrawer(), nil
			}
		}
		return m.updateKey(key)
	}
	if !empty {
		return m.updateKey(key)
	}
	switch key.Code {
	case tea.KeyEscape, 'q', 'Q', 'x', 'X', tea.KeyLeft:
		return m.closeSubagentPicker(), nil
	case tea.KeyDown, 'j':
		if m.subagentCursor < len(m.memoryHistoryItems)-1 {
			m.subagentCursor++
			m = m.resizeSubagentDrawer()
		}
	case tea.KeyUp, 'k':
		if m.subagentCursor > 0 {
			m.subagentCursor--
			m = m.resizeSubagentDrawer()
		}
	case tea.KeyEnter, tea.KeyRight:
		return m.openSelectedMemoryHistoryDetail()
	case tea.KeyPgDown:
		return m.setMemoryHistoryPage(m.memoryHistoryPage + 1), nil
	case tea.KeyPgUp:
		return m.setMemoryHistoryPage(m.memoryHistoryPage - 1), nil
	case 'r', 'R':
		m = m.reloadMemoryHistory()
		return m.resizeSubagentDrawer(), nil
	}
	return m, nil
}

func (m Model) updateMemoryHistoryDetailKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	switch key.Code {
	case tea.KeyEscape, 'q', 'Q', 'x', 'X', tea.KeyLeft:
		return m.closeSubagentLogToDrawer(), nil
	case tea.KeyDown, 'j':
		m.subagentLogVp.ScrollDown(1)
	case tea.KeyUp, 'k':
		m.subagentLogVp.ScrollUp(1)
	case tea.KeyPgDown:
		m.subagentLogVp.PageDown()
	case tea.KeyPgUp:
		m.subagentLogVp.PageUp()
	case tea.KeyEnd:
		m.subagentLogVp.GotoBottom()
	case 'd', 'D':
		return m.closeSubagentPicker(), nil
	}
	return m, nil
}

func (m Model) openSelectedMemoryHistoryDetail() (Model, tea.Cmd) {
	if m.subagentCursor < 0 || m.subagentCursor >= len(m.memoryHistoryItems) {
		return m, nil
	}
	m.memoryHistorySelected = m.memoryHistoryItems[m.subagentCursor]
	m.memoryHistoryDetailMode = true
	m = m.setFocus(focusSubagentLog)
	m.subagentLogVp.SetContent(m.memoryHistoryDetailContent(m.memoryHistorySelected))
	m.subagentLogVp.GotoTop()
	return m.resizeMemoryHistoryDetail(), nil
}

func (m Model) memoryHistoryDetailContent(item memoryHistoryItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "category: %s\nstate: %s\nlast updated: %s\nfirst seen: %s\nsource messages: %s\n\n",
		item.category,
		firstNonEmptyStr(item.entry.State, "unknown"),
		item.entry.LastSeenUTC,
		item.entry.FirstSeenUTC,
		strings.Join(item.entry.SourceMessageIDs, ", "))
	b.WriteString("MEMORY\n\n")
	b.WriteString(item.entry.Text)
	b.WriteString("\n\nEVIDENCE\n\n")
	b.WriteString(item.entry.Evidence)
	return b.String()
}

func (m Model) resizeMemoryHistoryDetail() Model {
	atBottom := m.subagentLogVp.TotalLineCount() == 0 || m.subagentLogVp.AtBottom()
	off := m.subagentLogVp.YOffset()
	w := max(minPaneWidth, m.width)
	m.subagentLogVp.SetWidth(max(pickerVpMinWidth, w-1))
	m.subagentLogVp.SetHeight(m.subagentLogVPHeight())
	m.subagentLogVp.SetContent(m.memoryHistoryDetailContent(m.memoryHistorySelected))
	if atBottom {
		m.subagentLogVp.GotoTop()
	} else {
		m.subagentLogVp.SetYOffset(off)
	}
	return m
}

func (m Model) memoryHistoryDetailScreen() string {
	w := max(1, m.width)
	h := max(1, m.height)
	item := m.memoryHistorySelected
	title := "MEMORY HISTORY  ·  " + item.category + "  ·  " + formatMemoryHistoryTime(item.entry.LastSeenUTC)
	closeBtn := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("[x]")
	headerText := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render(
		truncateRunes(title, w-lipgloss.Width(closeBtn)-1),
	)
	gap := max(1, w-lipgloss.Width(headerText)-lipgloss.Width(closeBtn))
	header := lipgloss.NewStyle().Width(w).Background(theme.ColorBg()).Render(headerText + strings.Repeat(" ", gap) + closeBtn)
	vpH := m.subagentLogVPHeight()
	body := withScrollbar(m.subagentLogVp.View(), m.subagentLogVp.Width(), vpH,
		m.subagentLogVp.ScrollPercent(), m.subagentLogVp.TotalLineCount() > vpH)
	bodyLines := strings.Split(body, "\n")
	for len(bodyLines) < vpH {
		bodyLines = append(bodyLines, "")
	}
	if len(bodyLines) > vpH {
		bodyLines = bodyLines[:vpH]
	}
	body = strings.Join(bodyLines, "\n")
	footer := hintStyle.Width(w).Render(truncateRunes(
		"↑/↓ scroll  ·  ← back  ·  [x] back",
		w,
	))
	out := lipgloss.JoinVertical(lipgloss.Left, header, "", body, m.subagentLogJumpBarView(), footer)
	return lipgloss.NewStyle().Background(theme.ColorBg()).Width(w).Height(h).Render(out)
}

func (m Model) memoryHistoryDetailCloseRect() (x0, y, x1 int, ok bool) {
	if !m.memoryHistoryDetailMode {
		return 0, 0, 0, false
	}
	for i, line := range strings.Split(m.memoryHistoryDetailScreen(), "\n") {
		plain := ansi.Strip(line)
		if !strings.Contains(plain, "MEMORY HISTORY") || !strings.Contains(plain, "[x]") {
			continue
		}
		start, end, found := displaySpan(plain, "[x]")
		if found {
			return max(0, start-1), i, end + 1, true
		}
	}
	return 0, 0, 0, false
}

func (m Model) memoryHistoryDetailHit(x, y int, button tea.MouseButton) (Model, tea.Cmd, bool) {
	if !m.memoryHistoryDetailMode || (button != tea.MouseLeft && button != tea.MouseRight) {
		return m, nil, false
	}
	if x0, cy, x1, ok := m.memoryHistoryDetailCloseRect(); ok && y == cy && x >= x0 && x < x1 {
		return m.closeSubagentLogToDrawer(), nil, true
	}
	if button == tea.MouseLeft && y == m.subagentLogJumpBarRow() && !m.subagentLogVp.AtBottom() {
		m.subagentLogVp.GotoBottom()
		return m, nil, true
	}
	return m, nil, true
}

func appendChatError(existing, next string) string {
	if strings.TrimSpace(existing) == "" {
		return next
	}
	return existing + "\n" + next
}
