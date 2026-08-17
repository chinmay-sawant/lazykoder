package chat

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/subagent"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

// subagentRow is one entry in the sub-agent drawer / log card.
type subagentRow struct {
	ID             string // job id when live, else child session id
	Name           string
	Role           string
	Status         string
	ChildSessionID string
	Summary        string
	Err            string
	Activity       string // one-liner on the right (tool or status)
	StartedAt      int64
	Live           bool
}

const (
	// Log view uses the full terminal: header + blank + footer.
	subagentLogHeaderRows = 2
	subagentLogFooterRows = 1
	maxSubagentActivity   = 48
	maxSubagentDrawerRows = 8
)

// openSubagentPicker opens the model-style drawer above the prompt and
// reloads rows for the current parent session.
func (m Model) openSubagentPicker() Model {
	// Remember whether the transcript was following latest output so shrinking
	// the viewport for the drawer does not leave the background stuck mid-scroll.
	follow := m.transcript.AtBottom()
	m.subagentPickerMode = true
	m.subagentDrawerCompact = false
	m.subagentLogMode = false
	m.subagentHover = -1
	m.slashMode = false
	m.slashCursor = 0
	m.pickerMode = false
	m.helpMode = false
	m.filePickerMode = false
	m.sessionPickerMode = false
	m.settingsMode = false
	m.prompt.SetValue("")
	m.promptUndo = nil
	m = m.ensureSubagentBuilt()
	m = m.reloadSubagentRows()
	if m.subagentCursor >= len(m.subagentItems) {
		m.subagentCursor = max(0, len(m.subagentItems)-1)
	}
	m = m.resizeSubagentDrawer()
	m.syncTranscript()
	if follow {
		m.transcript.GotoBottom()
	}
	return m
}

// collapseSubagentDrawerToSummary keeps the drawer open as a one-line summary
// (counts only). Used after todowrite so the checklist and agents both stay on
// screen without a tall list. No-op when there are no sub-agent rows.
func (m Model) collapseSubagentDrawerToSummary() Model {
	m = m.ensureSubagentBuilt()
	m = m.reloadSubagentRows()
	if len(m.subagentItems) == 0 {
		return m.closeSubagentPicker()
	}
	follow := m.transcript.AtBottom()
	m.subagentPickerMode = true
	m.subagentDrawerCompact = true
	m.subagentLogMode = false
	m.subagentHover = -1
	m = m.resizeSubagentDrawer()
	m.syncTranscript()
	if follow {
		m.transcript.GotoBottom()
	}
	return m
}

// expandSubagentDrawer restores the full list from compact summary mode.
func (m Model) expandSubagentDrawer() Model {
	if !m.subagentPickerMode {
		return m.openSubagentPicker()
	}
	follow := m.transcript.AtBottom()
	m.subagentDrawerCompact = false
	m.subagentLogMode = false
	m = m.reloadSubagentRows()
	m = m.resizeSubagentDrawer()
	m.syncTranscript()
	if follow {
		m.transcript.GotoBottom()
	}
	return m
}

// closeSubagentPicker closes the drawer and the full-screen log card.
func (m Model) closeSubagentPicker() Model {
	follow := m.transcript.AtBottom()
	m.subagentPickerMode = false
	m.subagentDrawerCompact = false
	m.subagentLogMode = false
	m.subagentHover = -1
	m.subagentLogItems = nil
	m.subagentLogSelected = -1
	m.subagentSelected = subagentRow{}
	// Reclaim drawer rows into the transcript and keep following the bottom.
	m.syncTranscript()
	if follow {
		m.transcript.GotoBottom()
	}
	return m
}

func (m Model) ensureSubagentBuilt() Model {
	if m.subagentBuilt {
		return m
	}
	m.subagentVp = viewport.New(viewport.WithWidth(pickerVpDefaultW), viewport.WithHeight(pickerVpDefaultH))
	m.subagentVp.FillHeight = true
	m.subagentLogVp = viewport.New(viewport.WithWidth(pickerVpDefaultW), viewport.WithHeight(pickerVpDefaultH))
	m.subagentLogVp.FillHeight = true
	m.subagentBuilt = true
	return m
}

// syncSubagentDrawer reloads sub-agent rows for the footer chip and drawer
// without forcing the drawer open. Closing sticks until a new spawn (see
// openSubagentDrawerIfNew) or the user reopens via /agents or the subs:N chip.
func (m Model) syncSubagentDrawer() Model {
	m = m.ensureSubagentBuilt()
	m = m.reloadSubagentRows()
	if len(m.subagentItems) == 0 {
		return m
	}
	if m.subagentPickerMode {
		return m.resizeSubagentDrawer()
	}
	return m
}

// openSubagentDrawerIfNew reloads rows and opens the full drawer when at
// least one job id was not already listed (true spawn). Status/wait ticks
// only refresh rows; they do not re-expand a user-collapsed compact strip.
func (m Model) openSubagentDrawerIfNew() Model {
	prev := make(map[string]bool, len(m.subagentItems))
	for _, r := range m.subagentItems {
		prev[r.ID] = true
	}
	m = m.ensureSubagentBuilt()
	m = m.reloadSubagentRows()
	newJob := false
	for _, r := range m.subagentItems {
		if !prev[r.ID] {
			newJob = true
			break
		}
	}
	if !newJob {
		if m.subagentPickerMode {
			return m.resizeSubagentDrawer()
		}
		return m
	}
	// New spawn: show the full list so live agents are visible again.
	follow := m.transcript.AtBottom()
	m.subagentPickerMode = true
	m.subagentDrawerCompact = false
	m.subagentLogMode = false
	m = m.resizeSubagentDrawer()
	m.syncTranscript()
	if follow {
		m.transcript.GotoBottom()
	}
	return m
}

