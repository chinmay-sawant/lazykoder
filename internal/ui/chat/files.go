package chat

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/chinmay-sawant/lazykoder/internal/agent"
	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

const (
	atKindFile  = "file"
	atKindAgent = "agent"
	// atAgentPrefix is the token written into the prompt: @agent:name
	atAgentPrefix = "agent:"
	// maxAtMentionContext caps injected sub-agent transcript context per mention.
	maxAtMentionContext = 4000
	maxAtPickerVisible  = 12
	maxAtPickerFiles    = 60
)

// atPickItem is one row in the @ picker (project file or session sub-agent).
type atPickItem struct {
	Kind      string // file | agent
	Label     string // left label
	Insert    string // text after @ (path or agent:name)
	Status    string // agents: lifecycle / activity
	Live      bool
	SessionID string // child session for context expand
	Summary   string
}

func (m Model) openFilePicker() Model {
	m.filePickerFilter = ""
	m.filePickerCursor = 0
	m.filePickerAt = strings.LastIndex(m.prompt.Value(), "@")
	if m.filePickerAt < 0 {
		m.filePickerAt = len(m.prompt.Value())
	}
	m.filePickerItems = m.listAtPickItems("")
	m.filePickerMode = true
	return m
}

func (m Model) closeFilePicker() Model {
	m.filePickerMode = false
	m.filePickerItems = nil
	m.filePickerFilter = ""
	return m
}

func (m Model) filePickerOverlay() string {
	sel := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText())
	dim := lipgloss.NewStyle().Foreground(theme.ColorMute())
	boxW := min(64, max(minPaneWidth, m.width-8))
	contentW := max(minPaneWidth, boxW-cardBorder-2*cardPad)

	cursor := m.filePickerCursor
	n := len(m.filePickerItems)
	if cursor < 0 {
		cursor = 0
	}
	if n > 0 && cursor >= n {
		cursor = n - 1
	}
	start, end := atPickerWindow(n, cursor, maxAtPickerVisible)
	overflow := n > maxAtPickerVisible
	listW := contentW
	if overflow {
		listW = max(8, contentW-1)
	}

	var rows []string
	if n == 0 {
		rows = append(rows, dim.Render("no matches"))
	} else {
		lastKind := ""
		for i := start; i < end; i++ {
			it := m.filePickerItems[i]
			if it.Kind != lastKind {
				rows = append(rows, dim.Render(atPickerSectionTitle(it.Kind)))
				lastKind = it.Kind
			}
			rows = append(rows, atPickerItemRow(it, i == cursor, listW, sel, dim))
		}
	}
	body := strings.Join(rows, "\n")
	if overflow {
		span := n - maxAtPickerVisible
		percent := 0.0
		if span > 0 {
			percent = float64(start) / float64(span)
		}
		body = withScrollbar(body, contentW, len(rows), percent, true)
	}

	head := lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("@ files & sub-agents")
	if m.filePickerFilter != "" {
		head = lipgloss.NewStyle().Bold(true).Foreground(theme.ColorText()).Render("@ " + m.filePickerFilter)
	}
	foot := hintStyle.Render("↑/↓ select  •  enter insert  •  esc close")
	content := lipgloss.JoinVertical(lipgloss.Left, head, body, foot)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorBorder()).
		Background(theme.ColorBg()).
		Padding(1, 2).
		Width(boxW).
		Render(content)
}

func atPickerSectionTitle(kind string) string {
	if kind == atKindAgent {
		return "sub-agents"
	}
	return "files"
}

