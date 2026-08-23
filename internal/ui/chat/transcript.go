package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
	"github.com/chinmay-sawant/lazykoder/internal/tools/edit"
	"github.com/chinmay-sawant/lazykoder/internal/ui/markdown"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

type itemKind int

const (
	itemUser itemKind = iota
	itemAssistant
	itemReasoning
	itemTool
	itemNote
)

const (
	maxToolBodyLines  = 100
	maxToolBodyRunes  = 6000
	toolBodyHeadLines = 70
	toolBodyTailLines = maxToolBodyLines - toolBodyHeadLines - 1

	// transcriptLinearPad is the 2-col inset applied to a transcript line.
	transcriptLinearPad = 2
	// diffStatParts is the max "+, -" segments in a diff stat.
	diffStatParts = 2
	// diffNumMinWidth is the minimum gutter number-column width.
	diffNumMinWidth = 2
	// diffBodyMinWidth is the floor width of a diff body column.
	diffBodyMinWidth = 8
	// diffHunkParts is the number of "-/+ range" fields in a hunk header.
	diffHunkParts = 2
	// diffEllipsisPad is the 2-col space reserved for the trailing ellipsis.
	diffEllipsisPad = 2
)

type transcriptItem struct {
	kind      itemKind
	text      string
	collapsed bool
	when      int64
	tool      db.ToolCall
	part      db.Part
}

const (
	roleYou       = "you"
	roleAssistant = "assistant"
	thinkingLabel = "thinking"
	maxToolTitle  = 72
	workBracket   = "["
	workRail      = "│"
	workRailCols  = 2
	streamCursor  = "▌"
	// memoryScanFrames follows the Braille loader from temp/4_load_animations.go.
	memoryScanFrames = "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏"
)

func (m *Model) replay(sessionID string) {
	res, err := projectSession(m.store, sessionID, projectOpts{
		CollapseReasoning:   true,
		SeedInputHistory:    true,
		IncludeCompactNotes: true,
		CollectUsage:        true,
	})
	if err != nil {
		m.err = "chat: " + err.Error()
		return
	}
	m.items = append(m.items, res.items...)
	m.inputHistory = append(m.inputHistory, res.history...)
	for _, u := range res.usage {
		m.applyUsage(u.part, u.modelID)
	}
	for _, p := range res.compactMeter {
		m.applyCompactMeter(p)
	}
	m.bumpTokenFloor()
	m.syncTranscript()
}

func (m *Model) applyUsage(p db.Part, modelID string) {
	var in, out, total int64
	if p.TokensInput != nil {
		in = *p.TokensInput
	}
	if p.TokensOutput != nil {
		out = *p.TokensOutput
	}
	if p.TokensTotal != nil {
		total = *p.TokensTotal
	}
	if total == 0 {
		total = in + out
	}
	// Live fill is the latest request size, not a session peak. Prefer
	// input (what occupied the window). Skip empty blobs so a missing
	// usage row does not wipe the meter.
	fill := in
	if fill == 0 {
		fill = total
	}
	if fill > 0 {
		m.tokensUsed = fill
	}
	var hit int64
	if p.TokensCacheRead != nil {
		hit = *p.TokensCacheRead
	}
	m.addStepCost(p, modelID)

	miss := cacheMissTokens(in, hit)
	if hit > 0 {
		m.cacheHit += hit
	}
	if miss > 0 {
		m.cacheMiss += miss
	}
	if !m.busy {
		return
	}
	gen := out
	if gen == 0 && p.TokensReasoning != nil {
		gen = *p.TokensReasoning
	}
	if gen > 0 {
		m.turnGenTokens += gen
	}
}

func (m *Model) bumpTokenFloor() {
	est := estimateModelContext(m.items)
	if est <= 0 {
		return
	}
	last := lastCompactIndex(m.items)
	if last >= 0 && last == len(m.items)-1 {
		// Just compacted (or replay ended on a checkpoint). The meter is
		// the rebuilt request, not the old peak.
		m.tokensUsed = est
		return
	}
	if est > m.tokensUsed {
		m.tokensUsed = est
	}
}

func lastCompactIndex(items []transcriptItem) int {
	last := -1
	for i, it := range items {
		if it.part.Type == agent.CompactPartType {
			last = i
		}
	}
	return last
}

// estimateModelContext is the fill the next provider call would see:
// after a compact, summary + tail (and any newer turns), not the painted
// pre-compact transcript.
func estimateModelContext(items []transcriptItem) int64 {
	last := lastCompactIndex(items)
	if last < 0 {
		return estimateTokens(items)
	}
	env := agent.CompactEnvelope{}
	if items[last].part.Text != nil {
		env = agent.ParseCompactText(*items[last].part.Text)
	}
	if env.TokensAfter > 0 {
		return env.TokensAfter + estimateTokens(items[last+1:])
	}
	n := agent.EstimateTokens(env.Summary)
	include := env.TailStartMessageID == ""
	for i, it := range items {
		if i == last || it.part.Type == agent.CompactPartType {
			continue
		}
		if !include && it.part.MessageID == env.TailStartMessageID {
			include = true
		}
		if include {
			n += estimateTokens([]transcriptItem{it})
		}
	}
	return n
}

func (m *Model) addStepCost(p db.Part, modelID string) {
	m.sessionCost += stepCostUSD(p, m.modelInfos, m.usageModelID(modelID))
}

func (m *Model) recomputeSessionCost() {
	if m.store == nil || m.session == nil || len(m.modelInfos) == 0 {
		return
	}
	m.sessionCost = sessionUsageOf(m.store, m.modelInfos, m.session.ID).Cost
}

func (m Model) usageModelID(modelID string) string {
	if strings.TrimSpace(modelID) != "" {
		return modelID
	}
	return m.modelLabel()
}

