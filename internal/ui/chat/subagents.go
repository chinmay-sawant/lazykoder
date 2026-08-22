package chat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/subagent"
	"github.com/chinmay-sawant/lazykoder/internal/ui/markdown"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

// subagentRow is one entry in the sub-agent drawer / log card.
type subagentRow struct {
	ID             string // job id when live, else child session id
	Name           string
	Role           string
	Model          string
	Variant        string
	Status         string
	ChildSessionID string
	Summary        string
	Err            string
	Activity       string // one-liner on the right (tool or status)
	StartedAt      int64
	Live           bool
	Cost           float64
	CacheHit       int64
	CacheMiss      int64
}

const (
	// Log view uses the full terminal: header + blank + jump row + footer.
	subagentLogHeaderRows = 2
	subagentLogJumpRows   = 1
	subagentLogFooterRows = 1
	maxSubagentActivity   = 48
	maxSubagentDrawerRows = 8
	// subagentLogRowFactor estimates the two log lines per item.
	subagentLogRowFactor = 2

	subagentDrawerRowChrome      = 4
	subagentDrawerColumnGap      = 2
	subagentDrawerMinLeftWidth   = 24
	subagentDrawerMinMetaWidth   = 20
	subagentDrawerCompactWidth   = 60
	subagentCompactActivityShare = 4
	subagentCompactMetadataShare = 3
	maxRecapDrawerRecords        = 8
)

// openSubagentPicker opens the model-style drawer above the prompt and
// reloads rows for the current parent session.
func (m Model) openSubagentPicker() Model {
	// Remember whether the transcript was following latest output so shrinking
	// the viewport for the drawer does not leave the background stuck mid-scroll.
	follow := m.transcript.AtBottom()
	m = m.setFocus(focusSubagents)
	m.subagentDrawerCompact = false
	m.subagentHover = -1
	m.prompt.SetValue("")
	m.promptUndo = nil
	m = m.ensureSubagentBuilt()
	m = m.reloadSubagentRows()
	if len(m.subagentItems) == 0 && len(m.recapItems) > 0 {
		m.recapSelected = true
	}
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
	if len(m.subagentItems) == 0 && len(m.recapItems) == 0 {
		return m.closeSubagentPicker()
	}
	follow := m.transcript.AtBottom()
	m = m.setFocus(focusSubagents)
	m.subagentDrawerCompact = true
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
	m = m.setFocus(focusSubagents)
	m.subagentDrawerCompact = false
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
	m = m.clearFocus(focusSubagents)
	m.subagentHover = -1
	m.subagentLogItems = nil
	m.subagentLogSelected = -1
	m.subagentSelected = subagentRow{}
	m.recapDetailMode = false
	m.recapSelected = false
	m.recapDetailRecord = db.RecapRecord{}
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
	m.recapDetailVp = viewport.New(viewport.WithWidth(pickerVpDefaultW), viewport.WithHeight(pickerVpDefaultH))
	m.recapDetailVp.FillHeight = true
	m.subagentBuilt = true
	return m
}