// atPickerWindow is the inclusive-start / exclusive-end item window that
// always contains cursor and is at most maxVisible items long.
func atPickerWindow(n, cursor, maxVisible int) (start, end int) {
	if n <= 0 {
		return 0, 0
	}
	if maxVisible < 1 {
		maxVisible = 1
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= n {
		cursor = n - 1
	}
	if n <= maxVisible {
		return 0, n
	}
	end = cursor + 1
	if end < maxVisible {
		end = maxVisible
	}
	if end > n {
		end = n
	}
	start = end - maxVisible
	if start < 0 {
		start = 0
	}
	return start, end
}

func atPickerItemRow(it atPickItem, selected bool, width int, sel, dim lipgloss.Style) string {
	liveSt := lipgloss.NewStyle().Foreground(theme.ColorAccent())
	goodSt := lipgloss.NewStyle().Foreground(theme.ColorGood())
	badSt := lipgloss.NewStyle().Foreground(theme.ColorDanger())
	left := it.Label
	if it.Kind == atKindAgent {
		left = "agent  " + it.Label
	}
	marker := "  "
	style := dim
	if selected {
		marker = "▸ "
		style = sel
	}
	right := ""
	if it.Kind == atKindAgent && it.Status != "" {
		st := it.Status
		diamond := lipgloss.NewStyle().Foreground(theme.StatusColor(statusKey(st))).Render(theme.StatusDiamond)
		rightRaw := diamond + " " + st
		rightStyle := dim
		if it.Live {
			rightStyle = liveSt
		} else if isFailedSubStatus(st) {
			rightStyle = badSt
		} else if isTerminalSubStatus(st) {
			rightStyle = goodSt
		}
		right = rightStyle.Render(rightRaw)
	}
	return joinAtPickerRow(style.Render(marker+left), right, width)
}

func joinAtPickerRow(left, right string, width int) string {
	if right == "" {
		return left
	}
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	gap := width - lw - rw
	if gap < 1 {
		maxLeft := max(4, width-rw-1)
		if lw > maxLeft {
			left = truncateAtLabel(left, maxLeft)
			lw = lipgloss.Width(left)
		}
		gap = max(1, width-lw-rw)
	}
	return left + strings.Repeat(" ", gap) + right
}

func truncateAtLabel(s string, maxW int) string {
	if lipgloss.Width(s) <= maxW {
		return s
	}
	if maxW <= 1 {
		return "…"
	}
	r := []rune(s)
	for n := len(r); n > 0; n-- {
		t := string(r[:n-1]) + "…"
		if lipgloss.Width(t) <= maxW {
			return t
		}
	}
	return "…"
}

func statusKey(st string) string {
	st = strings.ToLower(strings.TrimSpace(st))
	switch st {
	case "completed", "success", "done":
		return "completed"
	case "failed", "error", "denied", "cancelled", "timed_out", "crashed":
		return "failed"
	case "running", "pending", "queued":
		return "running"
	default:
		return st
	}
}

func (m Model) updateFilePickerKey(key tea.KeyPressMsg) (Model, tea.Cmd) {
	switch key.Code {
	case tea.KeyEscape:
		return m.closeFilePicker(), nil
	case tea.KeyEnter:
		if m.filePickerCursor >= 0 && m.filePickerCursor < len(m.filePickerItems) {
			it := m.filePickerItems[m.filePickerCursor]
			val := m.prompt.Value()
			prefix := val
			if m.filePickerAt >= 0 && m.filePickerAt <= len(val) {
				prefix = val[:m.filePickerAt]
			}
			m.prompt.SetValue(prefix + "@" + it.Insert + " ")
			m.prompt.SetHeight(m.promptHeight())
			m = m.closeFilePicker()
			return m, nil
		}
		return m, nil
	case tea.KeyDown:
		if m.filePickerCursor < len(m.filePickerItems)-1 {
			m.filePickerCursor++
		}
		if n := len(m.filePickerItems); n > 0 && m.filePickerCursor >= n {
			m.filePickerCursor = n - 1
		}
		return m, nil
	case tea.KeyUp:
		if m.filePickerCursor > 0 {
			m.filePickerCursor--
		}
		if m.filePickerCursor < 0 {
			m.filePickerCursor = 0
		}
		return m, nil
	case tea.KeyBackspace:
		if len(m.filePickerFilter) > 0 {
			r := []rune(m.filePickerFilter)
			m.filePickerFilter = string(r[:len(r)-1])
			m.filePickerItems = m.listAtPickItems(m.filePickerFilter)
			m.filePickerCursor = 0
		}
		return m, nil
	}
	if key.Text != "" {
		m.filePickerFilter += key.Text
		m.filePickerItems = m.listAtPickItems(m.filePickerFilter)
		m.filePickerCursor = 0
	}
	return m, nil
}

// listAtPickItems merges this session's sub-agents (first) with project files.
func (m Model) listAtPickItems(filter string) []atPickItem {
	filter = strings.ToLower(strings.TrimSpace(filter))
	nameFilter := strings.TrimPrefix(filter, "agent:")
	var out []atPickItem
	for _, row := range m.atPickerAgents() {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			name = row.ID
		}
		hay := strings.ToLower(name + " " + row.Role + " agent:" + name)
		if nameFilter != "" && !strings.Contains(hay, nameFilter) {
			continue
		}
		st := row.Status
		if st == "" {
			st = "completed"
		}
		if row.Activity != "" && row.Live {
			st = row.Activity
		}
		out = append(out, atPickItem{
			Kind:      atKindAgent,
			Label:     name,
			Insert:    atAgentPrefix + name,
			Status:    st,
			Live:      row.Live,
			SessionID: row.ChildSessionID,
			Summary:   row.Summary,
		})
	}
	// Typing agent:… filters to agents only.
	if !strings.HasPrefix(filter, "agent:") {
		for _, path := range listProjectFiles(m.workdir, filter) {
			out = append(out, atPickItem{
				Kind:   atKindFile,
				Label:  path,
				Insert: path,
			})
			if len(out) >= maxAtPickerFiles+32 {
				break
			}
		}
	}
	return out
}