type sessionUsage struct {
	Cost      float64
	CacheHit  int64
	CacheMiss int64
}

func sessionUsageOf(store *db.Store, infos []modelscache.Info, sessionID string) sessionUsage {
	var out sessionUsage
	if store == nil || sessionID == "" {
		return out
	}
	ctx := context.Background()
	sess, err := store.GetSession(ctx, sessionID)
	if err != nil {
		return out
	}
	msgs, err := store.ListMessages(ctx, sessionID)
	if err != nil {
		return out
	}
	for _, msg := range msgs {
		parts, err := store.ListParts(ctx, msg.ID)
		if err != nil {
			continue
		}
		modelID := msg.ModelID
		if modelID == "" {
			modelID = sess.Model
		}
		for _, p := range parts {
			if p.Type != "step-finish" {
				continue
			}
			out.Cost += stepCostUSD(p, infos, modelID)
			var in, hit int64
			if p.TokensInput != nil {
				in = *p.TokensInput
			}
			if p.TokensCacheRead != nil {
				hit = *p.TokensCacheRead
			}
			if hit > 0 {
				out.CacheHit += hit
			}
			if miss := cacheMissTokens(in, hit); miss > 0 {
				out.CacheMiss += miss
			}
		}
	}
	return out
}

func stepCostUSD(p db.Part, infos []modelscache.Info, modelID string) float64 {
	if p.Cost != nil && *p.Cost > 0 {
		return *p.Cost
	}
	var in, out, total, hit, written int64
	if p.TokensInput != nil {
		in = *p.TokensInput
	}
	if p.TokensOutput != nil {
		out = *p.TokensOutput
	}
	if p.TokensTotal != nil {
		total = *p.TokensTotal
	}
	if p.TokensCacheRead != nil {
		hit = *p.TokensCacheRead
	}
	if p.TokensCacheWrite != nil {
		written = *p.TokensCacheWrite
	}
	if in > 0 || out > 0 || hit > 0 || written > 0 {
		return costUSDFor(infos, modelID, in, out, hit, written)
	}
	if total > 0 {
		return costUSDFor(infos, modelID, 0, total, 0, 0)
	}
	return 0
}

func costUSDFor(infos []modelscache.Info, modelID string, input, output, cacheRead, cacheWrite int64) float64 {
	info, ok := modelscache.InfoOf(infos, modelID)
	if !ok {
		return 0
	}
	return info.CostUSD(input, output, cacheRead, cacheWrite)
}

func cacheMissTokens(input, hit int64) int64 {
	if hit <= 0 {
		return input
	}
	if input > hit {
		return input - hit
	}
	if input > 0 && input < hit {
		return input
	}
	return 0
}

// estimateTokens sums agent.EstimateTokens over transcript text (and tool
// outputs) so the fill meter matches Compaction's chars/4 helper.
func estimateTokens(items []transcriptItem) int64 {
	var n int64
	for _, it := range items {
		n += agent.EstimateTokens(it.text)
		if it.kind == itemTool && it.tool.Output != nil {
			n += agent.EstimateTokens(*it.tool.Output)
		}
	}
	return n
}

func (m *Model) syncTranscript() {
	// Leave columns for scrollbar (+ user-nav rail when present).
	m.transcript.SetWidth(m.transcriptContentWidth())
	m.transcript.SetHeight(max(1, m.transcriptRenderHeight()))
	atBottom := m.transcript.AtBottom()
	yOffset := m.transcript.YOffset()
	content := m.transcriptContent()
	if c := m.renderCache; c == nil || c.content != content {
		m.transcript.SetContent(content)
		if c != nil {
			c.content = content
		}
	}
	if atBottom {
		m.transcript.GotoBottom()
		return
	}
	m.transcript.SetYOffset(yOffset)
}

func (m Model) transcriptContent() string {
	return strings.Join(m.renderedItems(), "\n")
}