// syncSubagentDrawer reloads sub-agent rows for the footer chip and drawer
// without forcing the drawer open. Closing sticks until a new spawn (see
// openSubagentDrawerIfNew) or the user reopens via /agents or the subs:N chip.
func (m Model) syncSubagentDrawer() Model {
	m = m.ensureSubagentBuilt()
	if m.subMgr != nil {
		if err := m.subMgr.TakePersistenceError(); err != nil {
			m.err = "subagent state was not saved: " + err.Error()
		}
	}
	m = m.reloadSubagentRows()
	if len(m.subagentItems) == 0 && len(m.recapItems) == 0 {
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
	m = m.setFocus(focusSubagents)
	m.subagentDrawerCompact = false
	m = m.resizeSubagentDrawer()
	m.syncTranscript()
	if follow {
		m.transcript.GotoBottom()
	}
	return m
}

func (m Model) reloadSubagentRows() Model {
	m.subagentItems = m.collectSubagentRows()
	m = m.reloadRecapRows()
	for i := range m.subagentItems {
		m.subagentItems[i].Activity = m.subagentActivityLine(m.subagentItems[i])
		if sid := m.subagentItems[i].ChildSessionID; sid != "" {
			u := sessionUsageOf(m.store, m.modelInfos, sid)
			m.subagentItems[i].Cost = u.Cost
			m.subagentItems[i].CacheHit = u.CacheHit
			m.subagentItems[i].CacheMiss = u.CacheMiss
		}
	}
	if m.subagentCursor >= len(m.subagentItems) {
		m.subagentCursor = max(0, len(m.subagentItems)-1)
	}
	return m
}

func (m Model) reloadRecapRows() Model {
	if m.store == nil || m.session == nil || m.session.Kind == db.SessionKindSubagent || m.session.ParentSessionID != nil {
		m.recapItems = nil
		return m
	}
	recaps, err := m.store.ListRecaps(context.Background(), m.session.ID, maxRecapDrawerRecords)
	if err == nil {
		m.recapItems = recaps
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
			row.Live = !subagent.IsTerminalStatus(snap.Status)
			if snap.ChildSessionID != "" {
				row.ChildSessionID = snap.ChildSessionID
			}
			if snap.Summary != "" {
				row.Summary = snap.Summary
			}
			row.Err = snap.Err
			row.Model = firstNonEmptyStr(snap.Model, row.Model)
			row.Variant = firstNonEmptyStr(snap.Variant, row.Variant)
			if snap.StartedAt > 0 && row.StartedAt == 0 {
				row.StartedAt = snap.StartedAt
			}
			row.Name = firstNonEmptyStr(snap.Name, row.Name)
			row.Role = firstNonEmptyStr(snap.Role, row.Role)
			row.Activity = m.subagentActivityLine(row)
			if row.ChildSessionID != "" {
				u := sessionUsageOf(m.store, m.modelInfos, row.ChildSessionID)
				row.Cost = u.Cost
				row.CacheHit = u.CacheHit
				row.CacheMiss = u.CacheMiss
			}
			m.subagentItems[i] = row
			present[row.ID] = true
			continue
		}
		// Job left the manager map; keep the row but freeze as terminal.
		if row.Live {
			row.Live = false
			if !subagent.IsTerminalStatus(row.Status) {
				row.Status = string(subagent.StatusCompleted)
			}
		}
		row.Activity = m.subagentActivityLine(row)
		if row.ChildSessionID != "" {
			u := sessionUsageOf(m.store, m.modelInfos, row.ChildSessionID)
			row.Cost = u.Cost
			row.CacheHit = u.CacheHit
			row.CacheMiss = u.CacheMiss
		}
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
				Model:          snap.Model,
				Variant:        snap.Variant,
				Status:         snap.Status,
				ChildSessionID: snap.ChildSessionID,
				Summary:        snap.Summary,
				Err:            snap.Err,
				StartedAt:      snap.StartedAt,
				Live:           !subagent.IsTerminalStatus(snap.Status),
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
	childModels := map[string]string{}
	childVariants := map[string]string{}

	if m.subMgr != nil && parentID != "" {
		for _, snap := range m.subMgr.List(parentID) {
			key := "job:" + snap.ID
			row := subagentRow{
				ID:             snap.ID,
				Name:           snap.Name,
				Role:           snap.Role,
				Model:          snap.Model,
				Variant:        snap.Variant,
				Status:         snap.Status,
				ChildSessionID: snap.ChildSessionID,
				Summary:        snap.Summary,
				Err:            snap.Err,
				StartedAt:      snap.StartedAt,
				Live:           !subagent.IsTerminalStatus(snap.Status),
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
				childModels[sess.ID] = sess.Model
				if sess.Variant != nil {
					childVariants[sess.ID] = *sess.Variant
				}
				if claimedSession[sess.ID] {
					continue
				}
				key := "ses:" + sess.ID
				row := subagentRow{
					ID:             sess.ID,
					Name:           sess.Title,
					Model:          sess.Model,
					Variant:        childVariants[sess.ID],
					Status:         string(subagent.StatusCompleted),
					ChildSessionID: sess.ID,
					StartedAt:      sess.TimeCreated,
					Live:           false,
				}
				byKey[key] = row
				order = append(order, key)
			}
		}
	}
	for key, row := range byKey {
		if row.Model == "" {
			row.Model = childModels[row.ChildSessionID]
		}
		if row.Variant == "" {
			row.Variant = childVariants[row.ChildSessionID]
		}
		byKey[key] = row
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

func isFailedSubStatus(s string) bool {
	switch subagent.Status(strings.ToLower(s)) {
	case subagent.StatusFailed, subagent.StatusCancelled, subagent.StatusTimedOut:
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
	return string(subagent.StatusCompleted)
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

func (m Model) rolledSubagentUsage() sessionUsage {
	var out sessionUsage
	if len(m.subagentItems) > 0 {
		for _, r := range m.subagentItems {
			out.Cost += r.Cost
			out.CacheHit += r.CacheHit
			out.CacheMiss += r.CacheMiss
		}
		return out
	}
	if m.store == nil || m.session == nil {
		return out
	}
	children, err := m.store.ListChildSessions(context.Background(), m.session.ID)
	if err != nil {
		return out
	}
	for _, ch := range children {
		u := sessionUsageOf(m.store, m.modelInfos, ch.ID)
		out.Cost += u.Cost
		out.CacheHit += u.CacheHit
		out.CacheMiss += u.CacheMiss
	}
	return out
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
	if m.liveStatusInSubagentDrawer() {
		reserved += lipgloss.Height(m.liveStatusInDrawerView())
	}
	if recap := m.recapDrawerView(m.recapSelected); recap != "" {
		reserved += lipgloss.Height(recap)
	}
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

func (m Model) liveStatusInSubagentDrawer() bool {
	return m.subagentPickerMode && !m.subagentLogMode && m.showLiveStatus()
}

func (m Model) liveStatusInDrawerView() string {
	return strings.TrimPrefix(m.liveStatusView(), "\n")
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
	atBottom := m.subagentLogVp.TotalLineCount() == 0 || m.subagentLogVp.AtBottom()
	off := m.subagentLogVp.YOffset()
	w := max(minPaneWidth, m.width)
	vpH := m.subagentLogVPHeight()
	m.subagentLogVp.SetWidth(max(pickerVpMinWidth, w-1))
	m.subagentLogVp.SetHeight(vpH)
	m.subagentLogVp.SetContent(m.renderSubagentLogContent())
	if atBottom {
		m.subagentLogVp.GotoBottom()
	} else {
		m.subagentLogVp.SetYOffset(off)
	}
	return m
}

func (m Model) subagentLogVPHeight() int {
	return max(minPaneHeight, m.height-subagentLogHeaderRows-subagentLogJumpRows-subagentLogFooterRows)
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
	var parts []string
	if m.liveStatusInSubagentDrawer() {
		parts = append(parts, m.liveStatusInDrawerView())
	}
	if recap := m.recapDrawerView(m.recapSelected); recap != "" {
		parts = append(parts, recap)
	}

	meta := ""
	if m.subagentDrawerCompact {
		meta = fmt.Sprintf("%d", total)
		if live > 0 {
			meta += fmt.Sprintf("  ·  %d live", live)
		}
		if ok > 0 {
			meta += fmt.Sprintf("  ·  %d ok", ok)
		}
		if failed > 0 {
			meta += fmt.Sprintf("  ·  %d failed", failed)
		}
		footer := "enter/click expand  •  esc close"
		body := ""
		drawer := drawerChrome("sub-agents", meta, body, footer, cardW)
		if len(parts) > 0 {
			return lipgloss.JoinVertical(lipgloss.Left, append(parts, drawer)...)
		}
		return drawer
	}

	if live > 0 {
		meta = fmt.Sprintf("%d live  ·  %d total", live, total)
	} else {
		meta = fmt.Sprintf("%d", total)
	}

	body := hintStyle.Render("no sub-agents for this session")
	if total > 0 {
		body = withScrollbar(m.subagentVp.View(), m.subagentVp.Width(), m.subagentVp.Height(),
			m.subagentVp.ScrollPercent(), m.subagentVp.TotalLineCount() > m.subagentVp.Height())
	}
	footer := "↑/↓ select  •  → logs/context  •  d cancel live  •  esc close"
	drawer := drawerChrome("sub-agents", meta, body, footer, cardW)
	if len(parts) > 0 {
		return lipgloss.JoinVertical(lipgloss.Left, append(parts, drawer)...)
	}
	return drawer
}

// recapDrawerView keeps the hidden worker visible as a selectable drawer row.
// Its green rail separates local memory from blue assistant output. The
// database record is the source of truth; the full files open from this row.
func (m Model) recapDrawerView(selected bool) string {
	if !m.projectSettings.EffectiveRecap().Enabled || m.session == nil || m.session.Kind == db.SessionKindSubagent || m.session.ParentSessionID != nil {
		return ""
	}
	width := max(1, m.pickerDrawerWidth())
	counts := map[string]int{}
	for _, record := range m.recapItems {
		counts[record.Status]++
	}
	meta := fmt.Sprintf("%d record", len(m.recapItems))
	if len(m.recapItems) != 1 {
		meta += "s"
	}
	if counts[db.RecapStatusCompleted] > 0 {
		meta += fmt.Sprintf("  ·  %d done", counts[db.RecapStatusCompleted])
	}
	if open := counts[db.RecapStatusQueued] + counts[db.RecapStatusRunning]; open > 0 {
		meta += fmt.Sprintf("  ·  %d open", open)
	}
	if counts[db.RecapStatusFailed] > 0 {
		meta += fmt.Sprintf("  ·  %d failed", counts[db.RecapStatusFailed])
	}
	if len(m.recapItems) == 0 {
		return hintStyle.Width(width).MaxWidth(width).Render("recaps  ·  no records yet")
	}
	latest := m.recapItems[0]
	status := strings.TrimSpace(latest.Status)
	if status == "" {
		status = "unknown"
	}
	label := fmt.Sprintf("recaps  ·  %s  ·  messages %d-%d  ·  %s", status, latest.SourceStartSeq, latest.SourceEndSeq, meta)
	if latest.Error != "" {
		label += "  ·  error"
	}
	color := lipgloss.Color(recapStatusColor(status))
	rail := lipgloss.NewStyle().Foreground(color).Render("│")
	hint := hintStyle.Render("  enter/→ context")
	labelWidth := max(1, width-lipgloss.Width("  "+rail+" ")-lipgloss.Width(hint))
	content := lipgloss.NewStyle().Foreground(color).Bold(true).Render(
		truncateRunes(label, labelWidth),
	)
	line := "  " + rail + " " + content + hint
	lines := []string{line}
	if latest.Error != "" {
		errorLine := lipgloss.NewStyle().Foreground(color).Render(
			truncateRunes("  "+rail+" error="+singleLine(latest.Error), width),
		)
		lines = append(lines, errorLine)
	}
	line = strings.Join(lines, "\n")
	if selected {
		return lipgloss.NewStyle().Width(width).MaxWidth(width).Background(theme.ColorBorder()).Render(line)
	}
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(line)
}

func recapStatusColor(status string) string {
	switch status {
	case db.RecapStatusCompleted:
		return theme.Current().Good
	case db.RecapStatusFailed:
		return theme.Current().Danger
	case db.RecapStatusQueued, db.RecapStatusRunning:
		return theme.Current().Accent
	default:
		return theme.Current().Mute
	}
}

func (m Model) openSelectedRecapDetail() (Model, tea.Cmd) {
	if len(m.recapItems) == 0 {
		return m, nil
	}
	m.recapSelected = true
	m.recapDetailRecord = m.recapItems[0]
	m.recapDetailMode = true
	m = m.setFocus(focusSubagentLog)
	m.recapDetailVp.SetContent(m.recapDetailContent(m.recapDetailRecord))
	m.recapDetailVp.GotoTop()
	return m.resizeRecapDetail(), nil
}

func (m Model) recapDetailContent(record db.RecapRecord) string {
	var b strings.Builder
	fmt.Fprintf(&b, "status: %s\nsource messages: %d-%d\nmodel: %s\n\n", record.Status, record.SourceStartSeq, record.SourceEndSeq, record.Model)
	if record.Error != "" {
		b.WriteString("error: ")
		b.WriteString(singleLine(record.Error))
		b.WriteString("\n\n")
	}
	sections := []struct {
		label    string
		artifact db.RecapArtifact
	}{
		{label: "SUMMARY", artifact: record.Artifacts.Sessions},
		{label: "QUESTIONS", artifact: record.Artifacts.Questions},
		{label: "THINGS TO AVOID", artifact: record.Artifacts.ThingsToAvoid},
	}
	for _, section := range sections {
		if section.artifact.Path == "" {
			continue
		}
		body, err := m.readRecapArtifact(section.artifact.Path)
		if err != nil {
			body = "unable to read artifact: " + err.Error()
		} else {
			body = stripRecapFrontMatter(body)
		}
		b.WriteString(m.recapArtifactPanel(section.label, section.artifact.Path, body))
		b.WriteString("\n\n")
	}
	if b.Len() == 0 {
		return "no recap context"
	}
	return strings.TrimSuffix(b.String(), "\n\n")
}

func (m Model) recapArtifactPanel(label, path, body string) string {
	width := max(1, m.recapDetailVp.Width())
	innerWidth := max(1, width-cardHorzPad)
	border := theme.ColorGood()
	background := theme.ColorEditPanel()
	switch label {
	case "QUESTIONS":
		border = theme.ColorAssistantBorder()
		background = theme.ColorAssistantPanel()
	case "THINGS TO AVOID":
		border = theme.ColorDanger()
		background = theme.ColorEditDelBg()
	}
	header := lipgloss.NewStyle().Bold(true).Foreground(border).Render(label + "  ·  " + path)
	rendered := markdown.Render(body, innerWidth)
	content := header
	if strings.TrimSpace(rendered) != "" {
		content += "\n" + rendered
	}
	return transcriptPanel(content, width, background, border)
}

func stripRecapFrontMatter(body string) string {
	lines := strings.Split(body, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return strings.TrimSpace(body)
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		}
	}
	return strings.TrimSpace(body)
}

func (m Model) readRecapArtifact(relativePath string) (string, error) {
	root, err := filepath.Abs(m.workdir)
	if err != nil {
		return "", fmt.Errorf("resolve workdir: %w", err)
	}
	pathCandidates := []string{filepath.FromSlash(relativePath)}
	if !strings.HasPrefix(filepath.ToSlash(relativePath), "knowledge-base/recaps/") {
		pathCandidates = append(pathCandidates, filepath.Join("knowledge-base", "recaps", filepath.FromSlash(relativePath)))
	}
	for _, candidate := range pathCandidates {
		path := filepath.Join(root, candidate)
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return "", fmt.Errorf("artifact path escapes workspace")
		}
		body, err := os.ReadFile(path)
		if err == nil {
			return string(body), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
	}
	return "", os.ErrNotExist
}

// subagentDrawerCounts returns live / ok / failed / total for drawer chrome.
func (m Model) subagentDrawerCounts() (live, ok, failed, total int) {
	total = len(m.subagentItems)
	for _, r := range m.subagentItems {
		switch {
		case r.Live:
			live++
		case isFailedSubStatus(r.Status):
			failed++
		case subagent.IsTerminalStatus(r.Status):
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
		b.WriteString(m.subagentDrawerRow(row, i == m.subagentCursor && !m.recapSelected, width))
	}
	return b.String()
}

func (m Model) subagentDrawerRow(row subagentRow, selected bool, width int) string {
	prefix := "  "
	if selected {
		prefix = "▸ "
	}
	name := firstNonEmptyStr(row.Name, row.ID)
	if row.Role != "" {
		name += "  ·  " + row.Role
	}
	status := firstNonEmptyStr(strings.TrimSpace(row.Status), "unknown")
	leftText := status + "  " + name
	meta := m.subagentRowMetadata(row)
	if row.Model != "" || row.Variant != "" {
		meta = "thinking: " + firstNonEmptyStr(row.Variant, "?") + "  " + firstNonEmptyStr(row.Model, "model?") + "  " + formatCost(row.Cost)
	}
	meta = singleLine(meta)

	available := max(1, width-subagentDrawerRowChrome)
	leftNeeded := lipgloss.Width(prefix) +
		lipgloss.Width(theme.StatusDiamond) +
		subagentDrawerColumnGap +
		lipgloss.Width(leftText)
	metaNeeded := lipgloss.Width(meta)

	leftWidth := min(
		leftNeeded,
		max(subagentDrawerMinLeftWidth, available-subagentDrawerMinMetaWidth),
	)
	metaWidth := min(
		metaNeeded,
		max(
			subagentDrawerMinMetaWidth,
			available-leftWidth-subagentDrawerRowChrome-subagentDrawerColumnGap,
		),
	)
	if leftWidth+metaWidth > available {
		metaWidth = max(subagentDrawerMinMetaWidth, available-leftWidth)
	}
	activityWidth := max(0, available-leftWidth-metaWidth)
	if width < subagentDrawerCompactWidth {
		activityWidth = max(1, width/subagentCompactActivityShare)
		metaWidth = max(1, width/subagentCompactMetadataShare)
		leftWidth = max(1, width-metaWidth-activityWidth-subagentDrawerColumnGap)
	}

	leftBudget := max(1, leftWidth-lipgloss.Width(prefix)-lipgloss.Width(theme.StatusDiamond)-subagentDrawerColumnGap)
	left := prefix + m.subagentDiamond(row) + "  " + truncateRunes(status+"  "+name, leftBudget)
	metaStr := truncateRunes(meta, metaWidth)
	activity := truncateRunes(singleLine(firstNonEmptyStr(strings.TrimSpace(row.Activity), "·")), activityWidth)

	// Build a line whose measured width is exactly the drawer width. This is
	// important: lipgloss wraps a row when its styled content exceeds Width.
	line := padRight(left, leftWidth) + "  " + padRight(metaStr, metaWidth) + "  " + padRight(activity, activityWidth)
	if selected {
		return drawerSelectedStyle.Width(width).MaxWidth(width).Render(line)
	}
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(line)
}

func singleLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func (m Model) subagentRowMetadata(row subagentRow) string {
	return formatCost(row.Cost) + "  " + formatCache(row.CacheHit, row.CacheMiss) + "  " +
		firstNonEmptyStr(row.Model, "model?") + "  " + firstNonEmptyStr(row.Variant, "thinking?")
}

func padRight(value string, width int) string {
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}

// subagentRowRight keeps the actual resolved child model and thinking level
// visible before the best-effort activity preview. It never substitutes a
// vendor/model guess.
func (m Model) subagentRowRight(row subagentRow, available int) string {
	available = max(1, available)
	model := firstNonEmptyStr(row.Model, "unavailable")
	variant := firstNonEmptyStr(row.Variant, "default")
	modelLabel := "model: " + model
	identity := modelLabel + "  ·  thinking: " + variant
	if row.Cost > 0 || row.CacheHit > 0 || row.CacheMiss > 0 {
		identity = formatCost(row.Cost) + "  ·  " + formatCache(row.CacheHit, row.CacheMiss) + "  ·  " + identity
	}
	if activity := strings.TrimSpace(row.Activity); activity != "" && activity != strings.TrimSpace(row.Status) {
		withActivity := identity + "  ·  " + activity
		if lipgloss.Width(withActivity) <= available {
			return withActivity
		}
	}
	return truncateRunes(identity, available)
}

// subagentDiamond is the status mark: throb when live, green when done, red on crash.
func (m Model) subagentDiamond(row subagentRow) string {
	style := lipgloss.NewStyle()
	switch {
	case row.Live || row.Status == string(subagent.StatusQueued) || row.Status == string(subagent.StatusRunning):
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

func (m Model) resizeRecapDetail() Model {
	atBottom := m.recapDetailVp.TotalLineCount() == 0 || m.recapDetailVp.AtBottom()
	off := m.recapDetailVp.YOffset()
	w := max(minPaneWidth, m.width)
	m.recapDetailVp.SetWidth(max(pickerVpMinWidth, w-1))
	m.recapDetailVp.SetHeight(m.subagentLogVPHeight())
	m.recapDetailVp.SetContent(m.recapDetailContent(m.recapDetailRecord))
	if atBottom {
		m.recapDetailVp.GotoTop()
	} else {
		m.recapDetailVp.SetYOffset(off)
	}
	return m
}

func (m Model) recapDetailScreen() string {
	w := max(1, m.width)
	h := max(1, m.height)
	record := m.recapDetailRecord
	status := firstNonEmptyStr(record.Status, "unknown")
	title := fmt.Sprintf("RECAP  ·  %s  ·  messages %d-%d", status, record.SourceStartSeq, record.SourceEndSeq)
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(recapStatusColor(status)))
	closeBtn := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("[x]")
	headerText := titleStyle.Render(truncateRunes(title, w-lipgloss.Width(closeBtn)-1))
	gap := max(1, w-lipgloss.Width(headerText)-lipgloss.Width(closeBtn))
	header := headerText + strings.Repeat(" ", gap) + closeBtn
	header = lipgloss.NewStyle().Width(w).Background(theme.ColorBg()).Render(header)

	vpH := m.subagentLogVPHeight()
	body := withScrollbar(m.recapDetailVp.View(), m.recapDetailVp.Width(), vpH,
		m.recapDetailVp.ScrollPercent(), m.recapDetailVp.TotalLineCount() > vpH)
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
	out := lipgloss.JoinVertical(lipgloss.Left, header, "", body, m.recapDetailJumpBarView(), footer)
	return lipgloss.NewStyle().Background(theme.ColorBg()).Width(w).Height(h).Render(out)
}

func (m Model) recapDetailJumpBarRow() int {
	return subagentLogHeaderRows + m.recapDetailVp.Height()
}

func (m Model) recapDetailJumpBarView() string {
	w := max(1, m.width)
	row := strings.Repeat(" ", w)
	if m.recapDetailVp.AtBottom() {
		return row
	}
	return spliceDisplay(row, lipgloss.NewStyle().Faint(true).Render(jumpDownArrow), w/centerDiv)
}

func (m Model) recapDetailCloseRect() (x0, y, x1 int, ok bool) {
	if !m.recapDetailMode {
		return 0, 0, 0, false
	}
	for i, line := range strings.Split(m.recapDetailScreen(), "\n") {
		plain := ansi.Strip(line)
		if !strings.Contains(plain, "RECAP") || !strings.Contains(plain, "[x]") {
			continue
		}
		start, end, found := displaySpan(plain, "[x]")
		if found {
			return max(0, start-1), i, end + 1, true
		}
	}
	return 0, 0, 0, false
}

func (m Model) recapDetailHit(x, y int, button tea.MouseButton) (Model, tea.Cmd, bool) {
	if !m.recapDetailMode || (button != tea.MouseLeft && button != tea.MouseRight) {
		return m, nil, false
	}
	if x0, cy, x1, ok := m.recapDetailCloseRect(); ok && y == cy && x >= x0 && x < x1 {
		return m.closeSubagentLogToDrawer(), nil, true
	}
	if button == tea.MouseLeft && y == m.recapDetailJumpBarRow() && !m.recapDetailVp.AtBottom() {
		m.recapDetailVp.GotoBottom()
		return m, nil, true
	}
	return m, nil, true
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
	if model := strings.TrimSpace(m.subagentSelected.Model); model != "" {
		title += "  ·  " + model
	}
	title += "  ·  thinking: " + firstNonEmptyStr(m.subagentSelected.Variant, "default")
	if m.subagentSelected.Status != "" {
		title += "  ·  " + m.subagentSelected.Status
	}
	if m.subagentSelected.Cost > 0 || m.subagentSelected.CacheHit > 0 || m.subagentSelected.CacheMiss > 0 {
		title += "  ·  " + formatCost(m.subagentSelected.Cost) + "  ·  " + formatCache(m.subagentSelected.CacheHit, m.subagentSelected.CacheMiss)
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
		"↑/↓ scroll  •  → next agent  •  ← back  •  ctrl+p thinking  •  ctrl+e tools  •  enter toggle",
		w,
	))
	out := lipgloss.JoinVertical(lipgloss.Left, header, "", body, m.subagentLogJumpBarView(), footer)
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

// subagentLogJumpBarRow is the transparent row immediately above the footer.
func (m Model) subagentLogJumpBarRow() int {
	return subagentLogHeaderRows + m.subagentLogVp.Height()
}

// subagentLogJumpBarView mirrors the main transcript jump row and stays
// visually empty unless the log is scrolled away from its live tail.
func (m Model) subagentLogJumpBarView() string {
	w := max(1, m.width)
	row := strings.Repeat(" ", w)
	if m.subagentLogVp.AtBottom() {
		return row
	}
	return spliceDisplay(row, lipgloss.NewStyle().Faint(true).Render(jumpDownArrow), w/centerDiv)
}

// subagentLogHit handles a mouse press on the full-screen log view.
// [x] returns to the drawer; clicks on thinking/tool blocks expand or collapse.
func (m Model) subagentLogHit(x, y int, button tea.MouseButton) (Model, tea.Cmd, bool) {
	if m.recapDetailMode {
		return m.recapDetailHit(x, y, button)
	}
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
	if button == tea.MouseLeft && y == m.subagentLogJumpBarRow() && !m.subagentLogVp.AtBottom() {
		m.subagentLogVp.GotoBottom()
		return m, nil, true
	}
	if button == tea.MouseLeft {
		if idx, ok := m.subagentLogItemIndexAtScreenY(y); ok {
			kind := m.subagentLogItems[idx].kind
			if kind == itemTool || kind == itemReasoning {
				m.subagentLogSelected = idx
				return m, nil, true
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
	out := make([]string, 0, len(m.subagentLogItems)*subagentLogRowFactor)
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
		// The sub-agent log is an audit surface: expanded tool bodies stay full.
		body := itemM.renderItemWithToolMode(it, i == m.subagentLogSelected, false, true)
		if it.kind != itemUser && it.kind != itemNote {
			body = itemM.withWorkRail(body, false)
		}
		out = append(out, body)
	}
	return out
}

func (m Model) updateSubagentPickerKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	if m.subagentLogMode {
		if m.recapDetailMode {
			return m.updateRecapDetailKey(key)
		}
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
		case tea.KeyRight:
			if !empty {
				return m.updateKey(key)
			}
			return m.expandSubagentDrawer(), nil
		case tea.KeyLeft:
			if !empty {
				return m.updateKey(key)
			}
			return m.closeSubagentPicker(), nil
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
		if m.recapSelected {
			if len(m.subagentItems) > 0 {
				m.recapSelected = false
				m.subagentCursor = 0
			}
		} else if m.subagentCursor < len(m.subagentItems)-1 {
			m.subagentCursor++
			m = m.resizeSubagentDrawer()
		}
		return m, nil
	case tea.KeyUp:
		if !m.recapSelected && m.subagentCursor == 0 && len(m.recapItems) > 0 {
			m.recapSelected = true
			m = m.resizeSubagentDrawer()
		} else if !m.recapSelected && m.subagentCursor > 0 {
			m.subagentCursor--
			m = m.resizeSubagentDrawer()
		}
		return m, nil
	case 'j':
		if !empty {
			return m.updateKey(key)
		}
		if m.recapSelected {
			if len(m.subagentItems) > 0 {
				m.recapSelected = false
				m.subagentCursor = 0
			}
		} else if m.subagentCursor < len(m.subagentItems)-1 {
			m.subagentCursor++
			m = m.resizeSubagentDrawer()
		}
		return m, nil
	case 'k':
		if !empty {
			return m.updateKey(key)
		}
		if !m.recapSelected && m.subagentCursor == 0 && len(m.recapItems) > 0 {
			m.recapSelected = true
			m = m.resizeSubagentDrawer()
		} else if !m.recapSelected && m.subagentCursor > 0 {
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
		if m.recapSelected {
			return m.openSelectedRecapDetail()
		}
		return m.openSelectedSubagentLog()
	case tea.KeyRight:
		if empty {
			if m.recapSelected {
				return m.openSelectedRecapDetail()
			}
			return m.openSelectedSubagentLog()
		}
		return m.updateKey(key)
	case tea.KeyLeft:
		if empty {
			return m.closeSubagentPicker(), nil
		}
		return m.updateKey(key)
	case 'q', 'Q', 'x', 'X':
		if empty {
			return m.closeSubagentPicker(), nil
		}
		return m.updateKey(key)
	case 'd', 'D':
		if empty {
			if m.recapSelected {
				return m, nil
			}
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
	if m.recapDetailMode {
		return m.updateRecapDetailKey(key)
	}
	if key.Mod.Contains(tea.ModCtrl) {
		switch key.Code {
		case 'e', 'E':
			return m.toggleAllSubagentLogKind(itemTool), nil
		case 'p', 'P':
			return m.toggleAllSubagentLogKind(itemReasoning), nil
		}
	}
	switch key.Code {
	case tea.KeyEscape, 'q', 'Q', 'x', 'X':
		return m.closeSubagentLogToDrawer(), nil
	case tea.KeyLeft:
		return m.closeSubagentLogToDrawer(), nil
	case tea.KeyRight:
		if m.subagentCursor < len(m.subagentItems)-1 {
			m.subagentCursor++
			return m.openSelectedSubagentLog()
		}
		return m, nil
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
	case tea.KeyEnd:
		m.subagentLogVp.GotoBottom()
		return m, nil
	case tea.KeyEnter:
		return m.toggleSubagentLogSelected(), nil
	case 'd', 'D':
		return m.closeSubagentPicker(), nil
	}
	return m, nil
}

func (m Model) updateRecapDetailKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	switch key.Code {
	case tea.KeyEscape, 'q', 'Q', 'x', 'X', tea.KeyLeft:
		return m.closeSubagentLogToDrawer(), nil
	case tea.KeyDown, 'j':
		m.recapDetailVp.ScrollDown(1)
	case tea.KeyUp, 'k':
		m.recapDetailVp.ScrollUp(1)
	case tea.KeyPgDown:
		m.recapDetailVp.PageDown()
	case tea.KeyPgUp:
		m.recapDetailVp.PageUp()
	case tea.KeyEnd:
		m.recapDetailVp.GotoBottom()
	case 'd', 'D':
		return m.closeSubagentPicker(), nil
	}
	return m, nil
}

// closeSubagentLogToDrawer returns from a full-screen child log without
// closing the sub-agent drawer, so another child can be selected immediately.
func (m Model) closeSubagentLogToDrawer() Model {
	wasRecap := m.recapDetailMode
	m = m.setFocus(focusSubagents)
	m.subagentLogItems = nil
	m.subagentLogSelected = -1
	m.recapDetailMode = false
	m.recapSelected = wasRecap
	m.subagentDrawerCompact = false
	m = m.reloadSubagentRows()
	return m.resizeSubagentDrawer()
}

func (m Model) openSelectedSubagentLog() (Model, tea.Cmd) {
	if m.subagentCursor < 0 || m.subagentCursor >= len(m.subagentItems) {
		return m, nil
	}
	row := m.subagentItems[m.subagentCursor]
	m.recapSelected = false
	m.recapDetailMode = false
	m.subagentSelected = row
	m.subagentLogItems = m.loadSubagentLogItems(row)
	m.subagentLogSelected = m.lastSubagentLogMetaIndex()
	m = m.setFocus(focusSubagentLog)
	// A newly opened log starts from a clean viewport so resizeSubagentLogCard
	// follows the live tail even if the previous agent log was scrolled up.
	m.subagentLogVp.SetContent("")
	m.subagentLogVp.GotoTop()
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
	res, err := projectSession(m.store, sessID, projectOpts{
		CollapseReasoning: false,
		SkipEmptyText:     true,
	})
	if err != nil {
		return []transcriptItem{{kind: itemNote, text: "failed to load messages: " + err.Error()}}
	}
	if len(res.items) == 0 {
		if row.Summary != "" {
			return []transcriptItem{{kind: itemNote, text: row.Summary}}
		}
		return []transcriptItem{{kind: itemNote, text: "empty sub-agent transcript"}}
	}
	return res.items
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

	out := make([]string, 0, len(m.subagentLogItems)*subagentLogRowFactor)
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
		// The sub-agent log is an audit surface: expanded tool bodies stay full.
		body := itemM.renderItemWithToolMode(it, i == m.subagentLogSelected, false, true)
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

func (m Model) toggleAllSubagentLogKind(kind itemKind) Model {
	anyOpen := false
	for _, it := range m.subagentLogItems {
		if it.kind == kind && !it.collapsed {
			anyOpen = true
			break
		}
	}
	for i, it := range m.subagentLogItems {
		if it.kind == kind {
			it.collapsed = anyOpen
			m.subagentLogItems[i] = it
		}
	}
	m.subagentLogSelected = m.lastSubagentLogKindIndex(kind)
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
// ("sub-agents · …"). Full list: collapse to summary. Compact: expand list.
func (m Model) subagentHeaderAt(y int) bool {
	top, ok := m.subagentHeaderScreenY()
	return ok && y == top
}

func (m Model) recapRowAt(y int) bool {
	if !m.subagentPickerMode || m.subagentLogMode || len(m.recapItems) == 0 {
		return false
	}
	for i, line := range m.paintedLines() {
		if i == y && strings.Contains(ansi.Strip(line), "recaps  ·") {
			return true
		}
	}
	return false
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