func (m Model) atPickerAgents() []subagentRow {
	if len(m.subagentItems) > 0 {
		return m.subagentItems
	}
	return m.collectSubagentRows()
}

var agentMentionRE = regexp.MustCompile(`@agent:([^\s]+)`)

// withMentionContext expands @agent:name tokens by appending each sub-agent's
// summary/transcript so the parent model has their context on this turn.
// The user-visible transcript keeps the original text; only the send payload grows.
func (m Model) withMentionContext(text string) string {
	matches := agentMentionRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return text
	}
	seen := map[string]bool{}
	var b strings.Builder
	b.WriteString(text)
	for _, mt := range matches {
		name := strings.TrimSpace(mt[1])
		key := strings.ToLower(name)
		if name == "" || seen[key] {
			continue
		}
		seen[key] = true
		block := m.subagentMentionBlock(name)
		if block == "" {
			continue
		}
		b.WriteString("\n\n")
		b.WriteString(block)
	}
	return b.String()
}

func (m Model) subagentMentionBlock(name string) string {
	row, ok := m.findSubagentByName(name)
	if !ok {
		return fmt.Sprintf("---\nSub-agent %q was mentioned but is not in this session.\n", name)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "---\nSub-agent context: %s\n", row.Name)
	if row.Role != "" {
		fmt.Fprintf(&b, "role: %s\n", row.Role)
	}
	st := row.Status
	if st == "" {
		st = "unknown"
	}
	fmt.Fprintf(&b, "status: %s\n", st)
	if row.Summary != "" {
		fmt.Fprintf(&b, "summary: %s\n", strings.TrimSpace(row.Summary))
	}
	if row.Err != "" {
		fmt.Fprintf(&b, "error: %s\n", strings.TrimSpace(row.Err))
	}
	sid := row.ChildSessionID
	if sid == "" && !strings.HasPrefix(row.ID, "sub_") && !strings.HasPrefix(row.ID, "job:") {
		sid = row.ID
	}
	if sid != "" && m.store != nil {
		if body := m.subagentSessionExcerpt(sid); body != "" {
			b.WriteString("transcript:\n")
			b.WriteString(body)
			b.WriteByte('\n')
		}
	}
	out := b.String()
	if utf8.RuneCountInString(out) > maxAtMentionContext {
		r := []rune(out)
		out = string(r[:maxAtMentionContext-1]) + "…"
	}
	return out
}

func (m Model) findSubagentByName(name string) (subagentRow, bool) {
	for _, row := range m.atPickerAgents() {
		if strings.EqualFold(row.Name, name) {
			return row, true
		}
	}
	for _, row := range m.collectSubagentRows() {
		if strings.EqualFold(row.Name, name) {
			return row, true
		}
	}
	return subagentRow{}, false
}

func (m Model) subagentSessionExcerpt(sessionID string) string {
	if m.store == nil || sessionID == "" {
		return ""
	}
	ctx := context.Background()
	var parts []string
	msgs, err := m.store.ListMessages(ctx, sessionID)
	if err == nil {
		for _, msg := range msgs {
			if msg.Role != "user" {
				continue
			}
			ps, err := m.store.ListParts(ctx, msg.ID)
			if err != nil {
				break
			}
			var b strings.Builder
			for _, p := range ps {
				if p.Type == "text" && p.Text != nil {
					b.WriteString(*p.Text)
				}
			}
			if s := strings.TrimSpace(b.String()); s != "" {
				parts = append(parts, "task: "+truncateRunes(s, 800))
				break
			}
		}
	}
	if summary, err := agent.LastAssistantText(ctx, m.store, sessionID); err == nil && strings.TrimSpace(summary) != "" {
		parts = append(parts, "last reply: "+strings.TrimSpace(summary))
	}
	if act := m.latestToolActivity(sessionID); act != "" {
		parts = append(parts, "last tool: "+act)
	}
	return strings.Join(parts, "\n")
}

func listProjectFiles(root, filter string) []string {
	if root == "" {
		return nil
	}
	filter = strings.ToLower(filter)
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", ".lazykoder", "bin", "node_modules":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		if filter != "" && !strings.Contains(strings.ToLower(rel), filter) {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		if len(out) >= maxAtPickerFiles {
			return fs.SkipAll
		}
		return nil
	})
	return out
}