// highlightTranscriptSelection paints the drag selection onto the viewport
// output, mapping content rows back to the visible window via the viewport
// offset. Runs at view time so the underlying content stays stable during a
// drag and SetContent can be skipped on every motion event.
func (m Model) highlightTranscriptSelection(view string, yOffset int) string {
	start, end := m.selection.bounds()
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

func (m Model) renderedItems() []string {
	return m.renderedItemsMemo()
}

func (m Model) buildRenderedItems() []string {
	out := make([]string, 0, len(m.items))
	var itemKeys []uint64
	var itemRows []string
	if m.renderCache != nil {
		itemKeys = make([]uint64, len(m.items))
		itemRows = make([]string, len(m.items))
	}
	for i, it := range m.items {
		if i > 0 && (it.kind == itemUser || it.kind == itemAssistant) {
			out = append(out, "")
		}
		body := ""
		if m.renderCache != nil {
			key := m.itemRenderKey(i, it)
			itemKeys[i] = key
			if i < len(m.renderCache.itemKeys) && m.renderCache.itemKeys[i] == key {
				body = m.renderCache.itemRows[i]
			} else {
				body = m.railedItem(i, m.renderItemCopy(i, it))
				m.renderCache.itemRenders++
			}
			itemRows[i] = body
		} else {
			body = m.railedItem(i, m.renderItemCopy(i, it))
		}
		out = append(out, body)
	}
	if m.renderCache != nil {
		m.renderCache.itemKeys = itemKeys
		m.renderCache.itemRows = itemRows
	}
	return out
}

func (m Model) renderItemCopy(idx int, it transcriptItem) string {
	item := m
	if m.itemUsesWorkRail(idx) {
		item.railInset = workRailCols
	}
	streaming := m.busy && it.kind == itemReasoning && !it.collapsed && idx == m.lastReasoningIndex()
	return item.renderItem(it, idx == m.selectedItem, streaming)
}

func (m Model) railedItem(idx int, body string) string {
	if !m.itemUsesWorkRail(idx) {
		return body
	}
	return m.withWorkRail(body, m.itemInLiveTurn(idx))
}

func (m Model) itemUsesWorkRail(idx int) bool {
	if idx < 0 || idx >= len(m.items) {
		return false
	}
	if m.items[idx].kind == itemUser || m.items[idx].kind == itemNote {
		return false
	}
	return m.turnOwner(idx) >= 0
}

func (m Model) turnOwner(idx int) int {
	for i := idx; i >= 0; i-- {
		if m.items[i].kind == itemUser {
			return i
		}
	}
	return -1
}

func (m Model) itemInLiveTurn(idx int) bool {
	if !m.busy {
		return false
	}
	owner := m.turnOwner(idx)
	if owner < 0 {
		return false
	}
	liveUser := m.turnItemFrom - 1
	if liveUser < 0 {
		for i := len(m.items) - 1; i >= 0; i-- {
			if m.items[i].kind == itemUser {
				liveUser = i
				break
			}
		}
	}
	return owner == liveUser
}

func (m Model) workRailMark() string {
	if m.busy && m.pulseOn {
		return m.plasmaBlob()
	}
	return m.workRailLive(false)
}

func (m Model) workRailLive(throb bool) string {
	style := lipgloss.NewStyle().Foreground(theme.ColorAssistantBorder())
	if throb {
		style = lipgloss.NewStyle().Foreground(theme.PulseAssistant(m.pulseT()))
	}
	return style.Render(workRail)
}

func (m Model) withWorkRail(s string, live bool) string {
	rail := m.workRailLive(live && m.busy && m.pulseOn) + " "
	if s == "" {
		return rail
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = rail + line
	}
	return strings.Join(lines, "\n")
}

// frameUserPrompt draws one big open bracket on the left of the sent prompt:
// the corner curls sit on the first and last lines, and every line between
// them carries a side rail. The right edge stays open. The width follows the
// longest line so every line stays inside the frame, and it is capped at the
// pane width minus the marker and space columns so the frame never spills
// past the right edge.
func frameUserPrompt(text string, width int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	innerW := 0
	for _, line := range lines {
		if w := lipgloss.Width(line); w > innerW {
			innerW = w
		}
	}
	if width > 0 {
		innerW = min(innerW, max(1, width-transcriptLinearPad))
	}
	innerW = max(1, innerW)
	row := func(line, marker string) string {
		line = ansi.Truncate(line, innerW, "…")
		return marker + " " + line + strings.Repeat(" ", innerW-lipgloss.Width(line))
	}
	b := &strings.Builder{}
	for i, line := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		switch {
		case i == 0:
			b.WriteString(row(line, "╭"))
		case i == len(lines)-1:
			b.WriteString(row(line, "╰"))
		default:
			b.WriteString(row(line, workRail))
		}
	}
	return b.String()
}

func itemTime(messageMS int64, partMS int64) int64 {
	if partMS > 0 {
		return partMS
	}
	if messageMS > 0 {
		return messageMS
	}
	return time.Now().UnixMilli()
}

func (m Model) roleLine(label string, when int64) string {
	style := roleStyle
	switch label {
	case roleYou:
		style = userRoleStyle
	case roleAssistant:
		style = assistantRoleStyle
	}
	return m.alignMeta(style.Render(label), formatClock(when))
}

// metaBand renders the role/timestamp line for assistant turns as a
// full-pane-wide strip painted with the assistant panel color, so the blue
// surface runs edge to edge instead of stopping at the text column.
func (m Model) metaBand(label string, when int64) string {
	style := roleStyle
	switch label {
	case roleYou:
		style = userRoleStyle
	case roleAssistant:
		style = assistantRoleStyle
	}
	left := style.Background(theme.ColorAssistantPanel()).Render(label)
	stamp := hintStyle.Background(theme.ColorAssistantPanel()).Render(formatClock(when))
	width := m.metaWidth()
	rw := lipgloss.Width(stamp)
	lw := lipgloss.Width(left)
	gap := width - lw - rw
	gapStyle := lipgloss.NewStyle().Background(theme.ColorAssistantPanel())
	if gap < 1 {
		return left + gapStyle.Render(" ") + stamp
	}
	return left + gapStyle.Render(strings.Repeat(" ", gap)) + stamp
}

// transcriptPanel gives each conversational turn a quiet surface and rounded
// border. Keeping the role line outside the panel preserves the activity rail
// and makes the speaker boundary obvious even in a busy transcript.
func transcriptPanel(body string, width int, background, border color.Color) string {
	panelBorder := lipgloss.Border{
		Left:  "│",
		Right: " ",
	}
	body = keepBackground(body, background)
	return lipgloss.NewStyle().
		Background(background).
		Border(panelBorder).
		BorderForeground(border).
		BorderBackground(background).
		PaddingLeft(1).
		Width(max(1, width)).
		Render(body)
}

func (m Model) alignMeta(left, stamp string) string {
	if stamp == "" {
		return left
	}
	right := hintStyle.Render(stamp)
	width := m.metaWidth()
	rw := lipgloss.Width(right)
	maxLeft := width - rw - 1
	if maxLeft < 1 {
		return right
	}
	if lipgloss.Width(left) > maxLeft {
		left = ansi.Cut(left, 0, max(1, maxLeft-1)) + "…"
	}
	gap := width - lipgloss.Width(left) - rw
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) metaWidth() int {
	w := m.width
	if tw := m.transcript.Width(); tw > 0 {
		w = tw
	}
	if m.railInset > 0 {
		w -= m.railInset
	}
	return max(minPaneWidth, w)
}