func (m Model) reloadSubagentRows() Model {
	m.subagentItems = m.collectSubagentRows()
	for i := range m.subagentItems {
		m.subagentItems[i].Activity = m.subagentActivityLine(m.subagentItems[i])
	}
	if m.subagentCursor >= len(m.subagentItems) {
		m.subagentCursor = max(0, len(m.subagentItems)-1)
	}
	return m
}

// refreshSubagentDrawerLive updates status/activity in place without reordering
// existing rows (pulse-safe). New jobs are appended only.
func (m Model) refreshSubagentDrawerLive() Model {
	if m.session == nil {
		return m
	}
	if len(m.subagentItems) == 0 {
		return m.reloadSubagentRows()
	}
	parentID := m.session.ID
	byJob := map[string]subagent.Snapshot{}
	if m.subMgr != nil {
		for _, snap := range m.subMgr.List(parentID) {
			byJob[snap.ID] = snap
		}
	}
	present := map[string]bool{}
	for i := range m.subagentItems {
		row := m.subagentItems[i]
		if snap, ok := byJob[row.ID]; ok {
			row.Status = snap.Status
			row.Live = !isTerminalSubStatus(snap.Status)
			if snap.ChildSessionID != "" {
				row.ChildSessionID = snap.ChildSessionID
			}
			if snap.Summary != "" {
				row.Summary = snap.Summary
			}
			row.Err = snap.Err
			if snap.StartedAt > 0 && row.StartedAt == 0 {
				row.StartedAt = snap.StartedAt
			}
			row.Name = firstNonEmptyStr(snap.Name, row.Name)
			row.Role = firstNonEmptyStr(snap.Role, row.Role)
			row.Activity = m.subagentActivityLine(row)
			m.subagentItems[i] = row
			present[row.ID] = true
			continue
		}
		// Job left the manager map; keep the row but freeze as terminal.
		if row.Live {
			row.Live = false
			if !isTerminalSubStatus(row.Status) {
				row.Status = "completed"
			}
		}
		row.Activity = m.subagentActivityLine(row)
		m.subagentItems[i] = row
	}
	// Prepend brand-new jobs only (newest first); never reorder existing rows.
	if m.subMgr != nil {
		var newcomers []subagentRow
		for _, snap := range m.subMgr.List(parentID) {
			if present[snap.ID] {
				continue
			}
			row := subagentRow{
				ID:             snap.ID,
				Name:           snap.Name,
				Role:           snap.Role,
				Status:         snap.Status,
				ChildSessionID: snap.ChildSessionID,
				Summary:        snap.Summary,
				Err:            snap.Err,
				StartedAt:      snap.StartedAt,
				Live:           !isTerminalSubStatus(snap.Status),
			}
			row.Activity = m.subagentActivityLine(row)
			newcomers = append(newcomers, row)
		}
		// List() is oldest-first; reverse so the newest newcomer is on top.
		for i, j := 0, len(newcomers)-1; i < j; i, j = i+1, j-1 {
			newcomers[i], newcomers[j] = newcomers[j], newcomers[i]
		}
		if len(newcomers) > 0 {
			m.subagentItems = append(newcomers, m.subagentItems...)
		}
	}
	if m.subagentCursor >= len(m.subagentItems) {
		m.subagentCursor = max(0, len(m.subagentItems)-1)
	}
	return m.resizeSubagentDrawer()
}

func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (m Model) collectSubagentRows() []subagentRow {
	parentID := ""
	if m.session != nil {
		parentID = m.session.ID
	}
	// Stable identity: always key manager jobs by job id. Claim their child
	// session ids so the store does not add a second row for the same run.
	byKey := map[string]subagentRow{}
	var order []string
	claimedSession := map[string]bool{}

	if m.subMgr != nil && parentID != "" {
		for _, snap := range m.subMgr.List(parentID) {
			key := "job:" + snap.ID
			row := subagentRow{
				ID:             snap.ID,
				Name:           snap.Name,
				Role:           snap.Role,
				Status:         snap.Status,
				ChildSessionID: snap.ChildSessionID,
				Summary:        snap.Summary,
				Err:            snap.Err,
				StartedAt:      snap.StartedAt,
				Live:           !isTerminalSubStatus(snap.Status),
			}
			byKey[key] = row
			order = append(order, key)
			if snap.ChildSessionID != "" {
				claimedSession[snap.ChildSessionID] = true
			}
		}
	}

	if m.store != nil && parentID != "" {
		children, err := m.store.ListChildSessions(context.Background(), parentID)
		if err == nil {
			for _, sess := range children {
				if claimedSession[sess.ID] {
					continue
				}
				key := "ses:" + sess.ID
				row := subagentRow{
					ID:             sess.ID,
					Name:           sess.Title,
					Status:         "completed",
					ChildSessionID: sess.ID,
					StartedAt:      sess.TimeCreated,
					Live:           false,
				}
				byKey[key] = row
				order = append(order, key)
			}
		}
	}

	rows := make([]subagentRow, 0, len(order))
	seen := map[string]bool{}
	for _, key := range order {
		if seen[key] {
			continue
		}
		if r, ok := byKey[key]; ok {
			rows = append(rows, r)
			seen[key] = true
		}
	}
	// Stable order: newest first. Never sort live-first (that reshuffles
	// rows when a job finishes). Positions stay fixed across pulse ticks.
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].StartedAt != rows[j].StartedAt {
			return rows[i].StartedAt > rows[j].StartedAt
		}
		return rows[i].ID > rows[j].ID
	})
	return rows
}

