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

func TestRenderWrapsParagraphAtWidth(t *testing.T) {
	const width = 40
	text := strings.Repeat("This is a long sentence with several words. ", 6)
	lines := strings.Split(ansi.Strip(Render(text, width)), "\n")
	if len(lines) < 2 {
		t.Fatalf("long paragraph not wrapped: %q", lines)
	}
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line width = %d, want <= %d: %q", w, width, line)
		}
	}
	if joined := strings.Join(lines, " "); joined != strings.TrimSpace(text) {
		t.Errorf("wrapping changed the text:\nwant %q\ngot  %q", strings.TrimSpace(text), joined)
	}
}

func TestRenderWrapsShortLineUntouched(t *testing.T) {
	input := "**A short bold line**"
	got := Render(input, 80)
	if strings.Contains(got, "\n") {
		t.Errorf("short line should stay on one line: %q", got)
	}
}

func TestRenderWrapKeepsInlineStyles(t *testing.T) {
	const width = 20
	text := "Here is some `inline code` and **bold text** that should survive wrapping"
	got := ansi.Strip(Render(text, width))
	joined := strings.Join(strings.Split(got, "\n"), " ")
	if !strings.Contains(joined, "inline code") || !strings.Contains(joined, "bold text") {
		t.Errorf("inline styles lost in wrapped output: %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line width = %d, want <= %d: %q", w, width, line)
		}
	}
}

func TestRenderWrapsUnorderedItemWithHangingIndent(t *testing.T) {
	const width = 30
	item := "- " + strings.Repeat("word ", 10) + "end"
	lines := strings.Split(ansi.Strip(Render(item, width)), "\n")
	if len(lines) < 2 {
		t.Fatalf("long list item not wrapped: %q", lines)
	}
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("list line width = %d, want <= %d: %q", w, width, line)
		}
	}
	if !strings.HasPrefix(lines[0], "• ") {
		t.Errorf("first list line missing bullet: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "  ") {
		t.Errorf("continuation line missing hanging indent: %q", lines[1])
	}
}

func TestRenderWrapsOrderedItemWithHangingIndent(t *testing.T) {
	const width = 30
	item := "10. " + strings.Repeat("word ", 10) + "end"
	lines := strings.Split(ansi.Strip(Render(item, width)), "\n")
	if len(lines) < 2 {
		t.Fatalf("long ordered item not wrapped: %q", lines)
	}
	if !strings.HasPrefix(lines[0], "10. ") {
		t.Errorf("first line missing number prefix: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], strings.Repeat(" ", len("10. "))) {
		t.Errorf("continuation line missing hanging indent: %q", lines[1])
	}
}

func TestRenderWrapsBlockquote(t *testing.T) {
	const width = 30
	quote := "> " + strings.Repeat("quoted words ", 12)
	lines := strings.Split(ansi.Strip(Render(quote, width)), "\n")
	if len(lines) < 2 {
		t.Fatalf("long quote not wrapped: %q", lines)
	}
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("quote line width = %d, want <= %d: %q", w, width, line)
		}
	}
}

func TestRenderWrapsHeading(t *testing.T) {
	const width = 20
	heading := "## " + strings.Repeat("a ", 20) + "end"
	for _, line := range strings.Split(ansi.Strip(Render(heading, width)), "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("heading line width = %d, want <= %d: %q", w, width, line)
		}
	}
}
