package markdown

import (
	"os"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestGlowRenderBasicMarkdown(t *testing.T) {
	input := "# Main Title\n\n## Sub Title\n\n### Section\n\nThis is **bold**, *italic*, and `inline code`.\n\n> A blockquote line\n\n- Bullet item A\n- Bullet item B\n\n1. Numbered 1\n2. Numbered 2\n"
	got := ansi.Strip(Render(input, 80))

	for _, want := range []string{
		"Main Title",
		"Sub Title",
		"Section",
		"bold",
		"italic",
		"inline code",
		"blockquote",
		"Bullet item A",
		"Bullet item B",
		"Numbered 1",
		"Numbered 2",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() missing %q in output:\n%s", want, got)
		}
	}
}

func TestGlowRenderCodeBlocks(t *testing.T) {
	input := "```go\npackage main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n```"
	got := ansi.Strip(Render(input, 80))

	if !strings.Contains(got, "package main") || !strings.Contains(got, "func main()") {
		t.Fatalf("Render() missing code content: %q", got)
	}
	if strings.Contains(got, "```") {
		t.Errorf("Render() left fence markers: %q", got)
	}
}

func TestGlowRenderStreamingPartialFence(t *testing.T) {
	// Partial markdown during streaming: unclosed code fence
	input := "```go\npackage main\nfunc main"
	got := ansi.Strip(Render(input, 80))

	if !strings.Contains(got, "package main") {
		t.Errorf("partial stream fence dropped content: %q", got)
	}
}

func TestGlowRenderTables(t *testing.T) {
	input := "| Name | Role |\n| --- | --- |\n| Alice | Lead |\n| Bob | Dev |\n"
	got := ansi.Strip(Render(input, 80))

	for _, want := range []string{"Name", "Role", "Alice", "Lead", "Bob", "Dev"} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() table missing %q: %q", want, got)
		}
	}
}

func TestGlowRenderWidthWrapping(t *testing.T) {
	const width = 40
	input := strings.Repeat("This is a long sentence testing paragraph wrapping. ", 5)
	got := ansi.Strip(Render(input, width))
	lines := strings.Split(got, "\n")

	if len(lines) < 2 {
		t.Fatalf("expected paragraph to wrap into multiple lines: %q", got)
	}
	for _, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line width %d exceeds cap %d: %q", w, width, line)
		}
	}
}

func TestGlowRenderFallbackEnvFlag(t *testing.T) {
	t.Setenv("LAZYKODER_MARKDOWN", "fallback")
	input := "```go\nfmt.Println(1)\n```"
	got := ansi.Strip(Render(input, 40))
	if !strings.Contains(got, "fmt.Println(1)") {
		t.Fatalf("fallback Render() missing content: %q", got)
	}
}

func TestGlowFallbackParity(t *testing.T) {
	input := "# Heading\n\nParagraph with **bold** and `code`.\n\n- Item 1\n- Item 2\n"

	glowOut := ansi.Strip(Render(input, 80))

	os.Setenv("LAZYKODER_MARKDOWN", "fallback")
	fallbackOut := ansi.Strip(Render(input, 80))
	os.Unsetenv("LAZYKODER_MARKDOWN")

	for _, token := range []string{"Heading", "Paragraph with", "bold", "code", "Item 1", "Item 2"} {
		if !strings.Contains(glowOut, token) {
			t.Errorf("glow missing token %q", token)
		}
		if !strings.Contains(fallbackOut, token) {
			t.Errorf("fallback missing token %q", token)
		}
	}
}