func isTerminalSubStatus(s string) bool {
	switch subagent.Status(s) {
	case subagent.StatusCompleted, subagent.StatusFailed, subagent.StatusCancelled, subagent.StatusTimedOut:
		return true
	default:
		return false
	}
}

func isFailedSubStatus(s string) bool {
	switch strings.ToLower(s) {
	case string(subagent.StatusFailed), string(subagent.StatusCancelled), string(subagent.StatusTimedOut),
		"error", "denied", "crashed":
		return true
	default:
		return false
	}
}

// subagentActivityLine is the right-hand one-liner for a drawer row.
func (m Model) subagentActivityLine(row subagentRow) string {
	if row.ChildSessionID != "" && m.store != nil {
		if line := m.latestToolActivity(row.ChildSessionID); line != "" {
			return truncateRunes(line, maxSubagentActivity)
		}
	}
	if row.Live {
		st := row.Status
		if st == "" {
			st = "running"
		}
		return st
	}
	if row.Err != "" {
		return truncateRunes(row.Err, maxSubagentActivity)
	}
	if row.Summary != "" {
		return truncateRunes(strings.ReplaceAll(row.Summary, "\n", " "), maxSubagentActivity)
	}
	if row.Status != "" {
		return row.Status
	}
	return "done"
}

func (m Model) latestToolActivity(sessionID string) string {
	tcs, err := m.store.ListToolCalls(context.Background(), sessionID)
	if err != nil || len(tcs) == 0 {
		return ""
	}
	// Prefer the last non-completed tool (still running).
	for i := len(tcs) - 1; i >= 0; i-- {
		tc := tcs[i]
		st := strings.ToLower(tc.Status)
		if st == "pending" || st == "running" || st == "" {
			return formatToolActivity(tc)
		}
	}
	// Otherwise the most recent tool.
	return formatToolActivity(tcs[len(tcs)-1])
}

func formatToolActivity(tc db.ToolCall) string {
	name := tc.Tool
	if name == "" {
		name = "tool"
	}
	title := ""
	if tc.Title != nil {
		title = strings.TrimSpace(*tc.Title)
	}
	if title == "" {
		return name
	}
	if title == name {
		return name
	}
	return name + "  " + title
}

func (m Model) hasLiveSubagents() bool {
	for _, r := range m.subagentItems {
		if r.Live {
			return true
		}
	}
	// Cheap path when items not loaded yet.
	if m.subMgr != nil && m.session != nil {
		return m.subMgr.Active() > 0
	}
	return false
}

// subagentDrawerVPHeight caps the list so header + drawer + composer always
// fit in the terminal (same idea as pickerVPHeight for /model).
func (m Model) subagentDrawerVPHeight() int {
	if m.subagentDrawerCompact {
		return 0
	}
	// header, blank after header, alert row, drawer chrome (header+footer),
	// gap before prompt, full composer, and a floor for the transcript.
	reserved := lipgloss.Height(m.headerView()) + 1 + 1 + pickerDrawerChrome + 1 +
		lipgloss.Height(m.promptLine())
	if m.slashMode {
		reserved += 1 + lipgloss.Height(m.slashView())
	}
	if m.pickerMode {
		reserved += 1 + lipgloss.Height(m.pickerView())
	}
	if m.err != "" {
		reserved += lipgloss.Height(errStyle.Width(max(minPaneWidth, m.width)).Render(m.err))
	}
	// Keep at least minPaneHeight for the transcript above the drawer.
	reserved += minPaneHeight
	available := max(1, m.height-reserved)
	n := len(m.subagentItems)
	if n < 1 {
		n = 1
	}
	return min(n, min(maxSubagentDrawerRows, available))
}

func (m Model) resizeSubagentDrawer() Model {
	follow := m.transcript.AtBottom()
	cardW := m.pickerDrawerWidth()
	vpH := m.subagentDrawerVPHeight()
	m.subagentVp.SetWidth(max(pickerVpMinWidth, cardW-1))
	m.subagentVp.SetHeight(vpH)
	if !m.subagentDrawerCompact {
		m.subagentVp.SetContent(m.subagentDrawerContent(cardW - 1))
		m.ensureSubagentCursorVisible()
	}
	// Drawer height feeds transcriptRenderHeight; re-sync so the background
	// viewport keeps following when the user was already at the bottom.
	m.syncTranscript()
	if follow {
		m.transcript.GotoBottom()
	}
	return m
}

