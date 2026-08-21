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
	glowRenderers = make(map[rendererKey]*glamour.TermRenderer)
)

type rendererKey struct {
	width int
	mode  theme.Mode
}

const maxGlowRenderers = 16

//go:fix inline
func uintPtr(u uint) *uint { return new(u) }

//go:fix inline
func stringPtr(s string) *string { return new(s) }

//go:fix inline
func boolPtr(b bool) *bool { return new(b) }

// glowStyleConfig constructs a theme-matched style mapping to LazyKoder palette.
func glowStyleConfig() ansi.StyleConfig {
	cfg := styles.DarkStyleConfig
	if theme.CurrentMode() == theme.ModeLight {
		cfg = styles.LightStyleConfig
	}

	// Document frame - no outer margin or blank padding
	cfg.Document.Margin = uintPtr(0)
	cfg.Document.BlockPrefix = ""
	cfg.Document.BlockSuffix = ""
	cfg.Document.Color = stringPtr(theme.TextHex())
	// Assistant panels supply the background. Leaving markdown transparent
	// keeps every reply row on the surrounding panel instead of repainting the
	// application canvas behind each glyph.
	cfg.Document.BackgroundColor = nil

	// Headings
	cfg.Heading.Bold = boolPtr(true)
	cfg.H1.Color = stringPtr(theme.TextHex())
	cfg.H1.BackgroundColor = nil
	cfg.H1.Prefix = ""
	cfg.H1.Suffix = ""
	cfg.H1.Bold = boolPtr(true)

	cfg.H2.Color = stringPtr(theme.AccentHex())
	cfg.H2.Prefix = ""
	cfg.H2.Bold = boolPtr(true)

	cfg.H3.Color = stringPtr(theme.MuteHex())
	cfg.H3.Prefix = ""
	cfg.H3.Bold = boolPtr(true)

	// Blockquote
	cfg.BlockQuote.Color = stringPtr(theme.MuteHex())
	cfg.BlockQuote.Indent = uintPtr(1)
	cfg.BlockQuote.IndentToken = stringPtr("│ ")

	// Inline code
	cfg.Code.Color = stringPtr(theme.TextHex())
	cfg.Code.BackgroundColor = nil
	cfg.Code.Prefix = ""
	cfg.Code.Suffix = ""

	// Code block
	cfg.CodeBlock.Margin = uintPtr(0)
	cfg.CodeBlock.Color = stringPtr(theme.MuteHex())
	if cfg.CodeBlock.Chroma != nil {
		cfg.CodeBlock.Chroma.Background.BackgroundColor = nil
	}

	// Table
	cfg.Table.StyleBlock.Margin = uintPtr(0)

	// Links
	cfg.Link.Color = stringPtr(theme.AccentHex())
	cfg.LinkText.Color = stringPtr(theme.TextHex())

	// Lists
	cfg.Item.BlockPrefix = "• "

	return cfg
}

func getGlowRenderer(width int) (*glamour.TermRenderer, error) {
	key := rendererKey{width: width, mode: theme.CurrentMode()}
	glowMu.RLock()
	r, ok := glowRenderers[key]
	glowMu.RUnlock()
	if ok {
		return r, nil
	}

	glowMu.Lock()
	defer glowMu.Unlock()
	if r, ok := glowRenderers[key]; ok {
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
	if len(glowRenderers) > maxGlowRenderers {
		glowRenderers = make(map[rendererKey]*glamour.TermRenderer)
	}
	glowRenderers[key] = renderer
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
