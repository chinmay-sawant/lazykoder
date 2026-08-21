package markdown

import (
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	xansi "github.com/charmbracelet/x/ansi"

	"github.com/chinmay-sawant/lazykoder/internal/ui/theme"
)

var (
	glowMu        sync.RWMutex
	glowRenderers = make(map[int]*glamour.TermRenderer)
)

//go:fix inline
func uintPtr(u uint) *uint { return new(u) }

//go:fix inline
func stringPtr(s string) *string { return new(s) }

//go:fix inline
func boolPtr(b bool) *bool { return new(b) }

// glowStyleConfig constructs a theme-matched Dark style mapping to LazyKoder palette.
func glowStyleConfig() ansi.StyleConfig {
	cfg := styles.DarkStyleConfig

	// Document frame - no outer margin or blank padding
	cfg.Document.Margin = uintPtr(0)
	cfg.Document.BlockPrefix = ""
	cfg.Document.BlockSuffix = ""
	cfg.Document.Color = stringPtr(theme.Text)
	cfg.Document.BackgroundColor = stringPtr(theme.Bg)

	// Headings
	cfg.Heading.Bold = boolPtr(true)
	cfg.H1.Color = stringPtr(theme.Text)
	cfg.H1.BackgroundColor = nil
	cfg.H1.Prefix = ""
	cfg.H1.Suffix = ""
	cfg.H1.Bold = boolPtr(true)

	cfg.H2.Color = stringPtr(theme.Accent)
	cfg.H2.Prefix = ""
	cfg.H2.Bold = boolPtr(true)

	cfg.H3.Color = stringPtr(theme.Mute)
	cfg.H3.Prefix = ""
	cfg.H3.Bold = boolPtr(true)

	// Blockquote
	cfg.BlockQuote.Color = stringPtr(theme.Mute)
	cfg.BlockQuote.Indent = uintPtr(1)
	cfg.BlockQuote.IndentToken = stringPtr("│ ")

	// Inline code
	cfg.Code.Color = stringPtr(theme.Text)
	cfg.Code.BackgroundColor = stringPtr(theme.Bg)
	cfg.Code.Prefix = ""
	cfg.Code.Suffix = ""

	// Code block
	cfg.CodeBlock.Margin = uintPtr(0)
	cfg.CodeBlock.Color = stringPtr(theme.Mute)
	if cfg.CodeBlock.Chroma != nil {
		cfg.CodeBlock.Chroma.Background.BackgroundColor = stringPtr(theme.Bg)
	}

	// Table
	cfg.Table.StyleBlock.Margin = uintPtr(0)

	// Links
	cfg.Link.Color = stringPtr(theme.Accent)
	cfg.LinkText.Color = stringPtr(theme.Text)

	// Lists
	cfg.Item.BlockPrefix = "• "

	return cfg
}

func getGlowRenderer(width int) (*glamour.TermRenderer, error) {
	glowMu.RLock()
	r, ok := glowRenderers[width]
	glowMu.RUnlock()
	if ok {
		return r, nil
	}

	glowMu.Lock()
	defer glowMu.Unlock()
	if r, ok := glowRenderers[width]; ok {
		return r, nil
	}

	opts := []glamour.TermRendererOption{
		glamour.WithStyles(glowStyleConfig()),
		glamour.WithPreservedNewLines(),
	}
	if width > 0 {
		opts = append(opts, glamour.WithWordWrap(width))
	} else {
		opts = append(opts, glamour.WithWordWrap(0))
	}

	renderer, err := glamour.NewTermRenderer(opts...)
	if err != nil {
		return nil, err
	}

	// Keep cache bounded to recent widths
	if len(glowRenderers) > 16 {
		glowRenderers = make(map[int]*glamour.TermRenderer)
	}
	glowRenderers[width] = renderer
	return renderer, nil
}

func cleanGlowOutput(s string) string {
	lines := strings.Split(s, "\n")
	var cleaned []string
	for _, line := range lines {
		stripped := strings.TrimSpace(xansi.Strip(line))
		if len(cleaned) == 0 && stripped == "" {
			continue
		}
		cleaned = append(cleaned, line)
	}
	for len(cleaned) > 0 && strings.TrimSpace(xansi.Strip(cleaned[len(cleaned)-1])) == "" {
		cleaned = cleaned[:len(cleaned)-1]
	}
	return strings.Join(cleaned, "\n")
}

// renderGlow formats markdown using Glamour.
func renderGlow(input string, width int) (string, error) {
	renderer, err := getGlowRenderer(width)
	if err != nil {
		return "", err
	}
	out, err := renderer.Render(input)
	if err != nil {
		return "", err
	}
	return cleanGlowOutput(out), nil
}