func (m Model) resizeSubagentLogCard() Model {
	// Full terminal width/height: only a one-line header and footer are reserved.
	w := max(minPaneWidth, m.width)
	vpH := m.subagentLogVPHeight()
	m.subagentLogVp.SetWidth(max(pickerVpMinWidth, w-1))
	m.subagentLogVp.SetHeight(vpH)
	m.subagentLogVp.SetContent(m.renderSubagentLogContent())
	m.subagentLogVp.GotoTop()
	return m
}

func (m Model) subagentLogVPHeight() int {
	return max(minPaneHeight, m.height-subagentLogHeaderRows-subagentLogFooterRows)
}

func (m Model) refreshSubagentLogContent() Model {
	if !m.subagentLogMode {
		return m
	}
	atBottom := m.subagentLogVp.AtBottom()
	off := m.subagentLogVp.YOffset()
	m.subagentLogVp.SetContent(m.renderSubagentLogContent())
	if atBottom {
		m.subagentLogVp.GotoBottom()
	} else {
		m.subagentLogVp.SetYOffset(off)
	}
	return m
}

func (m Model) ensureSubagentCursorVisible() {
	if len(m.subagentItems) == 0 {
		return
	}
	line := m.subagentCursor
	h := m.subagentVp.Height()
	off := m.subagentVp.YOffset()
	if line < off {
		m.subagentVp.SetYOffset(line)
	}
	if line >= off+h {
		m.subagentVp.SetYOffset(line - h + 1)
	}
}

// subagentDrawerView is the model-picker-style list above the prompt.
// Compact mode is a short summary strip (header + footer only).
func (m Model) subagentDrawerView() string {
	cardW := m.pickerDrawerWidth()
	live, ok, failed, total := m.subagentDrawerCounts()
	header := hintStyle.Render("sub-agents  ·  ")
	if m.subagentDrawerCompact {
		header += lipgloss.NewStyle().Foreground(theme.ColorText()).Render(fmt.Sprintf("%d", total))
		if live > 0 {
			header += hintStyle.Render("  ·  ")
			header += lipgloss.NewStyle().Foreground(theme.ColorAccent()).Render(fmt.Sprintf("%d live", live))
		}
		if ok > 0 {
			header += hintStyle.Render("  ·  ")
			header += lipgloss.NewStyle().Foreground(theme.ColorGood()).Render(fmt.Sprintf("%d ok", ok))
		}
		if failed > 0 {
			header += hintStyle.Render("  ·  ")
			header += lipgloss.NewStyle().Foreground(theme.ColorDanger()).Render(fmt.Sprintf("%d failed", failed))
		}
		if lipgloss.Width(header) > cardW {
			header = truncateRunes(header, cardW)
		}
		footer := hintStyle.Width(cardW).Render(
			truncateRunes("enter expand  •  click title close  •  esc close", cardW),
		)
		return lipgloss.NewStyle().Width(cardW).Render(
			lipgloss.JoinVertical(lipgloss.Left, header, footer),
		)
	}

	if live > 0 {
		header += lipgloss.NewStyle().Foreground(theme.ColorAccent()).Render(fmt.Sprintf("%d live", live))
		header += hintStyle.Render(fmt.Sprintf("  ·  %d total", total))
	} else {
		header += hintStyle.Render(fmt.Sprintf("%d", total))
	}
	if lipgloss.Width(header) > cardW {
		header = truncateRunes(header, cardW)
	}

	body := hintStyle.Render("no sub-agents for this session")
	if total > 0 {
		body = withScrollbar(m.subagentVp.View(), m.subagentVp.Width(), m.subagentVp.Height(),
			m.subagentVp.ScrollPercent(), m.subagentVp.TotalLineCount() > m.subagentVp.Height())
	}
	footer := hintStyle.Width(cardW).Render(
		truncateRunes("j/k select  •  enter logs  •  d cancel live  •  esc close", cardW),
	)
	return lipgloss.NewStyle().Width(cardW).Render(
		lipgloss.JoinVertical(lipgloss.Left, header, body, footer),
	)
}

// subagentDrawerCounts returns live / ok / failed / total for drawer chrome.
func (m Model) subagentDrawerCounts() (live, ok, failed, total int) {
	total = len(m.subagentItems)
	for _, r := range m.subagentItems {
		st := strings.ToLower(strings.TrimSpace(r.Status))
		switch {
		case r.Live:
			live++
		case isFailedSubStatus(r.Status):
			failed++
		case st == "completed" || st == "success" || st == "done" || isTerminalSubStatus(r.Status):
			ok++
		default:
			live++
		}
	}
	return live, ok, failed, total
}

func (m Model) subagentDrawerContent(width int) string {
	var b strings.Builder
	for i, row := range m.subagentItems {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.subagentDrawerRow(row, i == m.subagentCursor, width))
	}
	return b.String()
}

func (m Model) subagentDrawerRow(row subagentRow, selected bool, width int) string {
	prefix := "  "
	if selected {
		prefix = "▸ "
	}
	diamond := m.subagentDiamond(row)
	name := row.Name
	if name == "" {
		name = row.ID
	}
	st := strings.TrimSpace(row.Status)
	if st == "" {
		st = "unknown"
	}
	if row.Role != "" {
		name = name + "  ·  " + row.Role
	}
	left := prefix + diamond + "  " + st + "  " + name
	right := row.Activity
	if right == st {
		right = ""
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		left = truncateRunes(left, max(6, width-lipgloss.Width(right)-1))
		gap = width - lipgloss.Width(left) - lipgloss.Width(right)
	}
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + hintStyle.Render(right)
	if selected {
		return lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).MaxWidth(width).Render(line)
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(line)
}