// stampZoneWidth is the right-hand margin the role line reserves for the
// timestamp stamp, plus one column of breathing room. Items without a stamp
// reserve nothing.
func (m Model) stampZoneWidth(when int64) int {
	if when <= 0 {
		return 0
	}
	return lipgloss.Width(formatClock(when)) + 1
}

// contentWidth is the pane width available to message text: the meta row
// width minus the timestamp zone, so wrapped text never runs into the
// columns where the clock sits on role lines above it.
func (m Model) contentWidth(when int64) int {
	return max(minPaneWidth, m.metaWidth()-m.stampZoneWidth(when))
}

func (m Model) plainTranscriptRows() []string {
	return m.plainRowsMemo()
}

func (m *Model) applyPart(p db.Part) {
	switch p.Type {
	case "step-start":
		m.tpsSamples = nil
		m.stepMetrics = false
	case "text":
		if p.Text == nil {
			return
		}
		if m.pendingUser != "" && *p.Text == m.pendingUser {
			m.pendingUser = ""
			return
		}
		m.collapseLiveReasoning()
		m.upsertItem(itemAssistant, p, *p.Text, false)
	case "reasoning":
		if p.Text == nil {
			return
		}
		if !m.upsertExisting(p, *p.Text) {
			m.collapseLiveReasoning()
			m.items = append(m.items, transcriptItem{
				kind:      itemReasoning,
				text:      *p.Text,
				collapsed: !m.busy,
				when:      itemTime(0, p.TimeCreated),
				part:      p,
			})
		}
	case "step-finish":
		m.collapseLiveReasoning()
		m.applyUsage(p, m.model)
	case agent.CompactPartType:
		m.applyCompactNotice(p)
	}
	m.syncTranscript()
}

// applyCompactMeter updates fill/cache from a compaction part without painting.
func (m *Model) applyCompactMeter(p db.Part) {
	if p.Text == nil {
		return
	}
	env := agent.ParseCompactText(*p.Text)
	if env.TokensAfter > 0 {
		m.tokensUsed = env.TokensAfter
	}
	// Cache stats are since the last checkpoint, not the whole session.
	m.cacheHit = 0
	m.cacheMiss = 0
}

func (m *Model) applyCompactNotice(p db.Part) {
	m.applyCompactMeter(p)
	it := compactNoticeItem(p)
	for i := range m.items {
		if m.items[i].part.ID != "" && m.items[i].part.ID == p.ID {
			m.items[i].text = it.text
			m.items[i].part = p
			return
		}
	}
	it.when = itemTime(0, p.TimeCreated)
	m.items = append(m.items, it)
}

func (m *Model) upsertExisting(p db.Part, text string) bool {
	if p.ID == "" {
		return false
	}
	for i := range m.items {
		if m.items[i].part.ID != p.ID {
			continue
		}
		m.items[i].text = text
		m.items[i].part = p
		return true
	}
	return false
}

func (m *Model) upsertItem(kind itemKind, p db.Part, text string, collapsed bool) {
	if m.upsertExisting(p, text) {
		return
	}
	m.items = append(m.items, transcriptItem{
		kind:      kind,
		text:      text,
		collapsed: collapsed,
		when:      itemTime(0, p.TimeCreated),
		part:      p,
	})
}

func (m *Model) collapseLiveReasoning() {
	from := 0
	if m.turnItemFrom > 0 {
		from = m.turnItemFrom
	}
	for i := from; i < len(m.items); i++ {
		if m.items[i].kind == itemReasoning {
			m.items[i].collapsed = true
		}
	}
}

func (m *Model) applyTool(ev agent.Event) {
	tool := ev.Tool
	part := ev.Part
	if tool.Name == "" && part.ToolName != "" {
		tool.Name = part.ToolName
	}
	if tool.Name == "" {
		return
	}
	m.collapseLiveReasoning()
	status := tool.Status
	if status == "" && part.ToolStatus != "" {
		status = part.ToolStatus
		tool.Status = status
	}
	if status == "" {
		status = "pending"
		tool.Status = status
	}
	when := itemTime(0, part.TimeCreated)
	if tool.TimeStart != nil {
		when = *tool.TimeStart
	}
	dbTool := dbToolFromDelta(tool)
	dbPart := dbPartFromDelta(part)
	// Edit opens by default so the diff is visible; user can collapse with e.
	collapsed := tool.Name != "edit"
	item := transcriptItem{kind: itemTool, collapsed: collapsed, when: when, tool: dbTool, part: dbPart}
	if status == "" || status == "pending" {
		m.items = append(m.items, item)
		m.lastTool = len(m.items) - 1
		m.selectedItem = m.lastTool
		m.syncTranscript()
		return
	}
	if idx := m.findToolItemIndex(dbTool, dbPart); idx >= 0 {
		// Keep the user's open/closed choice across status updates.
		item.collapsed = m.items[idx].collapsed
		m.items[idx] = item
		m.lastTool = idx
	} else {
		m.items = append(m.items, item)
		m.lastTool = len(m.items) - 1
	}
	m.syncTranscript()
}

// findToolItemIndex locates an in-flight tool row by part/call id so completed
// updates do not attach to the wrong card when several tools run in one turn.
func (m Model) findToolItemIndex(tc db.ToolCall, part db.Part) int {
	partID := tc.PartID
	if partID == "" {
		partID = part.ID
	}
	if partID != "" {
		for i := range m.items {
			if m.items[i].kind != itemTool {
				continue
			}
			if m.items[i].tool.PartID == partID || m.items[i].part.ID == partID {
				return i
			}
		}
	}
	if tc.CallID != "" {
		for i := range m.items {
			if m.items[i].kind == itemTool && m.items[i].tool.CallID == tc.CallID {
				return i
			}
		}
	}
	if m.lastTool >= 0 && m.lastTool < len(m.items) && m.items[m.lastTool].kind == itemTool {
		return m.lastTool
	}
	return -1
}

