package chat

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/modelscache"
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

type transcriptItem struct {
	kind      itemKind
	text      string
	collapsed bool
	when      int64
	tool      db.ToolCall
	part      db.Part
}

const (
	roleYou          = "you"
	roleAssistant    = "assistant"
	thinkingLabel    = "thinking"
	maxToolTitle     = 72
	workBracket      = "["
	workBracketClose = "]"
	workRail         = "│"
	workRailCols     = 2
	streamCursor     = "▌"
)

func (m *Model) replay(sessionID string) {
	ctx := context.Background()
	msgs, err := m.store.ListMessages(ctx, sessionID)
	if err != nil {
		m.err = "chat: " + err.Error()
		return
	}
	tcs, err := m.store.ListToolCalls(ctx, sessionID)
	if err != nil {
		m.err = "chat: " + err.Error()
		return
	}
	toolCalls := make(map[string]db.ToolCall, len(tcs))
	for _, tc := range tcs {
		toolCalls[tc.PartID] = tc
	}
	for _, msg := range msgs {
		if !msg.Visible {
			continue
		}
		parts, err := m.store.ListParts(ctx, msg.ID)
		if err != nil {
			m.err = "chat: " + err.Error()
			return
		}
		for _, p := range parts {
			switch p.Type {
			case "text":
				if p.Text != nil {
					if msg.Role == "user" {
						m.inputHistory = append(m.inputHistory, inputHistoryItem{messageID: msg.ID, text: *p.Text})
						m.items = append(m.items, transcriptItem{kind: itemUser, text: *p.Text, when: itemTime(msg.TimeCreated, p.TimeCreated), part: p})
					} else {
						m.items = append(m.items, transcriptItem{kind: itemAssistant, text: *p.Text, when: itemTime(msg.TimeCreated, p.TimeCreated), part: p})
					}
				}
			case "reasoning":
				if p.Text != nil {
					m.items = append(m.items, transcriptItem{kind: itemReasoning, text: *p.Text, collapsed: true, when: itemTime(msg.TimeCreated, p.TimeCreated), part: p})
				}
			case "tool":
				tool := db.ToolCall{PartID: p.ID}
				if stored, ok := toolCalls[p.ID]; ok {
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
				m.items = append(m.items, transcriptItem{kind: itemTool, collapsed: true, when: when, tool: tool, part: p})
			case "step-finish":
				m.applyUsage(p)
			}
		}
	}
	m.bumpTokenFloor()
	m.syncTranscript()
}

func (m *Model) applyUsage(p db.Part) {
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
	// Never drop the session total when a later step reports a smaller
	// (or empty) usage blob.
	if total > m.tokensUsed {
		m.tokensUsed = total
	}
	var hit int64
	if p.TokensCacheRead != nil {
		hit = *p.TokensCacheRead
	}
	m.addStepCost(p)

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
	if est := estimateTokens(m.items); est > m.tokensUsed {
		m.tokensUsed = est
	}
}

func (m *Model) addStepCost(p db.Part) {
	if p.Cost != nil && *p.Cost > 0 {
		m.sessionCost += *p.Cost
		return
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
		m.addCost(in, out, hit, written)
		return
	}
	if total > 0 {
		m.addCost(0, total, 0, 0)
	}
}

func (m *Model) recomputeSessionCost() {
	if m.store == nil || m.session == nil || len(m.modelInfos) == 0 {
		return
	}
	m.sessionCost = 0
	ctx := context.Background()
	msgs, err := m.store.ListMessages(ctx, m.session.ID)
	if err != nil {
		return
	}
	for _, msg := range msgs {
		parts, err := m.store.ListParts(ctx, msg.ID)
		if err != nil {
			continue
		}
		for _, p := range parts {
			if p.Type == "step-finish" {
				m.addStepCost(p)
			}
		}
	}
}

func (m *Model) addCost(inputTokens, outputTokens, cacheRead, cacheWrite int64) {
	info, ok := modelscache.InfoOf(m.modelInfos, m.modelLabel())
	if !ok {
		return
	}
	m.sessionCost += info.CostUSD(inputTokens, outputTokens, cacheRead, cacheWrite)
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

func estimateTokens(items []transcriptItem) int64 {
	var n int
	for _, it := range items {
		n += len([]rune(it.text))
		if it.kind == itemTool && it.tool.Output != nil {
			n += len([]rune(*it.tool.Output))
		}
	}
	if n == 0 {
		return 0
	}
	return int64(n / 4)
}

func (m *Model) syncTranscript() {
	m.transcript.SetWidth(max(minPaneWidth, m.width-1))
	m.transcript.SetHeight(max(minPaneHeight, m.transcriptRenderHeight()))
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
	for i, it := range m.items {
		if i > 0 && (it.kind == itemUser || it.kind == itemAssistant) {
			if it.kind == itemUser {
				out = append(out, "")
			} else {
				out = append(out, m.railedItem(i, " "))
			}
		}
		out = append(out, m.railedItem(i, m.renderItemCopy(i, it)))
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
	return m.workRailLive(m.busy && m.pulseOn)
}

func (m Model) workRailLive(throb bool) string {
	style := lipgloss.NewStyle().Foreground(theme.ColorAccent())
	if throb {
		style = lipgloss.NewStyle().Foreground(theme.PulseAccent(m.pulseT()))
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
		innerW = min(innerW, max(1, width-2))
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
	return m.alignMeta(roleStyle.Render(label), formatClock(when))
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
		m.applyUsage(p)
	}
	m.syncTranscript()
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
	if ev.Tool.Tool == "" && ev.Part.ToolName != nil {
		ev.Tool.Tool = *ev.Part.ToolName
	}
	if ev.Tool.Tool == "" {
		return
	}
	m.collapseLiveReasoning()
	status := ev.Tool.Status
	if status == "" && ev.Part.ToolStatus != nil {
		status = *ev.Part.ToolStatus
		ev.Tool.Status = status
	}
	if status == "" {
		status = "pending"
		ev.Tool.Status = status
	}
	when := itemTime(0, ev.Part.TimeCreated)
	if ev.Tool.TimeStart != nil {
		when = *ev.Tool.TimeStart
	}
	item := transcriptItem{kind: itemTool, collapsed: true, when: when, tool: ev.Tool, part: ev.Part}
	if status == "" || status == "pending" {
		m.items = append(m.items, item)
		m.lastTool = len(m.items) - 1
		m.selectedItem = m.lastTool
		m.syncTranscript()
		return
	}
	if m.lastTool >= 0 && m.lastTool < len(m.items) && m.items[m.lastTool].kind == itemTool {
		item.collapsed = m.items[m.lastTool].collapsed
		m.items[m.lastTool] = item
	} else {
		m.items = append(m.items, item)
		m.lastTool = len(m.items) - 1
	}
	m.syncTranscript()
}

func (m Model) renderItem(it transcriptItem, selected bool, streaming bool) string {
	switch it.kind {
	case itemUser:
		return m.roleLine(roleYou, it.when) + "\n" + userStyle.Render(frameUserPrompt(it.text, m.contentWidth(it.when)))
	case itemAssistant:
		rendered := markdown.Render(it.text, m.contentWidth(it.when))
		return m.roleLine(roleAssistant, it.when) + "\n" + rendered
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
		return m.renderTool(agent.Event{Part: it.part, Tool: it.tool}, it.collapsed, it.when)
	case itemNote:
		return hintStyle.Render(it.text)
	}
	if selected {
		return it.text
	}
	return it.text
}

func (m Model) renderTool(ev agent.Event, collapsed bool, when int64) string {
	name := ev.Tool.Tool
	if name == "" && ev.Part.ToolName != nil {
		name = *ev.Part.ToolName
	}
	status := ev.Tool.Status
	if status == "" && ev.Part.ToolStatus != nil {
		status = *ev.Part.ToolStatus
	}
	if status == "" {
		status = "pending"
	}
	title := toolCommand(ev.Tool)
	if title == "" {
		title = name
	}
	title = truncateRunes(title, maxToolTitle)
	chevron := "▸"
	if !collapsed {
		chevron = "▾"
	}
	label := name
	if title != name {
		label = name + "  " + title
	}
	diamondColor := theme.StatusColor(status)
	diamond := lipgloss.NewStyle().Foreground(diamondColor).Render(theme.StatusDiamond)
	left := diamond + "  " + lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render(chevron+"  "+label)
	header := m.alignMeta(left, formatClock(when))
	card := toolCardStyle.Width(m.toolCardWidth()).Background(theme.ColorBg())
	if collapsed {
		return card.Render(header)
	}
	bodyWidth := max(minPaneWidth, m.toolCardWidth())
	body := []string{header}
	switch ev.Tool.Tool {
	case "edit":
		if diff := toolMetadataDiff(ev.Tool); diff != "" {
			body = append(body, renderDiff(diff, bodyWidth))
			break
		}
		fallthrough
	case "write":
		if ev.Tool.Output != nil && *ev.Tool.Output != "" {
			preview := *ev.Tool.Output
			if ev.Tool.Tool == "write" && len([]rune(preview)) > 400 {
				preview = string([]rune(preview)[:400]) + "…"
			}
			body = append(body, toolOutputStyle.Width(bodyWidth).Render(strings.TrimSuffix(preview, "\n")))
		}
	default:
		if command := toolCommand(ev.Tool); command != "" {
			body = append(body, hintStyle.Width(bodyWidth).Render("$ "+command))
		}
		if ev.Tool.Output != nil && *ev.Tool.Output != "" {
			output := strings.TrimSuffix(*ev.Tool.Output, "\n")
			outputLabel := hintStyle.Width(bodyWidth).Render("output")
			outputBox := toolOutputStyle.Width(bodyWidth).Render(output)
			body = append(body, outputLabel, outputBox)
		}
	}
	return card.Render(strings.Join(body, "\n"))
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

func renderDiff(diff string, width int) string {
	var b strings.Builder
	for i, line := range strings.Split(diff, "\n") {
		if i > 0 {
			b.WriteString("\n")
		}
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			b.WriteString(diffAddStyle.Render(line))
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			b.WriteString(diffDelStyle.Render(line))
		default:
			b.WriteString(hintStyle.Render(line))
		}
	}
	return toolOutputStyle.Width(width).Render(b.String())
}

func (m Model) toolCardWidth() int {
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

func (m Model) toggleSelectedMeta() Model {
	idx := m.selectedItem
	if idx < 0 || idx >= len(m.items) {
		idx = m.lastMetaIndex()
	}
	if idx < 0 {
		return m
	}
	it := m.items[idx]
	if it.kind != itemReasoning && it.kind != itemTool {
		idx = m.lastMetaIndex()
		if idx < 0 {
			return m
		}
		it = m.items[idx]
	}
	it.collapsed = !it.collapsed
	m.items[idx] = it
	m.selectedItem = idx
	m.syncTranscript()
	return m
}

func (m Model) lastMetaIndex() int {
	for i := len(m.items) - 1; i >= 0; i-- {
		if m.items[i].kind == itemReasoning || m.items[i].kind == itemTool {
			return i
		}
	}
	return -1
}

func (m Model) lastReasoningIndex() int {
	for i := len(m.items) - 1; i >= 0; i-- {
		if m.items[i].kind == itemReasoning {
			return i
		}
	}
	return -1
}

func (m Model) lastToolIndex() int {
	for i := len(m.items) - 1; i >= 0; i-- {
		if m.items[i].kind == itemTool {
			return i
		}
	}
	return -1
}

func (m Model) toggleReasoning() Model {
	idx := m.lastReasoningIndex()
	if idx < 0 {
		return m
	}
	it := m.items[idx]
	it.collapsed = !it.collapsed
	m.items[idx] = it
	m.selectedItem = idx
	m.syncTranscript()
	return m
}

func (m Model) toggleLastTool() Model {
	idx := m.lastToolIndex()
	if idx < 0 {
		return m
	}
	it := m.items[idx]
	it.collapsed = !it.collapsed
	m.items[idx] = it
	m.selectedItem = idx
	m.lastTool = idx
	m.syncTranscript()
	return m
}

func (m Model) currentToolName() string {
	idx := m.lastToolIndex()
	if idx < 0 {
		return ""
	}
	name := m.items[idx].tool.Tool
	status := m.items[idx].tool.Status
	if status == "pending" || status == "running" {
		return name
	}
	return ""
}