// subagentDiamond is the status mark: throb when live, green when done, red on crash.
func (m Model) subagentDiamond(row subagentRow) string {
	style := lipgloss.NewStyle()
	switch {
	case row.Live || row.Status == string(subagent.StatusQueued) || row.Status == string(subagent.StatusRunning) || row.Status == "pending":
		if m.pulseOn {
			style = style.Foreground(theme.PulseAccent(m.pulseT()))
		} else {
			style = style.Foreground(theme.ColorText())
		}
	case isFailedSubStatus(row.Status):
		style = style.Foreground(theme.ColorDanger())
	default:
		style = style.Foreground(theme.ColorGood())
	}
	return style.Render(theme.StatusDiamond)
}

// subagentLogScreen paints the child transcript at 100% of the terminal,
// using the same thinking / tool / work-rail design as the main chat.
func (m Model) subagentLogScreen() string {
	w := max(1, m.width)
	h := max(1, m.height)
	name := m.subagentSelected.Name
	if name == "" {
		name = m.subagentSelected.ID
	}
	title := "SUB-AGENT  ·  " + name
	if m.subagentSelected.Status != "" {
		title += "  ·  " + m.subagentSelected.Status
	}
	closeBtn := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("[x]")
	gap := max(1, w-lipgloss.Width(title)-lipgloss.Width(closeBtn))
	header := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render(truncateRunes(title, w-lipgloss.Width(closeBtn)-1)) +
		strings.Repeat(" ", gap) + closeBtn
	header = lipgloss.NewStyle().Width(w).Background(theme.ColorBg()).Render(header)

	vpH := m.subagentLogVPHeight()
	body := withScrollbar(m.subagentLogVp.View(), m.subagentLogVp.Width(), vpH,
		m.subagentLogVp.ScrollPercent(), m.subagentLogVp.TotalLineCount() > vpH)
	// Pad body to exact remaining height so the footer sits on the last row.
	bodyLines := strings.Split(body, "\n")
	for len(bodyLines) < vpH {
		bodyLines = append(bodyLines, "")
	}
	if len(bodyLines) > vpH {
		bodyLines = bodyLines[:vpH]
	}
	body = strings.Join(bodyLines, "\n")

	footer := hintStyle.Width(w).Render(truncateRunes(
		"j/k scroll  •  t thinking  •  e tool  •  enter toggle  •  esc back  •  d close",
		w,
	))
	out := lipgloss.JoinVertical(lipgloss.Left, header, "", body, footer)
	// Guarantee full-frame paint.
	return lipgloss.NewStyle().
		Background(theme.ColorBg()).
		Width(w).
		Height(h).
		Render(out)
}

// subagentLogCloseRect is the screen rectangle of the [x] close control on the
// full-screen sub-agent log card header (same idea as settingsCloseRect).
func (m Model) subagentLogCloseRect() (x0, y, x1 int, ok bool) {
	if !m.subagentLogMode {
		return 0, 0, 0, false
	}
	for i, line := range strings.Split(m.subagentLogScreen(), "\n") {
		plain := ansi.Strip(line)
		if !strings.Contains(plain, "SUB-AGENT") || !strings.Contains(plain, "[x]") {
			continue
		}
		start, end, found := displaySpan(plain, "[x]")
		if !found {
			continue
		}
		// Expand hit target by one cell on each side for easier clicking.
		return max(0, start-1), i, end + 1, true
	}
	return 0, 0, 0, false
}

// subagentLogHit handles a mouse press on the full-screen log view.
// [x] returns to the drawer; clicks on thinking/tool blocks expand or collapse.
func (m Model) subagentLogHit(x, y int, button tea.MouseButton) (Model, tea.Cmd, bool) {
	if !m.subagentLogMode {
		return m, nil, false
	}
	if button != tea.MouseLeft && button != tea.MouseRight {
		return m, nil, false
	}
	if x0, cy, x1, ok := m.subagentLogCloseRect(); ok && y == cy && x >= x0 && x < x1 {
		next, cmd := m.updateSubagentLogKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		return next, cmd, true
	}
	if button == tea.MouseLeft {
		if idx, ok := m.subagentLogItemIndexAtScreenY(y); ok {
			kind := m.subagentLogItems[idx].kind
			if kind == itemTool || kind == itemReasoning {
				it := m.subagentLogItems[idx]
				it.collapsed = !it.collapsed
				m.subagentLogItems[idx] = it
				m.subagentLogSelected = idx
				return m.refreshSubagentLogContent(), nil, true
			}
		}
	}
	// Consume other clicks so they do not hit the chat underneath.
	return m, nil, true
}