func (m Model) renderItem(it transcriptItem, selected bool, streaming bool) string {
	return m.renderItemWithToolMode(it, selected, streaming, false)
}

func (m Model) renderItemWithToolMode(it transcriptItem, selected bool, streaming, fullToolOutput bool) string {
	switch it.kind {
	case itemUser:
		// The user frame already provides a clear boundary. Add a subtle wash
		// without wrapping it in a second border, which keeps selection and
		// copy coordinates stable.
		body := lipgloss.NewStyle().Background(theme.ColorUserPanel()).Render(
			userStyle.Render(frameUserPrompt(it.text, m.contentWidth(it.when))))
		return m.roleLine(roleYou, it.when) + "\n" + body
	case itemAssistant:
		// The panel and its role band span the full pane width. The clock
		// lives on the band above the panel, so message text never shares a
		// row with it and nothing needs to be reserved for the stamp.
		panelWidth := max(1, m.metaWidth())
		innerWidth := max(1, panelWidth-cardHorzPad)
		rendered := markdown.Render(it.text, innerWidth)
		rendered = transcriptPanel(rendered, panelWidth, theme.ColorAssistantPanel(), theme.ColorAssistantBorder())
		return m.metaBand(roleAssistant, it.when) + "\n" + rendered
	case itemReasoning:
		marker := "▸"
		if !it.collapsed {
			marker = "▾"
		}
		head := m.alignMeta(reasoningStyle.Render(marker+" "+thinkingLabel), formatClock(it.when))
		if it.collapsed {
			return head
		}
		body := it.text
		if streaming {
			body += streamCursor
		}
		return head + "\n" + reasoningStyle.Render(ansi.Wrap(body, m.contentWidth(it.when), " "))
	case itemTool:
		return m.renderToolMode(it.tool, it.part, it.collapsed, it.when, fullToolOutput)
	case itemNote:
		return hintStyle.Render(it.text)
	}
	if selected {
		return it.text
	}
	return it.text
}

func (m Model) renderTool(tool db.ToolCall, part db.Part, collapsed bool, when int64) string {
	return m.renderToolMode(tool, part, collapsed, when, false)
}

// renderToolMode renders the main transcript with bounded expanded bodies.
// The full-body mode is reserved for the full-screen sub-agent audit log.
func (m Model) renderToolMode(tool db.ToolCall, part db.Part, collapsed bool, when int64, fullToolOutput bool) string {
	name := tool.Tool
	if name == "" && part.ToolName != nil {
		name = *part.ToolName
	}
	status := tool.Status
	if status == "" && part.ToolStatus != nil {
		status = *part.ToolStatus
	}
	if status == "" {
		status = "pending"
	}
	title := toolCommand(tool)
	if title == "" {
		title = name
	}
	// File tools: full path relative to the project folder (never truncated
	// here; alignMeta still trims only if the clock would collide).
	if name == "edit" || name == "write" || name == "read" {
		title = m.relWorkPath(title)
	} else {
		title = truncateRunes(title, maxToolTitle)
	}
	chevron := "▸"
	if !collapsed {
		chevron = "▾"
	}
	label := name
	if title != name {
		label = name + "  " + title
	}
	// Collapsed or open: still show +/− counts on the edit header.
	if name == "edit" {
		if add, del := diffStat(m.toolEditDiff(tool)); add+del > 0 {
			label += "  " + formatDiffStat(add, del)
		}
	}
	statusMark := m.toolStatusMark(status)
	left := statusMark + "  " + lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render(chevron+"  "+label)
	header := m.alignMeta(left, formatClock(when))
	bodyWidth := max(minPaneWidth, m.toolCardWidth())

	// Edit tools: full-width soft green/red panel when expanded.
	if name == "edit" {
		return m.renderEditTool(header, tool, collapsed, bodyWidth)
	}

	card := toolCardStyle.Width(bodyWidth).Background(theme.ColorBg())
	if collapsed {
		return card.Render(keepBackground(header, theme.ColorBg()))
	}
	body := []string{header}
	switch tool.Tool {
	case "write":
		if tool.Output != nil && *tool.Output != "" {
			preview := strings.TrimSuffix(*tool.Output, "\n")
			if !fullToolOutput && len([]rune(preview)) > 400 {
				preview = string([]rune(preview)[:400]) + "…"
			}
			body = append(body, toolOutputStyle.Width(bodyWidth).Render(strings.TrimSuffix(preview, "\n")))
		}
	default:
		if command := toolCommand(tool); command != "" {
			body = append(body, hintStyle.Width(bodyWidth).Render("  $ "+command))
		}
		if tool.Output != nil && *tool.Output != "" {
			output := strings.TrimSuffix(*tool.Output, "\n")
			if !fullToolOutput {
				output, _ = truncateToolOutputForView(output)
			}
			outputLabel := hintStyle.Width(bodyWidth).Render("  output")
			outputBox := toolOutputStyle.Width(bodyWidth).Render("  " + output)
			body = append(body, "", outputLabel, outputBox)
		}
	}
	return card.Render(keepBackground(strings.Join(body, "\n"), theme.ColorBg()))
}

// toolInFlight reports whether a tool-run status means the call has not
// finished yet, so its baton mark should animate instead of sitting on a fixed
// status color.
func toolInFlight(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending", "running", "in_progress", "in-progress":
		return true
	default:
		return false
	}
}

func (m Model) hasInFlightTools() bool {
	for _, it := range m.items {
		if it.kind == itemTool && toolInFlight(toolItemStatus(it)) {
			return true
		}
	}
	return false
}

