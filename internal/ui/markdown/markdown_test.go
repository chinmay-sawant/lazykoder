package markdown

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderFormatsCommonMarkdown(t *testing.T) {
	input := "```python\nprint(\"Hello, World!\")\n```\n\n**How to run it:**\n\n1. Save the code in `hello.py`\n2. Run `python hello.py`"
	got := ansi.Strip(Render(input, 80))
	for _, want := range []string{
		"python",
		"print(\"Hello, World!\")",
		"How to run it:",
		"1. Save the code in",
		"hello.py",
		"2. Run python hello.py",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "```") {
		t.Errorf("Render() left fence markers in output: %q", got)
	}
	if strings.Contains(got, "╭") || strings.Contains(got, "╰") {
		t.Errorf("Render() still draws a code border: %q", got)
	}
}

func TestRenderCodeBlockSpansWidth(t *testing.T) {
	const width = 48
	got := ansi.Strip(Render("```go\nfmt.Println(1)\n```", width))
	if !strings.Contains(got, "fmt.Println(1)") {
		t.Fatalf("code body missing: %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "fmt.Println(1)") {
			if w := lipgloss.Width(line); w != width {
				t.Errorf("code line width = %d, want %d: %q", w, width, line)
			}
			return
		}
	}
}