// subagentLogItemIndexAtScreenY maps a click row in the full-screen log body
// to a subagentLogItems index (same idea as itemIndexAtScreenY for main chat).
func (m Model) subagentLogItemIndexAtScreenY(y int) (int, bool) {
	if !m.subagentLogMode || len(m.subagentLogItems) == 0 {
		return -1, false
	}
	// Body starts after the one-line header.
	top := subagentLogHeaderRows
	height := m.subagentLogVPHeight()
	if y < top || y >= top+height {
		return -1, false
	}
	target := y - top + m.subagentLogVp.YOffset()
	if target < 0 {
		return -1, false
	}
	rendered := m.renderedSubagentLogItems()
	row := 0
	ri := 0
	for i, it := range m.subagentLogItems {
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

// renderedSubagentLogItems returns each painted block for hit-testing.
func (m Model) renderedSubagentLogItems() []string {
	if len(m.subagentLogItems) == 0 {
		return nil
	}
	renderM := m
	renderM.width = max(minPaneWidth, m.width)
	if m.subagentLogVp.Width() > 0 {
		renderM.transcript = m.subagentLogVp
	}
	out := make([]string, 0, len(m.subagentLogItems)*2)
	for i, it := range m.subagentLogItems {
		if i > 0 && (it.kind == itemUser || it.kind == itemAssistant) {
			if it.kind == itemUser {
				out = append(out, "")
			} else {
				out = append(out, renderM.withWorkRail(" ", false))
			}
		}
		itemM := renderM
		if it.kind != itemUser && it.kind != itemNote {
			itemM.railInset = workRailCols
		}
		body := itemM.renderItem(it, i == m.subagentLogSelected, false)
		if it.kind != itemUser && it.kind != itemNote {
			body = itemM.withWorkRail(body, false)
		}
		out = append(out, body)
	}
	return out
}

func (m Model) updateSubagentPickerKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.subagentLogMode {
		if key.Mod.Contains(tea.ModCtrl) && key.Code == 'c' {
			return m.closeDone(), tea.Quit
		}
		return m.updateSubagentLogKey(key)
	}
	// Drawer stays open while the composer stays fully interactive: typing,
	// paste, send, etc. Only dedicated navigation keys are handled here.
	empty := strings.TrimSpace(m.prompt.Value()) == ""
	if m.subagentDrawerCompact {
		switch key.Code {
		case tea.KeyEscape, 'q', 'Q', 'x', 'X':
			if empty || key.Code == tea.KeyEscape {
				return m.closeSubagentPicker(), nil
			}
			return m.updateKey(key)
		case tea.KeyEnter:
			if !empty {
				return m.updateKey(key)
			}
			return m.expandSubagentDrawer(), nil
		case tea.KeyDown, tea.KeyUp, 'j', 'k':
			if !empty && (key.Code == 'j' || key.Code == 'k') {
				return m.updateKey(key)
			}
			return m.expandSubagentDrawer(), nil
		default:
			return m.updateKey(key)
		}
	}
	switch key.Code {
	case tea.KeyEscape:
		// Esc collapses to the summary strip first; second esc closes.
		return m.collapseSubagentDrawerToSummary(), nil
	case tea.KeyDown:
		if m.subagentCursor < len(m.subagentItems)-1 {
			m.subagentCursor++
			m = m.resizeSubagentDrawer()
		}
		return m, nil
	case tea.KeyUp:
		if m.subagentCursor > 0 {
			m.subagentCursor--
			m = m.resizeSubagentDrawer()
		}
		return m, nil
	case 'j':
		if !empty {
			return m.updateKey(key)
		}
		if m.subagentCursor < len(m.subagentItems)-1 {
			m.subagentCursor++
			m = m.resizeSubagentDrawer()
		}
		return m, nil
	case 'k':
		if !empty {
			return m.updateKey(key)
		}
		if m.subagentCursor > 0 {
			m.subagentCursor--
			m = m.resizeSubagentDrawer()
		}
		return m, nil
	case tea.KeyPgDown:
		m.subagentVp.PageDown()
		return m, nil
	case tea.KeyPgUp:
		m.subagentVp.PageUp()
		return m, nil
	case tea.KeyEnter:
		// With a draft, enter sends; with an empty prompt, open the log.
		if !empty {
			return m.updateKey(key)
		}
		return m.openSelectedSubagentLog()
	case 'q', 'Q', 'x', 'X':
		if empty {
			return m.closeSubagentPicker(), nil
		}
		return m.updateKey(key)
	case 'd', 'D':
		if empty {
			return m.cancelSelectedSubagent()
		}
		return m.updateKey(key)
	case 'r', 'R':
		if empty {
			m = m.reloadSubagentRows()
			return m.resizeSubagentDrawer(), nil
		}
		return m.updateKey(key)
	default:
		return m.updateKey(key)
	}
}

func (m Model) updateSubagentLogKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	switch key.Code {
	case tea.KeyEscape, 'q', 'Q', 'x', 'X':
		// Back to the drawer list (full-screen log closes; drawer stays).
		m.subagentLogMode = false
		m.subagentLogItems = nil
		m.subagentLogSelected = -1
		m.subagentPickerMode = true
		m = m.reloadSubagentRows()
		return m.resizeSubagentDrawer(), nil
	case 'j', tea.KeyDown:
		m.subagentLogVp.ScrollDown(1)
		return m, nil
	case 'k', tea.KeyUp:
		m.subagentLogVp.ScrollUp(1)
		return m, nil
	case tea.KeyPgDown:
		m.subagentLogVp.PageDown()
		return m, nil
	case tea.KeyPgUp:
		m.subagentLogVp.PageUp()
		return m, nil
	case 't', 'T':
		return m.toggleSubagentLogKind(itemReasoning), nil
	case 'e', 'E':
		return m.toggleSubagentLogKind(itemTool), nil
	case tea.KeyEnter:
		return m.toggleSubagentLogSelected(), nil
	case 'd', 'D':
		return m.closeSubagentPicker(), nil
	}
	return m, nil
}

