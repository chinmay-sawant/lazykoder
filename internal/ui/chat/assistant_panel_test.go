package chat

import (
	"strings"
	"testing"

	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

func TestAssistantPanelsDoNotPaintTheCanvasOverMarkdown(t *testing.T) {
	t.Cleanup(func() { theme.SetMode(string(theme.ModeDark)) })
	theme.SetMode(string(theme.ModeDark))
	configureThemeStyles()

	m := New(Options{
		Store:   newTestStore(t),
		Client:  deadClient(),
		Workdir: t.TempDir(),
	})
	m.width = 80
	item := transcriptItem{kind: itemAssistant, text: "panel background probe with `inline code`\n\n```go\nsecond response row\n```"}
	m.items = []transcriptItem{item}
	m.syncTranscript()

	assertAssistantPanelBackground(t, "main chat", m.transcriptContent())

	m.subagentLogItems = []transcriptItem{item}
	assertAssistantPanelBackground(t, "subagent log", m.renderSubagentLogContent())
}

func assertAssistantPanelBackground(t *testing.T, location, rendered string) {
	t.Helper()
	if !strings.Contains(rendered, "48;2;16;40;50") {
		t.Fatalf("%s is missing the assistant panel background: %q", location, rendered)
	}
	if strings.Contains(rendered, "48;2;5;5;5") {
		t.Fatalf("%s painted the canvas inside the assistant panel: %q", location, rendered)
	}
	assertAssistantPanelRowsAreFilled(t, location, rendered)
}

func assertAssistantPanelRowsAreFilled(t *testing.T, location, rendered string) {
	t.Helper()
	const panelBackground = "48;2;16;40;50"

	for _, line := range strings.Split(rendered, "\n") {
		if !strings.Contains(line, "panel background probe") && !strings.Contains(line, "second response row") {
			continue
		}
		backgrounds := ansiCellBackgrounds(line)
		started := false
		for column, background := range backgrounds {
			if background == panelBackground {
				started = true
			}
			if started && background != panelBackground {
				t.Fatalf("%s panel background ended at column %d in %q", location, column, line)
			}
		}
		if !started {
			t.Fatalf("%s did not paint a panel row: %q", location, line)
		}
	}
}

func ansiCellBackgrounds(line string) []string {
	background := ""
	cells := make([]string, 0, len(line))
	for index := 0; index < len(line); {
		if line[index] == '\x1b' && index+1 < len(line) && line[index+1] == '[' {
			end := strings.IndexByte(line[index+2:], 'm')
			if end >= 0 {
				background = applySGRBackground(line[index+2:index+2+end], background)
				index += end + 3
				continue
			}
		}
		cells = append(cells, background)
		index++
	}
	return cells
}

func applySGRBackground(params, background string) string {
	if params == "" {
		return ""
	}
	values := strings.Split(params, ";")
	for index := 0; index < len(values); index++ {
		switch values[index] {
		case "0", "49":
			background = ""
		case "48":
			if index+4 < len(values) && values[index+1] == "2" {
				background = strings.Join(values[index:index+5], ";")
				index += 4
			}
		}
	}
	return background
}
