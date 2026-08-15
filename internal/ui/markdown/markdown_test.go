package markdown

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderFormatsCommonMarkdown(t *testing.T) {
	input := "```python\nprint(\"Hello, World!\")\n```\n\n**How to run it:**\n\n1. Save the code in `hello.py`\n2. Run `python hello.py`"
	got := ansi.Strip(Render(input))
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
	if !strings.Contains(got, "╭") || !strings.Contains(got, "╰") {
		t.Errorf("Render() missing code block border: %q", got)
	}
}