func (m Model) openSelectedSubagentLog() (Model, tea.Cmd) {
	if m.subagentCursor < 0 || m.subagentCursor >= len(m.subagentItems) {
		return m, nil
	}
	row := m.subagentItems[m.subagentCursor]
	m.subagentSelected = row
	m.subagentLogItems = m.loadSubagentLogItems(row)
	m.subagentLogSelected = m.lastSubagentLogMetaIndex()
	m.subagentLogMode = true
	m.subagentPickerMode = true // keep mode for key routing
	return m.resizeSubagentLogCard(), nil
}

// loadSubagentLogItems rebuilds the child session as main-chat transcript
// items. Reasoning is expanded by default; tools stay collapsed.
func (m Model) loadSubagentLogItems(row subagentRow) []transcriptItem {
	sessID := row.ChildSessionID
	if sessID == "" || m.store == nil {
		note := "no session log yet"
		if row.Summary != "" {
			note = row.Summary
		}
		return []transcriptItem{{kind: itemNote, text: note}}
	}
	ctx := context.Background()
	msgs, err := m.store.ListMessages(ctx, sessID)
	if err != nil {
		return []transcriptItem{{kind: itemNote, text: "failed to load messages: " + err.Error()}}
	}
	tcs, err := m.store.ListToolCalls(ctx, sessID)
	if err != nil {
		return []transcriptItem{{kind: itemNote, text: "failed to load tools: " + err.Error()}}
	}
	byPart := make(map[string]db.ToolCall, len(tcs))
	for _, tc := range tcs {
		byPart[tc.PartID] = tc
	}
	var items []transcriptItem
	for _, msg := range msgs {
		if !msg.Visible {
			continue
		}
		parts, err := m.store.ListParts(ctx, msg.ID)
		if err != nil {
			continue
		}
		for _, p := range parts {
			switch p.Type {
			case "text":
				if p.Text == nil || *p.Text == "" {
					continue
				}
				if msg.Role == "user" {
					items = append(items, transcriptItem{
						kind: itemUser, text: *p.Text,
						when: itemTime(msg.TimeCreated, p.TimeCreated), part: p,
					})
				} else {
					items = append(items, transcriptItem{
						kind: itemAssistant, text: *p.Text,
						when: itemTime(msg.TimeCreated, p.TimeCreated), part: p,
					})
				}
			case "reasoning":
				if p.Text == nil || *p.Text == "" {
					continue
				}
				// Expanded by default in the sub-agent log view.
				items = append(items, transcriptItem{
					kind: itemReasoning, text: *p.Text, collapsed: false,
					when: itemTime(msg.TimeCreated, p.TimeCreated), part: p,
				})
			case "tool":
				tool := db.ToolCall{PartID: p.ID}
				if stored, ok := byPart[p.ID]; ok {
					tool = stored
				} else {
					tool.Tool = "tool"
					if p.ToolName != nil {
						tool.Tool = *p.ToolName
					}
					if p.ToolStatus != nil {
						tool.Status = *p.ToolStatus
					}
				}
				when := itemTime(msg.TimeCreated, p.TimeCreated)
				if tool.TimeStart != nil {
					when = *tool.TimeStart
				}
				// Edit cards stay open so the diff is always visible.
				collapsed := tool.Tool != "edit"
				items = append(items, transcriptItem{
					kind: itemTool, collapsed: collapsed, when: when, tool: tool, part: p,
				})
			}
		}
	}
	if len(items) == 0 {
		if row.Summary != "" {
			return []transcriptItem{{kind: itemNote, text: row.Summary}}
		}
		return []transcriptItem{{kind: itemNote, text: "empty sub-agent transcript"}}
	}
	return items
}

// renderSubagentLogContent paints log items with the same thinking, tool
// cards, and vertical work rails as the main chat transcript.
func (m Model) renderSubagentLogContent() string {
	if len(m.subagentLogItems) == 0 {
		return hintStyle.Render("no log")
	}
	// Force full-width layout for meta/tool card sizing.
	renderM := m
	renderM.width = max(minPaneWidth, m.width)
	// metaWidth prefers transcript.Width when set; align to log viewport.
	if m.subagentLogVp.Width() > 0 {
		renderM.transcript = m.subagentLogVp
	}

	out := make([]string, 0, len(m.subagentLogItems)*2)
	for i, it := range m.subagentLogItems {
		if i > 0 && (it.kind == itemUser || it.kind == itemAssistant) {
			if it.kind == itemUser {
				out = append(out, "")
			} else {
				// Blank railed spacer between assistant blocks (main chat style).
				out = append(out, renderM.withWorkRail(" ", false))
			}
		}
		itemM := renderM
		if it.kind != itemUser && it.kind != itemNote {
			itemM.railInset = workRailCols
		}
		body := itemM.renderItem(it, i == m.subagentLogSelected, false)
		if it.kind != itemUser && it.kind != itemNote {
			body = itemM.withWorkRail(body, false)
		}
		out = append(out, body)
	}
	return strings.Join(out, "\n")
}

