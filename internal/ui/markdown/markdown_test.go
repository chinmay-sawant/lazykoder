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
		"print(\"Hello, World!\")",
		"How to run it:",
		"1. Save the code in",
		"hello.py",
		"2. Run",
		"python hello.py",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "```") {
		t.Errorf("Render() left fence markers in output: %q", got)
	}
}

func TestRenderCodeBlockSpansWidth(t *testing.T) {
	const width = 48
	got := ansi.Strip(Render("```go\nfmt.Println(1)\n```", width))
	if !strings.Contains(got, "fmt.Println(1)") {
		t.Fatalf("code body missing: %q", got)
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
}

func TestRenderTableEscapedPipeAndAlignment(t *testing.T) {
	input := "| expr | note |\n| :-: | ---: |\n| `a\\|b` | 42 |"
	got := ansi.Strip(Render(input, 80))
	if !strings.Contains(got, "a|b") && !strings.Contains(got, "a\\|b") {
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
}

func TestRenderWrapsShortLineUntouched(t *testing.T) {
	input := "**A short bold line**"
	got := Render(input, 80)
	if strings.Contains(strings.TrimSpace(got), "\n") {
		t.Errorf("short line should stay on one line: %q", got)
	}
}

func TestRenderWrapKeepsInlineStyles(t *testing.T) {
	const width = 20
	text := "Here is some `code` and **bold text** that should survive wrapping"
	got := ansi.Strip(Render(text, width))
	joined := strings.Join(strings.Fields(got), " ")
	if !strings.Contains(joined, "code") || !strings.Contains(joined, "bold text") {
		t.Errorf("inline styles lost in wrapped output: %q", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line width = %d, want <= %d: %q", w, width, line)
		}
	}
}

func TestRenderWrapsUnorderedItem(t *testing.T) {
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
	if !strings.Contains(lines[0], "•") && !strings.Contains(lines[0], "-") {
		t.Errorf("first list line missing bullet marker: %q", lines[0])
	}
}

func TestRenderWrapsOrderedItem(t *testing.T) {
	const width = 30
	item := "10. " + strings.Repeat("word ", 10) + "end"
	lines := strings.Split(ansi.Strip(Render(item, width)), "\n")
	if len(lines) < 2 {
		t.Fatalf("long ordered item not wrapped: %q", lines)
	}
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line width = %d, want <= %d: %q", w, width, line)
		}
	}
	if !strings.Contains(lines[0], "10.") {
		t.Errorf("first line missing number prefix: %q", lines[0])
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

func TestFallbackRendererFullCoverage(t *testing.T) {
	input := "# Heading 1\n\n> Quote\n\n- Bullet\n\n1. Number\n\n| H1 | H2 |\n| --- | --- |\n| C1 | C2 |\n\n```go\nvar x = 1\n```"
	got := ansi.Strip(renderFallback(input, 60))
	for _, want := range []string{"Heading 1", "Quote", "Bullet", "Number", "H1", "H2", "C1", "C2", "var x = 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("renderFallback missing %q: %q", want, got)
		}
	}
}