// toolItemStatus resolves the effective status of a transcript tool item,
// preferring the live ToolCall status over the part snapshot.
func toolItemStatus(it transcriptItem) string {
	if it.tool.Status != "" {
		return it.tool.Status
	}
	if it.part.ToolStatus != nil {
		return *it.part.ToolStatus
	}
	return ""
}

// toolStatusMark uses a green baton spinner while a call is in flight and
// its first frame as the fixed status mark after the call finishes.
func (m Model) toolStatusMark(status string) string {
	style := lipgloss.NewStyle().Foreground(theme.StatusColor(status))
	frame := 0
	if toolInFlight(status) {
		frame = m.pulse
		style = style.Foreground(theme.ColorGood())
	}
	return style.Render(theme.StatusBatonFrame(frame))
}

// renderEditTool draws the edit tool as a full-width card with soft green/red
// row washes. Open by default; collapsed shows only the header (+/− stats).
func (m Model) renderEditTool(header string, tool db.ToolCall, collapsed bool, width int) string {
	width = max(minPaneWidth, width)
	panel := editCardStyle.Width(width)
	head := panel.Render(header)
	if collapsed {
		return head
	}
	diff := m.toolEditDiff(tool)
	if diff == "" {
		if tool.Output != nil && *tool.Output != "" {
			out := panel.Foreground(theme.ColorMute()).Render(strings.TrimSuffix(*tool.Output, "\n"))
			return head + "\n" + out
		}
		pending := panel.Foreground(theme.ColorMute()).Render("applying edit…")
		return head + "\n" + pending
	}
	return head + "\n" + renderDiff(diff, width)
}

func toolMetadataDiff(tc db.ToolCall) string {
	if tc.MetadataJSON == nil || *tc.MetadataJSON == "" {
		return ""
	}
	var meta struct {
		Diff     string `json:"diff"`
		FileDiff string `json:"filediff"`
	}
	if json.Unmarshal([]byte(*tc.MetadataJSON), &meta) != nil {
		return ""
	}
	if meta.Diff != "" {
		return meta.Diff
	}
	return meta.FileDiff
}

// toolEditDiff returns a unified diff for the edit card. Prefers a live
// recompute from the file + old/new args (correct line numbers even for
// historical tool rows that stored bad @@ -1 headers). Falls back to stored
// metadata, then a synthetic snippet-only diff.
func (m Model) toolEditDiff(tc db.ToolCall) string {
	args := parseEditArgs(tc)
	if d := m.recomputeEditDiff(args); d != "" {
		return d
	}
	if d := toolMetadataDiff(tc); d != "" {
		// Last resort for rows with no recoverable file state: relocate the
		// stored hunk headers by searching the file for the first anchor line.
		if fixed := m.relocateStoredDiff(d, args.FilePath); fixed != "" {
			return fixed
		}
		return d
	}
	if args.OldString == "" && args.NewString == "" {
		return ""
	}
	return syntheticEditDiff(args.OldString, args.NewString)
}

type editArgs struct {
	FilePath  string `json:"filePath"`
	OldString string `json:"oldString"`
	NewString string `json:"newString"`
}

func parseEditArgs(tc db.ToolCall) editArgs {
	var args editArgs
	if tc.InputJSON != "" {
		_ = json.Unmarshal([]byte(tc.InputJSON), &args)
	}
	if args.FilePath == "" && tc.Title != nil {
		args.FilePath = *tc.Title
	}
	return args
}

// recomputeEditDiff rebuilds a full-file unified diff with correct line
// numbers using the tool args and the current file on disk.
func (m Model) recomputeEditDiff(args editArgs) string {
	if m.workdir == "" || args.FilePath == "" || args.OldString == "" {
		return ""
	}
	abs := args.FilePath
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(m.workdir, abs)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return ""
	}
	cur := string(data)
	// Edit applied and still present: reconstruct pre-edit content.
	if args.NewString != "" && strings.Count(cur, args.NewString) == 1 {
		idx := strings.Index(cur, args.NewString)
		before := cur[:idx] + args.OldString + cur[idx+len(args.NewString):]
		return edit.UnifiedDiff(before, cur)
	}
	// File still has oldString (reverted or not yet applied): show prospective.
	if strings.Count(cur, args.OldString) == 1 {
		idx := strings.Index(cur, args.OldString)
		after := cur[:idx] + args.NewString + cur[idx+len(args.OldString):]
		return edit.UnifiedDiff(cur, after)
	}
	return ""
}

// relocateStoredDiff rewrites @@ hunk starts by finding the first content
// anchor of each hunk in the on-disk file. Used when recomputeEditDiff cannot
// reverse the edit (file changed further) but the stored body is still useful.
func (m Model) relocateStoredDiff(diff, filePath string) string {
	if m.workdir == "" || filePath == "" || diff == "" {
		return ""
	}
	abs := filePath
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(m.workdir, abs)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return ""
	}
	fileLines := strings.Split(string(data), "\n")
	// Map first 80 runes of each non-empty file line -> 1-based line number
	// (first occurrence). Enough to anchor typical hunk context.
	index := map[string]int{}
	for i, ln := range fileLines {
		key := strings.TrimRight(ln, "\r")
		if key == "" {
			continue
		}
		if _, ok := index[key]; !ok {
			index[key] = i + 1
		}
	}

	lines := strings.Split(strings.TrimRight(diff, "\n"), "\n")
	var out strings.Builder
	var hunkBody []string
	flush := func() {
		if len(hunkBody) == 0 {
			return
		}
		oldStart, newStart := 1, 1
		for _, hl := range hunkBody {
			var body string
			switch {
			case strings.HasPrefix(hl, "+++") || strings.HasPrefix(hl, "---"):
				continue
			case strings.HasPrefix(hl, "+"):
				body = hl[1:]
			case strings.HasPrefix(hl, "-"):
				body = hl[1:]
			case strings.HasPrefix(hl, " "):
				body = hl[1:]
			default:
				body = hl
			}
			body = strings.TrimRight(body, "\r")
			if body == "" {
				continue
			}
			if n, ok := index[body]; ok {
				oldStart, newStart = n, n
				break
			}
		}
		// Count sides for the header.
		aCount, bCount := 0, 0
		for _, hl := range hunkBody {
			switch {
			case strings.HasPrefix(hl, "+++") || strings.HasPrefix(hl, "---") || strings.HasPrefix(hl, "@@"):
				continue
			case strings.HasPrefix(hl, "+"):
				bCount++
			case strings.HasPrefix(hl, "-"):
				aCount++
			default:
				aCount++
				bCount++
			}
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", oldStart, aCount, newStart, bCount)
		for _, hl := range hunkBody {
			out.WriteString(hl)
			out.WriteByte('\n')
		}
		hunkBody = hunkBody[:0]
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "@@") {
			flush()
			continue
		}
		hunkBody = append(hunkBody, line)
	}
	flush()
	return strings.TrimRight(out.String(), "\n")
}

