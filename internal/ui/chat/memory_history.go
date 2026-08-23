package chat

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/recap"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

const memoryHistoryPageSize = 20

type memoryHistoryItem struct {
	category string
	entry    recap.MemoryEntry
	update   db.MemoryUpdate
}

// openMemoryHistory opens the shared drawer shell with facts sourced only
// from the active parent session.
func (m Model) openMemoryHistory() Model {
	follow := m.transcript.AtBottom()
	m = m.setFocus(focusSubagents)
	m.memoryHistoryMode = true
	m.memoryHistoryDetailMode = false
	m.memoryHistorySelected = memoryHistoryItem{}
	m.memoryHistoryError = ""
	m.memoryHistorySelection = textSelection{}
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
		m.memoryHistoryError = err.Error()
		return m
	}
	m.memoryHistoryError = ""
	m.err = removeMemoryHistoryError(m.err)
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
	workdir, err := filepath.Abs(m.workdir)
	if err != nil {
		return nil, fmt.Errorf("resolve workdir: %w", err)
	}
	updates, err := m.store.ListMemoryUpdatesForSession(context.Background(), workdir, m.session.ID)
	if err != nil {
		return nil, fmt.Errorf("list memory updates: %w", err)
	}
	updatesByMessage := make(map[string]db.MemoryUpdate, len(updates))
	for _, update := range updates {
		if _, exists := updatesByMessage[update.SourceEndMessageID]; !exists {
			updatesByMessage[update.SourceEndMessageID] = update
		}
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
	matchedUpdates := make(map[string]struct{}, len(updates))
	for _, section := range sections {
		for _, entry := range section.entries {
			if !memoryEntryBelongsToSession(entry, messageIDs) {
				continue
			}
			item := memoryHistoryItem{category: section.name, entry: entry}
			for _, sourceID := range entry.SourceMessageIDs {
				update, ok := updatesByMessage[sourceID]
				if !ok {
					continue
				}
				if item.update.ID == "" || memoryUpdateIsNewer(update, item.update) {
					item.update = update
				}
			}
			if item.update.ID != "" {
				matchedUpdates[item.update.ID] = struct{}{}
			}
			items = append(items, item)
		}
	}
	for _, update := range updates {
		if update.Status != db.MemoryUpdateStatusFailed {
			continue
		}
		if _, matched := matchedUpdates[update.ID]; matched {
			continue
		}
		items = append(items, memoryHistoryItem{
			category: "memory update",
			update:   update,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i].time()
		right := items[j].time()
		if !left.Equal(right) {
			return left.After(right)
		}
		leftValue := items[i].timestamp()
		rightValue := items[j].timestamp()
		if leftValue != rightValue {
			return leftValue > rightValue
		}
		return items[i].id() < items[j].id()
	})
	return items, nil
}

func memoryUpdateIsNewer(left, right db.MemoryUpdate) bool {
	if left.SourceEndSeq != right.SourceEndSeq {
		return left.SourceEndSeq > right.SourceEndSeq
	}
	return itemUpdateTime(left) > itemUpdateTime(right)
}

func (item memoryHistoryItem) id() string {
	if item.entry.ID != "" {
		return item.entry.ID
	}
	return item.update.ID
}

func (item memoryHistoryItem) timestamp() string {
	if item.entry.LastSeenUTC != "" {
		return item.entry.LastSeenUTC
	}
	return fmt.Sprintf("%020d", item.updateTimestamp())
}

func (item memoryHistoryItem) time() time.Time {
	if item.entry.LastSeenUTC != "" {
		return memoryHistoryTime(item.entry.LastSeenUTC)
	}
	return time.UnixMilli(item.updateTimestamp())
}

