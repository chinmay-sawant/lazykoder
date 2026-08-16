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

func TestRenderTableAlignsColumns(t *testing.T) {
	input := "| Path | Purpose |\n| --- | --- |\n| main.go | entry point |\n| internal/ | core packages |"
	got := ansi.Strip(Render(input, 80))
	for _, want := range []string{"Path", "Purpose", "main.go", "entry point", "internal/", "core packages"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "| ---") {
		t.Errorf("Render() left the separator row: %q", got)
	}
	lines := strings.Split(got, "\n")
	if len(lines) < 5 {
		t.Fatalf("table too short: %q", got)
	}
	borderW := lipgloss.Width(lines[0])
	for _, line := range lines[1:] {
		if w := lipgloss.Width(line); w != borderW {
			t.Errorf("table line width = %d, want %d: %q", w, borderW, line)
		}
	}
	if !strings.Contains(got, "│ Path") || !strings.Contains(got, "│ main.go") {
		t.Errorf("table cells not framed with rails: %q", got)
	}
}

func TestRenderTableWidthCapsColumns(t *testing.T) {
	got := ansi.Strip(Render("| A | BBB |\n| - | - |\n| x | longvalue |", 12))
	for _, line := range strings.Split(got, "\n") {
		if w := lipgloss.Width(line); w > 12 {
			t.Errorf("table line wider than cap: w=%d %q", w, line)
		}
	}
	if !strings.Contains(got, "…") {
		t.Errorf("capped table should truncate cells: %q", got)
	}
}

func TestRenderTableEscapedPipeAndAlignment(t *testing.T) {
	input := "| expr | note |\n| :-: | ---: |\n| `a\\|b` | 42 |"
	got := ansi.Strip(Render(input, 80))
	if !strings.Contains(got, "a|b") {
		t.Errorf("escaped pipe lost: %q", got)
	}
	if !strings.Contains(got, "expr") || !strings.Contains(got, "42") {
		t.Errorf("aligned separator table missing cells: %q", got)
	}
}

func TestRenderTableNotDetectedWithoutSeparator(t *testing.T) {
	input := "| Path | Purpose |\n| main.go | entry point |"
	got := ansi.Strip(Render(input, 80))
	if !strings.Contains(got, "|") {
		t.Errorf("plain pipe lines should pass through: %q", got)
	}
	if strings.Contains(got, "╭") {
		t.Errorf("plain pipe lines must not draw a table: %q", got)
	}
}