func syntheticEditDiff(oldStr, newStr string) string {
	// Snippet-only fallback when the file is gone; line numbers are relative
	// to the replaced block (starts at 1), not the full file.
	return edit.UnifiedDiff(oldStr, newStr)
}

func diffStat(diff string) (add, del int) {
	if diff == "" {
		return 0, 0
	}
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			continue
		case strings.HasPrefix(line, "+"):
			add++
		case strings.HasPrefix(line, "-"):
			del++
		}
	}
	return add, del
}

func formatDiffStat(add, del int) string {
	parts := make([]string, 0, diffStatParts)
	if add > 0 {
		parts = append(parts, "+"+strconv.Itoa(add))
	}
	if del > 0 {
		parts = append(parts, "-"+strconv.Itoa(del))
	}
	return strings.Join(parts, " ")
}

// renderDiff paints a unified diff at full card width with a dual line-number
// gutter (old/new). + rows get a soft greenish wash, - rows a soft reddish wash.
func renderDiff(diff string, width int) string {
	width = max(minPaneWidth, width)
	lines := strings.Split(strings.TrimRight(diff, "\n"), "\n")
	// First pass: walk hunk headers so we can size the number gutter.
	type diffRow struct {
		kind   byte // 'h' hunk, 'm' meta, 'k' context, 'd' del, 'a' add
		oldNum int  // 0 = blank in gutter
		newNum int
		text   string // body without leading +/-/space for content rows
	}
	rows := make([]diffRow, 0, len(lines))
	oldLn, newLn := 0, 0
	maxNum := 1
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "@@"):
			o, n, ok := parseHunkHeader(line)
			if ok {
				oldLn, newLn = o, n
			}
			rows = append(rows, diffRow{kind: 'h', text: line})
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			rows = append(rows, diffRow{kind: 'm', text: line})
		case strings.HasPrefix(line, "+"):
			rows = append(rows, diffRow{kind: 'a', newNum: newLn, text: line[1:]})
			if newLn > maxNum {
				maxNum = newLn
			}
			newLn++
		case strings.HasPrefix(line, "-"):
			rows = append(rows, diffRow{kind: 'd', oldNum: oldLn, text: line[1:]})
			if oldLn > maxNum {
				maxNum = oldLn
			}
			oldLn++
		default:
			// Context (leading space) or bare line.
			body := line
			if strings.HasPrefix(line, " ") {
				body = line[1:]
			}
			rows = append(rows, diffRow{kind: 'k', oldNum: oldLn, newNum: newLn, text: body})
			if oldLn > maxNum {
				maxNum = oldLn
			}
			if newLn > maxNum {
				maxNum = newLn
			}
			oldLn++
			newLn++
		}
	}
	numW := len(strconv.Itoa(maxNum))
	if numW < diffNumMinWidth {
		numW = diffNumMinWidth
	}
	// " old new │ " gutter: numW + space + numW + space + │ + space
	gutterW := numW + 1 + numW + 1 + 1 + 1
	bodyW := max(diffBodyMinWidth, width-gutterW)

	addRow := diffAddStyle.Width(width).MaxWidth(width)
	delRow := diffDelStyle.Width(width).MaxWidth(width)
	metaRow := diffMetaStyle.Width(width).MaxWidth(width)
	ctxRow := diffCtxStyle.Width(width).MaxWidth(width)

	out := make([]string, 0, len(rows))
	for _, r := range rows {
		switch r.kind {
		case 'h', 'm':
			display := r.text
			if lipgloss.Width(display) > width {
				display = ansi.Cut(display, 0, max(1, width-1)) + "…"
			}
			out = append(out, metaRow.Render(display))
		default:
			var marker string
			var style lipgloss.Style
			switch r.kind {
			case 'a':
				marker = "+"
				style = addRow
			case 'd':
				marker = "-"
				style = delRow
			default:
				marker = " "
				style = ctxRow
			}
			gutter := formatDiffGutter(r.oldNum, r.newNum, numW) + "│ "
			body := r.text
			if lipgloss.Width(body) > bodyW-1 {
				body = ansi.Cut(body, 0, max(1, bodyW-diffEllipsisPad)) + "…"
			}
			display := gutter + marker + body
			if lipgloss.Width(display) > width {
				display = ansi.Cut(display, 0, max(1, width-1)) + "…"
			}
			out = append(out, style.Render(display))
		}
	}
	return strings.Join(out, "\n")
}