func (item memoryHistoryItem) updateTimestamp() int64 {
	if item.update.TimeFinished != nil {
		return *item.update.TimeFinished
	}
	return item.update.TimeCreated
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

func formatMemoryUpdateTime(update db.MemoryUpdate) string {
	when := itemUpdateTime(update)
	if when <= 0 {
		return "unknown"
	}
	return time.UnixMilli(when).Local().Format("2006-01-02 15:04")
}

func itemUpdateTime(update db.MemoryUpdate) int64 {
	if update.TimeFinished != nil {
		return *update.TimeFinished
	}
	if update.TimeStarted != nil {
		return *update.TimeStarted
	}
	return update.TimeCreated
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
	bodyParts := make([]string, 0, 2)
	if m.memoryHistoryError != "" {
		bodyParts = append(bodyParts, errStyle.Width(max(1, width-2)).Render("memory history failed: "+m.memoryHistoryError))
	}
	body := hintStyle.Render("no memories from this chat")
	if len(m.memoryHistoryItems) > 0 {
		body = withScrollbar(m.subagentVp.View(), m.subagentVp.Width(), m.subagentVp.Height(),
			m.subagentVp.ScrollPercent(), m.subagentVp.TotalLineCount() > m.subagentVp.Height())
	}
	bodyParts = append(bodyParts, body)
	body = strings.Join(bodyParts, "\n\n")
	footer := "↑/↓ select  ·  → details  ·  pgup/pgdn page  ·  esc close"
	return drawerChrome("memory history", meta, body, footer, width)
}

func (m Model) memoryHistoryDrawerContent(width int) string {
	var b strings.Builder
	for i, item := range m.memoryHistoryItems {
		if i > 0 {
			b.WriteString("\n")
		}
		left := memoryHistoryStatusMark(item) + " " + memoryHistoryStatusLabel(item) + "  " + item.category + "  " + singleLine(memoryHistoryText(item))
		if item.entry.State != "" && item.entry.State != "active" {
			left += "  ·  " + item.entry.State
		}
		right := formatMemoryHistoryTime(item.entry.LastSeenUTC)
		if item.entry.LastSeenUTC == "" {
			right = formatMemoryUpdateTime(item.update)
		}
		b.WriteString(drawerRowLine(left, right, i == m.subagentCursor, width, 2))
	}
	return b.String()
}

func memoryHistoryText(item memoryHistoryItem) string {
	if strings.TrimSpace(item.entry.Text) != "" {
		return item.entry.Text
	}
	if strings.TrimSpace(item.update.Error) != "" {
		return "update failed: " + item.update.Error
	}
	return "memory update " + memoryHistoryStatusLabel(item)
}

func memoryHistoryStatus(item memoryHistoryItem) string {
	return strings.TrimSpace(item.update.Status)
}

func memoryHistoryStatusLabel(item memoryHistoryItem) string {
	switch memoryHistoryStatus(item) {
	case db.MemoryUpdateStatusCompleted:
		return "ok"
	case db.MemoryUpdateStatusFailed:
		return "failed"
	case db.MemoryUpdateStatusQueued, db.MemoryUpdateStatusRunning:
		return memoryHistoryStatus(item)
	default:
		return "unknown"
	}
}

func memoryHistoryStatusMark(item memoryHistoryItem) string {
	color := theme.ColorMute()
	switch memoryHistoryStatus(item) {
	case db.MemoryUpdateStatusCompleted:
		color = theme.ColorGood()
	case db.MemoryUpdateStatusFailed:
		color = theme.ColorDanger()
	case db.MemoryUpdateStatusQueued, db.MemoryUpdateStatusRunning:
		color = theme.ColorAccent()
	}
	return lipgloss.NewStyle().Foreground(color).Render("●")
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
	if key.Mod.Contains(tea.ModCtrl) {
		switch key.Code {
		case 'a', 'A':
			return m.selectAllMemoryHistoryDetail(), nil
		case 'c', 'C':
			text, ok := m.memoryHistorySelectedText()
			if !ok {
				return m, nil
			}
			m.copyNotice = "Text copied"
			return m, tea.Batch(tea.SetClipboard(text), clearCopyNotice())
		}
	}
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
	m.memoryHistorySelection = textSelection{}
	m = m.setFocus(focusSubagentLog)
	m.subagentLogVp.SetContent(m.memoryHistoryDetailContent(m.memoryHistorySelected))
	m.subagentLogVp.GotoTop()
	return m.resizeMemoryHistoryDetail(), nil
}

func (m Model) memoryHistoryDetailContent(item memoryHistoryItem) string {
	width := max(1, m.subagentLogVp.Width())
	status := memoryHistoryStatusLabel(item)
	statusColor := theme.ColorMute()
	statusBackground := theme.ColorSurface()
	switch memoryHistoryStatus(item) {
	case db.MemoryUpdateStatusCompleted:
		statusColor = theme.ColorGood()
		statusBackground = theme.ColorEditPanel()
	case db.MemoryUpdateStatusFailed:
		statusColor = theme.ColorDanger()
		statusBackground = theme.ColorEditDelBg()
	case db.MemoryUpdateStatusQueued, db.MemoryUpdateStatusRunning:
		statusColor = theme.ColorAccent()
		statusBackground = theme.ColorEditPanel()
	}
	meta := fmt.Sprintf("category: %s\nresult: %s\nstate: %s\nlast updated: %s\nfirst seen: %s\nsource messages: %s",
		item.category,
		status,
		firstNonEmptyStr(item.entry.State, "unknown"),
		firstNonEmptyStr(item.entry.LastSeenUTC, formatMemoryUpdateTime(item.update)),
		item.entry.FirstSeenUTC,
		strings.Join(item.entry.SourceMessageIDs, ", "))
	if item.update.Error != "" {
		meta += "\nerror: " + singleLine(item.update.Error)
	}
	panels := []string{
		transcriptPanel(
			lipgloss.NewStyle().Bold(true).Foreground(statusColor).Render("RESULT")+"\n\n"+meta,
			width,
			statusBackground,
			statusColor,
		),
	}
	if m.memoryHistoryError != "" {
		panels = append([]string{transcriptPanel(
			lipgloss.NewStyle().Bold(true).Foreground(theme.ColorDanger()).Render("MEMORY HISTORY ERROR")+"\n\n"+m.memoryHistoryError,
			width,
			theme.ColorEditDelBg(),
			theme.ColorDanger(),
		)}, panels...)
	}
	if item.entry.Text != "" {
		panels = append(panels, transcriptPanel(
			lipgloss.NewStyle().Bold(true).Foreground(theme.ColorGood()).Render("MEMORY")+"\n\n"+item.entry.Text,
			width,
			theme.ColorEditPanel(),
			theme.ColorGood(),
		))
	}
	if item.entry.Evidence != "" {
		panels = append(panels, transcriptPanel(
			lipgloss.NewStyle().Bold(true).Foreground(theme.ColorAssistantBorder()).Render("EVIDENCE")+"\n\n"+item.entry.Evidence,
			width,
			theme.ColorAssistantPanel(),
			theme.ColorAssistantBorder(),
		))
	}
	return strings.Join(panels, "\n\n")
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
	view := m.subagentLogVp.View()
	if m.memoryHistorySelection.active {
		view = m.highlightMemoryHistorySelection(view, m.subagentLogVp.YOffset())
	}
	body := withScrollbar(view, m.subagentLogVp.Width(), vpH,
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
	if button == tea.MouseLeft {
		if pos, ok := m.memoryHistoryDetailPosition(x, y); ok {
			m.memoryHistorySelection = textSelection{
				anchor:   pos,
				focus:    pos,
				active:   true,
				dragging: true,
			}
		}
	}
	return m, nil, true
}

func (m Model) memoryHistoryDetailPosition(x, y int) (textPosition, bool) {
	if !m.memoryHistoryDetailMode || x < 0 || x >= m.subagentLogVp.Width() {
		return textPosition{}, false
	}
	top := subagentLogHeaderRows
	if y < top || y >= top+m.subagentLogVp.Height() {
		return textPosition{}, false
	}
	visibleRow := y - top
	visibleLines := strings.Split(m.subagentLogVp.View(), "\n")
	if visibleRow < 0 || visibleRow >= len(visibleLines) {
		return textPosition{}, false
	}
	row := m.subagentLogVp.YOffset() + visibleRow
	contentLines := strings.Split(m.subagentLogVp.GetContent(), "\n")
	if row < 0 || row >= len(contentLines) {
		return textPosition{}, false
	}
	col := x + m.subagentLogVp.XOffset()
	return textPosition{row: row, col: min(col, lipgloss.Width(ansi.Strip(contentLines[row])))}, true
}

func (m Model) updateMemoryHistoryDetailSelection(mu tea.Mouse) Model {
	if !m.memoryHistorySelection.dragging {
		return m
	}
	if pos, ok := m.memoryHistoryDetailPosition(mu.X, mu.Y); ok {
		m.memoryHistorySelection.focus = pos
	}
	return m
}

func (m Model) selectAllMemoryHistoryDetail() Model {
	lines := strings.Split(m.subagentLogVp.GetContent(), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return m
	}
	last := len(lines) - 1
	m.memoryHistorySelection = textSelection{
		anchor: textPosition{},
		focus: textPosition{
			row: last,
			col: lipgloss.Width(ansi.Strip(lines[last])),
		},
		active: true,
	}
	return m
}

func (m Model) memoryHistorySelectedText() (string, bool) {
	if !m.memoryHistorySelection.hasRange() {
		return "", false
	}
	rows := strings.Split(m.subagentLogVp.GetContent(), "\n")
	start, end := m.memoryHistorySelection.bounds()
	if start.row < 0 || end.row >= len(rows) {
		return "", false
	}
	selected := make([]string, 0, end.row-start.row+1)
	for row := start.row; row <= end.row; row++ {
		from := 0
		to := lipgloss.Width(ansi.Strip(rows[row]))
		if row == start.row {
			from = start.col
		}
		if row == end.row {
			to = end.col
		}
		selected = append(selected, stripTranscriptChrome(ansi.Cut(rows[row], from, to)))
	}
	return strings.Join(selected, "\n"), true
}

func (m Model) highlightMemoryHistorySelection(view string, yOffset int) string {
	start, end := m.memoryHistorySelection.bounds()
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		row := yOffset + i
		if row < start.row || row > end.row {
			continue
		}
		from := 0
		to := lipgloss.Width(strings.TrimRight(ansi.Strip(line), " "))
		if row == start.row {
			from = start.col
		}
		if row == end.row {
			to = end.col
		}
		if from < to {
			lines[i] = lipgloss.StyleRanges(line, lipgloss.NewRange(from, to, selectionStyle))
		}
	}
	return strings.Join(lines, "\n")
}

func appendChatError(existing, next string) string {
	if strings.TrimSpace(existing) == "" {
		return next
	}
	return existing + "\n" + next
}

func removeMemoryHistoryError(existing string) string {
	const prefix = "memory history failed: "
	lines := strings.Split(existing, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
