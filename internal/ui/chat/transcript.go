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
	roleYou       = "you"
	roleAssistant = "assistant"
	thinkingLabel = "thinking"
	maxToolTitle  = 72
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
						m.items = append(m.items, transcriptItem{kind: itemUser, text: *p.Text, when: itemTime(msg.TimeCreated, p.TimeCreated)})
					} else {
						m.items = append(m.items, transcriptItem{kind: itemAssistant, text: *p.Text, when: itemTime(msg.TimeCreated, p.TimeCreated)})
					}
				}
			case "reasoning":
				if p.Text != nil {
					m.items = append(m.items, transcriptItem{kind: itemReasoning, text: *p.Text, collapsed: true, when: itemTime(msg.TimeCreated, p.TimeCreated)})
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
			}
		}
	}
	m.syncTranscript()
}

func (m *Model) syncTranscript() {
	atBottom := m.transcript.AtBottom()
	yOffset := m.transcript.YOffset()
	m.transcript.SetContent(m.transcriptContent())
	if atBottom {
		m.transcript.GotoBottom()
		return
	}
	m.transcript.SetYOffset(yOffset)
}

func (m Model) transcriptContent() string {
	content := strings.Join(m.renderedItems(), "\n")
	if !m.selection.active || !m.selection.hasRange() {
		return content
	}
	rows := strings.Split(content, "\n")
	start, end := m.selection.bounds()
	for row := start.row; row <= end.row && row < len(rows); row++ {
		from := 0
		to := lipgloss.Width(ansi.Strip(rows[row]))
		if row == start.row {
			from = start.col
		}
		if row == end.row {
			to = end.col
		}
		if from < to {
			rows[row] = lipgloss.StyleRanges(rows[row], lipgloss.NewRange(from, to, selectionStyle))
		}
	}
	return strings.Join(rows, "\n")
}

func (m Model) renderedItems() []string {
	out := make([]string, 0, len(m.items))
	for i, it := range m.items {
		if i > 0 && (it.kind == itemUser || it.kind == itemAssistant) {
			out = append(out, "")
		}
		out = append(out, m.renderItem(it, i == m.selectedItem))
	}
	return out
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

func roleLine(label string, when int64) string {
	stamp := formatSessionAge(when)
	if stamp == "" {
		return roleStyle.Render(label)
	}
	return roleStyle.Render(label) + hintStyle.Render("  ·  "+stamp)
}

func (m Model) plainTranscriptRows() []string {
	content := ansi.Strip(strings.Join(m.renderedItems(), "\n"))
	return strings.Split(content, "\n")
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
		m.items = append(m.items, transcriptItem{kind: itemAssistant, text: *p.Text, when: itemTime(0, p.TimeCreated)})
	case "reasoning":
		if p.Text != nil {
			m.items = append(m.items, transcriptItem{kind: itemReasoning, text: *p.Text, collapsed: true, when: itemTime(0, p.TimeCreated)})
		}
	}
	m.syncTranscript()
}

func (m *Model) applyTool(ev agent.Event) {
	if ev.Tool.Tool == "" && ev.Part.ToolName != nil {
		ev.Tool.Tool = *ev.Part.ToolName
	}
	if ev.Tool.Tool == "" {
		return
	}
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

func (m Model) renderItem(it transcriptItem, selected bool) string {
	switch it.kind {
	case itemUser:
		return roleLine(roleYou, it.when) + "\n" + userStyle.Render(it.text)
	case itemAssistant:
		rendered := markdown.Render(it.text)
		return roleLine(roleAssistant, it.when) + "\n" + rendered
	case itemReasoning:
		marker := "▸"
		if !it.collapsed {
			marker = "▾"
		}
		head := reasoningStyle.Render(marker+" "+thinkingLabel) + hintStyle.Render(ageSuffix(it.when))
		if it.collapsed {
			return head
		}
		return head + "\n" + reasoningStyle.Render(it.text)
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

func ageSuffix(when int64) string {
	if stamp := formatSessionAge(when); stamp != "" {
		return "  ·  " + stamp
	}
	return ""
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
	marker := "▸"
	if !collapsed {
		marker = "▾"
	}
	head := marker + " run  " + name + "  " + title + ageSuffix(when) + "  ·  " + status
	header := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render(head)
	if collapsed {
		return toolCardStyle.Width(m.toolCardWidth()).Render(header)
	}
	bodyWidth := max(minPaneWidth, m.toolCardWidth()-cardBorder*2)
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
	return toolCardStyle.Width(m.toolCardWidth()).Render(strings.Join(body, "\n"))
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
	return max(minPaneWidth, m.width-cardBorder*2)
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