func (m Model) lastSubagentLogMetaIndex() int {
	for i := len(m.subagentLogItems) - 1; i >= 0; i-- {
		k := m.subagentLogItems[i].kind
		if k == itemReasoning || k == itemTool {
			return i
		}
	}
	return -1
}

func (m Model) lastSubagentLogKindIndex(kind itemKind) int {
	for i := len(m.subagentLogItems) - 1; i >= 0; i-- {
		if m.subagentLogItems[i].kind == kind {
			return i
		}
	}
	return -1
}

func (m Model) toggleSubagentLogKind(kind itemKind) Model {
	idx := m.subagentLogSelected
	if idx < 0 || idx >= len(m.subagentLogItems) || m.subagentLogItems[idx].kind != kind {
		idx = m.lastSubagentLogKindIndex(kind)
	}
	if idx < 0 {
		return m
	}
	it := m.subagentLogItems[idx]
	it.collapsed = !it.collapsed
	m.subagentLogItems[idx] = it
	m.subagentLogSelected = idx
	return m.refreshSubagentLogContent()
}

func (m Model) toggleSubagentLogSelected() Model {
	idx := m.subagentLogSelected
	if idx < 0 || idx >= len(m.subagentLogItems) {
		idx = m.lastSubagentLogMetaIndex()
	}
	if idx < 0 {
		return m
	}
	it := m.subagentLogItems[idx]
	if it.kind != itemReasoning && it.kind != itemTool {
		return m
	}
	it.collapsed = !it.collapsed
	m.subagentLogItems[idx] = it
	m.subagentLogSelected = idx
	return m.refreshSubagentLogContent()
}

func (m Model) cancelSelectedSubagent() (Model, tea.Cmd) {
	if m.subMgr == nil || m.subagentCursor < 0 || m.subagentCursor >= len(m.subagentItems) {
		return m, nil
	}
	row := m.subagentItems[m.subagentCursor]
	if !row.Live || !strings.HasPrefix(row.ID, "sub_") {
		return m, nil
	}
	if _, err := m.subMgr.Cancel(row.ID); err != nil {
		m.err = err.Error()
	}
	m = m.reloadSubagentRows()
	return m.resizeSubagentDrawer(), nil
}

// subagentDrawerTop is the first screen row of the sub-agent drawer (header).
// Prefer the painted "sub-agents" line so bottom-pinned layout (pad above the
// composer) still maps clicks correctly.
func (m Model) subagentDrawerTop() int {
	if y, ok := m.subagentHeaderScreenY(); ok {
		return y
	}
	// Fallback: bottom chrome height from the terminal edge.
	h := lipgloss.Height(m.subagentDrawerView())
	bot := lipgloss.Height(m.composerBlock())
	if m.err != "" {
		bot += 1 + lipgloss.Height(errStyle.Width(max(minPaneWidth, m.width)).Render(m.err))
	}
	return max(0, m.height-bot-h)
}

// subagentHeaderScreenY finds the painted drawer title row.
func (m Model) subagentHeaderScreenY() (int, bool) {
	if !m.subagentPickerMode || m.subagentLogMode {
		return 0, false
	}
	for i, line := range m.paintedLines() {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "sub-agents") {
			return i, true
		}
	}
	return 0, false
}

// subagentHeaderAt reports whether screen row y is the drawer title line
// ("sub-agents · …"). Full list: collapse to summary. Compact: close drawer.
func (m Model) subagentHeaderAt(y int) bool {
	top, ok := m.subagentHeaderScreenY()
	return ok && y == top
}

// pointerInSubagentDrawer reports whether screen row y sits on the drawer
// chrome (header, list, footer), not the transcript or composer.
func (m Model) pointerInSubagentDrawer(y int) bool {
	if !m.subagentPickerMode || m.subagentLogMode {
		return false
	}
	top := m.subagentDrawerTop()
	h := lipgloss.Height(m.subagentDrawerView())
	return y >= top && y < top+h
}

// subagentIndexAtScreenY maps a click Y to a drawer row (chat layout).
// Only the visible list band counts so clicks on the header/footer/transcript
// never open a sub-agent by accident.
func (m Model) subagentIndexAtScreenY(y int) (int, bool) {
	if !m.subagentPickerMode || m.subagentLogMode || len(m.subagentItems) == 0 {
		return 0, false
	}
	// Never treat the title row as a list hit (collapse is handled separately).
	if m.subagentHeaderAt(y) {
		return 0, false
	}
	// Drawer header is one line, then the list viewport.
	listTop := m.subagentDrawerTop() + 1
	listH := m.subagentVp.Height()
	if listH < 1 {
		listH = 1
	}
	if y < listTop || y >= listTop+listH {
		return 0, false
	}
	rel := y - listTop + m.subagentVp.YOffset()
	if rel < 0 || rel >= len(m.subagentItems) {
		return 0, false
	}
	return rel, true
}

// hasSubagents reports whether the current session has any sub-agent rows.
func (m Model) hasSubagents() bool {
	return len(m.collectSubagentRows()) > 0
}

// subagentCounts returns live and total counts for the footer label.
func (m Model) subagentCounts() (live, total int) {
	rows := m.subagentItems
	if len(rows) == 0 {
		rows = m.collectSubagentRows()
	}
	total = len(rows)
	for _, r := range rows {
		if r.Live {
			live++
		}
	}
	return live, total
}
