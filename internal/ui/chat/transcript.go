package chat

import (
	"context"
	"encoding/json"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/db"
	"github.com/chinmay-sawant/lazykoder/internal/ui/markdown"
)

func renderUserLine(text string) string {
	return userStyle.Render("user: " + text)
}

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
						m.lines = append(m.lines, renderUserLine(*p.Text))
					} else {
						m.lines = append(m.lines, renderAssistantLine(*p.Text))
					}
				}
			case "reasoning":
				if p.Text != nil {
					m.lines = append(m.lines, renderReasoningLine(*p.Text))
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
				m.lines = append(m.lines, m.renderTool(agent.Event{Part: p, Tool: tool}))
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
	content := strings.Join(m.lines, "\n")
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

func (m Model) plainTranscriptRows() []string {
	content := ansi.Strip(strings.Join(m.lines, "\n"))
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
		m.lines = append(m.lines, renderAssistantLine(*p.Text))
	case "reasoning":
		if p.Text != nil {
			m.lines = append(m.lines, renderReasoningLine(*p.Text))
		}
	}
	m.syncTranscript()
}

func renderReasoningLine(text string) string {
	return reasoningStyle.Render("reasoning: " + text)
}

func renderAssistantLine(text string) string {
	rendered := markdown.Render(text)
	if strings.Contains(rendered, "\n") {
		return "assistant:\n" + rendered
	}
	return "assistant: " + rendered
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
	if status == "" || status == "pending" {
		m.lines = append(m.lines, m.renderTool(ev))
		m.lastTool = len(m.lines) - 1
		m.syncTranscript()
		return
	}
	if m.lastTool >= 0 && m.lastTool < len(m.lines) {
		m.lines[m.lastTool] = m.renderTool(ev)
	}
	m.syncTranscript()
}

func (m Model) renderTool(ev agent.Event) string {
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

	bodyWidth := max(minPaneWidth, m.toolCardWidth()-cardBorder*2)
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Render(name + ": " + status)
	body := []string{header}
	if command := toolCommand(ev.Tool); command != "" {
		body = append(body, hintStyle.Width(bodyWidth).Render("$ "+command))
	}
	if ev.Tool.Output != nil && *ev.Tool.Output != "" {
		output := strings.TrimSuffix(*ev.Tool.Output, "\n")
		outputLabel := hintStyle.Width(bodyWidth).Render("output")
		outputBox := toolOutputStyle.Width(bodyWidth).Render(output)
		body = append(body, outputLabel, outputBox)
	}
	return toolCardStyle.Width(m.toolCardWidth()).Render(strings.Join(body, "\n"))
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
	if tc.Title != nil {
		return *tc.Title
	}
	return ""
}