// parseHunkHeader reads @@ -old[,count] +new[,count] @@.
func parseHunkHeader(line string) (oldStart, newStart int, ok bool) {
	// Strip trailing "@@ ..." section name.
	s := strings.TrimSpace(line)
	if !strings.HasPrefix(s, "@@") {
		return 0, 0, false
	}
	// Find the two ranges after the first @@.
	rest := strings.TrimSpace(strings.TrimPrefix(s, "@@"))
	// rest like: "-12,3 +14,4 @@ optional"
	parts := strings.Fields(rest)
	if len(parts) < diffHunkParts {
		return 0, 0, false
	}
	oldStart, ok1 := parseDiffRange(parts[0])
	newStart, ok2 := parseDiffRange(parts[1])
	return oldStart, newStart, ok1 && ok2
}

func parseDiffRange(tok string) (start int, ok bool) {
	tok = strings.TrimPrefix(tok, "-")
	tok = strings.TrimPrefix(tok, "+")
	// "12" or "12,3"
	if i := strings.IndexByte(tok, ','); i >= 0 {
		tok = tok[:i]
	}
	n, err := strconv.Atoi(tok)
	if err != nil {
		return 0, false
	}
	// Unified diffs use 0 for empty file starts; display as 0 is fine.
	return n, true
}

func formatDiffGutter(oldNum, newNum, numW int) string {
	oldS := strings.Repeat(" ", numW)
	newS := strings.Repeat(" ", numW)
	if oldNum > 0 {
		oldS = fmt.Sprintf("%*d", numW, oldNum)
	}
	if newNum > 0 {
		newS = fmt.Sprintf("%*d", numW, newNum)
	}
	return oldS + " " + newS + " "
}

// relWorkPath returns path relative to the session workdir when possible,
// with forward slashes, so edit/read/write headers show project-local paths.
func (m Model) relWorkPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	// Prefer clean relative form; keep absolute only when outside workdir.
	if m.workdir != "" {
		abs := p
		if !filepath.IsAbs(p) {
			abs = filepath.Join(m.workdir, p)
		}
		if rel, err := filepath.Rel(m.workdir, abs); err == nil {
			// Inside or equal: show relative. Outside: keep original cleaned.
			if rel == "." {
				return filepath.Base(abs)
			}
			if !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
				return filepath.ToSlash(rel)
			}
		}
	}
	return filepath.ToSlash(filepath.Clean(p))
}

func (m Model) toolCardWidth() int {
	// Full transcript pane width (after work-rail inset when present).
	return m.metaWidth()
}

func toolCommand(tc db.ToolCall) string {
	if tc.Tool == "bash" {
		var args struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal([]byte(tc.InputJSON), &args); err == nil && args.Command != "" {
			return args.Command
		}
	}
	if tc.Tool == "edit" || tc.Tool == "write" || tc.Tool == "read" {
		var args struct {
			FilePath string `json:"filePath"`
		}
		if err := json.Unmarshal([]byte(tc.InputJSON), &args); err == nil && args.FilePath != "" {
			return args.FilePath
		}
	}
	if tc.Title != nil {
		return *tc.Title
	}
	return ""
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return s
	}
	return string(r[:n-1]) + "…"
}

func truncateToolOutputForView(s string) (string, bool) {
	s = strings.TrimSuffix(s, "\n")
	lines := strings.Split(s, "\n")
	omitted := false
	if len(lines) > maxToolBodyLines {
		omitted = true
		omittedLines := len(lines) - toolBodyHeadLines - toolBodyTailLines
		note := fmt.Sprintf("… %d lines omitted · showing first %d + last %d", omittedLines, toolBodyHeadLines, toolBodyTailLines)
		tail := append([]string{}, lines[len(lines)-toolBodyTailLines:]...)
		lines = append(append([]string{}, lines[:toolBodyHeadLines]...), note)
		lines = append(lines, tail...)
	}
	body := strings.Join(lines, "\n")
	if len([]rune(body)) > maxToolBodyRunes {
		omitted = true
		note := fmt.Sprintf("… output truncated to %d runes", maxToolBodyRunes)
		noteRunes := len([]rune(note))
		prefixRunes := maxToolBodyRunes - noteRunes - 1
		if prefixRunes < 0 {
			return string([]rune(body)[:maxToolBodyRunes]), true
		}
		body = string([]rune(body)[:prefixRunes]) + "\n" + note
	}
	return body, omitted
}

// toggleAllTools applies the bulk keyboard rule to every tool card in the
// main transcript. A mixed or partially open set collapses; an all-collapsed
// set expands. The transcript is synchronized once after the flip.
func (m Model) toggleAllTools() Model {
	anyOpen := false
	hasTool := false
	for _, it := range m.items {
		if it.kind != itemTool {
			continue
		}
		hasTool = true
		if !it.collapsed {
			anyOpen = true
			break
		}
	}
	if !hasTool {
		return m
	}
	for i, it := range m.items {
		if it.kind == itemTool {
			it.collapsed = anyOpen
			m.items[i] = it
			m.lastTool = i
		}
	}
	m.syncTranscript()
	return m
}

// toggleAllReasoning applies the same bulk rule to every thinking block.
func (m Model) toggleAllReasoning() Model {
	anyOpen := false
	hasReasoning := false
	for _, it := range m.items {
		if it.kind != itemReasoning {
			continue
		}
		hasReasoning = true
		if !it.collapsed {
			anyOpen = true
			break
		}
	}
	if !hasReasoning {
		return m
	}
	for i, it := range m.items {
		if it.kind == itemReasoning {
			it.collapsed = anyOpen
			m.items[i] = it
		}
	}
	m.syncTranscript()
	return m
}

func (m Model) lastReasoningIndex() int {
	for i := len(m.items) - 1; i >= 0; i-- {
		if m.items[i].kind == itemReasoning {
			return i
		}
	}
	return -1
}
